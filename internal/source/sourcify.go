package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rbee3u/yangjian/internal/config"
	"golang.org/x/time/rate"
)

type ChainID int64

func (c *ChainID) UnmarshalJSON(data []byte) error {
	text := strings.Trim(string(data), `"`)
	if text == "" || text == "null" {
		*c = 0
		return nil
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("parse chain id %q: %w", text, err)
	}
	*c = ChainID(value)
	return nil
}

var ErrNotFound = errors.New("sourcify: contract not found")

const maxResponseBytes = 32 << 20

type Client struct {
	endpoint   string
	http       *http.Client
	limiter    *rate.Limiter
	maxRetries int
}

func NewSourcify(profile config.ProviderProfile) (*Client, error) {
	endpoint := strings.TrimRight(profile.Endpoint, "/")
	if endpoint == "" {
		endpoint = "https://sourcify.dev/server"
	}
	timeout, err := profile.TimeoutDuration()
	if err != nil {
		return nil, err
	}
	rps := profile.RateLimitRPS
	if rps <= 0 {
		rps = 5
	}
	burst := profile.Burst
	if burst <= 0 {
		burst = int(rps)
	}
	return &Client{
		endpoint:   endpoint,
		http:       &http.Client{Timeout: timeout},
		limiter:    rate.NewLimiter(rate.Limit(rps), burst),
		maxRetries: profile.MaxRetries,
	}, nil
}

func (c *Client) Fetch(ctx context.Context, chainID int64, address string) (Record, error) {
	endpoint := fmt.Sprintf("%s/v2/contract/%d/%s?fields=all", c.endpoint, chainID, address)
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return Record{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return Record{}, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if !isRetryableNet(err) || attempt == c.maxRetries {
				return Record{}, err
			}
			if err := sleepBackoff(ctx, attempt); err != nil {
				return Record{}, err
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if readErr != nil {
			return Record{}, readErr
		}
		switch {
		case resp.StatusCode == http.StatusNotFound:
			return Record{}, ErrNotFound
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("sourcify status %d: %s", resp.StatusCode, truncate(body))
			if attempt == c.maxRetries {
				return Record{}, lastErr
			}
			if err := sleepBackoff(ctx, attempt); err != nil {
				return Record{}, err
			}
			continue
		case resp.StatusCode != http.StatusOK:
			return Record{}, fmt.Errorf("sourcify status %d: %s", resp.StatusCode, truncate(body))
		}
		var record Record
		if err := json.Unmarshal(body, &record); err != nil {
			return Record{}, fmt.Errorf("parse sourcify response: %w", err)
		}
		record.Raw = body
		return record, nil
	}
	return Record{}, lastErr
}

func isRetryableNet(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func sleepBackoff(ctx context.Context, attempt int) error {
	delay := time.Duration(1<<attempt) * 200 * time.Millisecond
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

func truncate(body []byte) string {
	const limit = 300
	if len(body) > limit {
		return string(body[:limit]) + "..."
	}
	return string(body)
}

type Record struct {
	Address         string            `json:"address"`
	ChainID         ChainID           `json:"chainId"`
	Match           string            `json:"match"`
	CreationMatch   string            `json:"creationMatch"`
	RuntimeMatch    string            `json:"runtimeMatch"`
	VerifiedAt      string            `json:"verifiedAt"`
	ABI             json.RawMessage   `json:"abi"`
	Metadata        json.RawMessage   `json:"metadata"`
	Sources         map[string]Source `json:"sources"`
	StdJSONInput    json.RawMessage   `json:"stdJsonInput"`
	Compilation     Compilation       `json:"compilation"`
	ProxyResolution ProxyResolution   `json:"proxyResolution"`
	StorageLayout   json.RawMessage   `json:"storageLayout"`
	RuntimeBytecode RuntimeBytecode   `json:"runtimeBytecode"`
	Raw             []byte            `json:"-"`
}

type Source struct {
	Content string `json:"content"`
}

type Metadata struct {
	Compiler struct {
		Version string `json:"version"`
	} `json:"compiler"`
	Language string                    `json:"language"`
	Settings Settings                  `json:"settings"`
	Sources  map[string]MetadataSource `json:"sources"`
}

type MetadataSource struct {
	Keccak256 string   `json:"keccak256"`
	License   string   `json:"license,omitempty"`
	URLs      []string `json:"urls,omitempty"`
}

type Settings struct {
	CompilationTarget map[string]string `json:"compilationTarget"`
	EVMVersion        string            `json:"evmVersion"`
	Libraries         map[string]any    `json:"libraries"`
	Metadata          struct {
		BytecodeHash string `json:"bytecodeHash"`
	} `json:"metadata"`
	Optimizer struct {
		Enabled bool `json:"enabled"`
		Runs    int  `json:"runs"`
	} `json:"optimizer"`
	Remappings []string `json:"remappings"`
}

type Compilation struct {
	Language           string   `json:"language"`
	Compiler           string   `json:"compiler"`
	CompilerVersion    string   `json:"compilerVersion"`
	CompilerSettings   Settings `json:"compilerSettings"`
	Name               string   `json:"name"`
	FullyQualifiedName string   `json:"fullyQualifiedName"`
}

type ProxyResolution struct {
	IsProxy         bool     `json:"isProxy"`
	ProxyType       string   `json:"proxyType"`
	Implementations []string `json:"implementations"`
}

type RuntimeBytecode struct {
	OnchainBytecode    string           `json:"onchainBytecode"`
	RecompiledBytecode string           `json:"recompiledBytecode"`
	Transformations    []Transformation `json:"transformations"`
}

type Transformation struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Reason string `json:"reason"`
}

func (r Record) ParseMetadata() (Metadata, error) {
	var metadata Metadata
	if len(r.Metadata) == 0 {
		return metadata, fmt.Errorf("sourcify metadata is empty")
	}
	if err := json.Unmarshal(r.Metadata, &metadata); err != nil {
		return metadata, fmt.Errorf("parse sourcify metadata: %w", err)
	}
	return metadata, nil
}

func (r Record) ParseStdJSONInputSources() map[string]Source {
	var input struct {
		Sources map[string]Source `json:"sources"`
	}
	if len(r.StdJSONInput) > 0 {
		_ = json.Unmarshal(r.StdJSONInput, &input)
	}
	return input.Sources
}

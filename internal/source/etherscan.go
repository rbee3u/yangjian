package source

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/config"
	"golang.org/x/time/rate"
)

var (
	ErrMissingAPIKey = errors.New("etherscan: API key is not configured")
	ErrNotVerified   = errors.New("etherscan: contract source is not verified")
)

type EtherscanClient struct {
	endpoint   string
	apiKey     string
	http       *http.Client
	limiter    *rate.Limiter
	maxRetries int
}

func NewEtherscan(profile config.ProviderProfile, apiKey string) (*EtherscanClient, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, ErrMissingAPIKey
	}
	endpoint := strings.TrimRight(profile.Endpoint, "/")
	if endpoint == "" {
		endpoint = "https://api.etherscan.io/v2/api"
	}
	timeout, err := profile.TimeoutDuration()
	if err != nil {
		return nil, err
	}
	rps := profile.RateLimitRPS
	if rps <= 0 {
		rps = 3
	}
	burst := profile.Burst
	if burst <= 0 {
		burst = int(rps)
	}
	return &EtherscanClient{
		endpoint:   endpoint,
		apiKey:     apiKey,
		http:       &http.Client{Timeout: timeout},
		limiter:    rate.NewLimiter(rate.Limit(rps), burst),
		maxRetries: profile.MaxRetries,
	}, nil
}

func (c *EtherscanClient) Fetch(ctx context.Context, chainID int64, address string) (Record, error) {
	query := url.Values{}
	query.Set("chainid", strconv.FormatInt(chainID, 10))
	query.Set("module", "contract")
	query.Set("action", "getsourcecode")
	query.Set("address", address)
	query.Set("apikey", c.apiKey)
	endpoint := c.endpoint + "?" + query.Encode()

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return Record{}, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return Record{}, err
		}
		response, err := c.http.Do(request)
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
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
		response.Body.Close()
		if readErr != nil {
			return Record{}, readErr
		}
		switch {
		case response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500:
			lastErr = fmt.Errorf("etherscan status %d", response.StatusCode)
			if attempt == c.maxRetries {
				return Record{}, lastErr
			}
			if err := sleepBackoff(ctx, attempt); err != nil {
				return Record{}, err
			}
			continue
		case response.StatusCode != http.StatusOK:
			return Record{}, fmt.Errorf("etherscan status %d", response.StatusCode)
		}
		record, err := parseEtherscanResponse(body, chainID, address)
		if err != nil {
			return Record{}, err
		}
		record.Raw = body
		return record, nil
	}
	return Record{}, lastErr
}

func parseEtherscanResponse(body []byte, chainID int64, address string) (Record, error) {
	var envelope struct {
		Status  string          `json:"status"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Record{}, fmt.Errorf("parse etherscan envelope: %w", err)
	}
	if envelope.Status != "1" {
		message := strings.TrimSpace(envelope.Message)
		var text string
		if json.Unmarshal(envelope.Result, &text) == nil && text != "" {
			message += ": " + text
		}
		lower := strings.ToLower(message)
		if strings.Contains(lower, "not verified") || strings.Contains(lower, "no matching") ||
			strings.Contains(lower, "unable to locate") {
			return Record{}, ErrNotVerified
		}
		if message == "" {
			message = "unknown response"
		}
		return Record{}, fmt.Errorf("etherscan: %s", message)
	}
	var results []struct {
		SourceCode       string `json:"SourceCode"`
		ABI              string `json:"ABI"`
		ContractName     string `json:"ContractName"`
		CompilerVersion  string `json:"CompilerVersion"`
		OptimizationUsed string `json:"OptimizationUsed"`
		Runs             string `json:"Runs"`
		EVMVersion       string `json:"EVMVersion"`
		LicenseType      string `json:"LicenseType"`
		Proxy            string `json:"Proxy"`
		Implementation   string `json:"Implementation"`
		SwarmSource      string `json:"SwarmSource"`
	}
	if err := json.Unmarshal(envelope.Result, &results); err != nil {
		return Record{}, fmt.Errorf("parse etherscan result: %w", err)
	}
	if len(results) == 0 {
		return Record{}, ErrNotVerified
	}
	entry := results[0]
	sources, settings, err := parseEtherscanSources(entry.SourceCode, entry.ContractName, address)
	if err != nil {
		return Record{}, err
	}
	settings.EVMVersion = entry.EVMVersion
	settings.Optimizer.Enabled = entry.OptimizationUsed == "1"
	if runs, err := strconv.Atoi(entry.Runs); err == nil {
		settings.Optimizer.Runs = runs
	}
	compilerVersion := strings.TrimPrefix(strings.TrimSpace(entry.CompilerVersion), "v")
	abi := json.RawMessage(strings.TrimSpace(entry.ABI))
	if len(abi) > 0 {
		var value any
		if json.Unmarshal(abi, &value) == nil {
			if compact, err := json.Marshal(value); err == nil {
				abi = compact
			}
		}
	}
	record := Record{
		Address:    address,
		ChainID:    ChainID(chainID),
		Match:      "etherscan_verified",
		VerifiedAt: time.Now().UTC().Format(time.RFC3339),
		ABI:        abi,
		Sources:    sources,
		Compilation: Compilation{
			Language:         "Solidity",
			Compiler:         "solc",
			CompilerVersion:  compilerVersion,
			CompilerSettings: settings,
			Name:             entry.ContractName,
		},
	}
	return record, nil
}

func parseEtherscanSources(sourceCode, contractName, address string) (map[string]Source, Settings, error) {
	var settings Settings
	trimmed := strings.TrimSpace(sourceCode)
	if trimmed == "" {
		return nil, settings, fmt.Errorf("etherscan: source code is empty")
	}
	standalone := trimmed
	if strings.HasPrefix(standalone, "{{") && strings.HasSuffix(standalone, "}}") {
		standalone = strings.TrimPrefix(standalone, "{")
		standalone = strings.TrimSuffix(standalone, "}")
	}
	if strings.HasPrefix(standalone, "{") {
		var standard struct {
			Language string `json:"language"`
			Sources  map[string]struct {
				Content string `json:"content"`
			} `json:"sources"`
			Settings Settings `json:"settings"`
		}
		if err := json.Unmarshal([]byte(standalone), &standard); err == nil && len(standard.Sources) > 0 {
			sources := make(map[string]Source, len(standard.Sources))
			for path, source := range standard.Sources {
				sources[path] = Source{Content: source.Content}
			}
			return sources, standard.Settings, nil
		}
	}
	name := strings.TrimSpace(contractName)
	if name == "" {
		name = strings.TrimSpace(address)
	}
	path := name + ".sol"
	return map[string]Source{path: {Content: trimmed}}, settings, nil
}

// CompareRecords checks whether the secondary verified-source record agrees
// with the primary record on ABI, compiler settings, and source content.
// Paths are compared by content hash because explorers normalize them.
func CompareRecords(primary, secondary Record) (bool, []string) {
	differences := make([]string, 0)
	if normalizeCompilerVersion(primary.Compilation.CompilerVersion) != normalizeCompilerVersion(secondary.Compilation.CompilerVersion) {
		differences = append(differences, fmt.Sprintf("compiler version %q != %q",
			primary.Compilation.CompilerVersion, secondary.Compilation.CompilerVersion))
	}
	if primary.Compilation.CompilerSettings.Optimizer.Enabled != secondary.Compilation.CompilerSettings.Optimizer.Enabled {
		differences = append(differences, "optimizer enabled flag differs")
	} else if primary.Compilation.CompilerSettings.Optimizer.Enabled &&
		primary.Compilation.CompilerSettings.Optimizer.Runs != secondary.Compilation.CompilerSettings.Optimizer.Runs {
		differences = append(differences, fmt.Sprintf("optimizer runs %d != %d",
			primary.Compilation.CompilerSettings.Optimizer.Runs, secondary.Compilation.CompilerSettings.Optimizer.Runs))
	}
	if primary.Compilation.CompilerSettings.EVMVersion != "" && secondary.Compilation.CompilerSettings.EVMVersion != "" &&
		primary.Compilation.CompilerSettings.EVMVersion != secondary.Compilation.CompilerSettings.EVMVersion {
		differences = append(differences, fmt.Sprintf("EVM version %q != %q",
			primary.Compilation.CompilerSettings.EVMVersion, secondary.Compilation.CompilerSettings.EVMVersion))
	}
	if !equalJSON(primary.ABI, secondary.ABI) {
		differences = append(differences, "ABI differs")
	}
	primaryHashes := sourceContentHashes(primary.Sources)
	secondaryHashes := sourceContentHashes(secondary.Sources)
	if len(secondaryHashes) == 0 {
		differences = append(differences, "secondary source set is empty")
	} else if !equalStringSets(primaryHashes, secondaryHashes) {
		differences = append(differences, fmt.Sprintf("source content differs (%d vs %d files)", len(primaryHashes), len(secondaryHashes)))
	}
	return len(differences) == 0, differences
}

func normalizeCompilerVersion(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "v")
}

func equalJSON(left, right json.RawMessage) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	leftCanonical, leftErr := json.Marshal(leftValue)
	rightCanonical, rightErr := json.Marshal(rightValue)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftCanonical) == string(rightCanonical)
}

func keccakHex(content string) string {
	return hex.EncodeToString(crypto.Keccak256([]byte(content)))
}

func sourceContentHashes(sources map[string]Source) []string {
	hashes := make([]string, 0, len(sources))
	for _, source := range sources {
		hashes = append(hashes, keccakHex(source.Content))
	}
	sortStrings(hashes)
	return hashes
}

func equalStringSets(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

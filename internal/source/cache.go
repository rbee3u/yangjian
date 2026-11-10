package source

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/rbee3u/yangjian/internal/config"
)

func NewSourcifyWithCache(profile config.ProviderProfile, cacheDir string, refresh bool) (*Client, error) {
	client, err := NewSourcify(profile)
	if err != nil {
		return nil, err
	}
	client.enableCache(filepath.Join(cacheDir, "providers", "sourcify"), refresh)
	return client, nil
}

func NewEtherscanWithCache(profile config.ProviderProfile, apiKey, cacheDir string, refresh bool) (*EtherscanClient, error) {
	client, err := NewEtherscan(profile, apiKey)
	if err != nil {
		return nil, err
	}
	client.enableCache(filepath.Join(cacheDir, "providers", "etherscan"), refresh)
	return client, nil
}

func (c *Client) enableCache(dir string, refresh bool) {
	base := c.http.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c.http.Transport = &cachedTransport{base: base, dir: dir, refresh: refresh}
}

func (c *EtherscanClient) enableCache(dir string, refresh bool) {
	base := c.http.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c.http.Transport = &cachedTransport{base: base, dir: dir, refresh: refresh}
}

type cachedTransport struct {
	base    http.RoundTripper
	dir     string
	refresh bool
}

type cachedHTTPResponse struct {
	Key    string      `json:"key"`
	Status int         `json:"status"`
	Header http.Header `json:"header"`
	Body   []byte      `json:"body"`
}

func (t *cachedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodGet {
		return t.base.RoundTrip(request)
	}
	key := request.Method + "\n" + request.URL.String()
	if !t.refresh {
		if response, ok := t.read(key, request); ok {
			return response, nil
		}
	}
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	response.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("provider response exceeds %d bytes", maxResponseBytes)
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	if response.StatusCode == http.StatusOK {
		_ = t.write(key, response, body)
	}
	return response, nil
}

func (t *cachedTransport) read(key string, request *http.Request) (*http.Response, bool) {
	data, err := os.ReadFile(t.path(key))
	if err != nil {
		return nil, false
	}
	var cached cachedHTTPResponse
	if err := json.Unmarshal(data, &cached); err != nil || cached.Key != key {
		return nil, false
	}
	return &http.Response{
		Status:        http.StatusText(cached.Status),
		StatusCode:    cached.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        cached.Header,
		Body:          io.NopCloser(bytes.NewReader(cached.Body)),
		ContentLength: int64(len(cached.Body)),
		Request:       request,
	}, true
}

func (t *cachedTransport) write(key string, response *http.Response, body []byte) error {
	entry, err := json.Marshal(cachedHTTPResponse{
		Key:    key,
		Status: response.StatusCode,
		Header: response.Header,
		Body:   body,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(t.dir, 0o755); err != nil {
		return err
	}
	path := t.path(key)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, entry, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func (t *cachedTransport) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(t.dir, hex.EncodeToString(sum[:])+".json")
}

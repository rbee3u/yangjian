package source

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rbee3u/yangjian/internal/config"
)

func TestChainIDUnmarshal(t *testing.T) {
	cases := []struct {
		input string
		want  ChainID
	}{
		{`"1"`, 1},
		{`1`, 1},
		{`null`, 0},
	}
	for _, tc := range cases {
		var id ChainID
		if err := json.Unmarshal([]byte(tc.input), &id); err != nil {
			t.Fatalf("%s: %v", tc.input, err)
		}
		if id != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.input, id, tc.want)
		}
	}
	var id ChainID
	if err := json.Unmarshal([]byte(`"nope"`), &id); err == nil {
		t.Fatal("expected invalid chain id error")
	}
}

func newTestClient(t *testing.T, endpoint string, maxRetries int) *Client {
	t.Helper()
	client, err := NewSourcify(config.ProviderProfile{
		Endpoint:     endpoint,
		Timeout:      "2s",
		RateLimitRPS: 100,
		Burst:        100,
		MaxRetries:   maxRetries,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/contract/1/0xabc" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("fields") != "all" {
			t.Fatalf("fields = %s", r.URL.Query().Get("fields"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "address": "0xabc",
		  "chainId": "1",
		  "match": "match",
		  "abi": [{"type":"function","name":"slot0","inputs":[],"outputs":[],"stateMutability":"view"}],
		  "metadata": {"compiler":{"version":"0.7.6"}},
		  "sources": {},
		  "stdJsonInput": {"sources": {}},
		  "compilation": {},
		  "proxyResolution": {"isProxy": false},
		  "storageLayout": {},
		  "runtimeBytecode": {}
		}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 0)
	record, err := client.Fetch(context.Background(), 1, "0xabc")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if record.ChainID != 1 {
		t.Fatalf("chain id = %d", record.ChainID)
	}
	if record.Match != "match" {
		t.Fatalf("match = %s", record.Match)
	}
	if len(record.Raw) == 0 {
		t.Fatal("raw response must be recorded")
	}
}

func TestFetchNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 0)
	_, err := client.Fetch(context.Background(), 1, "0xabc")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestFetchRetriesServerError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"address":"0xabc","chainId":"1","match":"match","sources":{},"stdJsonInput":{},"compilation":{},"proxyResolution":{},"storageLayout":{},"runtimeBytecode":{}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, 1)
	record, err := client.Fetch(context.Background(), 1, "0xabc")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if record.Match != "match" {
		t.Fatalf("match = %s", record.Match)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

package rpcprobe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "rpc-endpoints.json")
	reg := New(1)
	ep := Endpoint{
		Name:      "publicnode",
		URL:       "https://ethereum-rpc.publicnode.com",
		Transport: "http",
		Enabled:   true,
		Source:    "chainlist",
		Tracking:  "none",
		Notes:     "primary",
		Capabilities: Capabilities{
			Blocks: true, Logs: true, Call: true, Archive: false, DebugTrace: false,
		},
		Health: Health{
			Status:    StatusOK,
			CheckedAt: time.Now().UTC().Truncate(time.Second),
			ChainID:   1,
			HeadBlock: 100,
			LatencyMS: 42,
		},
	}
	if _, isNew := reg.Ensure(ep); !isNew {
		t.Fatal("expected a new endpoint")
	}
	if err := Save(path, reg); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Endpoints) != 1 {
		t.Fatalf("endpoints = %d", len(loaded.Endpoints))
	}
	got := loaded.Endpoints[0]
	if got.Name != "publicnode" || got.Capabilities.Logs != true || got.Health.HeadBlock != 100 {
		t.Fatalf("round trip = %+v", got)
	}
	if got.Health.Status != StatusOK || got.Enabled != true {
		t.Fatalf("health/enabled = %s/%t", got.Health.Status, got.Enabled)
	}
}

func TestEnsureDoesNotOverwriteExisting(t *testing.T) {
	reg := New(1)
	first := Endpoint{Name: "alpha", URL: "https://alpha.example", Transport: "http", Enabled: true, Notes: "keep me"}
	reg.Ensure(first)
	second := Endpoint{Name: "ignored", URL: "https://alpha.example", Transport: "http", Enabled: false}
	stored, isNew := reg.Ensure(second)
	if isNew {
		t.Fatal("duplicate URL was added")
	}
	if stored.Name != "alpha" || stored.Notes != "keep me" || !stored.Enabled {
		t.Fatalf("existing endpoint was overwritten: %+v", stored)
	}
	if len(reg.Endpoints) != 1 {
		t.Fatalf("endpoints = %d", len(reg.Endpoints))
	}
}

func TestUniqueName(t *testing.T) {
	reg := New(1)
	reg.Endpoints = []Endpoint{
		{Name: "rpc-example", URL: "https://one.example", Transport: "http"},
		{Name: "rpc-example-2", URL: "https://two.example", Transport: "http"},
	}
	if got := reg.UniqueName("rpc-example"); got != "rpc-example-3" {
		t.Fatalf("name = %s", got)
	}
	if got := reg.UniqueName("RPC Example"); got != "rpc-example-3" {
		t.Fatalf("sanitized name = %s", got)
	}
}

func TestApplyResultTracksHistoryAndFailures(t *testing.T) {
	reg := New(1)
	reg.Endpoints = []Endpoint{{
		Name: "alpha", URL: "https://alpha.example", Transport: "http", Enabled: true,
		Health: Health{Status: StatusUnprobed},
	}}
	now := time.Now().UTC()
	for i := 0; i < HistoryLimit+3; i++ {
		res := Result{
			Transport: "http",
			Health: Health{
				Status:    StatusOK,
				CheckedAt: now.Add(time.Duration(i) * time.Minute),
				ChainID:   1,
				HeadBlock: uint64(100 + i),
				LatencyMS: 10,
			},
		}
		ep, ok := reg.ApplyResult("alpha", res, uint64(110+i), 30)
		if !ok {
			t.Fatal("endpoint not found")
		}
		if ep.Health.ConsecutiveFailures != 0 {
			t.Fatalf("failures = %d", ep.Health.ConsecutiveFailures)
		}
	}
	ep, _ := reg.Find("alpha")
	if len(ep.History) != HistoryLimit {
		t.Fatalf("history = %d", len(ep.History))
	}
	if ep.Health.LastOKAt == nil {
		t.Fatal("last_ok_at was not recorded")
	}
	failure := Result{
		Transport: "http",
		Health: Health{
			Status:    StatusUnreachable,
			CheckedAt: now.Add(time.Hour),
			Message:   "down",
		},
	}
	ep, _ = reg.ApplyResult("alpha", failure, 999, 30)
	if ep.Health.ConsecutiveFailures != 1 {
		t.Fatalf("consecutive failures = %d", ep.Health.ConsecutiveFailures)
	}
	if ep.Health.LastOKAt == nil {
		t.Fatal("last_ok_at was lost on failure")
	}
}

func TestApplyResultMarksLaggingEndpointStale(t *testing.T) {
	reg := New(1)
	reg.Endpoints = []Endpoint{{
		Name: "slow", URL: "https://slow.example", Transport: "http", Enabled: true,
	}}
	res := Result{
		Transport: "http",
		Health: Health{
			Status:    StatusOK,
			CheckedAt: time.Now().UTC(),
			ChainID:   1,
			HeadBlock: 950,
		},
	}
	ep, ok := reg.ApplyResult("slow", res, 1000, 30)
	if !ok {
		t.Fatal("endpoint not found")
	}
	if ep.Health.Status != StatusStale {
		t.Fatalf("status = %s", ep.Health.Status)
	}
	if ep.Health.HeadLagBlocks != 50 {
		t.Fatalf("lag = %d", ep.Health.HeadLagBlocks)
	}
}

func TestNormalizeRejectsDuplicateNameAndURL(t *testing.T) {
	reg := New(1)
	reg.Endpoints = []Endpoint{
		{Name: "same", URL: "https://one.example", Transport: "http"},
		{Name: "same", URL: "https://two.example", Transport: "http"},
	}
	if err := reg.Normalize(); err == nil || !strings.Contains(err.Error(), "duplicate endpoint name") {
		t.Fatalf("duplicate name error = %v", err)
	}
	reg.Endpoints = []Endpoint{
		{Name: "one", URL: "https://same.example", Transport: "http"},
		{Name: "two", URL: "https://same.example", Transport: "http"},
	}
	if err := reg.Normalize(); err == nil || !strings.Contains(err.Error(), "duplicate endpoint url") {
		t.Fatalf("duplicate url error = %v", err)
	}
}

func TestLoadMissingRegistryIsAnError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(filepath.Join(t.TempDir(), "missing.json")); !os.IsNotExist(err) {
		t.Fatal("test setup created the file")
	}
}

func TestParseChainlist(t *testing.T) {
	data := []byte(`[
	  {"chainId":1,"name":"Ethereum Mainnet","rpc":[
	    {"url":"https://a.example","tracking":"none"},
	    {"url":"https://a.example","tracking":"none"},
	    {"url":"wss://b.example","tracking":"limited"},
	    {"url":"ipc:///tmp/geth.ipc"}
	  ]},
	  {"chainId":10,"name":"Optimism","rpc":[{"url":"https://op.example"}]}
	]`)
	items, err := ParseChainlist(data, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].URL != "https://a.example" || items[1].URL != "wss://b.example" {
		t.Fatalf("items = %+v", items)
	}
}

func TestParseChainlistMissingChain(t *testing.T) {
	if _, err := ParseChainlist([]byte(`[{"chainId":10,"rpc":[{"url":"https://op.example"}]}]`), 1); err == nil {
		t.Fatal("expected a missing-chain error")
	}
}

func TestEndpointFromChainlistRequiresEnv(t *testing.T) {
	const name = "YANGJIAN_TEST_CHAINLIST_KEY"
	_ = os.Unsetenv(name)
	ep := EndpointFromChainlist(1, ChainlistRPC{URL: "https://keyed.example/rpc?key=${" + name + "}", Tracking: "yes"})
	if ep.Enabled {
		t.Fatal("endpoint with a missing environment variable must be disabled")
	}
	if !strings.Contains(ep.RequiresEnv, name) {
		t.Fatalf("requires_env = %s", ep.RequiresEnv)
	}
	if !ep.CredentialInURL {
		t.Fatal("query credential was not detected")
	}
	if ep.Source != "chainlist" || ep.SourceURL != "https://chainlist.org/chain/1" {
		t.Fatalf("source = %+v", ep)
	}
}

func TestRedactURL(t *testing.T) {
	raw := "https://eth-mainnet.example.com/v1/abcdefghijklmnopqrstuvwxyz012345?apikey=secret"
	redacted := RedactURL(raw)
	if strings.Contains(redacted, "secret") || strings.Contains(redacted, "abcdefghijklmnopqrstuvwxyz012345") {
		t.Fatalf("redacted URL still contains a credential: %s", redacted)
	}
	if !strings.Contains(redacted, "eth-mainnet.example.com") {
		t.Fatalf("host was removed: %s", redacted)
	}
}

func TestTransportAndNameFromURL(t *testing.T) {
	cases := []struct {
		url       string
		transport string
		name      string
	}{
		{"https://ethereum-rpc.publicnode.com", "http", "ethereum-rpc-publicnode-com"},
		{"wss://eth.drpc.org", "ws", "eth-drpc-org"},
		{"https://rpc.mevblocker.io/fast", "http", "rpc-mevblocker-io-fast"},
		{"https://eth-mainnet.example.com/v1/abcdef0123456789", "http", "eth-mainnet-example-com"},
		{"ftp://example.com", "", "example-com"},
	}
	for _, tc := range cases {
		if got := TransportOf(tc.url); got != tc.transport {
			t.Fatalf("%s transport = %s", tc.url, got)
		}
		if got := NameFromURL(tc.url); got != tc.name {
			t.Fatalf("%s name = %s, want %s", tc.url, got, tc.name)
		}
	}
}

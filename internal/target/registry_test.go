package target

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadMainnetRegistry(t *testing.T) {
	path := filepath.Join("..", "..", "targets", "mainnet.json")
	registry, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	target, err := registry.Find("uniswap-v3")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	factory, err := target.Address("factory")
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if strings.ToLower(factory) != "0x1f98431c8ad98523631ae4a59f267346ea31f984" {
		t.Fatalf("factory = %s", factory)
	}
	usdc, err := target.Token("USDC")
	if err != nil {
		t.Fatalf("usdc: %v", err)
	}
	if !strings.HasPrefix(strings.ToLower(usdc), "0xa0b86991") {
		t.Fatalf("usdc = %s", usdc)
	}
	if target.SourceURL == "" || target.SourceRetrievedAt.IsZero() {
		t.Fatal("source evidence must be recorded")
	}
	if len(target.Acceptance.PoolPair) != 2 || target.Acceptance.FeeTier != 3000 {
		t.Fatalf("acceptance = %+v", target.Acceptance)
	}
}

func TestTargetValidate(t *testing.T) {
	valid := Target{
		Slug:              "x",
		Name:              "X",
		ChainID:           1,
		SourceURL:         "https://example.com",
		SourceRetrievedAt: time.Now(),
		Contracts:         map[string]string{"factory": "0x1F98431c8aD98523631AE4a59f267346ea31F984"},
		Tokens:            map[string]string{"USDC": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"},
		Acceptance:        Acceptance{PoolPair: []string{"A", "B"}, FeeTier: 3000, PoolDiscovery: "getPool"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid target: %v", err)
	}

	bad := valid
	bad.Contracts = map[string]string{"factory": "0xnothex"}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected invalid address error")
	}

	bad = valid
	bad.SourceURL = "ftp://example.com"
	if err := bad.Validate(); err == nil {
		t.Fatal("expected invalid source URL error")
	}

	bad = valid
	bad.Acceptance.PoolPair = []string{"A"}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected invalid pool pair error")
	}
}

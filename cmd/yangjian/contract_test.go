package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rbee3u/yangjian/internal/config"
)

const testRegistry = `{
  "schema_version": "1",
  "network": {"chain_id": 1, "name": "mainnet"},
  "targets": [{
    "slug": "test-protocol",
    "name": "Test Protocol",
    "chain_id": 1,
    "source_url": "https://example.invalid/deployments",
    "source_retrieved_at": "2026-11-10T00:00:00Z",
    "contracts": {"factory": "0x1111111111111111111111111111111111111111"},
    "tokens": {
      "USDC": "0x2222222222222222222222222222222222222222",
      "WETH": "0x3333333333333333333333333333333333333333"
    },
    "acceptance": {"pool_pair": ["USDC", "WETH"], "fee_tier": 3000, "pool_discovery": "getPool"}
  }]
}`

func testConfig(t *testing.T) config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(testRegistry), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.TargetRegistry = path
	return cfg
}

func TestResolveDiscoverySymbols(t *testing.T) {
	cfg := testConfig(t)
	factory, token0, token1, err := resolveDiscovery(cfg, 1, &discoverOptions{
		targetSlug: "test-protocol",
		token0:     "USDC",
		token1:     "WETH",
		fee:        3000,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if factory.Hex() != "0x1111111111111111111111111111111111111111" {
		t.Fatalf("factory = %s", factory.Hex())
	}
	if token0.Hex() != "0x2222222222222222222222222222222222222222" {
		t.Fatalf("token0 = %s", token0.Hex())
	}
	if token1.Hex() != "0x3333333333333333333333333333333333333333" {
		t.Fatalf("token1 = %s", token1.Hex())
	}
}

func TestResolveDiscoveryExplicitAddresses(t *testing.T) {
	cfg := config.Default()
	factory, token0, token1, err := resolveDiscovery(cfg, 1, &discoverOptions{
		factory: "0x1111111111111111111111111111111111111111",
		token0:  "0x2222222222222222222222222222222222222222",
		token1:  "0x3333333333333333333333333333333333333333",
		fee:     500,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if factory.Hex() != "0x1111111111111111111111111111111111111111" || token0.Hex() == token1.Hex() {
		t.Fatalf("resolved = %s %s %s", factory.Hex(), token0.Hex(), token1.Hex())
	}
}

func TestResolveDiscoveryRejectsInvalidInput(t *testing.T) {
	cfg := testConfig(t)
	cases := []struct {
		name string
		opts discoverOptions
	}{
		{"missing tokens", discoverOptions{targetSlug: "test-protocol", fee: 3000}},
		{"missing factory", discoverOptions{token0: "USDC", token1: "WETH", fee: 3000}},
		{"both factory and target", discoverOptions{factory: "0x1111111111111111111111111111111111111111", targetSlug: "test-protocol", token0: "USDC", token1: "WETH", fee: 3000}},
		{"zero fee", discoverOptions{targetSlug: "test-protocol", token0: "USDC", token1: "WETH"}},
		{"symbol without registry", discoverOptions{factory: "0x1111111111111111111111111111111111111111", token0: "USDC", token1: "WETH", fee: 3000}},
		{"unknown token", discoverOptions{targetSlug: "test-protocol", token0: "DAI", token1: "WETH", fee: 3000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := resolveDiscovery(cfg, 1, &tc.opts); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

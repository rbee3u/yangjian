package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.DefaultChainID != 1 {
		t.Fatalf("default chain = %d", cfg.DefaultChainID)
	}
	if cfg.RPCProfile().Endpoint == "" {
		t.Fatal("default RPC endpoint must not be empty")
	}
	if cfg.Budgets.MaxContracts != 50 {
		t.Fatalf("max contracts = %d", cfg.Budgets.MaxContracts)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config invalid: %v", err)
	}
}

func TestSolcConfig(t *testing.T) {
	cfg := Default()
	if cfg.Solc.BaseURL == "" || cfg.Solc.Timeout != "120s" {
		t.Fatalf("solc defaults = %+v", cfg.Solc)
	}
	bad := Default()
	bad.Solc.Timeout = "nope"
	if err := bad.Validate(); err == nil {
		t.Fatal("expected invalid solc timeout")
	}
	local := Default()
	local.Solc.BinaryPath = "/tmp/solc"
	local.Solc.BaseURL = ""
	if err := local.Validate(); err != nil {
		t.Fatalf("local binary config: %v", err)
	}
}

func TestLoadFileAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := []byte(`{
	  "schema_version": "1",
	  "default_chain_id": 10,
	  "cache_dir": ".cache",
	  "target_registry": "targets/mainnet.json",
	  "providers": {
	    "rpc": {
	      "default": {
	        "endpoint": "http://example.invalid",
	        "timeout": "5s",
	        "rate_limit_rps": 2,
	        "burst": 2,
	        "max_retries": 1
	      }
	    },
	    "sourcify": {"endpoint": "https://sourcify.dev/server"},
	    "etherscan": {"endpoint": "https://api.etherscan.io/v2/api", "api_key_env": "ETHERSCAN_API_KEY"}
	  },
	  "budgets": {"max_depth": 2, "max_contracts": 50, "max_requests_per_dossier": 1000}
	}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("YANGJIAN_RPC_URL", "http://override.invalid")
	t.Setenv("YANGJIAN_DEFAULT_CHAIN_ID", "99")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.DefaultChainID != 99 {
		t.Fatalf("chain = %d", cfg.DefaultChainID)
	}
	if cfg.RPCProfile().Endpoint != "http://override.invalid" {
		t.Fatalf("endpoint = %s", cfg.RPCProfile().Endpoint)
	}
	if cfg.RPCProfile().RateLimitRPS != 2 {
		t.Fatalf("rate = %f", cfg.RPCProfile().RateLimitRPS)
	}
}

func TestLoadMissingExplicit(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected error for missing explicit config")
	}
}

func TestValidateRejectsBadRate(t *testing.T) {
	cfg := Default()
	profile := cfg.RPCProfile()
	profile.RateLimitRPS = 0
	cfg.Providers.RPC["default"] = profile
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid rate error")
	}
}

func TestWriteDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")
	if err := WriteDefault(path, false); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if err := WriteDefault(path, false); err == nil {
		t.Fatal("expected exists error")
	}
	if err := WriteDefault(path, true); err != nil {
		t.Fatalf("force write: %v", err)
	}
}

func TestAPIKeyEnv(t *testing.T) {
	t.Setenv("YANGJIAN_TEST_KEY", "secret")
	profile := ProviderProfile{APIKeyEnv: "YANGJIAN_TEST_KEY"}
	if got := profile.APIKey(); got != "secret" {
		t.Fatalf("api key = %q", got)
	}
	if got := (ProviderProfile{}).APIKey(); got != "" {
		t.Fatalf("empty api key = %q", got)
	}
}

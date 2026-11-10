package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	SchemaVersion = "1"
	DefaultPath   = ".yangjian/config.json"
)

type Config struct {
	SchemaVersion  string     `json:"schema_version"`
	DefaultChainID int64      `json:"default_chain_id"`
	CacheDir       string     `json:"cache_dir"`
	TargetRegistry string     `json:"target_registry"`
	Providers      Providers  `json:"providers"`
	Solc           SolcConfig `json:"solc"`
	Budgets        Budgets    `json:"budgets"`
}

type SolcConfig struct {
	BinaryPath     string `json:"binary_path,omitempty"`
	BaseURL        string `json:"base_url,omitempty"`
	Timeout        string `json:"timeout,omitempty"`
	MaxInputBytes  int64  `json:"max_input_bytes,omitempty"`
	MaxOutputBytes int64  `json:"max_output_bytes,omitempty"`
}

type Providers struct {
	RPC       map[string]ProviderProfile `json:"rpc"`
	Sourcify  ProviderProfile            `json:"sourcify"`
	Etherscan ProviderProfile            `json:"etherscan"`
}

type ProviderProfile struct {
	Endpoint     string  `json:"endpoint"`
	APIKeyEnv    string  `json:"api_key_env,omitempty"`
	Timeout      string  `json:"timeout,omitempty"`
	RateLimitRPS float64 `json:"rate_limit_rps,omitempty"`
	Burst        int     `json:"burst,omitempty"`
	MaxRetries   int     `json:"max_retries,omitempty"`
}

type Budgets struct {
	MaxDepth              int `json:"max_depth"`
	MaxContracts          int `json:"max_contracts"`
	MaxRequestsPerDossier int `json:"max_requests_per_dossier"`
}

func Default() Config {
	return Config{
		SchemaVersion:  SchemaVersion,
		DefaultChainID: 1,
		CacheDir:       ".yangjian/cache",
		TargetRegistry: "targets/mainnet.json",
		Providers: Providers{
			RPC: map[string]ProviderProfile{
				"default": {
					Endpoint:     "https://ethereum-rpc.publicnode.com",
					Timeout:      "15s",
					RateLimitRPS: 5,
					Burst:        5,
					MaxRetries:   3,
				},
			},
			Sourcify: ProviderProfile{
				Endpoint:     "https://sourcify.dev/server",
				Timeout:      "20s",
				RateLimitRPS: 5,
				Burst:        5,
				MaxRetries:   3,
			},
			Etherscan: ProviderProfile{
				Endpoint:     "https://api.etherscan.io/v2/api",
				APIKeyEnv:    "ETHERSCAN_API_KEY",
				Timeout:      "20s",
				RateLimitRPS: 3,
				Burst:        3,
				MaxRetries:   3,
			},
		},
		Solc: SolcConfig{
			BaseURL:        "https://binaries.soliditylang.org",
			Timeout:        "120s",
			MaxInputBytes:  32 << 20,
			MaxOutputBytes: 64 << 20,
		},
		Budgets: Budgets{
			MaxDepth:              2,
			MaxContracts:          50,
			MaxRequestsPerDossier: 1000,
		},
	}
}

func Load(path string) (Config, error) {
	explicit := path != ""
	if path == "" {
		path = os.Getenv("YANGJIAN_CONFIG")
		explicit = path != ""
	}
	if path == "" {
		path = DefaultPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			cfg := Default()
			if err := applyEnv(&cfg); err != nil {
				return Config{}, err
			}
			return cfg, cfg.Validate()
		}
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := Default()
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported config schema_version %q", c.SchemaVersion)
	}
	if c.DefaultChainID <= 0 {
		return fmt.Errorf("default_chain_id must be positive")
	}
	if c.CacheDir == "" {
		return fmt.Errorf("cache_dir must not be empty")
	}
	if c.TargetRegistry == "" {
		return fmt.Errorf("target_registry must not be empty")
	}
	profile := c.RPCProfile()
	if profile.Endpoint == "" {
		return fmt.Errorf("providers.rpc.default.endpoint must not be empty")
	}
	if profile.RateLimitRPS <= 0 {
		return fmt.Errorf("providers.rpc.default.rate_limit_rps must be positive")
	}
	if profile.Burst <= 0 {
		return fmt.Errorf("providers.rpc.default.burst must be positive")
	}
	if profile.MaxRetries < 0 {
		return fmt.Errorf("providers.rpc.default.max_retries must not be negative")
	}
	if _, err := profile.TimeoutDuration(); err != nil {
		return err
	}
	if c.Budgets.MaxDepth <= 0 || c.Budgets.MaxContracts <= 0 || c.Budgets.MaxRequestsPerDossier <= 0 {
		return fmt.Errorf("budgets must be positive")
	}
	if c.Solc.BinaryPath == "" && c.Solc.BaseURL == "" {
		return fmt.Errorf("solc.base_url must not be empty when solc.binary_path is not set")
	}
	if c.Solc.Timeout != "" {
		d, err := time.ParseDuration(c.Solc.Timeout)
		if err != nil {
			return fmt.Errorf("invalid solc.timeout %q: %w", c.Solc.Timeout, err)
		}
		if d <= 0 {
			return fmt.Errorf("solc.timeout must be positive")
		}
	}
	if c.Solc.MaxInputBytes < 0 || c.Solc.MaxOutputBytes < 0 {
		return fmt.Errorf("solc size limits must not be negative")
	}
	return nil
}

func (c Config) RPCProfile() ProviderProfile {
	if profile, ok := c.Providers.RPC["default"]; ok {
		return profile
	}
	return ProviderProfile{}
}

func (p ProviderProfile) TimeoutDuration() (time.Duration, error) {
	if p.Timeout == "" {
		return 15 * time.Second, nil
	}
	d, err := time.ParseDuration(p.Timeout)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout %q: %w", p.Timeout, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("timeout must be positive")
	}
	return d, nil
}

func (p ProviderProfile) APIKey() string {
	if p.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(p.APIKeyEnv)
}

func applyEnv(c *Config) error {
	if v := os.Getenv("YANGJIAN_RPC_URL"); v != "" {
		profile := c.Providers.RPC["default"]
		profile.Endpoint = v
		c.Providers.RPC["default"] = profile
	}
	if v := os.Getenv("YANGJIAN_DEFAULT_CHAIN_ID"); v != "" {
		chainID, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("YANGJIAN_DEFAULT_CHAIN_ID: %w", err)
		}
		c.DefaultChainID = chainID
	}
	if v := os.Getenv("YANGJIAN_CACHE_DIR"); v != "" {
		c.CacheDir = v
	}
	if v := os.Getenv("YANGJIAN_TARGET_REGISTRY"); v != "" {
		c.TargetRegistry = v
	}
	return nil
}

func DefaultConfigJSON() ([]byte, error) {
	cfg := Default()
	return json.MarshalIndent(cfg, "", "  ")
}

func WriteDefault(path string, force bool) error {
	if path == "" {
		path = DefaultPath
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("config already exists: %s", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := DefaultConfigJSON()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

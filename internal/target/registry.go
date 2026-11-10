package target

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type Registry struct {
	SchemaVersion string   `json:"schema_version"`
	Network       Network  `json:"network"`
	Targets       []Target `json:"targets"`
}

type Network struct {
	ChainID int64  `json:"chain_id"`
	Name    string `json:"name"`
}

type Target struct {
	Slug              string            `json:"slug"`
	Name              string            `json:"name"`
	ChainID           int64             `json:"chain_id"`
	SourceURL         string            `json:"source_url"`
	SourceRetrievedAt time.Time         `json:"source_retrieved_at"`
	SourceModifiedAt  *time.Time        `json:"source_modified_at,omitempty"`
	Contracts         map[string]string `json:"contracts"`
	Tokens            map[string]string `json:"tokens"`
	Acceptance        Acceptance        `json:"acceptance"`
	Notes             string            `json:"notes,omitempty"`
}

type Acceptance struct {
	PoolPair      []string `json:"pool_pair"`
	FeeTier       int      `json:"fee_tier"`
	PoolDiscovery string   `json:"pool_discovery"`
}

func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read target registry %s: %w", path, err)
	}
	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("parse target registry %s: %w", path, err)
	}
	if registry.SchemaVersion != "1" {
		return nil, fmt.Errorf("unsupported target registry schema_version %q", registry.SchemaVersion)
	}
	if registry.Network.ChainID <= 0 {
		return nil, fmt.Errorf("network.chain_id must be positive")
	}
	for i := range registry.Targets {
		target := &registry.Targets[i]
		if target.ChainID != registry.Network.ChainID {
			return nil, fmt.Errorf("target %q chain_id %d does not match network %d", target.Slug, target.ChainID, registry.Network.ChainID)
		}
		if err := target.Validate(); err != nil {
			return nil, fmt.Errorf("target %q: %w", target.Slug, err)
		}
	}
	return &registry, nil
}

func (r *Registry) Find(slug string) (*Target, error) {
	for i := range r.Targets {
		if r.Targets[i].Slug == slug {
			return &r.Targets[i], nil
		}
	}
	return nil, fmt.Errorf("target %q not found", slug)
}

func (t *Target) Validate() error {
	if t.Slug == "" {
		return fmt.Errorf("slug must not be empty")
	}
	if t.Name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if t.ChainID <= 0 {
		return fmt.Errorf("chain_id must be positive")
	}
	if t.SourceURL == "" {
		return fmt.Errorf("source_url must not be empty")
	}
	parsed, err := url.Parse(t.SourceURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("source_url must be an http(s) URL")
	}
	if t.SourceRetrievedAt.IsZero() {
		return fmt.Errorf("source_retrieved_at must not be zero")
	}
	if len(t.Contracts) == 0 {
		return fmt.Errorf("contracts must not be empty")
	}
	for name, address := range t.Contracts {
		if !common.IsHexAddress(address) {
			return fmt.Errorf("contract %q has invalid address %q", name, address)
		}
	}
	for name, address := range t.Tokens {
		if !common.IsHexAddress(address) {
			return fmt.Errorf("token %q has invalid address %q", name, address)
		}
	}
	if len(t.Acceptance.PoolPair) != 2 {
		return fmt.Errorf("acceptance.pool_pair must contain two tokens")
	}
	if t.Acceptance.FeeTier <= 0 {
		return fmt.Errorf("acceptance.fee_tier must be positive")
	}
	if t.Acceptance.PoolDiscovery == "" {
		return fmt.Errorf("acceptance.pool_discovery must not be empty")
	}
	return nil
}

func (t *Target) Address(contract string) (string, error) {
	address, ok := t.Contracts[contract]
	if !ok {
		return "", fmt.Errorf("contract %q not found in target %q", contract, t.Slug)
	}
	return common.HexToAddress(address).Hex(), nil
}

func (t *Target) Token(symbol string) (string, error) {
	address, ok := t.Tokens[symbol]
	if !ok {
		return "", fmt.Errorf("token %q not found in target %q", symbol, t.Slug)
	}
	return common.HexToAddress(address).Hex(), nil
}

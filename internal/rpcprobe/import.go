package rpcprobe

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ChainlistRPC is one untrusted RPC suggestion from chainlist.org/rpcs.json.
// The data is treated as a candidate list; importing never selects a default.
type ChainlistRPC struct {
	URL      string `json:"url"`
	Tracking string `json:"tracking,omitempty"`
}

type chainlistChain struct {
	ChainID int64  `json:"chainId"`
	Name    string `json:"name"`
	RPC     []struct {
		URL      string `json:"url"`
		Tracking string `json:"tracking"`
	} `json:"rpc"`
}

func ParseChainlist(data []byte, chainID int64) ([]ChainlistRPC, error) {
	var chains []chainlistChain
	if err := json.Unmarshal(data, &chains); err != nil {
		return nil, fmt.Errorf("parse chainlist data: %w", err)
	}
	var selected *chainlistChain
	for i := range chains {
		if chains[i].ChainID == chainID {
			selected = &chains[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("chain %d is not present in chainlist data", chainID)
	}
	seen := map[string]bool{}
	out := make([]ChainlistRPC, 0, len(selected.RPC))
	for _, item := range selected.RPC {
		raw := strings.TrimSpace(item.URL)
		if raw == "" || seen[raw] {
			continue
		}
		if TransportOf(raw) == "" {
			continue
		}
		seen[raw] = true
		out = append(out, ChainlistRPC{URL: raw, Tracking: strings.TrimSpace(item.Tracking)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("chain %d has no supported HTTP or WebSocket endpoints", chainID)
	}
	return out, nil
}

func EndpointFromChainlist(chainID int64, item ChainlistRPC) Endpoint {
	sourceURL := fmt.Sprintf("https://chainlist.org/chain/%d", chainID)
	ep := Endpoint{
		URL:       item.URL,
		Transport: TransportOf(item.URL),
		Enabled:   true,
		Source:    "chainlist",
		SourceURL: sourceURL,
		Tracking:  item.Tracking,
		Health:    Health{Status: StatusUnprobed},
	}
	name := NameFromURL(item.URL)
	if ep.Transport == "ws" {
		name += "-ws"
	}
	if _, missing := EnvRefs(item.URL); len(missing) > 0 {
		ep.Enabled = false
		ep.RequiresEnv = strings.Join(missing, ", ")
		ep.Notes = "disabled until the referenced environment variable is set"
		ep.Health.Status = StatusUnconfigured
		name += "-keyed"
	}
	ep.Name = name
	ep.CredentialInURL = credentialInURL(item.URL)
	return ep
}

func credentialInURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	for key := range u.Query() {
		switch strings.ToLower(key) {
		case "apikey", "api_key", "api-key", "key", "token", "access_token":
			return true
		}
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, segment := range segments {
		if i == 0 {
			continue
		}
		if len(segment) >= 16 && validPathLabel(segment) && looksLikeToken(segment) {
			return true
		}
	}
	return false
}

// RedactURL hides credentials before a URL is printed in text output. The
// registry itself must keep the full URL so the endpoint can be used.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := u.Query()
	for key := range query {
		switch strings.ToLower(key) {
		case "apikey", "api_key", "api-key", "key", "token", "access_token":
			query.Set(key, "REDACTED")
		}
	}
	u.RawQuery = query.Encode()
	segments := strings.Split(u.Path, "/")
	for i, segment := range segments {
		if len(segment) >= 16 && validPathLabel(segment) && looksLikeToken(segment) {
			segments[i] = "REDACTED"
		}
	}
	u.Path = strings.Join(segments, "/")
	return u.String()
}

package rpcprobe

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion = "1"
	DefaultPath   = ".yangjian/rpc-endpoints.json"

	DefaultChainID      int64  = 1
	DefaultArchiveBlock uint64 = 12_965_000
	DefaultLogsWindow   uint64 = 2

	DefaultTimeout            = 8 * time.Second
	DefaultBudget             = 45 * time.Second
	DefaultConcurrency        = 4
	DefaultMaxHeadLag  uint64 = 30
	HistoryLimit              = 10
)

// Health status values. A capability gap is never a health failure: an
// endpoint can be healthy while it does not expose logs or trace methods.
const (
	StatusUnprobed     = "unprobed"
	StatusOK           = "ok"
	StatusDegraded     = "degraded"
	StatusStale        = "stale"
	StatusRateLimited  = "rate_limited"
	StatusUnreachable  = "unreachable"
	StatusWrongChain   = "wrong_chain"
	StatusAuthRequired = "auth_required"
	StatusUnconfigured = "unconfigured"
)

var statusOrder = map[string]int{
	StatusOK:           0,
	StatusDegraded:     1,
	StatusStale:        2,
	StatusRateLimited:  3,
	StatusUnreachable:  4,
	StatusAuthRequired: 5,
	StatusWrongChain:   6,
	StatusUnconfigured: 7,
	StatusUnprobed:     8,
}

type Registry struct {
	SchemaVersion string     `json:"schema_version"`
	ChainID       int64      `json:"chain_id"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Probe         ProbeSetup `json:"probe"`
	Endpoints     []Endpoint `json:"endpoints"`
}

type ProbeSetup struct {
	ArchiveBlock     uint64 `json:"archive_block"`
	LogsWindowBlocks uint64 `json:"logs_window_blocks"`
}

type Endpoint struct {
	Name            string       `json:"name"`
	URL             string       `json:"url"`
	Transport       string       `json:"transport"`
	Enabled         bool         `json:"enabled"`
	Source          string       `json:"source,omitempty"`
	SourceURL       string       `json:"source_url,omitempty"`
	Tracking        string       `json:"tracking,omitempty"`
	Tags            []string     `json:"tags,omitempty"`
	Notes           string       `json:"notes,omitempty"`
	RequiresEnv     string       `json:"requires_env,omitempty"`
	CredentialInURL bool         `json:"credential_in_url,omitempty"`
	ClientVersion   string       `json:"client_version,omitempty"`
	Capabilities    Capabilities `json:"capabilities"`
	Health          Health       `json:"health"`
	History         []Snapshot   `json:"history,omitempty"`
}

type Capabilities struct {
	Blocks      bool `json:"blocks"`
	Logs        bool `json:"logs"`
	Call        bool `json:"call"`
	Code        bool `json:"code"`
	Storage     bool `json:"storage"`
	Proof       bool `json:"proof"`
	FeeHistory  bool `json:"fee_history"`
	Archive     bool `json:"archive"`
	DebugTrace  bool `json:"debug_trace"`
	ParityTrace bool `json:"parity_trace"`
	Batch       bool `json:"batch"`
	WebSocket   bool `json:"websocket"`
}

type Health struct {
	Status              string            `json:"status"`
	CheckedAt           time.Time         `json:"checked_at,omitempty"`
	ChainID             int64             `json:"chain_id,omitempty"`
	HeadBlock           uint64            `json:"head_block,omitempty"`
	HeadHash            string            `json:"head_hash,omitempty"`
	HeadTimestamp       time.Time         `json:"head_timestamp,omitempty"`
	BlockAgeSeconds     int64             `json:"block_age_seconds,omitempty"`
	HeadLagBlocks       uint64            `json:"head_lag_blocks,omitempty"`
	LatencyMS           int64             `json:"latency_ms,omitempty"`
	DurationMS          int64             `json:"duration_ms,omitempty"`
	ConsecutiveFailures int               `json:"consecutive_failures,omitempty"`
	LastOKAt            *time.Time        `json:"last_ok_at,omitempty"`
	TraceBlock          uint64            `json:"trace_block,omitempty"`
	TraceTx             string            `json:"trace_tx,omitempty"`
	Errors              map[string]string `json:"probe_errors,omitempty"`
	Message             string            `json:"message,omitempty"`
}

type Snapshot struct {
	CheckedAt time.Time `json:"checked_at"`
	Status    string    `json:"status"`
	HeadBlock uint64    `json:"head_block,omitempty"`
	LatencyMS int64     `json:"latency_ms,omitempty"`
	Message   string    `json:"message,omitempty"`
}

type Summary struct {
	Total          int `json:"total"`
	Enabled        int `json:"enabled"`
	OK             int `json:"ok"`
	Degraded       int `json:"degraded"`
	Stale          int `json:"stale"`
	RateLimited    int `json:"rate_limited"`
	Unreachable    int `json:"unreachable"`
	AuthRequired   int `json:"auth_required"`
	WrongChain     int `json:"wrong_chain"`
	Unconfigured   int `json:"unconfigured"`
	Unprobed       int `json:"unprobed"`
	WithLogs       int `json:"with_logs"`
	WithArchive    int `json:"with_archive"`
	WithDebugTrace int `json:"with_debug_trace"`
	WithTrace      int `json:"with_parity_trace"`
}

func New(chainID int64) *Registry {
	return &Registry{
		SchemaVersion: SchemaVersion,
		ChainID:       chainID,
		Probe: ProbeSetup{
			ArchiveBlock:     DefaultArchiveBlock,
			LogsWindowBlocks: DefaultLogsWindow,
		},
		Endpoints: []Endpoint{},
	}
}

func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rpc registry %s: %w", path, err)
	}
	reg := &Registry{}
	if err := json.Unmarshal(data, reg); err != nil {
		return nil, fmt.Errorf("parse rpc registry %s: %w", path, err)
	}
	if err := reg.Normalize(); err != nil {
		return nil, fmt.Errorf("validate rpc registry %s: %w", path, err)
	}
	return reg, nil
}

func LoadOrNew(path string, chainID int64) (*Registry, error) {
	reg, err := Load(path)
	if err == nil {
		return reg, nil
	}
	if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") {
		return New(chainID), nil
	}
	return nil, err
}

func (r *Registry) Normalize() error {
	if r.SchemaVersion == "" {
		r.SchemaVersion = SchemaVersion
	}
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %q", r.SchemaVersion)
	}
	if r.ChainID <= 0 {
		return fmt.Errorf("chain_id must be positive")
	}
	if r.Probe.ArchiveBlock == 0 {
		r.Probe.ArchiveBlock = DefaultArchiveBlock
	}
	if r.Probe.LogsWindowBlocks == 0 {
		r.Probe.LogsWindowBlocks = DefaultLogsWindow
	}
	names := map[string]string{}
	urls := map[string]string{}
	for i := range r.Endpoints {
		ep := &r.Endpoints[i]
		ep.URL = strings.TrimSpace(ep.URL)
		ep.Name = strings.TrimSpace(ep.Name)
		if ep.URL == "" {
			return fmt.Errorf("endpoint %d has an empty url", i)
		}
		if ep.Transport == "" {
			ep.Transport = TransportOf(ep.URL)
		}
		if ep.Transport != "http" && ep.Transport != "ws" {
			return fmt.Errorf("endpoint %q has unsupported transport %q", ep.Name, ep.Transport)
		}
		if ep.Name == "" {
			ep.Name = NameFromURL(ep.URL)
		}
		key := strings.ToLower(ep.Name)
		if other, ok := names[key]; ok {
			return fmt.Errorf("duplicate endpoint name %q and %q", other, ep.Name)
		}
		names[key] = ep.Name
		if other, ok := urls[ep.URL]; ok {
			return fmt.Errorf("duplicate endpoint url for %q and %q", other, ep.Name)
		}
		urls[ep.URL] = ep.Name
		if len(ep.History) > HistoryLimit {
			ep.History = ep.History[len(ep.History)-HistoryLimit:]
		}
	}
	return nil
}

func Save(path string, reg *Registry) error {
	if err := reg.Normalize(); err != nil {
		return err
	}
	reg.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create registry directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".rpc-endpoints-*.tmp")
	if err != nil {
		return fmt.Errorf("create registry temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod registry temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write registry temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync registry temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close registry temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace registry: %w", err)
	}
	return nil
}

func (r *Registry) Find(nameOrURL string) (*Endpoint, bool) {
	for i := range r.Endpoints {
		if r.Endpoints[i].Name == nameOrURL || r.Endpoints[i].URL == nameOrURL {
			return &r.Endpoints[i], true
		}
	}
	for i := range r.Endpoints {
		if strings.EqualFold(r.Endpoints[i].Name, nameOrURL) {
			return &r.Endpoints[i], true
		}
	}
	return nil, false
}

func (r *Registry) UniqueName(base string) string {
	base = sanitizeName(base)
	if base == "" {
		base = "endpoint"
	}
	used := map[string]bool{}
	for _, ep := range r.Endpoints {
		used[strings.ToLower(ep.Name)] = true
	}
	if !used[strings.ToLower(base)] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !used[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

// Ensure adds an imported endpoint only when its URL is not already present.
// Existing probe data and user notes are never overwritten by an import.
func (r *Registry) Ensure(ep Endpoint) (Endpoint, bool) {
	if existing, ok := r.findByURL(ep.URL); ok {
		return *existing, false
	}
	if ep.Name == "" {
		ep.Name = NameFromURL(ep.URL)
	}
	ep.Name = r.UniqueName(ep.Name)
	r.Endpoints = append(r.Endpoints, ep)
	return ep, true
}

// AddProbed registers or updates an endpoint that was just probed. User-owned
// fields (notes, tags, source) are preserved when the caller leaves them empty.
func (r *Registry) AddProbed(ep Endpoint) (Endpoint, bool) {
	if existing, ok := r.findByURL(ep.URL); ok {
		existing.Name = choose(existing.Name, ep.Name)
		existing.Transport = ep.Transport
		existing.ClientVersion = ep.ClientVersion
		existing.Capabilities = ep.Capabilities
		existing.Health = carryHealth(existing.Health, ep.Health)
		existing.History = appendHistory(existing.History, existing.Health)
		existing.Enabled = ep.Enabled
		existing.Source = choose(existing.Source, ep.Source)
		existing.SourceURL = choose(existing.SourceURL, ep.SourceURL)
		existing.Tracking = choose(existing.Tracking, ep.Tracking)
		existing.RequiresEnv = choose(existing.RequiresEnv, ep.RequiresEnv)
		if len(ep.Tags) > 0 {
			existing.Tags = ep.Tags
		}
		if ep.Notes != "" {
			existing.Notes = ep.Notes
		}
		return *existing, false
	}
	if ep.Name == "" {
		ep.Name = NameFromURL(ep.URL)
	}
	ep.Name = r.UniqueName(ep.Name)
	ep.Health = carryHealth(Health{}, ep.Health)
	ep.History = appendHistory(nil, ep.Health)
	r.Endpoints = append(r.Endpoints, ep)
	return ep, true
}

// ApplyResult merges one health check into the registry. maxHead is the best
// head observed in the same check and is used to report lag without pretending
// that a lagging endpoint is unreachable.
func (r *Registry) ApplyResult(nameOrURL string, res Result, maxHead, maxLag uint64) (*Endpoint, bool) {
	ep, ok := r.Find(nameOrURL)
	if !ok {
		return nil, false
	}
	ep.Transport = res.Transport
	if res.ClientVersion != "" {
		ep.ClientVersion = res.ClientVersion
	}
	ep.Capabilities = res.Capabilities
	health := res.Health
	if maxHead > 0 && health.HeadBlock > 0 && maxHead > health.HeadBlock {
		health.HeadLagBlocks = maxHead - health.HeadBlock
	}
	if health.HeadLagBlocks > 0 && maxLag > 0 && health.HeadLagBlocks > maxLag && health.Status == StatusOK {
		health.Status = StatusStale
		health.Message = fmt.Sprintf("head lags %d blocks behind the best observed endpoint", health.HeadLagBlocks)
	}
	health = carryHealth(ep.Health, health)
	ep.Health = health
	ep.History = appendHistory(ep.History, health)
	return ep, true
}

func carryHealth(previous, next Health) Health {
	if next.Status == StatusOK {
		now := next.CheckedAt
		next.LastOKAt = &now
		next.ConsecutiveFailures = 0
		return next
	}
	next.ConsecutiveFailures = previous.ConsecutiveFailures + 1
	next.LastOKAt = previous.LastOKAt
	return next
}

func appendHistory(history []Snapshot, health Health) []Snapshot {
	if health.CheckedAt.IsZero() {
		return history
	}
	history = append(history, Snapshot{
		CheckedAt: health.CheckedAt,
		Status:    health.Status,
		HeadBlock: health.HeadBlock,
		LatencyMS: health.LatencyMS,
		Message:   health.Message,
	})
	if len(history) > HistoryLimit {
		history = history[len(history)-HistoryLimit:]
	}
	return history
}

func (r Registry) Summary() Summary {
	s := Summary{Total: len(r.Endpoints)}
	for _, ep := range r.Endpoints {
		if ep.Enabled {
			s.Enabled++
		}
		switch ep.Health.Status {
		case StatusOK:
			s.OK++
		case StatusDegraded:
			s.Degraded++
		case StatusStale:
			s.Stale++
		case StatusRateLimited:
			s.RateLimited++
		case StatusUnreachable:
			s.Unreachable++
		case StatusAuthRequired:
			s.AuthRequired++
		case StatusWrongChain:
			s.WrongChain++
		case StatusUnconfigured:
			s.Unconfigured++
		default:
			s.Unprobed++
		}
		if ep.Capabilities.Logs {
			s.WithLogs++
		}
		if ep.Capabilities.Archive {
			s.WithArchive++
		}
		if ep.Capabilities.DebugTrace {
			s.WithDebugTrace++
		}
		if ep.Capabilities.ParityTrace {
			s.WithTrace++
		}
	}
	return s
}

func (r *Registry) SortByName() {
	sort.SliceStable(r.Endpoints, func(i, j int) bool {
		return strings.ToLower(r.Endpoints[i].Name) < strings.ToLower(r.Endpoints[j].Name)
	})
}

func (r *Registry) findByURL(raw string) (*Endpoint, bool) {
	for i := range r.Endpoints {
		if r.Endpoints[i].URL == raw {
			return &r.Endpoints[i], true
		}
	}
	return nil, false
}

func TransportOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return "http"
	case "ws", "wss":
		return "ws"
	default:
		return ""
	}
}

func NameFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return "endpoint"
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	base := sanitizeName(host)
	if base == "" {
		base = "endpoint"
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	for _, segment := range segments {
		segment = strings.ToLower(segment)
		if len(segment) == 0 || len(segment) > 16 {
			continue
		}
		if !validPathLabel(segment) || looksLikeToken(segment) {
			continue
		}
		switch segment {
		case "v1", "v2", "v3", "rpc", "public", "eth", "api", "mainnet", "ethereum", "chain", "ws":
			continue
		}
		base += "-" + segment
		break
	}
	return base
}

func looksLikeToken(segment string) bool {
	if len(segment) < 12 {
		return false
	}
	hasDigit, hasAlpha := false, false
	for _, r := range segment {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r >= 'a' && r <= 'z':
			hasAlpha = true
		}
	}
	return hasDigit && hasAlpha
}

func sanitizeName(raw string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func validPathLabel(segment string) bool {
	for _, r := range segment {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

// EnvRefs returns the ${VAR} names referenced by a URL and the expanded URL.
func EnvRefs(raw string) (string, []string) {
	var missing []string
	expanded := os.Expand(raw, func(key string) string {
		value, ok := os.LookupEnv(key)
		if !ok {
			missing = append(missing, key)
			return "${" + key + "}"
		}
		return value
	})
	return expanded, missing
}

func choose(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func CapabilityNames(c Capabilities) []string {
	var names []string
	if c.Blocks {
		names = append(names, "blocks")
	}
	if c.Logs {
		names = append(names, "logs")
	}
	if c.Call {
		names = append(names, "call")
	}
	if c.Code {
		names = append(names, "code")
	}
	if c.Storage {
		names = append(names, "storage")
	}
	if c.Proof {
		names = append(names, "proof")
	}
	if c.FeeHistory {
		names = append(names, "fee_history")
	}
	if c.Archive {
		names = append(names, "archive")
	}
	if c.DebugTrace {
		names = append(names, "debug_trace")
	}
	if c.ParityTrace {
		names = append(names, "parity_trace")
	}
	if c.Batch {
		names = append(names, "batch")
	}
	if c.WebSocket {
		names = append(names, "websocket")
	}
	return names
}

func StatusRank(status string) int {
	if rank, ok := statusOrder[status]; ok {
		return rank
	}
	return 99
}

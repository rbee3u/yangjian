package solc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/rbee3u/yangjian/internal/config"
	"golang.org/x/crypto/sha3"
)

var (
	ErrCompilerNotFound  = errors.New("solc: compiler build not found")
	ErrHashMismatch      = errors.New("solc: binary hash mismatch")
	ErrInputTooLarge     = errors.New("solc: standard JSON input too large")
	ErrOutputTooLarge    = errors.New("solc: standard JSON output too large")
	ErrCompileTimeout    = errors.New("solc: compilation timed out")
	ErrCompilationFailed = errors.New("solc: compilation failed")
)

const (
	maxListBytes   = 8 << 20
	maxBinaryBytes = 256 << 20
	maxStderrBytes = 1 << 20
)

type Manager struct {
	binaryPath string
	cacheDir   string
	baseURL    string
	timeout    time.Duration
	maxInput   int64
	maxOutput  int64
	httpClient *http.Client
}

type Binary struct {
	Path       string `json:"path"`
	Version    string `json:"version"`
	SHA256     string `json:"sha256"`
	Keccak256  string `json:"keccak256"`
	SourceURL  string `json:"source_url"`
	ListURL    string `json:"list_url,omitempty"`
	ListSHA256 string `json:"list_sha256,omitempty"`
}

type Reference struct {
	Start  int `json:"start"`
	Length int `json:"length"`
}

type Contract struct {
	ABI json.RawMessage `json:"abi"`
	EVM struct {
		DeployedBytecode struct {
			Object              string                            `json:"object"`
			ImmutableReferences map[string][]Reference            `json:"immutableReferences"`
			LinkReferences      map[string]map[string][]Reference `json:"linkReferences"`
		} `json:"deployedBytecode"`
	} `json:"evm"`
}

type Diagnostic struct {
	Severity         string `json:"severity"`
	Type             string `json:"type"`
	Component        string `json:"component"`
	Message          string `json:"message"`
	FormattedMessage string `json:"formattedMessage"`
}

type Output struct {
	Raw       []byte
	Stderr    string
	Errors    []Diagnostic
	Contracts map[string]map[string]Contract
}

func NewManager(cfg config.SolcConfig, cacheDir string) (*Manager, error) {
	if cacheDir == "" {
		userCache, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("resolve cache dir: %w", err)
		}
		cacheDir = filepath.Join(userCache, "yangjian")
	}
	timeout := 120 * time.Second
	if cfg.Timeout != "" {
		parsed, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid solc timeout %q: %w", cfg.Timeout, err)
		}
		if parsed <= 0 {
			return nil, fmt.Errorf("solc timeout must be positive")
		}
		timeout = parsed
	}
	maxInput := cfg.MaxInputBytes
	if maxInput <= 0 {
		maxInput = 32 << 20
	}
	maxOutput := cfg.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = 64 << 20
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://binaries.soliditylang.org"
	}
	return &Manager{
		binaryPath: cfg.BinaryPath,
		cacheDir:   cacheDir,
		baseURL:    baseURL,
		timeout:    timeout,
		maxInput:   maxInput,
		maxOutput:  maxOutput,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func HostPlatform() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/amd64", "darwin/arm64":
		return "macosx-amd64", nil
	case "linux/amd64":
		return "linux-amd64", nil
	case "linux/arm64":
		return "linux-arm64", nil
	case "windows/amd64":
		return "windows-amd64", nil
	default:
		return "", fmt.Errorf("unsupported solc host platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

type listFile struct {
	Builds []listBuild `json:"builds"`
}

type listBuild struct {
	Path        string `json:"path"`
	Version     string `json:"version"`
	LongVersion string `json:"longVersion"`
	Keccak256   string `json:"keccak256"`
	SHA256      string `json:"sha256"`
}

func (m *Manager) Ensure(ctx context.Context, longVersion string) (Binary, error) {
	if longVersion == "" {
		return Binary{}, fmt.Errorf("compiler version is required")
	}
	if m.binaryPath != "" {
		return m.resolveLocal(ctx, longVersion)
	}
	platform, err := HostPlatform()
	if err != nil {
		return Binary{}, err
	}
	listURL := fmt.Sprintf("%s/%s/list.json", m.baseURL, platform)
	listRaw, err := m.fetch(ctx, listURL, maxListBytes)
	if err != nil {
		return Binary{}, fmt.Errorf("fetch solc-bin list: %w", err)
	}
	var list listFile
	if err := json.Unmarshal(listRaw, &list); err != nil {
		return Binary{}, fmt.Errorf("parse solc-bin list: %w", err)
	}
	build, ok := findBuild(list, longVersion)
	if !ok {
		return Binary{}, fmt.Errorf("%w: %s on %s", ErrCompilerNotFound, longVersion, platform)
	}
	filename := filepath.Base(build.Path)
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		return Binary{}, fmt.Errorf("invalid solc-bin filename %q", build.Path)
	}
	dir := filepath.Join(m.cacheDir, "solc", platform)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Binary{}, fmt.Errorf("create solc cache: %w", err)
	}
	path := filepath.Join(dir, filename)
	if ok, err := verifyFile(path, build.SHA256, build.Keccak256); err == nil && ok {
		return Binary{
			Path:       path,
			Version:    build.LongVersion,
			SHA256:     withPrefix("sha256", build.SHA256),
			Keccak256:  withPrefix("keccak256", build.Keccak256),
			SourceURL:  fmt.Sprintf("%s/%s/%s", m.baseURL, platform, filename),
			ListURL:    listURL,
			ListSHA256: modelHash(listRaw),
		}, nil
	}
	binaryURL := fmt.Sprintf("%s/%s/%s", m.baseURL, platform, filename)
	raw, err := m.fetch(ctx, binaryURL, maxBinaryBytes)
	if err != nil {
		return Binary{}, fmt.Errorf("download solc %s: %w", longVersion, err)
	}
	if err := checkHashes(raw, build.SHA256, build.Keccak256); err != nil {
		return Binary{}, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o755); err != nil {
		return Binary{}, fmt.Errorf("write solc: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return Binary{}, fmt.Errorf("install solc: %w", err)
	}
	return Binary{
		Path:       path,
		Version:    build.LongVersion,
		SHA256:     withPrefix("sha256", build.SHA256),
		Keccak256:  withPrefix("keccak256", build.Keccak256),
		SourceURL:  binaryURL,
		ListURL:    listURL,
		ListSHA256: modelHash(listRaw),
	}, nil
}

func (m *Manager) resolveLocal(ctx context.Context, longVersion string) (Binary, error) {
	info, err := os.Stat(m.binaryPath)
	if err != nil {
		return Binary{}, fmt.Errorf("stat solc binary: %w", err)
	}
	if info.IsDir() {
		return Binary{}, fmt.Errorf("solc binary path is a directory: %s", m.binaryPath)
	}
	versionCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(versionCtx, m.binaryPath, "--version")
	cmd.Env = minimalEnv(filepath.Dir(m.binaryPath))
	out, err := cmd.Output()
	if err != nil {
		return Binary{}, fmt.Errorf("run solc --version: %w", err)
	}
	if !strings.Contains(string(out), longVersion) {
		return Binary{}, fmt.Errorf("solc at %s does not report version %s: %s", m.binaryPath, longVersion, truncate(string(out), 300))
	}
	sha, keccak, err := hashFile(m.binaryPath)
	if err != nil {
		return Binary{}, err
	}
	return Binary{
		Path:      m.binaryPath,
		Version:   longVersion,
		SHA256:    sha,
		Keccak256: keccak,
		SourceURL: "local",
	}, nil
}

func (m *Manager) Compile(ctx context.Context, binary Binary, input []byte) (Output, error) {
	if len(input) == 0 {
		return Output{}, fmt.Errorf("solc input is empty")
	}
	if int64(len(input)) > m.maxInput {
		return Output{}, fmt.Errorf("%w: %d bytes exceeds %d", ErrInputTooLarge, len(input), m.maxInput)
	}
	baseTmp, err := filepath.Abs(filepath.Join(m.cacheDir, "tmp"))
	if err != nil {
		return Output{}, fmt.Errorf("resolve solc temp root: %w", err)
	}
	if err := os.MkdirAll(baseTmp, 0o700); err != nil {
		return Output{}, fmt.Errorf("create solc temp root: %w", err)
	}
	dir, err := os.MkdirTemp(baseTmp, "solc-")
	if err != nil {
		return Output{}, fmt.Errorf("create solc temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "input.json"), input, 0o600); err != nil {
		return Output{}, fmt.Errorf("write solc input: %w", err)
	}

	binaryPath, err := filepath.Abs(binary.Path)
	if err != nil {
		return Output{}, fmt.Errorf("resolve solc binary path: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, binaryPath,
		"--standard-json",
		"--base-path", dir,
		"--allow-paths", dir,
	)
	cmd.Dir = dir
	cmd.Env = minimalEnv(dir)
	cmd.Stdin = bytes.NewReader(input)
	stdout := newLimitedBuffer(m.maxOutput)
	stderr := newLimitedBuffer(maxStderrBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return Output{}, fmt.Errorf("%w after %s: %s", ErrCompileTimeout, m.timeout, truncate(stderr.String(), 300))
	}
	if stdout.Overflow() {
		return Output{}, fmt.Errorf("%w: exceeds %d bytes; stderr: %s", ErrOutputTooLarge, m.maxOutput, truncate(stderr.String(), 300))
	}
	out, parseErr := ParseOutput(stdout.Bytes())
	out.Stderr = stderr.String()
	if parseErr != nil {
		if runErr != nil {
			return out, fmt.Errorf("solc failed: %w: %s", runErr, truncate(stderr.String(), 500))
		}
		return out, parseErr
	}
	if out.HasErrors() {
		return out, fmt.Errorf("%w: %s", ErrCompilationFailed, out.ErrorSummary())
	}
	if runErr != nil {
		return out, fmt.Errorf("solc exited with error: %w: %s", runErr, truncate(stderr.String(), 500))
	}
	return out, nil
}

func ParseOutput(raw []byte) (Output, error) {
	var parsed struct {
		Errors    []Diagnostic                   `json:"errors"`
		Contracts map[string]map[string]Contract `json:"contracts"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return Output{}, fmt.Errorf("parse solc output: %w", err)
	}
	return Output{
		Raw:       raw,
		Errors:    parsed.Errors,
		Contracts: parsed.Contracts,
	}, nil
}

func (o Output) HasErrors() bool {
	for _, diagnostic := range o.Errors {
		if strings.EqualFold(diagnostic.Severity, "error") {
			return true
		}
	}
	return false
}

func (o Output) ErrorSummary() string {
	var parts []string
	for _, diagnostic := range o.Errors {
		if !strings.EqualFold(diagnostic.Severity, "error") {
			continue
		}
		message := diagnostic.FormattedMessage
		if message == "" {
			message = diagnostic.Message
		}
		parts = append(parts, truncate(strings.TrimSpace(message), 500))
		if len(parts) >= 3 {
			break
		}
	}
	if len(parts) == 0 {
		return "unknown compiler error"
	}
	return strings.Join(parts, "; ")
}

func (o Output) FindContract(sourcePath, name string) (Contract, string, bool) {
	trimmed := strings.TrimPrefix(sourcePath, "./")
	candidates := []string{sourcePath, trimmed, "./" + trimmed}
	for _, path := range candidates {
		if contracts, ok := o.Contracts[path]; ok {
			if contract, ok := contracts[name]; ok {
				return contract, path, true
			}
		}
	}
	base := filepath.Base(trimmed)
	paths := make([]string, 0, len(o.Contracts))
	for path := range o.Contracts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if filepath.Base(strings.TrimPrefix(path, "./")) != base {
			continue
		}
		if contract, ok := o.Contracts[path][name]; ok {
			return contract, path, true
		}
	}
	return Contract{}, "", false
}

func (c Contract) Deployed() ([]byte, error) {
	object := strings.TrimPrefix(strings.TrimSpace(c.EVM.DeployedBytecode.Object), "0x")
	if object == "" {
		return nil, fmt.Errorf("contract has no deployed bytecode")
	}
	code, err := hex.DecodeString(object)
	if err != nil {
		return nil, fmt.Errorf("decode deployed bytecode: %w", err)
	}
	return code, nil
}

func findBuild(list listFile, longVersion string) (listBuild, bool) {
	for _, build := range list.Builds {
		if build.LongVersion == longVersion {
			return build, true
		}
	}
	for _, build := range list.Builds {
		if build.Version == longVersion {
			return build, true
		}
	}
	return listBuild{}, false
}

func (m *Manager) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return raw, nil
}

func verifyFile(path, wantSHA, wantKeccak string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("not a regular file")
	}
	if info.Size() > maxBinaryBytes {
		return false, fmt.Errorf("file exceeds %d bytes", maxBinaryBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if err := checkHashes(raw, wantSHA, wantKeccak); err != nil {
		return false, nil
	}
	return true, nil
}

func checkHashes(raw []byte, wantSHA, wantKeccak string) error {
	actualSHA := sha256.Sum256(raw)
	actualKeccak := sha3.NewLegacyKeccak256()
	_, _ = actualKeccak.Write(raw)
	if wantSHA != "" && !equalHex(actualSHA[:], wantSHA) {
		return fmt.Errorf("%w: sha256 %s, want %s", ErrHashMismatch, hex.EncodeToString(actualSHA[:]), normalizeHex(wantSHA))
	}
	if wantKeccak != "" && !equalHex(actualKeccak.Sum(nil), wantKeccak) {
		return fmt.Errorf("%w: keccak256 %s, want %s", ErrHashMismatch, hex.EncodeToString(actualKeccak.Sum(nil)), normalizeHex(wantKeccak))
	}
	return nil
}

func hashFile(path string) (string, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	sha := sha256.Sum256(raw)
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(raw)
	return withPrefix("sha256", hex.EncodeToString(sha[:])), withPrefix("keccak256", hex.EncodeToString(hasher.Sum(nil))), nil
}

func equalHex(actual []byte, want string) bool {
	want = normalizeHex(want)
	return want != "" && strings.EqualFold(hex.EncodeToString(actual), want)
}

func normalizeHex(value string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "0x"))
}

func withPrefix(prefix, value string) string {
	value = normalizeHex(value)
	if value == "" {
		return ""
	}
	return prefix + ":" + value
}

func modelHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func minimalEnv(home string) []string {
	return []string{
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=" + home,
		"TMPDIR=" + home,
		"LANG=C",
		"LC_ALL=C",
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

type limitedBuffer struct {
	buf      bytes.Buffer
	limit    int64
	overflow bool
}

func newLimitedBuffer(limit int64) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.overflow {
		return len(p), nil
	}
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.overflow = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.overflow = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}

func (b *limitedBuffer) Overflow() bool {
	return b.overflow
}

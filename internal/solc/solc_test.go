package solc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/rbee3u/yangjian/internal/config"
	"golang.org/x/crypto/sha3"
)

func newTestManager(t *testing.T, timeout string) *Manager {
	t.Helper()
	mgr, err := NewManager(config.SolcConfig{
		Timeout:        timeout,
		MaxInputBytes:  1 << 20,
		MaxOutputBytes: 1 << 20,
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return mgr
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "solc")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnsureDownloadsAndReuses(t *testing.T) {
	binary := []byte("#!/bin/sh\nexit 0\n")
	sha := sha256.Sum256(binary)
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write(binary)
	platform, err := HostPlatform()
	if err != nil {
		t.Fatal(err)
	}

	var listCalls atomic.Int32
	var binaryCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + platform + "/list.json":
			listCalls.Add(1)
			fmt.Fprintf(w, `{"builds":[{"path":"solc-test","version":"0.7.6","longVersion":"0.7.6+commit.7338295f","sha256":"0x%s","keccak256":"0x%s"}]}`,
				hex.EncodeToString(sha[:]), hex.EncodeToString(hasher.Sum(nil)))
		case "/" + platform + "/solc-test":
			binaryCalls.Add(1)
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mgr, err := NewManager(config.SolcConfig{
		BaseURL:        server.URL,
		Timeout:        "5s",
		MaxInputBytes:  1 << 20,
		MaxOutputBytes: 1 << 20,
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := mgr.Ensure(context.Background(), "0.7.6+commit.7338295f")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if first.Path == "" || first.SHA256 == "" || first.Keccak256 == "" {
		t.Fatalf("binary = %+v", first)
	}
	data, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(binary) {
		t.Fatal("downloaded binary differs")
	}
	second, err := mgr.Ensure(context.Background(), "0.7.6+commit.7338295f")
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if second.Path != first.Path || second.SHA256 != first.SHA256 {
		t.Fatalf("second binary = %+v", second)
	}
	if binaryCalls.Load() != 1 {
		t.Fatalf("binary downloads = %d", binaryCalls.Load())
	}
	if listCalls.Load() != 2 {
		t.Fatalf("list calls = %d", listCalls.Load())
	}
}

func TestEnsureRejectsHashMismatch(t *testing.T) {
	platform, err := HostPlatform()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + platform + "/list.json":
			fmt.Fprintf(w, `{"builds":[{"path":"solc-test","version":"0.7.6","longVersion":"0.7.6+commit.7338295f","sha256":"0x%s","keccak256":""}]}`, "00")
		case "/" + platform + "/solc-test":
			_, _ = w.Write([]byte("binary"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mgr, err := NewManager(config.SolcConfig{
		BaseURL:        server.URL,
		Timeout:        "5s",
		MaxInputBytes:  1 << 20,
		MaxOutputBytes: 1 << 20,
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Ensure(context.Background(), "0.7.6+commit.7338295f")
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("error = %v", err)
	}
}

func TestEnsureMissingVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"builds":[]}`)
	}))
	defer server.Close()
	mgr, err := NewManager(config.SolcConfig{
		BaseURL: server.URL,
		Timeout: "5s",
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Ensure(context.Background(), "0.0.0")
	if !errors.Is(err, ErrCompilerNotFound) {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveLocalVersion(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'Version: 0.7.6+commit.7338295f'; exit 0; fi\nexit 1\n")
	mgr, err := NewManager(config.SolcConfig{
		BinaryPath: script,
		Timeout:    "5s",
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary, err := mgr.Ensure(context.Background(), "0.7.6+commit.7338295f")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if binary.SourceURL != "local" || binary.SHA256 == "" {
		t.Fatalf("binary = %+v", binary)
	}
}

func TestCompile(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\ncat >/dev/null\nprintf '%s' '{\"contracts\":{\"a.sol\":{\"A\":{\"evm\":{\"deployedBytecode\":{\"object\":\"6080\"}}}}}}'\n")
	mgr := newTestManager(t, "5s")
	out, err := mgr.Compile(context.Background(), Binary{Path: script}, []byte(`{"sources":{}}`))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out.Contracts) != 1 {
		t.Fatalf("contracts = %+v", out.Contracts)
	}
	contract, _, ok := out.FindContract("a.sol", "A")
	if !ok {
		t.Fatal("contract not found")
	}
	code, err := contract.Deployed()
	if err != nil || len(code) != 2 || code[0] != 0x60 || code[1] != 0x80 {
		t.Fatalf("code = %x, err = %v", code, err)
	}
}

func TestCompileFailure(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\ncat >/dev/null\nprintf '%s' '{\"errors\":[{\"severity\":\"error\",\"message\":\"boom\"}]}'\n")
	mgr := newTestManager(t, "5s")
	out, err := mgr.Compile(context.Background(), Binary{Path: script}, []byte(`{"sources":{}}`))
	if !errors.Is(err, ErrCompilationFailed) {
		t.Fatalf("error = %v", err)
	}
	if len(out.Errors) != 1 || out.Errors[0].Message != "boom" {
		t.Fatalf("output = %+v", out)
	}
}

func TestCompileTimeout(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\nexec sleep 2\n")
	mgr := newTestManager(t, "100ms")
	_, err := mgr.Compile(context.Background(), Binary{Path: script}, []byte(`{"sources":{}}`))
	if !errors.Is(err, ErrCompileTimeout) {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileOutputLimit(t *testing.T) {
	script := writeScript(t, "#!/bin/sh\nhead -c 1000 /dev/zero | tr '\\0' x\n")
	mgr := newTestManager(t, "5s")
	mgr.maxOutput = 100
	_, err := mgr.Compile(context.Background(), Binary{Path: script}, []byte(`{"sources":{}}`))
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileInputLimit(t *testing.T) {
	mgr := newTestManager(t, "5s")
	mgr.maxInput = 10
	_, err := mgr.Compile(context.Background(), Binary{Path: "/nonexistent"}, []byte(`{"sources":{"a":"0123456789"}}`))
	if !errors.Is(err, ErrInputTooLarge) {
		t.Fatalf("error = %v", err)
	}
}

func TestFindContractFallbacks(t *testing.T) {
	contract := Contract{}
	contract.EVM.DeployedBytecode.Object = "00"
	out := Output{Contracts: map[string]map[string]Contract{
		"./contracts/Pool.sol": {"Pool": contract},
	}}
	for _, path := range []string{"contracts/Pool.sol", "./contracts/Pool.sol"} {
		if _, _, ok := out.FindContract(path, "Pool"); !ok {
			t.Fatalf("not found for %s", path)
		}
	}
	if _, _, ok := out.FindContract("other/Pool.sol", "Pool"); !ok {
		t.Fatal("basename fallback failed")
	}
}

func TestParseOutputReferences(t *testing.T) {
	raw := []byte(`{
	  "contracts": {"a.sol": {"A": {"evm": {"deployedBytecode": {
	    "object": "6000",
	    "immutableReferences": {"1": [{"start": 1, "length": 1}]},
	    "linkReferences": {"b.sol": {"L": [{"start": 0, "length": 2}]}}
	  }}}}}
	}`)
	out, err := ParseOutput(raw)
	if err != nil {
		t.Fatal(err)
	}
	contract, _, ok := out.FindContract("a.sol", "A")
	if !ok {
		t.Fatal("contract not found")
	}
	if len(contract.EVM.DeployedBytecode.ImmutableReferences["1"]) != 1 {
		t.Fatal("immutable reference missing")
	}
	if len(contract.EVM.DeployedBytecode.LinkReferences["b.sol"]["L"]) != 1 {
		t.Fatal("link reference missing")
	}
}

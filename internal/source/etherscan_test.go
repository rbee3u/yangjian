package source

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rbee3u/yangjian/internal/config"
)

func TestEtherscanFetchStandardJSON(t *testing.T) {
	sourceContent := "contract Pool {}\n"
	abi := `[{"type":"function","name":"slot0","inputs":[],"outputs":[],"stateMutability":"view"}]`
	standard, err := json.Marshal(map[string]any{
		"language": "Solidity",
		"sources": map[string]any{
			"contracts/Pool.sol": map[string]string{"content": sourceContent},
		},
		"settings": map[string]any{
			"evmVersion": "istanbul",
			"optimizer":  map[string]any{"enabled": true, "runs": 800},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("chainid") != "1" || r.URL.Query().Get("address") != "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("module") != "contract" || r.URL.Query().Get("action") != "getsourcecode" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("apikey") != "test-key" {
			t.Errorf("apikey = %q", r.URL.Query().Get("apikey"))
		}
		response := map[string]any{
			"status":  "1",
			"message": "OK",
			"result": []map[string]string{{
				"SourceCode":       string(standard),
				"ABI":              abi,
				"ContractName":     "Pool",
				"CompilerVersion":  "v0.7.6+commit.7338295f",
				"OptimizationUsed": "1",
				"Runs":             "800",
				"EVMVersion":       "istanbul",
			}},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	client, err := NewEtherscan(config.ProviderProfile{Endpoint: server.URL, Timeout: "5s"}, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	record, err := client.Fetch(context.Background(), 1, "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(record.Sources) != 1 || record.Sources["contracts/Pool.sol"].Content != sourceContent {
		t.Fatalf("sources = %+v", record.Sources)
	}
	if record.Compilation.CompilerVersion != "0.7.6+commit.7338295f" {
		t.Fatalf("compiler = %q", record.Compilation.CompilerVersion)
	}
	if !record.Compilation.CompilerSettings.Optimizer.Enabled || record.Compilation.CompilerSettings.Optimizer.Runs != 800 {
		t.Fatalf("optimizer = %+v", record.Compilation.CompilerSettings.Optimizer)
	}
	if record.Compilation.CompilerSettings.EVMVersion != "istanbul" {
		t.Fatalf("evm = %q", record.Compilation.CompilerSettings.EVMVersion)
	}
	if !strings.Contains(string(record.ABI), "slot0") {
		t.Fatalf("abi = %s", record.ABI)
	}
	if len(record.Raw) == 0 {
		t.Fatal("raw response was not retained")
	}
}

func TestEtherscanNotVerified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "0",
			"message": "NOTOK",
			"result":  "Contract source code not verified",
		})
	}))
	defer server.Close()
	client, err := NewEtherscan(config.ProviderProfile{Endpoint: server.URL, Timeout: "5s"}, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Fetch(context.Background(), 1, "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	if !errors.Is(err, ErrNotVerified) {
		t.Fatalf("error = %v", err)
	}
}

func TestNewEtherscanMissingKey(t *testing.T) {
	if _, err := NewEtherscan(config.ProviderProfile{}, ""); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("error = %v", err)
	}
}

func TestCompareRecords(t *testing.T) {
	settings := Settings{EVMVersion: "istanbul"}
	settings.Optimizer.Enabled = true
	settings.Optimizer.Runs = 800
	primary := Record{
		ABI: json.RawMessage(`[{"type":"function","name":"slot0","inputs":[],"outputs":[]}]`),
		Sources: map[string]Source{
			"contracts/Pool.sol": {Content: "contract Pool {}"},
		},
		Compilation: Compilation{
			CompilerVersion:  "0.7.6+commit.7338295f",
			CompilerSettings: settings,
		},
	}
	secondary := primary
	secondary.Sources = map[string]Source{"Pool.sol": {Content: "contract Pool {}"}}
	matched, differences := CompareRecords(primary, secondary)
	if !matched || len(differences) != 0 {
		t.Fatalf("matched = %t, differences = %v", matched, differences)
	}

	compilerMismatch := secondary
	compilerMismatch.Compilation.CompilerVersion = "0.8.0"
	if matched, differences := CompareRecords(primary, compilerMismatch); matched || len(differences) == 0 {
		t.Fatalf("compiler differences = %v", differences)
	}

	abiMismatch := secondary
	abiMismatch.ABI = json.RawMessage(`[]`)
	if matched, differences := CompareRecords(primary, abiMismatch); matched || len(differences) == 0 {
		t.Fatalf("abi differences = %v", differences)
	}

	sourceMismatch := secondary
	sourceMismatch.Sources = map[string]Source{"Pool.sol": {Content: "contract Other {}"}}
	if matched, differences := CompareRecords(primary, sourceMismatch); matched || len(differences) == 0 {
		t.Fatalf("source differences = %v", differences)
	}
}

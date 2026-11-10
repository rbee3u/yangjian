package dossier

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/source"
	"github.com/rbee3u/yangjian/internal/store"
)

func p1Metadata(t *testing.T, content string) []byte {
	t.Helper()
	sourceHash := "0x" + hex.EncodeToString(crypto.Keccak256([]byte(content)))
	metadata := map[string]any{
		"compiler": map[string]any{"version": "0.7.6+commit.7338295f"},
		"settings": map[string]any{
			"compilationTarget": map[string]string{"contracts/Pool.sol": "Pool"},
			"evmVersion":        "istanbul",
			"libraries":         map[string]any{},
			"metadata":          map[string]string{"bytecodeHash": "ipfs"},
			"optimizer":         map[string]any{"enabled": true, "runs": 800},
			"remappings":        []string{},
		},
		"sources": map[string]any{
			"contracts/Pool.sol": map[string]string{"keccak256": sourceHash},
		},
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func runtimeWithMetadata(hash []byte) []byte {
	cbor := []byte{0xa2, 0x64, 'i', 'p', 'f', 's', 0x58, 0x22, 0x12, 0x20}
	cbor = append(cbor, hash...)
	cbor = append(cbor, 0x64, 's', 'o', 'l', 'c', 0x43, 0x00, 0x07, 0x06)
	length := len(cbor)
	code := []byte{0x60, 0x80}
	code = append(code, cbor...)
	code = append(code, byte(length>>8), byte(length))
	return code
}

func p1SourceRecord(t *testing.T, content string, metadata []byte) source.Record {
	t.Helper()
	stdJSON, err := json.Marshal(map[string]any{
		"language": "Solidity",
		"sources": map[string]any{
			"contracts/Pool.sol": map[string]string{"content": content},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := source.Settings{EVMVersion: "istanbul"}
	settings.Metadata.BytecodeHash = "ipfs"
	settings.Optimizer.Enabled = true
	settings.Optimizer.Runs = 800
	return source.Record{
		Address:       "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8",
		ChainID:       1,
		Match:         "exact_match",
		CreationMatch: "exact_match",
		RuntimeMatch:  "exact_match",
		VerifiedAt:    "2025-06-23T06:08:56Z",
		ABI:           json.RawMessage(`[]`),
		Metadata:      metadata,
		Sources: map[string]source.Source{
			"contracts/Pool.sol": {Content: content},
		},
		StdJSONInput: stdJSON,
		Compilation: source.Compilation{
			Compiler:         "solc",
			CompilerVersion:  "0.7.6+commit.7338295f",
			CompilerSettings: settings,
		},
		StorageLayout: json.RawMessage(`{"storage":[],"types":{}}`),
		Raw:           []byte(`{"mock":true}`),
	}
}

func newP1Dossier(t *testing.T, runtime func(metadata []byte) []byte) (*store.Store, common.Address) {
	t.Helper()
	content := "contract Pool {}"
	metadata := p1Metadata(t, content)
	code := runtime(metadata)
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	address := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDC58f1908eb6e6D8")
	chain := &fakeChain{header: testHeader(), code: code}
	if _, err := RunCode(context.Background(), chain, CodeParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-code",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := RunSource(context.Background(), SourceParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-source",
		Fetcher: fakeFetcher{record: p1SourceRecord(t, content, metadata)},
	}); err != nil {
		t.Fatal(err)
	}
	return st, address
}

func runProvenanceWithCompilerFailure(t *testing.T, st *store.Store, address common.Address) {
	t.Helper()
	compiler := &fakeCompiler{ensureErr: errors.New("compiler offline")}
	_, err := RunProvenance(context.Background(), ProvenanceParams{
		ChainID:  1,
		Address:  address,
		Store:    st,
		RunID:    "run-provenance",
		Compiler: compiler,
	})
	if err == nil {
		t.Fatal("expected compiler ensure error")
	}
}

func TestRunProvenanceP1Match(t *testing.T) {
	st, address := newP1Dossier(t, func(metadata []byte) []byte {
		return runtimeWithMetadata(crypto.Keccak256(metadata))
	})
	runProvenanceWithCompilerFailure(t, st, address)

	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.SourceRevisionIDs) != 1 {
		t.Fatalf("source revisions = %v", manifest.SourceRevisionIDs)
	}
	revision, err := readRevision(t, st, manifest.SourceRevisionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if revision.ProvenanceLevel != "P1" || revision.Correspondence != "proven" {
		t.Fatalf("revision = %+v", revision)
	}
	if contains(manifest.OpenQuestions, "P1 metadata binding not attempted") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	for _, question := range manifest.OpenQuestions {
		if strings.HasPrefix(question, "P1 metadata binding failed") {
			t.Fatalf("open questions = %v", manifest.OpenQuestions)
		}
	}
	found := false
	for _, id := range manifest.ObservationIDs {
		observation, err := readObservation(st, id)
		if err != nil {
			t.Fatal(err)
		}
		if observation.Kind == "metadata_binding" {
			found = true
			if matched, _ := observation.Meta["matched"].(bool); !matched {
				t.Fatalf("observation = %+v", observation)
			}
		}
	}
	if !found {
		t.Fatal("metadata_binding observation missing")
	}
}

func TestRunProvenanceP1Mismatch(t *testing.T) {
	st, address := newP1Dossier(t, func(metadata []byte) []byte {
		return runtimeWithMetadata(make([]byte, 32))
	})
	runProvenanceWithCompilerFailure(t, st, address)

	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	revision, err := readRevision(t, st, manifest.SourceRevisionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if revision.ProvenanceLevel != "P0" || revision.Correspondence != "unproven" {
		t.Fatalf("revision = %+v", revision)
	}
	found := false
	for _, question := range manifest.OpenQuestions {
		if strings.HasPrefix(question, "P1 metadata binding failed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
}

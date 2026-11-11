package fixtures

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/dossier"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/source"
	"github.com/rbee3u/yangjian/internal/store"
)

var updateGolden = flag.Bool("update", false, "update golden files")

type sourceFixture struct {
	SchemaVersion   string          `json:"schema_version"`
	ChainID         int64           `json:"chain_id"`
	Target          string          `json:"target"`
	Address         string          `json:"address"`
	Block           model.BlockRef  `json:"block"`
	RuntimeBytecode string          `json:"runtime_bytecode"`
	Sourcify        source.Record   `json:"sourcify"`
	SourcifyRaw     json.RawMessage `json:"sourcify_raw,omitempty"`
	RecordedAt      time.Time       `json:"recorded_at"`
}

type sourceGolden struct {
	SchemaVersion        string         `json:"schema_version"`
	Target               string         `json:"target"`
	Pool                 string         `json:"pool"`
	Block                model.BlockRef `json:"block"`
	RuntimeBytecodeHash  string         `json:"runtime_bytecode_hash"`
	RuntimeBytes         int            `json:"runtime_bytes"`
	Compiler             string         `json:"compiler"`
	RegistryMatch        string         `json:"registry_match"`
	SourceCount          int            `json:"source_count"`
	SourceHashMismatches int            `json:"source_hash_mismatches"`
	MissingSources       int            `json:"missing_sources"`
	ABIHash              string         `json:"abi_hash"`
	SourceRevisionID     string         `json:"source_revision_id"`
}

type fixtureFetcher struct {
	record source.Record
}

func (f fixtureFetcher) Fetch(ctx context.Context, chainID int64, address string) (source.Record, error) {
	return f.record, nil
}

func loadFixture(t *testing.T) sourceFixture {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "fixtures", "sourcify", "uniswap-v3-pool.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture sourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if fixture.SchemaVersion != model.SchemaVersion || fixture.ChainID <= 0 || fixture.Block.Number == 0 {
		t.Fatalf("fixture header is invalid: %+v", fixture)
	}
	code, err := hex.DecodeString(trimHex(fixture.RuntimeBytecode))
	if err != nil || len(code) == 0 {
		t.Fatalf("fixture runtime bytecode is invalid: %v", err)
	}
	if fixture.Sourcify.Address == "" || fixture.Sourcify.ChainID == 0 {
		t.Fatalf("fixture source record is invalid")
	}
	if len(fixture.SourcifyRaw) == 0 {
		t.Fatalf("fixture does not preserve the raw provider response")
	}
	fixture.Sourcify.Raw = append([]byte(nil), fixture.SourcifyRaw...)
	return fixture
}

func trimHex(value string) string {
	if len(value) >= 2 && (value[:2] == "0x" || value[:2] == "0X") {
		return value[2:]
	}
	return value
}

func TestReplaySourcifyPoolFixture(t *testing.T) {
	fixture := loadFixture(t)
	code, err := hex.DecodeString(trimHex(fixture.RuntimeBytecode))
	if err != nil {
		t.Fatal(err)
	}
	address := common.HexToAddress(fixture.Address)
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	codeObservation, err := st.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "rpc_call",
		RunID:         "fixture-replay",
		Request: map[string]any{
			"method": "eth_getCode",
			"params": []any{fixture.Address, fixture.Block.Number},
		},
		ResponseHash: model.ContentHash(code),
		Block:        &fixture.Block,
		RetrievedAt:  now,
		Source:       model.SourceRef{Provider: "fixture", EndpointClass: "offline"},
	})
	if err != nil {
		t.Fatal(err)
	}
	contractID := model.ContractID(fixture.ChainID, fixture.Address)
	versionID := model.ContractVersionID(fixture.ChainID, fixture.Address, fixture.Block.Number)
	contract := model.Contract{
		SchemaVersion: model.SchemaVersion,
		ID:            contractID,
		ChainID:       fixture.ChainID,
		Address:       address.Hex(),
		Versions:      []string{versionID},
		EvidenceRefs:  []string{codeObservation.ID},
	}
	_, contractHash, err := st.WriteJSONHashed(filepath.Join("entities", "contracts", "fixture.json"), contract)
	if err != nil {
		t.Fatal(err)
	}
	runtimeKeccak := "keccak256:" + hex.EncodeToString(crypto.Keccak256(code))
	version := model.ContractVersion{
		SchemaVersion:       model.SchemaVersion,
		ID:                  versionID,
		ContractID:          contractID,
		ValidFromBlock:      fixture.Block.Number,
		ValidFromSource:     "observed",
		RuntimeBytecodeHash: runtimeKeccak,
		ProxyType:           "none",
		EvidenceRefs:        []string{codeObservation.ID},
	}
	versionRel := filepath.Join("entities", "versions", "fixture.json")
	_, versionHash, err := st.WriteJSONHashed(versionRel, version)
	if err != nil {
		t.Fatal(err)
	}
	manifest := model.Manifest{
		SchemaVersion:  model.SchemaVersion,
		DossierType:    "contract",
		Target:         model.TargetRef{ChainID: fixture.ChainID, Address: address.Hex()},
		ToolVersion:    "fixture",
		RunID:          "fixture-replay",
		Status:         "partial",
		Block:          fixture.Block,
		Artifacts:      []model.Artifact{{Kind: "contract", Path: "entities/contracts/fixture.json", Hash: contractHash}, {Kind: "contract_version", Path: versionRel, Hash: versionHash}},
		ContractIDs:    []string{contractID},
		ObservationIDs: []string{codeObservation.ID},
		RelationIDs:    []string{},
		OpenQuestions:  []string{"source provenance not resolved"},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if _, err := st.WriteJSON("manifest.json", manifest); err != nil {
		t.Fatal(err)
	}

	result, err := dossier.RunSource(context.Background(), dossier.SourceParams{
		ChainID: fixture.ChainID,
		Address: address,
		Store:   st,
		RunID:   "fixture-replay",
		Fetcher: fixtureFetcher{record: fixture.Sourcify},
	})
	if err != nil {
		t.Fatalf("run source: %v", err)
	}
	if result.SourceHashMismatches != 0 || result.MissingSources != 0 {
		t.Fatalf("source replay mismatches: %+v", result)
	}
	if result.SourceRevisionID == "" || result.Compiler == "" || result.SourceCount == 0 {
		t.Fatalf("source result = %+v", result)
	}
	var storedVersion model.ContractVersion
	if err := st.ReadJSON(versionRel, &storedVersion); err != nil {
		t.Fatal(err)
	}
	actual := sourceGolden{
		SchemaVersion:        model.SchemaVersion,
		Target:               fixture.Target,
		Pool:                 address.Hex(),
		Block:                fixture.Block,
		RuntimeBytecodeHash:  runtimeKeccak,
		RuntimeBytes:         len(code),
		Compiler:             result.Compiler,
		RegistryMatch:        result.RegistryMatch,
		SourceCount:          result.SourceCount,
		SourceHashMismatches: result.SourceHashMismatches,
		MissingSources:       result.MissingSources,
		ABIHash:              storedVersion.ABIHash,
		SourceRevisionID:     result.SourceRevisionID,
	}

	goldenPath := filepath.Join("..", "..", "testdata", "golden", "sourcify", "uniswap-v3-pool.json")
	if *updateGolden {
		data, err := json.MarshalIndent(actual, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run go test ./internal/fixtures -update): %v", err)
	}
	var expected sourceGolden
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("golden mismatch\nactual:   %+v\nexpected: %+v", actual, expected)
	}
}

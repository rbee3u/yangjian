package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/schema"
)

var (
	now       = time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	testAddr  = "0x1111111111111111111111111111111111111111"
	testBlock = model.BlockRef{ChainID: 1, Number: 100, Hash: "0x" + strings.Repeat("11", 32)}
)

func validObservation() model.Observation {
	return model.Observation{
		SchemaVersion: model.SchemaVersion,
		ID:            "sha256:obs",
		Kind:          "rpc_call",
		RunID:         "run",
		Request:       map[string]any{"method": "eth_getCode"},
		ResponseHash:  "sha256:response",
		RetrievedAt:   now,
		Source:        model.SourceRef{Provider: "rpc"},
	}
}

func validContract() model.Contract {
	return model.Contract{
		SchemaVersion: model.SchemaVersion,
		ID:            "1:" + strings.ToLower(testAddr),
		ChainID:       1,
		Address:       testAddr,
		Versions:      []string{"1:" + strings.ToLower(testAddr) + "@100"},
		EvidenceRefs:  []string{"sha256:obs"},
	}
}

func validVersion() model.ContractVersion {
	return model.ContractVersion{
		SchemaVersion:       model.SchemaVersion,
		ID:                  "1:" + strings.ToLower(testAddr) + "@100",
		ContractID:          "1:" + strings.ToLower(testAddr),
		ValidFromBlock:      100,
		ValidFromSource:     "observed",
		RuntimeBytecodeHash: "keccak256:abc",
		ProxyType:           "none",
		EvidenceRefs:        []string{"sha256:obs"},
	}
}

func validRevision() model.SourceRevision {
	return model.SourceRevision{
		SchemaVersion:   model.SchemaVersion,
		ID:              "sha256:rev",
		Sources:         []model.SourceFile{{Path: "contracts/Pool.sol", Keccak256: "0xabc", ContentRef: "artifacts/sources/abc.sol"}},
		Compilation:     model.Compilation{Compiler: "0.7.6+commit.7338295f"},
		RegistryMatch:   "match",
		ProvenanceLevel: "P0",
		Correspondence:  "unproven",
		EvidenceRefs:    []string{"sha256:obs"},
	}
}

func validRelation() model.Relation {
	return model.Relation{
		SchemaVersion: model.SchemaVersion,
		ID:            "source_dependency:a:b:sha256:obs",
		Type:          "source_dependency",
		From:          "source:sha256:rev#contracts/A.sol",
		To:            "source:keccak256:abc",
		Attributes:    map[string]any{"source_path": "contracts/A.sol", "symbol": "A", "relation": "import"},
		EvidenceRefs:  []string{"sha256:obs"},
	}
}

func validManifest() model.Manifest {
	return model.Manifest{
		SchemaVersion:  model.SchemaVersion,
		DossierType:    "contract",
		Target:         model.TargetRef{ChainID: 1, Address: testAddr},
		ToolVersion:    "test",
		RunID:          "run",
		Status:         "partial",
		Block:          testBlock,
		Artifacts:      []model.Artifact{{Kind: "contract", Path: "entities/contracts/x.json", Hash: "sha256:x"}},
		ObservationIDs: []string{"sha256:obs"},
		RelationIDs:    []string{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func TestValidEntities(t *testing.T) {
	values := []any{
		validObservation(), validContract(), validVersion(), validRevision(), validRelation(), validManifest(),
		model.Protocol{
			SchemaVersion: model.SchemaVersion, ID: "protocol:test", Name: "Test",
			Deployments: []string{"1:test@100"}, TargetRegistryRefs: []string{"targets/mainnet.json#test"},
			EvidenceRefs: []string{"sha256:obs"},
		},
		model.Deployment{
			SchemaVersion: model.SchemaVersion, ID: "1:test@100", ProtocolID: "protocol:test",
			ChainID: 1, Block: testBlock, ContractVersionIDs: []string{}, EvidenceRefs: []string{"sha256:obs"},
		},
	}
	for _, value := range values {
		if err := schema.Validate(value); err != nil {
			t.Fatalf("%T: %v", value, err)
		}
	}
}

func TestInvalidEntities(t *testing.T) {
	observation := validObservation()
	observation.ResponseHash = ""
	contract := validContract()
	contract.EvidenceRefs = nil
	version := validVersion()
	version.ProxyType = "transparent"
	revision := validRevision()
	revision.ProvenanceLevel = "P9"
	relation := validRelation()
	delete(relation.Attributes, "relation")
	manifest := validManifest()
	manifest.Block.Hash = "0xabc"
	manifest.DossierType = "protocol"
	manifest.Target = model.TargetRef{ChainID: 1}
	protocol := model.Protocol{SchemaVersion: model.SchemaVersion, ID: "protocol:test", Name: "Test"}
	deployment := model.Deployment{SchemaVersion: model.SchemaVersion, ID: "x", ProtocolID: "protocol:test", ChainID: 1}
	cases := map[string]any{
		"observation":  observation,
		"contract":     contract,
		"version":      version,
		"revision":     revision,
		"relation":     relation,
		"manifest":     manifest,
		"protocol":     protocol,
		"deployment":   deployment,
		"nil manifest": (*model.Manifest)(nil),
	}
	for name, value := range cases {
		if err := schema.Validate(value); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestObservedCallRequiresTraceAttributes(t *testing.T) {
	relation := model.Relation{
		SchemaVersion: model.SchemaVersion,
		ID:            "observed_call:a:b:c",
		Type:          "observed_call",
		From:          "account:1:" + strings.ToLower(testAddr),
		To:            "1:" + strings.ToLower(testAddr),
		Attributes:    map[string]any{"tx_hash": "0xabc", "trace_reference": "sha256:obs", "call_path": "0"},
		EvidenceRefs:  []string{"sha256:obs"},
	}
	if err := schema.Validate(relation); err != nil {
		t.Fatalf("valid observed_call: %v", err)
	}
	delete(relation.Attributes, "call_path")
	if err := schema.Validate(relation); err == nil {
		t.Fatal("expected error for missing call_path")
	}
}

func TestSchemaFilesParse(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "schemas", "*.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 8 {
		t.Fatalf("schemas = %d, want at least 8", len(paths))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if document["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
			t.Fatalf("%s: missing draft 2020-12 $schema", path)
		}
		if document["type"] != "object" || document["title"] == nil {
			t.Fatalf("%s: missing object type or title", path)
		}
	}
}

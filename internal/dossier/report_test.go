package dossier

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

func newReportFixture(t *testing.T) (*store.Store, common.Address) {
	t.Helper()
	st, address, _, _, err := runStateFixture(t, true)
	if err != nil {
		t.Fatal(err)
	}
	compiler := &fakeCompiler{binary: testBinary(), output: outputWithCode([]byte{0x60, 0x80})}
	if _, err := RunProvenance(context.Background(), ProvenanceParams{
		ChainID:  1,
		Address:  address,
		Store:    st,
		RunID:    "run-provenance",
		Compiler: compiler,
	}); err != nil {
		t.Fatal(err)
	}
	return st, address
}

func TestRunReport(t *testing.T) {
	st, address := newReportFixture(t)
	result, err := RunReport(ReportParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-report",
	})
	if err != nil {
		t.Fatalf("run report: %v", err)
	}
	if result.ReportPath != "report.md" || result.TopologyPath != "topology.mmd" || result.ArchitecturePath != "architecture.mmd" {
		t.Fatalf("result = %+v", result)
	}
	report, err := os.ReadFile(st.Path(result.ReportPath))
	if err != nil {
		t.Fatal(err)
	}
	text := string(report)
	for _, want := range []string{
		"# Contract Dossier:",
		"not a security audit",
		"P2",
		"match",
		"slot0.tick",
		"token0",
		"onchain_reference",
		"## Open questions",
		"## Evidence index",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q", want)
		}
	}

	topology, err := os.ReadFile(st.Path(result.TopologyPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"flowchart LR", "-->|token0|", "-->|token1|", "-->|factory|"} {
		if !strings.Contains(string(topology), want) {
			t.Fatalf("topology missing %q:\n%s", want, topology)
		}
	}

	architecture, err := os.ReadFile(st.Path(result.ArchitecturePath))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Runtime code", "SourceRevision", "P2 build", "State @"} {
		if !strings.Contains(string(architecture), want) {
			t.Fatalf("architecture missing %q:\n%s", want, architecture)
		}
	}

	relationsRaw, err := os.ReadFile(st.Path(result.RelationsPath))
	if err != nil {
		t.Fatal(err)
	}
	var relations []model.Relation
	if err := json.Unmarshal(relationsRaw, &relations); err != nil {
		t.Fatal(err)
	}
	if len(relations) != 3 {
		t.Fatalf("relations = %+v", relations)
	}
	for _, relation := range relations {
		if relation.Type != "onchain_reference" {
			t.Fatalf("relation = %+v", relation)
		}
	}

	var evidence struct {
		Observations []model.Observation `json:"observations"`
		Artifacts    []model.Artifact    `json:"artifacts"`
		Relations    []model.Relation    `json:"relations"`
	}
	evidenceRaw, err := os.ReadFile(st.Path(result.EvidencePath))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(evidenceRaw, &evidence); err != nil {
		t.Fatal(err)
	}
	if len(evidence.Observations) < 12 || len(evidence.Relations) != 3 {
		t.Fatalf("evidence = observations %d relations %d", len(evidence.Observations), len(evidence.Relations))
	}
	kinds := make(map[string]bool)
	for _, artifact := range evidence.Artifacts {
		kinds[artifact.Kind] = true
	}
	for _, kind := range []string{"report", "topology", "architecture", "relations"} {
		if !kinds[kind] {
			t.Fatalf("evidence artifacts missing %s: %v", kind, kinds)
		}
	}

	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if contains(manifest.OpenQuestions, "report not generated") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	if !contains(manifest.OpenQuestions, "proxy detection not run") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	if len(result.ObservationIDs) != 1 {
		t.Fatalf("report observation = %v", result.ObservationIDs)
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Hash == "" {
			t.Fatalf("artifact hash missing: %+v", artifact)
		}
		data, err := os.ReadFile(st.Path(artifact.Path))
		if err != nil {
			t.Fatal(err)
		}
		if model.ContentHash(data) != artifact.Hash {
			t.Fatalf("artifact hash mismatch: %+v", artifact)
		}
	}
	manifestKinds := make(map[string]bool)
	for _, artifact := range manifest.Artifacts {
		manifestKinds[artifact.Kind] = true
	}
	if !manifestKinds["evidence"] {
		t.Fatalf("manifest artifacts missing evidence: %v", manifestKinds)
	}
}

func TestRunReportOnCodeOnlyDossier(t *testing.T) {
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	address := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	chain := &fakeChain{header: testHeader(), code: []byte{0x60}}
	if _, err := RunCode(context.Background(), chain, CodeParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-code",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := RunReport(ReportParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-report",
	})
	if err != nil {
		t.Fatalf("run report: %v", err)
	}
	report, err := os.ReadFile(st.Path(result.ReportPath))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Source provenance is not resolved", "State reads have not been run", "ABI artifact is unavailable"} {
		if !strings.Contains(string(report), want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

package dossier

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/solc"
	"github.com/rbee3u/yangjian/internal/store"
)

type fakeCompiler struct {
	binary    solc.Binary
	output    solc.Output
	err       error
	ensureErr error
	input     []byte
}

func (f *fakeCompiler) Ensure(ctx context.Context, longVersion string) (solc.Binary, error) {
	if f.ensureErr != nil {
		return solc.Binary{}, f.ensureErr
	}
	return f.binary, nil
}

func (f *fakeCompiler) Compile(ctx context.Context, binary solc.Binary, input []byte) (solc.Output, error) {
	f.input = input
	return f.output, f.err
}

func testBinary() solc.Binary {
	return solc.Binary{
		Version:   "0.7.6+commit.7338295f",
		SHA256:    "sha256:test",
		Keccak256: "keccak256:test",
		SourceURL: "test",
	}
}

func outputWithCode(code []byte) solc.Output {
	contract := solc.Contract{}
	contract.EVM.DeployedBytecode.Object = hex.EncodeToString(code)
	return solc.Output{
		Raw: []byte(`{"contracts":{"contracts/Pool.sol":{"Pool":{}}}}`),
		Contracts: map[string]map[string]solc.Contract{
			"contracts/Pool.sol": {"Pool": contract},
		},
	}
}

func newResolvedDossier(t *testing.T) (*store.Store, common.Address) {
	t.Helper()
	st, address := newCodeDossier(t)
	record := makeSourceRecord(t, "contract Pool {}", "")
	if _, err := RunSource(context.Background(), SourceParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-source",
		Fetcher: fakeFetcher{record: record},
	}); err != nil {
		t.Fatal(err)
	}
	return st, address
}

func TestRunProvenanceMatch(t *testing.T) {
	st, address := newResolvedDossier(t)
	var before model.Manifest
	if err := st.ReadJSON("manifest.json", &before); err != nil {
		t.Fatal(err)
	}
	before.OpenQuestions = append(before.OpenQuestions,
		"P2 compilation failed: stale",
		"P2 rebuild did not match runtime bytecode (stale)",
	)
	if _, err := st.WriteJSON("manifest.json", before); err != nil {
		t.Fatal(err)
	}
	compiler := &fakeCompiler{binary: testBinary(), output: outputWithCode([]byte{0x60, 0x80})}
	result, err := RunProvenance(context.Background(), ProvenanceParams{
		ChainID:  1,
		Address:  address,
		Store:    st,
		RunID:    "run-provenance",
		Compiler: compiler,
	})
	if err != nil {
		t.Fatalf("run provenance: %v", err)
	}
	if !result.Match || result.ProvenanceLevel != "P2" || result.Correspondence != "proven" {
		t.Fatalf("result = %+v", result)
	}
	if result.ComparedBytes != 2 || result.ImmutablesMasked != 0 || result.MismatchOffset != nil {
		t.Fatalf("result = %+v", result)
	}

	var input map[string]any
	if err := json.Unmarshal(compiler.input, &input); err != nil {
		t.Fatalf("compiled input: %v", err)
	}
	settings, _ := input["settings"].(map[string]any)
	if settings["outputSelection"] == nil {
		t.Fatal("outputSelection was not injected")
	}

	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.ObservationIDs) != 4 {
		t.Fatalf("observations = %v", manifest.ObservationIDs)
	}
	if contains(manifest.OpenQuestions, "local P2 rebuild not attempted") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	if contains(manifest.OpenQuestions, "P1 unavailable: metadata.bytecodeHash=none; P2 required") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	for _, question := range manifest.OpenQuestions {
		if strings.HasPrefix(question, "P2 compilation failed") || strings.HasPrefix(question, "P2 rebuild did not match") {
			t.Fatalf("stale provenance question = %q", question)
		}
	}

	revision, err := readRevision(t, st, manifest.SourceRevisionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if revision.ProvenanceLevel != "P2" || revision.Correspondence != "proven" {
		t.Fatalf("revision = %+v", revision)
	}
	if revision.Build == nil || !revision.Build.Match || revision.Build.ComparedBytes != 2 {
		t.Fatalf("build = %+v", revision.Build)
	}
}

func TestRunSourcePreservesBuild(t *testing.T) {
	st, address := newResolvedDossier(t)
	compiler := &fakeCompiler{binary: testBinary(), output: outputWithCode([]byte{0x60, 0x80})}
	if _, err := RunProvenance(context.Background(), ProvenanceParams{
		ChainID:  1,
		Address:  address,
		Store:    st,
		RunID:    "run-provenance",
		Compiler: compiler,
	}); err != nil {
		t.Fatalf("run provenance: %v", err)
	}
	record := makeSourceRecord(t, "contract Pool {}", "")
	result, err := RunSource(context.Background(), SourceParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-source-again",
		Fetcher: fakeFetcher{record: record},
	})
	if err != nil {
		t.Fatalf("run source again: %v", err)
	}
	if result.ProvenanceLevel != "P2" || result.Correspondence != "proven" {
		t.Fatalf("result = %+v", result)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if contains(manifest.OpenQuestions, "local P2 rebuild not attempted") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	if contains(manifest.OpenQuestions, "P1 unavailable: metadata.bytecodeHash=none; P2 required") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	revision, err := readRevision(t, st, manifest.SourceRevisionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if revision.Build == nil || !revision.Build.Match || revision.ProvenanceLevel != "P2" {
		t.Fatalf("revision = %+v", revision)
	}
}

func TestRunProvenanceMismatch(t *testing.T) {
	st, address := newResolvedDossier(t)
	compiler := &fakeCompiler{binary: testBinary(), output: outputWithCode([]byte{0x60, 0x81})}
	result, err := RunProvenance(context.Background(), ProvenanceParams{
		ChainID:  1,
		Address:  address,
		Store:    st,
		RunID:    "run-provenance",
		Compiler: compiler,
	})
	if err != nil {
		t.Fatalf("run provenance: %v", err)
	}
	if result.Match || result.ProvenanceLevel != "P0" || result.Correspondence != "unproven" {
		t.Fatalf("result = %+v", result)
	}
	if result.MismatchOffset == nil || *result.MismatchOffset != 1 {
		t.Fatalf("mismatch offset = %v", result.MismatchOffset)
	}

	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if !contains(manifest.OpenQuestions, "P2 rebuild did not match runtime bytecode (first mismatch at byte 1)") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	revision, err := readRevision(t, st, manifest.SourceRevisionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if revision.Build == nil || revision.Build.Match {
		t.Fatalf("build = %+v", revision.Build)
	}
}

func TestRunProvenanceCompileFailure(t *testing.T) {
	st, address := newResolvedDossier(t)
	compiler := &fakeCompiler{
		binary: testBinary(),
		output: solc.Output{
			Raw:    []byte(`{"errors":[{"severity":"error","message":"boom"}]}`),
			Errors: []solc.Diagnostic{{Severity: "error", Message: "boom"}},
		},
		err: fmt.Errorf("%w: boom", solc.ErrCompilationFailed),
	}
	_, err := RunProvenance(context.Background(), ProvenanceParams{
		ChainID:  1,
		Address:  address,
		Store:    st,
		RunID:    "run-provenance",
		Compiler: compiler,
	})
	if !errors.Is(err, solc.ErrCompilationFailed) {
		t.Fatalf("error = %v", err)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, question := range manifest.OpenQuestions {
		if strings.HasPrefix(question, "P2 compilation failed:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	revision, err := readRevision(t, st, manifest.SourceRevisionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if revision.Build == nil || revision.Build.Match {
		t.Fatalf("build = %+v", revision.Build)
	}
	if revision.ProvenanceLevel != "P0" {
		t.Fatalf("provenance = %s", revision.ProvenanceLevel)
	}
}

func TestRunProvenanceNeedsSource(t *testing.T) {
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteJSON("manifest.json", model.Manifest{
		SchemaVersion:  model.SchemaVersion,
		DossierType:    "contract",
		Target:         model.TargetRef{ChainID: 1, Address: "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"},
		RunID:          "run-provenance",
		Status:         "partial",
		Block:          model.BlockRef{ChainID: 1, Number: 1, Hash: "0x" + strings.Repeat("00", 32)},
		ObservationIDs: []string{"sha256:test"},
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := RunProvenance(context.Background(), ProvenanceParams{
		ChainID:  1,
		Address:  common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"),
		Store:    st,
		RunID:    "run-provenance",
		Compiler: &fakeCompiler{binary: testBinary()},
	})
	if !errors.Is(err, ErrSourceRevisionMissing) {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildP2InputRejectsRemoteAndTraversal(t *testing.T) {
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	metadata := []byte(`{"settings":{"compilationTarget":{"contracts/Pool.sol":"Pool"}}}`)
	if _, err := st.WriteFile("artifacts/metadata.json", metadata); err != nil {
		t.Fatal(err)
	}
	revision := model.SourceRevision{
		MetadataRef:     "artifacts/metadata.json",
		StdJSONInputRef: "artifacts/stdjsoninput.json",
	}
	cases := []struct {
		name    string
		sources string
		want    string
	}{
		{"urls", `{"contracts/Pool.sol":{"urls":["http://example.invalid"]}}`, "urls"},
		{"traversal", `{"../Pool.sol":{"content":"contract Pool {}"}}`, "escapes"},
		{"absolute", `{"/etc/Pool.sol":{"content":"contract Pool {}"}}`, "absolute"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(`{"language":"Solidity","sources":` + tc.sources + `,"settings":{}}`)
			_, _, err := buildP2Input(raw, revision, st)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestMaskAndCompare(t *testing.T) {
	code := []byte{1, 2, 3, 4}
	masked := maskBytecode(code, []solc.Reference{{Start: 1, Length: 2}})
	if masked[0] != 1 || masked[1] != 0 || masked[2] != 0 || masked[3] != 4 {
		t.Fatalf("masked = %v", masked)
	}
	if code[1] != 2 {
		t.Fatal("mask mutated the original")
	}
	match, compared, offset := compareBytecode([]byte{1, 2}, []byte{1, 2})
	if !match || compared != 2 || offset != nil {
		t.Fatalf("match=%t compared=%d offset=%v", match, compared, offset)
	}
	match, compared, offset = compareBytecode([]byte{1, 2}, []byte{1, 3})
	if match || compared != 2 || offset == nil || *offset != 1 {
		t.Fatalf("match=%t compared=%d offset=%v", match, compared, offset)
	}
	match, compared, offset = compareBytecode([]byte{1, 2}, []byte{1, 2, 3})
	if match || compared != 2 || offset == nil || *offset != 2 {
		t.Fatalf("match=%t compared=%d offset=%v", match, compared, offset)
	}
}

func TestCollectMaskRanges(t *testing.T) {
	contract := solc.Contract{}
	contract.EVM.DeployedBytecode.ImmutableReferences = map[string][]solc.Reference{
		"1": {{Start: 1, Length: 2}},
	}
	contract.EVM.DeployedBytecode.LinkReferences = map[string]map[string][]solc.Reference{
		"a.sol": {"L": {{Start: 0, Length: 1}}},
	}
	immutables, links, err := collectMaskRanges(contract, 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(immutables) != 1 || len(links) != 1 {
		t.Fatalf("immutables=%v links=%v", immutables, links)
	}
	if _, _, err := collectMaskRanges(contract, 2, 4); err == nil {
		t.Fatal("expected range error")
	}
}

func readRevision(t *testing.T, st *store.Store, id string) (model.SourceRevision, error) {
	t.Helper()
	var revision model.SourceRevision
	rel := "entities/sources/" + strings.TrimPrefix(id, "sha256:") + ".json"
	err := st.ReadJSON(rel, &revision)
	return revision, err
}

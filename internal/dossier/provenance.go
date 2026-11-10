package dossier

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/solc"
	"github.com/rbee3u/yangjian/internal/store"
)

var (
	ErrSourceRevisionMissing  = errors.New("source revision missing; run the source stage first")
	ErrCodeObservationMissing = errors.New("onchain runtime bytecode observation not found")
)

const solcSandboxWarning = "solc runs with a timeout and input/output size caps; OS memory/CPU rlimits are not enforced"

type Compiler interface {
	Ensure(ctx context.Context, longVersion string) (solc.Binary, error)
	Compile(ctx context.Context, binary solc.Binary, input []byte) (solc.Output, error)
}

type ProvenanceParams struct {
	ChainID  int64
	Address  common.Address
	Store    *store.Store
	RunID    string
	Compiler Compiler
}

type ProvenanceResult struct {
	Compiler             string   `json:"compiler"`
	CompilerSHA256       string   `json:"compiler_sha256"`
	Match                bool     `json:"match"`
	ProvenanceLevel      string   `json:"provenance_level"`
	Correspondence       string   `json:"correspondence"`
	ImmutablesMasked     int      `json:"immutables_masked"`
	LinkReferencesMasked int      `json:"link_references_masked"`
	OnchainBytes         int      `json:"onchain_bytes"`
	RecompiledBytes      int      `json:"recompiled_bytes"`
	ComparedBytes        int      `json:"compared_bytes"`
	MismatchOffset       *int     `json:"mismatch_offset,omitempty"`
	Warnings             []string `json:"warnings,omitempty"`
	ObservationIDs       []string `json:"observation_ids"`
}

type compilationTarget struct {
	Source string
	Name   string
}

func RunProvenance(ctx context.Context, params ProvenanceParams) (ProvenanceResult, error) {
	if params.Store == nil {
		return ProvenanceResult{}, fmt.Errorf("store is nil")
	}
	if params.Compiler == nil {
		return ProvenanceResult{}, fmt.Errorf("compiler is nil")
	}
	if params.RunID == "" {
		return ProvenanceResult{}, fmt.Errorf("run id is required")
	}

	manifest, revision, revisionRel, err := loadSourceRevision(params.Store)
	if err != nil {
		return ProvenanceResult{}, err
	}
	if revision.StdJSONInputRef == "" {
		return ProvenanceResult{}, fmt.Errorf("source revision has no standard JSON input")
	}
	inputRaw, err := os.ReadFile(params.Store.Path(revision.StdJSONInputRef))
	if err != nil {
		return ProvenanceResult{}, fmt.Errorf("read standard JSON input: %w", err)
	}
	input, target, err := buildP2Input(inputRaw, revision, params.Store)
	if err != nil {
		return ProvenanceResult{}, err
	}
	if revision.Compilation.Compiler == "" {
		return ProvenanceResult{}, fmt.Errorf("source revision has no compiler version")
	}
	onchain, onchainObsID, err := findOnchainCode(manifest, params.Store)
	if err != nil {
		return ProvenanceResult{}, err
	}

	binary, err := params.Compiler.Ensure(ctx, revision.Compilation.Compiler)
	if err != nil {
		return ProvenanceResult{}, fmt.Errorf("ensure compiler %s: %w", revision.Compilation.Compiler, err)
	}

	now := time.Now().UTC()
	inputRel := "artifacts/provenance/stdjsoninput.json"
	onchainRel := "artifacts/provenance/onchain-runtime.bin"
	artifacts := make([]model.Artifact, 0, 5)
	appendArtifact := func(kind, rel string, data []byte) error {
		artifact, err := saveArtifact(params.Store, kind, rel, data)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, artifact)
		return nil
	}
	if err := appendArtifact("p2_std_json_input", inputRel, input); err != nil {
		return ProvenanceResult{}, err
	}
	if err := appendArtifact("onchain_runtime", onchainRel, onchain); err != nil {
		return ProvenanceResult{}, err
	}

	output, compileErr := params.Compiler.Compile(ctx, binary, input)
	outputRel := "artifacts/provenance/solc-output.json"
	if len(output.Raw) > 0 {
		if err := appendArtifact("solc_output", outputRel, output.Raw); err != nil {
			return ProvenanceResult{}, err
		}
	}
	if compileErr != nil {
		errorText := compileErr.Error()
		if output.Stderr != "" {
			errorText += "\n" + output.Stderr
		}
		errorRel := "artifacts/provenance/solc-error.txt"
		if err := appendArtifact("solc_error", errorRel, []byte(errorText)); err != nil {
			return ProvenanceResult{}, err
		}
		build := model.BuildEvidence{
			CompilerVersion:      binary.Version,
			CompilerSHA256:       binary.SHA256,
			CompilerKeccak256:    binary.Keccak256,
			CompilerSource:       binary.SourceURL,
			StandardJSONInputRef: inputRel,
			SolcOutputRef:        outputRel,
			OnchainRuntimeRef:    onchainRel,
			Match:                false,
			OnchainBytes:         len(onchain),
			Warnings:             []string{errorText, solcSandboxWarning},
		}
		obs, obsErr := writeBuildObservation(params, manifest, binary, input, target, output, build, compileErr)
		if obsErr != nil {
			return ProvenanceResult{}, obsErr
		}
		revision.Build = &build
		revision.ProvenanceLevel = "P0"
		revision.Correspondence = "unproven"
		revision.EvidenceRefs = appendUnique(revision.EvidenceRefs, obs.ID)
		if _, err := params.Store.WriteJSON(revisionRel, revision); err != nil {
			return ProvenanceResult{}, fmt.Errorf("update source revision: %w", err)
		}
		manifest.ObservationIDs = appendUnique(manifest.ObservationIDs, obs.ID)
		for _, artifact := range artifacts {
			manifest.Artifacts = appendUniqueArtifact(manifest.Artifacts, artifact)
		}
		manifest.OpenQuestions = removeString(manifest.OpenQuestions, "local P2 rebuild not attempted")
		manifest.OpenQuestions = removeMatching(manifest.OpenQuestions, "P2 compilation failed")
		manifest.OpenQuestions = removeMatching(manifest.OpenQuestions, "P2 rebuild did not match")
		manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, "P2 compilation failed: "+truncateText(compileErr.Error(), 300))
		manifest.Status = "partial"
		manifest.UpdatedAt = now
		if _, err := params.Store.WriteJSON("manifest.json", manifest); err != nil {
			return ProvenanceResult{}, fmt.Errorf("update manifest: %w", err)
		}
		return ProvenanceResult{}, compileErr
	}

	contract, foundPath, ok := output.FindContract(target.Source, target.Name)
	warnings := []string{solcSandboxWarning}
	if !ok {
		var fallbackErr error
		contract, foundPath, fallbackErr = findContractBySize(output, len(onchain))
		if fallbackErr != nil {
			return ProvenanceResult{}, fallbackErr
		}
		warnings = append(warnings, fmt.Sprintf("compilation target %s:%s not found; matched %s by runtime size", target.Source, target.Name, foundPath))
	}
	recompiled, err := contract.Deployed()
	if err != nil {
		return ProvenanceResult{}, fmt.Errorf("read recompiled bytecode: %w", err)
	}

	immutables, links, err := collectMaskRanges(contract, len(recompiled), len(onchain))
	if err != nil {
		return ProvenanceResult{}, err
	}
	maskedOnchain := maskBytecode(onchain, immutables, links)
	maskedRecompiled := maskBytecode(recompiled, immutables, links)
	match, compared, mismatch := compareBytecode(maskedOnchain, maskedRecompiled)

	recompiledRel := "artifacts/provenance/recompiled-runtime.bin"
	if err := appendArtifact("recompiled_runtime", recompiledRel, recompiled); err != nil {
		return ProvenanceResult{}, err
	}

	build := model.BuildEvidence{
		CompilerVersion:      binary.Version,
		CompilerSHA256:       binary.SHA256,
		CompilerKeccak256:    binary.Keccak256,
		CompilerSource:       binary.SourceURL,
		StandardJSONInputRef: inputRel,
		SolcOutputRef:        outputRel,
		OnchainRuntimeRef:    onchainRel,
		RecompiledRuntimeRef: recompiledRel,
		Match:                match,
		ImmutablesMasked:     len(immutables),
		LinkReferencesMasked: len(links),
		OnchainBytes:         len(onchain),
		RecompiledBytes:      len(recompiled),
		ComparedBytes:        compared,
		MismatchOffset:       mismatch,
		Warnings:             warnings,
	}
	obs, err := writeBuildObservation(params, manifest, binary, input, target, output, build, nil)
	if err != nil {
		return ProvenanceResult{}, err
	}

	revision.Build = &build
	revision.EvidenceRefs = appendUnique(revision.EvidenceRefs, obs.ID)
	if match {
		revision.ProvenanceLevel = "P2"
		revision.Correspondence = "proven"
	} else {
		revision.ProvenanceLevel = "P0"
		revision.Correspondence = "unproven"
	}
	if _, err := params.Store.WriteJSON(revisionRel, revision); err != nil {
		return ProvenanceResult{}, fmt.Errorf("update source revision: %w", err)
	}

	manifest.ObservationIDs = appendUnique(manifest.ObservationIDs, obs.ID)
	for _, artifact := range artifacts {
		manifest.Artifacts = appendUniqueArtifact(manifest.Artifacts, artifact)
	}
	manifest.OpenQuestions = removeString(manifest.OpenQuestions, "local P2 rebuild not attempted")
	manifest.OpenQuestions = removeMatching(manifest.OpenQuestions, "P2 compilation failed")
	manifest.OpenQuestions = removeMatching(manifest.OpenQuestions, "P2 rebuild did not match")
	if match {
		manifest.OpenQuestions = removeString(manifest.OpenQuestions, "P1 unavailable: metadata.bytecodeHash=none; P2 required")
	} else {
		question := "P2 rebuild did not match runtime bytecode"
		if mismatch != nil {
			question += fmt.Sprintf(" (first mismatch at byte %d)", *mismatch)
		}
		manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, question)
	}
	manifest.Status = "partial"
	manifest.UpdatedAt = now
	if _, err := params.Store.WriteJSON("manifest.json", manifest); err != nil {
		return ProvenanceResult{}, fmt.Errorf("update manifest: %w", err)
	}

	observationIDs := []string{obs.ID}
	if onchainObsID != "" {
		observationIDs = append(observationIDs, onchainObsID)
	}
	return ProvenanceResult{
		Compiler:             binary.Version,
		CompilerSHA256:       binary.SHA256,
		Match:                match,
		ProvenanceLevel:      revision.ProvenanceLevel,
		Correspondence:       revision.Correspondence,
		ImmutablesMasked:     len(immutables),
		LinkReferencesMasked: len(links),
		OnchainBytes:         len(onchain),
		RecompiledBytes:      len(recompiled),
		ComparedBytes:        compared,
		MismatchOffset:       mismatch,
		Warnings:             warnings,
		ObservationIDs:       observationIDs,
	}, nil
}

func loadSourceRevision(st *store.Store) (model.Manifest, model.SourceRevision, string, error) {
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		return model.Manifest{}, model.SourceRevision{}, "", fmt.Errorf("%w: read manifest: %v", ErrDossierNotInitialized, err)
	}
	if len(manifest.SourceRevisionIDs) == 0 {
		return model.Manifest{}, model.SourceRevision{}, "", ErrSourceRevisionMissing
	}
	revisionID := manifest.SourceRevisionIDs[len(manifest.SourceRevisionIDs)-1]
	rel := filepath.Join("entities", "sources", strings.TrimPrefix(revisionID, "sha256:")+".json")
	var revision model.SourceRevision
	if err := st.ReadJSON(rel, &revision); err != nil {
		return model.Manifest{}, model.SourceRevision{}, "", fmt.Errorf("read source revision %s: %w", revisionID, err)
	}
	return manifest, revision, rel, nil
}

func buildP2Input(raw []byte, revision model.SourceRevision, st *store.Store) ([]byte, compilationTarget, error) {
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return nil, compilationTarget{}, fmt.Errorf("parse standard JSON input: %w", err)
	}
	if language, _ := root["language"].(string); language != "" && language != "Solidity" {
		return nil, compilationTarget{}, fmt.Errorf("unsupported standard JSON language %q", language)
	}
	sources, ok := root["sources"].(map[string]any)
	if !ok || len(sources) == 0 {
		return nil, compilationTarget{}, fmt.Errorf("standard JSON input has no sources")
	}
	for path, value := range sources {
		source, ok := value.(map[string]any)
		if !ok {
			return nil, compilationTarget{}, fmt.Errorf("source %q has invalid shape", path)
		}
		if _, ok := source["urls"]; ok {
			return nil, compilationTarget{}, fmt.Errorf("source %q uses urls; refusing to let solc fetch it", path)
		}
		if _, ok := source["content"].(string); !ok {
			return nil, compilationTarget{}, fmt.Errorf("source %q has no inline content; refusing to let solc fetch it", path)
		}
		if err := validateSourcePath(path); err != nil {
			return nil, compilationTarget{}, err
		}
	}
	settings, ok := root["settings"].(map[string]any)
	if !ok {
		settings = map[string]any{}
		root["settings"] = settings
	}
	settings["outputSelection"] = map[string]any{
		"*": map[string]any{
			"": []string{"ast"},
			"*": []string{
				"abi",
				"evm.methodIdentifiers",
				"storageLayout",
				"evm.deployedBytecode.object",
				"evm.deployedBytecode.immutableReferences",
				"evm.deployedBytecode.linkReferences",
			},
		},
	}
	target, err := compilationTargetFrom(revision, st)
	if err != nil {
		return nil, compilationTarget{}, err
	}
	resolved := resolveSourceKey(sources, target.Source)
	if resolved == "" {
		return nil, compilationTarget{}, fmt.Errorf("compilation target source %q is not in the standard JSON input", target.Source)
	}
	target.Source = resolved
	encoded, err := json.Marshal(root)
	if err != nil {
		return nil, compilationTarget{}, fmt.Errorf("encode standard JSON input: %w", err)
	}
	return encoded, target, nil
}

func compilationTargetFrom(revision model.SourceRevision, st *store.Store) (compilationTarget, error) {
	if revision.MetadataRef == "" {
		return compilationTarget{}, fmt.Errorf("source revision has no metadata artifact; cannot determine compilation target")
	}
	raw, err := os.ReadFile(st.Path(revision.MetadataRef))
	if err != nil {
		return compilationTarget{}, fmt.Errorf("read metadata artifact: %w", err)
	}
	var metadata struct {
		Settings struct {
			CompilationTarget map[string]string `json:"compilationTarget"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return compilationTarget{}, fmt.Errorf("parse metadata artifact: %w", err)
	}
	if len(metadata.Settings.CompilationTarget) != 1 {
		return compilationTarget{}, fmt.Errorf("metadata compilationTarget must contain exactly one contract, got %d", len(metadata.Settings.CompilationTarget))
	}
	for source, name := range metadata.Settings.CompilationTarget {
		return compilationTarget{Source: source, Name: name}, nil
	}
	return compilationTarget{}, fmt.Errorf("metadata compilationTarget is empty")
}

func resolveSourceKey(sources map[string]any, target string) string {
	trimmed := strings.TrimPrefix(target, "./")
	for _, candidate := range []string{target, trimmed, "./" + trimmed} {
		if candidate == "" {
			continue
		}
		if _, ok := sources[candidate]; ok {
			return candidate
		}
	}
	return ""
}

func validateSourcePath(path string) error {
	if path == "" {
		return fmt.Errorf("empty source path")
	}
	if strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("source path %q contains NUL", path)
	}
	cleaned := strings.TrimPrefix(path, "./")
	if cleaned == "" || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "/") {
		return fmt.Errorf("source path %q is absolute", path)
	}
	if strings.Contains(cleaned, "\\") {
		return fmt.Errorf("source path %q contains a backslash", path)
	}
	if volume := filepath.VolumeName(cleaned); volume != "" {
		return fmt.Errorf("source path %q has a volume", path)
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			return fmt.Errorf("source path %q escapes the source root", path)
		}
	}
	return nil
}

func findOnchainCode(manifest model.Manifest, st *store.Store) ([]byte, string, error) {
	observed := ""
	for _, id := range manifest.ObservationIDs {
		rel := filepath.Join("observations", strings.TrimPrefix(id, "sha256:")+".json")
		var obs model.Observation
		if err := st.ReadJSON(rel, &obs); err != nil {
			continue
		}
		method, _ := obs.Request["method"].(string)
		if method != "eth_getCode" {
			continue
		}
		if obs.Block != nil && manifest.Block.Number != 0 && obs.Block.Number != manifest.Block.Number {
			continue
		}
		hexCode, _ := obs.Meta["bytecode_hex"].(string)
		if hexCode == "" {
			continue
		}
		code, err := hex.DecodeString(hexCode)
		if err != nil {
			return nil, "", fmt.Errorf("decode runtime bytecode observation %s: %w", id, err)
		}
		observed = id
		return code, observed, nil
	}
	return nil, "", fmt.Errorf("%w; run the code stage first", ErrCodeObservationMissing)
}

func collectMaskRanges(contract solc.Contract, recompiledSize, onchainSize int) ([]solc.Reference, []solc.Reference, error) {
	immutables := flattenImmutableReferences(contract.EVM.DeployedBytecode.ImmutableReferences)
	links := flattenLinkReferences(contract.EVM.DeployedBytecode.LinkReferences)
	for _, ref := range append(append([]solc.Reference{}, immutables...), links...) {
		if err := validateReference(ref, recompiledSize); err != nil {
			return nil, nil, fmt.Errorf("compiler reference: %w", err)
		}
		if err := validateReference(ref, onchainSize); err != nil {
			return nil, nil, fmt.Errorf("onchain bytecode too short for compiler reference: %w", err)
		}
	}
	return immutables, links, nil
}

func flattenImmutableReferences(groups map[string][]solc.Reference) []solc.Reference {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var refs []solc.Reference
	for _, key := range keys {
		refs = append(refs, groups[key]...)
	}
	return dedupeReferences(refs)
}

func flattenLinkReferences(groups map[string]map[string][]solc.Reference) []solc.Reference {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var refs []solc.Reference
	for _, key := range keys {
		libraries := groups[key]
		names := make([]string, 0, len(libraries))
		for name := range libraries {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			refs = append(refs, libraries[name]...)
		}
	}
	return dedupeReferences(refs)
}

func dedupeReferences(refs []solc.Reference) []solc.Reference {
	seen := make(map[solc.Reference]struct{}, len(refs))
	out := make([]solc.Reference, 0, len(refs))
	for _, ref := range refs {
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].Length < out[j].Length
	})
	return out
}

func validateReference(ref solc.Reference, size int) error {
	if ref.Start < 0 || ref.Length <= 0 {
		return fmt.Errorf("invalid range start=%d length=%d", ref.Start, ref.Length)
	}
	if ref.Start+ref.Length > size {
		return fmt.Errorf("range [%d,%d) exceeds %d bytes", ref.Start, ref.Start+ref.Length, size)
	}
	return nil
}

func maskBytecode(code []byte, groups ...[]solc.Reference) []byte {
	masked := append([]byte(nil), code...)
	for _, refs := range groups {
		for _, ref := range refs {
			for i := ref.Start; i < ref.Start+ref.Length && i < len(masked); i++ {
				masked[i] = 0
			}
		}
	}
	return masked
}

func compareBytecode(onchain, recompiled []byte) (bool, int, *int) {
	compared := len(onchain)
	if len(recompiled) < compared {
		compared = len(recompiled)
	}
	for i := 0; i < compared; i++ {
		if onchain[i] != recompiled[i] {
			offset := i
			return false, compared, &offset
		}
	}
	if len(onchain) != len(recompiled) {
		offset := compared
		return false, compared, &offset
	}
	return true, compared, nil
}

func findContractBySize(output solc.Output, size int) (solc.Contract, string, error) {
	files := make([]string, 0, len(output.Contracts))
	for file := range output.Contracts {
		files = append(files, file)
	}
	sort.Strings(files)
	type candidate struct {
		contract solc.Contract
		file     string
		name     string
	}
	var candidates []candidate
	for _, file := range files {
		names := make([]string, 0, len(output.Contracts[file]))
		for name := range output.Contracts[file] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			contract := output.Contracts[file][name]
			code, err := contract.Deployed()
			if err == nil && len(code) == size {
				candidates = append(candidates, candidate{contract: contract, file: file, name: name})
			}
		}
	}
	if len(candidates) != 1 {
		return solc.Contract{}, "", fmt.Errorf("cannot identify recompiled contract: %d candidates with %d runtime bytes", len(candidates), size)
	}
	return candidates[0].contract, candidates[0].file + ":" + candidates[0].name, nil
}

func saveArtifact(st *store.Store, kind, rel string, data []byte) (model.Artifact, error) {
	if _, err := st.WriteFile(rel, data); err != nil {
		return model.Artifact{}, fmt.Errorf("write artifact %s: %w", rel, err)
	}
	return model.Artifact{Kind: kind, Path: rel, Hash: model.ContentHash(data)}, nil
}

func writeBuildObservation(params ProvenanceParams, manifest model.Manifest, binary solc.Binary, input []byte, target compilationTarget, output solc.Output, build model.BuildEvidence, buildErr error) (model.Observation, error) {
	response := output.Raw
	if len(response) == 0 {
		response = []byte(buildErrText(buildErr))
	}
	meta := map[string]any{
		"compiler_version":       binary.Version,
		"compiler_sha256":        binary.SHA256,
		"compiler_source":        binary.SourceURL,
		"target_source":          target.Source,
		"target_contract":        target.Name,
		"match":                  build.Match,
		"immutables_masked":      build.ImmutablesMasked,
		"link_references_masked": build.LinkReferencesMasked,
		"onchain_bytes":          build.OnchainBytes,
		"recompiled_bytes":       build.RecompiledBytes,
		"compared_bytes":         build.ComparedBytes,
		"diagnostics":            len(output.Errors),
	}
	if build.MismatchOffset != nil {
		meta["mismatch_offset"] = *build.MismatchOffset
	}
	obs := model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "build",
		RunID:         params.RunID,
		Request: map[string]any{
			"compiler_version": binary.Version,
			"compiler_sha256":  binary.SHA256,
			"input_hash":       model.ContentHash(input),
			"block_number":     manifest.Block.Number,
			"block_hash":       manifest.Block.Hash,
		},
		ResponseHash: model.ContentHash(response),
		Block:        &manifest.Block,
		RetrievedAt:  time.Now().UTC(),
		Source:       model.SourceRef{Provider: "solc", Version: binary.Version, EndpointClass: "local"},
		Meta:         meta,
	}
	if buildErr != nil {
		obs.Error = &model.ObservationError{Code: "compilation_failed", Message: truncateText(buildErr.Error(), 1000)}
	}
	return params.Store.WriteObservation(obs)
}

func buildErrText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func removeMatching(values []string, prefix string) []string {
	out := values[:0]
	for _, value := range values {
		if !strings.HasPrefix(value, prefix) {
			out = append(out, value)
		}
	}
	return out
}

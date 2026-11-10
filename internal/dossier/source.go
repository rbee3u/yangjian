package dossier

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/source"
	"github.com/rbee3u/yangjian/internal/store"
)

var ErrDossierNotInitialized = errors.New("dossier not initialized")

type SourceFetcher interface {
	Fetch(ctx context.Context, chainID int64, address string) (source.Record, error)
}

type SourceParams struct {
	ChainID int64
	Address common.Address
	Store   *store.Store
	RunID   string
	Fetcher SourceFetcher
}

type SourceResult struct {
	SourceRevisionID     string   `json:"source_revision_id"`
	RegistryMatch        string   `json:"registry_match"`
	Compiler             string   `json:"compiler"`
	SourceCount          int      `json:"source_count"`
	SourceHashMismatches int      `json:"source_hash_mismatches"`
	MissingSources       int      `json:"missing_sources"`
	ProvenanceLevel      string   `json:"provenance_level"`
	Correspondence       string   `json:"correspondence"`
	BlockNumber          uint64   `json:"block_number"`
	BlockHash            string   `json:"block_hash"`
	ObservationIDs       []string `json:"observation_ids"`
}

func RunSource(ctx context.Context, params SourceParams) (SourceResult, error) {
	if params.Store == nil {
		return SourceResult{}, fmt.Errorf("store is nil")
	}
	if params.Fetcher == nil {
		return SourceResult{}, fmt.Errorf("source fetcher is nil")
	}
	if params.RunID == "" {
		return SourceResult{}, fmt.Errorf("run id is required")
	}
	address, err := model.NormalizeAddress(params.Address.Hex())
	if err != nil {
		return SourceResult{}, err
	}

	var manifest model.Manifest
	if err := params.Store.ReadJSON("manifest.json", &manifest); err != nil {
		return SourceResult{}, fmt.Errorf("%w: read manifest: %v", ErrDossierNotInitialized, err)
	}
	versionPath := ""
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == "contract_version" {
			versionPath = artifact.Path
		}
	}
	if versionPath == "" {
		return SourceResult{}, fmt.Errorf("%w: contract version artifact not found", ErrDossierNotInitialized)
	}
	var contractVersion model.ContractVersion
	if err := params.Store.ReadJSON(versionPath, &contractVersion); err != nil {
		return SourceResult{}, fmt.Errorf("read contract version: %w", err)
	}

	record, err := params.Fetcher.Fetch(ctx, params.ChainID, address)
	if err != nil {
		return SourceResult{}, fmt.Errorf("fetch sourcify record: %w", err)
	}
	if record.ChainID != 0 && int64(record.ChainID) != params.ChainID {
		return SourceResult{}, fmt.Errorf("sourcify chain id %d does not match requested chain %d", record.ChainID, params.ChainID)
	}
	if record.Address != "" && common.HexToAddress(record.Address) != params.Address {
		return SourceResult{}, fmt.Errorf("sourcify address %s does not match requested address %s", record.Address, address)
	}

	now := time.Now().UTC()
	match := record.Match
	if match == "" {
		match = "unknown"
	}

	obs, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "source_fetch",
		RunID:         params.RunID,
		Request: map[string]any{
			"provider": "sourcify",
			"chain_id": params.ChainID,
			"address":  address,
			"fields":   "all",
		},
		ResponseHash: model.ContentHash(record.Raw),
		Block: &model.BlockRef{
			ChainID: params.ChainID,
			Number:  contractVersion.ValidFromBlock,
			Hash:    manifest.Block.Hash,
		},
		RetrievedAt: now,
		Source:      model.SourceRef{Provider: "sourcify", Version: "v2", EndpointClass: "registry"},
		Meta: map[string]any{
			"match":          record.Match,
			"creation_match": record.CreationMatch,
			"runtime_match":  record.RuntimeMatch,
			"verified_at":    record.VerifiedAt,
		},
	})
	if err != nil {
		return SourceResult{}, fmt.Errorf("write source observation: %w", err)
	}

	artifacts := make([]model.Artifact, 0, 6)
	writeArtifact := func(kind, rel string, data []byte) error {
		if len(data) == 0 || string(data) == "null" {
			return nil
		}
		if _, err := params.Store.WriteFile(rel, data); err != nil {
			return err
		}
		artifacts = append(artifacts, model.Artifact{
			Kind: kind,
			Path: rel,
			Hash: model.ContentHash(data),
		})
		return nil
	}
	if err := writeArtifact("sourcify_response", "artifacts/sourcify-response.json", record.Raw); err != nil {
		return SourceResult{}, err
	}
	if err := writeArtifact("abi", "artifacts/abi.json", record.ABI); err != nil {
		return SourceResult{}, err
	}
	if err := writeArtifact("metadata", "artifacts/metadata.json", record.Metadata); err != nil {
		return SourceResult{}, err
	}
	if err := writeArtifact("std_json_input", "artifacts/stdjsoninput.json", record.StdJSONInput); err != nil {
		return SourceResult{}, err
	}
	if err := writeArtifact("storage_layout", "artifacts/storagelayout.json", record.StorageLayout); err != nil {
		return SourceResult{}, err
	}

	metadata, err := record.ParseMetadata()
	if err != nil {
		return SourceResult{}, err
	}
	contents := make(map[string]string)
	for path, file := range record.Sources {
		contents[path] = file.Content
	}
	for path, file := range record.ParseStdJSONInputSources() {
		if _, ok := contents[path]; !ok {
			contents[path] = file.Content
		}
	}

	paths := make([]string, 0, len(metadata.Sources))
	for path := range metadata.Sources {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	sourceFiles := make([]model.SourceFile, 0, len(paths))
	mismatches := 0
	missing := 0
	for _, path := range paths {
		sourceMeta := metadata.Sources[path]
		content := contents[path]
		if content == "" {
			content = contents["./"+strings.TrimPrefix(path, "./")]
		}
		expected := strings.ToLower(strings.TrimPrefix(sourceMeta.Keccak256, "0x"))
		actualHash := keccakHex([]byte(content))
		if expected != actualHash {
			mismatches++
		}
		ref := ""
		if content != "" {
			rel := filepath.Join("artifacts", "sources", actualHash+".sol")
			if _, err := params.Store.WriteFile(rel, []byte(content)); err != nil {
				return SourceResult{}, err
			}
			ref = rel
		} else {
			missing++
		}
		sourceFiles = append(sourceFiles, model.SourceFile{
			Path:       path,
			Keccak256:  sourceMeta.Keccak256,
			ContentRef: ref,
		})
	}

	compilation := model.Compilation{
		Compiler:     record.Compilation.CompilerVersion,
		EVMVersion:   record.Compilation.CompilerSettings.EVMVersion,
		Remappings:   record.Compilation.CompilerSettings.Remappings,
		Libraries:    flattenLibraries(record.Compilation.CompilerSettings.Libraries),
		BytecodeHash: record.Compilation.CompilerSettings.Metadata.BytecodeHash,
	}
	if record.Compilation.CompilerSettings.Optimizer.Enabled {
		runs := record.Compilation.CompilerSettings.Optimizer.Runs
		compilation.OptimizerRuns = &runs
	}

	canonical, err := json.Marshal(struct {
		Sources       []model.SourceFile `json:"sources"`
		Compilation   model.Compilation  `json:"compilation"`
		RegistryMatch string             `json:"registry_match"`
	}{
		Sources:       sourceFiles,
		Compilation:   compilation,
		RegistryMatch: match,
	})
	if err != nil {
		return SourceResult{}, fmt.Errorf("canonicalize source revision: %w", err)
	}
	sourceRevisionID := model.SourceRevisionID(canonical)
	sourceRevision := model.SourceRevision{
		SchemaVersion:   model.SchemaVersion,
		ID:              sourceRevisionID,
		Sources:         sourceFiles,
		MetadataRef:     "artifacts/metadata.json",
		StdJSONInputRef: "artifacts/stdjsoninput.json",
		Compilation:     compilation,
		RegistryMatch:   match,
		ProvenanceLevel: "P0",
		Correspondence:  "unproven",
		RegistrySources: []model.RegistrySource{{
			Provider:    "sourcify",
			Address:     address,
			RetrievedAt: now,
		}},
		EvidenceRefs: []string{obs.ID},
	}
	sourceRel := filepath.Join("entities", "sources", strings.TrimPrefix(sourceRevisionID, "sha256:")+".json")
	var existing model.SourceRevision
	if err := params.Store.ReadJSON(sourceRel, &existing); err == nil && existing.ID == sourceRevisionID {
		switch {
		case existing.Build != nil:
			sourceRevision.Build = existing.Build
			sourceRevision.ProvenanceLevel = existing.ProvenanceLevel
			sourceRevision.Correspondence = existing.Correspondence
			sourceRevision.EvidenceRefs = appendUnique(existing.EvidenceRefs, obs.ID)
		case existing.ProvenanceLevel == "P1" && existing.Correspondence == "proven":
			sourceRevision.ProvenanceLevel = existing.ProvenanceLevel
			sourceRevision.Correspondence = existing.Correspondence
			sourceRevision.EvidenceRefs = appendUnique(existing.EvidenceRefs, obs.ID)
		}
	}
	if _, err := params.Store.WriteJSON(sourceRel, sourceRevision); err != nil {
		return SourceResult{}, fmt.Errorf("write source revision: %w", err)
	}

	contractVersion.SourceRevisionID = sourceRevisionID
	contractVersion.ABIHash = model.ContentHash(record.ABI)
	_, versionHash, err := params.Store.WriteJSONHashed(versionPath, contractVersion)
	if err != nil {
		return SourceResult{}, fmt.Errorf("update contract version: %w", err)
	}
	manifest.Artifacts = appendUniqueArtifact(manifest.Artifacts, model.Artifact{
		Kind: "contract_version",
		Path: versionPath,
		Hash: versionHash,
	})

	manifest.ObservationIDs = appendUnique(manifest.ObservationIDs, obs.ID)
	manifest.SourceRevisionIDs = appendUnique(manifest.SourceRevisionIDs, sourceRevisionID)
	for _, artifact := range artifacts {
		manifest.Artifacts = appendUniqueArtifact(manifest.Artifacts, artifact)
	}
	manifest.OpenQuestions = removeString(manifest.OpenQuestions, "source provenance not resolved")
	manifest.OpenQuestions = removeMatching(manifest.OpenQuestions, "P1 metadata binding failed")
	if sourceRevision.Build == nil {
		manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, "local P2 rebuild not attempted")
		if compilation.BytecodeHash == "none" {
			manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, "P1 unavailable: metadata.bytecodeHash=none; P2 required")
			manifest.OpenQuestions = removeString(manifest.OpenQuestions, "P1 metadata binding not attempted")
		} else if sourceRevision.ProvenanceLevel != "P1" {
			manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, "P1 metadata binding not attempted")
		} else {
			manifest.OpenQuestions = removeString(manifest.OpenQuestions, "P1 metadata binding not attempted")
		}
	} else if sourceRevision.Build.Match {
		manifest.OpenQuestions = removeString(manifest.OpenQuestions, "local P2 rebuild not attempted")
		manifest.OpenQuestions = removeString(manifest.OpenQuestions, "P1 unavailable: metadata.bytecodeHash=none; P2 required")
		manifest.OpenQuestions = removeMatching(manifest.OpenQuestions, "P2 compilation failed")
		manifest.OpenQuestions = removeMatching(manifest.OpenQuestions, "P2 rebuild did not match")
	}
	if mismatches > 0 {
		manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, fmt.Sprintf("%d source hash mismatches", mismatches))
	}
	if missing > 0 {
		manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, fmt.Sprintf("%d source contents missing", missing))
	}
	manifest.Status = "partial"
	manifest.UpdatedAt = now
	if _, err := params.Store.WriteJSON("manifest.json", manifest); err != nil {
		return SourceResult{}, fmt.Errorf("update manifest: %w", err)
	}

	return SourceResult{
		SourceRevisionID:     sourceRevisionID,
		RegistryMatch:        match,
		Compiler:             compilation.Compiler,
		SourceCount:          len(sourceFiles),
		SourceHashMismatches: mismatches,
		MissingSources:       missing,
		ProvenanceLevel:      sourceRevision.ProvenanceLevel,
		Correspondence:       sourceRevision.Correspondence,
		BlockNumber:          contractVersion.ValidFromBlock,
		BlockHash:            manifest.Block.Hash,
		ObservationIDs:       []string{obs.ID},
	}, nil
}

func keccakHex(data []byte) string {
	return hex.EncodeToString(crypto.Keccak256(data))
}

func flattenLibraries(libraries map[string]any) map[string]string {
	if len(libraries) == 0 {
		return nil
	}
	out := make(map[string]string)
	for key, value := range libraries {
		if text, ok := value.(string); ok {
			out[key] = text
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func removeString(values []string, value string) []string {
	out := values[:0]
	for _, existing := range values {
		if existing != value {
			out = append(out, existing)
		}
	}
	return out
}

func appendUniqueArtifact(values []model.Artifact, artifact model.Artifact) []model.Artifact {
	for i, existing := range values {
		if existing.Path == artifact.Path {
			values[i] = artifact
			return values
		}
	}
	return append(values, artifact)
}

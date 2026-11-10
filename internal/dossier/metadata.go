package dossier

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

type p1Result struct {
	Attempted    bool
	Matched      bool
	Algorithm    string
	MetadataHash string
	EmbeddedHash string
	SourceFiles  int
	Reason       string
}

type metadataDocument struct {
	Compiler struct {
		Version string `json:"version"`
	} `json:"compiler"`
	Settings struct {
		Optimizer struct {
			Enabled bool `json:"enabled"`
			Runs    int  `json:"runs"`
		} `json:"optimizer"`
		EVMVersion string `json:"evmVersion"`
		Metadata   struct {
			BytecodeHash string `json:"bytecodeHash"`
		} `json:"metadata"`
		Libraries  json.RawMessage `json:"libraries"`
		Remappings []string        `json:"remappings"`
	} `json:"settings"`
	Sources map[string]struct {
		Keccak256 string `json:"keccak256"`
	} `json:"sources"`
}

// assessMetadataBinding implements P1: the metadata hash embedded in the
// runtime bytecode must match the recorded metadata artifact, and the metadata
// source hashes and build settings must match the recorded source revision.
func assessMetadataBinding(revision model.SourceRevision, st *store.Store, onchain []byte) (p1Result, error) {
	algorithm := strings.ToLower(strings.TrimSpace(revision.Compilation.BytecodeHash))
	if algorithm == "" || algorithm == "none" {
		return p1Result{}, nil
	}
	result := p1Result{Attempted: true, Algorithm: algorithm}
	if revision.MetadataRef == "" {
		result.Reason = "metadata artifact is missing"
		return result, nil
	}
	raw, err := os.ReadFile(st.Path(revision.MetadataRef))
	if err != nil {
		if os.IsNotExist(err) {
			result.Reason = "metadata artifact is missing"
			return result, nil
		}
		return p1Result{}, fmt.Errorf("read metadata artifact: %w", err)
	}
	metadataHash := crypto.Keccak256(raw)
	result.MetadataHash = "0x" + hex.EncodeToString(metadataHash)
	var metadata metadataDocument
	if err := json.Unmarshal(raw, &metadata); err != nil {
		result.Reason = "metadata artifact is not valid JSON: " + truncateText(err.Error(), 200)
		return result, nil
	}

	reasons := make([]string, 0)
	embedded, err := extractEmbeddedMetadataHash(onchain, algorithm)
	if err != nil {
		reasons = append(reasons, err.Error())
	} else {
		result.EmbeddedHash = "0x" + hex.EncodeToString(embedded)
		if !bytes.Equal(embedded, metadataHash) {
			reasons = append(reasons, "embedded metadata hash does not match the metadata artifact")
		}
	}

	matchedSources, sourceReason := compareMetadataSources(metadata, revision)
	result.SourceFiles = matchedSources
	if sourceReason != "" {
		reasons = append(reasons, sourceReason)
	}
	if settingReason := compareMetadataSettings(metadata, revision); settingReason != "" {
		reasons = append(reasons, settingReason)
	}

	if len(reasons) == 0 {
		result.Matched = true
		return result, nil
	}
	result.Reason = strings.Join(reasons, "; ")
	return result, nil
}

func compareMetadataSources(metadata metadataDocument, revision model.SourceRevision) (int, string) {
	if len(metadata.Sources) == 0 {
		return 0, "metadata lists no sources"
	}
	available := make(map[string]string, len(revision.Sources))
	for _, file := range revision.Sources {
		hash := normalizeKeccakHex(file.Keccak256)
		for _, key := range uniqueSourceCandidates(file.Path) {
			available[key] = hash
		}
	}
	matched := 0
	mismatches := make([]string, 0)
	for path, source := range metadata.Sources {
		expected := normalizeKeccakHex(source.Keccak256)
		actual, ok := "", false
		for _, key := range uniqueSourceCandidates(path) {
			if hash, found := available[key]; found {
				actual, ok = hash, true
				break
			}
		}
		switch {
		case expected == "":
			mismatches = append(mismatches, path+" has no metadata hash")
		case !ok:
			mismatches = append(mismatches, path+" is missing from the source revision")
		case actual != expected:
			mismatches = append(mismatches, path+" hash differs")
		default:
			matched++
		}
	}
	if len(mismatches) == 0 {
		return matched, ""
	}
	sort.Strings(mismatches)
	if len(mismatches) > 3 {
		mismatches = mismatches[:3]
	}
	return matched, fmt.Sprintf("source hashes do not match metadata (%s)", strings.Join(mismatches, ", "))
}

func compareMetadataSettings(metadata metadataDocument, revision model.SourceRevision) string {
	mismatches := make([]string, 0)
	if metadata.Compiler.Version != "" && metadata.Compiler.Version != revision.Compilation.Compiler {
		mismatches = append(mismatches, fmt.Sprintf("compiler %q != %q", metadata.Compiler.Version, revision.Compilation.Compiler))
	}
	optimizerEnabled := revision.Compilation.OptimizerRuns != nil
	if metadata.Settings.Optimizer.Enabled != optimizerEnabled {
		mismatches = append(mismatches, "optimizer enabled flag differs")
	} else if optimizerEnabled && metadata.Settings.Optimizer.Runs != *revision.Compilation.OptimizerRuns {
		mismatches = append(mismatches, fmt.Sprintf("optimizer runs %d != %d", metadata.Settings.Optimizer.Runs, *revision.Compilation.OptimizerRuns))
	}
	if metadata.Settings.EVMVersion != "" && metadata.Settings.EVMVersion != revision.Compilation.EVMVersion {
		mismatches = append(mismatches, fmt.Sprintf("EVM version %q != %q", metadata.Settings.EVMVersion, revision.Compilation.EVMVersion))
	}
	if metadata.Settings.Metadata.BytecodeHash != "" && metadata.Settings.Metadata.BytecodeHash != revision.Compilation.BytecodeHash {
		mismatches = append(mismatches, fmt.Sprintf("bytecodeHash %q != %q", metadata.Settings.Metadata.BytecodeHash, revision.Compilation.BytecodeHash))
	}
	if !reflect.DeepEqual(sortedStrings(metadata.Settings.Remappings), sortedStrings(revision.Compilation.Remappings)) {
		mismatches = append(mismatches, "remappings differ")
	}
	if !reflect.DeepEqual(flattenMetadataLibraries(metadata.Settings.Libraries), revision.Compilation.Libraries) {
		mismatches = append(mismatches, "libraries differ")
	}
	if len(mismatches) == 0 {
		return ""
	}
	return "build settings differ: " + strings.Join(mismatches, ", ")
}

func flattenMetadataLibraries(raw json.RawMessage) map[string]string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var nested map[string]any
	if err := json.Unmarshal(raw, &nested); err != nil {
		return nil
	}
	flattened := make(map[string]string)
	for key, value := range nested {
		switch typed := value.(type) {
		case string:
			flattened[key] = typed
		case map[string]any:
			for name, address := range typed {
				if text, ok := address.(string); ok {
					flattened[key+":"+name] = text
				}
			}
		}
	}
	if len(flattened) == 0 {
		return nil
	}
	return flattened
}

func sortedStrings(values []string) []string {
	sorted := append([]string{}, values...)
	sort.Strings(sorted)
	return sorted
}

func normalizeKeccakHex(value string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(value), "0x"))
}

func extractEmbeddedMetadataHash(code []byte, algorithm string) ([]byte, error) {
	if len(code) < 4 {
		return nil, fmt.Errorf("runtime bytecode is too short for metadata")
	}
	length := int(code[len(code)-2])<<8 | int(code[len(code)-1])
	if length <= 0 || length+2 > len(code) {
		return nil, fmt.Errorf("runtime bytecode has an invalid metadata length %d", length)
	}
	cbor := code[len(code)-2-length : len(code)-2]
	var pattern []byte
	switch algorithm {
	case "ipfs":
		pattern = []byte{0x64, 'i', 'p', 'f', 's', 0x58, 0x22, 0x12, 0x20}
	case "bzzr1":
		pattern = []byte{0x65, 'b', 'z', 'z', 'r', '1', 0x58, 0x20}
	case "bzzr0":
		pattern = []byte{0x65, 'b', 'z', 'z', 'r', '0', 0x58, 0x20}
	default:
		return nil, fmt.Errorf("unsupported metadata.bytecodeHash %q", algorithm)
	}
	index := bytes.Index(cbor, pattern)
	if index < 0 {
		return nil, fmt.Errorf("runtime bytecode does not contain a %s metadata hash", algorithm)
	}
	start := index + len(pattern)
	if len(cbor) < start+32 {
		return nil, fmt.Errorf("runtime metadata is truncated")
	}
	return cbor[start : start+32], nil
}

func writeMetadataBindingObservation(params ProvenanceParams, manifest model.Manifest, revision model.SourceRevision, result p1Result, codeObservationID string) (model.Observation, error) {
	canonical, err := json.Marshal(struct {
		Matched      bool   `json:"matched"`
		Algorithm    string `json:"algorithm"`
		MetadataHash string `json:"metadata_hash"`
		EmbeddedHash string `json:"embedded_hash"`
		SourceFiles  int    `json:"source_files"`
		Reason       string `json:"reason,omitempty"`
	}{
		Matched:      result.Matched,
		Algorithm:    result.Algorithm,
		MetadataHash: result.MetadataHash,
		EmbeddedHash: result.EmbeddedHash,
		SourceFiles:  result.SourceFiles,
		Reason:       result.Reason,
	})
	if err != nil {
		return model.Observation{}, fmt.Errorf("encode metadata binding result: %w", err)
	}
	observation, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "metadata_binding",
		RunID:         params.RunID,
		Request: map[string]any{
			"method":              "metadata_binding",
			"source_revision_id":  revision.ID,
			"metadata_ref":        revision.MetadataRef,
			"code_observation_id": codeObservationID,
			"block_number":        manifest.Block.Number,
			"block_hash":          manifest.Block.Hash,
			"algorithm":           result.Algorithm,
		},
		ResponseHash: model.ContentHash(canonical),
		Block:        &manifest.Block,
		RetrievedAt:  time.Now().UTC(),
		Source:       model.SourceRef{Provider: "yangjian", Version: "1", EndpointClass: "local"},
		Meta: map[string]any{
			"matched":       result.Matched,
			"metadata_hash": result.MetadataHash,
			"embedded_hash": result.EmbeddedHash,
			"source_files":  result.SourceFiles,
			"reason":        result.Reason,
		},
	})
	if err != nil {
		return model.Observation{}, fmt.Errorf("write metadata binding observation: %w", err)
	}
	return observation, nil
}

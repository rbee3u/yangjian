package dossier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

type ProxyStorage interface {
	StorageAt(ctx context.Context, account common.Address, slot common.Hash, blockNumber *big.Int) ([]byte, error)
}

type AnalyzeParams struct {
	ChainID int64
	Address common.Address
	Store   *store.Store
	RunID   string
	Reader  ProxyStorage
}

type AnalyzeResult struct {
	ProxyType             string         `json:"proxy_type"`
	ImplementationAddress string         `json:"implementation_address,omitempty"`
	BeaconAddress         string         `json:"beacon_address,omitempty"`
	AdminAddress          string         `json:"admin_address,omitempty"`
	Warnings              []string       `json:"warnings,omitempty"`
	ObservationIDs        []string       `json:"observation_ids"`
	Artifact              string         `json:"artifact"`
	Static                *StaticSummary `json:"static,omitempty"`
	StaticUnavailable     []string       `json:"static_unavailable,omitempty"`
}

type StaticSummary struct {
	CompilationTarget string   `json:"compilation_target"`
	Contracts         int      `json:"contracts"`
	Linearization     []string `json:"linearization"`
	StorageEntries    int      `json:"storage_entries"`
	Immutables        []string `json:"immutables"`
	Methods           int      `json:"methods"`
	CallSites         int      `json:"call_sites"`
	Delegatecalls     int      `json:"delegatecalls"`
	LibraryDelegate   int      `json:"library_delegatecalls"`
	ExternalCalls     int      `json:"external_calls"`
	Callbacks         int      `json:"callbacks"`
	Artifact          string   `json:"artifact"`
	Warnings          []string `json:"warnings,omitempty"`
}

type proxySlot struct {
	name string
	hash common.Hash
}

var proxySlots = []proxySlot{
	{"eip1967.implementation", eip1967Slot("eip1967.proxy.implementation")},
	{"eip1967.beacon", eip1967Slot("eip1967.proxy.beacon")},
	{"eip1967.admin", eip1967Slot("eip1967.proxy.admin")},
	{"eip1822.proxiable", crypto.Keccak256Hash([]byte("PROXIABLE"))},
}

var (
	minimalProxyPrefix = []byte{0x36, 0x3d, 0x3d, 0x37, 0x3d, 0x3d, 0x3d, 0x36, 0x3d, 0x73}
	minimalProxySuffix = []byte{0x5a, 0xf4, 0x3d, 0x82, 0x80, 0x3e, 0x90, 0x3d, 0x91, 0x60, 0x2b, 0x57, 0xfd, 0x5b, 0xf3}
)

func RunAnalyze(ctx context.Context, params AnalyzeParams) (AnalyzeResult, error) {
	if params.Store == nil {
		return AnalyzeResult{}, fmt.Errorf("store is nil")
	}
	if params.Reader == nil {
		return AnalyzeResult{}, fmt.Errorf("proxy storage reader is nil")
	}
	if params.RunID == "" {
		return AnalyzeResult{}, fmt.Errorf("run id is required")
	}
	address, err := model.NormalizeAddress(params.Address.Hex())
	if err != nil {
		return AnalyzeResult{}, err
	}
	var manifest model.Manifest
	if err := params.Store.ReadJSON("manifest.json", &manifest); err != nil {
		return AnalyzeResult{}, fmt.Errorf("%w: read manifest: %v", ErrDossierNotInitialized, err)
	}
	version, versionPath, err := loadCurrentVersion(manifest, params.Store)
	if err != nil {
		return AnalyzeResult{}, err
	}
	code, codeObservationID, err := findOnchainCode(manifest, params.Store)
	if err != nil {
		return AnalyzeResult{}, err
	}
	blockNumber := new(big.Int).SetUint64(manifest.Block.Number)

	values := make(map[string]common.Hash, len(proxySlots))
	observed := make(map[string]string, len(proxySlots))
	observations := make([]string, 0, len(proxySlots)+2)
	for _, slot := range proxySlots {
		raw, err := params.Reader.StorageAt(ctx, params.Address, slot.hash, blockNumber)
		if err != nil {
			return AnalyzeResult{}, fmt.Errorf("read %s slot: %w", slot.name, err)
		}
		word := common.BytesToHash(raw)
		values[slot.name] = word
		observation, err := params.Store.WriteObservation(model.Observation{
			SchemaVersion: model.SchemaVersion,
			Kind:          "rpc_call",
			RunID:         params.RunID,
			Request: map[string]any{
				"method": "eth_getStorageAt",
				"params": []any{
					strings.ToLower(address),
					slot.hash.Hex(),
					fmt.Sprintf("0x%x", manifest.Block.Number),
				},
				"slot_name": slot.name,
			},
			ResponseHash: model.ContentHash(word.Bytes()),
			Block:        &manifest.Block,
			RetrievedAt:  time.Now().UTC(),
			Source:       model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
			Meta: map[string]any{
				"slot_name": slot.name,
				"slot":      slot.hash.Hex(),
				"value":     word.Hex(),
			},
		})
		if err != nil {
			return AnalyzeResult{}, fmt.Errorf("write %s observation: %w", slot.name, err)
		}
		observed[slot.name] = observation.ID
		observations = append(observations, observation.ID)
	}

	proxyType, implementation, beacon, admin, warnings := detectProxy(code, values)
	artifact := proxyArtifact{
		SchemaVersion:         model.SchemaVersion,
		ChainID:               params.ChainID,
		Address:               address,
		Block:                 manifest.Block,
		ProxyType:             proxyType,
		ImplementationAddress: implementation,
		BeaconAddress:         beacon,
		AdminAddress:          admin,
		Slots:                 make(map[string]string, len(values)),
		CodeObservationID:     codeObservationID,
		ObservationIDs:        append([]string{}, observations...),
		Warnings:              warnings,
		RetrievedAt:           time.Now().UTC(),
	}
	for name, value := range values {
		artifact.Slots[name] = value.Hex()
	}
	artifactData, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("encode proxy artifact: %w", err)
	}
	saved, err := saveArtifact(params.Store, "proxy", "artifacts/proxy.json", append(artifactData, '\n'))
	if err != nil {
		return AnalyzeResult{}, err
	}

	canonical, err := json.Marshal(struct {
		ProxyType             string            `json:"proxy_type"`
		ImplementationAddress string            `json:"implementation_address,omitempty"`
		BeaconAddress         string            `json:"beacon_address,omitempty"`
		AdminAddress          string            `json:"admin_address,omitempty"`
		Slots                 map[string]string `json:"slots"`
		Warnings              []string          `json:"warnings,omitempty"`
	}{
		ProxyType:             proxyType,
		ImplementationAddress: implementation,
		BeaconAddress:         beacon,
		AdminAddress:          admin,
		Slots:                 artifact.Slots,
		Warnings:              warnings,
	})
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("encode proxy probe result: %w", err)
	}
	probe, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "proxy_probe",
		RunID:         params.RunID,
		Request: map[string]any{
			"method":              "proxy_probe",
			"slots":               slotNames(),
			"code_observation_id": codeObservationID,
			"block_number":        manifest.Block.Number,
			"block_hash":          manifest.Block.Hash,
		},
		ResponseHash: model.ContentHash(canonical),
		Block:        &manifest.Block,
		RetrievedAt:  time.Now().UTC(),
		Source:       model.SourceRef{Provider: "yangjian", Version: "1", EndpointClass: "local"},
		Meta: map[string]any{
			"proxy_type":             proxyType,
			"implementation_address": implementation,
			"beacon_address":         beacon,
			"admin_address":          admin,
			"slot_observation_ids":   observed,
			"warnings":               warnings,
		},
	})
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("write proxy probe observation: %w", err)
	}
	observations = append(observations, probe.ID)

	staticAnalysis, staticRevision, staticRevisionPath, staticReasons, err := loadStaticForAnalyze(params.Store, manifest)
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("static analysis: %w", err)
	}
	var staticArtifact model.Artifact
	if staticAnalysis != nil {
		staticAnalysis.Artifact = staticArtifactRel
		staticData, err := json.MarshalIndent(staticAnalysis, "", "  ")
		if err != nil {
			return AnalyzeResult{}, fmt.Errorf("encode static artifact: %w", err)
		}
		staticData = append(staticData, '\n')
		staticArtifact, err = saveArtifact(params.Store, "static_analysis", staticArtifactRel, staticData)
		if err != nil {
			return AnalyzeResult{}, err
		}
		staticObservation, err := params.Store.WriteObservation(model.Observation{
			SchemaVersion: model.SchemaVersion,
			Kind:          "static_analysis",
			RunID:         params.RunID,
			Request: map[string]any{
				"method":             "static_analysis",
				"source_revision_id": staticAnalysis.SourceRevisionID,
				"solc_output_ref":    staticRevision.Build.SolcOutputRef,
				"compilation_target": staticAnalysis.CompilationTarget,
				"block_number":       manifest.Block.Number,
				"block_hash":         manifest.Block.Hash,
			},
			ResponseHash: model.ContentHash(staticData),
			Block:        &manifest.Block,
			RetrievedAt:  time.Now().UTC(),
			Source:       model.SourceRef{Provider: "yangjian", Version: "1", EndpointClass: "local"},
			Meta: map[string]any{
				"artifact":        staticArtifact.Path,
				"contracts":       len(staticAnalysis.Inheritance),
				"storage_entries": len(staticAnalysis.Storage),
				"immutables":      len(staticAnalysis.Immutables),
				"methods":         len(staticAnalysis.Methods),
				"call_sites":      len(staticAnalysis.CallSites),
				"delegatecalls":   staticAnalysis.Counts.Delegatecall,
				"linearization":   staticAnalysis.Linearization,
			},
		})
		if err != nil {
			return AnalyzeResult{}, fmt.Errorf("write static analysis observation: %w", err)
		}
		observations = append(observations, staticObservation.ID)
		if staticRevision != nil {
			staticRevision.EvidenceRefs = appendUnique(staticRevision.EvidenceRefs, staticObservation.ID)
			if _, err := params.Store.WriteJSON(staticRevisionPath, staticRevision); err != nil {
				return AnalyzeResult{}, fmt.Errorf("update source revision: %w", err)
			}
		}
	}

	version.ProxyType = proxyType
	version.ImplementationAddress = implementation
	version.EvidenceRefs = appendUnique(version.EvidenceRefs, probe.ID)
	_, versionHash, err := params.Store.WriteJSONHashed(versionPath, version)
	if err != nil {
		return AnalyzeResult{}, fmt.Errorf("update contract version: %w", err)
	}
	manifest.Artifacts = appendUniqueArtifact(manifest.Artifacts, model.Artifact{
		Kind: "contract_version",
		Path: versionPath,
		Hash: versionHash,
	})

	for _, id := range observations {
		manifest.ObservationIDs = appendUnique(manifest.ObservationIDs, id)
	}
	manifest.Artifacts = appendUniqueArtifact(manifest.Artifacts, saved)
	if staticArtifact.Path != "" {
		manifest.Artifacts = appendUniqueArtifact(manifest.Artifacts, staticArtifact)
	}
	manifest.OpenQuestions = removeString(manifest.OpenQuestions, "proxy detection not run")
	manifest.OpenQuestions = removeString(manifest.OpenQuestions, "static analysis not run")
	manifest.OpenQuestions = removeMatching(manifest.OpenQuestions, "static analysis unavailable")
	manifest.OpenQuestions = removeMatching(manifest.OpenQuestions, "static analysis warning")
	if proxyType == "beacon" {
		manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, "beacon proxy implementation not resolved")
	}
	for _, warning := range warnings {
		manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, "proxy probe warning: "+warning)
	}
	if staticAnalysis != nil {
		for _, warning := range staticAnalysis.Warnings {
			manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, "static analysis warning: "+warning)
		}
	} else {
		for _, reason := range staticReasons {
			manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, "static analysis unavailable: "+reason)
		}
	}
	manifest.Status = "partial"
	manifest.UpdatedAt = time.Now().UTC()
	if _, err := params.Store.WriteJSON("manifest.json", manifest); err != nil {
		return AnalyzeResult{}, fmt.Errorf("update manifest: %w", err)
	}

	result := AnalyzeResult{
		ProxyType:             proxyType,
		ImplementationAddress: implementation,
		BeaconAddress:         beacon,
		AdminAddress:          admin,
		Warnings:              warnings,
		ObservationIDs:        observations,
		Artifact:              saved.Path,
		StaticUnavailable:     staticReasons,
	}
	if staticAnalysis != nil {
		result.Static = summarizeStatic(staticAnalysis)
	}
	return result, nil
}

func loadStaticForAnalyze(st *store.Store, manifest model.Manifest) (*StaticAnalysis, *model.SourceRevision, string, []string, error) {
	_, revision, revisionPath, err := loadSourceRevision(st)
	if err != nil {
		if errors.Is(err, ErrSourceRevisionMissing) {
			return nil, nil, "", []string{"source revision is missing"}, nil
		}
		return nil, nil, "", nil, err
	}
	analysis, reasons, err := buildStaticAnalysis(st, manifest, &revision)
	if err != nil {
		return nil, nil, "", nil, err
	}
	return analysis, &revision, revisionPath, reasons, nil
}

func summarizeStatic(analysis *StaticAnalysis) *StaticSummary {
	immutables := make([]string, 0, len(analysis.Immutables))
	for _, immutable := range analysis.Immutables {
		immutables = append(immutables, immutable.Name)
	}
	return &StaticSummary{
		CompilationTarget: analysis.CompilationTarget,
		Contracts:         len(analysis.Inheritance),
		Linearization:     analysis.Linearization,
		StorageEntries:    len(analysis.Storage),
		Immutables:        immutables,
		Methods:           len(analysis.Methods),
		CallSites:         len(analysis.CallSites),
		Delegatecalls:     analysis.Counts.Delegatecall,
		LibraryDelegate:   analysis.Counts.LibraryDelegatecall,
		ExternalCalls:     analysis.Counts.External,
		Callbacks:         analysis.Counts.Callback,
		Artifact:          analysis.Artifact,
		Warnings:          analysis.Warnings,
	}
}

func detectProxy(code []byte, slots map[string]common.Hash) (string, string, string, string, []string) {
	proxyType := "none"
	implementation := ""
	beacon := ""
	admin := ""
	warnings := make([]string, 0)
	markers := 0

	if address, ok := minimalProxyImplementation(code); ok {
		proxyType = "EIP-1167"
		implementation = address.Hex()
		markers++
	}
	if word := slots["eip1967.implementation"]; word != (common.Hash{}) {
		markers++
		if isCanonicalAddressWord(word) {
			if proxyType == "none" {
				proxyType = "EIP-1967"
				implementation = common.BytesToAddress(word.Bytes()[12:]).Hex()
			} else if proxyType == "EIP-1167" {
				warnings = append(warnings, "EIP-1967 implementation slot is also set; EIP-1167 code pattern takes precedence")
			}
		} else {
			warnings = append(warnings, "EIP-1967 implementation slot is not a canonical address word")
		}
	}
	if word := slots["eip1967.beacon"]; word != (common.Hash{}) {
		markers++
		if isCanonicalAddressWord(word) {
			beacon = common.BytesToAddress(word.Bytes()[12:]).Hex()
			if proxyType == "none" {
				proxyType = "beacon"
			}
		} else {
			warnings = append(warnings, "EIP-1967 beacon slot is not a canonical address word")
		}
	}
	if word := slots["eip1822.proxiable"]; word != (common.Hash{}) {
		markers++
		if isCanonicalAddressWord(word) {
			if proxyType == "none" {
				proxyType = "EIP-1822"
				implementation = common.BytesToAddress(word.Bytes()[12:]).Hex()
			}
		} else {
			warnings = append(warnings, "EIP-1822 PROXIABLE slot is not a canonical address word")
		}
	}
	if word := slots["eip1967.admin"]; word != (common.Hash{}) && isCanonicalAddressWord(word) {
		admin = common.BytesToAddress(word.Bytes()[12:]).Hex()
	}
	if markers > 1 {
		warnings = append(warnings, fmt.Sprintf("%d proxy markers detected; precedence is EIP-1167, EIP-1967, beacon, EIP-1822", markers))
	}
	if proxyType == "none" && markers > 0 {
		proxyType = "other"
	}
	return proxyType, implementation, beacon, admin, warnings
}

func minimalProxyImplementation(code []byte) (common.Address, bool) {
	if len(code) != len(minimalProxyPrefix)+common.AddressLength+len(minimalProxySuffix) {
		return common.Address{}, false
	}
	if !bytes.HasPrefix(code, minimalProxyPrefix) || !bytes.HasSuffix(code, minimalProxySuffix) {
		return common.Address{}, false
	}
	return common.BytesToAddress(code[len(minimalProxyPrefix) : len(minimalProxyPrefix)+common.AddressLength]), true
}

func isCanonicalAddressWord(word common.Hash) bool {
	for _, b := range word.Bytes()[:12] {
		if b != 0 {
			return false
		}
	}
	return true
}

func eip1967Slot(name string) common.Hash {
	hash := crypto.Keccak256Hash([]byte(name))
	value := new(big.Int).SetBytes(hash.Bytes())
	value.Sub(value, big.NewInt(1))
	return common.BigToHash(value)
}

func slotNames() []string {
	names := make([]string, 0, len(proxySlots))
	for _, slot := range proxySlots {
		names = append(names, slot.name)
	}
	return names
}

type proxyArtifact struct {
	SchemaVersion         string            `json:"schema_version"`
	ChainID               int64             `json:"chain_id"`
	Address               string            `json:"address"`
	Block                 model.BlockRef    `json:"block"`
	ProxyType             string            `json:"proxy_type"`
	ImplementationAddress string            `json:"implementation_address,omitempty"`
	BeaconAddress         string            `json:"beacon_address,omitempty"`
	AdminAddress          string            `json:"admin_address,omitempty"`
	Slots                 map[string]string `json:"slots"`
	CodeObservationID     string            `json:"code_observation_id"`
	ObservationIDs        []string          `json:"observation_ids"`
	Warnings              []string          `json:"warnings,omitempty"`
	RetrievedAt           time.Time         `json:"retrieved_at"`
}

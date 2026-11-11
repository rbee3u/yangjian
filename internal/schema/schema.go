// Package schema enforces the structural invariants documented in
// docs/DATA_MODEL.md at store boundaries. The JSON Schema files under schemas/
// are the machine-readable contract; this package is the dependency-free
// runtime subset used by the CLI.
package schema

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
)

var (
	observationKinds = map[string]bool{
		"rpc_call":          true,
		"source_fetch":      true,
		"build":             true,
		"analysis":          true,
		"proxy_probe":       true,
		"report":            true,
		"metadata_binding":  true,
		"source_crosscheck": true,
		"static_analysis":   true,
	}
	relationTypes = map[string]bool{
		"source_dependency": true,
		"onchain_reference": true,
		"observed_call":     true,
		"inferred_relation": true,
	}
	proxyTypes = map[string]bool{
		"unknown": true, "none": true, "EIP-1967": true, "EIP-1822": true,
		"EIP-1167": true, "beacon": true, "other": true,
	}
	validFromSources = map[string]bool{"observed": true, "estimated": true, "deployment": true}
	registryMatches  = map[string]bool{"exact_match": true, "match": true, "none": true, "unknown": true}
	provenanceLevels = map[string]bool{"P0": true, "P1": true, "P2": true, "P3": true}
	correspondences  = map[string]bool{"proven": true, "unproven": true}
	dossierTypes     = map[string]bool{"contract": true, "protocol": true, "discovery": true, "transaction": true}
	manifestStatuses = map[string]bool{"complete": true, "partial": true, "failed": true}
)

// Validate checks a model value. Unknown Go types are accepted so callers can
// still write derived artifacts; known entities are checked strictly.
func Validate(value any) error {
	switch v := value.(type) {
	case model.Observation:
		return validateObservation(v)
	case *model.Observation:
		if v == nil {
			return fmt.Errorf("observation is nil")
		}
		return validateObservation(*v)
	case model.Contract:
		return validateContract(v)
	case *model.Contract:
		if v == nil {
			return fmt.Errorf("contract is nil")
		}
		return validateContract(*v)
	case model.ContractVersion:
		return validateContractVersion(v)
	case *model.ContractVersion:
		if v == nil {
			return fmt.Errorf("contract version is nil")
		}
		return validateContractVersion(*v)
	case model.SourceRevision:
		return validateSourceRevision(v)
	case *model.SourceRevision:
		if v == nil {
			return fmt.Errorf("source revision is nil")
		}
		return validateSourceRevision(*v)
	case model.Relation:
		return validateRelation(v)
	case *model.Relation:
		if v == nil {
			return fmt.Errorf("relation is nil")
		}
		return validateRelation(*v)
	case model.Manifest:
		return validateManifest(v)
	case *model.Manifest:
		if v == nil {
			return fmt.Errorf("manifest is nil")
		}
		return validateManifest(*v)
	case model.Protocol:
		return validateProtocol(v)
	case *model.Protocol:
		if v == nil {
			return fmt.Errorf("protocol is nil")
		}
		return validateProtocol(*v)
	case model.Deployment:
		return validateDeployment(v)
	case *model.Deployment:
		if v == nil {
			return fmt.Errorf("deployment is nil")
		}
		return validateDeployment(*v)
	default:
		return nil
	}
}

func validateObservation(v model.Observation) error {
	if v.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("observation schema_version %q", v.SchemaVersion)
	}
	if v.ID == "" || v.RunID == "" || v.Request == nil || v.ResponseHash == "" {
		return fmt.Errorf("observation %q is missing id, run_id, request, or response_hash", v.ID)
	}
	if !observationKinds[v.Kind] {
		return fmt.Errorf("observation %q has unsupported kind %q", v.ID, v.Kind)
	}
	if v.RetrievedAt.IsZero() {
		return fmt.Errorf("observation %q has no retrieved_at", v.ID)
	}
	if v.Source.Provider == "" {
		return fmt.Errorf("observation %q has no source provider", v.ID)
	}
	return nil
}

func validateContract(v model.Contract) error {
	if v.SchemaVersion != model.SchemaVersion || v.ID == "" || v.ChainID <= 0 {
		return fmt.Errorf("contract identity or schema_version is invalid")
	}
	if !common.IsHexAddress(v.Address) {
		return fmt.Errorf("contract %q has invalid address %q", v.ID, v.Address)
	}
	if len(v.Versions) == 0 || len(v.EvidenceRefs) == 0 {
		return fmt.Errorf("contract %q has no versions or evidence_refs", v.ID)
	}
	return nil
}

func validateContractVersion(v model.ContractVersion) error {
	if v.SchemaVersion != model.SchemaVersion || v.ID == "" || v.ContractID == "" || v.ValidFromBlock == 0 {
		return fmt.Errorf("contract version identity is invalid")
	}
	if !validFromSources[v.ValidFromSource] {
		return fmt.Errorf("contract version %q has invalid valid_from_source %q", v.ID, v.ValidFromSource)
	}
	if v.RuntimeBytecodeHash == "" {
		return fmt.Errorf("contract version %q has no runtime_bytecode_hash", v.ID)
	}
	if !proxyTypes[v.ProxyType] {
		return fmt.Errorf("contract version %q has invalid proxy_type %q", v.ID, v.ProxyType)
	}
	if len(v.EvidenceRefs) == 0 {
		return fmt.Errorf("contract version %q has no evidence_refs", v.ID)
	}
	return nil
}

func validateSourceRevision(v model.SourceRevision) error {
	if v.SchemaVersion != model.SchemaVersion || v.ID == "" {
		return fmt.Errorf("source revision identity is invalid")
	}
	if len(v.Sources) == 0 {
		return fmt.Errorf("source revision %q has no sources", v.ID)
	}
	if !registryMatches[v.RegistryMatch] {
		return fmt.Errorf("source revision %q has invalid registry_match %q", v.ID, v.RegistryMatch)
	}
	if !provenanceLevels[v.ProvenanceLevel] {
		return fmt.Errorf("source revision %q has invalid provenance_level %q", v.ID, v.ProvenanceLevel)
	}
	if !correspondences[v.Correspondence] {
		return fmt.Errorf("source revision %q has invalid correspondence %q", v.ID, v.Correspondence)
	}
	if v.Compilation.Compiler == "" {
		return fmt.Errorf("source revision %q has no compiler", v.ID)
	}
	if len(v.EvidenceRefs) == 0 {
		return fmt.Errorf("source revision %q has no evidence_refs", v.ID)
	}
	return nil
}

func validateRelation(v model.Relation) error {
	if v.SchemaVersion != model.SchemaVersion || v.ID == "" || v.From == "" || v.To == "" {
		return fmt.Errorf("relation identity is invalid: %q", v.ID)
	}
	if !relationTypes[v.Type] {
		return fmt.Errorf("relation %q has unsupported type %q", v.ID, v.Type)
	}
	if len(v.EvidenceRefs) == 0 {
		return fmt.Errorf("relation %q has no evidence_refs", v.ID)
	}
	switch v.Type {
	case "source_dependency":
		for _, key := range []string{"source_path", "symbol", "relation"} {
			if !stringAttribute(v.Attributes, key) {
				return fmt.Errorf("relation %q is missing attribute %q", v.ID, key)
			}
		}
	case "onchain_reference":
		for _, key := range []string{"method", "observation_id", "block_hash"} {
			if !stringAttribute(v.Attributes, key) {
				return fmt.Errorf("relation %q is missing attribute %q", v.ID, key)
			}
		}
		if v.BlockScope == nil || v.BlockScope.FromBlock == 0 {
			return fmt.Errorf("relation %q has no block scope", v.ID)
		}
	case "observed_call":
		for _, key := range []string{"tx_hash", "trace_reference", "call_path"} {
			if !stringAttribute(v.Attributes, key) {
				return fmt.Errorf("relation %q is missing attribute %q", v.ID, key)
			}
		}
	}
	return nil
}

func validateManifest(v model.Manifest) error {
	if v.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("manifest schema_version %q", v.SchemaVersion)
	}
	if !dossierTypes[v.DossierType] {
		return fmt.Errorf("manifest has unsupported dossier_type %q", v.DossierType)
	}
	if !manifestStatuses[v.Status] {
		return fmt.Errorf("manifest has unsupported status %q", v.Status)
	}
	if v.Target.ChainID <= 0 || v.Target.ChainID != v.Block.ChainID {
		return fmt.Errorf("manifest target chain %d does not match block chain %d", v.Target.ChainID, v.Block.ChainID)
	}
	if v.Block.Number == 0 || !common.IsHexHash(v.Block.Hash) {
		return fmt.Errorf("manifest block %d/%q is invalid", v.Block.Number, v.Block.Hash)
	}
	switch v.DossierType {
	case "contract", "discovery":
		if !common.IsHexAddress(v.Target.Address) {
			return fmt.Errorf("manifest %s target address %q is invalid", v.DossierType, v.Target.Address)
		}
	case "protocol":
		if v.Target.ProtocolSlug == "" {
			return fmt.Errorf("manifest protocol target has no protocol_slug")
		}
	case "transaction":
		if !common.IsHexHash(v.Target.TransactionHash) {
			return fmt.Errorf("manifest transaction target hash %q is invalid", v.Target.TransactionHash)
		}
	}
	if len(v.ObservationIDs) == 0 {
		return fmt.Errorf("manifest has no observation_ids")
	}
	for _, artifact := range v.Artifacts {
		if artifact.Kind == "" || artifact.Path == "" || artifact.Hash == "" {
			return fmt.Errorf("manifest artifact %+v is incomplete", artifact)
		}
	}
	if v.CreatedAt.IsZero() || v.UpdatedAt.IsZero() {
		return fmt.Errorf("manifest timestamps are missing")
	}
	return nil
}

func validateProtocol(v model.Protocol) error {
	if v.SchemaVersion != model.SchemaVersion || v.ID == "" || v.Name == "" {
		return fmt.Errorf("protocol identity is invalid")
	}
	if len(v.Deployments) == 0 || len(v.TargetRegistryRefs) == 0 || len(v.EvidenceRefs) == 0 {
		return fmt.Errorf("protocol %q is missing deployments, registry refs, or evidence", v.ID)
	}
	return nil
}

func validateDeployment(v model.Deployment) error {
	if v.SchemaVersion != model.SchemaVersion || v.ID == "" || v.ProtocolID == "" || v.ChainID <= 0 {
		return fmt.Errorf("deployment identity is invalid")
	}
	if v.Block.ChainID != v.ChainID || v.Block.Number == 0 || !common.IsHexHash(v.Block.Hash) {
		return fmt.Errorf("deployment %q has an invalid block", v.ID)
	}
	if len(v.EvidenceRefs) == 0 {
		return fmt.Errorf("deployment %q has no evidence_refs", v.ID)
	}
	return nil
}

func stringAttribute(attributes map[string]any, key string) bool {
	if attributes == nil {
		return false
	}
	value, ok := attributes[key].(string)
	return ok && value != ""
}

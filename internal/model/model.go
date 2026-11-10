package model

import "time"

const SchemaVersion = "1"

type BlockRef struct {
	ChainID int64  `json:"chain_id"`
	Number  uint64 `json:"number"`
	Hash    string `json:"hash"`
}

type SourceRef struct {
	Provider      string `json:"provider"`
	Version       string `json:"version,omitempty"`
	EndpointClass string `json:"endpoint_class,omitempty"`
}

type ObservationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Observation struct {
	SchemaVersion string            `json:"schema_version"`
	ID            string            `json:"id"`
	Kind          string            `json:"kind"`
	RunID         string            `json:"run_id"`
	Request       map[string]any    `json:"request"`
	ResponseHash  string            `json:"response_hash"`
	ResponseRef   string            `json:"response_ref"`
	Block         *BlockRef         `json:"block,omitempty"`
	RetrievedAt   time.Time         `json:"retrieved_at"`
	Source        SourceRef         `json:"source"`
	Error         *ObservationError `json:"error,omitempty"`
	Meta          map[string]any    `json:"meta,omitempty"`
}

type Contract struct {
	SchemaVersion string   `json:"schema_version"`
	ID            string   `json:"id"`
	ChainID       int64    `json:"chain_id"`
	Address       string   `json:"address"`
	Versions      []string `json:"versions"`
	Labels        []string `json:"labels,omitempty"`
	EvidenceRefs  []string `json:"evidence_refs"`
}

type ContractVersion struct {
	SchemaVersion         string   `json:"schema_version"`
	ID                    string   `json:"id"`
	ContractID            string   `json:"contract_id"`
	ValidFromBlock        uint64   `json:"valid_from_block"`
	ValidFromSource       string   `json:"valid_from_source"`
	ValidToBlock          *uint64  `json:"valid_to_block,omitempty"`
	RuntimeBytecodeHash   string   `json:"runtime_bytecode_hash"`
	ProxyType             string   `json:"proxy_type"`
	ImplementationAddress string   `json:"implementation_address,omitempty"`
	SourceRevisionID      string   `json:"source_revision_id,omitempty"`
	ABIHash               string   `json:"abi_hash,omitempty"`
	EvidenceRefs          []string `json:"evidence_refs"`
}

type SourceFile struct {
	Path       string `json:"path"`
	Keccak256  string `json:"keccak256"`
	ContentRef string `json:"content_ref"`
}

type Compilation struct {
	Compiler      string            `json:"compiler"`
	OptimizerRuns *int              `json:"optimizer_runs,omitempty"`
	EVMVersion    string            `json:"evm_version,omitempty"`
	Remappings    []string          `json:"remappings,omitempty"`
	Libraries     map[string]string `json:"libraries,omitempty"`
	BytecodeHash  string            `json:"bytecode_hash,omitempty"`
}

type RegistrySource struct {
	Provider    string    `json:"provider"`
	Address     string    `json:"address"`
	RetrievedAt time.Time `json:"retrieved_at"`
}

type BuildEvidence struct {
	CompilerVersion      string   `json:"compiler_version"`
	CompilerSHA256       string   `json:"compiler_sha256,omitempty"`
	CompilerKeccak256    string   `json:"compiler_keccak256,omitempty"`
	CompilerSource       string   `json:"compiler_source,omitempty"`
	StandardJSONInputRef string   `json:"standard_json_input_ref,omitempty"`
	SolcOutputRef        string   `json:"solc_output_ref,omitempty"`
	OnchainRuntimeRef    string   `json:"onchain_runtime_ref,omitempty"`
	RecompiledRuntimeRef string   `json:"recompiled_runtime_ref,omitempty"`
	Match                bool     `json:"match"`
	ImmutablesMasked     int      `json:"immutables_masked"`
	LinkReferencesMasked int      `json:"link_references_masked"`
	OnchainBytes         int      `json:"onchain_bytes"`
	RecompiledBytes      int      `json:"recompiled_bytes"`
	ComparedBytes        int      `json:"compared_bytes"`
	MismatchOffset       *int     `json:"mismatch_offset,omitempty"`
	Warnings             []string `json:"warnings,omitempty"`
}

type SourceRevision struct {
	SchemaVersion   string           `json:"schema_version"`
	ID              string           `json:"id"`
	Sources         []SourceFile     `json:"sources"`
	MetadataRef     string           `json:"metadata_ref,omitempty"`
	StdJSONInputRef string           `json:"std_json_input_ref,omitempty"`
	Compilation     Compilation      `json:"compilation"`
	RegistryMatch   string           `json:"registry_match"`
	ProvenanceLevel string           `json:"provenance_level"`
	Correspondence  string           `json:"correspondence"`
	RegistrySources []RegistrySource `json:"registry_sources,omitempty"`
	Build           *BuildEvidence   `json:"build,omitempty"`
	EvidenceRefs    []string         `json:"evidence_refs"`
}

type BlockScope struct {
	FromBlock uint64  `json:"from_block"`
	ToBlock   *uint64 `json:"to_block,omitempty"`
}

type Relation struct {
	SchemaVersion string         `json:"schema_version"`
	ID            string         `json:"id"`
	Type          string         `json:"type"`
	From          string         `json:"from"`
	To            string         `json:"to"`
	BlockScope    *BlockScope    `json:"block_scope,omitempty"`
	Attributes    map[string]any `json:"attributes,omitempty"`
	EvidenceRefs  []string       `json:"evidence_refs"`
}

type Artifact struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
	Hash string `json:"hash,omitempty"`
}

type TargetRef struct {
	ChainID      int64  `json:"chain_id"`
	Address      string `json:"address,omitempty"`
	ProtocolSlug string `json:"protocol_slug,omitempty"`
}

type Manifest struct {
	SchemaVersion     string     `json:"schema_version"`
	DossierType       string     `json:"dossier_type"`
	Target            TargetRef  `json:"target"`
	ToolVersion       string     `json:"tool_version"`
	RunID             string     `json:"run_id"`
	Status            string     `json:"status"`
	Block             BlockRef   `json:"block"`
	Artifacts         []Artifact `json:"artifacts"`
	ContractIDs       []string   `json:"contract_ids"`
	ObservationIDs    []string   `json:"observation_ids"`
	SourceRevisionIDs []string   `json:"source_revision_ids,omitempty"`
	RelationIDs       []string   `json:"relation_ids"`
	OpenQuestions     []string   `json:"open_questions"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

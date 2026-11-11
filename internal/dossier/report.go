package dossier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

type ReportParams struct {
	ChainID int64
	Address common.Address
	Store   *store.Store
	RunID   string
}

type ReportResult struct {
	ReportPath       string   `json:"report_path"`
	TopologyPath     string   `json:"topology_path"`
	ArchitecturePath string   `json:"architecture_path"`
	RelationsPath    string   `json:"relations_path"`
	EvidencePath     string   `json:"evidence_path"`
	ObservationIDs   []string `json:"observation_ids"`
	OpenQuestions    []string `json:"open_questions"`
}

func RunReport(params ReportParams) (ReportResult, error) {
	if params.Store == nil {
		return ReportResult{}, fmt.Errorf("store is nil")
	}
	if params.RunID == "" {
		return ReportResult{}, fmt.Errorf("run id is required")
	}
	address, err := model.NormalizeAddress(params.Address.Hex())
	if err != nil {
		return ReportResult{}, err
	}
	var manifest model.Manifest
	if err := params.Store.ReadJSON("manifest.json", &manifest); err != nil {
		return ReportResult{}, fmt.Errorf("%w: read manifest: %v", ErrDossierNotInitialized, err)
	}
	manifest.OpenQuestions = removeString(manifest.OpenQuestions, "report not generated")

	contract, err := loadContract(manifest, params.Store, address)
	if err != nil {
		return ReportResult{}, err
	}
	version, versionPath, err := loadCurrentVersion(manifest, params.Store)
	if err != nil {
		return ReportResult{}, err
	}
	source, err := loadSource(manifest, params.Store)
	if err != nil {
		return ReportResult{}, err
	}
	crossCheck, err := loadCrossCheck(manifest, params.Store)
	if err != nil {
		manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, "source cross-check artifact is unreadable")
	}
	state, err := loadState(manifest, params.Store)
	if err != nil {
		return ReportResult{}, err
	}
	relations, err := loadRelations(manifest, params.Store)
	if err != nil {
		return ReportResult{}, err
	}
	contractABI, abiErr := loadABIArtifact(manifest, params.Store)
	if abiErr != nil {
		manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, "ABI artifact is missing; the method table is omitted")
	} else if state == nil {
		if _, ok := contractABI.Methods["slot0"]; !ok {
			manifest.OpenQuestions = removeString(manifest.OpenQuestions, "state reads not run")
		}
	}
	staticAnalysis, staticErr := loadStatic(manifest, params.Store)
	if staticErr != nil {
		manifest.OpenQuestions = appendUnique(manifest.OpenQuestions, "static analysis artifact is unreadable; the static section is omitted")
	}

	observations := make([]model.Observation, 0, len(manifest.ObservationIDs))
	for _, id := range manifest.ObservationIDs {
		observation, err := readObservation(params.Store, id)
		if err != nil {
			return ReportResult{}, fmt.Errorf("read observation %s: %w", id, err)
		}
		observations = append(observations, observation)
	}

	status := "partial"
	if len(manifest.OpenQuestions) == 0 {
		status = "complete"
	}
	now := time.Now().UTC()
	toolVersion := manifest.ToolVersion
	if toolVersion == "" {
		toolVersion = "dev"
	}

	topology := topologyMermaid(version, relations)
	architecture := architectureMermaid(params.Store, manifest, version, source, state)
	report := renderReport(reportData{
		Manifest:         manifest,
		Status:           status,
		Address:          address,
		Contract:         contract,
		Version:          version,
		VersionPath:      versionPath,
		Source:           source,
		CrossCheck:       crossCheck,
		State:            state,
		Relations:        relations,
		ABI:              contractABI,
		ABIErr:           abiErr,
		Static:           staticAnalysis,
		Observations:     observations,
		ToolVersion:      toolVersion,
		GeneratedAt:      now,
		Topology:         topology,
		Architecture:     architecture,
		TopologyPath:     "topology.mmd",
		ArchitecturePath: "architecture.mmd",
		RelationsPath:    "relations.json",
		EvidencePath:     "evidence.json",
	})

	artifacts := make([]model.Artifact, 0, 5)
	add := func(kind, rel string, data []byte) error {
		artifact, err := saveArtifact(params.Store, kind, rel, data)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, artifact)
		return nil
	}
	if err := add("report", "report.md", []byte(report)); err != nil {
		return ReportResult{}, err
	}
	if err := add("topology", "topology.mmd", []byte(topology)); err != nil {
		return ReportResult{}, err
	}
	if err := add("architecture", "architecture.mmd", []byte(architecture)); err != nil {
		return ReportResult{}, err
	}
	relationsJSON, err := json.MarshalIndent(relations, "", "  ")
	if err != nil {
		return ReportResult{}, fmt.Errorf("encode relations: %w", err)
	}
	if err := add("relations", "relations.json", append(relationsJSON, '\n')); err != nil {
		return ReportResult{}, err
	}
	allArtifacts := append([]model.Artifact{}, manifest.Artifacts...)
	for _, artifact := range artifacts {
		allArtifacts = appendUniqueArtifact(allArtifacts, artifact)
	}
	evidence := evidenceIndex{
		SchemaVersion:   model.SchemaVersion,
		Target:          manifest.Target,
		Block:           manifest.Block,
		Contract:        contract,
		ContractVersion: version,
		SourceRevision:  source,
		Relations:       relations,
		Observations:    observations,
		Artifacts:       allArtifacts,
	}
	evidenceJSON, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return ReportResult{}, fmt.Errorf("encode evidence index: %w", err)
	}
	if err := add("evidence", "evidence.json", append(evidenceJSON, '\n')); err != nil {
		return ReportResult{}, err
	}

	observation, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "report",
		RunID:         params.RunID,
		Request: map[string]any{
			"command": "contract dossier",
			"stage":   "report",
			"formats": []string{"markdown", "mermaid", "json"},
		},
		ResponseHash: model.ContentHash([]byte(report)),
		Block:        &manifest.Block,
		RetrievedAt:  now,
		Source:       model.SourceRef{Provider: "yangjian", Version: toolVersion, EndpointClass: "local"},
		Meta: map[string]any{
			"report_path":       "report.md",
			"topology_path":     "topology.mmd",
			"architecture_path": "architecture.mmd",
			"relations_path":    "relations.json",
			"evidence_path":     "evidence.json",
			"relations":         len(relations),
			"observations":      len(observations),
		},
	})
	if err != nil {
		return ReportResult{}, fmt.Errorf("write report observation: %w", err)
	}

	manifest.ObservationIDs = appendUnique(manifest.ObservationIDs, observation.ID)
	for _, artifact := range artifacts {
		manifest.Artifacts = appendUniqueArtifact(manifest.Artifacts, artifact)
	}
	manifest.Status = status
	manifest.UpdatedAt = now
	if _, err := params.Store.WriteJSON("manifest.json", manifest); err != nil {
		return ReportResult{}, fmt.Errorf("update manifest: %w", err)
	}

	return ReportResult{
		ReportPath:       "report.md",
		TopologyPath:     "topology.mmd",
		ArchitecturePath: "architecture.mmd",
		RelationsPath:    "relations.json",
		EvidencePath:     "evidence.json",
		ObservationIDs:   []string{observation.ID},
		OpenQuestions:    append([]string{}, manifest.OpenQuestions...),
	}, nil
}

type reportData struct {
	Manifest         model.Manifest
	Status           string
	Address          string
	Contract         model.Contract
	Version          model.ContractVersion
	VersionPath      string
	Source           *model.SourceRevision
	CrossCheck       *CrossCheckResult
	State            *stateArtifact
	Relations        []model.Relation
	ABI              abi.ABI
	ABIErr           error
	Static           *StaticAnalysis
	Observations     []model.Observation
	ToolVersion      string
	GeneratedAt      time.Time
	Topology         string
	Architecture     string
	TopologyPath     string
	ArchitecturePath string
	RelationsPath    string
	EvidencePath     string
}

func renderReport(data reportData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Contract Dossier: %s\n\n", data.Address)
	b.WriteString("> Evidence-backed research generated by Yangjian. A verified source is not a security audit. ")
	b.WriteString("A P2 match establishes bytecode correspondence under the recorded compiler and inputs; it does not prove correctness or safety.\n\n")

	b.WriteString("## Summary\n\n")
	b.WriteString("| Field | Value |\n|---|---|\n")
	row := func(field, value string) { fmt.Fprintf(&b, "| %s | %s |\n", md(field), md(value)) }
	row("Chain ID", fmt.Sprintf("%d", data.Manifest.Block.ChainID))
	row("Address", data.Address)
	row("Pinned block", fmt.Sprintf("%d (%s)", data.Manifest.Block.Number, data.Manifest.Block.Hash))
	row("Dossier status", data.Status)
	row("Tool version", data.ToolVersion)
	row("Manifest run ID", data.Manifest.RunID)
	row("Generated at", data.GeneratedAt.Format(time.RFC3339))
	b.WriteString("\n## Generated files\n\n")
	b.WriteString("- `report.md`\n- `topology.mmd`\n- `architecture.mmd`\n- `relations.json`\n- `evidence.json`\n\n")

	b.WriteString("## Runtime code\n\n")
	b.WriteString("| Field | Value |\n|---|---|\n")
	row("Contract ID", data.Contract.ID)
	row("Contract version", data.Version.ID)
	row("Valid from block", fmt.Sprintf("%d (%s)", data.Version.ValidFromBlock, data.Version.ValidFromSource))
	row("Runtime bytecode hash", data.Version.RuntimeBytecodeHash)
	row("Proxy type", proxyDescription(data.Version.ProxyType))
	row("Source revision", data.Version.SourceRevisionID)
	row("ABI hash", data.Version.ABIHash)
	b.WriteString("\n")

	b.WriteString("## Source provenance\n\n")
	if data.Source == nil {
		b.WriteString("Source provenance is not resolved. The open questions below record what remains.\n\n")
	} else {
		b.WriteString("| Field | Value |\n|---|---|\n")
		row("Registry match", data.Source.RegistryMatch)
		row("Compiler", data.Source.Compilation.Compiler)
		if data.Source.Compilation.OptimizerRuns != nil {
			row("Optimizer runs", fmt.Sprintf("%d", *data.Source.Compilation.OptimizerRuns))
		}
		row("EVM version", data.Source.Compilation.EVMVersion)
		row("metadata.bytecodeHash", data.Source.Compilation.BytecodeHash)
		row("Source files", fmt.Sprintf("%d", len(data.Source.Sources)))
		row("Provenance level", data.Source.ProvenanceLevel)
		row("Correspondence", data.Source.Correspondence)
		b.WriteString("\n")
		if data.CrossCheck != nil {
			b.WriteString("### Source cross-check\n\n")
			b.WriteString("| Field | Value |\n|---|---|\n")
			row("Provider", data.CrossCheck.Provider)
			row("Status", data.CrossCheck.Status)
			row("Compiler", data.CrossCheck.CompilerVersion)
			row("Source files", fmt.Sprintf("%d", data.CrossCheck.SourceCount))
			row("Differences", strings.Join(data.CrossCheck.Differences, "; "))
			b.WriteString("\nThe cross-check is secondary evidence; it does not replace the primary registry match or P1/P2 provenance.\n\n")
		}
		if data.Source.Build != nil {
			build := data.Source.Build
			b.WriteString("### P2 build evidence\n\n")
			b.WriteString("| Field | Value |\n|---|---|\n")
			row("Compiler version", build.CompilerVersion)
			row("Compiler SHA-256", build.CompilerSHA256)
			row("Compiler source", build.CompilerSource)
			row("Match", fmt.Sprintf("%t", build.Match))
			row("Immutables masked", fmt.Sprintf("%d", build.ImmutablesMasked))
			row("Link references masked", fmt.Sprintf("%d", build.LinkReferencesMasked))
			row("On-chain / recompiled bytes", fmt.Sprintf("%d / %d", build.OnchainBytes, build.RecompiledBytes))
			row("Compared bytes", fmt.Sprintf("%d", build.ComparedBytes))
			if build.MismatchOffset != nil {
				row("First mismatch offset", fmt.Sprintf("%d", *build.MismatchOffset))
			}
			for _, warning := range build.Warnings {
				row("Build warning", warning)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("## ABI methods\n\n")
	if data.ABIErr != nil {
		b.WriteString("ABI artifact is unavailable. The method table is omitted.\n\n")
	} else {
		methods := make([]abi.Method, 0, len(data.ABI.Methods))
		for _, method := range data.ABI.Methods {
			methods = append(methods, method)
		}
		sort.Slice(methods, func(i, j int) bool { return methods[i].Sig < methods[j].Sig })
		fmt.Fprintf(&b, "<details><summary>%d ABI methods</summary>\n\n", len(methods))
		b.WriteString("| Signature | Selector | Mutability |\n|---|---|---|\n")
		for _, method := range methods {
			fmt.Fprintf(&b, "| %s | `0x%s` | %s |\n", md(method.Sig), hexBytes(method.ID), md(method.StateMutability))
		}
		b.WriteString("\n</details>\n\nSelectors are derived from the recorded ABI; they are not proof that every method is externally callable.\n\n")
	}

	renderStaticSection(&b, data.Static)

	b.WriteString("## State\n\n")
	if data.State == nil {
		b.WriteString("State reads have not been run.\n\n")
	} else {
		b.WriteString("| Field | Value |\n|---|---|\n")
		row("Block", fmt.Sprintf("%d (%s)", data.State.Block.Number, data.State.Block.Hash))
		row("slot0.sqrtPriceX96", data.State.Slot0.SqrtPriceX96)
		row("slot0.tick", fmt.Sprintf("%d", data.State.Slot0.Tick))
		row("slot0.observationIndex", fmt.Sprintf("%d", data.State.Slot0.ObservationIndex))
		row("slot0.observationCardinality", fmt.Sprintf("%d", data.State.Slot0.ObservationCardinality))
		row("slot0.observationCardinalityNext", fmt.Sprintf("%d", data.State.Slot0.ObservationCardinalityNext))
		row("slot0.feeProtocol", fmt.Sprintf("%d", data.State.Slot0.FeeProtocol))
		row("slot0.unlocked", fmt.Sprintf("%t", data.State.Slot0.Unlocked))
		row("liquidity", data.State.Liquidity)
		b.WriteString("\n")
		b.WriteString("### Current tick\n\n")
		b.WriteString("| Field | Value |\n|---|---|\n")
		row("Tick", fmt.Sprintf("%d", data.State.Tick.Tick))
		row("Initialized", fmt.Sprintf("%t", data.State.Tick.Initialized))
		row("liquidityGross", data.State.Tick.LiquidityGross)
		row("liquidityNet", data.State.Tick.LiquidityNet)
		row("feeGrowthOutside0X128", data.State.Tick.FeeGrowthOutside0X128)
		row("feeGrowthOutside1X128", data.State.Tick.FeeGrowthOutside1X128)
		row("tickCumulativeOutside", data.State.Tick.TickCumulativeOutside)
		row("secondsPerLiquidityOutsideX128", data.State.Tick.SecondsPerLiquidityOutsideX128)
		row("secondsOutside", fmt.Sprintf("%d", data.State.Tick.SecondsOutside))
		b.WriteString("\n")
		b.WriteString("### Pool immutables\n\n")
		b.WriteString("| Field | Value |\n|---|---|\n")
		row("token0", data.State.Immutables.Token0)
		row("token1", data.State.Immutables.Token1)
		row("factory", data.State.Immutables.Factory)
		row("fee", fmt.Sprintf("%d", data.State.Immutables.Fee))
		row("tickSpacing", fmt.Sprintf("%d", data.State.Immutables.TickSpacing))
		row("maxLiquidityPerTick", data.State.Immutables.MaxLiquidityPerTick)
		b.WriteString("\n")
	}

	b.WriteString("## Relations\n\n")
	if len(data.Relations) == 0 {
		b.WriteString("No relations were established. No transaction trace or `observed_call` relation is included.\n\n")
	} else {
		b.WriteString("| Type | From | To | Method | Evidence |\n|---|---|---|---|---|\n")
		for _, relation := range data.Relations {
			method, _ := relation.Attributes["method"].(string)
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
				md(relation.Type), md(relation.From), md(relation.To), md(method), md(strings.Join(relation.EvidenceRefs, ", ")))
		}
		b.WriteString("\n`onchain_reference` edges come from pinned-block calls; `source_dependency` edges come from the matched source. Neither is transaction execution evidence.\n\n")
	}

	b.WriteString("## Topology\n\n```mermaid\n")
	b.WriteString(strings.TrimSpace(data.Topology))
	b.WriteString("\n```\n\n")
	b.WriteString("## Evidence architecture\n\n```mermaid\n")
	b.WriteString(strings.TrimSpace(data.Architecture))
	b.WriteString("\n```\n\n")

	b.WriteString("## Open questions\n\n")
	if len(data.Manifest.OpenQuestions) == 0 {
		b.WriteString("None recorded.\n\n")
	} else {
		for _, question := range data.Manifest.OpenQuestions {
			fmt.Fprintf(&b, "- %s\n", question)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Evidence index\n\n")
	b.WriteString("### Observations\n\n")
	b.WriteString("| ID | Kind | Request | Block | Response hash |\n|---|---|---|---|---|\n")
	for _, observation := range data.Observations {
		request := observationRequest(observation)
		block := ""
		if observation.Block != nil {
			block = fmt.Sprintf("%d", observation.Block.Number)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			md(observation.ID), md(observation.Kind), md(request), md(block), md(observation.ResponseHash))
	}
	b.WriteString("\n### Artifacts\n\n")
	b.WriteString("| Kind | Path | Hash |\n|---|---|---|\n")
	for _, artifact := range data.Manifest.Artifacts {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", md(artifact.Kind), md(artifact.Path), md(artifact.Hash))
	}
	b.WriteString("\nStructured relations are in `relations.json`; the full evidence index is in `evidence.json`.\n")
	return b.String()
}

func renderStaticSection(b *strings.Builder, analysis *StaticAnalysis) {
	b.WriteString("## Static structure\n\n")
	if analysis == nil {
		b.WriteString("Static structure is unavailable. The open questions record why; no structure is inferred from the latest source tree.\n\n")
		return
	}
	b.WriteString("Derived from the P2-matched solc output recorded in build evidence. It describes source structure, not an observed execution.\n\n")
	b.WriteString("| Field | Value |\n|---|---|\n")
	row := func(field, value string) { fmt.Fprintf(b, "| %s | %s |\n", md(field), md(value)) }
	row("Compilation target", analysis.CompilationTarget)
	row("Compiler", analysis.Compiler)
	row("Contracts in linearization", fmt.Sprintf("%d", len(analysis.Inheritance)))
	row("Linearization", strings.Join(analysis.Linearization, " -> "))
	row("Storage entries", fmt.Sprintf("%d", len(analysis.Storage)))
	immutableNames := make([]string, 0, len(analysis.Immutables))
	for _, immutable := range analysis.Immutables {
		immutableNames = append(immutableNames, immutable.Name)
	}
	row("Immutable variables", strings.Join(immutableNames, ", "))
	row("Method identifiers", fmt.Sprintf("%d", len(analysis.Methods)))
	row("Source imports", fmt.Sprintf("%d", len(analysis.Imports)))
	row("Call sites", fmt.Sprintf("%d (internal %d, external %d, library %d, library-delegatecall %d, delegatecall %d, low-level %d, callback %d, create %d)",
		analysis.Counts.Total, analysis.Counts.Internal, analysis.Counts.External, analysis.Counts.Library,
		analysis.Counts.LibraryDelegatecall, analysis.Counts.Delegatecall,
		analysis.Counts.Call+analysis.Counts.Staticcall+analysis.Counts.Send+analysis.Counts.Transfer,
		analysis.Counts.Callback, analysis.Counts.Create))
	b.WriteString("\n")

	if len(analysis.Inheritance) > 0 {
		b.WriteString("### Inheritance\n\n")
		b.WriteString("| Contract | Kind | Abstract | Direct bases | Source |\n|---|---|---|---|---|\n")
		for _, node := range analysis.Inheritance {
			fmt.Fprintf(b, "| %s | %s | %t | %s | %s |\n",
				md(node.Contract), md(node.Kind), node.Abstract, md(strings.Join(node.Bases, ", ")), md(node.Source))
		}
		b.WriteString("\n")
	}

	if len(analysis.Storage) > 0 {
		b.WriteString("### Storage layout\n\n")
		limit := len(analysis.Storage)
		if limit > 100 {
			limit = 100
		}
		fmt.Fprintf(b, "<details><summary>%d storage entries (showing %d)</summary>\n\n", len(analysis.Storage), limit)
		b.WriteString("| Slot | Offset | Contract | Label | Type |\n|---|---|---|---|---|\n")
		for _, entry := range analysis.Storage[:limit] {
			fmt.Fprintf(b, "| %s | %d | %s | %s | %s |\n",
				md(entry.Slot), entry.Offset, md(entry.Contract), md(entry.Label), md(entry.Type))
		}
		b.WriteString("\n</details>\n\n")
	}

	if len(analysis.Immutables) > 0 {
		b.WriteString("### Immutable variables\n\n")
		b.WriteString("| Name | Type | Declared in | Byte ranges |\n|---|---|---|---|\n")
		for _, immutable := range analysis.Immutables {
			fmt.Fprintf(b, "| %s | %s | %s | %d |\n",
				md(immutable.Name), md(immutable.Type), md(immutable.Contract), len(immutable.References))
		}
		b.WriteString("\n")
	}

	if len(analysis.Functions) > 0 {
		b.WriteString("### Functions and access surface\n\n")
		limit := len(analysis.Functions)
		if limit > 200 {
			limit = 200
		}
		fmt.Fprintf(b, "<details><summary>%d functions (showing %d)</summary>\n\n", len(analysis.Functions), limit)
		b.WriteString("| Contract | Function | Visibility | Mutability | Modifiers |\n|---|---|---|---|---|\n")
		for _, function := range analysis.Functions[:limit] {
			fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n",
				md(function.Contract), md(function.Display), md(function.Visibility),
				md(function.StateMutability), md(strings.Join(function.Modifiers, ", ")))
		}
		b.WriteString("\n</details>\n\nModifiers are source-level declarations; they are not proof that a permission check is effective.\n\n")
	}

	if len(analysis.CallSites) > 0 {
		b.WriteString("### Call sites\n\n")
		limit := len(analysis.CallSites)
		if limit > 200 {
			limit = 200
		}
		fmt.Fprintf(b, "<details><summary>%d call sites (showing %d)</summary>\n\n", len(analysis.CallSites), limit)
		b.WriteString("| Caller | Kind | Callee | Target type | Source |\n|---|---|---|---|---|\n")
		for _, call := range analysis.CallSites[:limit] {
			fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n",
				md(call.Caller), md(call.Kind), md(call.Callee), md(call.TargetType), md(call.Source))
		}
		b.WriteString("\n</details>\n\nStatic call sites are possible calls in the matched source. Only a trace can establish an observed execution call.\n\n")
	}

	if len(analysis.Warnings) > 0 {
		b.WriteString("### Static analysis warnings\n\n")
		for _, warning := range analysis.Warnings {
			fmt.Fprintf(b, "- %s\n", warning)
		}
		b.WriteString("\n")
	}
}

func loadCrossCheck(manifest model.Manifest, st *store.Store) (*CrossCheckResult, error) {
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind != "source_crosscheck" {
			continue
		}
		var result CrossCheckResult
		if err := st.ReadJSON(artifact.Path, &result); err != nil {
			return nil, err
		}
		return &result, nil
	}
	return nil, nil
}

func loadStatic(manifest model.Manifest, st *store.Store) (*StaticAnalysis, error) {
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind != "static_analysis" {
			continue
		}
		var analysis StaticAnalysis
		if err := st.ReadJSON(artifact.Path, &analysis); err != nil {
			return nil, err
		}
		return &analysis, nil
	}
	return nil, nil
}

func loadContract(manifest model.Manifest, st *store.Store, address string) (model.Contract, error) {
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind != "contract" {
			continue
		}
		var contract model.Contract
		if err := st.ReadJSON(artifact.Path, &contract); err != nil {
			return model.Contract{}, fmt.Errorf("read contract entity: %w", err)
		}
		return contract, nil
	}
	return model.Contract{
		SchemaVersion: model.SchemaVersion,
		ID:            model.ContractID(manifest.Target.ChainID, address),
		ChainID:       manifest.Target.ChainID,
		Address:       address,
	}, nil
}

func loadCurrentVersion(manifest model.Manifest, st *store.Store) (model.ContractVersion, string, error) {
	var fallback model.ContractVersion
	fallbackPath := ""
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind != "contract_version" {
			continue
		}
		var version model.ContractVersion
		if err := st.ReadJSON(artifact.Path, &version); err != nil {
			continue
		}
		fallback = version
		fallbackPath = artifact.Path
		if version.ValidFromBlock == manifest.Block.Number {
			return version, artifact.Path, nil
		}
	}
	if fallbackPath != "" {
		return fallback, fallbackPath, nil
	}
	return model.ContractVersion{}, "", fmt.Errorf("contract version artifact not found")
}

func loadSource(manifest model.Manifest, st *store.Store) (*model.SourceRevision, error) {
	if len(manifest.SourceRevisionIDs) == 0 {
		return nil, nil
	}
	id := manifest.SourceRevisionIDs[len(manifest.SourceRevisionIDs)-1]
	rel := filepath.Join("entities", "sources", strings.TrimPrefix(id, "sha256:")+".json")
	var revision model.SourceRevision
	if err := st.ReadJSON(rel, &revision); err != nil {
		return nil, fmt.Errorf("read source revision %s: %w", id, err)
	}
	return &revision, nil
}

func loadState(manifest model.Manifest, st *store.Store) (*stateArtifact, error) {
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind != "state" {
			continue
		}
		var state stateArtifact
		if err := st.ReadJSON(artifact.Path, &state); err != nil {
			return nil, fmt.Errorf("read state artifact: %w", err)
		}
		return &state, nil
	}
	return nil, nil
}

func loadRelations(manifest model.Manifest, st *store.Store) ([]model.Relation, error) {
	relations := make([]model.Relation, 0, len(manifest.RelationIDs))
	for _, id := range manifest.RelationIDs {
		var relation model.Relation
		if err := st.ReadJSON(relationPath(id), &relation); err != nil {
			return nil, fmt.Errorf("read relation %s: %w", id, err)
		}
		relations = append(relations, relation)
	}
	sort.Slice(relations, func(i, j int) bool { return relations[i].ID < relations[j].ID })
	return relations, nil
}

func loadABIArtifact(manifest model.Manifest, st *store.Store) (abi.ABI, error) {
	rel := "artifacts/abi.json"
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == "abi" {
			rel = artifact.Path
			break
		}
	}
	data, err := os.ReadFile(st.Path(rel))
	if err != nil {
		return abi.ABI{}, err
	}
	return abi.JSON(bytes.NewReader(data))
}

func readObservation(st *store.Store, id string) (model.Observation, error) {
	rel := filepath.Join("observations", strings.TrimPrefix(id, "sha256:")+".json")
	var observation model.Observation
	if err := st.ReadJSON(rel, &observation); err != nil {
		return model.Observation{}, err
	}
	return observation, nil
}

func topologyMermaid(version model.ContractVersion, relations []model.Relation) string {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	fmt.Fprintf(&b, "  Pool[\"Contract<br/>%s\"]\n", mermaid(strings.Split(version.ID, "@")[0]))
	seen := map[string]string{}
	node := 0
	for _, relation := range relations {
		if relation.Type != "onchain_reference" {
			continue
		}
		method, _ := relation.Attributes["method"].(string)
		if method != "token0" && method != "token1" && method != "factory" {
			continue
		}
		target, _ := relation.Attributes["address"].(string)
		if target == "" {
			target = relation.To
		}
		id, ok := seen[target]
		if !ok {
			id = fmt.Sprintf("N%d", node)
			node++
			seen[target] = id
			fmt.Fprintf(&b, "  %s[\"%s<br/>%s\"]\n", id, mermaid(method), mermaid(target))
		}
		fmt.Fprintf(&b, "  Pool -->|%s| %s\n", mermaid(method), id)
	}
	return b.String()
}

func architectureMermaid(st *store.Store, manifest model.Manifest, version model.ContractVersion, source *model.SourceRevision, state *stateArtifact) string {
	var b strings.Builder
	b.WriteString("flowchart TD\n")
	fmt.Fprintf(&b, "  T[\"Target<br/>chain %d<br/>%s\"]\n", manifest.Block.ChainID, mermaid(manifest.Target.Address))
	fmt.Fprintf(&b, "  V[\"ContractVersion<br/>%s<br/>block %d\"]\n", mermaid(short(version.ID)), manifest.Block.Number)
	fmt.Fprintf(&b, "  C[\"Runtime code<br/>%s<br/>%s\"]\n", mermaid(short(version.RuntimeBytecodeHash)), mermaid(runtimeSizeLabel(st, manifest)))
	b.WriteString("  T --> V\n  V --> C\n")
	if source != nil {
		fmt.Fprintf(&b, "  S[\"SourceRevision<br/>%s<br/>%s\"]\n", mermaid(short(source.ID)), mermaid(source.ProvenanceLevel+" / "+source.Correspondence))
		b.WriteString("  V --> S\n")
		if source.Build != nil {
			fmt.Fprintf(&b, "  B[\"P2 build<br/>match=%t<br/>%d immutables masked\"]\n", source.Build.Match, source.Build.ImmutablesMasked)
			b.WriteString("  S --> B\n")
		}
	}
	if state != nil {
		fmt.Fprintf(&b, "  ST[\"State @ %d<br/>tick=%d<br/>liquidity=%s\"]\n", state.Block.Number, state.Slot0.Tick, mermaid(short(state.Liquidity)))
		b.WriteString("  V --> ST\n")
	}
	return b.String()
}

func runtimeSizeLabel(st *store.Store, manifest model.Manifest) string {
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind != "onchain_runtime" {
			continue
		}
		if info, err := os.Stat(st.Path(artifact.Path)); err == nil {
			return fmt.Sprintf("%d bytes", info.Size())
		}
	}
	return "size not recorded"
}

type evidenceIndex struct {
	SchemaVersion   string                `json:"schema_version"`
	Target          model.TargetRef       `json:"target"`
	Block           model.BlockRef        `json:"block"`
	Contract        model.Contract        `json:"contract"`
	ContractVersion model.ContractVersion `json:"contract_version"`
	SourceRevision  *model.SourceRevision `json:"source_revision,omitempty"`
	Relations       []model.Relation      `json:"relations"`
	Observations    []model.Observation   `json:"observations"`
	Artifacts       []model.Artifact      `json:"artifacts"`
}

func proxyDescription(proxyType string) string {
	if proxyType == "" || proxyType == "unknown" {
		return "unknown (proxy detection not run)"
	}
	return proxyType
}

func observationRequest(observation model.Observation) string {
	method, _ := observation.Request["method"].(string)
	function, _ := observation.Request["function"].(string)
	if function != "" {
		return method + " " + function
	}
	return method
}

func md(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", "<br>")
	value = strings.ReplaceAll(value, "\r", "")
	return value
}

func mermaid(value string) string {
	replacer := strings.NewReplacer(
		"\"", "#quot;", "\n", " ", "\r", " ",
		"[", "(", "]", ")", "{", "(", "}", ")",
		";", ",",
	)
	return replacer.Replace(value)
}

func hexBytes(value []byte) string {
	return fmt.Sprintf("%x", value)
}

func short(value string) string {
	if len(value) <= 24 {
		return value
	}
	return value[:14] + "..." + value[len(value)-8:]
}

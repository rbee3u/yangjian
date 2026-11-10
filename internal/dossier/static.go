package dossier

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/solc"
	"github.com/rbee3u/yangjian/internal/store"
)

const (
	staticArtifactRel    = "artifacts/static.json"
	maxStaticOutputBytes = 96 << 20
	maxStaticContracts   = 200
	maxStaticCallSites   = 5000
)

var errStaticOutputTooLarge = errors.New("solc output exceeds the static-analysis read limit")

type StaticAnalysis struct {
	SchemaVersion     string                 `json:"schema_version"`
	SourceRevisionID  string                 `json:"source_revision_id"`
	Block             model.BlockRef         `json:"block"`
	CompilationTarget string                 `json:"compilation_target"`
	Compiler          string                 `json:"compiler,omitempty"`
	Linearization     []string               `json:"linearization"`
	Inheritance       []InheritanceNode      `json:"inheritance"`
	Storage           []StorageEntry         `json:"storage"`
	StorageTypes      map[string]StorageType `json:"storage_types,omitempty"`
	Immutables        []ImmutableVariable    `json:"immutables"`
	Methods           []MethodIdentifier     `json:"methods"`
	Functions         []FunctionSurface      `json:"functions"`
	CallSites         []CallSite             `json:"call_sites"`
	Counts            CallSiteCounts         `json:"call_site_counts"`
	Warnings          []string               `json:"warnings,omitempty"`
	Artifact          string                 `json:"artifact,omitempty"`
}

type InheritanceNode struct {
	Contract string   `json:"contract"`
	Source   string   `json:"source"`
	Kind     string   `json:"kind,omitempty"`
	Abstract bool     `json:"abstract,omitempty"`
	Bases    []string `json:"bases,omitempty"`
}

type StorageEntry struct {
	Contract string `json:"contract"`
	Label    string `json:"label"`
	Slot     string `json:"slot"`
	Offset   int    `json:"offset"`
	Type     string `json:"type"`
	ASTID    int64  `json:"ast_id,omitempty"`
}

type StorageType struct {
	Encoding      string              `json:"encoding,omitempty"`
	Label         string              `json:"label,omitempty"`
	NumberOfBytes string              `json:"number_of_bytes,omitempty"`
	Base          string              `json:"base,omitempty"`
	Key           string              `json:"key,omitempty"`
	Value         string              `json:"value,omitempty"`
	Members       []StorageTypeMember `json:"members,omitempty"`
}

type StorageTypeMember struct {
	Label  string `json:"label"`
	Slot   string `json:"slot"`
	Offset int    `json:"offset"`
	Type   string `json:"type"`
}

type ImmutableVariable struct {
	Name       string           `json:"name"`
	Type       string           `json:"type,omitempty"`
	Contract   string           `json:"contract,omitempty"`
	Source     string           `json:"source,omitempty"`
	ASTID      int64            `json:"ast_id"`
	References []solc.Reference `json:"references"`
}

type MethodIdentifier struct {
	Signature string `json:"signature"`
	Selector  string `json:"selector"`
}

type FunctionSurface struct {
	Contract        string   `json:"contract"`
	Name            string   `json:"name"`
	Display         string   `json:"display"`
	Kind            string   `json:"kind,omitempty"`
	Visibility      string   `json:"visibility,omitempty"`
	StateMutability string   `json:"state_mutability,omitempty"`
	Modifiers       []string `json:"modifiers,omitempty"`
	Virtual         bool     `json:"virtual,omitempty"`
	Source          string   `json:"source"`
	Src             string   `json:"src"`
}

type CallSite struct {
	Caller                string `json:"caller"`
	Kind                  string `json:"kind"`
	Callee                string `json:"callee,omitempty"`
	TargetType            string `json:"target_type,omitempty"`
	ReferencedDeclaration int64  `json:"referenced_declaration,omitempty"`
	Source                string `json:"source"`
	Src                   string `json:"src"`
}

type CallSiteCounts struct {
	Total               int `json:"total"`
	Internal            int `json:"internal"`
	External            int `json:"external"`
	Library             int `json:"library"`
	LibraryDelegatecall int `json:"library_delegatecall"`
	Delegatecall        int `json:"delegatecall"`
	Call                int `json:"call"`
	Staticcall          int `json:"staticcall"`
	Send                int `json:"send"`
	Transfer            int `json:"transfer"`
	Create              int `json:"create"`
	Callback            int `json:"callback"`
}

type staticOutput struct {
	Contracts map[string]map[string]staticContract `json:"contracts"`
	Sources   map[string]staticSource              `json:"sources"`
}

type staticSource struct {
	AST map[string]any `json:"ast"`
}

type staticContract struct {
	StorageLayout struct {
		Storage []struct {
			ASTID    int64  `json:"astId"`
			Contract string `json:"contract"`
			Label    string `json:"label"`
			Offset   int    `json:"offset"`
			Slot     string `json:"slot"`
			Type     string `json:"type"`
		} `json:"storage"`
		Types map[string]struct {
			Encoding      string `json:"encoding"`
			Label         string `json:"label"`
			NumberOfBytes string `json:"numberOfBytes"`
			Base          string `json:"base"`
			Key           string `json:"key"`
			Value         string `json:"value"`
			Members       []struct {
				Label  string `json:"label"`
				Slot   string `json:"slot"`
				Offset int    `json:"offset"`
				Type   string `json:"type"`
			} `json:"members"`
		} `json:"types"`
	} `json:"storageLayout"`
	EVM struct {
		MethodIdentifiers map[string]string `json:"methodIdentifiers"`
		DeployedBytecode  struct {
			ImmutableReferences map[string][]solc.Reference `json:"immutableReferences"`
		} `json:"deployedBytecode"`
	} `json:"evm"`
}

type astContract struct {
	ID        int64
	Name      string
	Source    string
	Kind      string
	Abstract  bool
	BaseIDs   []int64
	BaseNames []string
	Node      map[string]any
}

type astFunction struct {
	ID           int64
	Contract     string
	ContractKind string
	Visibility   string
	Display      string
	Source       string
}

type astVariable struct {
	Name     string
	Type     string
	Contract string
	Source   string
}

// buildStaticAnalysis derives source structure from the solc output recorded by
// the provenance stage. It returns unavailable reasons instead of fabricating a
// result when provenance evidence is missing or unproven.
func buildStaticAnalysis(st *store.Store, manifest model.Manifest, revision *model.SourceRevision) (*StaticAnalysis, []string, error) {
	if revision == nil {
		return nil, []string{"source revision is missing"}, nil
	}
	if revision.Build == nil {
		return nil, []string{"P2 build evidence is missing; run provenance"}, nil
	}
	if !revision.Build.Match {
		return nil, []string{"P2 build did not match runtime bytecode"}, nil
	}
	if revision.Build.SolcOutputRef == "" {
		return nil, []string{"source revision has no solc output artifact"}, nil
	}
	target, err := compilationTargetFrom(*revision, st)
	if err != nil {
		return nil, []string{err.Error()}, nil
	}
	raw, err := readStaticOutput(st.Path(revision.Build.SolcOutputRef))
	if err != nil {
		switch {
		case errors.Is(err, errStaticOutputTooLarge):
			return nil, []string{err.Error()}, nil
		case os.IsNotExist(err):
			return nil, []string{"solc output artifact is missing"}, nil
		default:
			return nil, nil, err
		}
	}
	var output staticOutput
	if err := json.Unmarshal(raw, &output); err != nil {
		return nil, []string{"solc output is not valid JSON: " + truncateText(err.Error(), 200)}, nil
	}
	if len(output.Contracts) == 0 || len(output.Sources) == 0 {
		return nil, []string{"solc output has no contracts or sources"}, nil
	}

	sourceKey := findStaticKey(output.Sources, target.Source)
	if sourceKey == "" {
		return nil, []string{fmt.Sprintf("compilation target source %q is not in the solc output", target.Source)}, nil
	}
	_, contract, ok := findStaticContract(output.Contracts, target.Source, target.Name)
	if !ok {
		return nil, []string{fmt.Sprintf("compilation target %s:%s is not in the solc output", target.Source, target.Name)}, nil
	}

	contracts := collectASTContracts(output.Sources)
	byID := make(map[int64]*astContract, len(contracts))
	byName := make(map[string][]*astContract, len(contracts))
	for _, contract := range contracts {
		byID[contract.ID] = contract
		byName[contract.Name] = append(byName[contract.Name], contract)
	}
	warnings := resolveBaseContracts(contracts, byID, byName)
	targetContract := findASTContract(byID, byName, sourceKey, target.Name)
	if targetContract == nil {
		return nil, []string{fmt.Sprintf("compilation target %s:%s has no AST node", sourceKey, target.Name)}, nil
	}

	order, linearizationWarnings := linearizeContracts(byID, targetContract.ID)
	warnings = append(warnings, linearizationWarnings...)
	if len(order) > maxStaticContracts {
		order = order[:maxStaticContracts]
		warnings = append(warnings, fmt.Sprintf("inheritance closure truncated to %d contracts", maxStaticContracts))
	}

	analysis := &StaticAnalysis{
		SchemaVersion:     model.SchemaVersion,
		SourceRevisionID:  revision.ID,
		Block:             manifest.Block,
		CompilationTarget: sourceKey + ":" + target.Name,
		Compiler:          revision.Compilation.Compiler,
		Linearization:     make([]string, 0, len(order)),
		Inheritance:       make([]InheritanceNode, 0, len(order)),
		Immutables:        []ImmutableVariable{},
		Methods:           []MethodIdentifier{},
		Functions:         []FunctionSurface{},
		CallSites:         []CallSite{},
	}
	for _, id := range order {
		contract := byID[id]
		if contract == nil {
			continue
		}
		bases := make([]string, 0, len(contract.BaseIDs))
		for _, baseID := range contract.BaseIDs {
			if base := byID[baseID]; base != nil {
				bases = append(bases, qualifiedContract(base))
			}
		}
		analysis.Linearization = append(analysis.Linearization, qualifiedContract(contract))
		analysis.Inheritance = append(analysis.Inheritance, InheritanceNode{
			Contract: contract.Name,
			Source:   contract.Source,
			Kind:     contract.Kind,
			Abstract: contract.Abstract,
			Bases:    bases,
		})
	}

	closure := make(map[int64]bool, len(order))
	for _, id := range order {
		closure[id] = true
	}
	functionIndex, declarationKinds := indexASTDeclarations(contracts)
	variables := indexASTVariables(byID, order)

	entries, types, storageWarnings := convertStorageLayout(contract.StorageLayout.Storage, contract.StorageLayout.Types)
	analysis.Storage = entries
	analysis.StorageTypes = types
	warnings = append(warnings, storageWarnings...)

	analysis.Immutables = convertImmutables(contract.EVM.DeployedBytecode.ImmutableReferences, variables, &warnings)

	for signature, selector := range contract.EVM.MethodIdentifiers {
		analysis.Methods = append(analysis.Methods, MethodIdentifier{Signature: signature, Selector: selector})
	}
	sort.Slice(analysis.Methods, func(i, j int) bool {
		return analysis.Methods[i].Signature < analysis.Methods[j].Signature
	})

	callSites := make([]CallSite, 0)
	truncated := false
	for _, id := range order {
		contract := byID[id]
		if contract == nil {
			continue
		}
		functions := contractFunctions(contract.Node)
		for _, fn := range functions {
			surface := functionSurface(contract, fn)
			analysis.Functions = append(analysis.Functions, surface)
			if truncated {
				continue
			}
			calls, cut := extractCallSites(fn, surface.Display, contract.Source, functionIndex, declarationKinds, maxStaticCallSites-len(callSites))
			callSites = append(callSites, calls...)
			truncated = truncated || cut
		}
	}
	if truncated {
		warnings = append(warnings, fmt.Sprintf("call-site list truncated at %d entries", maxStaticCallSites))
	}
	sort.Slice(callSites, func(i, j int) bool {
		if callSites[i].Caller != callSites[j].Caller {
			return callSites[i].Caller < callSites[j].Caller
		}
		if callSites[i].Src != callSites[j].Src {
			return srcStart(callSites[i].Src) < srcStart(callSites[j].Src)
		}
		if callSites[i].Kind != callSites[j].Kind {
			return callSites[i].Kind < callSites[j].Kind
		}
		return callSites[i].Callee < callSites[j].Callee
	})
	analysis.CallSites = callSites
	analysis.Counts = countCallSites(callSites)
	sort.Slice(analysis.Functions, func(i, j int) bool {
		if analysis.Functions[i].Contract != analysis.Functions[j].Contract {
			return analysis.Functions[i].Contract < analysis.Functions[j].Contract
		}
		if analysis.Functions[i].Src != analysis.Functions[j].Src {
			return srcStart(analysis.Functions[i].Src) < srcStart(analysis.Functions[j].Src)
		}
		return analysis.Functions[i].Display < analysis.Functions[j].Display
	})
	analysis.Warnings = warnings
	return analysis, nil, nil
}

func readStaticOutput(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxStaticOutputBytes {
		return nil, fmt.Errorf("%w: %d bytes exceeds %d", errStaticOutputTooLarge, info.Size(), maxStaticOutputBytes)
	}
	return os.ReadFile(path)
}

func findStaticKey[T any](values map[string]T, target string) string {
	trimmed := strings.TrimPrefix(target, "./")
	for _, candidate := range []string{target, trimmed, "./" + trimmed} {
		if candidate == "" {
			continue
		}
		if _, ok := values[candidate]; ok {
			return candidate
		}
	}
	return ""
}

func findStaticContract(contracts map[string]map[string]staticContract, source, name string) (string, staticContract, bool) {
	for _, candidate := range uniqueSourceCandidates(source) {
		byName, ok := contracts[candidate]
		if !ok {
			continue
		}
		if contract, ok := byName[name]; ok {
			return candidate, contract, true
		}
	}
	return "", staticContract{}, false
}

func uniqueSourceCandidates(source string) []string {
	trimmed := strings.TrimPrefix(source, "./")
	result := make([]string, 0, 3)
	for _, candidate := range []string{source, trimmed, "./" + trimmed} {
		seen := false
		for _, existing := range result {
			if existing == candidate {
				seen = true
				break
			}
		}
		if candidate != "" && !seen {
			result = append(result, candidate)
		}
	}
	return result
}

func collectASTContracts(sources map[string]staticSource) []*astContract {
	contracts := make([]*astContract, 0)
	for source, src := range sources {
		walkAST(src.AST, func(node map[string]any) {
			if astString(node, "nodeType") != "ContractDefinition" {
				return
			}
			contract := &astContract{
				ID:       nodeInt(node, "id"),
				Name:     astString(node, "name"),
				Source:   source,
				Kind:     astString(node, "contractKind"),
				Abstract: astBool(node, "abstract"),
				Node:     node,
			}
			if rawBases, ok := node["baseContracts"].([]any); ok {
				for _, raw := range rawBases {
					base := astMap(raw)
					baseName := astMap(base["baseName"])
					contract.BaseIDs = append(contract.BaseIDs, nodeInt(baseName, "referencedDeclaration"))
					contract.BaseNames = append(contract.BaseNames, astString(baseName, "name"))
				}
			}
			contracts = append(contracts, contract)
		})
	}
	sort.Slice(contracts, func(i, j int) bool {
		if contracts[i].Source != contracts[j].Source {
			return contracts[i].Source < contracts[j].Source
		}
		return contracts[i].ID < contracts[j].ID
	})
	return contracts
}

func resolveBaseContracts(contracts []*astContract, byID map[int64]*astContract, byName map[string][]*astContract) []string {
	warnings := make([]string, 0)
	for _, contract := range contracts {
		resolved := make([]int64, 0, len(contract.BaseIDs))
		for i, baseID := range contract.BaseIDs {
			if _, ok := byID[baseID]; ok {
				resolved = append(resolved, baseID)
				continue
			}
			name := ""
			if i < len(contract.BaseNames) {
				name = contract.BaseNames[i]
			}
			candidates := byName[name]
			if len(candidates) == 1 {
				resolved = append(resolved, candidates[0].ID)
				continue
			}
			warnings = append(warnings, fmt.Sprintf("unresolved base contract %q of %s", name, qualifiedContract(contract)))
		}
		contract.BaseIDs = resolved
	}
	return warnings
}

func findASTContract(byID map[int64]*astContract, byName map[string][]*astContract, source, name string) *astContract {
	candidates := byName[name]
	for _, candidate := range candidates {
		if candidate.Source == source {
			return candidate
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	for _, candidate := range byID {
		if candidate.Source == source && candidate.Name == name {
			return candidate
		}
	}
	return nil
}

func linearizeContracts(byID map[int64]*astContract, target int64) ([]int64, []string) {
	cache := make(map[int64][]int64)
	visiting := make(map[int64]bool)
	var visit func(int64) ([]int64, error)
	visit = func(id int64) ([]int64, error) {
		if seq, ok := cache[id]; ok {
			return seq, nil
		}
		if visiting[id] {
			return nil, fmt.Errorf("inheritance cycle at contract %d", id)
		}
		contract := byID[id]
		if contract == nil {
			return nil, fmt.Errorf("contract %d is missing", id)
		}
		visiting[id] = true
		defer delete(visiting, id)
		bases := append([]int64{}, contract.BaseIDs...)
		reverseIDs(bases)
		sequences := make([][]int64, 0, len(bases)+1)
		for _, base := range bases {
			sequence, err := visit(base)
			if err != nil {
				return nil, err
			}
			sequences = append(sequences, sequence)
		}
		sequences = append(sequences, bases)
		merged, err := mergeC3(sequences)
		if err != nil {
			return nil, err
		}
		result := append([]int64{id}, merged...)
		cache[id] = result
		return result, nil
	}
	order, err := visit(target)
	if err != nil {
		return []int64{target}, []string{"inheritance linearization failed: " + err.Error()}
	}
	return order, nil
}

func mergeC3(sequences [][]int64) ([]int64, error) {
	merged := make([]int64, 0)
	for {
		nonEmpty := sequences[:0]
		for _, sequence := range sequences {
			if len(sequence) > 0 {
				nonEmpty = append(nonEmpty, sequence)
			}
		}
		sequences = nonEmpty
		if len(sequences) == 0 {
			return merged, nil
		}
		head := int64(0)
		found := false
		for _, sequence := range sequences {
			candidate := sequence[0]
			if !inAnyTail(sequences, candidate) {
				head = candidate
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("inheritance cycle in C3 merge")
		}
		merged = append(merged, head)
		for i := range sequences {
			if len(sequences[i]) > 0 && sequences[i][0] == head {
				sequences[i] = sequences[i][1:]
			}
		}
	}
}

func inAnyTail(sequences [][]int64, candidate int64) bool {
	for _, sequence := range sequences {
		for _, id := range sequence[1:] {
			if id == candidate {
				return true
			}
		}
	}
	return false
}

func reverseIDs(values []int64) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}

func indexASTDeclarations(contracts []*astContract) (map[int64]astFunction, map[int64]string) {
	functions := make(map[int64]astFunction)
	kinds := make(map[int64]string)
	for _, contract := range contracts {
		if contract == nil {
			continue
		}
		walkAST(contract.Node, func(node map[string]any) {
			nodeType := astString(node, "nodeType")
			nodeID := nodeInt(node, "id")
			if nodeID != 0 && nodeType != "" {
				kinds[nodeID] = nodeType
			}
			if nodeType != "FunctionDefinition" {
				return
			}
			functions[nodeID] = astFunction{
				ID:           nodeID,
				Contract:     contract.Name,
				ContractKind: contract.Kind,
				Visibility:   astString(node, "visibility"),
				Display:      contract.Name + "." + functionDisplay(node),
				Source:       contract.Source,
			}
		})
	}
	return functions, kinds
}

func indexASTVariables(byID map[int64]*astContract, order []int64) map[int64]astVariable {
	variables := make(map[int64]astVariable)
	for _, id := range order {
		contract := byID[id]
		if contract == nil {
			continue
		}
		walkAST(contract.Node, func(node map[string]any) {
			if astString(node, "nodeType") != "VariableDeclaration" || !astBool(node, "stateVariable") {
				return
			}
			variables[nodeInt(node, "id")] = astVariable{
				Name:     astString(node, "name"),
				Type:     astTypeString(node),
				Contract: contract.Name,
				Source:   contract.Source,
			}
		})
	}
	return variables
}

func convertStorageLayout(entries []struct {
	ASTID    int64  `json:"astId"`
	Contract string `json:"contract"`
	Label    string `json:"label"`
	Offset   int    `json:"offset"`
	Slot     string `json:"slot"`
	Type     string `json:"type"`
}, types map[string]struct {
	Encoding      string `json:"encoding"`
	Label         string `json:"label"`
	NumberOfBytes string `json:"numberOfBytes"`
	Base          string `json:"base"`
	Key           string `json:"key"`
	Value         string `json:"value"`
	Members       []struct {
		Label  string `json:"label"`
		Slot   string `json:"slot"`
		Offset int    `json:"offset"`
		Type   string `json:"type"`
	} `json:"members"`
}) ([]StorageEntry, map[string]StorageType, []string) {
	converted := make([]StorageEntry, 0, len(entries))
	for _, entry := range entries {
		converted = append(converted, StorageEntry{
			Contract: entry.Contract,
			Label:    entry.Label,
			Slot:     entry.Slot,
			Offset:   entry.Offset,
			Type:     entry.Type,
			ASTID:    entry.ASTID,
		})
	}
	sort.SliceStable(converted, func(i, j int) bool {
		left, leftOK := new(big.Int).SetString(converted[i].Slot, 10)
		right, rightOK := new(big.Int).SetString(converted[j].Slot, 10)
		if leftOK && rightOK {
			if comparison := left.Cmp(right); comparison != 0 {
				return comparison < 0
			}
		} else if converted[i].Slot != converted[j].Slot {
			return converted[i].Slot < converted[j].Slot
		}
		return converted[i].Offset < converted[j].Offset
	})

	referenced := make(map[string]StorageType)
	warnings := make([]string, 0)
	var visit func(string)
	visit = func(key string) {
		if key == "" {
			return
		}
		if _, ok := referenced[key]; ok {
			return
		}
		raw, ok := types[key]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("storage type %q is missing from the solc output", key))
			return
		}
		convertedType := StorageType{
			Encoding:      raw.Encoding,
			Label:         raw.Label,
			NumberOfBytes: raw.NumberOfBytes,
			Base:          raw.Base,
			Key:           raw.Key,
			Value:         raw.Value,
		}
		for _, member := range raw.Members {
			convertedType.Members = append(convertedType.Members, StorageTypeMember{
				Label:  member.Label,
				Slot:   member.Slot,
				Offset: member.Offset,
				Type:   member.Type,
			})
		}
		referenced[key] = convertedType
		visit(raw.Base)
		visit(raw.Key)
		visit(raw.Value)
		for _, member := range raw.Members {
			visit(member.Type)
		}
	}
	for _, entry := range converted {
		visit(entry.Type)
	}
	if len(referenced) == 0 {
		referenced = nil
	}
	return converted, referenced, warnings
}

func convertImmutables(references map[string][]solc.Reference, variables map[int64]astVariable, warnings *[]string) []ImmutableVariable {
	result := make([]ImmutableVariable, 0, len(references))
	for rawID, ranges := range references {
		id, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil {
			*warnings = append(*warnings, fmt.Sprintf("immutable reference has non-numeric AST id %q", rawID))
			continue
		}
		variable := variables[id]
		name := variable.Name
		if name == "" {
			name = fmt.Sprintf("ast:%d", id)
			*warnings = append(*warnings, fmt.Sprintf("immutable AST id %d has no variable declaration", id))
		}
		sorted := append([]solc.Reference{}, ranges...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
		result = append(result, ImmutableVariable{
			Name:       name,
			Type:       variable.Type,
			Contract:   variable.Contract,
			Source:     variable.Source,
			ASTID:      id,
			References: sorted,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].ASTID < result[j].ASTID
	})
	return result
}

func contractFunctions(contract map[string]any) []map[string]any {
	nodes, ok := contract["nodes"].([]any)
	if !ok {
		return nil
	}
	functions := make([]map[string]any, 0)
	for _, raw := range nodes {
		node := astMap(raw)
		if astString(node, "nodeType") == "FunctionDefinition" {
			functions = append(functions, node)
		}
	}
	return functions
}

func functionSurface(contract *astContract, fn map[string]any) FunctionSurface {
	kind := astString(fn, "kind")
	if kind == "" {
		kind = "function"
	}
	name := astString(fn, "name")
	if name == "" {
		name = kind
	}
	return FunctionSurface{
		Contract:        contract.Name,
		Name:            name,
		Display:         contract.Name + "." + functionDisplay(fn),
		Kind:            kind,
		Visibility:      astString(fn, "visibility"),
		StateMutability: astString(fn, "stateMutability"),
		Modifiers:       functionModifiers(fn),
		Virtual:         astBool(fn, "virtual"),
		Source:          contract.Source,
		Src:             astString(fn, "src"),
	}
}

func functionDisplay(fn map[string]any) string {
	name := astString(fn, "name")
	kind := astString(fn, "kind")
	if name == "" {
		name = kind
	}
	if name == "" {
		name = "function"
	}
	parameters := astMap(fn["parameters"])
	rawParameters, _ := parameters["parameters"].([]any)
	types := make([]string, 0, len(rawParameters))
	for _, raw := range rawParameters {
		types = append(types, astTypeString(astMap(raw)))
	}
	return name + "(" + strings.Join(types, ",") + ")"
}

func functionModifiers(fn map[string]any) []string {
	rawModifiers, _ := fn["modifiers"].([]any)
	modifiers := make([]string, 0, len(rawModifiers))
	for _, raw := range rawModifiers {
		modifier := astMap(raw)
		name := astString(astMap(modifier["modifierName"]), "name")
		if name == "" {
			name = astString(astMap(modifier["modifierName"]), "namePath")
		}
		if name != "" {
			modifiers = append(modifiers, name)
		}
	}
	return modifiers
}

func extractCallSites(fn map[string]any, caller, source string, functions map[int64]astFunction, declarationKinds map[int64]string, remaining int) ([]CallSite, bool) {
	if remaining <= 0 {
		return nil, true
	}
	calls := make([]CallSite, 0)
	truncated := false
	walkAST(fn["body"], func(node map[string]any) {
		if truncated {
			return
		}
		switch astString(node, "nodeType") {
		case "FunctionCall":
			call, ok := classifyCall(node, caller, source, functions, declarationKinds)
			if ok {
				calls = append(calls, call)
			}
		case "NewExpression":
			callee := astString(astMap(node["typeName"]), "name")
			if callee == "" {
				callee = astTypeString(node)
			}
			calls = append(calls, CallSite{
				Caller:     caller,
				Kind:       "create",
				Callee:     callee,
				TargetType: astTypeString(node),
				Source:     source,
				Src:        astString(node, "src"),
			})
		}
		if len(calls) >= remaining {
			truncated = true
		}
	})
	return calls, truncated
}

func classifyCall(node map[string]any, caller, source string, functions map[int64]astFunction, declarationKinds map[int64]string) (CallSite, bool) {
	kind := astString(node, "kind")
	if kind != "" && kind != "functionCall" {
		return CallSite{}, false
	}
	expression := astMap(node["expression"])
	if expression == nil {
		return CallSite{}, false
	}
	if astString(expression, "nodeType") == "FunctionCallOptions" {
		expression = astMap(expression["expression"])
		if expression == nil {
			return CallSite{}, false
		}
	}
	site := CallSite{
		Caller: caller,
		Source: source,
		Src:    astString(node, "src"),
	}
	switch astString(expression, "nodeType") {
	case "Identifier":
		name := astString(expression, "name")
		reference := nodeInt(expression, "referencedDeclaration")
		switch declarationKinds[reference] {
		case "FunctionDefinition":
			callee, ok := functions[reference]
			if !ok {
				return CallSite{}, false
			}
			site.Kind = "internal"
			site.Callee = callee.Display
			site.TargetType = astTypeString(expression)
			site.ReferencedDeclaration = reference
			return site, true
		case "VariableDeclaration":
			if !strings.HasPrefix(astTypeString(expression), "function") || name == "" {
				return CallSite{}, false
			}
			site.Kind = "callback"
			site.Callee = name
			site.TargetType = astTypeString(expression)
			site.ReferencedDeclaration = reference
			return site, true
		default:
			return CallSite{}, false
		}
	case "MemberAccess":
		member := astString(expression, "memberName")
		if member == "" {
			return CallSite{}, false
		}
		inner := astMap(expression["expression"])
		targetType := astTypeString(inner)
		if astString(inner, "name") == "abi" || targetType == "magic" {
			return CallSite{}, false
		}
		site.Callee = member
		site.TargetType = targetType
		switch member {
		case "delegatecall", "call", "staticcall":
			site.Kind = member
			return site, true
		case "send", "transfer":
			if targetType == "address" || targetType == "address payable" {
				site.Kind = member
				return site, true
			}
		}
		site.Kind = "external"
		reference := nodeInt(expression, "referencedDeclaration")
		if callee, ok := functions[reference]; ok {
			if astString(inner, "nodeType") == "Identifier" {
				innerName := astString(inner, "name")
				switch {
				case innerName == "super":
					site.Kind = "internal"
					site.Callee = callee.Display
				case innerName == "this":
					site.Kind = "external"
					site.Callee = callee.Display
				case callee.ContractKind == "library":
					site.Kind = libraryCallKind(callee)
					site.Callee = callee.Display
				}
			} else if callee.ContractKind == "library" {
				site.Kind = libraryCallKind(callee)
				site.Callee = callee.Display
			}
		}
		site.ReferencedDeclaration = reference
		return site, true
	default:
		return CallSite{}, false
	}
}

func libraryCallKind(callee astFunction) string {
	if callee.Visibility == "public" || callee.Visibility == "external" {
		return "library_delegatecall"
	}
	return "library"
}

func countCallSites(calls []CallSite) CallSiteCounts {
	counts := CallSiteCounts{Total: len(calls)}
	for _, call := range calls {
		switch call.Kind {
		case "internal":
			counts.Internal++
		case "external":
			counts.External++
		case "library":
			counts.Library++
		case "library_delegatecall":
			counts.LibraryDelegatecall++
		case "delegatecall":
			counts.Delegatecall++
		case "call":
			counts.Call++
		case "staticcall":
			counts.Staticcall++
		case "send":
			counts.Send++
		case "transfer":
			counts.Transfer++
		case "create":
			counts.Create++
		case "callback":
			counts.Callback++
		}
	}
	return counts
}

func qualifiedContract(contract *astContract) string {
	return contract.Source + ":" + contract.Name
}

func srcStart(src string) int {
	parts := strings.SplitN(src, ":", 2)
	if len(parts) == 0 {
		return 0
	}
	value, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return value
}

func walkAST(value any, fn func(map[string]any)) {
	switch typed := value.(type) {
	case map[string]any:
		if _, ok := typed["nodeType"].(string); ok {
			fn(typed)
		}
		for _, child := range typed {
			walkAST(child, fn)
		}
	case []any:
		for _, child := range typed {
			walkAST(child, fn)
		}
	}
}

func astMap(value any) map[string]any {
	mapped, _ := value.(map[string]any)
	return mapped
}

func astString(node map[string]any, key string) string {
	if node == nil {
		return ""
	}
	value, _ := node[key].(string)
	return value
}

func astBool(node map[string]any, key string) bool {
	if node == nil {
		return false
	}
	value, _ := node[key].(bool)
	return value
}

func nodeInt(node map[string]any, key string) int64 {
	if node == nil {
		return 0
	}
	switch value := node[key].(type) {
	case float64:
		return int64(value)
	case json.Number:
		parsed, _ := value.Int64()
		return parsed
	case int64:
		return value
	case int:
		return int64(value)
	}
	return 0
}

func astTypeString(node map[string]any) string {
	return astString(astMap(node["typeDescriptions"]), "typeString")
}

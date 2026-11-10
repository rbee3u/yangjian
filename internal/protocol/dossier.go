package protocol

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/dossier"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
	"github.com/rbee3u/yangjian/internal/target"
)

type Chain interface {
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	StorageAt(ctx context.Context, account common.Address, slot common.Hash, blockNumber *big.Int) ([]byte, error)
}

type Params struct {
	ChainID        int64
	Target         target.Target
	Block          *big.Int
	Store          *store.Store
	RunID          string
	Chain          Chain
	Fetcher        dossier.SourceFetcher
	CrossCheck     dossier.SourceFetcher
	CrossCheckName string
	Compiler       dossier.Compiler
	ContractRoot   string
	MaxContracts   int
	MaxRequests    int
	ToolVersion    string
	RegistryRef    string
}

type ContractSummary struct {
	Role                string   `json:"role"`
	Address             string   `json:"address"`
	ContractID          string   `json:"contract_id"`
	ContractVersionID   string   `json:"contract_version_id"`
	SourceRevisionID    string   `json:"source_revision_id,omitempty"`
	RuntimeBytecodeHash string   `json:"runtime_bytecode_hash,omitempty"`
	ProxyType           string   `json:"proxy_type,omitempty"`
	ProvenanceLevel     string   `json:"provenance_level,omitempty"`
	Correspondence      string   `json:"correspondence,omitempty"`
	DossierPath         string   `json:"dossier_path"`
	OpenQuestions       []string `json:"open_questions,omitempty"`
	Error               string   `json:"error,omitempty"`
}

type Result struct {
	SchemaVersion    string            `json:"schema_version"`
	ProtocolID       string            `json:"protocol_id"`
	DeploymentID     string            `json:"deployment_id"`
	Slug             string            `json:"slug"`
	Name             string            `json:"name"`
	ChainID          int64             `json:"chain_id"`
	Block            model.BlockRef    `json:"block"`
	Factory          string            `json:"factory"`
	Pool             string            `json:"pool"`
	PoolFound        bool              `json:"pool_found"`
	PoolCodeHash     string            `json:"pool_code_hash,omitempty"`
	PoolCodeSize     int               `json:"pool_code_size"`
	Token0           string            `json:"token0"`
	Token1           string            `json:"token1"`
	Fee              uint32            `json:"fee"`
	Contracts        []ContractSummary `json:"contracts"`
	RelationIDs      []string          `json:"relation_ids"`
	ObservationIDs   []string          `json:"observation_ids"`
	OpenQuestions    []string          `json:"open_questions"`
	ReportPath       string            `json:"report_path"`
	TopologyPath     string            `json:"topology_path"`
	ArchitecturePath string            `json:"architecture_path"`
	RelationsPath    string            `json:"relations_path"`
	EvidencePath     string            `json:"evidence_path"`
	Artifact         string            `json:"artifact"`
	CreatedAt        time.Time         `json:"created_at"`
}

var (
	getPoolABI = mustABI(`[
      {"type":"function","name":"getPool","inputs":[
        {"name":"tokenA","type":"address"},{"name":"tokenB","type":"address"},
        {"name":"fee","type":"uint24"}],"outputs":[
        {"name":"pool","type":"address"}],"stateMutability":"view"}
    ]`)
	factoryABI = mustABI(`[
      {"type":"function","name":"factory","inputs":[],"outputs":[
        {"name":"","type":"address"}],"stateMutability":"view"}
    ]`)
	wethABI = mustABI(`[
      {"type":"function","name":"WETH9","inputs":[],"outputs":[
        {"name":"","type":"address"}],"stateMutability":"view"}
    ]`)
)

func mustABI(raw string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(fmt.Sprintf("parse protocol ABI: %v", err))
	}
	return parsed
}

func RunDossier(ctx context.Context, params Params) (Result, error) {
	if params.Store == nil {
		return Result{}, fmt.Errorf("store is nil")
	}
	if params.Chain == nil {
		return Result{}, fmt.Errorf("chain is nil")
	}
	if params.Fetcher == nil {
		return Result{}, fmt.Errorf("source fetcher is nil")
	}
	if params.Compiler == nil {
		return Result{}, fmt.Errorf("compiler is nil")
	}
	if params.RunID == "" {
		return Result{}, fmt.Errorf("run id is required")
	}
	if params.Target.ChainID != params.ChainID {
		return Result{}, fmt.Errorf("target chain %d does not match requested chain %d", params.Target.ChainID, params.ChainID)
	}
	if params.MaxContracts <= 0 {
		params.MaxContracts = 50
	}
	if params.MaxRequests <= 0 {
		params.MaxRequests = 1000
	}
	if params.ContractRoot == "" {
		params.ContractRoot = filepath.Join("research", fmt.Sprintf("%d", params.ChainID), "contracts")
	}

	header, err := params.Chain.HeaderByNumber(ctx, params.Block)
	if err != nil {
		return Result{}, fmt.Errorf("fetch block header: %w", err)
	}
	if header == nil || header.Number == nil {
		return Result{}, fmt.Errorf("block header has no number")
	}
	blockNumber := header.Number.Uint64()
	blockRef := model.BlockRef{ChainID: params.ChainID, Number: blockNumber, Hash: header.Hash().Hex()}
	now := time.Now().UTC()
	openQuestions := make([]string, 0)
	observationIDs := make([]string, 0)

	headerObservation, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "rpc_call",
		RunID:         params.RunID,
		Request: map[string]any{
			"method": "eth_getBlockByNumber",
			"params": []any{fmt.Sprintf("0x%x", blockNumber), false},
		},
		ResponseHash: "keccak256:" + hex.EncodeToString(crypto.Keccak256(header.Hash().Bytes())),
		Block:        &blockRef,
		RetrievedAt:  now,
		Source:       model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
		Meta: map[string]any{
			"block_number": blockNumber,
			"block_hash":   blockRef.Hash,
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("record block header: %w", err)
	}
	observationIDs = append(observationIDs, headerObservation.ID)

	factoryAddress, err := params.Target.Address("factory")
	if err != nil {
		return Result{}, err
	}
	factory := common.HexToAddress(factoryAddress)
	if len(params.Target.Acceptance.PoolPair) != 2 {
		return Result{}, fmt.Errorf("target acceptance pool_pair must contain two tokens")
	}
	token0, err := params.Target.Token(params.Target.Acceptance.PoolPair[0])
	if err != nil {
		return Result{}, err
	}
	token1, err := params.Target.Token(params.Target.Acceptance.PoolPair[1])
	if err != nil {
		return Result{}, err
	}
	fee := uint32(params.Target.Acceptance.FeeTier)

	discovery, err := discoverPool(ctx, params, factory, common.HexToAddress(token0), common.HexToAddress(token1), fee, blockRef)
	if err != nil {
		return Result{}, err
	}
	pool := discovery.Pool
	observationIDs = append(observationIDs, discovery.ObservationIDs...)
	if !discovery.Found {
		openQuestions = appendUnique(openQuestions, "factory getPool did not return a contract with runtime code at the pinned block")
	}
	discoveryData, err := json.MarshalIndent(struct {
		SchemaVersion  string         `json:"schema_version"`
		ChainID        int64          `json:"chain_id"`
		Factory        string         `json:"factory"`
		Token0         string         `json:"token0"`
		Token1         string         `json:"token1"`
		Fee            uint32         `json:"fee"`
		Block          model.BlockRef `json:"block"`
		Pool           string         `json:"pool"`
		Found          bool           `json:"found"`
		PoolCodeHash   string         `json:"pool_code_hash,omitempty"`
		PoolCodeSize   int            `json:"pool_code_size"`
		ObservationIDs []string       `json:"observation_ids"`
	}{
		SchemaVersion:  model.SchemaVersion,
		ChainID:        params.ChainID,
		Factory:        factory.Hex(),
		Token0:         token0,
		Token1:         token1,
		Fee:            fee,
		Block:          blockRef,
		Pool:           pool.Hex(),
		Found:          discovery.Found,
		PoolCodeHash:   discovery.CodeHash,
		PoolCodeSize:   discovery.CodeSize,
		ObservationIDs: discovery.ObservationIDs,
	}, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encode discovery artifact: %w", err)
	}
	discoveryArtifact, err := saveArtifact(params.Store, "discovery", "artifacts/discovery.json", append(discoveryData, '\n'))
	if err != nil {
		return Result{}, err
	}

	contracts := selectContracts(params.Target, factory, pool, discovery.Found)
	if len(contracts) > params.MaxContracts {
		contracts = contracts[:params.MaxContracts]
		openQuestions = appendUnique(openQuestions, fmt.Sprintf("contract selection truncated at %d", params.MaxContracts))
	}

	summaries := make([]ContractSummary, 0, len(contracts))
	contractObservationCount := 0
	for _, contract := range contracts {
		if contractObservationCount >= params.MaxRequests {
			openQuestions = appendUnique(openQuestions, "request budget exceeded while running contract dossiers")
			break
		}
		summary := runContractDossier(ctx, params, contract, blockRef, pool)
		summaries = append(summaries, summary)
		if summary.Error != "" {
			openQuestions = appendUnique(openQuestions, fmt.Sprintf("%s contract dossier failed: %s", contract.Role, summary.Error))
		}
		contractObservationCount += estimateRequests(params.ContractRoot, contract.Address.Hex())
	}
	if contractObservationCount >= params.MaxRequests {
		openQuestions = appendUnique(openQuestions, fmt.Sprintf("request budget reached: %d observations across contract dossiers", contractObservationCount))
	}
	for _, summary := range summaries {
		for _, question := range summary.OpenQuestions {
			if question == "state reads not run" && summary.Role != "pool" {
				continue
			}
			openQuestions = appendUnique(openQuestions, fmt.Sprintf("%s: %s", roleLabel(summary.Role), question))
		}
	}

	relationIDs := make([]string, 0)
	byRole := make(map[string]ContractSummary, len(summaries))
	for _, summary := range summaries {
		byRole[summary.Role] = summary
	}
	if pool != (common.Address{}) {
		if byRole["factory"].ContractVersionID == "" {
			openQuestions = appendUnique(openQuestions, "factory contract version is unavailable; the factory-to-pool relation cannot be recorded")
		} else {
			observationID := discovery.CallObservationID
			relation, err := writeRelation(params.Store, "onchain_reference", byRole["factory"].ContractVersionID,
				model.ContractID(params.ChainID, pool.Hex()), blockRef, map[string]any{
					"method":         "getPool",
					"selector":       "0x" + hex.EncodeToString(getPoolABI.Methods["getPool"].ID),
					"address":        pool.Hex(),
					"block_hash":     blockRef.Hash,
					"observation_id": observationID,
					"role":           "pool",
				}, []string{observationID})
			if err != nil {
				return Result{}, err
			}
			relationIDs = appendUnique(relationIDs, relation.ID)
		}
	}
	if summary, ok := byRole["nonfungible_position_manager"]; ok {
		if summary.ContractVersionID == "" {
			openQuestions = appendUnique(openQuestions, "NPM contract version is unavailable; NPM relations were skipped")
		} else {
			relations, observations, warning, err := resolveFactoryRelation(ctx, params, "nonfungible_position_manager", summary, factory, blockRef)
			if err != nil {
				openQuestions = appendUnique(openQuestions, "NPM factory relation: "+err.Error())
			} else {
				relationIDs = appendUniqueAll(relationIDs, relations)
				observationIDs = appendUniqueAll(observationIDs, observations)
				if warning != "" {
					openQuestions = appendUnique(openQuestions, warning)
				}
			}
		}
	}
	if summary, ok := byRole["router"]; ok {
		if summary.ContractVersionID == "" {
			openQuestions = appendUnique(openQuestions, "router contract version is unavailable; router relations were skipped")
		} else {
			relations, observations, warning, err := resolveFactoryRelation(ctx, params, "router", summary, factory, blockRef)
			if err != nil {
				openQuestions = appendUnique(openQuestions, "router factory relation: "+err.Error())
			} else {
				relationIDs = appendUniqueAll(relationIDs, relations)
				observationIDs = appendUniqueAll(observationIDs, observations)
				if warning != "" {
					openQuestions = appendUnique(openQuestions, warning)
				}
			}
			relations, observations, warning, err = resolveWETHPairRelation(ctx, params, summary, token0, token1, blockRef)
			if err != nil {
				openQuestions = appendUnique(openQuestions, "router WETH relation: "+err.Error())
			} else {
				relationIDs = appendUniqueAll(relationIDs, relations)
				observationIDs = appendUniqueAll(observationIDs, observations)
				if warning != "" {
					openQuestions = appendUnique(openQuestions, warning)
				}
			}
		}
	}
	if pool == (common.Address{}) {
		openQuestions = appendUnique(openQuestions, "pool-dependent relations are unavailable")
	}

	registryRef := params.RegistryRef
	if registryRef == "" {
		registryRef = "targets/mainnet.json#" + params.Target.Slug
	}
	deploymentID := fmt.Sprintf("%d:%s@%d", params.ChainID, params.Target.Slug, blockNumber)
	protocolID := "protocol:" + params.Target.Slug
	protocolEntity := model.Protocol{
		SchemaVersion:      model.SchemaVersion,
		ID:                 protocolID,
		Name:               params.Target.Name,
		Deployments:        []string{deploymentID},
		TargetRegistryRefs: []string{registryRef},
		EvidenceRefs:       discovery.ObservationIDs,
	}
	protocolRel, protocolHash, err := params.Store.WriteJSONHashed(filepath.Join("entities", "protocols", strings.TrimPrefix(protocolID, "protocol:")+".json"), protocolEntity)
	if err != nil {
		return Result{}, fmt.Errorf("write protocol entity: %w", err)
	}
	contractVersionIDs := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		if summary.ContractVersionID != "" {
			contractVersionIDs = appendUnique(contractVersionIDs, summary.ContractVersionID)
		}
	}
	deployment := model.Deployment{
		SchemaVersion:      model.SchemaVersion,
		ID:                 deploymentID,
		ProtocolID:         protocolID,
		ChainID:            params.ChainID,
		Block:              blockRef,
		ContractVersionIDs: contractVersionIDs,
		EvidenceRefs:       discovery.ObservationIDs,
	}
	deploymentRel, deploymentHash, err := params.Store.WriteJSONHashed(filepath.Join("entities", "deployments", deploymentHashName(deploymentID)+".json"), deployment)
	if err != nil {
		return Result{}, fmt.Errorf("write deployment entity: %w", err)
	}

	manifest := model.Manifest{
		SchemaVersion: model.SchemaVersion,
		DossierType:   "protocol",
		Target:        model.TargetRef{ChainID: params.ChainID, ProtocolSlug: params.Target.Slug},
		ToolVersion:   params.ToolVersion,
		RunID:         params.RunID,
		Status:        "partial",
		Block:         blockRef,
		Artifacts: []model.Artifact{
			{Kind: "protocol", Path: protocolRel, Hash: protocolHash},
			{Kind: "deployment", Path: deploymentRel, Hash: deploymentHash},
			discoveryArtifact,
		},
		ContractIDs:       contractIDs(summaries),
		ObservationIDs:    observationIDs,
		SourceRevisionIDs: sourceRevisionIDs(summaries),
		RelationIDs:       relationIDs,
		ProtocolIDs:       []string{protocolID},
		DeploymentIDs:     []string{deploymentID},
		OpenQuestions:     openQuestions,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	result := Result{
		SchemaVersion:    model.SchemaVersion,
		ProtocolID:       protocolID,
		DeploymentID:     deploymentID,
		Slug:             params.Target.Slug,
		Name:             params.Target.Name,
		ChainID:          params.ChainID,
		Block:            blockRef,
		Factory:          factory.Hex(),
		Pool:             pool.Hex(),
		PoolFound:        discovery.Found,
		PoolCodeHash:     discovery.CodeHash,
		PoolCodeSize:     discovery.CodeSize,
		Token0:           common.HexToAddress(token0).Hex(),
		Token1:           common.HexToAddress(token1).Hex(),
		Fee:              fee,
		Contracts:        summaries,
		RelationIDs:      relationIDs,
		ObservationIDs:   observationIDs,
		OpenQuestions:    openQuestions,
		ReportPath:       "report.md",
		TopologyPath:     "topology.mmd",
		ArchitecturePath: "architecture.mmd",
		RelationsPath:    "relations.json",
		EvidencePath:     "evidence.json",
		Artifact:         "protocol.json",
		CreatedAt:        now,
	}

	contractRelations, contractObservations, contractQuestions, err := collectContractEvidence(params, summaries)
	if err != nil {
		openQuestions = appendUnique(openQuestions, "collect contract evidence: "+err.Error())
	}
	for _, question := range contractQuestions {
		openQuestions = appendUnique(openQuestions, question)
	}
	result.OpenQuestions = openQuestions
	if err := writeProtocolReport(params, result, contractRelations, contractObservations); err != nil {
		return Result{}, err
	}

	reportData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encode protocol result: %w", err)
	}
	artifact, err := saveArtifact(params.Store, "protocol_result", result.Artifact, append(reportData, '\n'))
	if err != nil {
		return Result{}, err
	}
	for _, rel := range []model.Artifact{
		{Kind: "report", Path: result.ReportPath, Hash: hashFile(params.Store, result.ReportPath)},
		{Kind: "topology", Path: result.TopologyPath, Hash: hashFile(params.Store, result.TopologyPath)},
		{Kind: "architecture", Path: result.ArchitecturePath, Hash: hashFile(params.Store, result.ArchitecturePath)},
		{Kind: "relations", Path: result.RelationsPath, Hash: hashFile(params.Store, result.RelationsPath)},
		{Kind: "evidence", Path: result.EvidencePath, Hash: hashFile(params.Store, result.EvidencePath)},
	} {
		manifest.Artifacts = appendUniqueArtifact(manifest.Artifacts, rel)
	}
	manifest.Artifacts = appendUniqueArtifact(manifest.Artifacts, artifact)
	manifest.OpenQuestions = result.OpenQuestions
	if len(manifest.OpenQuestions) == 0 {
		manifest.Status = "complete"
	}
	manifest.UpdatedAt = time.Now().UTC()
	if _, err := params.Store.WriteJSON("manifest.json", manifest); err != nil {
		return Result{}, fmt.Errorf("write protocol manifest: %w", err)
	}
	return result, nil
}

func selectContracts(t target.Target, factory, pool common.Address, poolFound bool) []ContractCandidate {
	candidates := []ContractCandidate{{Role: "factory", Address: factory}}
	if poolFound && pool != (common.Address{}) {
		candidates = append(candidates, ContractCandidate{Role: "pool", Address: pool})
	}
	if address, ok := t.Contracts["nonfungible_position_manager"]; ok {
		candidates = append(candidates, ContractCandidate{Role: "nonfungible_position_manager", Address: common.HexToAddress(address)})
	}
	for _, role := range []string{"swap_router_02", "swap_router"} {
		if address, ok := t.Contracts[role]; ok {
			candidates = append(candidates, ContractCandidate{Role: "router", Address: common.HexToAddress(address)})
			break
		}
	}
	return candidates
}

type ContractCandidate struct {
	Role    string
	Address common.Address
}

func runContractDossier(ctx context.Context, params Params, candidate ContractCandidate, blockRef model.BlockRef, pool common.Address) ContractSummary {
	summary := ContractSummary{
		Role:        candidate.Role,
		Address:     candidate.Address.Hex(),
		DossierPath: filepath.Join(params.ContractRoot, strings.ToLower(candidate.Address.Hex())),
	}
	st := store.New(summary.DossierPath)
	if err := st.Init(); err != nil {
		summary.Error = err.Error()
		return summary
	}
	codeResult, err := dossier.RunCode(ctx, params.Chain, dossier.CodeParams{
		ChainID: params.ChainID,
		Address: candidate.Address,
		Block:   new(big.Int).SetUint64(blockRef.Number),
		Store:   st,
		RunID:   params.RunID,
	})
	if err != nil {
		summary.Error = err.Error()
		return summary
	}
	if codeResult.BlockHash != blockRef.Hash {
		summary.Error = fmt.Sprintf("runtime code is at block %s, not pinned block %s", codeResult.BlockHash, blockRef.Hash)
		return summary
	}
	_, sourceErr := dossier.RunSource(ctx, dossier.SourceParams{
		ChainID:        params.ChainID,
		Address:        candidate.Address,
		Store:          st,
		RunID:          params.RunID,
		Fetcher:        params.Fetcher,
		CrossCheck:     params.CrossCheck,
		CrossCheckName: params.CrossCheckName,
	})
	if sourceErr != nil {
		summary.OpenQuestions = appendUnique(summary.OpenQuestions, "source unavailable: "+sourceErr.Error())
	} else {
		if _, err := dossier.RunProvenance(ctx, dossier.ProvenanceParams{
			ChainID:  params.ChainID,
			Address:  candidate.Address,
			Store:    st,
			RunID:    params.RunID,
			Compiler: params.Compiler,
		}); err != nil {
			summary.OpenQuestions = appendUnique(summary.OpenQuestions, "provenance failed: "+err.Error())
		}
	}
	if _, err := dossier.RunAnalyze(ctx, dossier.AnalyzeParams{
		ChainID: params.ChainID,
		Address: candidate.Address,
		Store:   st,
		RunID:   params.RunID,
		Reader:  params.Chain,
	}); err != nil {
		summary.OpenQuestions = appendUnique(summary.OpenQuestions, "analyze failed: "+err.Error())
	}
	if candidate.Address == pool {
		if _, err := dossier.RunState(ctx, dossier.StateParams{
			ChainID: params.ChainID,
			Address: candidate.Address,
			Store:   st,
			RunID:   params.RunID,
			Caller:  params.Chain,
		}); err != nil {
			summary.OpenQuestions = appendUnique(summary.OpenQuestions, "state failed: "+err.Error())
		}
	}
	if _, err := dossier.RunReport(dossier.ReportParams{
		ChainID: params.ChainID,
		Address: candidate.Address,
		Store:   st,
		RunID:   params.RunID,
	}); err != nil {
		summary.OpenQuestions = appendUnique(summary.OpenQuestions, "report failed: "+err.Error())
	}

	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		summary.Error = err.Error()
		return summary
	}
	summary.ContractID = first(manifest.ContractIDs)
	summary.OpenQuestions = appendUniqueAll(summary.OpenQuestions, manifest.OpenQuestions)
	if len(manifest.SourceRevisionIDs) > 0 {
		summary.SourceRevisionID = manifest.SourceRevisionIDs[len(manifest.SourceRevisionIDs)-1]
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind != "contract_version" {
			continue
		}
		var version model.ContractVersion
		if err := st.ReadJSON(artifact.Path, &version); err != nil {
			continue
		}
		summary.ContractVersionID = version.ID
		summary.RuntimeBytecodeHash = version.RuntimeBytecodeHash
		summary.ProxyType = version.ProxyType
	}
	if summary.SourceRevisionID != "" {
		rel := filepath.Join("entities", "sources", strings.TrimPrefix(summary.SourceRevisionID, "sha256:")+".json")
		var revision model.SourceRevision
		if err := st.ReadJSON(rel, &revision); err == nil {
			summary.ProvenanceLevel = revision.ProvenanceLevel
			summary.Correspondence = revision.Correspondence
		}
	}
	return summary
}

type discoveryResult struct {
	Pool              common.Address
	Found             bool
	CodeHash          string
	CodeSize          int
	CallObservationID string
	ObservationIDs    []string
}

func discoverPool(ctx context.Context, params Params, factory, token0, token1 common.Address, fee uint32, blockRef model.BlockRef) (discoveryResult, error) {
	data, err := getPoolABI.Pack("getPool", token0, token1, new(big.Int).SetUint64(uint64(fee)))
	if err != nil {
		return discoveryResult{}, err
	}
	blockNumber := new(big.Int).SetUint64(blockRef.Number)
	raw, err := params.Chain.CallContract(ctx, ethereum.CallMsg{To: &factory, Data: data}, blockNumber)
	if err != nil {
		return discoveryResult{}, fmt.Errorf("call getPool: %w", err)
	}
	var pool common.Address
	if err := getPoolABI.UnpackIntoInterface(&pool, "getPool", raw); err != nil {
		return discoveryResult{}, fmt.Errorf("decode getPool: %w", err)
	}
	observation, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "rpc_call",
		RunID:         params.RunID,
		Request: map[string]any{
			"method":   "eth_call",
			"function": "getPool",
			"params": []any{
				map[string]any{"to": strings.ToLower(factory.Hex()), "data": "0x" + hex.EncodeToString(data)},
				fmt.Sprintf("0x%x", blockRef.Number),
			},
			"arguments": map[string]any{
				"token0": strings.ToLower(token0.Hex()),
				"token1": strings.ToLower(token1.Hex()),
				"fee":    fee,
			},
		},
		ResponseHash: model.ContentHash(raw),
		Block:        &blockRef,
		RetrievedAt:  time.Now().UTC(),
		Source:       model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
		Meta: map[string]any{
			"response_hex": "0x" + hex.EncodeToString(raw),
			"pool":         pool.Hex(),
		},
	})
	if err != nil {
		return discoveryResult{}, err
	}
	result := discoveryResult{Pool: pool, CallObservationID: observation.ID, ObservationIDs: []string{observation.ID}}
	if pool == (common.Address{}) {
		return result, nil
	}
	code, err := params.Chain.CodeAt(ctx, pool, blockNumber)
	if err != nil {
		return discoveryResult{}, fmt.Errorf("fetch pool code: %w", err)
	}
	codeObservation, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "rpc_call",
		RunID:         params.RunID,
		Request: map[string]any{
			"method": "eth_getCode",
			"params": []any{strings.ToLower(pool.Hex()), fmt.Sprintf("0x%x", blockRef.Number)},
		},
		ResponseHash: hashHex(crypto.Keccak256(code)),
		Block:        &blockRef,
		RetrievedAt:  time.Now().UTC(),
		Source:       model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
		Meta: map[string]any{
			"bytecode_size": len(code),
			"bytecode_hex":  hex.EncodeToString(code),
		},
	})
	if err != nil {
		return discoveryResult{}, err
	}
	result.ObservationIDs = append(result.ObservationIDs, codeObservation.ID)
	result.Found = len(code) > 0
	result.CodeSize = len(code)
	result.CodeHash = hashHex(crypto.Keccak256(code))
	return result, nil
}

func resolveFactoryRelation(ctx context.Context, params Params, role string, summary ContractSummary, factory common.Address, blockRef model.BlockRef) ([]string, []string, string, error) {
	address := common.HexToAddress(summary.Address)
	raw, data, err := callGetter(ctx, params, address, factoryABI, "factory", blockRef)
	if err != nil {
		return nil, nil, "", err
	}
	var resolved common.Address
	if err := factoryABI.UnpackIntoInterface(&resolved, "factory", raw); err != nil {
		return nil, nil, "", fmt.Errorf("decode factory(): %w", err)
	}
	if resolved == (common.Address{}) {
		return nil, nil, "", fmt.Errorf("factory() returned the zero address")
	}
	observation, err := writeCallObservation(params, blockRef, address, "factory", data, raw, resolved)
	if err != nil {
		return nil, nil, "", err
	}
	relation, err := writeRelation(params.Store, "onchain_reference", summary.ContractVersionID,
		model.ContractID(params.ChainID, resolved.Hex()), blockRef, map[string]any{
			"method":                 "factory",
			"selector":               "0x" + hex.EncodeToString(factoryABI.Methods["factory"].ID),
			"address":                resolved.Hex(),
			"block_hash":             blockRef.Hash,
			"observation_id":         observation.ID,
			"role":                   "factory",
			"source_role":            role,
			"matches_target_factory": resolved == factory,
		}, []string{observation.ID})
	if err != nil {
		return nil, nil, "", err
	}
	warning := ""
	if resolved != factory {
		warning = fmt.Sprintf("%s factory() returned %s, which does not match the registry factory %s", role, resolved.Hex(), factory.Hex())
	}
	return []string{relation.ID}, []string{observation.ID}, warning, nil
}

func resolveWETHPairRelation(ctx context.Context, params Params, summary ContractSummary, token0, token1 string, blockRef model.BlockRef) ([]string, []string, string, error) {
	address := common.HexToAddress(summary.Address)
	raw, data, err := callGetter(ctx, params, address, wethABI, "WETH9", blockRef)
	if err != nil {
		return nil, nil, "", err
	}
	var weth common.Address
	if err := wethABI.UnpackIntoInterface(&weth, "WETH9", raw); err != nil {
		return nil, nil, "", fmt.Errorf("decode WETH9(): %w", err)
	}
	if weth == (common.Address{}) {
		return nil, nil, "", fmt.Errorf("WETH9() returned the zero address")
	}
	observation, err := writeCallObservation(params, blockRef, address, "WETH9", data, raw, weth)
	if err != nil {
		return nil, nil, "", err
	}
	relation, err := writeRelation(params.Store, "onchain_reference", summary.ContractVersionID,
		model.ContractID(params.ChainID, weth.Hex()), blockRef, map[string]any{
			"method":         "WETH9",
			"selector":       "0x" + hex.EncodeToString(wethABI.Methods["WETH9"].ID),
			"address":        weth.Hex(),
			"block_hash":     blockRef.Hash,
			"observation_id": observation.ID,
			"role":           "wrapped_native_token",
		}, []string{observation.ID})
	if err != nil {
		return nil, nil, "", err
	}
	warning := ""
	if weth != common.HexToAddress(token0) && weth != common.HexToAddress(token1) {
		warning = fmt.Sprintf("router WETH9() returned %s, which is not one of the target pool tokens", weth.Hex())
	}
	return []string{relation.ID}, []string{observation.ID}, warning, nil
}

func callGetter(ctx context.Context, params Params, address common.Address, contractABI abi.ABI, method string, blockRef model.BlockRef) ([]byte, []byte, error) {
	data, err := contractABI.Pack(method)
	if err != nil {
		return nil, nil, err
	}
	raw, err := params.Chain.CallContract(ctx, ethereum.CallMsg{To: &address, Data: data}, new(big.Int).SetUint64(blockRef.Number))
	if err != nil {
		return nil, nil, fmt.Errorf("call %s: %w", method, err)
	}
	return raw, data, nil
}

func writeCallObservation(params Params, blockRef model.BlockRef, address common.Address, method string, data, raw []byte, resolved common.Address) (model.Observation, error) {
	return params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "rpc_call",
		RunID:         params.RunID,
		Request: map[string]any{
			"method":   "eth_call",
			"function": method,
			"params": []any{
				map[string]any{"to": strings.ToLower(address.Hex()), "data": "0x" + hex.EncodeToString(data)},
				fmt.Sprintf("0x%x", blockRef.Number),
			},
		},
		ResponseHash: model.ContentHash(raw),
		Block:        &blockRef,
		RetrievedAt:  time.Now().UTC(),
		Source:       model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
		Meta: map[string]any{
			"response_hex": "0x" + hex.EncodeToString(raw),
			"resolved":     resolved.Hex(),
		},
	})
}

func writeRelation(st *store.Store, relationType, from, to string, blockRef model.BlockRef, attributes map[string]any, evidenceRefs []string) (model.Relation, error) {
	evidenceHash := ""
	if len(evidenceRefs) > 0 {
		evidenceHash = evidenceRefs[0]
	}
	id := model.RelationID(relationType, from, to, evidenceHash)
	relation := model.Relation{
		SchemaVersion: model.SchemaVersion,
		ID:            id,
		Type:          relationType,
		From:          from,
		To:            to,
		BlockScope:    &model.BlockScope{FromBlock: blockRef.Number},
		Attributes:    attributes,
		EvidenceRefs:  evidenceRefs,
	}
	if _, err := st.WriteJSON(relationPath(id), relation); err != nil {
		return model.Relation{}, err
	}
	return relation, nil
}

func relationPath(id string) string {
	hash := strings.TrimPrefix(model.ContentHash([]byte(id)), "sha256:")
	return filepath.Join("entities", "relations", hash+".json")
}

func collectContractEvidence(params Params, summaries []ContractSummary) ([]model.Relation, []model.Observation, []string, error) {
	relations := make([]model.Relation, 0)
	observations := make([]model.Observation, 0)
	questions := make([]string, 0)
	for _, summary := range summaries {
		if summary.ContractVersionID == "" {
			continue
		}
		st := store.New(summary.DossierPath)
		var manifest model.Manifest
		if err := st.ReadJSON("manifest.json", &manifest); err != nil {
			continue
		}
		for _, id := range manifest.RelationIDs {
			var relation model.Relation
			if err := st.ReadJSON(relationPath(id), &relation); err != nil {
				questions = appendUnique(questions, fmt.Sprintf("%s relation %s is unreadable", summary.Role, id))
				continue
			}
			relations = append(relations, relation)
		}
		for _, id := range manifest.ObservationIDs {
			var observation model.Observation
			rel := filepath.Join("observations", strings.TrimPrefix(id, "sha256:")+".json")
			if err := st.ReadJSON(rel, &observation); err == nil {
				observations = append(observations, observation)
			}
		}
	}
	sort.Slice(relations, func(i, j int) bool { return relations[i].ID < relations[j].ID })
	return relations, observations, questions, nil
}

func contractIDs(summaries []ContractSummary) []string {
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		if summary.ContractID != "" {
			ids = appendUnique(ids, summary.ContractID)
		}
	}
	return ids
}

func sourceRevisionIDs(summaries []ContractSummary) []string {
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		if summary.SourceRevisionID != "" {
			ids = appendUnique(ids, summary.SourceRevisionID)
		}
	}
	return ids
}

func estimateRequests(root, address string) int {
	st := store.New(filepath.Join(root, strings.ToLower(address)))
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		return 0
	}
	return len(manifest.ObservationIDs)
}

func deploymentHashName(id string) string {
	return strings.TrimPrefix(model.ContentHash([]byte(id)), "sha256:")
}

func hashHex(hash []byte) string {
	return "keccak256:" + hex.EncodeToString(hash)
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueAll(values, additions []string) []string {
	for _, value := range additions {
		values = appendUnique(values, value)
	}
	return values
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

func saveArtifact(st *store.Store, kind, rel string, data []byte) (model.Artifact, error) {
	if _, err := st.WriteFile(rel, data); err != nil {
		return model.Artifact{}, err
	}
	return model.Artifact{Kind: kind, Path: rel, Hash: model.ContentHash(data)}, nil
}

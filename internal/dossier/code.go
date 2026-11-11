package dossier

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
	"github.com/rbee3u/yangjian/internal/version"
)

type ChainReader interface {
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
}

type CodeParams struct {
	ChainID int64
	Address common.Address
	Block   *big.Int
	Store   *store.Store
	RunID   string
}

type CodeResult struct {
	ContractID          string   `json:"contract_id"`
	ContractVersionID   string   `json:"contract_version_id"`
	BlockNumber         uint64   `json:"block_number"`
	BlockHash           string   `json:"block_hash"`
	RuntimeBytecodeHash string   `json:"runtime_bytecode_hash"`
	BytecodeSize        int      `json:"bytecode_size"`
	ObservationIDs      []string `json:"observation_ids"`
}

func RunCode(ctx context.Context, chain ChainReader, params CodeParams) (CodeResult, error) {
	if params.Store == nil {
		return CodeResult{}, fmt.Errorf("store is nil")
	}
	if params.RunID == "" {
		return CodeResult{}, fmt.Errorf("run id is required")
	}
	address, err := model.NormalizeAddress(params.Address.Hex())
	if err != nil {
		return CodeResult{}, err
	}

	header, err := chain.HeaderByNumber(ctx, params.Block)
	if err != nil {
		return CodeResult{}, fmt.Errorf("fetch block header: %w", err)
	}
	if header == nil || header.Number == nil {
		return CodeResult{}, fmt.Errorf("block header has no number")
	}
	blockNumber := header.Number.Uint64()
	blockHash := header.Hash().Hex()
	blockRef := model.BlockRef{
		ChainID: params.ChainID,
		Number:  blockNumber,
		Hash:    blockHash,
	}
	blockTag := "latest"
	if params.Block != nil {
		blockTag = fmt.Sprintf("0x%x", blockNumber)
	}
	now := time.Now().UTC()

	headerObs, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "rpc_call",
		RunID:         params.RunID,
		Request: map[string]any{
			"method": "eth_getBlockByNumber",
			"params": []any{blockTag, false},
		},
		ResponseHash: hashHex(header.Hash().Bytes()),
		Block:        &blockRef,
		RetrievedAt:  now,
		Source:       model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
		Meta: map[string]any{
			"block_number": blockNumber,
		},
	})
	if err != nil {
		return CodeResult{}, fmt.Errorf("write header observation: %w", err)
	}

	code, err := chain.CodeAt(ctx, params.Address, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return CodeResult{}, fmt.Errorf("fetch runtime code: %w", err)
	}
	codeHash := crypto.Keccak256(code)
	codeHashHex := hashHex(codeHash)

	codeObs, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "rpc_call",
		RunID:         params.RunID,
		Request: map[string]any{
			"method": "eth_getCode",
			"params": []any{address, fmt.Sprintf("0x%x", blockNumber)},
		},
		ResponseHash: codeHashHex,
		Block:        &blockRef,
		RetrievedAt:  now,
		Source:       model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
		Meta: map[string]any{
			"bytecode_size": len(code),
			"bytecode_hex":  hex.EncodeToString(code),
		},
	})
	if err != nil {
		return CodeResult{}, fmt.Errorf("write code observation: %w", err)
	}

	contractID := model.ContractID(params.ChainID, address)
	versionID := model.ContractVersionID(params.ChainID, address, blockNumber)
	evidence := []string{headerObs.ID, codeObs.ID}

	contractRel := fmt.Sprintf("entities/contracts/%d-%s.json", params.ChainID, strings.ToLower(address))
	contract := model.Contract{
		SchemaVersion: model.SchemaVersion,
		ID:            contractID,
		ChainID:       params.ChainID,
		Address:       address,
		Versions:      []string{versionID},
		EvidenceRefs:  evidence,
	}
	var existingContract model.Contract
	if err := params.Store.ReadJSON(contractRel, &existingContract); err == nil && existingContract.ID == contractID {
		for _, id := range existingContract.Versions {
			contract.Versions = appendUnique(contract.Versions, id)
		}
		for _, id := range existingContract.EvidenceRefs {
			contract.EvidenceRefs = appendUnique(contract.EvidenceRefs, id)
		}
	}
	_, contractHash, err := params.Store.WriteJSONHashed(contractRel, contract)
	if err != nil {
		return CodeResult{}, fmt.Errorf("write contract entity: %w", err)
	}

	versionRel := fmt.Sprintf("entities/versions/%d-%s@%d.json", params.ChainID, strings.ToLower(address), blockNumber)
	contractVersion := model.ContractVersion{
		SchemaVersion:       model.SchemaVersion,
		ID:                  versionID,
		ContractID:          contractID,
		ValidFromBlock:      blockNumber,
		ValidFromSource:     "observed",
		RuntimeBytecodeHash: codeHashHex,
		ProxyType:           "unknown",
		EvidenceRefs:        evidence,
	}
	_, versionHash, err := params.Store.WriteJSONHashed(versionRel, contractVersion)
	if err != nil {
		return CodeResult{}, fmt.Errorf("write contract version entity: %w", err)
	}

	manifest := model.Manifest{
		SchemaVersion: model.SchemaVersion,
		DossierType:   "contract",
		Target: model.TargetRef{
			ChainID: params.ChainID,
			Address: address,
		},
		ToolVersion: version.Version,
		RunID:       params.RunID,
		Status:      "partial",
		Block:       blockRef,
		Artifacts: []model.Artifact{
			{Kind: "contract", Path: contractRel, Hash: contractHash},
			{Kind: "contract_version", Path: versionRel, Hash: versionHash},
		},
		ContractIDs:    []string{contractID},
		ObservationIDs: evidence,
		RelationIDs:    []string{},
		OpenQuestions: []string{
			"proxy detection not run",
			"source provenance not resolved",
			"static analysis not run",
			"state reads not run",
			"report not generated",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := params.Store.WriteJSON("manifest.json", manifest); err != nil {
		return CodeResult{}, fmt.Errorf("write manifest: %w", err)
	}

	return CodeResult{
		ContractID:          contractID,
		ContractVersionID:   versionID,
		BlockNumber:         blockNumber,
		BlockHash:           blockHash,
		RuntimeBytecodeHash: codeHashHex,
		BytecodeSize:        len(code),
		ObservationIDs:      evidence,
	}, nil
}

func hashHex(hash []byte) string {
	return "keccak256:" + hex.EncodeToString(hash)
}

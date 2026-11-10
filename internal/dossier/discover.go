package dossier

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
	"github.com/rbee3u/yangjian/internal/version"
)

var getPoolABI = mustParseABI(`[
  {"type":"function","name":"getPool","inputs":[
    {"name":"tokenA","type":"address"},
    {"name":"tokenB","type":"address"},
    {"name":"fee","type":"uint24"}],"outputs":[
    {"name":"pool","type":"address"}],"stateMutability":"view"}
]`)

func mustParseABI(raw string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(fmt.Sprintf("parse built-in ABI: %v", err))
	}
	return parsed
}

type DiscoveryChain interface {
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

type DiscoverParams struct {
	ChainID int64
	Factory common.Address
	Token0  common.Address
	Token1  common.Address
	Fee     uint32
	Block   *big.Int
	Store   *store.Store
	RunID   string
	Chain   DiscoveryChain
}

type DiscoveryResult struct {
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
	CreatedAt      time.Time      `json:"created_at"`
	Artifact       string         `json:"artifact"`
}

func RunDiscover(ctx context.Context, params DiscoverParams) (DiscoveryResult, error) {
	if params.Store == nil {
		return DiscoveryResult{}, fmt.Errorf("store is nil")
	}
	if params.Chain == nil {
		return DiscoveryResult{}, fmt.Errorf("chain reader is nil")
	}
	if params.RunID == "" {
		return DiscoveryResult{}, fmt.Errorf("run id is required")
	}
	if params.Fee == 0 || params.Fee > 0xFFFFFF {
		return DiscoveryResult{}, fmt.Errorf("fee %d is outside uint24", params.Fee)
	}
	if params.Token0 == (common.Address{}) || params.Token1 == (common.Address{}) {
		return DiscoveryResult{}, fmt.Errorf("token addresses must be non-zero")
	}
	if params.Token0 == params.Token1 {
		return DiscoveryResult{}, fmt.Errorf("token0 and token1 must differ")
	}
	if params.Factory == (common.Address{}) {
		return DiscoveryResult{}, fmt.Errorf("factory address must be non-zero")
	}

	header, err := params.Chain.HeaderByNumber(ctx, params.Block)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("fetch block header: %w", err)
	}
	if header == nil || header.Number == nil {
		return DiscoveryResult{}, fmt.Errorf("block header has no number")
	}
	blockNumber := header.Number.Uint64()
	blockRef := model.BlockRef{ChainID: params.ChainID, Number: blockNumber, Hash: header.Hash().Hex()}
	blockTag := "latest"
	if params.Block != nil {
		blockTag = fmt.Sprintf("0x%x", blockNumber)
	}
	now := time.Now().UTC()

	observations := make([]string, 0, 3)
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
		Meta:         map[string]any{"block_number": blockNumber},
	})
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("write header observation: %w", err)
	}
	observations = append(observations, headerObs.ID)

	callData, err := getPoolABI.Pack("getPool", params.Token0, params.Token1, new(big.Int).SetUint64(uint64(params.Fee)))
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("pack getPool: %w", err)
	}
	factory := params.Factory
	raw, err := params.Chain.CallContract(ctx, ethereum.CallMsg{To: &factory, Data: callData}, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("call getPool at block %d: %w", blockNumber, err)
	}
	var pool common.Address
	if err := getPoolABI.UnpackIntoInterface(&pool, "getPool", raw); err != nil {
		return DiscoveryResult{}, fmt.Errorf("decode getPool result: %w", err)
	}

	callObs, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "rpc_call",
		RunID:         params.RunID,
		Request: map[string]any{
			"method": "eth_call",
			"params": []any{
				map[string]any{"to": strings.ToLower(params.Factory.Hex()), "data": "0x" + hex.EncodeToString(callData)},
				fmt.Sprintf("0x%x", blockNumber),
			},
			"function": "getPool",
			"arguments": map[string]any{
				"token0": strings.ToLower(params.Token0.Hex()),
				"token1": strings.ToLower(params.Token1.Hex()),
				"fee":    params.Fee,
			},
		},
		ResponseHash: model.ContentHash(raw),
		Block:        &blockRef,
		RetrievedAt:  now,
		Source:       model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
		Meta: map[string]any{
			"response_hex": "0x" + hex.EncodeToString(raw),
			"pool":         pool.Hex(),
		},
	})
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("write getPool observation: %w", err)
	}
	observations = append(observations, callObs.ID)

	codeHash := ""
	codeSize := 0
	if pool != (common.Address{}) {
		code, err := params.Chain.CodeAt(ctx, pool, new(big.Int).SetUint64(blockNumber))
		if err != nil {
			return DiscoveryResult{}, fmt.Errorf("fetch pool runtime code: %w", err)
		}
		codeSize = len(code)
		codeHash = hashHex(crypto.Keccak256(code))
		codeObs, err := params.Store.WriteObservation(model.Observation{
			SchemaVersion: model.SchemaVersion,
			Kind:          "rpc_call",
			RunID:         params.RunID,
			Request: map[string]any{
				"method": "eth_getCode",
				"params": []any{strings.ToLower(pool.Hex()), fmt.Sprintf("0x%x", blockNumber)},
			},
			ResponseHash: codeHash,
			Block:        &blockRef,
			RetrievedAt:  now,
			Source:       model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
			Meta: map[string]any{
				"bytecode_size": codeSize,
				"bytecode_hex":  hex.EncodeToString(code),
			},
		})
		if err != nil {
			return DiscoveryResult{}, fmt.Errorf("write pool code observation: %w", err)
		}
		observations = append(observations, codeObs.ID)
	}

	found := pool != (common.Address{}) && codeSize > 0
	result := DiscoveryResult{
		SchemaVersion:  model.SchemaVersion,
		ChainID:        params.ChainID,
		Factory:        params.Factory.Hex(),
		Token0:         params.Token0.Hex(),
		Token1:         params.Token1.Hex(),
		Fee:            params.Fee,
		Block:          blockRef,
		Pool:           pool.Hex(),
		Found:          found,
		PoolCodeHash:   codeHash,
		PoolCodeSize:   codeSize,
		ObservationIDs: observations,
		CreatedAt:      now,
		Artifact:       "discovery.json",
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("encode discovery artifact: %w", err)
	}
	artifact, err := saveArtifact(params.Store, "discovery", result.Artifact, append(data, '\n'))
	if err != nil {
		return DiscoveryResult{}, err
	}

	manifest := model.Manifest{
		SchemaVersion:  model.SchemaVersion,
		DossierType:    "discovery",
		Target:         model.TargetRef{ChainID: params.ChainID, Address: params.Factory.Hex()},
		ToolVersion:    version.Version,
		RunID:          params.RunID,
		Status:         "complete",
		Block:          blockRef,
		Artifacts:      []model.Artifact{artifact},
		ContractIDs:    []string{},
		ObservationIDs: observations,
		RelationIDs:    []string{},
		OpenQuestions:  []string{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if pool != (common.Address{}) && codeSize == 0 {
		manifest.Status = "partial"
		manifest.OpenQuestions = append(manifest.OpenQuestions, "factory returned an address with no runtime code")
	}
	if _, err := params.Store.WriteJSON("manifest.json", manifest); err != nil {
		return DiscoveryResult{}, fmt.Errorf("write manifest: %w", err)
	}
	return result, nil
}

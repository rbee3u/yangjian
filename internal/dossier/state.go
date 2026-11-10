package dossier

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

var ErrABIMissing = errors.New("ABI artifact missing; run the source stage first")

type ContractCaller interface {
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

type StateParams struct {
	ChainID int64
	Address common.Address
	Store   *store.Store
	RunID   string
	Caller  ContractCaller
}

type Slot0Result struct {
	SqrtPriceX96               string `json:"sqrt_price_x96"`
	Tick                       int32  `json:"tick"`
	ObservationIndex           uint16 `json:"observation_index"`
	ObservationCardinality     uint16 `json:"observation_cardinality"`
	ObservationCardinalityNext uint16 `json:"observation_cardinality_next"`
	FeeProtocol                uint8  `json:"fee_protocol"`
	Unlocked                   bool   `json:"unlocked"`
}

type PoolImmutables struct {
	Token0              string `json:"token0"`
	Token1              string `json:"token1"`
	Factory             string `json:"factory"`
	Fee                 uint32 `json:"fee"`
	TickSpacing         int32  `json:"tick_spacing"`
	MaxLiquidityPerTick string `json:"max_liquidity_per_tick"`
}

type TickResult struct {
	Tick                           int32  `json:"tick"`
	LiquidityGross                 string `json:"liquidity_gross"`
	LiquidityNet                   string `json:"liquidity_net"`
	FeeGrowthOutside0X128          string `json:"fee_growth_outside_0x128"`
	FeeGrowthOutside1X128          string `json:"fee_growth_outside_1x128"`
	TickCumulativeOutside          string `json:"tick_cumulative_outside"`
	SecondsPerLiquidityOutsideX128 string `json:"seconds_per_liquidity_outside_x128"`
	SecondsOutside                 uint32 `json:"seconds_outside"`
	Initialized                    bool   `json:"initialized"`
}

type StateResult struct {
	BlockNumber    uint64         `json:"block_number"`
	BlockHash      string         `json:"block_hash"`
	Slot0          Slot0Result    `json:"slot0"`
	Liquidity      string         `json:"liquidity"`
	Tick           TickResult     `json:"tick"`
	Immutables     PoolImmutables `json:"immutables"`
	RelationIDs    []string       `json:"relation_ids"`
	ObservationIDs []string       `json:"observation_ids"`
	Artifact       string         `json:"artifact"`
}

func RunState(ctx context.Context, params StateParams) (StateResult, error) {
	if params.Store == nil {
		return StateResult{}, fmt.Errorf("store is nil")
	}
	if params.Caller == nil {
		return StateResult{}, fmt.Errorf("contract caller is nil")
	}
	if params.RunID == "" {
		return StateResult{}, fmt.Errorf("run id is required")
	}
	address, err := model.NormalizeAddress(params.Address.Hex())
	if err != nil {
		return StateResult{}, err
	}
	var manifest model.Manifest
	if err := params.Store.ReadJSON("manifest.json", &manifest); err != nil {
		return StateResult{}, fmt.Errorf("%w: read manifest: %v", ErrDossierNotInitialized, err)
	}
	if manifest.Block.Number == 0 {
		return StateResult{}, fmt.Errorf("%w: manifest has no pinned block", ErrDossierNotInitialized)
	}
	contractABI, abiRel, err := loadABI(manifest, params.Store)
	if err != nil {
		return StateResult{}, err
	}
	for _, method := range []string{"slot0", "liquidity", "ticks", "token0", "token1", "factory", "fee", "tickSpacing", "maxLiquidityPerTick"} {
		if _, ok := contractABI.Methods[method]; !ok {
			return StateResult{}, fmt.Errorf("ABI %s has no %s() method; the state stage currently supports Uniswap V3 pool ABIs", abiRel, method)
		}
	}

	blockNumber := new(big.Int).SetUint64(manifest.Block.Number)
	observations := make([]string, 0, 3)

	slot0Raw, slot0Obs, err := callRead(ctx, params, contractABI, "slot0", nil, manifest.Block, blockNumber, abiRel)
	if err != nil {
		return StateResult{}, err
	}
	observations = append(observations, slot0Obs.ID)
	slot0, err := decodeSlot0(contractABI, slot0Raw)
	if err != nil {
		return StateResult{}, err
	}

	liquidityRaw, liquidityObs, err := callRead(ctx, params, contractABI, "liquidity", nil, manifest.Block, blockNumber, abiRel)
	if err != nil {
		return StateResult{}, err
	}
	observations = append(observations, liquidityObs.ID)
	var liquidity *big.Int
	if err := contractABI.UnpackIntoInterface(&liquidity, "liquidity", liquidityRaw); err != nil {
		return StateResult{}, fmt.Errorf("decode liquidity: %w", err)
	}
	if liquidity == nil {
		return StateResult{}, fmt.Errorf("decode liquidity: nil result")
	}

	ticksRaw, ticksObs, err := callRead(ctx, params, contractABI, "ticks", []any{big.NewInt(int64(slot0.Tick))}, manifest.Block, blockNumber, abiRel)
	if err != nil {
		return StateResult{}, err
	}
	observations = append(observations, ticksObs.ID)
	tick, err := decodeTick(contractABI, ticksRaw, slot0.Tick)
	if err != nil {
		return StateResult{}, err
	}

	immutables, immutableObs, err := readPoolImmutables(ctx, params, contractABI, manifest.Block, blockNumber, abiRel)
	if err != nil {
		return StateResult{}, err
	}
	for _, method := range []string{"token0", "token1", "factory", "fee", "tickSpacing", "maxLiquidityPerTick"} {
		observations = append(observations, immutableObs[method])
	}
	relationIDs, err := writePoolRelations(params, contractABI, manifest, common.HexToAddress(address), immutables, immutableObs)
	if err != nil {
		return StateResult{}, err
	}

	now := time.Now().UTC()
	artifact := stateArtifact{
		SchemaVersion:  model.SchemaVersion,
		ChainID:        params.ChainID,
		Address:        address,
		Block:          manifest.Block,
		Slot0:          slot0,
		Liquidity:      liquidity.String(),
		Tick:           tick,
		Immutables:     immutables,
		RelationIDs:    relationIDs,
		ObservationIDs: observations,
		RetrievedAt:    now,
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return StateResult{}, fmt.Errorf("encode state artifact: %w", err)
	}
	saved, err := saveArtifact(params.Store, "state", "artifacts/state.json", append(data, '\n'))
	if err != nil {
		return StateResult{}, err
	}

	for _, id := range observations {
		manifest.ObservationIDs = appendUnique(manifest.ObservationIDs, id)
	}
	for _, id := range relationIDs {
		manifest.RelationIDs = appendUnique(manifest.RelationIDs, id)
	}
	manifest.Artifacts = appendUniqueArtifact(manifest.Artifacts, saved)
	manifest.OpenQuestions = removeString(manifest.OpenQuestions, "state reads not run")
	if !tick.Initialized {
		manifest.OpenQuestions = appendUnique(manifest.OpenQuestions,
			fmt.Sprintf("current tick %d is not initialized; ticks() returned zero values", tick.Tick))
	}
	manifest.Status = "partial"
	manifest.UpdatedAt = now
	if _, err := params.Store.WriteJSON("manifest.json", manifest); err != nil {
		return StateResult{}, fmt.Errorf("update manifest: %w", err)
	}

	return StateResult{
		BlockNumber:    manifest.Block.Number,
		BlockHash:      manifest.Block.Hash,
		Slot0:          slot0,
		Liquidity:      liquidity.String(),
		Tick:           tick,
		Immutables:     immutables,
		RelationIDs:    relationIDs,
		ObservationIDs: observations,
		Artifact:       saved.Path,
	}, nil
}

func readPoolImmutables(ctx context.Context, params StateParams, contractABI abi.ABI, blockRef model.BlockRef, blockNumber *big.Int, abiRel string) (PoolImmutables, map[string]string, error) {
	observed := make(map[string]string)
	token0, token0Obs, err := callAddress(ctx, params, contractABI, "token0", blockRef, blockNumber, abiRel)
	if err != nil {
		return PoolImmutables{}, nil, err
	}
	observed["token0"] = token0Obs.ID
	token1, token1Obs, err := callAddress(ctx, params, contractABI, "token1", blockRef, blockNumber, abiRel)
	if err != nil {
		return PoolImmutables{}, nil, err
	}
	observed["token1"] = token1Obs.ID
	factory, factoryObs, err := callAddress(ctx, params, contractABI, "factory", blockRef, blockNumber, abiRel)
	if err != nil {
		return PoolImmutables{}, nil, err
	}
	observed["factory"] = factoryObs.ID

	fee, feeObs, err := callBig(ctx, params, contractABI, "fee", blockRef, blockNumber, abiRel)
	if err != nil {
		return PoolImmutables{}, nil, err
	}
	if !fee.IsUint64() || fee.BitLen() > 24 {
		return PoolImmutables{}, nil, fmt.Errorf("decode fee: %s does not fit uint24", fee)
	}
	observed["fee"] = feeObs.ID

	tickSpacing, tickSpacingObs, err := callBig(ctx, params, contractABI, "tickSpacing", blockRef, blockNumber, abiRel)
	if err != nil {
		return PoolImmutables{}, nil, err
	}
	tickSpacingValue, err := int32FromBig(tickSpacing)
	if err != nil {
		return PoolImmutables{}, nil, fmt.Errorf("decode tickSpacing: %w", err)
	}
	observed["tickSpacing"] = tickSpacingObs.ID

	maxLiquidity, maxLiquidityObs, err := callBig(ctx, params, contractABI, "maxLiquidityPerTick", blockRef, blockNumber, abiRel)
	if err != nil {
		return PoolImmutables{}, nil, err
	}
	observed["maxLiquidityPerTick"] = maxLiquidityObs.ID

	return PoolImmutables{
		Token0:              token0.Hex(),
		Token1:              token1.Hex(),
		Factory:             factory.Hex(),
		Fee:                 uint32(fee.Uint64()),
		TickSpacing:         tickSpacingValue,
		MaxLiquidityPerTick: maxLiquidity.String(),
	}, observed, nil
}

func callAddress(ctx context.Context, params StateParams, contractABI abi.ABI, method string, blockRef model.BlockRef, blockNumber *big.Int, abiRel string) (common.Address, model.Observation, error) {
	raw, obs, err := callRead(ctx, params, contractABI, method, nil, blockRef, blockNumber, abiRel)
	if err != nil {
		return common.Address{}, obs, err
	}
	var out common.Address
	if err := contractABI.UnpackIntoInterface(&out, method, raw); err != nil {
		return common.Address{}, obs, fmt.Errorf("decode %s: %w", method, err)
	}
	return out, obs, nil
}

func callBig(ctx context.Context, params StateParams, contractABI abi.ABI, method string, blockRef model.BlockRef, blockNumber *big.Int, abiRel string) (*big.Int, model.Observation, error) {
	raw, obs, err := callRead(ctx, params, contractABI, method, nil, blockRef, blockNumber, abiRel)
	if err != nil {
		return nil, obs, err
	}
	var out *big.Int
	if err := contractABI.UnpackIntoInterface(&out, method, raw); err != nil {
		return nil, obs, fmt.Errorf("decode %s: %w", method, err)
	}
	if out == nil {
		return nil, obs, fmt.Errorf("decode %s: nil result", method)
	}
	return out, obs, nil
}

func writePoolRelations(params StateParams, contractABI abi.ABI, manifest model.Manifest, pool common.Address, immutables PoolImmutables, observed map[string]string) ([]string, error) {
	versionID := model.ContractVersionID(params.ChainID, pool.Hex(), manifest.Block.Number)
	targets := []struct {
		method string
		value  common.Address
	}{
		{"token0", common.HexToAddress(immutables.Token0)},
		{"token1", common.HexToAddress(immutables.Token1)},
		{"factory", common.HexToAddress(immutables.Factory)},
	}
	relationIDs := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.value == (common.Address{}) {
			return nil, fmt.Errorf("%s() returned the zero address", target.method)
		}
		obsID := observed[target.method]
		if obsID == "" {
			return nil, fmt.Errorf("%s observation is missing", target.method)
		}
		relation, err := writeOnchainRelation(params.Store, params.ChainID, versionID, manifest.Block, target.method, contractABI.Methods[target.method].ID, target.value, obsID)
		if err != nil {
			return nil, err
		}
		relationIDs = appendUnique(relationIDs, relation.ID)
	}
	return relationIDs, nil
}

func writeOnchainRelation(st *store.Store, chainID int64, fromVersion string, blockRef model.BlockRef, method string, selector []byte, to common.Address, obsID string) (model.Relation, error) {
	toID := model.ContractID(chainID, to.Hex())
	id := model.RelationID("onchain_reference", fromVersion, toID, obsID)
	relation := model.Relation{
		SchemaVersion: model.SchemaVersion,
		ID:            id,
		Type:          "onchain_reference",
		From:          fromVersion,
		To:            toID,
		BlockScope:    &model.BlockScope{FromBlock: blockRef.Number},
		Attributes: map[string]any{
			"method":         method,
			"selector":       "0x" + hex.EncodeToString(selector),
			"address":        to.Hex(),
			"block_hash":     blockRef.Hash,
			"observation_id": obsID,
		},
		EvidenceRefs: []string{obsID},
	}
	if _, err := st.WriteJSON(relationPath(id), relation); err != nil {
		return model.Relation{}, fmt.Errorf("write %s relation: %w", method, err)
	}
	return relation, nil
}

func relationPath(id string) string {
	hash := strings.TrimPrefix(model.ContentHash([]byte(id)), "sha256:")
	return "entities/relations/" + hash + ".json"
}

func callRead(ctx context.Context, params StateParams, contractABI abi.ABI, method string, args []any, blockRef model.BlockRef, blockNumber *big.Int, abiRel string) ([]byte, model.Observation, error) {
	data, err := contractABI.Pack(method, args...)
	if err != nil {
		return nil, model.Observation{}, fmt.Errorf("pack %s: %w", method, err)
	}
	to := params.Address
	raw, err := params.Caller.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, blockNumber)
	if err != nil {
		return nil, model.Observation{}, fmt.Errorf("call %s at block %s: %w", method, blockNumber.String(), err)
	}
	selector := contractABI.Methods[method].ID
	request := map[string]any{
		"method": "eth_call",
		"params": []any{
			map[string]any{
				"to":   strings.ToLower(params.Address.Hex()),
				"data": "0x" + hex.EncodeToString(data),
			},
			fmt.Sprintf("0x%x", blockNumber.Uint64()),
		},
		"function": method,
		"selector": "0x" + hex.EncodeToString(selector),
	}
	if len(args) > 0 {
		if tickArg, ok := args[0].(*big.Int); ok {
			request["arguments"] = map[string]any{"tick": tickArg.String()}
		} else {
			request["arguments"] = args
		}
	}
	if blockRef.ChainID == 0 {
		blockRef.ChainID = params.ChainID
	}
	meta := map[string]any{
		"function":       method,
		"selector":       "0x" + hex.EncodeToString(selector),
		"response_hex":   "0x" + hex.EncodeToString(raw),
		"response_bytes": len(raw),
		"abi_ref":        abiRel,
	}
	obs, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "rpc_call",
		RunID:         params.RunID,
		Request:       request,
		ResponseHash:  model.ContentHash(raw),
		Block:         &blockRef,
		RetrievedAt:   time.Now().UTC(),
		Source:        model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
		Meta:          meta,
	})
	if err != nil {
		return nil, model.Observation{}, fmt.Errorf("write %s observation: %w", method, err)
	}
	return raw, obs, nil
}

func loadABI(manifest model.Manifest, st *store.Store) (abi.ABI, string, error) {
	rel := ""
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == "abi" {
			rel = artifact.Path
			break
		}
	}
	if rel == "" {
		rel = "artifacts/abi.json"
		if _, err := os.Stat(st.Path(rel)); err != nil {
			return abi.ABI{}, "", ErrABIMissing
		}
	}
	data, err := os.ReadFile(st.Path(rel))
	if err != nil {
		return abi.ABI{}, "", fmt.Errorf("read ABI artifact: %w", err)
	}
	contractABI, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		return abi.ABI{}, "", fmt.Errorf("parse ABI artifact: %w", err)
	}
	return contractABI, rel, nil
}

func decodeSlot0(contractABI abi.ABI, raw []byte) (Slot0Result, error) {
	var out struct {
		SqrtPriceX96               *big.Int
		Tick                       *big.Int
		ObservationIndex           uint16
		ObservationCardinality     uint16
		ObservationCardinalityNext uint16
		FeeProtocol                uint8
		Unlocked                   bool
	}
	if err := contractABI.UnpackIntoInterface(&out, "slot0", raw); err != nil {
		return Slot0Result{}, fmt.Errorf("decode slot0: %w", err)
	}
	if out.SqrtPriceX96 == nil || out.Tick == nil {
		return Slot0Result{}, fmt.Errorf("decode slot0: numeric result is nil")
	}
	tick, err := int32FromBig(out.Tick)
	if err != nil {
		return Slot0Result{}, fmt.Errorf("decode slot0 tick: %w", err)
	}
	return Slot0Result{
		SqrtPriceX96:               out.SqrtPriceX96.String(),
		Tick:                       tick,
		ObservationIndex:           out.ObservationIndex,
		ObservationCardinality:     out.ObservationCardinality,
		ObservationCardinalityNext: out.ObservationCardinalityNext,
		FeeProtocol:                out.FeeProtocol,
		Unlocked:                   out.Unlocked,
	}, nil
}

func decodeTick(contractABI abi.ABI, raw []byte, tick int32) (TickResult, error) {
	var out struct {
		LiquidityGross                 *big.Int
		LiquidityNet                   *big.Int
		FeeGrowthOutside0X128          *big.Int
		FeeGrowthOutside1X128          *big.Int
		TickCumulativeOutside          *big.Int
		SecondsPerLiquidityOutsideX128 *big.Int
		SecondsOutside                 uint32
		Initialized                    bool
	}
	if err := contractABI.UnpackIntoInterface(&out, "ticks", raw); err != nil {
		return TickResult{}, fmt.Errorf("decode ticks: %w", err)
	}
	for name, value := range map[string]*big.Int{
		"liquidityGross":                 out.LiquidityGross,
		"liquidityNet":                   out.LiquidityNet,
		"feeGrowthOutside0X128":          out.FeeGrowthOutside0X128,
		"feeGrowthOutside1X128":          out.FeeGrowthOutside1X128,
		"tickCumulativeOutside":          out.TickCumulativeOutside,
		"secondsPerLiquidityOutsideX128": out.SecondsPerLiquidityOutsideX128,
	} {
		if value == nil {
			return TickResult{}, fmt.Errorf("decode ticks: %s is nil", name)
		}
	}
	return TickResult{
		Tick:                           tick,
		LiquidityGross:                 out.LiquidityGross.String(),
		LiquidityNet:                   out.LiquidityNet.String(),
		FeeGrowthOutside0X128:          out.FeeGrowthOutside0X128.String(),
		FeeGrowthOutside1X128:          out.FeeGrowthOutside1X128.String(),
		TickCumulativeOutside:          out.TickCumulativeOutside.String(),
		SecondsPerLiquidityOutsideX128: out.SecondsPerLiquidityOutsideX128.String(),
		SecondsOutside:                 out.SecondsOutside,
		Initialized:                    out.Initialized,
	}, nil
}

func int32FromBig(value *big.Int) (int32, error) {
	if !value.IsInt64() {
		return 0, fmt.Errorf("%s does not fit int64", value)
	}
	n := value.Int64()
	if n < -1<<31 || n > 1<<31-1 {
		return 0, fmt.Errorf("%d does not fit int32", n)
	}
	return int32(n), nil
}

type stateArtifact struct {
	SchemaVersion  string         `json:"schema_version"`
	ChainID        int64          `json:"chain_id"`
	Address        string         `json:"address"`
	Block          model.BlockRef `json:"block"`
	Slot0          Slot0Result    `json:"slot0"`
	Liquidity      string         `json:"liquidity"`
	Tick           TickResult     `json:"tick"`
	Immutables     PoolImmutables `json:"immutables"`
	RelationIDs    []string       `json:"relation_ids"`
	ObservationIDs []string       `json:"observation_ids"`
	RetrievedAt    time.Time      `json:"retrieved_at"`
}

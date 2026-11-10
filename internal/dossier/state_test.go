package dossier

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

type fakeCaller struct {
	responses map[string][]byte
	calls     []ethereum.CallMsg
	blocks    []*big.Int
	err       error
}

func (f *fakeCaller) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	f.calls = append(f.calls, call)
	f.blocks = append(f.blocks, new(big.Int).Set(blockNumber))
	if f.err != nil {
		return nil, f.err
	}
	selector := hex.EncodeToString(call.Data[:4])
	return f.responses[selector], nil
}

func runStateFixture(t *testing.T, initialized bool) (*store.Store, common.Address, StateResult, *fakeCaller, error) {
	t.Helper()
	st, address := newResolvedDossier(t)
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	contractABI, _, err := loadABI(manifest, st)
	if err != nil {
		t.Fatal(err)
	}
	slot0Raw, err := contractABI.Methods["slot0"].Outputs.Pack(
		big.NewInt(123456789),
		big.NewInt(-5),
		uint16(1),
		uint16(2),
		uint16(3),
		uint8(0),
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	liquidityRaw, err := contractABI.Methods["liquidity"].Outputs.Pack(big.NewInt(1000))
	if err != nil {
		t.Fatal(err)
	}
	ticksRaw, err := contractABI.Methods["ticks"].Outputs.Pack(
		big.NewInt(2000),
		big.NewInt(-300),
		big.NewInt(10),
		big.NewInt(20),
		big.NewInt(-7),
		big.NewInt(99),
		uint32(42),
		initialized,
	)
	if err != nil {
		t.Fatal(err)
	}
	token0Raw, err := contractABI.Methods["token0"].Outputs.Pack(common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"))
	if err != nil {
		t.Fatal(err)
	}
	token1Raw, err := contractABI.Methods["token1"].Outputs.Pack(common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"))
	if err != nil {
		t.Fatal(err)
	}
	factoryRaw, err := contractABI.Methods["factory"].Outputs.Pack(common.HexToAddress("0x1F98431c8aD98523631AE4a59f267346ea31F984"))
	if err != nil {
		t.Fatal(err)
	}
	feeRaw, err := contractABI.Methods["fee"].Outputs.Pack(big.NewInt(3000))
	if err != nil {
		t.Fatal(err)
	}
	tickSpacingRaw, err := contractABI.Methods["tickSpacing"].Outputs.Pack(big.NewInt(60))
	if err != nil {
		t.Fatal(err)
	}
	maxLiquidityRaw, err := contractABI.Methods["maxLiquidityPerTick"].Outputs.Pack(big.NewInt(123456789))
	if err != nil {
		t.Fatal(err)
	}
	caller := &fakeCaller{responses: map[string][]byte{
		hex.EncodeToString(contractABI.Methods["slot0"].ID):               slot0Raw,
		hex.EncodeToString(contractABI.Methods["liquidity"].ID):           liquidityRaw,
		hex.EncodeToString(contractABI.Methods["ticks"].ID):               ticksRaw,
		hex.EncodeToString(contractABI.Methods["token0"].ID):              token0Raw,
		hex.EncodeToString(contractABI.Methods["token1"].ID):              token1Raw,
		hex.EncodeToString(contractABI.Methods["factory"].ID):             factoryRaw,
		hex.EncodeToString(contractABI.Methods["fee"].ID):                 feeRaw,
		hex.EncodeToString(contractABI.Methods["tickSpacing"].ID):         tickSpacingRaw,
		hex.EncodeToString(contractABI.Methods["maxLiquidityPerTick"].ID): maxLiquidityRaw,
	}}
	result, err := RunState(context.Background(), StateParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-state",
		Caller:  caller,
	})
	return st, address, result, caller, err
}

func TestRunState(t *testing.T) {
	st, address, result, caller, err := runStateFixture(t, true)
	if err != nil {
		t.Fatalf("run state: %v", err)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if result.BlockNumber != manifest.Block.Number || result.BlockHash != manifest.Block.Hash {
		t.Fatalf("block = %d %s, want %d %s", result.BlockNumber, result.BlockHash, manifest.Block.Number, manifest.Block.Hash)
	}
	if result.Slot0.SqrtPriceX96 != "123456789" || result.Slot0.Tick != -5 {
		t.Fatalf("slot0 = %+v", result.Slot0)
	}
	if result.Slot0.ObservationIndex != 1 || result.Slot0.ObservationCardinality != 2 || result.Slot0.ObservationCardinalityNext != 3 {
		t.Fatalf("slot0 = %+v", result.Slot0)
	}
	if !result.Slot0.Unlocked {
		t.Fatalf("slot0 = %+v", result.Slot0)
	}
	if result.Liquidity != "1000" {
		t.Fatalf("liquidity = %s", result.Liquidity)
	}
	if result.Tick.Tick != -5 || result.Tick.LiquidityGross != "2000" || result.Tick.LiquidityNet != "-300" {
		t.Fatalf("tick = %+v", result.Tick)
	}
	if result.Tick.SecondsOutside != 42 || !result.Tick.Initialized {
		t.Fatalf("tick = %+v", result.Tick)
	}
	if result.Immutables.Token0 != "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48" {
		t.Fatalf("token0 = %s", result.Immutables.Token0)
	}
	if result.Immutables.Token1 != "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2" {
		t.Fatalf("token1 = %s", result.Immutables.Token1)
	}
	if result.Immutables.Factory != "0x1F98431c8aD98523631AE4a59f267346ea31F984" {
		t.Fatalf("factory = %s", result.Immutables.Factory)
	}
	if result.Immutables.Fee != 3000 || result.Immutables.TickSpacing != 60 || result.Immutables.MaxLiquidityPerTick != "123456789" {
		t.Fatalf("immutables = %+v", result.Immutables)
	}
	if len(caller.calls) != 9 {
		t.Fatalf("calls = %d", len(caller.calls))
	}
	for _, block := range caller.blocks {
		if block.Uint64() != 123 {
			t.Fatalf("call block = %s", block)
		}
	}
	for _, call := range caller.calls {
		if call.To == nil || *call.To != address {
			t.Fatalf("call to = %v", call.To)
		}
	}

	if len(manifest.ObservationIDs) != 12 {
		t.Fatalf("observations = %v", manifest.ObservationIDs)
	}
	if len(manifest.RelationIDs) != 3 {
		t.Fatalf("relations = %v", manifest.RelationIDs)
	}
	toIDs := make(map[string]bool)
	for _, relationID := range manifest.RelationIDs {
		var relation model.Relation
		if err := st.ReadJSON(relationPath(relationID), &relation); err != nil {
			t.Fatal(err)
		}
		if relation.Type != "onchain_reference" || relation.From != model.ContractVersionID(1, address.Hex(), manifest.Block.Number) {
			t.Fatalf("relation = %+v", relation)
		}
		toIDs[relation.To] = true
	}
	for _, token := range []string{
		"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
		"0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2",
		"0x1F98431c8aD98523631AE4a59f267346ea31F984",
	} {
		if !toIDs[model.ContractID(1, token)] {
			t.Fatalf("missing relation to %s: %v", token, toIDs)
		}
	}
	if contains(manifest.OpenQuestions, "state reads not run") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	if _, err := os.Stat(st.Path(result.Artifact)); err != nil {
		t.Fatalf("state artifact: %v", err)
	}
	var artifact stateArtifact
	if err := st.ReadJSON(result.Artifact, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Block.Number != 123 || artifact.Slot0.Tick != -5 || artifact.Liquidity != "1000" {
		t.Fatalf("artifact = %+v", artifact)
	}

	contractABI, _, err := loadABI(manifest, st)
	if err != nil {
		t.Fatal(err)
	}
	ticksSelector := contractABI.Methods["ticks"].ID
	foundTicks := false
	for _, call := range caller.calls {
		if !bytes.HasPrefix(call.Data, ticksSelector) {
			continue
		}
		foundTicks = true
		args, err := contractABI.Methods["ticks"].Inputs.Unpack(call.Data[4:])
		if err != nil {
			t.Fatal(err)
		}
		if len(args) != 1 || args[0].(*big.Int).Int64() != -5 {
			t.Fatalf("ticks args = %v", args)
		}
	}
	if !foundTicks {
		t.Fatal("ticks call not found")
	}
}

func TestRunStateUninitializedTick(t *testing.T) {
	st, _, result, _, err := runStateFixture(t, false)
	if err != nil {
		t.Fatalf("run state: %v", err)
	}
	if result.Tick.Initialized {
		t.Fatal("tick should be uninitialized")
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	for _, question := range manifest.OpenQuestions {
		if strings.Contains(question, "is not initialized") {
			t.Fatalf("uninitialized tick is chain state, not an open question: %v", manifest.OpenQuestions)
		}
	}
}

func TestRunStateNeedsABI(t *testing.T) {
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	address := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	chain := &fakeChain{header: testHeader(), code: []byte{0x60}}
	if _, err := RunCode(context.Background(), chain, CodeParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-code",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := RunState(context.Background(), StateParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-state",
		Caller:  &fakeCaller{},
	})
	if !errors.Is(err, ErrABIMissing) {
		t.Fatalf("error = %v", err)
	}
}

func TestRunStateCallError(t *testing.T) {
	st, address := newResolvedDossier(t)
	_, err := RunState(context.Background(), StateParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-state",
		Caller:  &fakeCaller{err: errors.New("rpc down")},
	})
	if err == nil {
		t.Fatal("expected call error")
	}
}

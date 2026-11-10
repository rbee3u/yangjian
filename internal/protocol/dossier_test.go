package protocol

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/dossier"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/solc"
	"github.com/rbee3u/yangjian/internal/source"
	"github.com/rbee3u/yangjian/internal/store"
	"github.com/rbee3u/yangjian/internal/target"
)

const testPoolABIJSON = `[
  {"type":"function","name":"slot0","inputs":[],"outputs":[
    {"name":"sqrtPriceX96","type":"uint160"},{"name":"tick","type":"int24"},
    {"name":"observationIndex","type":"uint16"},{"name":"observationCardinality","type":"uint16"},
    {"name":"observationCardinalityNext","type":"uint16"},{"name":"feeProtocol","type":"uint8"},
    {"name":"unlocked","type":"bool"}],"stateMutability":"view"},
  {"type":"function","name":"liquidity","inputs":[],"outputs":[{"name":"","type":"uint128"}],"stateMutability":"view"},
  {"type":"function","name":"ticks","inputs":[{"name":"tick","type":"int24"}],"outputs":[
    {"name":"liquidityGross","type":"uint128"},{"name":"liquidityNet","type":"int128"},
    {"name":"feeGrowthOutside0X128","type":"uint256"},{"name":"feeGrowthOutside1X128","type":"uint256"},
    {"name":"tickCumulativeOutside","type":"int56"},{"name":"secondsPerLiquidityOutsideX128","type":"uint160"},
    {"name":"secondsOutside","type":"uint32"},{"name":"initialized","type":"bool"}],"stateMutability":"view"},
  {"type":"function","name":"token0","inputs":[],"outputs":[{"name":"","type":"address"}],"stateMutability":"view"},
  {"type":"function","name":"token1","inputs":[],"outputs":[{"name":"","type":"address"}],"stateMutability":"view"},
  {"type":"function","name":"factory","inputs":[],"outputs":[{"name":"","type":"address"}],"stateMutability":"view"},
  {"type":"function","name":"fee","inputs":[],"outputs":[{"name":"","type":"uint24"}],"stateMutability":"view"},
  {"type":"function","name":"tickSpacing","inputs":[],"outputs":[{"name":"","type":"int24"}],"stateMutability":"view"},
  {"type":"function","name":"maxLiquidityPerTick","inputs":[],"outputs":[{"name":"","type":"uint128"}],"stateMutability":"view"}
]`

var testPoolABI = mustABI(testPoolABIJSON)

type fakeChain struct {
	header  *types.Header
	factory common.Address
	pool    common.Address
	token0  common.Address
	token1  common.Address
	weth    common.Address
}

func (f *fakeChain) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	return f.header, nil
}

func (f *fakeChain) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	return []byte{0x60, 0x00, 0x60, 0x00, 0xf3}, nil
}

func (f *fakeChain) StorageAt(ctx context.Context, account common.Address, slot common.Hash, blockNumber *big.Int) ([]byte, error) {
	return make([]byte, 32), nil
}

func (f *fakeChain) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if len(call.Data) < 4 {
		return nil, errors.New("call data too short")
	}
	selector := string(call.Data[:4])
	switch selector {
	case string(getPoolABI.Methods["getPool"].ID):
		return getPoolABI.Methods["getPool"].Outputs.Pack(f.pool)
	case string(factoryABI.Methods["factory"].ID):
		return factoryABI.Methods["factory"].Outputs.Pack(f.factory)
	case string(wethABI.Methods["WETH9"].ID):
		return wethABI.Methods["WETH9"].Outputs.Pack(f.weth)
	case string(testPoolABI.Methods["token0"].ID):
		return testPoolABI.Methods["token0"].Outputs.Pack(f.token0)
	case string(testPoolABI.Methods["token1"].ID):
		return testPoolABI.Methods["token1"].Outputs.Pack(f.token1)
	case string(testPoolABI.Methods["fee"].ID):
		return testPoolABI.Methods["fee"].Outputs.Pack(big.NewInt(3000))
	case string(testPoolABI.Methods["tickSpacing"].ID):
		return testPoolABI.Methods["tickSpacing"].Outputs.Pack(big.NewInt(60))
	case string(testPoolABI.Methods["maxLiquidityPerTick"].ID):
		return testPoolABI.Methods["maxLiquidityPerTick"].Outputs.Pack(new(big.Int).Lsh(big.NewInt(1), 100))
	case string(testPoolABI.Methods["slot0"].ID):
		return testPoolABI.Methods["slot0"].Outputs.Pack(
			new(big.Int).Lsh(big.NewInt(1), 96), big.NewInt(0), uint16(0), uint16(0), uint16(0), uint8(0), true)
	case string(testPoolABI.Methods["liquidity"].ID):
		return testPoolABI.Methods["liquidity"].Outputs.Pack(big.NewInt(1234))
	case string(testPoolABI.Methods["ticks"].ID):
		return testPoolABI.Methods["ticks"].Outputs.Pack(
			big.NewInt(10), big.NewInt(-5), big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), uint32(0), false)
	}
	return nil, errors.New("unexpected selector " + hex.EncodeToString(call.Data[:4]))
}

type fakeFetcher struct {
	record source.Record
}

func (f fakeFetcher) Fetch(ctx context.Context, chainID int64, address string) (source.Record, error) {
	record := f.record
	record.Address = address
	return record, nil
}

type failingCompiler struct{}

func (failingCompiler) Ensure(ctx context.Context, version string) (solc.Binary, error) {
	return solc.Binary{}, errors.New("compiler unavailable in test")
}

func (failingCompiler) Compile(ctx context.Context, binary solc.Binary, input []byte) (solc.Output, error) {
	return solc.Output{}, errors.New("compiler unavailable in test")
}

func testSourceRecord(t *testing.T) source.Record {
	t.Helper()
	content := "// SPDX-License-Identifier: MIT\npragma solidity 0.7.6;\ncontract Pool {}\n"
	contentHash := "0x" + hex.EncodeToString(crypto.Keccak256([]byte(content)))
	metadata := map[string]any{
		"compiler": map[string]any{"version": "0.7.6+commit.7338295f"},
		"settings": map[string]any{
			"compilationTarget": map[string]string{"contracts/Pool.sol": "Pool"},
			"evmVersion":        "istanbul",
			"libraries":         map[string]any{},
			"metadata":          map[string]string{"bytecodeHash": "none"},
			"optimizer":         map[string]any{"enabled": true, "runs": 800},
			"remappings":        []string{},
		},
		"sources": map[string]any{"contracts/Pool.sol": map[string]string{"keccak256": contentHash}},
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	stdJSON, err := json.Marshal(map[string]any{
		"language": "Solidity",
		"sources":  map[string]any{"contracts/Pool.sol": map[string]string{"content": content}},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := source.Settings{EVMVersion: "istanbul"}
	settings.Metadata.BytecodeHash = "none"
	settings.Optimizer.Enabled = true
	settings.Optimizer.Runs = 800
	return source.Record{
		ChainID:       1,
		Match:         "match",
		CreationMatch: "match",
		RuntimeMatch:  "match",
		VerifiedAt:    "2026-11-11T00:00:00Z",
		ABI:           json.RawMessage(testPoolABIJSON),
		Metadata:      metadataJSON,
		Sources:       map[string]source.Source{"contracts/Pool.sol": {Content: content}},
		StdJSONInput:  stdJSON,
		Compilation: source.Compilation{
			Compiler:         "solc",
			CompilerVersion:  "0.7.6+commit.7338295f",
			CompilerSettings: settings,
		},
		StorageLayout: json.RawMessage(`{"storage":[],"types":{}}`),
		Raw:           []byte(`{"mock":true}`),
	}
}

func testTarget(factory, npm, router, usdc, weth common.Address) target.Target {
	return target.Target{
		Slug:              "test-protocol",
		Name:              "Test Protocol",
		ChainID:           1,
		SourceURL:         "https://example.invalid/deployments",
		SourceRetrievedAt: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC),
		Contracts: map[string]string{
			"factory":                      factory.Hex(),
			"nonfungible_position_manager": npm.Hex(),
			"swap_router_02":               router.Hex(),
		},
		Tokens:     map[string]string{"USDC": usdc.Hex(), "WETH": weth.Hex()},
		Acceptance: target.Acceptance{PoolPair: []string{"USDC", "WETH"}, FeeTier: 3000, PoolDiscovery: "getPool"},
	}
}

func TestRunDossierBuildsTopologyAndEvidence(t *testing.T) {
	factory := common.HexToAddress("0x1111111111111111111111111111111111111111")
	pool := common.HexToAddress("0x2222222222222222222222222222222222222222")
	npm := common.HexToAddress("0x3333333333333333333333333333333333333333")
	router := common.HexToAddress("0x4444444444444444444444444444444444444444")
	usdc := common.HexToAddress("0x5555555555555555555555555555555555555555")
	weth := common.HexToAddress("0x6666666666666666666666666666666666666666")
	header := &types.Header{
		Number:     big.NewInt(100),
		Time:       uint64(time.Now().Unix()),
		Difficulty: big.NewInt(1),
		GasLimit:   30_000_000,
		Extra:      []byte{0x01},
	}
	chain := &fakeChain{header: header, factory: factory, pool: pool, token0: usdc, token1: weth, weth: weth}
	root := t.TempDir()
	protocolStore := store.New(filepath.Join(root, "protocol"))
	if err := protocolStore.Init(); err != nil {
		t.Fatal(err)
	}
	record := testSourceRecord(t)
	result, err := RunDossier(context.Background(), Params{
		ChainID:      1,
		Target:       testTarget(factory, npm, router, usdc, weth),
		Block:        big.NewInt(100),
		Store:        protocolStore,
		RunID:        "test-run",
		Chain:        chain,
		Fetcher:      fakeFetcher{record: record},
		Compiler:     failingCompiler{},
		ContractRoot: filepath.Join(root, "contracts"),
		MaxContracts: 50,
		MaxRequests:  1000,
		ToolVersion:  "test",
		RegistryRef:  "targets/mainnet.json#test-protocol",
	})
	if err != nil {
		t.Fatalf("run dossier: %v", err)
	}
	if result.Pool != pool.Hex() {
		t.Fatalf("pool = %s", result.Pool)
	}
	if len(result.Contracts) != 4 {
		t.Fatalf("contracts = %d, want 4", len(result.Contracts))
	}
	roles := make(map[string]bool)
	for _, contract := range result.Contracts {
		roles[contract.Role] = true
		if contract.ContractVersionID == "" {
			t.Fatalf("%s has no contract version", contract.Role)
		}
		if contract.Error != "" {
			t.Fatalf("%s: %s", contract.Role, contract.Error)
		}
	}
	for _, role := range []string{"factory", "pool", "nonfungible_position_manager", "router"} {
		if !roles[role] {
			t.Fatalf("missing role %s", role)
		}
	}

	var manifest model.Manifest
	if err := protocolStore.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.DossierType != "protocol" || manifest.Status != "partial" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if len(manifest.ProtocolIDs) != 1 || len(manifest.DeploymentIDs) != 1 {
		t.Fatalf("protocol/deployment refs = %v %v", manifest.ProtocolIDs, manifest.DeploymentIDs)
	}
	if len(manifest.RelationIDs) < 4 {
		t.Fatalf("protocol relation ids = %v", manifest.RelationIDs)
	}
	for _, artifact := range manifest.Artifacts {
		data, err := os.ReadFile(protocolStore.Path(artifact.Path))
		if err != nil {
			t.Fatalf("artifact %s: %v", artifact.Path, err)
		}
		if hash := model.ContentHash(data); hash != artifact.Hash {
			t.Fatalf("artifact %s hash = %s, want %s", artifact.Path, artifact.Hash, hash)
		}
	}

	var relations relationsDocument
	if err := protocolStore.ReadJSON("relations.json", &relations); err != nil {
		t.Fatal(err)
	}
	methods := make(map[string]bool)
	for _, relation := range relations.Relations {
		if method, ok := relation.Attributes["method"].(string); ok {
			methods[method] = true
		}
		if relation.Type == "observed_call" {
			t.Fatalf("unexpected observed_call relation")
		}
	}
	for _, method := range []string{"getPool", "factory", "token0", "token1", "WETH9"} {
		if !methods[method] {
			t.Fatalf("missing relation method %s in %v", method, methods)
		}
	}

	report, err := os.ReadFile(protocolStore.Path("report.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(report)
	for _, want := range []string{"not a security audit", "## Topology", "## Relations", "no `observed_call`", result.DeploymentID} {
		if !strings.Contains(text, want) {
			t.Fatalf("report missing %q", want)
		}
	}
}

func TestRunDossierRespectsContractBudget(t *testing.T) {
	factory := common.HexToAddress("0x1111111111111111111111111111111111111111")
	pool := common.HexToAddress("0x2222222222222222222222222222222222222222")
	npm := common.HexToAddress("0x3333333333333333333333333333333333333333")
	router := common.HexToAddress("0x4444444444444444444444444444444444444444")
	usdc := common.HexToAddress("0x5555555555555555555555555555555555555555")
	weth := common.HexToAddress("0x6666666666666666666666666666666666666666")
	chain := &fakeChain{
		header:  &types.Header{Number: big.NewInt(100), Difficulty: big.NewInt(1), GasLimit: 30_000_000, Extra: []byte{1}},
		factory: factory, pool: pool, token0: usdc, token1: weth, weth: weth,
	}
	root := t.TempDir()
	protocolStore := store.New(filepath.Join(root, "protocol"))
	if err := protocolStore.Init(); err != nil {
		t.Fatal(err)
	}
	result, err := RunDossier(context.Background(), Params{
		ChainID: 1, Target: testTarget(factory, npm, router, usdc, weth), Block: big.NewInt(100),
		Store: protocolStore, RunID: "budget-run", Chain: chain, Fetcher: fakeFetcher{record: testSourceRecord(t)},
		Compiler: failingCompiler{}, ContractRoot: filepath.Join(root, "contracts"), MaxContracts: 1, MaxRequests: 1000,
	})
	if err != nil {
		t.Fatalf("run dossier: %v", err)
	}
	if len(result.Contracts) != 1 || result.Contracts[0].Role != "factory" {
		t.Fatalf("contracts = %+v", result.Contracts)
	}
	found := false
	for _, question := range result.OpenQuestions {
		if strings.Contains(question, "truncated at 1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing truncation question: %v", result.OpenQuestions)
	}
}

func TestRunDossierRecordsMissingPool(t *testing.T) {
	factory := common.HexToAddress("0x1111111111111111111111111111111111111111")
	npm := common.HexToAddress("0x3333333333333333333333333333333333333333")
	router := common.HexToAddress("0x4444444444444444444444444444444444444444")
	usdc := common.HexToAddress("0x5555555555555555555555555555555555555555")
	weth := common.HexToAddress("0x6666666666666666666666666666666666666666")
	chain := &fakeChain{
		header:  &types.Header{Number: big.NewInt(100), Difficulty: big.NewInt(1), GasLimit: 30_000_000, Extra: []byte{1}},
		factory: factory, token0: usdc, token1: weth, weth: weth,
	}
	root := t.TempDir()
	protocolStore := store.New(filepath.Join(root, "protocol"))
	if err := protocolStore.Init(); err != nil {
		t.Fatal(err)
	}
	result, err := RunDossier(context.Background(), Params{
		ChainID: 1, Target: testTarget(factory, npm, router, usdc, weth), Block: big.NewInt(100),
		Store: protocolStore, RunID: "missing-pool", Chain: chain, Fetcher: fakeFetcher{record: testSourceRecord(t)},
		Compiler: failingCompiler{}, ContractRoot: filepath.Join(root, "contracts"), MaxContracts: 50, MaxRequests: 1000,
	})
	if err != nil {
		t.Fatalf("run dossier: %v", err)
	}
	if len(result.Contracts) != 3 {
		t.Fatalf("contracts = %d, want 3 (factory, NPM, router)", len(result.Contracts))
	}
	for _, contract := range result.Contracts {
		if contract.Role == "pool" {
			t.Fatalf("pool dossier should not run when discovery finds no code")
		}
	}
	if !strings.Contains(strings.Join(result.OpenQuestions, "\n"), "did not return a contract with runtime code") {
		t.Fatalf("missing pool question: %v", result.OpenQuestions)
	}
}

func TestTopologyDiagramDeduplicatesContractVersions(t *testing.T) {
	result := Result{
		ChainID: 1,
		Contracts: []ContractSummary{
			{Role: "pool", Address: "0x2222222222222222222222222222222222222222"},
		},
	}
	relations := []model.Relation{
		{
			Type:       "onchain_reference",
			From:       "1:0x2222222222222222222222222222222222222222@100",
			To:         "1:0x5555555555555555555555555555555555555555",
			Attributes: map[string]any{"method": "token0"},
		},
	}
	diagram := topologyDiagram(result, relations)
	if strings.Count(diagram, "Pool 0x2222") != 1 {
		t.Fatalf("pool node duplicated:\n%s", diagram)
	}
	if !strings.Contains(diagram, "-->|token0|") {
		t.Fatalf("missing token0 edge:\n%s", diagram)
	}
}

func TestTopologyDiagramEmpty(t *testing.T) {
	diagram := topologyDiagram(Result{ChainID: 1}, nil)
	if !strings.Contains(diagram, "no onchain_reference relations") {
		t.Fatalf("diagram = %s", diagram)
	}
}

var _ dossier.Compiler = failingCompiler{}

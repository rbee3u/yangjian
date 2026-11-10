package dossier

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

func discoveryChain(t *testing.T, pool common.Address, code []byte) *fakeChain {
	t.Helper()
	encoded, err := getPoolABI.Methods["getPool"].Outputs.Pack(pool)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeChain{
		header: testHeader(),
		code:   code,
		callResponses: map[string][]byte{
			hex.EncodeToString(getPoolABI.Methods["getPool"].ID): encoded,
		},
	}
}

func TestRunDiscover(t *testing.T) {
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	pool := common.HexToAddress("0x3333333333333333333333333333333333333333")
	code := []byte{0x60, 0x80, 0x60, 0x40}
	chain := discoveryChain(t, pool, code)
	factory := common.HexToAddress("0x1111111111111111111111111111111111111111")
	token0 := common.HexToAddress("0x2222222222222222222222222222222222222222")
	token1 := common.HexToAddress("0x4444444444444444444444444444444444444444")
	result, err := RunDiscover(context.Background(), DiscoverParams{
		ChainID: 1,
		Factory: factory,
		Token0:  token0,
		Token1:  token1,
		Fee:     3000,
		Store:   st,
		RunID:   "run-discover",
		Chain:   chain,
	})
	if err != nil {
		t.Fatalf("run discover: %v", err)
	}
	if result.Pool != pool.Hex() || !result.Found {
		t.Fatalf("result = %+v", result)
	}
	if result.PoolCodeSize != len(code) || result.PoolCodeHash == "" {
		t.Fatalf("result = %+v", result)
	}
	if result.Block.Number != 123 {
		t.Fatalf("block = %+v", result.Block)
	}
	if len(result.ObservationIDs) != 3 {
		t.Fatalf("observations = %v", result.ObservationIDs)
	}
	if _, err := os.Stat(st.Path(result.Artifact)); err != nil {
		t.Fatal(err)
	}
	if len(chain.calls) != 1 {
		t.Fatalf("calls = %d", len(chain.calls))
	}
	inputs, err := getPoolABI.Methods["getPool"].Inputs.Unpack(chain.calls[0].Data[4:])
	if err != nil {
		t.Fatal(err)
	}
	if inputs[0].(common.Address) != token0 || inputs[1].(common.Address) != token1 {
		t.Fatalf("call inputs = %v", inputs)
	}
	if inputs[2].(*big.Int).Uint64() != 3000 {
		t.Fatalf("call fee = %v", inputs[2])
	}

	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.DossierType != "discovery" || manifest.Status != "complete" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.Target.Address != factory.Hex() || len(manifest.ObservationIDs) != 3 {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestRunDiscoverNoPool(t *testing.T) {
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	chain := discoveryChain(t, common.Address{}, nil)
	result, err := RunDiscover(context.Background(), DiscoverParams{
		ChainID: 1,
		Factory: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Token0:  common.HexToAddress("0x2222222222222222222222222222222222222222"),
		Token1:  common.HexToAddress("0x4444444444444444444444444444444444444444"),
		Fee:     500,
		Store:   st,
		RunID:   "run-discover",
		Chain:   chain,
	})
	if err != nil {
		t.Fatalf("run discover: %v", err)
	}
	if result.Found || result.Pool != (common.Address{}).Hex() {
		t.Fatalf("result = %+v", result)
	}
	if len(result.ObservationIDs) != 2 {
		t.Fatalf("observations = %v", result.ObservationIDs)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Status != "complete" || len(manifest.OpenQuestions) != 0 {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestRunDiscoverNoCode(t *testing.T) {
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	pool := common.HexToAddress("0x3333333333333333333333333333333333333333")
	chain := discoveryChain(t, pool, nil)
	result, err := RunDiscover(context.Background(), DiscoverParams{
		ChainID: 1,
		Factory: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Token0:  common.HexToAddress("0x2222222222222222222222222222222222222222"),
		Token1:  common.HexToAddress("0x4444444444444444444444444444444444444444"),
		Fee:     3000,
		Store:   st,
		RunID:   "run-discover",
		Chain:   chain,
	})
	if err != nil {
		t.Fatalf("run discover: %v", err)
	}
	if result.Found || result.Pool != pool.Hex() || result.PoolCodeSize != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.ObservationIDs) != 3 {
		t.Fatalf("observations = %v", result.ObservationIDs)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Status != "partial" || !contains(manifest.OpenQuestions, "factory returned an address with no runtime code") {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func TestRunDiscoverValidation(t *testing.T) {
	chain := discoveryChain(t, common.Address{}, nil)
	base := DiscoverParams{
		ChainID: 1,
		Factory: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Token0:  common.HexToAddress("0x2222222222222222222222222222222222222222"),
		Token1:  common.HexToAddress("0x4444444444444444444444444444444444444444"),
		Fee:     3000,
		Store:   store.New(t.TempDir()),
		RunID:   "run",
		Chain:   chain,
	}
	if _, err := RunDiscover(context.Background(), func() DiscoverParams {
		p := base
		p.Fee = 0
		return p
	}()); err == nil {
		t.Fatal("expected fee error")
	}
	if _, err := RunDiscover(context.Background(), func() DiscoverParams {
		p := base
		p.Token1 = p.Token0
		return p
	}()); err == nil {
		t.Fatal("expected token error")
	}
	if _, err := RunDiscover(context.Background(), func() DiscoverParams {
		p := base
		p.Chain = nil
		return p
	}()); err == nil {
		t.Fatal("expected chain error")
	}
}

func TestRunDiscoverCallError(t *testing.T) {
	chain := discoveryChain(t, common.Address{}, nil)
	chain.callErr = errors.New("rpc down")
	_, err := RunDiscover(context.Background(), DiscoverParams{
		ChainID: 1,
		Factory: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Token0:  common.HexToAddress("0x2222222222222222222222222222222222222222"),
		Token1:  common.HexToAddress("0x4444444444444444444444444444444444444444"),
		Fee:     3000,
		Store:   store.New(t.TempDir()),
		RunID:   "run",
		Chain:   chain,
	})
	if err == nil {
		t.Fatal("expected call error")
	}
}

package dossier

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

type fakeChain struct {
	header        *types.Header
	code          []byte
	storage       map[common.Hash][]byte
	callResponses map[string][]byte
	calls         []ethereum.CallMsg
	headerErr     error
	codeErr       error
	storageErr    error
	callErr       error
}

func (f *fakeChain) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if f.headerErr != nil {
		return nil, f.headerErr
	}
	return f.header, nil
}

func (f *fakeChain) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	if f.codeErr != nil {
		return nil, f.codeErr
	}
	return f.code, nil
}

func (f *fakeChain) StorageAt(ctx context.Context, account common.Address, slot common.Hash, blockNumber *big.Int) ([]byte, error) {
	if f.storageErr != nil {
		return nil, f.storageErr
	}
	return f.storage[slot], nil
}

func (f *fakeChain) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	f.calls = append(f.calls, call)
	if f.callErr != nil {
		return nil, f.callErr
	}
	if len(call.Data) < 4 {
		return nil, nil
	}
	return f.callResponses[hex.EncodeToString(call.Data[:4])], nil
}

func testHeader() *types.Header {
	return &types.Header{
		UncleHash:   types.EmptyUncleHash,
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(1),
		Number:      big.NewInt(123),
		GasLimit:    30_000_000,
		Time:        1_700_000_000,
		BaseFee:     big.NewInt(1),
	}
}

func TestRunCode(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	address := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	code := []byte{0x60, 0x80, 0x60, 0x40, 0x52}
	chain := &fakeChain{header: testHeader(), code: code}

	result, err := RunCode(context.Background(), chain, CodeParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-1",
	})
	if err != nil {
		t.Fatalf("run code: %v", err)
	}
	if result.BlockNumber != 123 {
		t.Fatalf("block = %d", result.BlockNumber)
	}
	if result.BytecodeSize != len(code) {
		t.Fatalf("size = %d", result.BytecodeSize)
	}
	if !strings.HasPrefix(result.RuntimeBytecodeHash, "keccak256:") {
		t.Fatalf("hash = %s", result.RuntimeBytecodeHash)
	}
	if len(result.ObservationIDs) != 2 {
		t.Fatalf("observations = %+v", result.ObservationIDs)
	}

	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if manifest.Status != "partial" {
		t.Fatalf("status = %s", manifest.Status)
	}
	if manifest.Target.Address != address.Hex() {
		t.Fatalf("target = %+v", manifest.Target)
	}
	if len(manifest.ContractIDs) != 1 || len(manifest.ObservationIDs) != 2 {
		t.Fatalf("manifest refs = %+v", manifest)
	}
	for _, artifact := range manifest.Artifacts {
		if artifact.Hash == "" || !strings.HasPrefix(artifact.Hash, "sha256:") {
			t.Fatalf("artifact hash = %+v", artifact)
		}
	}
	if len(manifest.OpenQuestions) == 0 {
		t.Fatal("open questions must be recorded")
	}

	var version model.ContractVersion
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == "contract_version" {
			if err := st.ReadJSON(artifact.Path, &version); err != nil {
				t.Fatalf("version: %v", err)
			}
		}
	}
	if version.ProxyType != "unknown" {
		t.Fatalf("proxy type = %s", version.ProxyType)
	}
	if version.SourceRevisionID != "" {
		t.Fatalf("source revision = %s", version.SourceRevisionID)
	}

	second, err := RunCode(context.Background(), chain, CodeParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-2",
	})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if strings.Join(second.ObservationIDs, ",") != strings.Join(result.ObservationIDs, ",") {
		t.Fatalf("observation ids changed: %v != %v", second.ObservationIDs, result.ObservationIDs)
	}

	chain.header.Number = big.NewInt(456)
	third, err := RunCode(context.Background(), chain, CodeParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-3",
	})
	if err != nil {
		t.Fatalf("third run: %v", err)
	}
	if third.ContractVersionID == result.ContractVersionID {
		t.Fatalf("version id did not change: %s", third.ContractVersionID)
	}
	var contract model.Contract
	contractRel := "entities/contracts/1-" + strings.ToLower(address.Hex()) + ".json"
	if err := st.ReadJSON(contractRel, &contract); err != nil {
		t.Fatalf("contract: %v", err)
	}
	if len(contract.Versions) != 2 {
		t.Fatalf("versions = %v", contract.Versions)
	}
}

func TestRunCodeFetchFailure(t *testing.T) {
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	chain := &fakeChain{header: testHeader(), codeErr: errors.New("boom")}
	_, err := RunCode(context.Background(), chain, CodeParams{
		ChainID: 1,
		Address: common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"),
		Store:   st,
		RunID:   "run-1",
	})
	if err == nil || !strings.Contains(err.Error(), "fetch runtime code") {
		t.Fatalf("error = %v", err)
	}
}

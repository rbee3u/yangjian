package chain

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type countingReader struct {
	headerCalls  int
	codeCalls    int
	storageCalls int
	callCalls    int
	code         []byte
	storage      []byte
	call         []byte
}

func (c *countingReader) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	c.headerCalls++
	headerNumber := big.NewInt(100)
	if number != nil {
		headerNumber = new(big.Int).Set(number)
	}
	return &types.Header{
		UncleHash:   types.EmptyUncleHash,
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		Difficulty:  big.NewInt(1),
		Number:      headerNumber,
		GasLimit:    30_000_000,
		Time:        1_700_000_000,
		BaseFee:     big.NewInt(1),
	}, nil
}

func (c *countingReader) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	c.codeCalls++
	return c.code, nil
}

func (c *countingReader) StorageAt(ctx context.Context, account common.Address, slot common.Hash, blockNumber *big.Int) ([]byte, error) {
	c.storageCalls++
	return c.storage, nil
}

func (c *countingReader) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	c.callCalls++
	return c.call, nil
}

func TestCachedClient(t *testing.T) {
	reader := &countingReader{
		code:    []byte{0x60, 0x80},
		storage: []byte{0x01},
		call:    []byte{0x02},
	}
	dir := t.TempDir()
	cached := WithCache(reader, dir, "test", false)
	ctx := context.Background()
	block := big.NewInt(10)
	address := common.HexToAddress("0x1111111111111111111111111111111111111111")
	slot := common.HexToHash("0x01")

	for i := 0; i < 2; i++ {
		if _, err := cached.HeaderByNumber(ctx, block); err != nil {
			t.Fatal(err)
		}
		code, err := cached.CodeAt(ctx, address, block)
		if err != nil || len(code) != 2 {
			t.Fatalf("code = %x, err = %v", code, err)
		}
		storage, err := cached.StorageAt(ctx, address, slot, block)
		if err != nil || len(storage) != 1 {
			t.Fatalf("storage = %x, err = %v", storage, err)
		}
		result, err := cached.CallContract(ctx, ethereum.CallMsg{To: &address, Data: []byte{0x01}}, block)
		if err != nil || len(result) != 1 {
			t.Fatalf("call = %x, err = %v", result, err)
		}
	}
	if reader.headerCalls != 1 || reader.codeCalls != 1 || reader.storageCalls != 1 || reader.callCalls != 1 {
		t.Fatalf("cache misses = headers %d code %d storage %d call %d",
			reader.headerCalls, reader.codeCalls, reader.storageCalls, reader.callCalls)
	}

	refreshed := WithCache(reader, dir, "test", true)
	if _, err := refreshed.HeaderByNumber(ctx, block); err != nil {
		t.Fatal(err)
	}
	if _, err := refreshed.CodeAt(ctx, address, block); err != nil {
		t.Fatal(err)
	}
	if reader.headerCalls != 2 || reader.codeCalls != 2 {
		t.Fatalf("refresh did not bypass cache: headers %d code %d", reader.headerCalls, reader.codeCalls)
	}

	for i := 0; i < 2; i++ {
		if _, err := cached.HeaderByNumber(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	if reader.headerCalls != 4 {
		t.Fatalf("latest header was cached: %d calls", reader.headerCalls)
	}
}

func TestCachedClientSeparatesNamespaces(t *testing.T) {
	reader := &countingReader{code: []byte{0x60}}
	dir := t.TempDir()
	block := big.NewInt(10)
	address := common.HexToAddress("0x1111111111111111111111111111111111111111")
	first := WithCache(reader, dir, "first", false)
	second := WithCache(reader, dir, "second", false)
	if _, err := first.CodeAt(context.Background(), address, block); err != nil {
		t.Fatal(err)
	}
	if _, err := second.CodeAt(context.Background(), address, block); err != nil {
		t.Fatal(err)
	}
	if reader.codeCalls != 2 {
		t.Fatalf("namespaces shared cache entries: %d calls", reader.codeCalls)
	}
}

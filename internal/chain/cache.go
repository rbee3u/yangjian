package chain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type Reader interface {
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	StorageAt(ctx context.Context, account common.Address, slot common.Hash, blockNumber *big.Int) ([]byte, error)
}

type CachedClient struct {
	inner   Reader
	cache   *responseCache
	refresh bool
}

func WithCache(inner Reader, cacheDir, namespace string, refresh bool) *CachedClient {
	return &CachedClient{
		inner:   inner,
		cache:   &responseCache{dir: filepath.Join(cacheDir, "rpc", namespace)},
		refresh: refresh,
	}
}

func CacheNamespace(endpoint string) string {
	sum := sha256.Sum256([]byte(endpoint))
	return hex.EncodeToString(sum[:])[:16]
}

func (c *CachedClient) Close() {
	if closer, ok := c.inner.(interface{ Close() }); ok {
		closer.Close()
	}
}

type rawCaller interface {
	RawCall(ctx context.Context, result any, method string, args ...any) error
}

// RawCall passes through uncached read-only methods. Trace responses are kept
// as dossier artifacts rather than in the RPC response cache.
func (c *CachedClient) RawCall(ctx context.Context, result any, method string, args ...any) error {
	caller, ok := c.inner.(rawCaller)
	if !ok {
		return fmt.Errorf("underlying client does not support raw calls")
	}
	return caller.RawCall(ctx, result, method, args...)
}

func (c *CachedClient) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	if number == nil {
		header, err := c.inner.HeaderByNumber(ctx, nil)
		if err == nil && header != nil && header.Number != nil {
			key := cacheKey("eth_getBlockByNumber", header.Number.String())
			_ = c.cache.put(key, header)
		}
		return header, err
	}
	key := cacheKey("eth_getBlockByNumber", number.String())
	if !c.refresh {
		var header types.Header
		if c.cache.get(key, &header) {
			return &header, nil
		}
	}
	header, err := c.inner.HeaderByNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	_ = c.cache.put(key, header)
	return header, nil
}

func (c *CachedClient) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	key := cacheKey("eth_getCode", account.Hex(), blockLabel(blockNumber))
	if !c.refresh {
		var encoded string
		if c.cache.get(key, &encoded) {
			code, err := hex.DecodeString(encoded)
			if err == nil {
				return code, nil
			}
		}
	}
	code, err := c.inner.CodeAt(ctx, account, blockNumber)
	if err != nil {
		return nil, err
	}
	_ = c.cache.put(key, hex.EncodeToString(code))
	return code, nil
}

func (c *CachedClient) StorageAt(ctx context.Context, account common.Address, slot common.Hash, blockNumber *big.Int) ([]byte, error) {
	key := cacheKey("eth_getStorageAt", account.Hex(), slot.Hex(), blockLabel(blockNumber))
	if !c.refresh {
		var encoded string
		if c.cache.get(key, &encoded) {
			value, err := hex.DecodeString(encoded)
			if err == nil {
				return value, nil
			}
		}
	}
	value, err := c.inner.StorageAt(ctx, account, slot, blockNumber)
	if err != nil {
		return nil, err
	}
	_ = c.cache.put(key, hex.EncodeToString(value))
	return value, nil
}

func (c *CachedClient) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	accessList := ""
	if encoded, err := json.Marshal(call.AccessList); err == nil {
		accessList = string(encoded)
	}
	key := cacheKey("eth_call", blockLabel(blockNumber), call.From.Hex(), addressLabel(call.To),
		hex.EncodeToString(call.Data), bigLabel(call.Value), bigLabel(call.GasPrice),
		bigLabel(call.GasFeeCap), bigLabel(call.GasTipCap), fmt.Sprintf("%d", call.Gas), accessList)
	if !c.refresh {
		var encoded string
		if c.cache.get(key, &encoded) {
			result, err := hex.DecodeString(encoded)
			if err == nil {
				return result, nil
			}
		}
	}
	result, err := c.inner.CallContract(ctx, call, blockNumber)
	if err != nil {
		return nil, err
	}
	_ = c.cache.put(key, hex.EncodeToString(result))
	return result, nil
}

func blockLabel(number *big.Int) string {
	if number == nil {
		return "latest"
	}
	return number.String()
}

func addressLabel(address *common.Address) string {
	if address == nil {
		return ""
	}
	return address.Hex()
}

func bigLabel(value *big.Int) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func cacheKey(parts ...string) string {
	encoded, _ := json.Marshal(parts)
	return string(encoded)
}

type responseCache struct {
	dir string
}

type cacheEntry struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

func (c *responseCache) get(key string, target any) bool {
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return false
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil || entry.Key != key {
		return false
	}
	return json.Unmarshal(entry.Value, target) == nil
}

func (c *responseCache) put(key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	entry, err := json.Marshal(cacheEntry{Key: key, Value: encoded})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	path := c.path(key)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, entry, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func (c *responseCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".json")
}

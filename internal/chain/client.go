package chain

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/rbee3u/yangjian/internal/config"
	"golang.org/x/time/rate"
)

type Client struct {
	eth        *ethclient.Client
	limiter    *rate.Limiter
	timeout    time.Duration
	maxRetries int
}

func Dial(ctx context.Context, profile config.ProviderProfile) (*Client, error) {
	if profile.Endpoint == "" {
		return nil, fmt.Errorf("rpc endpoint is empty")
	}
	eth, err := ethclient.DialContext(ctx, profile.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("dial rpc: %w", err)
	}
	timeout, err := profile.TimeoutDuration()
	if err != nil {
		eth.Close()
		return nil, err
	}
	rps := profile.RateLimitRPS
	if rps <= 0 {
		rps = 5
	}
	burst := profile.Burst
	if burst <= 0 {
		burst = int(rps)
	}
	return &Client{
		eth:        eth,
		limiter:    rate.NewLimiter(rate.Limit(rps), burst),
		timeout:    timeout,
		maxRetries: profile.MaxRetries,
	}, nil
}

func (c *Client) Close() {
	if c.eth != nil {
		c.eth.Close()
	}
}

// ChainID asks the provider which chain it serves. Callers compare it with the
// requested chain before labelling any block or code with that chain ID.
func (c *Client) ChainID(ctx context.Context) (int64, error) {
	var id *big.Int
	err := c.do(ctx, func(callCtx context.Context) error {
		var err error
		id, err = c.eth.ChainID(callCtx)
		return err
	})
	if err != nil {
		return 0, err
	}
	if id == nil {
		return 0, fmt.Errorf("rpc returned an empty chain id")
	}
	return id.Int64(), nil
}

func (c *Client) CodeAt(ctx context.Context, address common.Address, blockNumber *big.Int) ([]byte, error) {
	var code []byte
	err := c.do(ctx, func(callCtx context.Context) error {
		var err error
		code, err = c.eth.CodeAt(callCtx, address, blockNumber)
		return err
	})
	return code, err
}

func (c *Client) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	var header *types.Header
	err := c.do(ctx, func(callCtx context.Context) error {
		var err error
		header, err = c.eth.HeaderByNumber(callCtx, number)
		return err
	})
	return header, err
}

func (c *Client) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	var out []byte
	err := c.do(ctx, func(callCtx context.Context) error {
		var err error
		out, err = c.eth.CallContract(callCtx, call, blockNumber)
		return err
	})
	return out, err
}

func (c *Client) StorageAt(ctx context.Context, address common.Address, slot common.Hash, blockNumber *big.Int) ([]byte, error) {
	var value []byte
	err := c.do(ctx, func(callCtx context.Context) error {
		var err error
		value, err = c.eth.StorageAt(callCtx, address, slot, blockNumber)
		return err
	})
	return value, err
}

// RawCall executes a read-only JSON-RPC method and decodes the result through
// the standard encoding/json rules. Methods outside the read-only allowlist are
// rejected before any network call.
func (c *Client) RawCall(ctx context.Context, result any, method string, args ...any) error {
	if err := CheckReadOnly(method); err != nil {
		return err
	}
	return c.do(ctx, func(callCtx context.Context) error {
		return c.eth.Client().CallContext(callCtx, result, method, args...)
	})
}

func (c *Client) do(ctx context.Context, fn func(context.Context) error) error {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		err := fn(callCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryable(err) || attempt == c.maxRetries {
			return err
		}
		delay := time.Duration(1<<attempt) * 200 * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}

func isRetryable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var httpValue rpc.HTTPError
	if errors.As(err, &httpValue) {
		return httpValue.StatusCode == 429 || httpValue.StatusCode >= 500
	}
	var httpPointer *rpc.HTTPError
	if errors.As(err, &httpPointer) && httpPointer != nil {
		return httpPointer.StatusCode == 429 || httpPointer.StatusCode >= 500
	}
	return false
}

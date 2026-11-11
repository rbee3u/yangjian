package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/chain"
	"github.com/rbee3u/yangjian/internal/config"
	"github.com/rbee3u/yangjian/internal/envelope"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/source"
	"github.com/rbee3u/yangjian/internal/target"
	"github.com/spf13/cobra"
)

type fixtureOptions struct {
	chainID    int64
	targetSlug string
	block      string
	out        string
}

type sourceFixture struct {
	SchemaVersion   string         `json:"schema_version"`
	ChainID         int64          `json:"chain_id"`
	Target          string         `json:"target"`
	Address         string         `json:"address"`
	Block           model.BlockRef `json:"block"`
	RuntimeBytecode string         `json:"runtime_bytecode"`
	Sourcify        source.Record  `json:"sourcify"`
	RecordedAt      time.Time      `json:"recorded_at"`
}

var fixtureGetPoolABI = mustFixtureABI(`[
  {"type":"function","name":"getPool","inputs":[
    {"name":"tokenA","type":"address"},{"name":"tokenB","type":"address"},
    {"name":"fee","type":"uint24"}],"outputs":[
    {"name":"pool","type":"address"}],"stateMutability":"view"}
]`)

func mustFixtureABI(raw string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	return parsed
}

func (a *app) fixturesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fixtures",
		Short: "Developer fixture commands",
	}
	opts := &fixtureOptions{}
	recordCmd := &cobra.Command{
		Use:   "record",
		Short: "Record a live pool-source fixture for offline replay",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runFixtureRecord(opts)
		},
	}
	recordCmd.Flags().Int64Var(&opts.chainID, "chain", 0, "chain ID")
	recordCmd.Flags().StringVar(&opts.targetSlug, "target", "", "target registry slug")
	recordCmd.Flags().StringVar(&opts.block, "block", "latest", "block number or latest")
	recordCmd.Flags().StringVar(&opts.out, "out", "", "fixture path")
	cmd.AddCommand(recordCmd)
	return cmd
}

func (a *app) runFixtureRecord(opts *fixtureOptions) error {
	start := time.Now()
	runID := envelope.NewRunID()
	meta := envelope.Meta{RunID: runID}
	cfg, err := config.Load(a.configPath)
	if err != nil {
		return a.fail("fixtures record", envelope.CodeConfigInvalid, err, start, meta)
	}
	chainID := opts.chainID
	if chainID == 0 {
		chainID = cfg.DefaultChainID
	}
	meta.ChainID = chainID
	if opts.targetSlug == "" {
		return a.fail("fixtures record", envelope.CodeUsage, fmt.Errorf("--target is required"), start, meta)
	}
	registry, err := target.Load(cfg.TargetRegistry)
	if err != nil {
		return a.fail("fixtures record", envelope.CodeTargetNotFound, err, start, meta)
	}
	entry, err := registry.Find(opts.targetSlug)
	if err != nil {
		return a.fail("fixtures record", envelope.CodeTargetNotFound, err, start, meta)
	}
	if entry.ChainID != chainID {
		return a.fail("fixtures record", envelope.CodeUsage, fmt.Errorf("target chain %d does not match %d", entry.ChainID, chainID), start, meta)
	}
	blockNumber, err := parseBlock(opts.block)
	if err != nil {
		return a.fail("fixtures record", envelope.CodeUsage, err, start, meta)
	}
	factoryAddress, err := entry.Address("factory")
	if err != nil {
		return a.fail("fixtures record", envelope.CodeUsage, err, start, meta)
	}
	if len(entry.Acceptance.PoolPair) != 2 {
		return a.fail("fixtures record", envelope.CodeUsage, fmt.Errorf("acceptance.pool_pair must contain two tokens"), start, meta)
	}
	token0, err := entry.Token(entry.Acceptance.PoolPair[0])
	if err != nil {
		return a.fail("fixtures record", envelope.CodeUsage, err, start, meta)
	}
	token1, err := entry.Token(entry.Acceptance.PoolPair[1])
	if err != nil {
		return a.fail("fixtures record", envelope.CodeUsage, err, start, meta)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	client, dialErr := chain.Dial(ctx, cfg.RPCProfile())
	if dialErr != nil {
		return a.fail("fixtures record", chainDialErrorCode(dialErr), dialErr, start, meta)
	}
	defer client.Close()
	actualChainID, err := client.ChainID(ctx)
	if err != nil {
		return a.fail("fixtures record", mapChainError(err), err, start, meta)
	}
	if actualChainID != chainID {
		return a.fail("fixtures record", envelope.CodeConfigInvalid,
			fmt.Errorf("rpc endpoint chain id %d does not match requested chain %d", actualChainID, chainID), start, meta)
	}
	header, err := client.HeaderByNumber(ctx, blockNumber)
	if err != nil {
		return a.fail("fixtures record", mapChainError(err), err, start, meta)
	}
	blockRef := model.BlockRef{ChainID: chainID, Number: header.Number.Uint64(), Hash: header.Hash().Hex()}
	factory := common.HexToAddress(factoryAddress)
	data, err := fixtureGetPoolABI.Pack("getPool", common.HexToAddress(token0), common.HexToAddress(token1), new(big.Int).SetUint64(uint64(entry.Acceptance.FeeTier)))
	if err != nil {
		return a.fail("fixtures record", envelope.CodeInternal, err, start, meta)
	}
	raw, err := client.CallContract(ctx, ethereum.CallMsg{To: &factory, Data: data}, header.Number)
	if err != nil {
		return a.fail("fixtures record", mapChainError(err), err, start, meta)
	}
	var pool common.Address
	if err := fixtureGetPoolABI.UnpackIntoInterface(&pool, "getPool", raw); err != nil {
		return a.fail("fixtures record", envelope.CodeInternal, err, start, meta)
	}
	if pool == (common.Address{}) {
		return a.fail("fixtures record", envelope.CodeTargetNotFound, fmt.Errorf("factory returned the zero pool"), start, meta)
	}
	code, err := client.CodeAt(ctx, pool, header.Number)
	if err != nil {
		return a.fail("fixtures record", mapChainError(err), err, start, meta)
	}
	if len(code) == 0 {
		return a.fail("fixtures record", envelope.CodeTargetNotFound, fmt.Errorf("pool %s has no runtime code", pool.Hex()), start, meta)
	}
	fetcher, err := source.NewSourcify(cfg.Providers.Sourcify)
	if err != nil {
		return a.fail("fixtures record", envelope.CodeConfigInvalid, err, start, meta)
	}
	record, err := fetcher.Fetch(ctx, chainID, pool.Hex())
	if err != nil {
		return a.fail("fixtures record", envelope.CodeSourceNotFound, err, start, meta)
	}
	if record.ChainID != 0 && int64(record.ChainID) != chainID {
		return a.fail("fixtures record", envelope.CodeSourceNotFound,
			fmt.Errorf("sourcify chain id %d does not match %d", record.ChainID, chainID), start, meta)
	}
	if record.Address != "" && common.HexToAddress(record.Address) != pool {
		return a.fail("fixtures record", envelope.CodeSourceNotFound,
			fmt.Errorf("sourcify address %s does not match pool %s", record.Address, pool.Hex()), start, meta)
	}
	out := opts.out
	if out == "" {
		out = filepath.Join("testdata", "fixtures", "sourcify", entry.Slug+"-pool.json")
	}
	fixture := sourceFixture{
		SchemaVersion:   model.SchemaVersion,
		ChainID:         chainID,
		Target:          entry.Slug,
		Address:         pool.Hex(),
		Block:           blockRef,
		RuntimeBytecode: "0x" + hex.EncodeToString(code),
		Sourcify:        record,
		RecordedAt:      time.Now().UTC(),
	}
	dataJSON, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return a.fail("fixtures record", envelope.CodeInternal, err, start, meta)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return a.fail("fixtures record", envelope.CodeInternal, err, start, meta)
	}
	if err := os.WriteFile(out, append(dataJSON, '\n'), 0o644); err != nil {
		return a.fail("fixtures record", envelope.CodeInternal, err, start, meta)
	}
	meta.BlockNumber = &blockRef.Number
	meta.BlockHash = blockRef.Hash
	if !a.json {
		fmt.Fprintf(a.out, "fixture: %s\n", out)
		fmt.Fprintf(a.out, "pool: %s at block %d (%s), %d runtime bytes\n", pool.Hex(), blockRef.Number, blockRef.Hash, len(code))
		return nil
	}
	return a.success("fixtures record", map[string]any{
		"path":  out,
		"pool":  pool.Hex(),
		"block": blockRef,
		"bytes": len(code),
	}, meta, start)
}

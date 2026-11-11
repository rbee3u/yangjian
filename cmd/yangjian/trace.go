package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/config"
	"github.com/rbee3u/yangjian/internal/envelope"
	"github.com/rbee3u/yangjian/internal/store"
	"github.com/rbee3u/yangjian/internal/trace"
	"github.com/rbee3u/yangjian/internal/version"
	"github.com/spf13/cobra"
)

type traceOptions struct {
	chainID int64
	txHash  string
	out     string
	refresh bool
}

func (a *app) traceCommand() *cobra.Command {
	opts := &traceOptions{}
	cmd := &cobra.Command{
		Use:   "trace",
		Short: "Transaction trace commands",
	}
	transactionCmd := &cobra.Command{
		Use:   "transaction",
		Short: "Trace one transaction and record observed_call relations",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runTraceTransaction(opts)
		},
	}
	transactionCmd.Flags().Int64Var(&opts.chainID, "chain", 0, "chain ID")
	transactionCmd.Flags().StringVar(&opts.txHash, "tx", "", "transaction hash")
	transactionCmd.Flags().StringVar(&opts.out, "out", "", "output directory")
	transactionCmd.Flags().BoolVar(&opts.refresh, "refresh", false, "ignore cached RPC responses")
	cmd.AddCommand(transactionCmd)
	return cmd
}

func (a *app) runTraceTransaction(opts *traceOptions) error {
	start := time.Now()
	runID := envelope.NewRunID()
	meta := envelope.Meta{RunID: runID}

	cfg, err := config.Load(a.configPath)
	if err != nil {
		return a.fail("trace transaction", envelope.CodeConfigInvalid, err, start, meta)
	}
	chainID := opts.chainID
	if chainID == 0 {
		chainID = cfg.DefaultChainID
	}
	meta.ChainID = chainID
	if !common.IsHexHash(opts.txHash) {
		return a.fail("trace transaction", envelope.CodeUsage, fmt.Errorf("--tx must be a 32-byte hex hash"), start, meta)
	}
	txHash := common.HexToHash(opts.txHash)
	outDir := opts.out
	if outDir == "" {
		outDir = filepath.Join("research", strconv.FormatInt(chainID, 10), "transactions", strings.ToLower(txHash.Hex()))
	}
	st := store.New(outDir)
	if err := st.Init(); err != nil {
		return a.fail("trace transaction", envelope.CodeInternal, err, start, meta)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	client, dialErr := cachedChain(ctx, cfg, chainID, opts.refresh)
	if dialErr != nil {
		return a.fail("trace transaction", chainDialErrorCode(dialErr), dialErr, start, meta)
	}
	defer client.Close()

	info := version.Current()
	result, err := trace.RunTransaction(ctx, trace.Params{
		ChainID:     chainID,
		TxHash:      txHash,
		Store:       st,
		RunID:       runID,
		RPC:         client,
		ToolVersion: strings.TrimSpace(info.Version + "+" + info.Commit),
	})
	if err != nil {
		code := envelope.CodeInternal
		switch {
		case errors.Is(err, trace.ErrTransactionNotFound):
			code = envelope.CodeTargetNotFound
		case errors.Is(err, trace.ErrTraceUnsupported):
			code = envelope.CodeRPCMethodUnsupported
		case errors.Is(err, trace.ErrTraceInvalid):
			code = envelope.CodeTraceInvalid
		case errors.Is(err, context.DeadlineExceeded):
			code = envelope.CodeRPCTimeout
		default:
			code = mapChainError(err)
		}
		return a.fail("trace transaction", code, err, start, meta)
	}
	meta.BlockNumber = &result.Block.Number
	meta.BlockHash = result.Block.Hash

	var manifest struct {
		Status        string   `json:"status"`
		OpenQuestions []string `json:"open_questions"`
	}
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		return a.fail("trace transaction", envelope.CodeInternal, err, start, meta)
	}
	meta.Status = manifest.Status

	if !a.json {
		fmt.Fprintf(a.out, "transaction: %s\n", result.TransactionHash)
		fmt.Fprintf(a.out, "block: %d (%s), status %d, gas used %d\n", result.Block.Number, result.Block.Hash, result.Status, result.GasUsed)
		fmt.Fprintf(a.out, "trace: %s via %s, %d calls\n", result.TraceProvider, result.TraceMethod, result.CallCount)
		fmt.Fprintf(a.out, "observed calls: %d\n", len(result.RelationIDs))
		fmt.Fprintf(a.out, "report: %s\n", filepath.Join(outDir, result.ReportPath))
		if len(manifest.OpenQuestions) > 0 {
			fmt.Fprintf(a.out, "open questions: %d\n", len(manifest.OpenQuestions))
			for _, question := range manifest.OpenQuestions {
				fmt.Fprintf(a.out, "  - %s\n", question)
			}
		}
		if manifest.Status == "complete" {
			return nil
		}
		return &exitError{code: 3}
	}

	return a.success("trace transaction", map[string]any{
		"out":         outDir,
		"manifest":    filepath.Join(outDir, "manifest.json"),
		"transaction": result,
	}, meta, start)
}

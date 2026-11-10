package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rbee3u/yangjian/internal/config"
	"github.com/rbee3u/yangjian/internal/dossier"
	"github.com/rbee3u/yangjian/internal/envelope"
	"github.com/rbee3u/yangjian/internal/protocol"
	"github.com/rbee3u/yangjian/internal/solc"
	"github.com/rbee3u/yangjian/internal/source"
	"github.com/rbee3u/yangjian/internal/store"
	"github.com/rbee3u/yangjian/internal/target"
	"github.com/rbee3u/yangjian/internal/version"
	"github.com/spf13/cobra"
)

type protocolOptions struct {
	chainID      int64
	targetSlug   string
	block        string
	out          string
	contractsOut string
	maxContracts int
	maxRequests  int
	refresh      bool
}

func (a *app) protocolCommand() *cobra.Command {
	opts := &protocolOptions{}
	cmd := &cobra.Command{
		Use:   "protocol",
		Short: "Protocol research commands",
	}
	dossierCmd := &cobra.Command{
		Use:   "dossier",
		Short: "Build a protocol dossier from a target registry entry",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runProtocolDossier(opts)
		},
	}
	dossierCmd.Flags().Int64Var(&opts.chainID, "chain", 0, "chain ID")
	dossierCmd.Flags().StringVar(&opts.targetSlug, "target", "", "target registry slug")
	dossierCmd.Flags().StringVar(&opts.block, "block", "latest", "block number or latest")
	dossierCmd.Flags().StringVar(&opts.out, "out", "", "output directory")
	dossierCmd.Flags().StringVar(&opts.contractsOut, "contracts-root", "", "contract dossier root (default research/<chain>/contracts)")
	dossierCmd.Flags().IntVar(&opts.maxContracts, "max-contracts", 50, "maximum contract dossiers")
	dossierCmd.Flags().IntVar(&opts.maxRequests, "max-requests", 1000, "maximum observation requests across contract dossiers")
	dossierCmd.Flags().BoolVar(&opts.refresh, "refresh", false, "ignore cached provider responses")
	cmd.AddCommand(dossierCmd)
	return cmd
}

func (a *app) runProtocolDossier(opts *protocolOptions) error {
	start := time.Now()
	runID := envelope.NewRunID()
	meta := envelope.Meta{RunID: runID}

	cfg, err := config.Load(a.configPath)
	if err != nil {
		return a.fail("protocol dossier", envelope.CodeConfigInvalid, err, start, meta)
	}
	chainID := opts.chainID
	if chainID == 0 {
		chainID = cfg.DefaultChainID
	}
	meta.ChainID = chainID
	if opts.targetSlug == "" {
		return a.fail("protocol dossier", envelope.CodeUsage, fmt.Errorf("--target is required"), start, meta)
	}
	blockNumber, err := parseBlock(opts.block)
	if err != nil {
		return a.fail("protocol dossier", envelope.CodeUsage, err, start, meta)
	}
	registry, err := target.Load(cfg.TargetRegistry)
	if err != nil {
		return a.fail("protocol dossier", envelope.CodeTargetNotFound, err, start, meta)
	}
	targetEntry, err := registry.Find(opts.targetSlug)
	if err != nil {
		return a.fail("protocol dossier", envelope.CodeTargetNotFound, err, start, meta)
	}
	if targetEntry.ChainID != chainID {
		return a.fail("protocol dossier", envelope.CodeUsage,
			fmt.Errorf("target chain %d does not match requested chain %d", targetEntry.ChainID, chainID), start, meta)
	}
	outDir := opts.out
	if outDir == "" {
		outDir = filepath.Join("research", strconv.FormatInt(chainID, 10), "protocols", targetEntry.Slug)
	}
	contractsRoot := opts.contractsOut
	if contractsRoot == "" {
		contractsRoot = filepath.Join("research", strconv.FormatInt(chainID, 10), "contracts")
	}
	st := store.New(outDir)
	if err := st.Init(); err != nil {
		return a.fail("protocol dossier", envelope.CodeInternal, err, start, meta)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
	defer cancel()
	client, dialErr := cachedChain(ctx, cfg, chainID, opts.refresh)
	if dialErr != nil {
		return a.fail("protocol dossier", envelope.CodeRPCUnavailable, dialErr, start, meta)
	}
	defer client.Close()
	fetcher, fetchErr := source.NewSourcifyWithCache(cfg.Providers.Sourcify, cfg.CacheDir, opts.refresh)
	if fetchErr != nil {
		return a.fail("protocol dossier", envelope.CodeConfigInvalid, fetchErr, start, meta)
	}
	compiler, compilerErr := solc.NewManager(cfg.Solc, cfg.CacheDir)
	if compilerErr != nil {
		return a.fail("protocol dossier", envelope.CodeConfigInvalid, compilerErr, start, meta)
	}
	params := protocol.Params{
		ChainID:      chainID,
		Target:       *targetEntry,
		Block:        blockNumber,
		Store:        st,
		RunID:        runID,
		Chain:        client,
		Fetcher:      fetcher,
		Compiler:     compiler,
		ContractRoot: contractsRoot,
		MaxContracts: opts.maxContracts,
		MaxRequests:  opts.maxRequests,
		ToolVersion:  versionString(),
		RegistryRef:  cfg.TargetRegistry + "#" + targetEntry.Slug,
	}
	if keyEnv := cfg.Providers.Etherscan.APIKeyEnv; keyEnv != "" {
		if apiKey := os.Getenv(keyEnv); apiKey != "" {
			crossCheck, crossErr := source.NewEtherscanWithCache(cfg.Providers.Etherscan, apiKey, cfg.CacheDir, opts.refresh)
			if crossErr != nil {
				return a.fail("protocol dossier", envelope.CodeConfigInvalid, crossErr, start, meta)
			}
			params.CrossCheck = crossCheck
			params.CrossCheckName = "etherscan"
		}
	}
	result, err := protocol.RunDossier(ctx, params)
	if err != nil {
		code := envelope.CodeInternal
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			code = envelope.CodeRPCTimeout
		case errors.Is(err, dossier.ErrDossierNotInitialized), errors.Is(err, dossier.ErrSourceRevisionMissing):
			code = envelope.CodeUsage
		default:
			code = mapChainError(err)
		}
		return a.fail("protocol dossier", code, err, start, meta)
	}
	meta.BlockNumber = &result.Block.Number
	meta.BlockHash = result.Block.Hash

	var manifest struct {
		Status        string   `json:"status"`
		OpenQuestions []string `json:"open_questions"`
	}
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		return a.fail("protocol dossier", envelope.CodeInternal, err, start, meta)
	}
	meta.Status = manifest.Status

	if !a.json {
		fmt.Fprintf(a.out, "protocol: %s (%s)\n", result.ProtocolID, outDir)
		fmt.Fprintf(a.out, "deployment: %s at block %d (%s)\n", result.DeploymentID, result.Block.Number, result.Block.Hash)
		fmt.Fprintf(a.out, "pool: %s (token0 %s, token1 %s, fee %d)\n", orPlaceholder(result.Pool), result.Token0, result.Token1, result.Fee)
		fmt.Fprintln(a.out, "contracts:")
		for _, contract := range result.Contracts {
			fmt.Fprintf(a.out, "  %s %s source=%s provenance=%s proxy=%s dossier=%s\n",
				contract.Role, contract.Address, orPlaceholder(contract.SourceRevisionID),
				orPlaceholder(contract.ProvenanceLevel), orPlaceholder(contract.ProxyType), contract.DossierPath)
		}
		fmt.Fprintf(a.out, "relations: %d protocol, report: %s\n", len(result.RelationIDs), filepath.Join(outDir, result.ReportPath))
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

	return a.success("protocol dossier", map[string]any{
		"out":      outDir,
		"manifest": filepath.Join(outDir, "manifest.json"),
		"protocol": result,
	}, meta, start)
}

func orPlaceholder(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func versionString() string {
	info := version.Current()
	return strings.TrimSpace(info.Version + "+" + info.Commit)
}

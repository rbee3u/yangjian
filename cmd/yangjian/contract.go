package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/rbee3u/yangjian/internal/chain"
	"github.com/rbee3u/yangjian/internal/config"
	"github.com/rbee3u/yangjian/internal/dossier"
	"github.com/rbee3u/yangjian/internal/envelope"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/solc"
	"github.com/rbee3u/yangjian/internal/source"
	"github.com/rbee3u/yangjian/internal/store"
	"github.com/rbee3u/yangjian/internal/target"
	"github.com/spf13/cobra"
)

type contractOptions struct {
	chainID      int64
	address      string
	targetSlug   string
	contractName string
	block        string
	out          string
	stage        string
	refresh      bool
}

type discoverOptions struct {
	chainID    int64
	factory    string
	targetSlug string
	token0     string
	token1     string
	fee        uint32
	block      string
	out        string
	refresh    bool
}

func (a *app) contractCommand() *cobra.Command {
	opts := &contractOptions{}
	cmd := &cobra.Command{
		Use:   "contract",
		Short: "Contract research commands",
	}
	dossierCmd := &cobra.Command{
		Use:   "dossier",
		Short: "Build a contract dossier",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runContractDossier(opts)
		},
	}
	dossierCmd.Flags().Int64Var(&opts.chainID, "chain", 0, "chain ID")
	dossierCmd.Flags().StringVar(&opts.address, "address", "", "contract address")
	dossierCmd.Flags().StringVar(&opts.targetSlug, "target", "", "target registry slug")
	dossierCmd.Flags().StringVar(&opts.contractName, "contract", "factory", "contract name in the target registry")
	dossierCmd.Flags().StringVar(&opts.block, "block", "latest", "block number or latest")
	dossierCmd.Flags().StringVar(&opts.out, "out", "", "output directory")
	dossierCmd.Flags().StringVar(&opts.stage, "stage", "", "run one stage: code, source, provenance, analyze, state, report")
	dossierCmd.Flags().BoolVar(&opts.refresh, "refresh", false, "ignore cached provider responses")
	cmd.AddCommand(dossierCmd)

	discoverOpts := &discoverOptions{}
	discoverCmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover a contract address from a factory",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runContractDiscover(discoverOpts)
		},
	}
	discoverCmd.Flags().Int64Var(&discoverOpts.chainID, "chain", 0, "chain ID")
	discoverCmd.Flags().StringVar(&discoverOpts.factory, "factory", "", "factory address")
	discoverCmd.Flags().StringVar(&discoverOpts.targetSlug, "target", "", "target registry slug")
	discoverCmd.Flags().StringVar(&discoverOpts.token0, "token0", "", "token0 address or registry symbol")
	discoverCmd.Flags().StringVar(&discoverOpts.token1, "token1", "", "token1 address or registry symbol")
	discoverCmd.Flags().Uint32Var(&discoverOpts.fee, "fee", 0, "fee tier in hundredths of a bip")
	discoverCmd.Flags().StringVar(&discoverOpts.block, "block", "latest", "block number or latest")
	discoverCmd.Flags().StringVar(&discoverOpts.out, "out", "", "output directory")
	discoverCmd.Flags().BoolVar(&discoverOpts.refresh, "refresh", false, "ignore cached provider responses")
	cmd.AddCommand(discoverCmd)
	return cmd
}

func (a *app) runContractDiscover(opts *discoverOptions) error {
	start := time.Now()
	runID := envelope.NewRunID()
	meta := envelope.Meta{RunID: runID}

	cfg, err := config.Load(a.configPath)
	if err != nil {
		return a.fail("contract discover", envelope.CodeConfigInvalid, err, start, meta)
	}
	chainID := opts.chainID
	if chainID == 0 {
		chainID = cfg.DefaultChainID
	}
	meta.ChainID = chainID

	factory, token0, token1, err := resolveDiscovery(cfg, chainID, opts)
	if err != nil {
		return a.fail("contract discover", envelope.CodeUsage, err, start, meta)
	}
	blockNumber, err := parseBlock(opts.block)
	if err != nil {
		return a.fail("contract discover", envelope.CodeUsage, err, start, meta)
	}
	outDir := opts.out
	if outDir == "" {
		outDir = filepath.Join("research", strconv.FormatInt(chainID, 10), "discoveries",
			strings.ToLower(factory.Hex()),
			fmt.Sprintf("%s-%s-%d", strings.ToLower(token0.Hex()), strings.ToLower(token1.Hex()), opts.fee))
	}
	st := store.New(outDir)
	if err := st.Init(); err != nil {
		return a.fail("contract discover", envelope.CodeInternal, err, start, meta)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, dialErr := cachedChain(ctx, cfg, chainID, opts.refresh)
	if dialErr != nil {
		return a.fail("contract discover", envelope.CodeRPCUnavailable, dialErr, start, meta)
	}
	result, err := dossier.RunDiscover(ctx, dossier.DiscoverParams{
		ChainID: chainID,
		Factory: factory,
		Token0:  token0,
		Token1:  token1,
		Fee:     opts.fee,
		Block:   blockNumber,
		Store:   st,
		RunID:   runID,
		Chain:   client,
	})
	client.Close()
	if err != nil {
		return a.fail("contract discover", mapChainError(err), err, start, meta)
	}
	meta.BlockNumber = &result.Block.Number
	meta.BlockHash = result.Block.Hash

	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		return a.fail("contract discover", envelope.CodeInternal, err, start, meta)
	}
	meta.Status = manifest.Status

	if !a.json {
		fmt.Fprintf(a.out, "discovery: %s\n", outDir)
		if result.Found {
			fmt.Fprintf(a.out, "pool: %s (%d bytes) at block %d (%s)\n", result.Pool, result.PoolCodeSize, result.Block.Number, result.Block.Hash)
		} else {
			fmt.Fprintf(a.out, "pool: not found at block %d (%s)\n", result.Block.Number, result.Block.Hash)
		}
		if manifest.Status == "complete" {
			return nil
		}
		return &exitError{code: 3}
	}

	return a.success("contract discover", map[string]any{
		"out":       outDir,
		"manifest":  filepath.Join(outDir, "manifest.json"),
		"discovery": result,
	}, meta, start)
}

func resolveDiscovery(cfg config.Config, chainID int64, opts *discoverOptions) (common.Address, common.Address, common.Address, error) {
	if opts.token0 == "" || opts.token1 == "" {
		return common.Address{}, common.Address{}, common.Address{}, fmt.Errorf("--token0 and --token1 are required")
	}
	if opts.fee == 0 || opts.fee > 0xFFFFFF {
		return common.Address{}, common.Address{}, common.Address{}, fmt.Errorf("--fee must be in [1, 16777215]")
	}
	if (opts.factory == "") == (opts.targetSlug == "") {
		return common.Address{}, common.Address{}, common.Address{}, fmt.Errorf("exactly one of --factory or --target is required")
	}

	var targetEntry *target.Target
	var factory common.Address
	if opts.factory != "" {
		normalized, err := model.NormalizeAddress(opts.factory)
		if err != nil {
			return common.Address{}, common.Address{}, common.Address{}, err
		}
		factory = common.HexToAddress(normalized)
	} else {
		registry, err := target.Load(cfg.TargetRegistry)
		if err != nil {
			return common.Address{}, common.Address{}, common.Address{}, err
		}
		entry, err := registry.Find(opts.targetSlug)
		if err != nil {
			return common.Address{}, common.Address{}, common.Address{}, err
		}
		if entry.ChainID != chainID {
			return common.Address{}, common.Address{}, common.Address{}, fmt.Errorf("target chain %d does not match requested chain %d", entry.ChainID, chainID)
		}
		address, err := entry.Address("factory")
		if err != nil {
			return common.Address{}, common.Address{}, common.Address{}, err
		}
		factory = common.HexToAddress(address)
		targetEntry = entry
	}

	resolveToken := func(input string) (common.Address, error) {
		if common.IsHexAddress(input) {
			normalized, err := model.NormalizeAddress(input)
			if err != nil {
				return common.Address{}, err
			}
			return common.HexToAddress(normalized), nil
		}
		if targetEntry == nil {
			return common.Address{}, fmt.Errorf("token %q is not an address; provide --target or explicit token addresses", input)
		}
		address, err := targetEntry.Token(input)
		if err != nil {
			return common.Address{}, err
		}
		return common.HexToAddress(address), nil
	}
	token0, err := resolveToken(opts.token0)
	if err != nil {
		return common.Address{}, common.Address{}, common.Address{}, err
	}
	token1, err := resolveToken(opts.token1)
	if err != nil {
		return common.Address{}, common.Address{}, common.Address{}, err
	}
	return factory, token0, token1, nil
}

func (a *app) runContractDossier(opts *contractOptions) error {
	start := time.Now()
	runID := envelope.NewRunID()
	meta := envelope.Meta{RunID: runID}

	cfg, err := config.Load(a.configPath)
	if err != nil {
		return a.fail("contract dossier", envelope.CodeConfigInvalid, err, start, meta)
	}

	chainID := opts.chainID
	if chainID == 0 {
		chainID = cfg.DefaultChainID
	}
	meta.ChainID = chainID

	address, err := resolveAddress(cfg, chainID, opts)
	if err != nil {
		return a.fail("contract dossier", envelope.CodeTargetNotFound, err, start, meta)
	}

	blockNumber, err := parseBlock(opts.block)
	if err != nil {
		return a.fail("contract dossier", envelope.CodeUsage, err, start, meta)
	}

	outDir := opts.out
	if outDir == "" {
		outDir = filepath.Join("research", strconv.FormatInt(chainID, 10), "contracts", strings.ToLower(address.Hex()))
	}

	stage := opts.stage
	if stage != "" && stage != "code" && stage != "source" && stage != "provenance" && stage != "analyze" && stage != "state" && stage != "report" {
		return a.fail("contract dossier", envelope.CodeStageNotImplemented, fmt.Errorf("stage %q is not implemented yet", stage), start, meta)
	}

	st := store.New(outDir)
	if err := st.Init(); err != nil {
		return a.fail("contract dossier", envelope.CodeInternal, err, start, meta)
	}

	stages := make([]string, 0, 6)
	var codeResult dossier.CodeResult
	var sourceResult dossier.SourceResult
	var provenanceResult dossier.ProvenanceResult
	var analyzeResult dossier.AnalyzeResult
	var stateResult dossier.StateResult
	var reportResult dossier.ReportResult

	if stage == "" || stage == "code" {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		client, dialErr := cachedChain(ctx, cfg, chainID, opts.refresh)
		if dialErr != nil {
			cancel()
			return a.fail("contract dossier", envelope.CodeRPCUnavailable, dialErr, start, meta)
		}
		codeResult, err = dossier.RunCode(ctx, client, dossier.CodeParams{
			ChainID: chainID,
			Address: address,
			Block:   blockNumber,
			Store:   st,
			RunID:   runID,
		})
		client.Close()
		cancel()
		if err != nil {
			return a.fail("contract dossier", mapChainError(err), err, start, meta)
		}
		stages = append(stages, "code")
		meta.BlockNumber = &codeResult.BlockNumber
		meta.BlockHash = codeResult.BlockHash
	}

	if stage == "" || stage == "source" {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		fetcher, fetchErr := source.NewSourcifyWithCache(cfg.Providers.Sourcify, cfg.CacheDir, opts.refresh)
		if fetchErr != nil {
			cancel()
			return a.fail("contract dossier", envelope.CodeConfigInvalid, fetchErr, start, meta)
		}
		sourceParams := dossier.SourceParams{
			ChainID: chainID,
			Address: address,
			Store:   st,
			RunID:   runID,
			Fetcher: fetcher,
		}
		if keyEnv := cfg.Providers.Etherscan.APIKeyEnv; keyEnv != "" {
			if apiKey := os.Getenv(keyEnv); apiKey != "" {
				crossCheck, crossErr := source.NewEtherscanWithCache(cfg.Providers.Etherscan, apiKey, cfg.CacheDir, opts.refresh)
				if crossErr != nil {
					cancel()
					return a.fail("contract dossier", envelope.CodeConfigInvalid, crossErr, start, meta)
				}
				sourceParams.CrossCheck = crossCheck
				sourceParams.CrossCheckName = "etherscan"
			} else {
				sourceParams.CrossCheckSkipReason = keyEnv + " is not set"
			}
		} else {
			sourceParams.CrossCheckSkipReason = "no Etherscan API key environment variable is configured"
		}
		sourceResult, err = dossier.RunSource(ctx, sourceParams)
		cancel()
		if err != nil {
			code := envelope.CodeInternal
			switch {
			case errors.Is(err, dossier.ErrDossierNotInitialized):
				code = envelope.CodeUsage
			case errors.Is(err, source.ErrNotFound):
				code = envelope.CodeSourceNotFound
			}
			return a.fail("contract dossier", code, err, start, meta)
		}
		stages = append(stages, "source")
		if meta.BlockNumber == nil {
			meta.BlockNumber = &sourceResult.BlockNumber
		}
		if meta.BlockHash == "" {
			meta.BlockHash = sourceResult.BlockHash
		}
	}

	if stage == "" || stage == "provenance" {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		compiler, compilerErr := solc.NewManager(cfg.Solc, cfg.CacheDir)
		if compilerErr != nil {
			cancel()
			return a.fail("contract dossier", envelope.CodeConfigInvalid, compilerErr, start, meta)
		}
		provenanceResult, err = dossier.RunProvenance(ctx, dossier.ProvenanceParams{
			ChainID:  chainID,
			Address:  address,
			Store:    st,
			RunID:    runID,
			Compiler: compiler,
		})
		cancel()
		if err != nil {
			return a.fail("contract dossier", provenanceErrorCode(err), err, start, meta)
		}
		stages = append(stages, "provenance")
		sourceResult.ProvenanceLevel = provenanceResult.ProvenanceLevel
		sourceResult.Correspondence = provenanceResult.Correspondence
		if meta.BlockNumber == nil || meta.BlockHash == "" {
			var manifest model.Manifest
			if readErr := st.ReadJSON("manifest.json", &manifest); readErr == nil {
				if meta.BlockNumber == nil {
					meta.BlockNumber = &manifest.Block.Number
				}
				if meta.BlockHash == "" {
					meta.BlockHash = manifest.Block.Hash
				}
			}
		}
	}

	if stage == "" || stage == "analyze" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		client, dialErr := cachedChain(ctx, cfg, chainID, opts.refresh)
		if dialErr != nil {
			cancel()
			return a.fail("contract dossier", envelope.CodeRPCUnavailable, dialErr, start, meta)
		}
		analyzeResult, err = dossier.RunAnalyze(ctx, dossier.AnalyzeParams{
			ChainID: chainID,
			Address: address,
			Store:   st,
			RunID:   runID,
			Reader:  client,
		})
		client.Close()
		cancel()
		if err != nil {
			code := envelope.CodeInternal
			switch {
			case errors.Is(err, dossier.ErrDossierNotInitialized), errors.Is(err, dossier.ErrCodeObservationMissing):
				code = envelope.CodeUsage
			default:
				code = mapChainError(err)
			}
			return a.fail("contract dossier", code, err, start, meta)
		}
		stages = append(stages, "analyze")
	}

	if stage == "" || stage == "state" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		client, dialErr := cachedChain(ctx, cfg, chainID, opts.refresh)
		if dialErr != nil {
			cancel()
			return a.fail("contract dossier", envelope.CodeRPCUnavailable, dialErr, start, meta)
		}
		stateResult, err = dossier.RunState(ctx, dossier.StateParams{
			ChainID: chainID,
			Address: address,
			Store:   st,
			RunID:   runID,
			Caller:  client,
		})
		client.Close()
		cancel()
		if err != nil {
			code := envelope.CodeInternal
			switch {
			case errors.Is(err, dossier.ErrDossierNotInitialized), errors.Is(err, dossier.ErrABIMissing):
				code = envelope.CodeUsage
			default:
				code = mapChainError(err)
			}
			return a.fail("contract dossier", code, err, start, meta)
		}
		stages = append(stages, "state")
		if meta.BlockNumber == nil {
			meta.BlockNumber = &stateResult.BlockNumber
		}
		if meta.BlockHash == "" {
			meta.BlockHash = stateResult.BlockHash
		}
	}

	if stage == "" || stage == "report" {
		reportResult, err = dossier.RunReport(dossier.ReportParams{
			ChainID: chainID,
			Address: address,
			Store:   st,
			RunID:   runID,
		})
		if err != nil {
			code := envelope.CodeInternal
			if errors.Is(err, dossier.ErrDossierNotInitialized) {
				code = envelope.CodeUsage
			}
			return a.fail("contract dossier", code, err, start, meta)
		}
		stages = append(stages, "report")
	}

	meta.Status = "partial"
	if reportResult.ReportPath != "" && len(reportResult.OpenQuestions) == 0 {
		meta.Status = "complete"
	}

	if !a.json {
		fmt.Fprintf(a.out, "dossier: %s\n", outDir)
		if len(stages) > 0 {
			fmt.Fprintf(a.out, "stages: %s\n", strings.Join(stages, ","))
		}
		if codeResult.ContractID != "" {
			fmt.Fprintf(a.out, "code: block %d (%s), %d bytes\n", codeResult.BlockNumber, codeResult.BlockHash, codeResult.BytecodeSize)
		}
		if sourceResult.SourceRevisionID != "" {
			fmt.Fprintf(a.out, "source: %s, compiler %s, %d files, provenance %s (%s)\n",
				sourceResult.RegistryMatch, sourceResult.Compiler, sourceResult.SourceCount,
				sourceResult.ProvenanceLevel, sourceResult.Correspondence)
		}
		if sourceResult.CrossCheck != nil {
			fmt.Fprintf(a.out, "cross-check: %s %s", sourceResult.CrossCheck.Provider, sourceResult.CrossCheck.Status)
			if len(sourceResult.CrossCheck.Differences) > 0 {
				fmt.Fprintf(a.out, " (%s)", strings.Join(sourceResult.CrossCheck.Differences, "; "))
			}
			fmt.Fprintln(a.out)
		}
		if provenanceResult.Compiler != "" {
			fmt.Fprintf(a.out, "provenance: P2 match=%t, compiler %s, %d immutables masked, %d bytes compared\n",
				provenanceResult.Match, provenanceResult.Compiler, provenanceResult.ImmutablesMasked, provenanceResult.ComparedBytes)
		}
		if analyzeResult.Artifact != "" {
			fmt.Fprintf(a.out, "analyze: proxy type %s", analyzeResult.ProxyType)
			if analyzeResult.ImplementationAddress != "" {
				fmt.Fprintf(a.out, ", implementation %s", analyzeResult.ImplementationAddress)
			}
			if analyzeResult.BeaconAddress != "" {
				fmt.Fprintf(a.out, ", beacon %s", analyzeResult.BeaconAddress)
			}
			fmt.Fprintln(a.out)
		}
		if analyzeResult.Static != nil {
			fmt.Fprintf(a.out, "static: %s, %d contracts, %d storage entries, %d methods, %d call sites (%d delegatecall, %d library-delegatecall)\n",
				analyzeResult.Static.CompilationTarget, analyzeResult.Static.Contracts,
				analyzeResult.Static.StorageEntries, analyzeResult.Static.Methods,
				analyzeResult.Static.CallSites, analyzeResult.Static.Delegatecalls,
				analyzeResult.Static.LibraryDelegate)
		} else if len(analyzeResult.StaticUnavailable) > 0 {
			fmt.Fprintf(a.out, "static: unavailable (%s)\n", strings.Join(analyzeResult.StaticUnavailable, "; "))
		}
		if stateResult.Artifact != "" {
			fmt.Fprintf(a.out, "state: slot0 tick=%d, liquidity=%s, tick %d initialized=%t\n",
				stateResult.Slot0.Tick, stateResult.Liquidity, stateResult.Tick.Tick, stateResult.Tick.Initialized)
		}
		if reportResult.ReportPath != "" {
			fmt.Fprintf(a.out, "report: %s\n", filepath.Join(outDir, reportResult.ReportPath))
		}
		if meta.Status == "complete" {
			return nil
		}
		return &exitError{code: 3}
	}

	data := map[string]any{
		"out":      outDir,
		"stages":   stages,
		"manifest": filepath.Join(outDir, "manifest.json"),
	}
	if codeResult.ContractID != "" {
		data["code"] = codeResult
	}
	if sourceResult.SourceRevisionID != "" {
		data["source"] = sourceResult
	}
	if provenanceResult.Compiler != "" {
		data["provenance"] = provenanceResult
	}
	if analyzeResult.Artifact != "" {
		data["analyze"] = analyzeResult
	}
	if stateResult.Artifact != "" {
		data["state"] = stateResult
	}
	if reportResult.ReportPath != "" {
		data["report"] = reportResult
	}
	return a.success("contract dossier", data, meta, start)
}

func resolveAddress(cfg config.Config, chainID int64, opts *contractOptions) (common.Address, error) {
	if opts.address != "" && opts.targetSlug != "" {
		return common.Address{}, fmt.Errorf("--address and --target are mutually exclusive")
	}
	if opts.address != "" {
		normalized, err := model.NormalizeAddress(opts.address)
		if err != nil {
			return common.Address{}, err
		}
		return common.HexToAddress(normalized), nil
	}
	if opts.targetSlug != "" {
		registry, err := target.Load(cfg.TargetRegistry)
		if err != nil {
			return common.Address{}, err
		}
		t, err := registry.Find(opts.targetSlug)
		if err != nil {
			return common.Address{}, err
		}
		if t.ChainID != chainID {
			return common.Address{}, fmt.Errorf("target chain %d does not match requested chain %d", t.ChainID, chainID)
		}
		address, err := t.Address(opts.contractName)
		if err != nil {
			return common.Address{}, err
		}
		return common.HexToAddress(address), nil
	}
	return common.Address{}, fmt.Errorf("either --address or --target is required")
}

func parseBlock(value string) (*big.Int, error) {
	if value == "" || value == "latest" {
		return nil, nil
	}
	base := 10
	digits := value
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 16
		digits = value[2:]
	}
	n, ok := new(big.Int).SetString(digits, base)
	if !ok || n.Sign() < 0 {
		return nil, fmt.Errorf("invalid block %q", value)
	}
	return n, nil
}

func cachedChain(ctx context.Context, cfg config.Config, chainID int64, refresh bool) (*chain.CachedClient, error) {
	profile := cfg.RPCProfile()
	client, err := chain.Dial(ctx, profile)
	if err != nil {
		return nil, err
	}
	namespace := chain.CacheNamespace(profile.Endpoint + "#" + strconv.FormatInt(chainID, 10))
	return chain.WithCache(client, cfg.CacheDir, namespace, refresh), nil
}

func mapChainError(err error) envelope.Code {
	if errors.Is(err, context.DeadlineExceeded) {
		return envelope.CodeRPCTimeout
	}
	var httpErr *rpc.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == 429 {
			return envelope.CodeRPCRateLimited
		}
		return envelope.CodeRPCUnavailable
	}
	return envelope.CodeRPCUnavailable
}

func provenanceErrorCode(err error) envelope.Code {
	switch {
	case errors.Is(err, dossier.ErrDossierNotInitialized),
		errors.Is(err, dossier.ErrSourceRevisionMissing),
		errors.Is(err, dossier.ErrCodeObservationMissing):
		return envelope.CodeUsage
	case errors.Is(err, solc.ErrCompilationFailed),
		errors.Is(err, solc.ErrCompilerNotFound),
		errors.Is(err, solc.ErrHashMismatch),
		errors.Is(err, solc.ErrInputTooLarge),
		errors.Is(err, solc.ErrOutputTooLarge),
		errors.Is(err, solc.ErrCompileTimeout):
		return envelope.CodeCompilerError
	default:
		return envelope.CodeCompilerUnavailable
	}
}

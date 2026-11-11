package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/rbee3u/yangjian/internal/config"
	"github.com/rbee3u/yangjian/internal/envelope"
	"github.com/rbee3u/yangjian/internal/rpcprobe"
	"github.com/spf13/cobra"
)

func (a *app) rpcCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rpc",
		Short: "Manage probed RPC endpoints",
		Long: "Manage the local RPC endpoint registry. Probing is read-only, records " +
			"capabilities separately from health, and never promotes a chainlist suggestion to a default.",
	}
	cmd.AddCommand(a.rpcImportCommand())
	cmd.AddCommand(a.rpcProbeCommand())
	cmd.AddCommand(a.rpcCheckCommand())
	cmd.AddCommand(a.rpcListCommand())
	cmd.AddCommand(a.rpcEnableCommand(true))
	cmd.AddCommand(a.rpcEnableCommand(false))
	cmd.AddCommand(a.rpcUseCommand())
	return cmd
}

func registryPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if value := os.Getenv("YANGJIAN_RPC_REGISTRY"); value != "" {
		return value
	}
	return rpcprobe.DefaultPath
}

type endpointView struct {
	Name                string                `json:"name"`
	URL                 string                `json:"url"`
	Transport           string                `json:"transport"`
	Enabled             bool                  `json:"enabled"`
	Source              string                `json:"source,omitempty"`
	SourceURL           string                `json:"source_url,omitempty"`
	Tracking            string                `json:"tracking,omitempty"`
	Status              string                `json:"status"`
	ClientVersion       string                `json:"client_version,omitempty"`
	ChainID             int64                 `json:"chain_id,omitempty"`
	HeadBlock           uint64                `json:"head_block,omitempty"`
	BlockAgeSeconds     int64                 `json:"block_age_seconds,omitempty"`
	HeadLagBlocks       uint64                `json:"head_lag_blocks,omitempty"`
	LatencyMS           int64                 `json:"latency_ms,omitempty"`
	CheckedAt           time.Time             `json:"checked_at,omitempty"`
	LastOKAt            *time.Time            `json:"last_ok_at,omitempty"`
	ConsecutiveFailures int                   `json:"consecutive_failures,omitempty"`
	Capabilities        rpcprobe.Capabilities `json:"capabilities"`
	CapabilityNames     []string              `json:"capability_names"`
	RequiresEnv         string                `json:"requires_env,omitempty"`
	CredentialInURL     bool                  `json:"credential_in_url,omitempty"`
	Notes               string                `json:"notes,omitempty"`
	Errors              map[string]string     `json:"probe_errors,omitempty"`
	Message             string                `json:"message,omitempty"`
	History             []rpcprobe.Snapshot   `json:"history,omitempty"`
}

type inventory struct {
	Registry  string           `json:"registry"`
	ChainID   int64            `json:"chain_id"`
	UpdatedAt time.Time        `json:"updated_at,omitempty"`
	Summary   rpcprobe.Summary `json:"summary"`
	Endpoints []endpointView   `json:"endpoints"`
}

func viewOf(ep rpcprobe.Endpoint) endpointView {
	return endpointView{
		Name:                ep.Name,
		URL:                 ep.URL,
		Transport:           ep.Transport,
		Enabled:             ep.Enabled,
		Source:              ep.Source,
		SourceURL:           ep.SourceURL,
		Tracking:            ep.Tracking,
		Status:              ep.Health.Status,
		ClientVersion:       ep.ClientVersion,
		ChainID:             ep.Health.ChainID,
		HeadBlock:           ep.Health.HeadBlock,
		BlockAgeSeconds:     ep.Health.BlockAgeSeconds,
		HeadLagBlocks:       ep.Health.HeadLagBlocks,
		LatencyMS:           ep.Health.LatencyMS,
		CheckedAt:           ep.Health.CheckedAt,
		LastOKAt:            ep.Health.LastOKAt,
		ConsecutiveFailures: ep.Health.ConsecutiveFailures,
		Capabilities:        ep.Capabilities,
		CapabilityNames:     rpcprobe.CapabilityNames(ep.Capabilities),
		RequiresEnv:         ep.RequiresEnv,
		CredentialInURL:     ep.CredentialInURL,
		Notes:               ep.Notes,
		Errors:              ep.Health.Errors,
		Message:             ep.Health.Message,
		History:             ep.History,
	}
}

func makeInventory(path string, reg *rpcprobe.Registry, endpoints []rpcprobe.Endpoint) inventory {
	views := make([]endpointView, 0, len(endpoints))
	for _, ep := range endpoints {
		views = append(views, viewOf(ep))
	}
	sort.SliceStable(views, func(i, j int) bool {
		left, right := rpcprobe.StatusRank(views[i].Status), rpcprobe.StatusRank(views[j].Status)
		if left != right {
			return left < right
		}
		return strings.ToLower(views[i].Name) < strings.ToLower(views[j].Name)
	})
	return inventory{
		Registry:  path,
		ChainID:   reg.ChainID,
		UpdatedAt: reg.UpdatedAt,
		Summary:   rpcprobe.Registry{Endpoints: endpoints}.Summary(),
		Endpoints: views,
	}
}

func (a *app) writeInventory(regPath string, inv inventory) {
	tw := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSTATUS\tCHAIN\tHEAD\tLAG\tLAT(ms)\tCAPABILITIES\tURL")
	for _, ep := range inv.Endpoints {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			ep.Name,
			ep.Status,
			dashInt(ep.ChainID),
			dashUint(ep.HeadBlock),
			lagValue(ep.HeadBlock, ep.HeadLagBlocks),
			dashInt(ep.LatencyMS),
			dashJoin(ep.CapabilityNames),
			rpcprobe.RedactURL(ep.URL),
		)
	}
	tw.Flush()
	s := inv.Summary
	fmt.Fprintf(a.out, "\n%d endpoints (%d enabled): %d ok, %d degraded, %d stale, %d rate limited, %d unreachable, %d auth required, %d wrong chain, %d unconfigured, %d unprobed\n",
		s.Total, s.Enabled, s.OK, s.Degraded, s.Stale, s.RateLimited, s.Unreachable, s.AuthRequired, s.WrongChain, s.Unconfigured, s.Unprobed)
	fmt.Fprintf(a.out, "capabilities: %d logs, %d archive, %d debug_trace, %d parity_trace\n", s.WithLogs, s.WithArchive, s.WithDebugTrace, s.WithTrace)
	fmt.Fprintf(a.out, "registry: %s (updated %s)\n", regPath, inv.UpdatedAt.Format(time.RFC3339))
	for _, ep := range inv.Endpoints {
		if ep.Status == rpcprobe.StatusOK || ep.Status == rpcprobe.StatusUnprobed || !ep.Enabled {
			continue
		}
		detail := ep.Message
		if detail == "" && len(ep.Errors) > 0 {
			detail = firstError(ep.Errors)
		}
		if detail != "" {
			fmt.Fprintf(a.out, "  %-28s %-14s %s\n", ep.Name, ep.Status, detail)
		}
	}
}

type importOptions struct {
	from      string
	registry  string
	chainID   int64
	apply     bool
	includeWS bool
}

func (a *app) rpcImportCommand() *cobra.Command {
	opts := importOptions{}
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import chainlist RPC candidates without trusting or probing them",
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			if opts.from == "" {
				return a.fail("rpc import", envelope.CodeUsage, fmt.Errorf("--from-chainlist is required"), start, envelope.Meta{})
			}
			info, err := os.Stat(opts.from)
			if err != nil {
				return a.fail("rpc import", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			if info.Size() > 64<<20 {
				return a.fail("rpc import", envelope.CodeConfigInvalid,
					fmt.Errorf("chainlist file is larger than 64 MiB"), start, envelope.Meta{})
			}
			data, err := os.ReadFile(opts.from)
			if err != nil {
				return a.fail("rpc import", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			items, err := rpcprobe.ParseChainlist(data, opts.chainID)
			if err != nil {
				return a.fail("rpc import", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			path := registryPath(opts.registry)
			reg, err := rpcprobe.LoadOrNew(path, opts.chainID)
			if err != nil {
				return a.fail("rpc import", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			if reg.ChainID != opts.chainID {
				return a.fail("rpc import", envelope.CodeConfigInvalid,
					fmt.Errorf("registry %s is for chain %d, not %d", path, reg.ChainID, opts.chainID), start, envelope.Meta{})
			}
			var added, existing int
			candidates := make([]endpointView, 0, len(items))
			for _, item := range items {
				ep := rpcprobe.EndpointFromChainlist(opts.chainID, item)
				if ep.Transport == "ws" && !opts.includeWS {
					continue
				}
				if opts.apply {
					stored, isNew := reg.Ensure(ep)
					if isNew {
						added++
					} else {
						existing++
					}
					candidates = append(candidates, viewOf(stored))
					continue
				}
				// Dry run: reserve names locally so the preview matches the
				// names that Apply would choose.
				if ep.Name == "" {
					ep.Name = rpcprobe.NameFromURL(ep.URL)
				}
				ep.Name = reg.UniqueName(ep.Name)
				reg.Endpoints = append(reg.Endpoints, ep)
				candidates = append(candidates, viewOf(ep))
			}
			if opts.apply {
				if err := rpcprobe.Save(path, reg); err != nil {
					return a.fail("rpc import", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
				}
			}
			sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })
			meta := envelope.Meta{ChainID: opts.chainID}
			if !a.json {
				fmt.Fprintf(a.out, "chain %d: %d candidates, applied=%t, new=%d, existing=%d\n", opts.chainID, len(candidates), opts.apply, added, existing)
				tw := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "NAME\tTRANSPORT\tENABLED\tENV\tCREDENTIAL\tURL")
				for _, ep := range candidates {
					fmt.Fprintf(tw, "%s\t%s\t%t\t%s\t%t\t%s\n",
						ep.Name, ep.Transport, ep.Enabled, dashString(ep.RequiresEnv), ep.CredentialInURL, rpcprobe.RedactURL(ep.URL))
				}
				tw.Flush()
				if opts.apply {
					fmt.Fprintf(a.out, "registry: %s\nrun `yangjian rpc check` to probe these endpoints before use\n", path)
				} else {
					fmt.Fprintln(a.out, "dry run: re-run with --apply to write the registry")
				}
				return nil
			}
			return a.success("rpc import", map[string]any{
				"registry":  path,
				"applied":   opts.apply,
				"chain_id":  opts.chainID,
				"added":     added,
				"existing":  existing,
				"endpoints": candidates,
			}, meta, start)
		},
	}
	cmd.Flags().StringVar(&opts.from, "from-chainlist", "", "path to a chainlist rpcs.json download")
	cmd.Flags().StringVar(&opts.registry, "registry", "", "registry path (default .yangjian/rpc-endpoints.json)")
	cmd.Flags().Int64Var(&opts.chainID, "chain", rpcprobe.DefaultChainID, "chain ID to import")
	cmd.Flags().BoolVar(&opts.apply, "apply", false, "write candidates to the registry")
	cmd.Flags().BoolVar(&opts.includeWS, "include-ws", true, "include ws:// and wss:// endpoints")
	return cmd
}

type probeOptions struct {
	url          string
	name         string
	registry     string
	source       string
	tracking     string
	notes        string
	chainID      int64
	timeout      time.Duration
	budget       time.Duration
	archiveBlock uint64
	logsWindow   uint64
	noTrace      bool
	register     bool
	enabled      bool
}

func (a *app) rpcProbeCommand() *cobra.Command {
	opts := probeOptions{}
	cmd := &cobra.Command{
		Use:   "probe",
		Short: "Probe one RPC endpoint and optionally register it",
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			if opts.url == "" {
				return a.fail("rpc probe", envelope.CodeUsage, fmt.Errorf("--url is required"), start, envelope.Meta{})
			}
			if rpcprobe.TransportOf(opts.url) == "" {
				return a.fail("rpc probe", envelope.CodeUsage, fmt.Errorf("unsupported URL scheme; expected http(s) or ws(s)"), start, envelope.Meta{})
			}
			if opts.name == "" {
				opts.name = rpcprobe.NameFromURL(opts.url)
			}
			ep := rpcprobe.Endpoint{
				Name:      opts.name,
				URL:       opts.url,
				Transport: rpcprobe.TransportOf(opts.url),
				Enabled:   opts.enabled,
				Source:    opts.source,
				Tracking:  opts.tracking,
				Notes:     opts.notes,
			}
			result := rpcprobe.Probe(cmd.Context(), ep, rpcprobe.Options{
				ChainID:      opts.chainID,
				Timeout:      opts.timeout,
				Budget:       opts.budget,
				ArchiveBlock: opts.archiveBlock,
				LogsWindow:   opts.logsWindow,
				Trace:        !opts.noTrace,
			})
			path := registryPath(opts.registry)
			registered, added := false, false
			if opts.register && cmd.Context().Err() != nil {
				return a.fail("rpc probe", envelope.CodeRPCUnavailable,
					fmt.Errorf("probe canceled; registry not updated: %w", cmd.Context().Err()), start, envelope.Meta{})
			}
			if opts.register {
				reg, err := rpcprobe.LoadOrNew(path, opts.chainID)
				if err != nil {
					return a.fail("rpc probe", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
				}
				if reg.ChainID != opts.chainID {
					return a.fail("rpc probe", envelope.CodeConfigInvalid,
						fmt.Errorf("registry %s is for chain %d, not %d", path, reg.ChainID, opts.chainID), start, envelope.Meta{})
				}
				ep.Transport = result.Transport
				ep.ClientVersion = result.ClientVersion
				ep.Capabilities = result.Capabilities
				ep.Health = result.Health
				if result.Health.Status == rpcprobe.StatusWrongChain || result.Health.Status == rpcprobe.StatusUnconfigured {
					ep.Enabled = false
				}
				if ep.Name == "" {
					ep.Name = rpcprobe.NameFromURL(ep.URL)
				}
				stored, isNew := reg.AddProbed(ep)
				if err := rpcprobe.Save(path, reg); err != nil {
					return a.fail("rpc probe", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
				}
				registered = true
				added = isNew
				ep = stored
				result.Name = stored.Name
			}
			meta := envelope.Meta{ChainID: opts.chainID}
			if result.Head.Number > 0 {
				number := result.Head.Number
				meta.BlockNumber = &number
				meta.BlockHash = result.Head.Hash
			}
			if !a.json {
				fmt.Fprintf(a.out, "%s\n", ep.Name)
				fmt.Fprintf(a.out, "url: %s\n", rpcprobe.RedactURL(ep.URL))
				fmt.Fprintf(a.out, "status: %s\n", result.Health.Status)
				if result.ObservedChainID != 0 {
					fmt.Fprintf(a.out, "chain: %d\n", result.ObservedChainID)
				}
				if result.Head.Number != 0 {
					fmt.Fprintf(a.out, "head: %d %s\n", result.Head.Number, result.Head.Hash)
				}
				if result.Health.BlockAgeSeconds != 0 {
					fmt.Fprintf(a.out, "head age: %ds\n", result.Health.BlockAgeSeconds)
				}
				fmt.Fprintf(a.out, "latency: %dms\n", result.Health.LatencyMS)
				fmt.Fprintf(a.out, "capabilities: %s\n", dashJoin(rpcprobe.CapabilityNames(result.Capabilities)))
				if result.ClientVersion != "" {
					fmt.Fprintf(a.out, "client: %s\n", result.ClientVersion)
				}
				if result.Health.Message != "" {
					fmt.Fprintf(a.out, "message: %s\n", result.Health.Message)
				}
				for _, pr := range result.Probes {
					if pr.OK {
						continue
					}
					fmt.Fprintf(a.out, "probe %s: %s %s\n", pr.Method, pr.Code, pr.Error)
				}
				if registered {
					fmt.Fprintf(a.out, "registered: %s (new=%t)\n", path, added)
				}
				return nil
			}
			return a.success("rpc probe", map[string]any{
				"registry":   path,
				"registered": registered,
				"added":      added,
				"result":     result,
			}, meta, start)
		},
	}
	cmd.Flags().StringVar(&opts.url, "url", "", "RPC endpoint URL (http(s) or ws(s))")
	cmd.Flags().StringVar(&opts.name, "name", "", "registry name; derived from the host when empty")
	cmd.Flags().StringVar(&opts.registry, "registry", "", "registry path (default .yangjian/rpc-endpoints.json)")
	cmd.Flags().StringVar(&opts.source, "source", "", "provenance of the URL, for example chainlist or user")
	cmd.Flags().StringVar(&opts.tracking, "tracking", "", "chainlist tracking label, if known")
	cmd.Flags().StringVar(&opts.notes, "note", "", "free-form note stored with the endpoint")
	cmd.Flags().Int64Var(&opts.chainID, "chain", rpcprobe.DefaultChainID, "expected chain ID")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", rpcprobe.DefaultTimeout, "per-request timeout")
	cmd.Flags().DurationVar(&opts.budget, "budget", rpcprobe.DefaultBudget, "overall probe budget per endpoint")
	cmd.Flags().Uint64Var(&opts.archiveBlock, "archive-block", rpcprobe.DefaultArchiveBlock, "historical block used for archive checks")
	cmd.Flags().Uint64Var(&opts.logsWindow, "logs-window", rpcprobe.DefaultLogsWindow, "block window for the eth_getLogs check")
	cmd.Flags().BoolVar(&opts.noTrace, "no-trace", false, "skip debug_traceTransaction and trace_transaction probes")
	cmd.Flags().BoolVar(&opts.register, "register", false, "write the result to the registry")
	cmd.Flags().BoolVar(&opts.enabled, "enabled", true, "register the endpoint as enabled")
	return cmd
}

type checkOptions struct {
	registry     string
	names        []string
	statuses     []string
	all          bool
	chainID      int64
	timeout      time.Duration
	budget       time.Duration
	archiveBlock uint64
	logsWindow   uint64
	noTrace      bool
	concurrency  int
	delay        time.Duration
	maxLag       uint64
	noSave       bool
}

func (a *app) rpcCheckCommand() *cobra.Command {
	opts := checkOptions{}
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Health-check registered RPC endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			path := registryPath(opts.registry)
			reg, err := rpcprobe.Load(path)
			if err != nil {
				return a.fail("rpc check", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			if opts.chainID != 0 && opts.chainID != reg.ChainID {
				return a.fail("rpc check", envelope.CodeConfigInvalid,
					fmt.Errorf("registry %s is for chain %d, not %d", path, reg.ChainID, opts.chainID), start, envelope.Meta{})
			}
			selected, err := selectEndpoints(reg, opts.names, opts.all)
			if err != nil {
				return a.fail("rpc check", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			if len(opts.statuses) > 0 {
				filtered := selected[:0]
				for _, ep := range selected {
					if matchesStatus(ep.Health.Status, opts.statuses) {
						filtered = append(filtered, ep)
					}
				}
				selected = filtered
			}
			if len(selected) == 0 {
				return a.fail("rpc check", envelope.CodeConfigInvalid, fmt.Errorf("no endpoints selected"), start, envelope.Meta{})
			}
			archiveBlock := opts.archiveBlock
			if archiveBlock == 0 {
				archiveBlock = reg.Probe.ArchiveBlock
			}
			logsWindow := opts.logsWindow
			if logsWindow == 0 {
				logsWindow = reg.Probe.LogsWindowBlocks
			}
			probeOpts := rpcprobe.Options{
				ChainID:      reg.ChainID,
				Timeout:      opts.timeout,
				Budget:       opts.budget,
				ArchiveBlock: archiveBlock,
				LogsWindow:   logsWindow,
				Trace:        !opts.noTrace,
			}
			results := probeAll(cmd.Context(), selected, probeOpts, opts.concurrency, opts.delay)
			var maxHead uint64
			for _, result := range results {
				if result.Health.HeadBlock > maxHead {
					maxHead = result.Health.HeadBlock
				}
			}
			updated := make([]rpcprobe.Endpoint, 0, len(selected))
			for i, result := range results {
				ep, ok := reg.ApplyResult(selected[i].URL, result, maxHead, opts.maxLag)
				if ok {
					updated = append(updated, *ep)
				}
			}
			if err := cmd.Context().Err(); err != nil {
				return a.fail("rpc check", envelope.CodeRPCUnavailable,
					fmt.Errorf("check canceled; registry not updated: %w", err), start, envelope.Meta{})
			}
			if !opts.noSave {
				if err := rpcprobe.Save(path, reg); err != nil {
					return a.fail("rpc check", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
				}
			}
			inv := makeInventory(path, reg, updated)
			status := "complete"
			for _, ep := range updated {
				if ep.Enabled && ep.Health.Status != rpcprobe.StatusOK {
					status = "partial"
					break
				}
			}
			meta := envelope.Meta{ChainID: reg.ChainID, Status: status}
			if !a.json {
				a.writeInventory(path, inv)
				if opts.noSave {
					fmt.Fprintln(a.out, "not saved (--no-save)")
				}
				return nil
			}
			return a.success("rpc check", inv, meta, start)
		},
	}
	cmd.Flags().StringVar(&opts.registry, "registry", "", "registry path (default .yangjian/rpc-endpoints.json)")
	cmd.Flags().StringSliceVar(&opts.names, "name", nil, "endpoint name or URL to check (repeatable)")
	cmd.Flags().StringSliceVar(&opts.statuses, "status", nil, "only check endpoints currently in this health status (repeatable)")
	cmd.Flags().BoolVar(&opts.all, "all", false, "include disabled endpoints")
	cmd.Flags().Int64Var(&opts.chainID, "chain", 0, "expected chain ID; defaults to the registry chain")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", rpcprobe.DefaultTimeout, "per-request timeout")
	cmd.Flags().DurationVar(&opts.budget, "budget", rpcprobe.DefaultBudget, "overall probe budget per endpoint")
	cmd.Flags().Uint64Var(&opts.archiveBlock, "archive-block", 0, "historical block used for archive checks")
	cmd.Flags().Uint64Var(&opts.logsWindow, "logs-window", 0, "block window for the eth_getLogs check")
	cmd.Flags().BoolVar(&opts.noTrace, "no-trace", false, "skip trace probes")
	cmd.Flags().IntVar(&opts.concurrency, "concurrency", rpcprobe.DefaultConcurrency, "number of endpoints probed concurrently")
	cmd.Flags().DurationVar(&opts.delay, "delay", 0, "minimum spacing between endpoint probe starts (for rate-limited providers)")
	cmd.Flags().Uint64Var(&opts.maxLag, "max-lag", rpcprobe.DefaultMaxHeadLag, "mark a healthy endpoint stale when it lags the best head by more blocks")
	cmd.Flags().BoolVar(&opts.noSave, "no-save", false, "probe without updating the registry")
	return cmd
}

func selectEndpoints(reg *rpcprobe.Registry, names []string, all bool) ([]rpcprobe.Endpoint, error) {
	if len(names) > 0 {
		seen := map[string]bool{}
		selected := make([]rpcprobe.Endpoint, 0, len(names))
		for _, name := range names {
			ep, ok := reg.Find(strings.TrimSpace(name))
			if !ok {
				return nil, fmt.Errorf("endpoint %q is not in the registry", name)
			}
			if seen[ep.URL] {
				continue
			}
			seen[ep.URL] = true
			selected = append(selected, *ep)
		}
		return selected, nil
	}
	selected := make([]rpcprobe.Endpoint, 0, len(reg.Endpoints))
	for _, ep := range reg.Endpoints {
		if all || ep.Enabled {
			selected = append(selected, ep)
		}
	}
	return selected, nil
}

func probeAll(ctx context.Context, endpoints []rpcprobe.Endpoint, opts rpcprobe.Options, concurrency int, delay time.Duration) []rpcprobe.Result {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(endpoints) {
		concurrency = len(endpoints)
	}
	var tick <-chan time.Time
	if delay > 0 {
		ticker := time.NewTicker(delay)
		defer ticker.Stop()
		tick = ticker.C
	}
	results := make([]rpcprobe.Result, len(endpoints))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if tick != nil {
					select {
					case <-tick:
					case <-ctx.Done():
					}
				}
				results[index] = rpcprobe.Probe(ctx, endpoints[index], opts)
			}
		}()
	}
	for i := range endpoints {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return results
}

type listOptions struct {
	registry     string
	capabilities []string
	statuses     []string
	enabledOnly  bool
}

func (a *app) rpcListCommand() *cobra.Command {
	opts := listOptions{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered RPC endpoints and their capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			path := registryPath(opts.registry)
			reg, err := rpcprobe.Load(path)
			if err != nil {
				return a.fail("rpc list", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			if err := validateCapabilities(opts.capabilities); err != nil {
				return a.fail("rpc list", envelope.CodeUsage, err, start, envelope.Meta{})
			}
			filtered := make([]rpcprobe.Endpoint, 0, len(reg.Endpoints))
			for _, ep := range reg.Endpoints {
				if opts.enabledOnly && !ep.Enabled {
					continue
				}
				if !matchesCapabilities(ep.Capabilities, opts.capabilities) {
					continue
				}
				if !matchesStatus(ep.Health.Status, opts.statuses) {
					continue
				}
				filtered = append(filtered, ep)
			}
			inv := makeInventory(path, reg, filtered)
			meta := envelope.Meta{ChainID: reg.ChainID}
			if !a.json {
				a.writeInventory(path, inv)
				return nil
			}
			return a.success("rpc list", inv, meta, start)
		},
	}
	cmd.Flags().StringVar(&opts.registry, "registry", "", "registry path (default .yangjian/rpc-endpoints.json)")
	cmd.Flags().StringSliceVar(&opts.capabilities, "capability", nil, "required capability (repeatable): logs, archive, debug_trace, parity_trace, trace, proof, fee_history, batch, websocket")
	cmd.Flags().StringSliceVar(&opts.statuses, "status", nil, "health status filter (repeatable)")
	cmd.Flags().BoolVar(&opts.enabledOnly, "enabled-only", false, "hide disabled endpoints")
	return cmd
}

func validateCapabilities(requested []string) error {
	known := map[string]bool{
		"blocks": true, "logs": true, "events": true, "call": true, "code": true,
		"storage": true, "proof": true, "fee_history": true, "fees": true,
		"archive": true, "historical": true, "debug_trace": true, "debug": true,
		"parity_trace": true, "parity": true, "trace": true, "batch": true,
		"websocket": true, "ws": true,
	}
	for _, item := range expandList(requested) {
		if !known[normalizeCapability(item)] {
			return fmt.Errorf("unknown capability %q", item)
		}
	}
	return nil
}

func matchesCapabilities(caps rpcprobe.Capabilities, requested []string) bool {
	for _, item := range expandList(requested) {
		switch normalizeCapability(item) {
		case "blocks":
			if !caps.Blocks {
				return false
			}
		case "logs", "events":
			if !caps.Logs {
				return false
			}
		case "call":
			if !caps.Call {
				return false
			}
		case "code":
			if !caps.Code {
				return false
			}
		case "storage":
			if !caps.Storage {
				return false
			}
		case "proof":
			if !caps.Proof {
				return false
			}
		case "fee_history", "fees":
			if !caps.FeeHistory {
				return false
			}
		case "archive", "historical":
			if !caps.Archive {
				return false
			}
		case "debug_trace", "debug":
			if !caps.DebugTrace {
				return false
			}
		case "parity_trace", "parity":
			if !caps.ParityTrace {
				return false
			}
		case "trace":
			if !caps.DebugTrace && !caps.ParityTrace {
				return false
			}
		case "batch":
			if !caps.Batch {
				return false
			}
		case "websocket", "ws":
			if !caps.WebSocket {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func matchesStatus(status string, requested []string) bool {
	items := expandList(requested)
	if len(items) == 0 {
		return true
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), status) {
			return true
		}
	}
	return false
}

func expandList(values []string) []string {
	var out []string
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
	}
	return out
}

func normalizeCapability(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
}

func (a *app) rpcEnableCommand(enable bool) *cobra.Command {
	verb := "enable"
	if !enable {
		verb = "disable"
	}
	var registry, note string
	cmd := &cobra.Command{
		Use:   verb + " <name>",
		Short: verb + " a registry endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			path := registryPath(registry)
			reg, err := rpcprobe.Load(path)
			if err != nil {
				return a.fail("rpc "+verb, envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			ep, ok := reg.Find(args[0])
			if !ok {
				return a.fail("rpc "+verb, envelope.CodeConfigInvalid, fmt.Errorf("endpoint %q is not in the registry", args[0]), start, envelope.Meta{})
			}
			ep.Enabled = enable
			if note != "" {
				ep.Notes = note
			}
			if err := rpcprobe.Save(path, reg); err != nil {
				return a.fail("rpc "+verb, envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			if !a.json {
				fmt.Fprintf(a.out, "%s %s\n", ep.Name, verb+"d")
				return nil
			}
			return a.success("rpc "+verb, map[string]any{
				"name":    ep.Name,
				"url":     ep.URL,
				"enabled": ep.Enabled,
				"notes":   ep.Notes,
			}, envelope.Meta{ChainID: reg.ChainID}, start)
		},
	}
	cmd.Flags().StringVar(&registry, "registry", "", "registry path (default .yangjian/rpc-endpoints.json)")
	cmd.Flags().StringVar(&note, "note", "", "replace the endpoint note")
	return cmd
}

type useOptions struct {
	registry string
	config   string
	name     string
	print    bool
	force    bool
}

func (a *app) rpcUseCommand() *cobra.Command {
	opts := useOptions{}
	cmd := &cobra.Command{
		Use:   "use <name>",
		Short: "Point the CLI's default RPC profile at a healthy registry endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			start := time.Now()
			path := registryPath(opts.registry)
			reg, err := rpcprobe.Load(path)
			if err != nil {
				return a.fail("rpc use", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			ep, ok := reg.Find(args[0])
			if !ok {
				return a.fail("rpc use", envelope.CodeConfigInvalid, fmt.Errorf("endpoint %q is not in the registry", args[0]), start, envelope.Meta{})
			}
			if !ep.Enabled {
				return a.fail("rpc use", envelope.CodeConfigInvalid, fmt.Errorf("endpoint %q is disabled", ep.Name), start, envelope.Meta{})
			}
			if ep.Health.Status == rpcprobe.StatusUnprobed {
				return a.fail("rpc use", envelope.CodeConfigInvalid, fmt.Errorf("endpoint %q has not been probed; run `yangjian rpc probe --url ... --register`", ep.Name), start, envelope.Meta{})
			}
			if ep.Health.ChainID != reg.ChainID {
				return a.fail("rpc use", envelope.CodeConfigInvalid,
					fmt.Errorf("endpoint %q reports chain %d, registry is chain %d", ep.Name, ep.Health.ChainID, reg.ChainID), start, envelope.Meta{})
			}
			if ep.Health.Status != rpcprobe.StatusOK && !opts.force {
				return a.fail("rpc use", envelope.CodeConfigInvalid,
					fmt.Errorf("endpoint %q is %s; rerun `yangjian rpc check --name %s` or pass --force", ep.Name, ep.Health.Status, ep.Name), start, envelope.Meta{})
			}
			if opts.print {
				export := "YANGJIAN_RPC_URL=" + shellQuote(ep.URL)
				if !a.json {
					fmt.Fprintln(a.out, export)
					return nil
				}
				return a.success("rpc use", map[string]string{
					"name":     ep.Name,
					"url":      ep.URL,
					"endpoint": ep.URL,
					"export":   export,
				}, envelope.Meta{ChainID: reg.ChainID}, start)
			}
			configPath := opts.config
			if configPath == "" {
				configPath = a.configPath
			}
			if configPath == "" {
				configPath = config.DefaultPath
			}
			cfg, err := config.LoadForUpdate(configPath)
			if err != nil {
				return a.fail("rpc use", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			if cfg.Providers.RPC == nil {
				cfg.Providers.RPC = map[string]config.ProviderProfile{}
			}
			profile := cfg.Providers.RPC["default"]
			profile.Endpoint = ep.URL
			if profile.Timeout == "" {
				profile.Timeout = "15s"
			}
			if profile.RateLimitRPS <= 0 {
				profile.RateLimitRPS = 5
			}
			if profile.Burst <= 0 {
				profile.Burst = 5
			}
			cfg.Providers.RPC["default"] = profile
			if err := config.Write(configPath, cfg); err != nil {
				return a.fail("rpc use", envelope.CodeConfigInvalid, err, start, envelope.Meta{})
			}
			if !a.json {
				fmt.Fprintf(a.out, "wrote %s: providers.rpc.default -> %s (%s)\n", configPath, ep.Name, ep.URL)
				return nil
			}
			return a.success("rpc use", map[string]string{
				"name":       ep.Name,
				"url":        ep.URL,
				"endpoint":   ep.URL,
				"config":     configPath,
				"config_key": "providers.rpc.default.endpoint",
			}, envelope.Meta{ChainID: reg.ChainID}, start)
		},
	}
	cmd.Flags().StringVar(&opts.registry, "registry", "", "registry path (default .yangjian/rpc-endpoints.json)")
	cmd.Flags().StringVar(&opts.config, "config", "", "config path (default .yangjian/config.json)")
	cmd.Flags().BoolVar(&opts.print, "print", false, "print an export line instead of writing the config")
	cmd.Flags().BoolVar(&opts.force, "force", false, "allow a degraded or stale endpoint")
	return cmd
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func dashUint(value uint64) string {
	if value == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", value)
}

func lagValue(head, lag uint64) string {
	if head == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", lag)
}

func dashInt(value int64) string {
	if value == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", value)
}

func dashString(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func dashJoin(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

func firstError(errors map[string]string) string {
	keys := make([]string, 0, len(errors))
	for key := range errors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0] + ": " + errors[keys[0]]
}

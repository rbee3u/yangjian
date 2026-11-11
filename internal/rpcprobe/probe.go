package rpcprobe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
)

const (
	// WETH is used only as a stable, high-volume probe target. It is a public
	// protocol address from targets/mainnet.json, not a research conclusion.
	wethAddress = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	// keccak256("Transfer(address,address,uint256)")
	transferTopic       = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	totalSupplySelector = "0x18160ddd"

	DefaultStaleAfter = 10 * time.Minute
)

var errStopped = errors.New("probe stopped after an auth or rate-limit response")

type Options struct {
	ChainID      int64
	Timeout      time.Duration
	Budget       time.Duration
	ArchiveBlock uint64
	LogsWindow   uint64
	Trace        bool
	UserAgent    string
}

func (o Options) withDefaults() Options {
	if o.ChainID == 0 {
		o.ChainID = DefaultChainID
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.Budget <= 0 {
		o.Budget = DefaultBudget
	}
	if o.ArchiveBlock == 0 {
		o.ArchiveBlock = DefaultArchiveBlock
	}
	if o.LogsWindow == 0 {
		o.LogsWindow = DefaultLogsWindow
	}
	if o.UserAgent == "" {
		o.UserAgent = "yangjian-rpc-probe/1"
	}
	return o
}

type Result struct {
	Name            string        `json:"name,omitempty"`
	URL             string        `json:"url"`
	Transport       string        `json:"transport"`
	ObservedChainID int64         `json:"observed_chain_id,omitempty"`
	ClientVersion   string        `json:"client_version,omitempty"`
	Head            BlockRef      `json:"head,omitempty"`
	Capabilities    Capabilities  `json:"capabilities"`
	Health          Health        `json:"health"`
	Probes          []ProbeResult `json:"probes,omitempty"`
	DurationMS      int64         `json:"duration_ms"`
}

type BlockRef struct {
	Number    uint64    `json:"number,omitempty"`
	Hash      string    `json:"hash,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Txs       []string  `json:"-"`
}

type ProbeResult struct {
	Method    string `json:"method"`
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Code      string `json:"code,omitempty"`
	Error     string `json:"error,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type rpcBlock struct {
	Number       string            `json:"number"`
	Hash         string            `json:"hash"`
	Timestamp    string            `json:"timestamp"`
	Transactions []json.RawMessage `json:"transactions"`
}

func (b rpcBlock) txHashes() []string {
	hashes := make([]string, 0, len(b.Transactions))
	for _, raw := range b.Transactions {
		var hash string
		if err := json.Unmarshal(raw, &hash); err == nil && hash != "" {
			hashes = append(hashes, hash)
			continue
		}
		var tx rpcTransaction
		if err := json.Unmarshal(raw, &tx); err == nil && tx.Hash != "" {
			hashes = append(hashes, tx.Hash)
		}
	}
	return hashes
}

type rpcTransaction struct {
	Hash  string  `json:"hash"`
	To    *string `json:"to"`
	Input string  `json:"input"`
	Data  string  `json:"data"`
	Value string  `json:"value"`
}

func (t rpcTransaction) calldata() string {
	if t.Input != "" {
		return t.Input
	}
	return t.Data
}

type rpcCallMsg struct {
	To   string `json:"to"`
	Data string `json:"data"`
}

type rpcProof struct {
	AccountProof []string `json:"accountProof"`
}

type rpcFeeHistory struct {
	BaseFeePerGas []string `json:"baseFeePerGas"`
}

type prober struct {
	client         *rpc.Client
	opts           Options
	ctx            context.Context
	probes         []ProbeResult
	rateLimited    bool
	authRequired   bool
	budgetExceeded bool
	stopped        bool
	traceBlock     uint64
	traceTx        string
}

// Probe runs a bounded, read-only capability check. Network failures are
// findings recorded in Result.Health, not command errors.
func Probe(ctx context.Context, ep Endpoint, opts Options) (res Result) {
	start := time.Now()
	opts = opts.withDefaults()
	res = Result{
		Name:      ep.Name,
		URL:       ep.URL,
		Transport: TransportOf(ep.URL),
		Health: Health{
			Status:    StatusUnreachable,
			CheckedAt: time.Now().UTC(),
		},
	}
	defer func() {
		res.DurationMS = time.Since(start).Milliseconds()
		res.Health.DurationMS = res.DurationMS
	}()

	if res.Transport == "" {
		res.Health.Message = "unsupported URL scheme; expected http(s) or ws(s)"
		return res
	}
	expanded, missing := EnvRefs(ep.URL)
	if len(missing) > 0 {
		res.Health.Status = StatusUnconfigured
		res.Health.Message = "missing environment variables: " + strings.Join(missing, ", ")
		return res
	}

	probeCtx, cancel := context.WithTimeout(ctx, opts.Budget)
	defer cancel()
	client, err := dial(probeCtx, expanded, res.Transport, opts.UserAgent, opts.Timeout)
	if err != nil {
		pe := classifyError(err)
		res.Health.Message = "dial failed: " + pe.Message
		res.Health.Errors = map[string]string{"dial": pe.Message}
		if pe.Auth {
			res.Health.Status = StatusAuthRequired
		}
		return res
	}
	defer client.Close()

	p := &prober{client: client, opts: opts, ctx: probeCtx}

	chainID, ok := p.chainID()
	if !ok {
		res.Probes = p.probes
		res.Health.Status = p.connectionStatus()
		res.Health.Message = p.failureMessage()
		res.Health.Errors = p.healthErrors()
		return res
	}
	res.ObservedChainID = chainID
	res.Health.ChainID = chainID
	if opts.ChainID != 0 && chainID != opts.ChainID {
		res.Probes = p.probes
		res.Health.Status = StatusWrongChain
		res.Health.Message = fmt.Sprintf("endpoint serves chain %d, expected %d", chainID, opts.ChainID)
		res.Health.Errors = p.healthErrors()
		return res
	}

	head, ok := p.head()
	if !ok {
		res.Probes = p.probes
		res.Health.Status = p.connectionStatus()
		res.Health.Message = p.failureMessage()
		res.Health.Errors = p.healthErrors()
		return res
	}
	res.Head = head
	res.Health.HeadBlock = head.Number
	res.Health.HeadHash = head.Hash
	res.Health.HeadTimestamp = head.Timestamp
	if !head.Timestamp.IsZero() {
		age := time.Since(head.Timestamp)
		if age < 0 {
			age = 0
		}
		res.Health.BlockAgeSeconds = int64(age.Seconds())
	}
	res.Capabilities.Blocks = true
	if res.Transport == "ws" {
		res.Capabilities.WebSocket = true
	}

	codeOK := p.probeCode()
	callOK := p.probeCall()
	storageOK := p.probeStorage()
	res.Capabilities.Code = codeOK
	res.Capabilities.Call = callOK
	res.Capabilities.Storage = storageOK
	basicOK := codeOK && callOK && storageOK

	if !p.stopped {
		res.Capabilities.Proof = p.probeProof()
		res.Capabilities.FeeHistory = p.probeFeeHistory()
		res.Capabilities.Logs = p.probeLogs(head.Number)
		archiveBalance, archiveCall := p.probeArchive()
		res.Capabilities.Archive = archiveBalance && archiveCall
		if opts.Trace {
			res.Capabilities.DebugTrace, res.Capabilities.ParityTrace = p.probeTrace(head)
			res.Health.TraceBlock = p.traceBlock
			res.Health.TraceTx = p.traceTx
		}
		res.Capabilities.Batch = p.probeBatch()
	}

	// The client version is informative only. Reading it last keeps a
	// rate-limited web3_clientVersion from hiding the endpoint's real
	// capabilities.
	res.ClientVersion = p.clientVersion()
	res.Probes = p.probes
	res.Health.LatencyMS = medianLatency(p.probes)
	res.Health.Errors = p.healthErrors()
	res.Health.Status = p.failureStatus(basicOK)
	if res.Health.Status == StatusOK && !head.Timestamp.IsZero() && time.Since(head.Timestamp) > DefaultStaleAfter {
		res.Health.Status = StatusStale
		res.Health.Message = fmt.Sprintf("head block is %s old", time.Since(head.Timestamp).Round(time.Second))
	}
	if res.Health.Status != StatusOK && res.Health.Message == "" {
		res.Health.Message = p.failureMessage()
	}
	return res
}

func dial(ctx context.Context, raw, transport, userAgent string, timeout time.Duration) (*rpc.Client, error) {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if transport == "ws" {
		return rpc.DialWebsocket(dialCtx, raw, "")
	}
	return rpc.DialOptions(dialCtx, raw, rpc.WithHeader("User-Agent", userAgent))
}

func (p *prober) clientVersion() string {
	var version string
	if err := p.informational("web3_clientVersion", &version); err != nil {
		return ""
	}
	return version
}

func (p *prober) informational(method string, result any) error {
	if p.stopped {
		return errStopped
	}
	start := time.Now()
	callCtx, cancel := context.WithTimeout(p.ctx, p.opts.Timeout)
	err := p.client.CallContext(callCtx, result, method)
	cancel()
	latency := time.Since(start)
	pe := classifyError(err)
	p.record(method, latency, err, pe, "")
	return err
}

func (p *prober) chainID() (int64, bool) {
	var raw string
	if err := p.call("eth_chainId", &raw, ""); err != nil {
		return 0, false
	}
	id, err := parseHexUint(raw)
	if err != nil {
		p.markFailed("eth_chainId", "invalid_hex", "invalid chain id: "+err.Error())
		return 0, false
	}
	p.setLastDetail(fmt.Sprintf("chain id %d", id))
	return int64(id), true
}

func (p *prober) head() (BlockRef, bool) {
	var block rpcBlock
	if err := p.call("eth_getBlockByNumber", &block, "", "latest", false); err != nil {
		return BlockRef{}, false
	}
	number, err := parseHexUint(block.Number)
	if err != nil {
		p.markFailed("eth_getBlockByNumber", "invalid_block", "invalid block number: "+err.Error())
		return BlockRef{}, false
	}
	ref := BlockRef{Number: number, Hash: block.Hash, Txs: block.txHashes()}
	if ts, err := parseHexUint(block.Timestamp); err == nil && ts > 0 {
		ref.Timestamp = time.Unix(int64(ts), 0).UTC()
	}
	p.setLastDetail(fmt.Sprintf("block %d", number))
	return ref, true
}

func (p *prober) probeCode() bool {
	var code string
	if err := p.call("eth_getCode", &code, "", wethAddress, "latest"); err != nil {
		return false
	}
	if hexBytes(code) == 0 {
		p.setLastDetail("empty runtime code")
		return false
	}
	p.setLastDetail(fmt.Sprintf("%d bytes", hexBytes(code)))
	return true
}

func (p *prober) probeCall() bool {
	var out string
	msg := rpcCallMsg{To: wethAddress, Data: totalSupplySelector}
	if err := p.call("eth_call", &out, "", msg, "latest"); err != nil {
		return false
	}
	if hexBytes(out) == 0 {
		p.setLastDetail("empty eth_call result")
		return false
	}
	p.setLastDetail(fmt.Sprintf("%d bytes", hexBytes(out)))
	return true
}

func (p *prober) probeStorage() bool {
	var out string
	if err := p.call("eth_getStorageAt", &out, "", wethAddress, "0x0", "latest"); err != nil {
		return false
	}
	if isZeroHex(out) {
		p.setLastDetail("zero value at slot 0")
		return false
	}
	p.setLastDetail("slot 0 is non-zero")
	return true
}

func (p *prober) probeProof() bool {
	var proof rpcProof
	if err := p.optional("eth_getProof", &proof, "", wethAddress, []string{}, "latest"); err != nil {
		return false
	}
	if len(proof.AccountProof) == 0 {
		p.setLastDetail("empty account proof")
		return false
	}
	p.setLastDetail(fmt.Sprintf("%d proof nodes", len(proof.AccountProof)))
	return true
}

func (p *prober) probeFeeHistory() bool {
	var history rpcFeeHistory
	if err := p.optional("eth_feeHistory", &history, "", "0x4", "latest", []float64{}); err != nil {
		return false
	}
	if len(history.BaseFeePerGas) == 0 {
		p.setLastDetail("empty fee history")
		return false
	}
	p.setLastDetail(fmt.Sprintf("%d base fee entries", len(history.BaseFeePerGas)))
	return true
}

func (p *prober) probeLogs(head uint64) bool {
	from := uint64(0)
	if head > p.opts.LogsWindow {
		from = head - p.opts.LogsWindow
	}
	filter := map[string]any{
		"address":   wethAddress,
		"fromBlock": hexUint(from),
		"toBlock":   hexUint(head),
		"topics":    []string{transferTopic},
	}
	var logs []json.RawMessage
	err := p.optional("eth_getLogs", &logs, "", filter)
	if err == nil {
		p.setLastDetail(fmt.Sprintf("%d logs in %d blocks", len(logs), head-from+1))
		return true
	}
	pe := classifyError(err)
	if pe.RangeLimit {
		p.setLastDetail("supported, but this small query was rejected: " + truncate(pe.Message, 140))
		return true
	}
	return false
}

func (p *prober) probeArchive() (balanceOK, callOK bool) {
	block := hexUint(p.opts.ArchiveBlock)
	label := strconv.FormatUint(p.opts.ArchiveBlock, 10)
	before := len(p.probes)
	var balance string
	if err := p.optional("eth_getBalance", &balance, "", wethAddress, block); err == nil {
		balanceOK = true
		p.setLastDetail("balance at block " + label)
	}
	p.renameLastProbe(before, "eth_getBalance[archive]")

	before = len(p.probes)
	var out string
	msg := rpcCallMsg{To: wethAddress, Data: totalSupplySelector}
	if err := p.optional("eth_call", &out, "", msg, block); err == nil && hexBytes(out) > 0 {
		callOK = true
		p.setLastDetail("eth_call at block " + label)
	}
	p.renameLastProbe(before, "eth_call[archive]")
	return balanceOK, callOK
}

func (p *prober) renameLastProbe(before int, method string) {
	if len(p.probes) > before {
		p.probes[len(p.probes)-1].Method = method
	}
}

func (p *prober) probeTrace(head BlockRef) (debugOK, parityOK bool) {
	if p.stopped {
		return false, false
	}
	tx := p.pickTraceTx(head)
	if tx == "" {
		if p.stopped {
			return false, false
		}
		p.record("debug_traceTransaction", 0, errors.New("no transaction in head block"),
			probeError{Code: "skipped", Message: "no transaction in head block"}, "")
		p.record("trace_transaction", 0, errors.New("no transaction in head block"),
			probeError{Code: "skipped", Message: "no transaction in head block"}, "")
		return false, false
	}
	p.traceTx = tx
	p.traceBlock = head.Number

	var raw json.RawMessage
	if err := p.optional("debug_traceTransaction", &raw, "", tx, map[string]any{"tracer": "callTracer"}); err == nil {
		if len(raw) == 0 || string(raw) == "null" {
			p.setLastDetail("empty callTracer result")
		} else {
			frames := countCallFrames(raw)
			p.setLastDetail(fmt.Sprintf("%d call frames", frames))
			debugOK = frames > 0
		}
	}

	var traces []json.RawMessage
	if err := p.optional("trace_transaction", &traces, "", tx); err == nil {
		if len(traces) == 0 {
			p.setLastDetail("empty trace_transaction result")
		} else {
			p.setLastDetail(fmt.Sprintf("%d trace entries", len(traces)))
			parityOK = true
		}
	}
	return debugOK, parityOK
}

func (p *prober) pickTraceTx(head BlockRef) string {
	if len(head.Txs) == 0 {
		return ""
	}
	limit := len(head.Txs)
	if limit > 6 {
		limit = 6
	}
	for i := 0; i < limit; i++ {
		hash := head.Txs[i]
		var tx rpcTransaction
		if err := p.optional("eth_getTransactionByHash", &tx, "", hash); err != nil {
			if p.stopped {
				return ""
			}
			continue
		}
		calldata := tx.calldata()
		if tx.To != nil && (calldata == "" || calldata == "0x") {
			return hash
		}
	}
	return head.Txs[0]
}

func (p *prober) probeBatch() bool {
	if p.stopped {
		return false
	}
	start := time.Now()
	var chainID, blockNumber string
	batch := []rpc.BatchElem{
		{Method: "eth_chainId", Result: &chainID},
		{Method: "eth_blockNumber", Result: &blockNumber},
	}
	callCtx, cancel := context.WithTimeout(p.ctx, p.opts.Timeout)
	err := p.client.BatchCallContext(callCtx, batch)
	cancel()
	latency := time.Since(start)
	if err == nil {
		for _, elem := range batch {
			if elem.Error != nil {
				err = elem.Error
				break
			}
		}
	}
	pe := classifyError(err)
	p.record("batch", latency, err, pe, "")
	if err != nil {
		if pe.RateLimited {
			p.rateLimited = true
			p.stopped = true
		}
		return false
	}
	pid, errID := parseHexUint(chainID)
	bid, errBlock := parseHexUint(blockNumber)
	if errID != nil || errBlock != nil {
		p.markFailed("batch", "invalid_hex", "batch returned an invalid chain id or block number")
		return false
	}
	p.setLastDetail(fmt.Sprintf("chain %d, block %d", pid, bid))
	return true
}

// call probes a method whose failure means the endpoint is not usable for
// Yangjian's core reads. An authentication failure on a critical method marks
// the whole endpoint auth_required.
func (p *prober) call(method string, result any, detail string, args ...any) error {
	return p.callWith(true, method, result, detail, args...)
}

// optional probes a capability that a healthy endpoint may legitimately not
// expose. Its authentication failure is a capability gap, not an endpoint
// health failure; a rate limit still stops the probe to avoid hammering.
func (p *prober) optional(method string, result any, detail string, args ...any) error {
	return p.callWith(false, method, result, detail, args...)
}

func (p *prober) callWith(critical bool, method string, result any, detail string, args ...any) error {
	if p.stopped {
		return errStopped
	}
	if p.ctx.Err() != nil {
		p.record(method, 0, p.ctx.Err(), probeError{Code: "budget_exceeded", Message: "probe budget exceeded"}, detail)
		p.budgetExceeded = true
		p.stopped = true
		return p.ctx.Err()
	}
	start := time.Now()
	callCtx, cancel := context.WithTimeout(p.ctx, p.opts.Timeout)
	err := p.client.CallContext(callCtx, result, method, args...)
	cancel()
	latency := time.Since(start)
	pe := classifyError(err)
	if err != nil && p.ctx.Err() != nil {
		pe = probeError{Code: "budget_exceeded", Message: "probe budget exceeded"}
		p.budgetExceeded = true
		p.stopped = true
	}
	p.record(method, latency, err, pe, detail)
	if pe.RateLimited {
		p.rateLimited = true
		p.stopped = true
	}
	if critical && pe.Auth {
		p.authRequired = true
		p.stopped = true
	}
	return err
}

func (p *prober) record(method string, latency time.Duration, err error, pe probeError, detail string) {
	pr := ProbeResult{
		Method:    method,
		OK:        err == nil,
		LatencyMS: latency.Milliseconds(),
		Detail:    detail,
	}
	if err != nil {
		pr.Code = pe.Code
		pr.Error = pe.Message
	}
	p.probes = append(p.probes, pr)
}

func (p *prober) markFailed(method, code, message string) {
	for i := len(p.probes) - 1; i >= 0; i-- {
		if p.probes[i].Method == method {
			p.probes[i].OK = false
			p.probes[i].Code = code
			p.probes[i].Error = message
			return
		}
	}
	p.probes = append(p.probes, ProbeResult{Method: method, OK: false, Code: code, Error: message})
}

func (p *prober) setLastDetail(detail string) {
	if len(p.probes) == 0 {
		return
	}
	p.probes[len(p.probes)-1].Detail = detail
}

func (p *prober) healthErrors() map[string]string {
	errs := map[string]string{}
	for _, pr := range p.probes {
		if !pr.OK && pr.Method != "web3_clientVersion" && pr.Error != "" {
			errs[pr.Method] = pr.Error
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (p *prober) connectionStatus() string {
	switch {
	case p.authRequired:
		return StatusAuthRequired
	case p.rateLimited:
		return StatusRateLimited
	default:
		return StatusUnreachable
	}
}

func (p *prober) failureStatus(basicOK bool) string {
	switch {
	case p.authRequired && !basicOK:
		return StatusAuthRequired
	case p.rateLimited && !basicOK:
		return StatusRateLimited
	case p.budgetExceeded:
		return StatusDegraded
	case !basicOK:
		return StatusDegraded
	case p.authRequired || p.rateLimited:
		return StatusDegraded
	default:
		return StatusOK
	}
}

func (p *prober) failureMessage() string {
	// Prefer the core reads that determine whether Yangjian can use the
	// endpoint at all. Optional trace/log failures are reported separately and
	// must not hide a failed eth_call.
	for _, method := range []string{"eth_chainId", "eth_getBlockByNumber", "eth_getCode", "eth_call", "eth_getStorageAt"} {
		for i := len(p.probes) - 1; i >= 0; i-- {
			pr := p.probes[i]
			if !pr.OK && pr.Method == method && pr.Error != "" {
				return pr.Method + ": " + pr.Error
			}
		}
	}
	for i := len(p.probes) - 1; i >= 0; i-- {
		pr := p.probes[i]
		if pr.OK || pr.Method == "web3_clientVersion" {
			continue
		}
		if pr.Code == "timeout" || pr.Code == "budget_exceeded" {
			return pr.Method + ": " + pr.Error
		}
	}
	for i := len(p.probes) - 1; i >= 0; i-- {
		pr := p.probes[i]
		if !pr.OK && pr.Method != "web3_clientVersion" && pr.Error != "" {
			return pr.Method + ": " + pr.Error
		}
	}
	if p.rateLimited {
		return "provider rate limited the probe"
	}
	if p.authRequired {
		return "provider requires authentication"
	}
	return "no basic state read succeeded"
}

type probeError struct {
	Code        string
	Message     string
	RateLimited bool
	Auth        bool
	Unsupported bool
	Archive     bool
	RangeLimit  bool
}

func classifyError(err error) probeError {
	if err == nil {
		return probeError{}
	}
	pe := probeError{Code: "error", Message: truncate(err.Error(), 500)}
	if errors.Is(err, context.DeadlineExceeded) {
		pe.Code = "timeout"
		pe.Message = "request timed out: " + pe.Message
		return pe
	}
	if errors.Is(err, context.Canceled) {
		pe.Code = "canceled"
		pe.Message = "request canceled: " + pe.Message
		return pe
	}
	var httpErr rpc.HTTPError
	if errors.As(err, &httpErr) {
		return classifyHTTP(httpErr, pe)
	}
	var httpPtr *rpc.HTTPError
	if errors.As(err, &httpPtr) && httpPtr != nil {
		return classifyHTTP(*httpPtr, pe)
	}
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) {
		pe.Code = fmt.Sprintf("rpc_%d", rpcErr.ErrorCode())
		pe.Message = truncate(rpcErr.Error(), 500)
	}
	lower := strings.ToLower(pe.Message)
	if isAuthText(lower) {
		pe.Auth = true
	}
	if isRateLimitText(lower) {
		pe.RateLimited = true
	}
	if isUnsupportedText(lower) {
		pe.Unsupported = true
	}
	if isArchiveText(lower) {
		pe.Archive = true
	}
	if isRangeText(lower) {
		pe.RangeLimit = true
	}
	return pe
}

func classifyHTTP(httpErr rpc.HTTPError, pe probeError) probeError {
	pe.Code = fmt.Sprintf("http_%d", httpErr.StatusCode)
	pe.Message = truncate(strings.TrimSpace(httpErr.Error()), 500)
	switch {
	case httpErr.StatusCode == 401 || httpErr.StatusCode == 403:
		pe.Auth = true
	case httpErr.StatusCode == 429:
		pe.RateLimited = true
	case httpErr.StatusCode == 404:
		pe.Unsupported = true
	}
	lower := strings.ToLower(pe.Message)
	if isAuthText(lower) {
		pe.Auth = true
	}
	if isRateLimitText(lower) {
		pe.RateLimited = true
	}
	if isUnsupportedText(lower) {
		pe.Unsupported = true
	}
	if strings.Contains(lower, "whitelist") {
		// A method allowlist is a capability gap, not an account-level auth
		// requirement, even when the provider expresses it as HTTP 403.
		pe.Unsupported = true
		pe.Auth = false
	}
	if isArchiveText(lower) {
		pe.Archive = true
	}
	if isRangeText(lower) {
		pe.RangeLimit = true
	}
	return pe
}

func isAuthText(lower string) bool {
	for _, text := range []string{
		"unauthorized", "forbidden", "authentication", "access denied",
		"not authorized", "invalid api key", "invalid apikey", "missing api key",
		"api key is required", "api-key", "apikey required", "invalid key",
	} {
		if strings.Contains(lower, text) {
			return true
		}
	}
	return false
}

func isRateLimitText(lower string) bool {
	for _, text := range []string{
		"rate limit", "ratelimit", "too many requests", "request limit",
		"quota exceeded", "quota", "limit exceeded", "exceeded the limit",
		"try again later", "throttl",
	} {
		if strings.Contains(lower, text) {
			return true
		}
	}
	return false
}

func isUnsupportedText(lower string) bool {
	for _, text := range []string{
		"method not found", "not supported", "unsupported", "does not exist",
		"not implemented", "unknown method", "method is disabled", "method disabled",
		"not enabled", "not exposed", "not available", "whitelist",
	} {
		if strings.Contains(lower, text) {
			return true
		}
	}
	return false
}

func isArchiveText(lower string) bool {
	for _, text := range []string{
		"missing trie node", "historical state", "historical block", "state not available",
		"state is not available", "pruned", "archive node", "archive required",
		"old state", "state trie", "header not found", "block not found", "no state",
	} {
		if strings.Contains(lower, text) {
			return true
		}
	}
	return false
}

func isRangeText(lower string) bool {
	for _, text := range []string{
		"block range", "range too large", "range is too", "more than",
		"max results", "too many results", "query timeout", "exceed maximum",
		"exceeded max", "limit of", "response size", "too large",
	} {
		if strings.Contains(lower, text) {
			return true
		}
	}
	return false
}

func parseHexUint(raw string) (uint64, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "0x")
	raw = strings.TrimPrefix(raw, "0X")
	if raw == "" {
		return 0, errors.New("empty hex value")
	}
	return strconv.ParseUint(raw, 16, 64)
}

func hexUint(value uint64) string {
	return "0x" + strconv.FormatUint(value, 16)
}

func hexBytes(raw string) int {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "0x")
	raw = strings.TrimPrefix(raw, "0X")
	if raw == "" {
		return 0
	}
	return len(raw) / 2
}

func isZeroHex(raw string) bool {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "0x")
	raw = strings.TrimPrefix(raw, "0X")
	if raw == "" {
		return true
	}
	for _, r := range raw {
		if r != '0' {
			return false
		}
	}
	return true
}

func countCallFrames(raw json.RawMessage) int {
	return countCallFramesAt(raw, 0)
}

func countCallFramesAt(raw json.RawMessage, depth int) int {
	if depth > 512 {
		return 0
	}
	var frame struct {
		Type  string            `json:"type"`
		Calls []json.RawMessage `json:"calls"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return 0
	}
	if frame.Type == "" && len(frame.Calls) == 0 {
		return 0
	}
	count := 1
	for _, call := range frame.Calls {
		count += countCallFramesAt(call, depth+1)
		if count > 100_000 {
			return 100_000
		}
	}
	return count
}

func medianLatency(probes []ProbeResult) int64 {
	var values []int64
	for _, pr := range probes {
		if pr.OK && pr.Method != "web3_clientVersion" {
			values = append(values, pr.LatencyMS)
		}
	}
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values[len(values)/2]
}

func truncate(raw string, max int) string {
	raw = strings.Join(strings.Fields(raw), " ")
	if len(raw) <= max {
		return raw
	}
	return raw[:max] + "..."
}

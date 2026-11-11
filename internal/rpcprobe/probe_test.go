package rpcprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fakeNode struct {
	chainID     string
	httpStatus  int
	unsupported map[string]bool
	failArchive bool
	logsRange   bool
	traceOK     bool
	parityOK    bool
	delay       time.Duration
	timestamp   time.Time
}

type rpcRequest struct {
	ID     json.RawMessage   `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

type rpcFailure struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcFailure     `json:"error,omitempty"`
}

func newFakeServer(t *testing.T, node *fakeNode) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if node.delay > 0 {
			time.Sleep(node.delay)
		}
		if node.httpStatus != 0 {
			w.WriteHeader(node.httpStatus)
			_, _ = w.Write([]byte(`{"error":"denied"}`))
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		trimmed := bytes.TrimSpace(body)
		if len(trimmed) > 0 && trimmed[0] == '[' {
			var batch []rpcRequest
			if err := json.Unmarshal(trimmed, &batch); err != nil {
				t.Errorf("decode batch: %v", err)
				return
			}
			responses := make([]rpcResponse, 0, len(batch))
			for _, req := range batch {
				responses = append(responses, node.response(req))
			}
			_ = json.NewEncoder(w).Encode(responses)
			return
		}
		var req rpcRequest
		if err := json.Unmarshal(trimmed, &req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		_ = json.NewEncoder(w).Encode(node.response(req))
	}))
	t.Cleanup(server.Close)
	return server
}

func (n *fakeNode) response(req rpcRequest) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if n.chainID == "" {
		n.chainID = "0x1"
	}
	if n.timestamp.IsZero() {
		n.timestamp = time.Now()
	}
	paramCounts := map[string]int{
		"web3_clientVersion":       0,
		"eth_chainId":              0,
		"eth_blockNumber":          0,
		"eth_getBlockByNumber":     2,
		"eth_getTransactionByHash": 1,
		"eth_getCode":              2,
		"eth_call":                 2,
		"eth_getStorageAt":         3,
		"eth_getProof":             3,
		"eth_feeHistory":           3,
		"eth_getLogs":              1,
		"eth_getBalance":           2,
		"debug_traceTransaction":   2,
		"trace_transaction":        1,
	}
	if want, ok := paramCounts[req.Method]; ok && len(req.Params) != want {
		resp.Error = &rpcFailure{Code: -32602, Message: fmt.Sprintf("expected %d params, got %d", want, len(req.Params))}
		return resp
	}
	if n.unsupported[req.Method] {
		resp.Error = &rpcFailure{Code: -32601, Message: "method not found"}
		return resp
	}
	isArchive := false
	if len(req.Params) > 1 {
		var block string
		if json.Unmarshal(req.Params[1], &block) == nil && block == hexUint(DefaultArchiveBlock) {
			isArchive = true
		}
	}
	if n.failArchive && isArchive && (req.Method == "eth_getBalance" || req.Method == "eth_call") {
		resp.Error = &rpcFailure{Code: -32000, Message: "missing trie node: historical state is pruned"}
		return resp
	}
	switch req.Method {
	case "web3_clientVersion":
		resp.Result = rawJSON(`"fake/v1.0.0"`)
	case "eth_chainId":
		resp.Result = rawJSON(strconv.Quote(n.chainID))
	case "eth_blockNumber":
		resp.Result = rawJSON(`"0x64"`)
	case "eth_getBlockByNumber":
		block := fmt.Sprintf(`{"number":"0x64","hash":"0xabc","timestamp":"0x%x","transactions":["0x1111","0x2222"]}`, n.timestamp.Unix())
		resp.Result = rawJSON(block)
	case "eth_getTransactionByHash":
		resp.Result = rawJSON(`{"hash":"0x1111","to":"0x0000000000000000000000000000000000000001","input":"0x","value":"0x1"}`)
	case "eth_getCode":
		resp.Result = rawJSON(`"0x6080"`)
	case "eth_call":
		resp.Result = rawJSON(`"0x0000000000000000000000000000000000000000000000000000000000000001"`)
	case "eth_getStorageAt":
		resp.Result = rawJSON(`"0x0000000000000000000000000000000000000000000000000000000000000001"`)
	case "eth_getProof":
		resp.Result = rawJSON(`{"accountProof":["0xdead"]}`)
	case "eth_feeHistory":
		resp.Result = rawJSON(`{"baseFeePerGas":["0x1","0x2"]}`)
	case "eth_getLogs":
		if n.logsRange {
			resp.Error = &rpcFailure{Code: -32000, Message: "query exceeds the maximum block range"}
			return resp
		}
		resp.Result = rawJSON(`[{"address":"0x0000000000000000000000000000000000000001"}]`)
	case "eth_getBalance":
		resp.Result = rawJSON(`"0x1"`)
	case "debug_traceTransaction":
		if !n.traceOK {
			resp.Error = &rpcFailure{Code: -32601, Message: "method not found"}
			return resp
		}
		resp.Result = rawJSON(`{"type":"CALL","calls":[{"type":"STATICCALL"}]}`)
	case "trace_transaction":
		if !n.parityOK {
			resp.Error = &rpcFailure{Code: -32601, Message: "method not found"}
			return resp
		}
		resp.Result = rawJSON(`[{"action":{"callType":"call"}}]`)
	default:
		resp.Error = &rpcFailure{Code: -32601, Message: "method not found"}
	}
	return resp
}

func rawJSON(value string) json.RawMessage {
	return json.RawMessage(value)
}

func TestProbeCapabilities(t *testing.T) {
	server := newFakeServer(t, &fakeNode{traceOK: true, parityOK: true})
	result := Probe(context.Background(), Endpoint{URL: server.URL}, Options{
		ChainID:      1,
		Timeout:      2 * time.Second,
		Budget:       5 * time.Second,
		ArchiveBlock: DefaultArchiveBlock,
		LogsWindow:   2,
		Trace:        true,
	})
	if result.Health.Status != StatusOK {
		t.Fatalf("status = %s: %s", result.Health.Status, result.Health.Message)
	}
	if result.ObservedChainID != 1 || result.Head.Number != 100 {
		t.Fatalf("chain/head = %d/%d", result.ObservedChainID, result.Head.Number)
	}
	if result.ClientVersion == "" {
		t.Fatal("client version probe did not succeed")
	}
	caps := result.Capabilities
	if !caps.Blocks || !caps.Logs || !caps.Call || !caps.Code || !caps.Storage || !caps.Proof ||
		!caps.FeeHistory || !caps.Archive || !caps.DebugTrace || !caps.ParityTrace || !caps.Batch {
		t.Fatalf("capabilities = %+v", caps)
	}
	if result.Health.TraceTx != "0x1111" || result.Health.TraceBlock != 100 {
		t.Fatalf("trace target = %s/%d", result.Health.TraceTx, result.Health.TraceBlock)
	}
	if len(result.Probes) == 0 {
		t.Fatal("probe details were not recorded")
	}
}

func TestProbeWrongChain(t *testing.T) {
	server := newFakeServer(t, &fakeNode{chainID: "0x5"})
	result := Probe(context.Background(), Endpoint{URL: server.URL}, Options{ChainID: 1, Timeout: time.Second, Budget: 3 * time.Second})
	if result.Health.Status != StatusWrongChain {
		t.Fatalf("status = %s", result.Health.Status)
	}
	if result.ObservedChainID != 5 {
		t.Fatalf("observed chain = %d", result.ObservedChainID)
	}
	if result.Capabilities.Blocks {
		t.Fatal("wrong-chain endpoint must not be credited with block reads")
	}
}

func TestProbeCapabilityGaps(t *testing.T) {
	server := newFakeServer(t, &fakeNode{
		unsupported: map[string]bool{
			"eth_getProof":           true,
			"eth_feeHistory":         true,
			"eth_getLogs":            true,
			"debug_traceTransaction": true,
			"trace_transaction":      true,
		},
		failArchive: true,
	})
	result := Probe(context.Background(), Endpoint{URL: server.URL}, Options{
		ChainID:      1,
		Timeout:      2 * time.Second,
		Budget:       5 * time.Second,
		ArchiveBlock: DefaultArchiveBlock,
		LogsWindow:   2,
		Trace:        true,
	})
	if result.Health.Status != StatusOK {
		t.Fatalf("status = %s: %s", result.Health.Status, result.Health.Message)
	}
	if result.Capabilities.Logs {
		t.Fatal("logs were wrongly credited after an unsupported error")
	}
	if result.Capabilities.Archive || result.Capabilities.Proof || result.Capabilities.FeeHistory {
		t.Fatalf("capability gaps not recorded: %+v", result.Capabilities)
	}
	if result.Capabilities.DebugTrace || result.Capabilities.ParityTrace {
		t.Fatalf("trace capability gaps not recorded: %+v", result.Capabilities)
	}
	if len(result.Health.Errors) == 0 {
		t.Fatal("probe errors were not preserved")
	}
}

func TestProbeLogsRangeLimitStillSupported(t *testing.T) {
	server := newFakeServer(t, &fakeNode{
		logsRange: true,
		unsupported: map[string]bool{
			"eth_getProof":   true,
			"eth_feeHistory": true,
		},
	})
	// failLogs returns a range-limit message, which proves the method exists.
	result := Probe(context.Background(), Endpoint{URL: server.URL}, Options{
		ChainID:      1,
		Timeout:      2 * time.Second,
		Budget:       5 * time.Second,
		ArchiveBlock: DefaultArchiveBlock,
		LogsWindow:   2,
	})
	if !result.Capabilities.Logs {
		t.Fatalf("logs range limit should still prove support: %+v", result.Health)
	}
}

func TestProbeRateLimited(t *testing.T) {
	server := newFakeServer(t, &fakeNode{httpStatus: http.StatusTooManyRequests})
	result := Probe(context.Background(), Endpoint{URL: server.URL}, Options{ChainID: 1, Timeout: time.Second, Budget: 2 * time.Second})
	if result.Health.Status != StatusRateLimited {
		t.Fatalf("status = %s: %s", result.Health.Status, result.Health.Message)
	}
}

func TestProbeAuthRequired(t *testing.T) {
	server := newFakeServer(t, &fakeNode{httpStatus: http.StatusForbidden})
	result := Probe(context.Background(), Endpoint{URL: server.URL}, Options{ChainID: 1, Timeout: time.Second, Budget: 2 * time.Second})
	if result.Health.Status != StatusAuthRequired {
		t.Fatalf("status = %s: %s", result.Health.Status, result.Health.Message)
	}
}

func TestProbeTimeout(t *testing.T) {
	server := newFakeServer(t, &fakeNode{delay: 200 * time.Millisecond})
	result := Probe(context.Background(), Endpoint{URL: server.URL}, Options{ChainID: 1, Timeout: 20 * time.Millisecond, Budget: time.Second})
	if result.Health.Status != StatusUnreachable {
		t.Fatalf("status = %s: %s", result.Health.Status, result.Health.Message)
	}
	if !strings.Contains(result.Health.Message, "timed out") {
		t.Fatalf("message = %s", result.Health.Message)
	}
}

func TestProbeMissingEnv(t *testing.T) {
	const name = "YANGJIAN_TEST_MISSING_RPC_KEY"
	_ = os.Unsetenv(name)
	result := Probe(context.Background(), Endpoint{URL: "https://example.invalid/${" + name + "}"}, Options{
		ChainID: 1,
		Timeout: time.Second,
		Budget:  time.Second,
	})
	if result.Health.Status != StatusUnconfigured {
		t.Fatalf("status = %s", result.Health.Status)
	}
	if !strings.Contains(result.Health.Message, name) {
		t.Fatalf("message = %s", result.Health.Message)
	}
}

func TestProbeInvalidScheme(t *testing.T) {
	result := Probe(context.Background(), Endpoint{URL: "ftp://example.invalid"}, Options{ChainID: 1})
	if result.Health.Status != StatusUnreachable {
		t.Fatalf("status = %s", result.Health.Status)
	}
	if !strings.Contains(result.Health.Message, "unsupported URL scheme") {
		t.Fatalf("message = %s", result.Health.Message)
	}
}

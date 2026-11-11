package trace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

const (
	testTxHash = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testEOA    = "0x1000000000000000000000000000000000000001"
	testRouter = "0x2000000000000000000000000000000000000002"
	testPool   = "0x3000000000000000000000000000000000000003"
)

type fakeRPC struct {
	responses map[string]json.RawMessage
	errors    map[string]error
	code      map[string][]byte
	codeErrs  map[string]error
}

func (f *fakeRPC) RawCall(ctx context.Context, result any, method string, args ...any) error {
	if err := f.errors[method]; err != nil {
		return err
	}
	raw, ok := f.responses[method]
	if !ok {
		return fmt.Errorf("no fake response for %s", method)
	}
	target, ok := result.(*json.RawMessage)
	if !ok {
		return fmt.Errorf("fake result is not *json.RawMessage")
	}
	*target = append(json.RawMessage(nil), raw...)
	return nil
}

func (f *fakeRPC) CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	key := strings.ToLower(account.Hex())
	if err := f.codeErrs[key]; err != nil {
		return nil, err
	}
	return f.code[key], nil
}

type fakeRPCError struct {
	code    int
	message string
}

func (e fakeRPCError) Error() string  { return e.message }
func (e fakeRPCError) ErrorCode() int { return e.code }

func testTransactionJSON() json.RawMessage {
	return json.RawMessage(`{
	  "type":"0x0","nonce":"0x1","gasPrice":"0x1","gas":"0x5208",
	  "to":"` + testRouter + `","value":"0x0","input":"0x12345678",
	  "v":"0x1b","r":"0x1","s":"0x1",
	  "hash":"` + testTxHash + `"
	}`)
}

func testReceiptJSON() json.RawMessage {
	return json.RawMessage(`{
	  "transactionHash":"` + testTxHash + `","transactionIndex":"0x0",
	  "blockHash":"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	  "blockNumber":"0x64","from":"` + testEOA + `","to":"` + testRouter + `",
	  "cumulativeGasUsed":"0x5208","gasUsed":"0x5208","contractAddress":null,
	  "logs":[],"logsBloom":"0x` + strings.Repeat("00", 256) + `","status":"0x1",
	  "effectiveGasPrice":"0x1"
	}`)
}

func callTracerJSON() json.RawMessage {
	return json.RawMessage(`{
	  "type":"CALL","from":"` + testEOA + `","to":"` + testRouter + `",
	  "input":"0x12345678","output":"0x","gas":"0x100","gasUsed":"0x50","value":"0x0",
	  "calls":[
	    {"type":"STATICCALL","from":"` + testRouter + `","to":"` + testPool + `",
	     "input":"0xdeadbeef","output":"0x01","gas":"0x50","gasUsed":"0x20","calls":[]}
	  ]
	}`)
}

func parityTraceJSON() json.RawMessage {
	return json.RawMessage(`[
	  {"action":{"callType":"call","from":"` + testEOA + `","to":"` + testRouter + `",
	    "gas":"0x100","input":"0x12345678","value":"0x0"},
	   "result":{"gasUsed":"0x50","output":"0x"},"traceAddress":[],"type":"call"},
	  {"action":{"callType":"staticcall","from":"` + testRouter + `","to":"` + testPool + `",
	    "gas":"0x50","input":"0xdeadbeef","value":"0x0"},
	   "result":{"gasUsed":"0x20","output":"0x01"},"traceAddress":[0],"type":"call"}
	]`)
}

func newFakeRPC() *fakeRPC {
	return &fakeRPC{
		responses: map[string]json.RawMessage{
			"eth_getTransactionByHash":  testTransactionJSON(),
			"eth_getTransactionReceipt": testReceiptJSON(),
			"debug_traceTransaction":    callTracerJSON(),
		},
		errors: map[string]error{},
		code: map[string][]byte{
			testRouter: []byte{0x60, 0x00},
			testPool:   []byte{0x60, 0x01},
		},
		codeErrs: map[string]error{},
	}
}

func runTraceFixture(t *testing.T, rpc RPC) (*store.Store, Result, error) {
	t.Helper()
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	result, err := RunTransaction(context.Background(), Params{
		ChainID:     1,
		TxHash:      common.HexToHash(testTxHash),
		Store:       st,
		RunID:       "trace-run",
		RPC:         rpc,
		ToolVersion: "test",
	})
	return st, result, err
}

func TestRunTransactionCallTracer(t *testing.T) {
	st, result, err := runTraceFixture(t, newFakeRPC())
	if err != nil {
		t.Fatalf("run trace: %v", err)
	}
	if result.TraceProvider != "debug_traceTransaction" || result.TraceMethod != "debug_traceTransaction" {
		t.Fatalf("provider = %s/%s", result.TraceProvider, result.TraceMethod)
	}
	if result.CallCount != 2 || len(result.Calls) != 2 {
		t.Fatalf("calls = %+v", result.Calls)
	}
	if result.Calls[0].Selector != "0x12345678" || result.Calls[0].Path != "0" {
		t.Fatalf("root call = %+v", result.Calls[0])
	}
	if result.Calls[1].Path != "0.0" || result.Calls[1].Depth != 1 || result.Calls[1].Selector != "0xdeadbeef" {
		t.Fatalf("child call = %+v", result.Calls[1])
	}
	if len(result.RelationIDs) != 2 {
		t.Fatalf("relations = %v", result.RelationIDs)
	}

	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.DossierType != "transaction" || manifest.Status != "complete" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.Target.TransactionHash != testTxHash {
		t.Fatalf("target = %+v", manifest.Target)
	}
	for _, artifact := range manifest.Artifacts {
		data, err := os.ReadFile(st.Path(artifact.Path))
		if err != nil {
			t.Fatalf("%s: %v", artifact.Path, err)
		}
		if hash := model.ContentHash(data); hash != artifact.Hash {
			t.Fatalf("%s hash = %s, want %s", artifact.Path, artifact.Hash, hash)
		}
	}

	var relation model.Relation
	if err := st.ReadJSON(relationPath(manifest.RelationIDs[0]), &relation); err != nil {
		t.Fatal(err)
	}
	if relation.Type != "observed_call" || !strings.HasPrefix(relation.From, "account:1:") {
		t.Fatalf("root relation = %+v", relation)
	}
	if relation.Attributes["tx_hash"] != testTxHash {
		t.Fatalf("attributes = %v", relation.Attributes)
	}

	report, err := os.ReadFile(st.Path("report.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"not a security audit", "## Sequence", "observed_call", "debug_traceTransaction"} {
		if !strings.Contains(string(report), want) {
			t.Fatalf("report missing %q", want)
		}
	}
	sequence, err := os.ReadFile(st.Path("sequence.mmd"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sequence), "sequenceDiagram") || !strings.Contains(string(sequence), "->>") {
		t.Fatalf("sequence = %s", sequence)
	}
}

func TestRunTransactionFallsBackToParity(t *testing.T) {
	rpc := newFakeRPC()
	delete(rpc.responses, "debug_traceTransaction")
	rpc.errors["debug_traceTransaction"] = fakeRPCError{code: -32601, message: "method not found"}
	rpc.responses["trace_transaction"] = parityTraceJSON()
	_, result, err := runTraceFixture(t, rpc)
	if err != nil {
		t.Fatalf("run trace: %v", err)
	}
	if result.TraceMethod != "trace_transaction" {
		t.Fatalf("method = %s", result.TraceMethod)
	}
	if result.CallCount != 2 || result.Calls[1].Path != "0.0" {
		t.Fatalf("calls = %+v", result.Calls)
	}
	if len(result.ObservationIDs) < 5 {
		t.Fatalf("observations = %v", result.ObservationIDs)
	}
}

func TestRunTransactionUnsupported(t *testing.T) {
	rpc := newFakeRPC()
	delete(rpc.responses, "debug_traceTransaction")
	rpc.errors["debug_traceTransaction"] = fakeRPCError{code: -32601, message: "method not found"}
	rpc.errors["trace_transaction"] = fakeRPCError{code: -32601, message: "method not found"}
	st, _, err := runTraceFixture(t, rpc)
	if !errors.Is(err, ErrTraceUnsupported) {
		t.Fatalf("err = %v", err)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatalf("failure manifest: %v", err)
	}
	if manifest.Status != "failed" {
		t.Fatalf("status = %s", manifest.Status)
	}
	if len(manifest.OpenQuestions) == 0 || !strings.Contains(manifest.OpenQuestions[0], "trace unavailable") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	if len(manifest.ObservationIDs) < 4 {
		t.Fatalf("observations = %v", manifest.ObservationIDs)
	}
}

func TestRunTransactionMissingReceipt(t *testing.T) {
	rpc := newFakeRPC()
	rpc.responses["eth_getTransactionReceipt"] = json.RawMessage("null")
	_, _, err := runTraceFixture(t, rpc)
	if !errors.Is(err, ErrTransactionNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunTransactionCodeCheckFailure(t *testing.T) {
	rpc := newFakeRPC()
	rpc.codeErrs[testRouter] = errors.New("code unavailable")
	_, result, err := runTraceFixture(t, rpc)
	if err != nil {
		t.Fatalf("run trace: %v", err)
	}
	found := false
	for _, question := range result.OpenQuestions {
		if strings.Contains(question, "code check failed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("open questions = %v", result.OpenQuestions)
	}
	if len(result.RelationIDs) != 2 {
		t.Fatalf("relations = %v", result.RelationIDs)
	}
}

func TestNormalizeCallTracerTruncates(t *testing.T) {
	raw := json.RawMessage(`{"from":"` + testEOA + `","to":"` + testRouter + `","input":"0x","calls":[
	  {"from":"` + testRouter + `","to":"` + testPool + `","input":"0x01","calls":[]}
	]}`)
	calls, warnings, err := normalizeCallTracer(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "truncated") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestNormalizeParityTraceInvalidAddress(t *testing.T) {
	raw := json.RawMessage(`[
	  {"action":{"callType":"call","from":"not-an-address","to":"` + testRouter + `","input":"0x01"},"traceAddress":[],"type":"call"}
	]`)
	calls, warnings, err := normalizeParityTrace(raw, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].From != "" {
		t.Fatalf("calls = %+v", calls)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning")
	}
}

func TestUnsupportedError(t *testing.T) {
	if !unsupportedError(fakeRPCError{code: -32601, message: "x"}) {
		t.Fatal("expected unsupported")
	}
	if unsupportedError(errors.New("transaction not found")) {
		t.Fatal("transaction not found is not an unsupported-method error")
	}
	if !unsupportedError(errors.New("the method debug_traceTransaction does not exist")) {
		t.Fatal("expected unsupported message")
	}
}

func TestReceiptStatus(t *testing.T) {
	if receiptStatus(1) != "success" || receiptStatus(0) != "reverted" {
		t.Fatalf("receiptStatus = %s %s", receiptStatus(1), receiptStatus(0))
	}
	if !strings.Contains(receiptStatus(7), "unknown") {
		t.Fatal("expected unknown status")
	}
}

func TestRunTransactionRelationIDsStable(t *testing.T) {
	_, first, err := runTraceFixture(t, newFakeRPC())
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := runTraceFixture(t, newFakeRPC())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(first.RelationIDs, "|") != strings.Join(second.RelationIDs, "|") {
		t.Fatalf("relations differ: %v %v", first.RelationIDs, second.RelationIDs)
	}
}

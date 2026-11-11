package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

const maxTraceResponseBytes = 32 << 20

var (
	ErrTransactionNotFound = errors.New("transaction or receipt not found")
	ErrTraceUnsupported    = errors.New("no supported transaction trace method is available")
	ErrTraceInvalid        = errors.New("trace response is invalid")
)

type RPC interface {
	RawCall(ctx context.Context, result any, method string, args ...any) error
	CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
}

type Params struct {
	ChainID       int64
	TxHash        common.Hash
	Store         *store.Store
	RunID         string
	RPC           RPC
	ToolVersion   string
	MaxCalls      int
	MaxCodeChecks int
}

type Call struct {
	Path         string `json:"path"`
	Depth        int    `json:"depth"`
	Type         string `json:"type,omitempty"`
	From         string `json:"from,omitempty"`
	To           string `json:"to,omitempty"`
	Selector     string `json:"selector,omitempty"`
	InputHash    string `json:"input_hash,omitempty"`
	InputSize    int    `json:"input_size"`
	OutputHash   string `json:"output_hash,omitempty"`
	OutputSize   int    `json:"output_size"`
	Gas          string `json:"gas,omitempty"`
	GasUsed      string `json:"gas_used,omitempty"`
	Value        string `json:"value,omitempty"`
	Error        string `json:"error,omitempty"`
	RevertReason string `json:"revert_reason,omitempty"`
}

type Result struct {
	SchemaVersion      string         `json:"schema_version"`
	ChainID            int64          `json:"chain_id"`
	TransactionHash    string         `json:"transaction_hash"`
	Block              model.BlockRef `json:"block"`
	From               string         `json:"from"`
	To                 string         `json:"to,omitempty"`
	Value              string         `json:"value,omitempty"`
	Status             uint64         `json:"status"`
	GasUsed            uint64         `json:"gas_used"`
	TraceProvider      string         `json:"trace_provider"`
	TraceMethod        string         `json:"trace_method"`
	Calls              []Call         `json:"calls"`
	CallCount          int            `json:"call_count"`
	Warnings           []string       `json:"warnings,omitempty"`
	OpenQuestions      []string       `json:"open_questions"`
	ObservationIDs     []string       `json:"observation_ids"`
	RelationIDs        []string       `json:"relation_ids"`
	ContractIDs        []string       `json:"contract_ids"`
	TraceObservationID string         `json:"trace_observation_id"`
	RawTraceArtifact   string         `json:"raw_trace_artifact"`
	TraceArtifact      string         `json:"trace_artifact"`
	TransactionPath    string         `json:"transaction_path"`
	ReportPath         string         `json:"report_path"`
	SequencePath       string         `json:"sequence_path"`
	RelationsPath      string         `json:"relations_path"`
	EvidencePath       string         `json:"evidence_path"`
	CreatedAt          time.Time      `json:"created_at"`
}

type traceAttempt struct {
	Provider       string
	Method         string
	Params         []any
	Raw            json.RawMessage
	Err            error
	ObservationID  string
	ObservationIDs []string
}

func RunTransaction(ctx context.Context, params Params) (Result, error) {
	if params.Store == nil {
		return Result{}, fmt.Errorf("store is nil")
	}
	if params.RPC == nil {
		return Result{}, fmt.Errorf("trace rpc is nil")
	}
	if params.RunID == "" {
		return Result{}, fmt.Errorf("run id is required")
	}
	if params.TxHash == (common.Hash{}) {
		return Result{}, fmt.Errorf("transaction hash is required")
	}
	if params.MaxCalls <= 0 {
		params.MaxCalls = 10000
	}
	if params.MaxCodeChecks <= 0 {
		params.MaxCodeChecks = 256
	}
	now := time.Now().UTC()
	observationIDs := make([]string, 0)
	openQuestions := make([]string, 0)
	artifacts := make([]model.Artifact, 0)
	addObservation := func(id string) {
		if id != "" {
			observationIDs = appendUnique(observationIDs, id)
		}
	}
	addArtifact := func(kind, rel string, data []byte) error {
		if _, err := params.Store.WriteFile(rel, data); err != nil {
			return err
		}
		artifacts = append(artifacts, model.Artifact{Kind: kind, Path: rel, Hash: model.ContentHash(data)})
		return nil
	}

	rawTx, txObservationID, err := fetchRaw(ctx, params, "eth_getTransactionByHash", []any{params.TxHash.Hex()}, nil)
	addObservation(txObservationID)
	if err != nil {
		return Result{}, fmt.Errorf("fetch transaction: %w", err)
	}
	if isJSONNull(rawTx) {
		return Result{}, fmt.Errorf("%w: %s", ErrTransactionNotFound, params.TxHash.Hex())
	}
	var tx types.Transaction
	if err := tx.UnmarshalJSON(rawTx); err != nil {
		return Result{}, fmt.Errorf("decode transaction: %w", err)
	}
	if err := addArtifact("transaction_raw", "artifacts/transaction-raw.json", appendIndent(rawTx)); err != nil {
		return Result{}, err
	}

	rawReceipt, receiptObservationID, err := fetchRaw(ctx, params, "eth_getTransactionReceipt", []any{params.TxHash.Hex()}, nil)
	addObservation(receiptObservationID)
	if err != nil {
		return Result{}, fmt.Errorf("fetch receipt: %w", err)
	}
	if isJSONNull(rawReceipt) {
		return Result{}, fmt.Errorf("%w: receipt for %s is unavailable (pending or pruned)", ErrTransactionNotFound, params.TxHash.Hex())
	}
	var receipt types.Receipt
	if err := receipt.UnmarshalJSON(rawReceipt); err != nil {
		return Result{}, fmt.Errorf("decode receipt: %w", err)
	}
	var receiptMeta struct {
		From string `json:"from"`
	}
	if err := json.Unmarshal(rawReceipt, &receiptMeta); err != nil {
		return Result{}, fmt.Errorf("decode receipt sender: %w", err)
	}
	from, _ := cleanAddress(receiptMeta.From)
	if from == "" {
		return Result{}, fmt.Errorf("%w: receipt has no valid sender", ErrTraceInvalid)
	}
	if receipt.BlockNumber == nil || receipt.BlockHash == (common.Hash{}) {
		return Result{}, fmt.Errorf("%w: receipt has no block", ErrTraceInvalid)
	}
	blockRef := model.BlockRef{
		ChainID: params.ChainID,
		Number:  receipt.BlockNumber.Uint64(),
		Hash:    receipt.BlockHash.Hex(),
	}
	if err := addArtifact("receipt", "artifacts/receipt-raw.json", appendIndent(rawReceipt)); err != nil {
		return Result{}, err
	}

	attempt, calls, warnings, err := fetchTrace(ctx, params, blockRef)
	for _, id := range attempt.ObservationIDs {
		addObservation(id)
	}
	if err != nil {
		writeFailureManifest(params, blockRef, &tx, observationIDs, artifacts, err)
		return Result{}, err
	}
	for _, warning := range warnings {
		openQuestions = appendUnique(openQuestions, warning)
	}

	rawName := "artifacts/trace-calltracer.json"
	if attempt.Method == "trace_transaction" {
		rawName = "artifacts/trace-parity.json"
	}
	if err := addArtifact("trace_raw", rawName, appendIndent(attempt.Raw)); err != nil {
		return Result{}, err
	}
	callsData, err := json.MarshalIndent(calls, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encode trace calls: %w", err)
	}
	if err := addArtifact("trace", "artifacts/trace-calls.json", append(callsData, '\n')); err != nil {
		return Result{}, err
	}

	endpoints, codeObservations, codeWarnings, err := classifyEndpoints(ctx, params, calls, blockRef)
	if err != nil {
		return Result{}, err
	}
	for _, id := range codeObservations {
		addObservation(id)
	}
	for _, warning := range codeWarnings {
		openQuestions = appendUnique(openQuestions, warning)
	}

	relationIDs := make([]string, 0)
	for _, call := range calls {
		from, fromOK := endpoints[call.From]
		to, toOK := endpoints[call.To]
		if !fromOK || !toOK {
			continue
		}
		relation, err := writeObservedCall(params, blockRef, attempt.ObservationID, rawName, call, from, to)
		if err != nil {
			return Result{}, err
		}
		relationIDs = appendUnique(relationIDs, relation.ID)
	}
	if len(calls) == 0 {
		openQuestions = appendUnique(openQuestions, "trace returned no call frames")
	}

	contractIDs := make([]string, 0)
	for _, endpoint := range endpoints {
		if endpoint.Kind == "contract" {
			contractIDs = appendUnique(contractIDs, endpoint.ID)
		}
	}
	sort.Strings(contractIDs)

	result := Result{
		SchemaVersion:      model.SchemaVersion,
		ChainID:            params.ChainID,
		TransactionHash:    params.TxHash.Hex(),
		Block:              blockRef,
		From:               strings.ToLower(from),
		To:                 transferTarget(tx.To()),
		Value:              tx.Value().String(),
		Status:             receipt.Status,
		GasUsed:            receipt.GasUsed,
		TraceProvider:      attempt.Provider,
		TraceMethod:        attempt.Method,
		Calls:              calls,
		CallCount:          len(calls),
		Warnings:           warnings,
		OpenQuestions:      openQuestions,
		ObservationIDs:     observationIDs,
		RelationIDs:        relationIDs,
		ContractIDs:        contractIDs,
		TraceObservationID: attempt.ObservationID,
		RawTraceArtifact:   rawName,
		TraceArtifact:      "artifacts/trace-calls.json",
		TransactionPath:    "transaction.json",
		ReportPath:         "report.md",
		SequencePath:       "sequence.mmd",
		RelationsPath:      "relations.json",
		EvidencePath:       "evidence.json",
		CreatedAt:          now,
	}

	summaryData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encode transaction summary: %w", err)
	}
	if err := addArtifact("transaction", "transaction.json", append(summaryData, '\n')); err != nil {
		return Result{}, err
	}
	if err := writeTraceReport(params, &result, artifacts); err != nil {
		return Result{}, err
	}
	for _, rel := range []struct {
		kind string
		path string
	}{
		{"report", "report.md"},
		{"sequence", "sequence.mmd"},
		{"relations", "relations.json"},
		{"evidence", "evidence.json"},
	} {
		data, err := readFile(params.Store, rel.path)
		if err != nil {
			return Result{}, err
		}
		artifacts = append(artifacts, model.Artifact{Kind: rel.kind, Path: rel.path, Hash: model.ContentHash(data)})
	}

	reportData, err := readFile(params.Store, result.ReportPath)
	if err != nil {
		return Result{}, err
	}
	reportObservation, err := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "report",
		RunID:         params.RunID,
		Request: map[string]any{
			"command": "trace transaction",
			"stage":   "report",
			"formats": []string{"markdown", "mermaid", "json"},
		},
		ResponseHash: model.ContentHash(reportData),
		Block:        &blockRef,
		RetrievedAt:  time.Now().UTC(),
		Source:       model.SourceRef{Provider: "yangjian", Version: params.ToolVersion, EndpointClass: "local"},
		Meta: map[string]any{
			"report_path":   result.ReportPath,
			"sequence_path": result.SequencePath,
			"relations":     len(relationIDs),
			"calls":         len(calls),
		},
	})
	if err != nil {
		return Result{}, fmt.Errorf("write report observation: %w", err)
	}
	addObservation(reportObservation.ID)
	result.ObservationIDs = observationIDs

	manifest := model.Manifest{
		SchemaVersion:  model.SchemaVersion,
		DossierType:    "transaction",
		Target:         model.TargetRef{ChainID: params.ChainID, Address: result.To, TransactionHash: result.TransactionHash},
		ToolVersion:    params.ToolVersion,
		RunID:          params.RunID,
		Status:         "partial",
		Block:          blockRef,
		Artifacts:      artifacts,
		ContractIDs:    contractIDs,
		ObservationIDs: observationIDs,
		RelationIDs:    relationIDs,
		OpenQuestions:  openQuestions,
		CreatedAt:      now,
		UpdatedAt:      time.Now().UTC(),
	}
	if len(openQuestions) == 0 {
		manifest.Status = "complete"
	}
	if _, err := params.Store.WriteJSON("manifest.json", manifest); err != nil {
		return Result{}, fmt.Errorf("write transaction manifest: %w", err)
	}
	return result, nil
}

func fetchRaw(ctx context.Context, params Params, method string, args []any, block *model.BlockRef) (json.RawMessage, string, error) {
	var raw json.RawMessage
	err := params.RPC.RawCall(ctx, &raw, method, args...)
	responseHash := model.ContentHash(raw)
	var observationError *model.ObservationError
	if err != nil {
		responseHash = model.ContentHash([]byte(err.Error()))
		code := "rpc_error"
		if unsupportedError(err) {
			code = "rpc_method_unsupported"
		}
		observationError = &model.ObservationError{Code: code, Message: err.Error()}
	}
	obs, writeErr := params.Store.WriteObservation(model.Observation{
		SchemaVersion: model.SchemaVersion,
		Kind:          "rpc_call",
		RunID:         params.RunID,
		Request:       map[string]any{"method": method, "params": args},
		ResponseHash:  responseHash,
		Block:         block,
		RetrievedAt:   time.Now().UTC(),
		Source:        model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
		Error:         observationError,
	})
	if writeErr != nil {
		return nil, "", writeErr
	}
	return raw, obs.ID, err
}

func fetchTrace(ctx context.Context, params Params, blockRef model.BlockRef) (traceAttempt, []Call, []string, error) {
	attempts := []traceAttempt{
		{
			Provider: "debug_traceTransaction",
			Method:   "debug_traceTransaction",
			Params:   []any{params.TxHash.Hex(), map[string]any{"tracer": "callTracer"}},
		},
		{
			Provider: "trace_transaction",
			Method:   "trace_transaction",
			Params:   []any{params.TxHash.Hex()},
		},
	}
	failures := make([]string, 0)
	allObservationIDs := make([]string, 0)
	for _, attempt := range attempts {
		raw, observationID, err := fetchRaw(ctx, params, attempt.Method, attempt.Params, &blockRef)
		attempt.Raw = raw
		attempt.ObservationID = observationID
		attempt.Err = err
		if observationID != "" {
			allObservationIDs = appendUnique(allObservationIDs, observationID)
		}
		attempt.ObservationIDs = append([]string{}, allObservationIDs...)
		if err != nil {
			failures = append(failures, attempt.Method+": "+err.Error())
			if unsupportedError(err) {
				continue
			}
			return attempt, nil, nil, fmt.Errorf("%s failed: %w", attempt.Method, err)
		}
		if isJSONNull(raw) {
			return attempt, nil, nil, fmt.Errorf("%w: %s returned null", ErrTraceInvalid, attempt.Method)
		}
		if len(raw) > maxTraceResponseBytes {
			return attempt, nil, nil, fmt.Errorf("%w: trace response is %d bytes, cap is %d", ErrTraceInvalid, len(raw), maxTraceResponseBytes)
		}
		var calls []Call
		var warnings []string
		switch attempt.Method {
		case "debug_traceTransaction":
			calls, warnings, err = normalizeCallTracer(raw, params.MaxCalls)
		case "trace_transaction":
			calls, warnings, err = normalizeParityTrace(raw, params.MaxCalls)
		}
		if err != nil {
			return attempt, nil, nil, err
		}
		return attempt, calls, warnings, nil
	}
	return traceAttempt{ObservationIDs: allObservationIDs}, nil, nil, fmt.Errorf("%w: %s", ErrTraceUnsupported, strings.Join(failures, "; "))
}

func writeFailureManifest(params Params, blockRef model.BlockRef, tx *types.Transaction, observationIDs []string, artifacts []model.Artifact, traceErr error) {
	target := ""
	if tx.To() != nil {
		target = strings.ToLower(tx.To().Hex())
	}
	now := time.Now().UTC()
	manifest := model.Manifest{
		SchemaVersion:  model.SchemaVersion,
		DossierType:    "transaction",
		Target:         model.TargetRef{ChainID: params.ChainID, Address: target, TransactionHash: params.TxHash.Hex()},
		ToolVersion:    params.ToolVersion,
		RunID:          params.RunID,
		Status:         "failed",
		Block:          blockRef,
		Artifacts:      artifacts,
		ContractIDs:    []string{},
		ObservationIDs: observationIDs,
		RelationIDs:    []string{},
		OpenQuestions:  []string{"trace unavailable: " + traceErr.Error()},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_, _ = params.Store.WriteJSON("manifest.json", manifest)
}

func classifyEndpoints(ctx context.Context, params Params, calls []Call, blockRef model.BlockRef) (map[string]endpoint, []string, []string, error) {
	addresses := make([]string, 0)
	for _, call := range calls {
		if call.From != "" {
			addresses = appendUnique(addresses, call.From)
		}
		if call.To != "" {
			addresses = appendUnique(addresses, call.To)
		}
	}
	endpoints := make(map[string]endpoint, len(addresses))
	observations := make([]string, 0)
	warnings := make([]string, 0)
	blockNumber := new(big.Int).SetUint64(blockRef.Number)
	for _, address := range addresses {
		if common.HexToAddress(address) == (common.Address{}) {
			endpoints[address] = endpoint{ID: model.AccountID(params.ChainID, address), Kind: "account", Checked: true}
			continue
		}
		if len(observations) >= params.MaxCodeChecks {
			warnings = appendUnique(warnings, fmt.Sprintf("code checks stopped at %d unique addresses; some observed_call endpoints are unclassified", params.MaxCodeChecks))
			endpoints[address] = endpoint{ID: model.AccountID(params.ChainID, address), Kind: "account", Checked: false}
			continue
		}
		code, err := params.RPC.CodeAt(ctx, common.HexToAddress(address), blockNumber)
		responseHash := model.ContentHash(code)
		var observationError *model.ObservationError
		if err != nil {
			responseHash = model.ContentHash([]byte(err.Error()))
			observationError = &model.ObservationError{Code: "rpc_error", Message: err.Error()}
		}
		obs, writeErr := params.Store.WriteObservation(model.Observation{
			SchemaVersion: model.SchemaVersion,
			Kind:          "rpc_call",
			RunID:         params.RunID,
			Request: map[string]any{
				"method": "eth_getCode",
				"params": []any{strings.ToLower(address), fmt.Sprintf("0x%x", blockRef.Number)},
			},
			ResponseHash: responseHash,
			Block:        &blockRef,
			RetrievedAt:  time.Now().UTC(),
			Source:       model.SourceRef{Provider: "rpc", EndpointClass: "configured"},
			Error:        observationError,
			Meta:         map[string]any{"code_size": len(code)},
		})
		if writeErr != nil {
			return nil, nil, nil, writeErr
		}
		observations = append(observations, obs.ID)
		if err != nil {
			warnings = appendUnique(warnings, fmt.Sprintf("code check failed for %s: %v", shortAddress(address), err))
			endpoints[address] = endpoint{ID: model.AccountID(params.ChainID, address), Kind: "account", Checked: false}
			continue
		}
		if len(code) > 0 {
			endpoints[address] = endpoint{ID: model.ContractID(params.ChainID, address), Kind: "contract", Checked: true, CodeSize: len(code)}
		} else {
			endpoints[address] = endpoint{ID: model.AccountID(params.ChainID, address), Kind: "account", Checked: true}
		}
	}
	return endpoints, observations, warnings, nil
}

type endpoint struct {
	ID       string
	Kind     string
	Checked  bool
	CodeSize int
}

func writeObservedCall(params Params, blockRef model.BlockRef, observationID, traceArtifact string, call Call, from, to endpoint) (model.Relation, error) {
	evidence := observationID + "#" + call.Path
	id := model.RelationID("observed_call", from.ID, to.ID, evidence)
	attributes := map[string]any{
		"tx_hash":           params.TxHash.Hex(),
		"trace_reference":   observationID,
		"trace_artifact":    traceArtifact,
		"call_path":         call.Path,
		"call_type":         call.Type,
		"selector":          call.Selector,
		"from_kind":         from.Kind,
		"to_kind":           to.Kind,
		"contract_creation": call.Type == "create" || call.Type == "create2",
	}
	if from.CodeSize > 0 {
		attributes["from_code_size"] = from.CodeSize
	}
	if to.CodeSize > 0 {
		attributes["to_code_size"] = to.CodeSize
	}
	if call.Error != "" {
		attributes["error"] = call.Error
	}
	relation := model.Relation{
		SchemaVersion: model.SchemaVersion,
		ID:            id,
		Type:          "observed_call",
		From:          from.ID,
		To:            to.ID,
		BlockScope:    &model.BlockScope{FromBlock: blockRef.Number},
		Attributes:    attributes,
		EvidenceRefs:  []string{observationID},
	}
	if _, err := params.Store.WriteJSON(relationPath(id), relation); err != nil {
		return model.Relation{}, err
	}
	return relation, nil
}

func relationPath(id string) string {
	hash := strings.TrimPrefix(model.ContentHash([]byte(id)), "sha256:")
	return filepath.Join("entities", "relations", hash+".json")
}

func unsupportedError(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr rpc.Error
	if errors.As(err, &rpcErr) && rpcErr.ErrorCode() == -32601 {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"method not found",
		"method is not available",
		"not supported",
		"unsupported method",
		"tracer not found",
		"unknown tracer",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return strings.Contains(message, "does not exist") && strings.Contains(message, "method")
}

func isJSONNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

func appendIndent(raw json.RawMessage) []byte {
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, raw, "", "  "); err != nil {
		return append([]byte(raw), '\n')
	}
	return append(buffer.Bytes(), '\n')
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func transferTarget(to *common.Address) string {
	if to == nil {
		return ""
	}
	return strings.ToLower(to.Hex())
}

func shortAddress(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:6] + "…" + value[len(value)-4:]
}

func readFile(st *store.Store, rel string) ([]byte, error) {
	data, err := os.ReadFile(st.Path(rel))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return data, nil
}

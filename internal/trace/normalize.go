package trace

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
)

const maxTraceDepth = 128

type callTracerFrame struct {
	Type         string            `json:"type"`
	From         string            `json:"from"`
	To           string            `json:"to"`
	Input        string            `json:"input"`
	Output       string            `json:"output"`
	Gas          string            `json:"gas"`
	GasUsed      string            `json:"gasUsed"`
	Value        string            `json:"value"`
	Error        string            `json:"error"`
	RevertReason string            `json:"revertReason"`
	Calls        []callTracerFrame `json:"calls"`
}

func normalizeCallTracer(raw json.RawMessage, maxCalls int) ([]Call, []string, error) {
	var frame callTracerFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return nil, nil, fmt.Errorf("%w: decode callTracer output: %v", ErrTraceInvalid, err)
	}
	if frame.From == "" && frame.To == "" && frame.Type == "" && len(frame.Calls) == 0 && frame.Input == "" {
		return nil, nil, fmt.Errorf("%w: callTracer returned an empty frame", ErrTraceInvalid)
	}
	calls := make([]Call, 0)
	warnings := make([]string, 0)
	truncated := false
	var walk func(frame callTracerFrame, path string, depth int)
	walk = func(frame callTracerFrame, path string, depth int) {
		if len(calls) >= maxCalls {
			if !truncated {
				warnings = append(warnings, fmt.Sprintf("trace truncated at %d calls", maxCalls))
				truncated = true
			}
			return
		}
		if depth > maxTraceDepth {
			warnings = appendUnique(warnings, fmt.Sprintf("trace depth truncated at %d", maxTraceDepth))
			return
		}
		call := Call{
			Path:         path,
			Depth:        depth,
			Type:         normalizeCallType(frame.Type),
			Gas:          safeShort(frame.Gas, 32),
			GasUsed:      safeShort(frame.GasUsed, 32),
			Value:        safeShort(frame.Value, 80),
			Error:        safeShort(frame.Error, 512),
			RevertReason: safeShort(frame.RevertReason, 512),
		}
		call.From, _ = cleanAddress(frame.From)
		if call.From == "" && frame.From != "" {
			warnings = appendUnique(warnings, "trace contained an invalid caller address")
		}
		call.To, _ = cleanAddress(frame.To)
		if call.To == "" && frame.To != "" {
			warnings = appendUnique(warnings, "trace contained an invalid callee address")
		}
		if call.Type != "create" && call.Type != "create2" && frame.To == "" {
			warnings = appendUnique(warnings, fmt.Sprintf("trace call %s has no callee", path))
		}
		call.InputHash, call.InputSize, call.Selector = describeData(frame.Input)
		call.OutputHash, call.OutputSize, _ = describeData(frame.Output)
		if frame.Input != "" && call.InputHash == "" {
			warnings = appendUnique(warnings, fmt.Sprintf("trace call %s has invalid input data", path))
		}
		calls = append(calls, call)
		for i, child := range frame.Calls {
			walk(child, fmt.Sprintf("%s.%d", path, i), depth+1)
		}
	}
	walk(frame, "0", 0)
	return calls, warnings, nil
}

type parityTrace struct {
	Action struct {
		CallType string `json:"callType"`
		From     string `json:"from"`
		To       string `json:"to"`
		Gas      string `json:"gas"`
		Input    string `json:"input"`
		Value    string `json:"value"`
	} `json:"action"`
	Result *struct {
		GasUsed string `json:"gasUsed"`
		Output  string `json:"output"`
		Address string `json:"address"`
	} `json:"result"`
	TraceAddress []int  `json:"traceAddress"`
	Error        string `json:"error"`
	Type         string `json:"type"`
}

func normalizeParityTrace(raw json.RawMessage, maxCalls int) ([]Call, []string, error) {
	var frames []parityTrace
	if err := json.Unmarshal(raw, &frames); err != nil {
		return nil, nil, fmt.Errorf("%w: decode trace_transaction output: %v", ErrTraceInvalid, err)
	}
	calls := make([]Call, 0, len(frames))
	warnings := make([]string, 0)
	for i, frame := range frames {
		if len(calls) >= maxCalls {
			warnings = appendUnique(warnings, fmt.Sprintf("trace truncated at %d calls", maxCalls))
			break
		}
		path := parityPath(frame.TraceAddress)
		depth := len(frame.TraceAddress)
		if depth > maxTraceDepth {
			warnings = appendUnique(warnings, fmt.Sprintf("trace depth truncated at %d", maxTraceDepth))
			continue
		}
		call := Call{
			Path:  path,
			Depth: depth,
			Type:  normalizeCallType(firstNonEmpty(frame.Action.CallType, frame.Type)),
			Gas:   safeShort(frame.Action.Gas, 32),
			Error: safeShort(frame.Error, 512),
			Value: safeShort(frame.Action.Value, 80),
			From:  "",
			To:    "",
		}
		call.From, _ = cleanAddress(frame.Action.From)
		if call.From == "" && frame.Action.From != "" {
			warnings = appendUnique(warnings, "trace contained an invalid caller address")
		}
		call.To, _ = cleanAddress(frame.Action.To)
		if call.To == "" && frame.Result != nil && frame.Result.Address != "" {
			call.To, _ = cleanAddress(frame.Result.Address)
		}
		if call.To == "" && frame.Action.To != "" {
			warnings = appendUnique(warnings, "trace contained an invalid callee address")
		}
		call.InputHash, call.InputSize, call.Selector = describeData(frame.Action.Input)
		if frame.Result != nil {
			call.GasUsed = safeShort(frame.Result.GasUsed, 32)
			call.OutputHash, call.OutputSize, _ = describeData(frame.Result.Output)
		}
		if call.Type != "create" && call.Type != "create2" && call.To == "" {
			warnings = appendUnique(warnings, fmt.Sprintf("trace call %d has no callee", i))
		}
		calls = append(calls, call)
	}
	return calls, warnings, nil
}

func parityPath(address []int) string {
	parts := make([]string, 0, len(address)+1)
	parts = append(parts, "0")
	for _, index := range address {
		parts = append(parts, fmt.Sprintf("%d", index))
	}
	return strings.Join(parts, ".")
}

func normalizeCallType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "call", "delegatecall", "staticcall", "callcode", "create", "create2", "selfdestruct", "suicide":
		if normalized == "suicide" {
			return "selfdestruct"
		}
		return normalized
	case "":
		return "call"
	default:
		return safeShort(normalized, 32)
	}
}

func cleanAddress(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || !common.IsHexAddress(value) {
		return "", false
	}
	return common.HexToAddress(value).Hex(), true
}

func describeData(value string) (hash string, size int, selector string) {
	raw, ok := decodeHexData(value)
	if !ok {
		return "", 0, ""
	}
	hash = model.ContentHash(raw)
	size = len(raw)
	if len(raw) >= 4 {
		selector = "0x" + hex.EncodeToString(raw[:4])
	}
	return hash, size, selector
}

func decodeHexData(value string) ([]byte, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []byte{}, true
	}
	trimmed := strings.TrimPrefix(strings.TrimPrefix(value, "0X"), "0x")
	if len(trimmed)%2 != 0 {
		return nil, false
	}
	decoded, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, false
	}
	return decoded, true
}

func safeShort(value string, limit int) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	if len(value) > limit {
		return value[:limit] + "…"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

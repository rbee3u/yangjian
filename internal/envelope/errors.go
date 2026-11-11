package envelope

type Code string

const (
	CodeRPCTimeout           Code = "rpc_timeout"
	CodeRPCRateLimited       Code = "rpc_rate_limited"
	CodeRPCUnavailable       Code = "rpc_unavailable"
	CodeRPCMethodUnsupported Code = "rpc_method_unsupported"
	CodeBlockUnavailable     Code = "block_unavailable"
	CodeSourceNotFound       Code = "source_not_found"
	CodeSourceMatchFailed    Code = "source_match_failed"
	CodeCompilerUnavailable  Code = "compiler_unavailable"
	CodeCompilerError        Code = "compiler_error"
	CodeProxyUnresolved      Code = "proxy_unresolved"
	CodeTraceInvalid         Code = "trace_invalid"
	CodeSchemaInvalid        Code = "schema_invalid"
	CodeBudgetExceeded       Code = "budget_exceeded"
	CodeConfigInvalid        Code = "config_invalid"
	CodeTargetNotFound       Code = "target_not_found"
	CodeUsage                Code = "usage"
	CodeStageNotImplemented  Code = "stage_not_implemented"
	CodeInternal             Code = "internal"
)

type Error struct {
	Code      Code           `json:"code"`
	Category  string         `json:"category"`
	Retryable bool           `json:"retryable"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
}

type codeInfo struct {
	Category  string
	Retryable bool
}

var codeInfos = map[Code]codeInfo{
	CodeRPCTimeout:           {Category: "transport", Retryable: true},
	CodeRPCRateLimited:       {Category: "transport", Retryable: true},
	CodeRPCUnavailable:       {Category: "transport", Retryable: true},
	CodeRPCMethodUnsupported: {Category: "capability", Retryable: false},
	CodeBlockUnavailable:     {Category: "capability", Retryable: false},
	CodeSourceNotFound:       {Category: "source", Retryable: false},
	CodeSourceMatchFailed:    {Category: "provenance", Retryable: false},
	CodeCompilerUnavailable:  {Category: "tooling", Retryable: true},
	CodeCompilerError:        {Category: "tooling", Retryable: false},
	CodeProxyUnresolved:      {Category: "analysis", Retryable: false},
	CodeTraceInvalid:         {Category: "provider", Retryable: false},
	CodeSchemaInvalid:        {Category: "internal", Retryable: false},
	CodeBudgetExceeded:       {Category: "policy", Retryable: false},
	CodeConfigInvalid:        {Category: "usage", Retryable: false},
	CodeTargetNotFound:       {Category: "usage", Retryable: false},
	CodeUsage:                {Category: "usage", Retryable: false},
	CodeStageNotImplemented:  {Category: "internal", Retryable: false},
	CodeInternal:             {Category: "internal", Retryable: false},
}

func NewError(code Code, message string, details map[string]any) *Error {
	info, ok := codeInfos[code]
	if !ok {
		info = codeInfos[CodeInternal]
	}
	return &Error{
		Code:      code,
		Category:  info.Category,
		Retryable: info.Retryable,
		Message:   message,
		Details:   details,
	}
}

func (e *Error) ExitCode() int {
	switch e.Code {
	case CodeConfigInvalid, CodeTargetNotFound, CodeUsage:
		return 2
	default:
		return 1
	}
}

func IsRetryable(code Code) bool {
	info, ok := codeInfos[code]
	return ok && info.Retryable
}

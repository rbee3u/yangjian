package envelope

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSuccessRoundTrip(t *testing.T) {
	env := Success("version", map[string]string{"version": "dev"}, Meta{DurationMS: 3})
	if !env.OK || env.Error != nil {
		t.Fatalf("unexpected success envelope: %+v", env)
	}
	var buf bytes.Buffer
	if err := Write(&buf, env); err != nil {
		t.Fatalf("write: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["schema_version"] != SchemaVersion {
		t.Fatalf("schema_version = %v", decoded["schema_version"])
	}
	if decoded["ok"] != true {
		t.Fatalf("ok = %v", decoded["ok"])
	}
	if ExitCode(env) != 0 {
		t.Fatalf("exit code = %d", ExitCode(env))
	}
}

func TestFailureExitCodes(t *testing.T) {
	cases := []struct {
		code Code
		want int
	}{
		{CodeConfigInvalid, 2},
		{CodeTargetNotFound, 2},
		{CodeUsage, 2},
		{CodeRPCTimeout, 1},
		{CodeSourceMatchFailed, 1},
	}
	for _, tc := range cases {
		env := Failure("contract dossier", NewError(tc.code, "boom", nil), Meta{})
		if env.OK || env.Error == nil {
			t.Fatalf("%s: expected failure envelope", tc.code)
		}
		if got := ExitCode(env); got != tc.want {
			t.Fatalf("%s: exit code = %d, want %d", tc.code, got, tc.want)
		}
	}
}

func TestErrorMetadata(t *testing.T) {
	err := NewError(CodeRPCTimeout, "timeout", map[string]any{"endpoint": "x"})
	if err.Category != "transport" || !err.Retryable {
		t.Fatalf("unexpected error metadata: %+v", err)
	}
	if !IsRetryable(CodeRPCTimeout) {
		t.Fatal("expected rpc_timeout to be retryable")
	}
	if IsRetryable(CodeSourceNotFound) {
		t.Fatal("expected source_not_found to be non-retryable")
	}
}

func TestRunID(t *testing.T) {
	a := NewRunID()
	b := NewRunID()
	if a == "" || b == "" {
		t.Fatal("run id must not be empty")
	}
	if a == b {
		t.Fatal("run ids must differ")
	}
}

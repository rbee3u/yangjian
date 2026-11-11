package chain

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"
)

func TestCheckReadOnly(t *testing.T) {
	for _, method := range []string{"eth_getCode", "eth_call", "eth_getStorageAt", "debug_traceTransaction", "trace_transaction"} {
		if err := CheckReadOnly(method); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	for _, method := range []string{"eth_sendRawTransaction", "eth_sendTransaction", "eth_sign", "personal_unlockAccount", "wallet_sendCalls", "admin_addPeer"} {
		if err := CheckReadOnly(method); err == nil {
			t.Fatalf("%s: expected rejection", method)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	if !isRetryable(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded should be retryable")
	}
	if isRetryable(context.Canceled) {
		t.Fatal("canceled should not be retryable")
	}
	if !isRetryable(&rpc.HTTPError{StatusCode: 429}) {
		t.Fatal("429 should be retryable")
	}
	if !isRetryable(&rpc.HTTPError{StatusCode: 503}) {
		t.Fatal("503 should be retryable")
	}
	if isRetryable(&rpc.HTTPError{StatusCode: 400}) {
		t.Fatal("400 should not be retryable")
	}
	if !isRetryable(rpc.HTTPError{StatusCode: 429}) {
		t.Fatal("HTTPError value 429 should be retryable")
	}
	if isRetryable(rpc.HTTPError{StatusCode: 404}) {
		t.Fatal("HTTPError value 404 should not be retryable")
	}
}

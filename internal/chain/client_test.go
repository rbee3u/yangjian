package chain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rbee3u/yangjian/internal/config"
)

func TestClientChainID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if request.Method != "eth_chainId" {
			t.Errorf("method = %q", request.Method)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  "0x89",
		})
	}))
	defer server.Close()

	client, err := Dial(context.Background(), config.ProviderProfile{
		Endpoint:     server.URL,
		Timeout:      "5s",
		RateLimitRPS: 100,
		Burst:        1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	chainID, err := client.ChainID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if chainID != 137 {
		t.Fatalf("chain id = %d, want 137", chainID)
	}
}

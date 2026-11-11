package fixtures

import (
	"context"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/chain"
	"github.com/rbee3u/yangjian/internal/config"
	"github.com/rbee3u/yangjian/internal/dossier"
	"github.com/rbee3u/yangjian/internal/source"
	"github.com/rbee3u/yangjian/internal/store"
)

func TestLiveAcceptanceSourcifyPool(t *testing.T) {
	if os.Getenv("YANGJIAN_LIVE") != "1" {
		t.Skip("set YANGJIAN_LIVE=1 to run live provider acceptance")
	}
	fixture := loadFixture(t)
	cfg := config.Default()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	client, err := chain.Dial(ctx, cfg.RPCProfile())
	if err != nil {
		t.Fatalf("dial rpc: %v", err)
	}
	defer client.Close()
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	address := common.HexToAddress(fixture.Address)
	code, err := dossier.RunCode(ctx, client, dossier.CodeParams{
		ChainID: fixture.ChainID,
		Address: address,
		Store:   st,
		RunID:   "live-acceptance",
	})
	if err != nil {
		t.Fatalf("code stage: %v", err)
	}
	expectedHash := fixtureRuntimeHash(t, fixture)
	if code.RuntimeBytecodeHash != expectedHash {
		t.Fatalf("runtime hash = %s, fixture = %s", code.RuntimeBytecodeHash, expectedHash)
	}
	fetcher, err := source.NewSourcify(cfg.Providers.Sourcify)
	if err != nil {
		t.Fatalf("sourcify client: %v", err)
	}
	result, err := dossier.RunSource(ctx, dossier.SourceParams{
		ChainID: fixture.ChainID,
		Address: address,
		Store:   st,
		RunID:   "live-acceptance",
		Fetcher: fetcher,
	})
	if err != nil {
		t.Fatalf("source stage: %v", err)
	}
	if result.SourceCount == 0 || result.SourceHashMismatches != 0 || result.MissingSources != 0 {
		t.Fatalf("source result = %+v", result)
	}
	if result.Compiler != fixture.Sourcify.Compilation.CompilerVersion {
		t.Fatalf("compiler = %s, fixture = %s", result.Compiler, fixture.Sourcify.Compilation.CompilerVersion)
	}
}

func fixtureRuntimeHash(t *testing.T, fixture sourceFixture) string {
	t.Helper()
	code, err := hex.DecodeString(trimHex(fixture.RuntimeBytecode))
	if err != nil {
		t.Fatal(err)
	}
	return "keccak256:" + hex.EncodeToString(crypto.Keccak256(code))
}

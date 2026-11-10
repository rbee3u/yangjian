package dossier

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

func newAnalyzeDossier(t *testing.T, code []byte, storage map[common.Hash][]byte) (*store.Store, common.Address, *fakeChain) {
	t.Helper()
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	address := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	chain := &fakeChain{header: testHeader(), code: code, storage: storage}
	if _, err := RunCode(context.Background(), chain, CodeParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-code",
	}); err != nil {
		t.Fatal(err)
	}
	return st, address, chain
}

func analyzeSlot(name string) common.Hash {
	for _, slot := range proxySlots {
		if slot.name == name {
			return slot.hash
		}
	}
	return common.Hash{}
}

func addressWord(address common.Address) []byte {
	word := make([]byte, 32)
	copy(word[12:], address.Bytes())
	return word
}

func runAnalyze(t *testing.T, st *store.Store, address common.Address, chain *fakeChain) AnalyzeResult {
	t.Helper()
	result, err := RunAnalyze(context.Background(), AnalyzeParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-analyze",
		Reader:  chain,
	})
	if err != nil {
		t.Fatalf("run analyze: %v", err)
	}
	return result
}

func TestRunAnalyzeNoProxy(t *testing.T) {
	st, address, chain := newAnalyzeDossier(t, []byte{0x60, 0x80}, nil)
	result := runAnalyze(t, st, address, chain)
	if result.ProxyType != "none" || result.ImplementationAddress != "" {
		t.Fatalf("result = %+v", result)
	}
	if result.Static != nil || !contains(result.StaticUnavailable, "source revision is missing") {
		t.Fatalf("static result = %+v, unavailable = %v", result.Static, result.StaticUnavailable)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if contains(manifest.OpenQuestions, "proxy detection not run") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	if len(manifest.ObservationIDs) != 7 {
		t.Fatalf("observations = %v", manifest.ObservationIDs)
	}
	version, _, err := loadCurrentVersion(manifest, st)
	if err != nil {
		t.Fatal(err)
	}
	if version.ProxyType != "none" {
		t.Fatalf("version = %+v", version)
	}
	if _, err := os.Stat(st.Path(result.Artifact)); err != nil {
		t.Fatal(err)
	}
}

func TestRunAnalyzeEIP1967(t *testing.T) {
	implementation := common.HexToAddress("0x1111111111111111111111111111111111111111")
	storage := map[common.Hash][]byte{
		analyzeSlot("eip1967.implementation"): addressWord(implementation),
	}
	st, address, chain := newAnalyzeDossier(t, []byte{0x60, 0x80}, storage)
	result := runAnalyze(t, st, address, chain)
	if result.ProxyType != "EIP-1967" || result.ImplementationAddress != implementation.Hex() {
		t.Fatalf("result = %+v", result)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	version, _, err := loadCurrentVersion(manifest, st)
	if err != nil {
		t.Fatal(err)
	}
	if version.ProxyType != "EIP-1967" || version.ImplementationAddress != implementation.Hex() {
		t.Fatalf("version = %+v", version)
	}
}

func TestRunAnalyzeMinimalProxy(t *testing.T) {
	implementation := common.HexToAddress("0x2222222222222222222222222222222222222222")
	code := append([]byte{}, minimalProxyPrefix...)
	code = append(code, implementation.Bytes()...)
	code = append(code, minimalProxySuffix...)
	st, address, chain := newAnalyzeDossier(t, code, nil)
	result := runAnalyze(t, st, address, chain)
	if result.ProxyType != "EIP-1167" || result.ImplementationAddress != implementation.Hex() {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunAnalyzeBeacon(t *testing.T) {
	beacon := common.HexToAddress("0x3333333333333333333333333333333333333333")
	implementation := common.HexToAddress("0x4444444444444444444444444444444444444444")
	storage := map[common.Hash][]byte{
		analyzeSlot("eip1967.beacon"): addressWord(beacon),
	}
	st, address, chain := newAnalyzeDossier(t, []byte{0x60, 0x80}, storage)
	encoded, err := beaconABI.Methods["implementation"].Outputs.Pack(implementation)
	if err != nil {
		t.Fatal(err)
	}
	chain.callResponses = map[string][]byte{
		hex.EncodeToString(beaconABI.Methods["implementation"].ID): encoded,
	}
	result := runAnalyze(t, st, address, chain)
	if result.ProxyType != "beacon" || result.BeaconAddress != beacon.Hex() || result.ImplementationAddress != implementation.Hex() {
		t.Fatalf("result = %+v", result)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if contains(manifest.OpenQuestions, "beacon proxy implementation not resolved") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
}

func TestRunAnalyzeBeaconUnresolved(t *testing.T) {
	beacon := common.HexToAddress("0x3333333333333333333333333333333333333333")
	storage := map[common.Hash][]byte{
		analyzeSlot("eip1967.beacon"): addressWord(beacon),
	}
	st, address, chain := newAnalyzeDossier(t, []byte{0x60, 0x80}, storage)
	result := runAnalyze(t, st, address, chain)
	if result.ProxyType != "beacon" || result.ImplementationAddress != "" {
		t.Fatalf("result = %+v", result)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if !contains(manifest.OpenQuestions, "beacon proxy implementation not resolved") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
}

func TestRunAnalyzeBeaconCallError(t *testing.T) {
	beacon := common.HexToAddress("0x3333333333333333333333333333333333333333")
	storage := map[common.Hash][]byte{
		analyzeSlot("eip1967.beacon"): addressWord(beacon),
	}
	st, address, chain := newAnalyzeDossier(t, []byte{0x60, 0x80}, storage)
	chain.callErr = errors.New("rpc down")
	result := runAnalyze(t, st, address, chain)
	if result.ImplementationAddress != "" || len(result.Warnings) == 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunAnalyzeEIP1822(t *testing.T) {
	implementation := common.HexToAddress("0x4444444444444444444444444444444444444444")
	storage := map[common.Hash][]byte{
		analyzeSlot("eip1822.proxiable"): addressWord(implementation),
	}
	st, address, chain := newAnalyzeDossier(t, []byte{0x60, 0x80}, storage)
	result := runAnalyze(t, st, address, chain)
	if result.ProxyType != "EIP-1822" || result.ImplementationAddress != implementation.Hex() {
		t.Fatalf("result = %+v", result)
	}
}

func TestRunAnalyzeStorageError(t *testing.T) {
	st, address, chain := newAnalyzeDossier(t, []byte{0x60}, nil)
	chain.storageErr = errors.New("rpc down")
	_, err := RunAnalyze(context.Background(), AnalyzeParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-analyze",
		Reader:  chain,
	})
	if err == nil {
		t.Fatal("expected storage error")
	}
}

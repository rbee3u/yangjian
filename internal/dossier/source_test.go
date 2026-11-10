package dossier

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/source"
	"github.com/rbee3u/yangjian/internal/store"
)

type fakeFetcher struct {
	record source.Record
	err    error
}

func (f fakeFetcher) Fetch(ctx context.Context, chainID int64, address string) (source.Record, error) {
	return f.record, f.err
}

func makeSourceRecord(t *testing.T, content string, metadataHash string) source.Record {
	t.Helper()
	if metadataHash == "" {
		metadataHash = "0x" + hex.EncodeToString(crypto.Keccak256([]byte(content)))
	}
	metadata := map[string]any{
		"compiler": map[string]any{"version": "0.7.6+commit.7338295f"},
		"settings": map[string]any{
			"compilationTarget": map[string]string{"contracts/Pool.sol": "Pool"},
			"evmVersion":        "istanbul",
			"libraries":         map[string]any{},
			"metadata":          map[string]string{"bytecodeHash": "none"},
			"optimizer":         map[string]any{"enabled": true, "runs": 800},
			"remappings":        []string{},
		},
		"sources": map[string]any{
			"contracts/Pool.sol": map[string]string{"keccak256": metadataHash},
		},
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	stdJSON, err := json.Marshal(map[string]any{
		"language": "Solidity",
		"sources": map[string]any{
			"contracts/Pool.sol": map[string]string{"content": content},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := source.Settings{EVMVersion: "istanbul"}
	settings.Metadata.BytecodeHash = "none"
	settings.Optimizer.Enabled = true
	settings.Optimizer.Runs = 800
	return source.Record{
		Address:       "0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8",
		ChainID:       1,
		Match:         "match",
		CreationMatch: "match",
		RuntimeMatch:  "match",
		VerifiedAt:    "2025-06-23T06:08:56Z",
		ABI: json.RawMessage(`[
		  {"type":"function","name":"slot0","inputs":[],"outputs":[
		    {"name":"sqrtPriceX96","type":"uint160"},
		    {"name":"tick","type":"int24"},
		    {"name":"observationIndex","type":"uint16"},
		    {"name":"observationCardinality","type":"uint16"},
		    {"name":"observationCardinalityNext","type":"uint16"},
		    {"name":"feeProtocol","type":"uint8"},
		    {"name":"unlocked","type":"bool"}],"stateMutability":"view"},
		  {"type":"function","name":"liquidity","inputs":[],"outputs":[
		    {"name":"","type":"uint128"}],"stateMutability":"view"},
		  {"type":"function","name":"ticks","inputs":[
		    {"name":"tick","type":"int24"}],"outputs":[
		    {"name":"liquidityGross","type":"uint128"},
		    {"name":"liquidityNet","type":"int128"},
		    {"name":"feeGrowthOutside0X128","type":"uint256"},
		    {"name":"feeGrowthOutside1X128","type":"uint256"},
		    {"name":"tickCumulativeOutside","type":"int56"},
		    {"name":"secondsPerLiquidityOutsideX128","type":"uint160"},
		    {"name":"secondsOutside","type":"uint32"},
		    {"name":"initialized","type":"bool"}],"stateMutability":"view"},
		  {"type":"function","name":"token0","inputs":[],"outputs":[{"name":"","type":"address"}],"stateMutability":"view"},
		  {"type":"function","name":"token1","inputs":[],"outputs":[{"name":"","type":"address"}],"stateMutability":"view"},
		  {"type":"function","name":"factory","inputs":[],"outputs":[{"name":"","type":"address"}],"stateMutability":"view"},
		  {"type":"function","name":"fee","inputs":[],"outputs":[{"name":"","type":"uint24"}],"stateMutability":"view"},
		  {"type":"function","name":"tickSpacing","inputs":[],"outputs":[{"name":"","type":"int24"}],"stateMutability":"view"},
		  {"type":"function","name":"maxLiquidityPerTick","inputs":[],"outputs":[{"name":"","type":"uint128"}],"stateMutability":"view"}
		]`),
		Metadata: metadataJSON,
		Sources: map[string]source.Source{
			"contracts/Pool.sol": {Content: content},
		},
		StdJSONInput: stdJSON,
		Compilation: source.Compilation{
			Compiler:         "solc",
			CompilerVersion:  "0.7.6+commit.7338295f",
			CompilerSettings: settings,
		},
		StorageLayout: json.RawMessage(`{"storage":[],"types":{}}`),
		Raw:           []byte(`{"mock":true}`),
	}
}

func newCodeDossier(t *testing.T) (*store.Store, common.Address) {
	t.Helper()
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	address := common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8")
	chain := &fakeChain{header: testHeader(), code: []byte{0x60, 0x80}}
	if _, err := RunCode(context.Background(), chain, CodeParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-code",
	}); err != nil {
		t.Fatal(err)
	}
	return st, address
}

func TestRunSource(t *testing.T) {
	st, address := newCodeDossier(t)
	content := "// SPDX-License-Identifier: MIT\npragma solidity 0.7.6;\ncontract Pool {}\n"
	record := makeSourceRecord(t, content, "")
	params := SourceParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-source",
		Fetcher: fakeFetcher{record: record},
	}
	result, err := RunSource(context.Background(), params)
	if err != nil {
		t.Fatalf("run source: %v", err)
	}
	if result.RegistryMatch != "match" {
		t.Fatalf("match = %s", result.RegistryMatch)
	}
	if result.Compiler != "0.7.6+commit.7338295f" {
		t.Fatalf("compiler = %s", result.Compiler)
	}
	if result.SourceCount != 1 || result.SourceHashMismatches != 0 || result.MissingSources != 0 {
		t.Fatalf("result = %+v", result)
	}
	if result.ProvenanceLevel != "P0" || result.Correspondence != "unproven" {
		t.Fatalf("provenance = %s/%s", result.ProvenanceLevel, result.Correspondence)
	}

	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.SourceRevisionIDs) != 1 {
		t.Fatalf("source revisions = %v", manifest.SourceRevisionIDs)
	}
	if len(manifest.ObservationIDs) != 3 {
		t.Fatalf("observations = %v", manifest.ObservationIDs)
	}
	if contains(manifest.OpenQuestions, "source provenance not resolved") {
		t.Fatal("source question should be resolved")
	}
	if !contains(manifest.OpenQuestions, "local P2 rebuild not attempted") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	if !contains(manifest.OpenQuestions, "P1 unavailable: metadata.bytecodeHash=none; P2 required") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}

	sourcePath := "entities/sources/" + strings.TrimPrefix(result.SourceRevisionID, "sha256:") + ".json"
	var revision model.SourceRevision
	if err := st.ReadJSON(sourcePath, &revision); err != nil {
		t.Fatal(err)
	}
	if revision.ProvenanceLevel != "P0" || revision.Correspondence != "unproven" {
		t.Fatalf("revision = %+v", revision)
	}
	if len(revision.Sources) != 1 || revision.Sources[0].ContentRef == "" {
		t.Fatalf("sources = %+v", revision.Sources)
	}
	if _, err := os.Stat(st.Path(revision.Sources[0].ContentRef)); err != nil {
		t.Fatalf("content ref: %v", err)
	}

	var version model.ContractVersion
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == "contract_version" {
			if err := st.ReadJSON(artifact.Path, &version); err != nil {
				t.Fatal(err)
			}
		}
	}
	if version.SourceRevisionID != result.SourceRevisionID {
		t.Fatalf("version source revision = %s", version.SourceRevisionID)
	}
	if version.ABIHash == "" {
		t.Fatal("abi hash must be set")
	}

	second, err := RunSource(context.Background(), params)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second.SourceRevisionID != result.SourceRevisionID {
		t.Fatalf("source revision changed: %s != %s", second.SourceRevisionID, result.SourceRevisionID)
	}
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.SourceRevisionIDs) != 1 {
		t.Fatalf("duplicate source revisions: %v", manifest.SourceRevisionIDs)
	}
}

func TestRunSourceHashMismatch(t *testing.T) {
	st, address := newCodeDossier(t)
	content := "contract Pool {}"
	record := makeSourceRecord(t, content, "0x"+strings.Repeat("11", 32))
	result, err := RunSource(context.Background(), SourceParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-source",
		Fetcher: fakeFetcher{record: record},
	})
	if err != nil {
		t.Fatalf("run source: %v", err)
	}
	if result.SourceHashMismatches != 1 {
		t.Fatalf("mismatches = %d", result.SourceHashMismatches)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if !contains(manifest.OpenQuestions, "1 source hash mismatches") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
}

func TestRunSourceNeedsDossier(t *testing.T) {
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	_, err := RunSource(context.Background(), SourceParams{
		ChainID: 1,
		Address: common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"),
		Store:   st,
		RunID:   "run-source",
		Fetcher: fakeFetcher{record: makeSourceRecord(t, "contract Pool {}", "")},
	})
	if !errors.Is(err, ErrDossierNotInitialized) {
		t.Fatalf("error = %v", err)
	}
}

func contains(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

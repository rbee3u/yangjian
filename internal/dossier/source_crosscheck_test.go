package dossier

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

func runSourceCrossCheck(t *testing.T, cross SourceFetcher, skipReason string) (*store.Store, SourceResult, model.Manifest) {
	t.Helper()
	st, address := newCodeDossier(t)
	content := "// SPDX-License-Identifier: MIT\npragma solidity 0.7.6;\ncontract Pool {}\n"
	record := makeSourceRecord(t, content, "")
	result, err := RunSource(context.Background(), SourceParams{
		ChainID:              1,
		Address:              address,
		Store:                st,
		RunID:                "run-source",
		Fetcher:              fakeFetcher{record: record},
		CrossCheck:           cross,
		CrossCheckName:       "etherscan",
		CrossCheckSkipReason: skipReason,
	})
	if err != nil {
		t.Fatalf("run source: %v", err)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	return st, result, manifest
}

func TestSourceCrossCheckMatch(t *testing.T) {
	content := "// SPDX-License-Identifier: MIT\npragma solidity 0.7.6;\ncontract Pool {}\n"
	record := makeSourceRecord(t, content, "")
	st, result, manifest := runSourceCrossCheck(t, fakeFetcher{record: record}, "")
	if result.CrossCheck == nil || result.CrossCheck.Status != "match" {
		t.Fatalf("cross-check = %+v", result.CrossCheck)
	}
	if len(result.ObservationIDs) != 2 || len(manifest.ObservationIDs) != 4 {
		t.Fatalf("observations = %v / %v", result.ObservationIDs, manifest.ObservationIDs)
	}
	for _, question := range manifest.OpenQuestions {
		if strings.HasPrefix(question, "Etherscan cross-check") {
			t.Fatalf("open questions = %v", manifest.OpenQuestions)
		}
	}
	foundArtifact := false
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == "etherscan_response" {
			foundArtifact = true
			if _, err := os.Stat(st.Path(artifact.Path)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !foundArtifact {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	revision, err := readRevision(t, st, manifest.SourceRevisionIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	foundProvider := false
	for _, registrySource := range revision.RegistrySources {
		if registrySource.Provider == "etherscan" {
			foundProvider = true
		}
	}
	if !foundProvider {
		t.Fatalf("registry sources = %+v", revision.RegistrySources)
	}
	report, err := RunReport(ReportParams{ChainID: 1, Address: common.HexToAddress("0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8"), Store: st, RunID: "run-report"})
	if err != nil {
		t.Fatalf("run report: %v", err)
	}
	raw, err := os.ReadFile(st.Path(report.ReportPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "### Source cross-check") || !strings.Contains(string(raw), "| Status | match |") {
		t.Fatalf("report did not render the cross-check:\n%s", raw)
	}
}

func TestSourceCrossCheckMismatch(t *testing.T) {
	content := "// SPDX-License-Identifier: MIT\npragma solidity 0.7.6;\ncontract Pool {}\n"
	record := makeSourceRecord(t, content, "")
	secondary := record
	secondary.Compilation.CompilerVersion = "0.8.0+commit.test"
	_, result, manifest := runSourceCrossCheck(t, fakeFetcher{record: secondary}, "")
	if result.CrossCheck == nil || result.CrossCheck.Status != "mismatch" || len(result.CrossCheck.Differences) == 0 {
		t.Fatalf("cross-check = %+v", result.CrossCheck)
	}
	found := false
	for _, question := range manifest.OpenQuestions {
		if strings.HasPrefix(question, "Etherscan cross-check mismatch") {
			found = true
		}
	}
	if !found {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
}

func TestSourceCrossCheckSkipped(t *testing.T) {
	_, result, manifest := runSourceCrossCheck(t, nil, "ETHERSCAN_API_KEY is not set")
	if result.CrossCheck == nil || result.CrossCheck.Status != "skipped" {
		t.Fatalf("cross-check = %+v", result.CrossCheck)
	}
	found := false
	for _, question := range manifest.OpenQuestions {
		if strings.HasPrefix(question, "Etherscan cross-check skipped") {
			found = true
		}
	}
	if !found {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
}

func TestSourceCrossCheckUnavailable(t *testing.T) {
	content := "// SPDX-License-Identifier: MIT\npragma solidity 0.7.6;\ncontract Pool {}\n"
	record := makeSourceRecord(t, content, "")
	_, result, manifest := runSourceCrossCheck(t, fakeFetcher{record: record, err: errors.New("explorer down")}, "")
	if result.CrossCheck == nil || result.CrossCheck.Status != "unavailable" {
		t.Fatalf("cross-check = %+v", result.CrossCheck)
	}
	found := false
	for _, question := range manifest.OpenQuestions {
		if strings.HasPrefix(question, "Etherscan cross-check unavailable") {
			found = true
		}
	}
	if !found {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
}

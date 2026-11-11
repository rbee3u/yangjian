package trace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rbee3u/yangjian/internal/model"
)

const (
	maxReportCalls   = 1000
	maxSequenceCalls = 200
)

type traceRelationsDocument struct {
	SchemaVersion   string           `json:"schema_version"`
	TransactionHash string           `json:"transaction_hash"`
	Block           model.BlockRef   `json:"block"`
	Relations       []model.Relation `json:"relations"`
}

type traceEvidenceDocument struct {
	SchemaVersion   string              `json:"schema_version"`
	TransactionHash string              `json:"transaction_hash"`
	Block           model.BlockRef      `json:"block"`
	From            string              `json:"from"`
	To              string              `json:"to,omitempty"`
	Status          uint64              `json:"status"`
	GasUsed         uint64              `json:"gas_used"`
	TraceProvider   string              `json:"trace_provider"`
	TraceMethod     string              `json:"trace_method"`
	Calls           []Call              `json:"calls"`
	Observations    []model.Observation `json:"observations"`
	Relations       []model.Relation    `json:"relations"`
	Artifacts       []model.Artifact    `json:"artifacts"`
	OpenQuestions   []string            `json:"open_questions"`
}

func writeTraceReport(params Params, result *Result, artifacts []model.Artifact) error {
	relations := make([]model.Relation, 0, len(result.RelationIDs))
	for _, id := range result.RelationIDs {
		var relation model.Relation
		if err := params.Store.ReadJSON(relationPath(id), &relation); err != nil {
			return fmt.Errorf("read observed_call relation %s: %w", id, err)
		}
		relations = append(relations, relation)
	}
	sort.Slice(relations, func(i, j int) bool { return relations[i].ID < relations[j].ID })

	sequence := renderSequence(result)
	if _, err := params.Store.WriteFile(result.SequencePath, []byte(sequence)); err != nil {
		return err
	}
	relationsData, err := json.MarshalIndent(traceRelationsDocument{
		SchemaVersion:   model.SchemaVersion,
		TransactionHash: result.TransactionHash,
		Block:           result.Block,
		Relations:       relations,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode trace relations: %w", err)
	}
	if _, err := params.Store.WriteFile(result.RelationsPath, append(relationsData, '\n')); err != nil {
		return err
	}
	if _, err := params.Store.WriteFile(result.ReportPath, []byte(renderTraceReport(params, *result, relations))); err != nil {
		return err
	}

	allArtifacts := append([]model.Artifact{}, artifacts...)
	for _, rel := range []struct {
		kind string
		path string
	}{
		{"report", result.ReportPath},
		{"sequence", result.SequencePath},
		{"relations", result.RelationsPath},
	} {
		data, err := os.ReadFile(params.Store.Path(rel.path))
		if err != nil {
			return fmt.Errorf("read %s: %w", rel.path, err)
		}
		allArtifacts = append(allArtifacts, model.Artifact{Kind: rel.kind, Path: rel.path, Hash: model.ContentHash(data)})
	}
	observations := make([]model.Observation, 0, len(result.ObservationIDs))
	for _, id := range result.ObservationIDs {
		var observation model.Observation
		rel := filepath.Join("observations", strings.TrimPrefix(id, "sha256:")+".json")
		if err := params.Store.ReadJSON(rel, &observation); err != nil {
			continue
		}
		observations = append(observations, observation)
	}
	evidenceData, err := json.MarshalIndent(traceEvidenceDocument{
		SchemaVersion:   model.SchemaVersion,
		TransactionHash: result.TransactionHash,
		Block:           result.Block,
		From:            result.From,
		To:              result.To,
		Status:          result.Status,
		GasUsed:         result.GasUsed,
		TraceProvider:   result.TraceProvider,
		TraceMethod:     result.TraceMethod,
		Calls:           result.Calls,
		Observations:    observations,
		Relations:       relations,
		Artifacts:       allArtifacts,
		OpenQuestions:   result.OpenQuestions,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode trace evidence: %w", err)
	}
	if _, err := params.Store.WriteFile(result.EvidencePath, append(evidenceData, '\n')); err != nil {
		return err
	}
	return nil
}

func renderSequence(result *Result) string {
	var b strings.Builder
	b.WriteString("%% Call order, not wall-clock timing.\n")
	b.WriteString("sequenceDiagram\n")
	participants := make(map[string]string)
	order := make([]string, 0)
	participant := func(address string) string {
		if address == "" {
			return ""
		}
		if id, ok := participants[address]; ok {
			return id
		}
		id := fmt.Sprintf("p%d", len(participants))
		participants[address] = id
		order = append(order, address)
		return id
	}
	for i, call := range result.Calls {
		if i >= maxSequenceCalls {
			break
		}
		participant(call.From)
		participant(call.To)
	}
	for _, address := range order {
		fmt.Fprintf(&b, "  participant %s as %s\n", participants[address], mermaidText(shortAddress(address)))
	}
	if len(order) == 0 {
		b.WriteString("  participant none as no calls\n")
	}
	for i, call := range result.Calls {
		if i >= maxSequenceCalls {
			b.WriteString("  Note over " + firstParticipant(participants) + ": sequence truncated; see trace-calls.json\n")
			break
		}
		from := participants[call.From]
		to := participants[call.To]
		label := strings.ToUpper(call.Type)
		if call.Selector != "" {
			label += " " + call.Selector
		}
		if call.Error != "" {
			label += " [error]"
		}
		if from == "" || to == "" {
			if from != "" {
				fmt.Fprintf(&b, "  Note over %s: %s %s\n", from, mermaidText(label), mermaidText(call.Path))
			}
			continue
		}
		fmt.Fprintf(&b, "  %s->>%s: %s\n", from, to, mermaidText(label))
	}
	return b.String()
}

func firstParticipant(participants map[string]string) string {
	keys := make([]string, 0, len(participants))
	for key := range participants {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return "none"
	}
	return participants[keys[0]]
}

func renderTraceReport(params Params, result Result, relations []model.Relation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Transaction Report — `%s`\n\n", result.TransactionHash)
	fmt.Fprintf(&b, "Generated: %s\n\n", result.CreatedAt.Format(time.RFC3339))
	b.WriteString("This report renders one provider's transaction trace. The trace is untrusted external data: call frames can be incomplete, provider-specific, or truncated. ")
	b.WriteString("An `observed_call` relation means the call appears in this trace; it does not prove that every execution path, branch, or internal call was exercised, and it is not a security audit.\n\n")

	b.WriteString("## Transaction\n\n")
	b.WriteString("| Field | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| Transaction | `%s` |\n", result.TransactionHash)
	fmt.Fprintf(&b, "| Chain ID | %d |\n", result.ChainID)
	fmt.Fprintf(&b, "| Block | %d |\n", result.Block.Number)
	fmt.Fprintf(&b, "| Block hash | `%s` |\n", result.Block.Hash)
	fmt.Fprintf(&b, "| From | `%s` |\n", result.From)
	fmt.Fprintf(&b, "| To | `%s` |\n", orUnknown(result.To))
	fmt.Fprintf(&b, "| Value (wei) | %s |\n", orUnknown(result.Value))
	fmt.Fprintf(&b, "| Receipt status | %s |\n", receiptStatus(result.Status))
	fmt.Fprintf(&b, "| Gas used | %d |\n", result.GasUsed)
	fmt.Fprintf(&b, "| Trace provider | `%s` / `%s` |\n", result.TraceProvider, result.TraceMethod)
	fmt.Fprintf(&b, "| Call frames | %d |\n\n", result.CallCount)

	b.WriteString("## Calls\n\n")
	if len(result.Calls) == 0 {
		b.WriteString("The provider returned no call frames. The raw response is preserved as evidence.\n\n")
	} else {
		b.WriteString("| Path | Depth | Type | From | To | Selector | Gas used | Error |\n")
		b.WriteString("|---|---|---|---|---|---|---|---|\n")
		rendered := len(result.Calls)
		if rendered > maxReportCalls {
			rendered = maxReportCalls
		}
		for _, call := range result.Calls[:rendered] {
			fmt.Fprintf(&b, "| `%s` | %d | %s | `%s` | `%s` | %s | %s | %s |\n",
				call.Path, call.Depth, orUnknown(call.Type), orUnknown(call.From), orUnknown(call.To),
				orUnknown(call.Selector), orUnknown(call.GasUsed), oneLine(call.Error))
		}
		if len(result.Calls) > rendered {
			fmt.Fprintf(&b, "\nOnly the first %d calls are rendered; all %d calls are in `trace-calls.json`.\n", rendered, len(result.Calls))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Sequence\n\n```mermaid\n")
	b.WriteString(renderSequence(&result))
	b.WriteString("```\n\n")

	b.WriteString("## Observed call relations\n\n")
	if len(relations) == 0 {
		b.WriteString("No observed_call relations were written.\n\n")
	} else {
		b.WriteString("| From | To | Call path | Type | Selector | Evidence |\n")
		b.WriteString("|---|---|---|---|---|---|\n")
		for _, relation := range relations {
			callPath, _ := relation.Attributes["call_path"].(string)
			callType, _ := relation.Attributes["call_type"].(string)
			selector, _ := relation.Attributes["selector"].(string)
			traceReference, _ := relation.Attributes["trace_reference"].(string)
			fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | %s | %s | `%s` |\n",
				relation.From, relation.To, callPath, orUnknown(callType), orUnknown(selector), traceReference)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Warnings and open questions\n\n")
	if len(result.OpenQuestions) == 0 {
		b.WriteString("None recorded.\n\n")
	} else {
		for _, question := range result.OpenQuestions {
			fmt.Fprintf(&b, "- %s\n", oneLine(question))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Evidence index\n\n")
	fmt.Fprintf(&b, "- Observations: %d\n", len(result.ObservationIDs))
	fmt.Fprintf(&b, "- Observed call relations: %d\n", len(relations))
	fmt.Fprintf(&b, "- Raw trace: `%s`\n", result.RawTraceArtifact)
	fmt.Fprintf(&b, "- Normalized calls: `%s`\n", result.TraceArtifact)
	b.WriteString("- `sequence.mmd`, `relations.json`, `evidence.json`\n\n")
	b.WriteString("## Scope\n\n")
	b.WriteString("The trace records execution at the receipt block only. It does not read historical state for other blocks, and does not simulate alternate inputs. ")
	fmt.Fprintf(&b, "The observed-call graph uses `ContractID` for addresses with runtime code at block %d and `AccountID` otherwise; endpoints whose code check failed or was skipped are marked `unknown` and `checked: false` rather than asserted to be accounts.\n", result.Block.Number)
	return b.String()
}

func receiptStatus(status uint64) string {
	if status == 1 {
		return "success"
	}
	if status == 0 {
		return "reverted"
	}
	return fmt.Sprintf("unknown (%d)", status)
}

func orUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func oneLine(value string) string {
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	if len(value) > 300 {
		return value[:300] + "…"
	}
	return value
}

func mermaidText(value string) string {
	replacer := strings.NewReplacer(
		"\n", " ", "\r", " ",
		";", ",",
		"\"", "'",
	)
	return replacer.Replace(value)
}

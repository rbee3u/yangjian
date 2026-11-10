package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/store"
)

type relationsDocument struct {
	SchemaVersion string           `json:"schema_version"`
	ProtocolID    string           `json:"protocol_id"`
	DeploymentID  string           `json:"deployment_id"`
	Block         model.BlockRef   `json:"block"`
	Relations     []model.Relation `json:"relations"`
}

type evidenceDocument struct {
	SchemaVersion string              `json:"schema_version"`
	ProtocolID    string              `json:"protocol_id"`
	DeploymentID  string              `json:"deployment_id"`
	Block         model.BlockRef      `json:"block"`
	Contracts     []ContractSummary   `json:"contracts"`
	Observations  []model.Observation `json:"observations"`
	Relations     []model.Relation    `json:"relations"`
	Artifacts     []model.Artifact    `json:"artifacts"`
	OpenQuestions []string            `json:"open_questions"`
}

func writeProtocolReport(params Params, result Result, contractRelations []model.Relation, observations []model.Observation) error {
	allRelations := make([]model.Relation, 0, len(result.RelationIDs)+len(contractRelations))
	for _, id := range result.RelationIDs {
		var relation model.Relation
		if err := params.Store.ReadJSON(relationPath(id), &relation); err != nil {
			return fmt.Errorf("read protocol relation %s: %w", id, err)
		}
		allRelations = append(allRelations, relation)
	}
	allRelations = append(allRelations, contractRelations...)
	sort.Slice(allRelations, func(i, j int) bool { return allRelations[i].ID < allRelations[j].ID })

	relationsData, err := json.MarshalIndent(relationsDocument{
		SchemaVersion: model.SchemaVersion,
		ProtocolID:    result.ProtocolID,
		DeploymentID:  result.DeploymentID,
		Block:         result.Block,
		Relations:     allRelations,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode protocol relations: %w", err)
	}
	if _, err := params.Store.WriteFile("relations.json", append(relationsData, '\n')); err != nil {
		return err
	}
	if _, err := params.Store.WriteFile("topology.mmd", []byte(topologyDiagram(result, allRelations))); err != nil {
		return err
	}
	if _, err := params.Store.WriteFile("architecture.mmd", []byte(architectureDiagram(result))); err != nil {
		return err
	}
	if _, err := params.Store.WriteFile("report.md", []byte(renderProtocolReport(params, result, allRelations))); err != nil {
		return err
	}

	artifactPaths := []struct {
		kind string
		path string
	}{
		{"protocol", filepath.Join("entities", "protocols", strings.TrimPrefix(result.ProtocolID, "protocol:")+".json")},
		{"deployment", filepath.Join("entities", "deployments", deploymentHashName(result.DeploymentID)+".json")},
		{"discovery", "artifacts/discovery.json"},
		{"report", "report.md"},
		{"topology", "topology.mmd"},
		{"architecture", "architecture.mmd"},
		{"relations", "relations.json"},
	}
	artifacts := make([]model.Artifact, 0, len(artifactPaths))
	for _, entry := range artifactPaths {
		hash := hashFile(params.Store, entry.path)
		if hash == "" {
			continue
		}
		artifacts = append(artifacts, model.Artifact{Kind: entry.kind, Path: entry.path, Hash: hash})
	}
	evidenceData, err := json.MarshalIndent(evidenceDocument{
		SchemaVersion: model.SchemaVersion,
		ProtocolID:    result.ProtocolID,
		DeploymentID:  result.DeploymentID,
		Block:         result.Block,
		Contracts:     result.Contracts,
		Observations:  observations,
		Relations:     allRelations,
		Artifacts:     artifacts,
		OpenQuestions: result.OpenQuestions,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode protocol evidence: %w", err)
	}
	if _, err := params.Store.WriteFile("evidence.json", append(evidenceData, '\n')); err != nil {
		return err
	}
	return nil
}

func topologyDiagram(result Result, relations []model.Relation) string {
	labels := make(map[string]string)
	ids := make(map[string]string)
	next := 0
	addNode := func(entityID, label string) string {
		key := nodeKey(entityID)
		if id, ok := ids[key]; ok {
			return id
		}
		id := fmt.Sprintf("n%d", next)
		next++
		ids[key] = id
		labels[id] = label
		return id
	}
	for _, summary := range result.Contracts {
		addNode(model.ContractID(result.ChainID, summary.Address), fmt.Sprintf("%s %s", roleLabel(summary.Role), shortAddress(summary.Address)))
	}
	edges := make([]string, 0)
	seen := make(map[string]bool)
	for _, relation := range relations {
		if relation.Type != "onchain_reference" {
			continue
		}
		label, _ := relation.Attributes["method"].(string)
		fromLabel := shortEntity(relation.From)
		toLabel := shortEntity(relation.To)
		switch label {
		case "token0", "token1":
			toLabel = label + " " + toLabel
		case "WETH9":
			toLabel = "WETH9 " + toLabel
		}
		fromNode := addNode(relation.From, fromLabel)
		toNode := addNode(relation.To, toLabel)
		key := fromNode + "->" + toNode + ":" + label
		if seen[key] {
			continue
		}
		seen[key] = true
		if label == "" {
			edges = append(edges, fmt.Sprintf("  %s --> %s", fromNode, toNode))
		} else {
			edges = append(edges, fmt.Sprintf("  %s -->|%s| %s", fromNode, mermaidLabel(label), toNode))
		}
	}
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for id := 0; id < next; id++ {
		nodeID := fmt.Sprintf("n%d", id)
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", nodeID, mermaidLabel(labels[nodeID]))
	}
	for _, edge := range edges {
		b.WriteString(edge)
		b.WriteString("\n")
	}
	if len(edges) == 0 {
		b.WriteString("  %% no onchain_reference relations were observed at the pinned block\n")
	}
	return b.String()
}

func architectureDiagram(result Result) string {
	var b strings.Builder
	b.WriteString("flowchart TB\n")
	fmt.Fprintf(&b, "  p[\"%s\"]\n", mermaidLabel(result.ProtocolID))
	fmt.Fprintf(&b, "  d[\"deployment %d@%d\"]\n", result.ChainID, result.Block.Number)
	b.WriteString("  p --> d\n")
	for i, summary := range result.Contracts {
		contractNode := fmt.Sprintf("c%d", i)
		label := fmt.Sprintf("%s %s", roleLabel(summary.Role), shortAddress(summary.Address))
		if summary.ProxyType != "" && summary.ProxyType != "none" {
			label += " (" + summary.ProxyType + ")"
		}
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", contractNode, mermaidLabel(label))
		fmt.Fprintf(&b, "  d --> %s\n", contractNode)
		if summary.SourceRevisionID != "" {
			sourceNode := fmt.Sprintf("s%d", i)
			fmt.Fprintf(&b, "  %s[\"source %s\"]\n", sourceNode, mermaidLabel(shortHash(summary.SourceRevisionID)))
			fmt.Fprintf(&b, "  %s -->|source| %s\n", contractNode, sourceNode)
			if summary.ProvenanceLevel != "" {
				fmt.Fprintf(&b, "  %s -->|%s %s| %s\n", sourceNode, summary.ProvenanceLevel, summary.Correspondence, contractNode)
			}
		}
	}
	return b.String()
}

func renderProtocolReport(params Params, result Result, relations []model.Relation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Protocol Dossier — %s\n\n", result.Name)
	fmt.Fprintf(&b, "Generated: %s\n\n", result.CreatedAt.Format(time.RFC3339))
	b.WriteString("This report is derived from pinned-block chain observations and registry source claims. ")
	b.WriteString("It is not a security audit, and verified source is not proof of deployment correspondence unless the provenance section says P2. ")
	b.WriteString("Static dependencies are not execution calls; no `observed_call` relation appears here because no transaction trace was read.\n\n")

	b.WriteString("## Deployment\n\n")
	b.WriteString("| Field | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| Protocol | `%s` |\n", result.ProtocolID)
	fmt.Fprintf(&b, "| Deployment | `%s` |\n", result.DeploymentID)
	fmt.Fprintf(&b, "| Chain ID | %d |\n", result.ChainID)
	fmt.Fprintf(&b, "| Block | %d |\n", result.Block.Number)
	fmt.Fprintf(&b, "| Block hash | `%s` |\n", result.Block.Hash)
	fmt.Fprintf(&b, "| Factory | `%s` |\n", result.Factory)
	fmt.Fprintf(&b, "| Pool | `%s` |\n", result.Pool)
	fmt.Fprintf(&b, "| Pool tokens | `%s`, `%s` |\n", result.Token0, result.Token1)
	fmt.Fprintf(&b, "| Fee tier | %d |\n", result.Fee)
	fmt.Fprintf(&b, "| Target registry | `%s` |\n\n", params.RegistryRef)

	b.WriteString("## Contract dossiers\n\n")
	b.WriteString("| Role | Address | Contract version | Source revision | Provenance | Proxy | Dossier |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, summary := range result.Contracts {
		fmt.Fprintf(&b, "| %s | `%s` | `%s` | `%s` | %s %s | %s | `%s` |\n",
			roleLabel(summary.Role), summary.Address, orUnknown(summary.ContractVersionID),
			orUnknown(summary.SourceRevisionID), orUnknown(summary.ProvenanceLevel),
			orUnknown(summary.Correspondence), orUnknown(summary.ProxyType), summary.DossierPath)
	}
	if len(result.Contracts) == 0 {
		b.WriteString("| none | | | | | | |\n")
	}
	b.WriteString("\nContract dossiers live at `research/<chain-id>/contracts/<address>/` and own their artifacts, observations, and contract-level relations. ")
	b.WriteString("This protocol dossier references them instead of copying their evidence.\n\n")

	b.WriteString("## Topology\n\n```mermaid\n")
	b.WriteString(topologyDiagram(result, relations))
	b.WriteString("```\n\n")

	b.WriteString("## Relations\n\n")
	sourceDependencies := 0
	onchainRelations := make([]model.Relation, 0)
	for _, relation := range relations {
		if relation.Type == "source_dependency" {
			sourceDependencies++
			continue
		}
		onchainRelations = append(onchainRelations, relation)
	}
	if len(onchainRelations) == 0 {
		b.WriteString("No on-chain relations were recorded.\n\n")
	} else {
		b.WriteString("| Type | From | To | Method | Block evidence | Observation |\n")
		b.WriteString("|---|---|---|---|---|---|\n")
		for _, relation := range onchainRelations {
			method, _ := relation.Attributes["method"].(string)
			blockHash, _ := relation.Attributes["block_hash"].(string)
			observation, _ := relation.Attributes["observation_id"].(string)
			fmt.Fprintf(&b, "| %s | `%s` | `%s` | %s | `%s` | `%s` |\n",
				relation.Type, relation.From, relation.To, orUnknown(method), orUnknown(blockHash), orUnknown(observation))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Source dependency relations live in the contract dossiers: %d aggregated in `relations.json`.\n\n", sourceDependencies)

	b.WriteString("## Open questions\n\n")
	if len(result.OpenQuestions) == 0 {
		b.WriteString("None recorded.\n\n")
	} else {
		for _, question := range result.OpenQuestions {
			fmt.Fprintf(&b, "- %s\n", question)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Evidence index\n\n")
	fmt.Fprintf(&b, "- Protocol observations: %d\n", len(result.ObservationIDs))
	fmt.Fprintf(&b, "- Aggregated relations: %d\n", len(relations))
	fmt.Fprintf(&b, "- Contract dossiers: %d\n", len(result.Contracts))
	b.WriteString("- `topology.mmd`: on-chain reference topology\n")
	b.WriteString("- `architecture.mmd`: protocol, deployment, contracts, and source binding\n")
	b.WriteString("- `relations.json`: every relation with evidence references\n")
	b.WriteString("- `evidence.json`: observations, contracts, relations, and open questions\n\n")
	b.WriteString("## Disclaimer\n\n")
	b.WriteString("Source verification is not an audit. Immutable and constructor-argument masking is evidence of a reproducible build, not proof that the deployed contract is safe. ")
	b.WriteString("Addresses are taken from the target registry or discovered from observed factory state; none are guessed.\n")
	return b.String()
}

func roleLabel(role string) string {
	switch role {
	case "factory":
		return "Factory"
	case "pool":
		return "Pool"
	case "nonfungible_position_manager":
		return "NonfungiblePositionManager"
	case "router":
		return "SwapRouter02"
	case "wrapped_native_token":
		return "WETH9"
	default:
		return role
	}
}

func shortAddress(address string) string {
	if len(address) <= 12 {
		return address
	}
	return address[:6] + "…" + address[len(address)-4:]
}

func shortEntity(id string) string {
	if address := entityAddress(id); address != "" {
		return shortAddress(address)
	}
	return shortHash(id)
}

func nodeKey(id string) string {
	if address := entityAddress(id); address != "" {
		return strings.ToLower(address)
	}
	return id
}

func entityAddress(id string) string {
	if at := strings.Index(id, "@"); at >= 0 {
		id = id[:at]
	}
	if colon := strings.Index(id, ":"); colon >= 0 {
		id = id[colon+1:]
	}
	if strings.HasPrefix(id, "0x") && len(id) == 42 {
		return id
	}
	return ""
}

func shortHash(value string) string {
	if len(value) <= 18 {
		return value
	}
	return value[:10] + "…" + value[len(value)-6:]
}

func mermaidLabel(value string) string {
	replacer := strings.NewReplacer("\"", "'", "\n", " ", "[", "(", "]", ")", "{", "(", "}", ")")
	return replacer.Replace(value)
}

func orUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func hashFile(st *store.Store, rel string) string {
	data, err := os.ReadFile(st.Path(rel))
	if err != nil {
		return ""
	}
	return model.ContentHash(data)
}

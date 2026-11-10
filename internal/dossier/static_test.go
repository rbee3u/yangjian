package dossier

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/rbee3u/yangjian/internal/model"
	"github.com/rbee3u/yangjian/internal/solc"
	"github.com/rbee3u/yangjian/internal/store"
)

const staticOutputTemplate = `{
  "contracts": {
    "contracts/Pool.sol": {
      "Pool": {
        "storageLayout": {
          "storage": [
            {"astId": 10, "contract": "Pool", "label": "slot0", "offset": 0, "slot": "0", "type": "t_struct(Slot0)"},
            {"astId": 11, "contract": "Pool", "label": "factory", "offset": 0, "slot": "4", "type": "t_address"}
          ],
          "types": {
            "t_struct(Slot0)": {
              "encoding": "inplace",
              "label": "struct Pool.Slot0",
              "numberOfBytes": "32",
              "members": [
                {"label": "sqrtPriceX96", "slot": "0", "offset": 0, "type": "t_uint160"},
                {"label": "tick", "slot": "0", "offset": 20, "type": "t_int24"}
              ]
            },
            "t_uint160": {"encoding": "inplace", "label": "uint160", "numberOfBytes": "20"},
            "t_int24": {"encoding": "inplace", "label": "int24", "numberOfBytes": "3"},
            "t_address": {"encoding": "inplace", "label": "address", "numberOfBytes": "20"},
            "t_unused": {"encoding": "inplace", "label": "unused", "numberOfBytes": "32"}
          }
        },
        "evm": {
          "methodIdentifiers": {
            "slot0()": "3850c7bd",
            "swap(address,bool,int256,uint160,bytes)": "128acb08"
          },
          "deployedBytecode": {
            "object": "STATIC_CODE",
            "immutableReferences": {"12": [{"start": 100, "length": 32}]}
          }
        }
      }
    }
  },
  "sources": {
    "contracts/Pool.sol": {
      "ast": {
        "nodeType": "SourceUnit",
        "id": 0,
        "nodes": [
          {
            "nodeType": "ImportDirective",
            "file": "contracts/Base.sol",
            "src": "0:10:0",
            "symbolAliases": [{"foreign": {"name": "PoolBase"}}]
          },
          {
            "nodeType": "ContractDefinition",
            "id": 1,
            "name": "PoolBase",
            "contractKind": "contract",
            "abstract": false,
            "baseContracts": [],
            "nodes": [
              {
                "nodeType": "VariableDeclaration",
                "id": 12,
                "name": "factory",
                "stateVariable": true,
                "mutability": "immutable",
                "typeDescriptions": {"typeString": "address"},
                "src": "10:7:0"
              },
              {
                "nodeType": "FunctionDefinition",
                "id": 20,
                "name": "_getFactory",
                "kind": "function",
                "visibility": "internal",
                "stateMutability": "view",
                "parameters": {"parameters": []},
                "modifiers": [],
                "src": "30:10:0",
                "body": {"nodeType": "Block", "statements": []}
              }
            ]
          },
          {
            "nodeType": "ContractDefinition",
            "id": 2,
            "name": "Pool",
            "contractKind": "contract",
            "abstract": false,
            "baseContracts": [
              {"baseName": {"name": "PoolBase", "referencedDeclaration": 1}}
            ],
            "nodes": [
              {
                "nodeType": "FunctionDefinition",
                "id": 30,
                "name": "slot0",
                "kind": "function",
                "visibility": "public",
                "stateMutability": "view",
                "parameters": {"parameters": []},
                "modifiers": [],
                "src": "40:10:0",
                "body": {"nodeType": "Block", "statements": []}
              },
              {
                "nodeType": "FunctionDefinition",
                "id": 31,
                "name": "swap",
                "kind": "function",
                "visibility": "external",
                "stateMutability": "nonpayable",
                "parameters": {
                  "parameters": [
                    {
                      "nodeType": "VariableDeclaration",
                      "id": 40,
                      "name": "delta",
                      "typeDescriptions": {"typeString": "function (int256, int256, bytes calldata) external"}
                    }
                  ]
                },
                "modifiers": [
                  {"modifierName": {"name": "nonReentrant", "referencedDeclaration": 50}}
                ],
                "src": "50:10:0",
                "body": {
                  "nodeType": "Block",
                  "statements": [
                    {
                      "nodeType": "ExpressionStatement",
                      "expression": {
                        "nodeType": "FunctionCall",
                        "kind": "functionCall",
                        "src": "60:10:0",
                        "expression": {
                          "nodeType": "Identifier",
                          "name": "_beforeSwap",
                          "referencedDeclaration": 32,
                          "typeDescriptions": {"typeString": "function ()"}
                        }
                      }
                    },
                    {
                      "nodeType": "ExpressionStatement",
                      "expression": {
                        "nodeType": "FunctionCall",
                        "kind": "functionCall",
                        "src": "70:10:0",
                        "expression": {
                          "nodeType": "MemberAccess",
                          "memberName": "transfer",
                          "src": "70:10:0",
                          "expression": {
                            "nodeType": "Identifier",
                            "name": "recipient",
                            "typeDescriptions": {"typeString": "address"}
                          },
                          "typeDescriptions": {"typeString": "function (uint256)"}
                        }
                      }
                    },
                    {
                      "nodeType": "ExpressionStatement",
                      "expression": {
                        "nodeType": "FunctionCall",
                        "kind": "functionCall",
                        "src": "80:10:0",
                        "expression": {
                          "nodeType": "MemberAccess",
                          "memberName": "delegatecall",
                          "src": "80:10:0",
                          "expression": {
                            "nodeType": "Identifier",
                            "name": "implementation",
                            "typeDescriptions": {"typeString": "address"}
                          },
                          "typeDescriptions": {"typeString": "function (bytes memory) returns (bool)"}
                        }
                      }
                    },
                    {
                      "nodeType": "ExpressionStatement",
                      "expression": {
                        "nodeType": "FunctionCall",
                        "kind": "functionCall",
                        "src": "90:10:0",
                        "expression": {
                          "nodeType": "MemberAccess",
                          "memberName": "transfer",
                          "src": "90:10:0",
                          "expression": {
                            "nodeType": "FunctionCall",
                            "kind": "typeConversion",
                            "src": "89:5:0",
                            "expression": {"nodeType": "Identifier", "name": "token"},
                            "typeDescriptions": {"typeString": "contract IERC20"}
                          },
                          "typeDescriptions": {"typeString": "function (address,uint256) returns (bool)"}
                        }
                      }
                    },
                    {
                      "nodeType": "ExpressionStatement",
                      "expression": {
                        "nodeType": "FunctionCall",
                        "kind": "functionCall",
                        "src": "100:10:0",
                        "expression": {
                          "nodeType": "Identifier",
                          "name": "delta",
                          "referencedDeclaration": 40,
                          "typeDescriptions": {"typeString": "function (int256, int256, bytes calldata) external"}
                        }
                      }
                    },
                    {
                      "nodeType": "ExpressionStatement",
                      "expression": {
                        "nodeType": "NewExpression",
                        "src": "110:10:0",
                        "typeName": {"name": "Pool"},
                        "typeDescriptions": {"typeString": "contract Pool"}
                      }
                    }
                  ]
                }
              },
              {
                "nodeType": "FunctionDefinition",
                "id": 32,
                "name": "_beforeSwap",
                "kind": "function",
                "visibility": "internal",
                "stateMutability": "nonpayable",
                "parameters": {"parameters": []},
                "modifiers": [],
                "src": "120:10:0",
                "body": {"nodeType": "Block", "statements": []}
              }
            ]
          }
        ]
      }
    }
  }
}`

func staticOutputJSON(t *testing.T, code []byte) []byte {
	t.Helper()
	raw := strings.ReplaceAll(staticOutputTemplate, "STATIC_CODE", hex.EncodeToString(code))
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("static output template: %v", err)
	}
	return []byte(raw)
}

func staticCompilerOutput(t *testing.T, code []byte) solc.Output {
	t.Helper()
	contract := solc.Contract{}
	contract.EVM.DeployedBytecode.Object = hex.EncodeToString(code)
	return solc.Output{
		Raw: staticOutputJSON(t, code),
		Contracts: map[string]map[string]solc.Contract{
			"contracts/Pool.sol": {"Pool": contract},
		},
	}
}

func staticAnalysisFixture(t *testing.T) (*store.Store, model.Manifest, *model.SourceRevision) {
	t.Helper()
	st := store.New(t.TempDir())
	if err := st.Init(); err != nil {
		t.Fatal(err)
	}
	metadata := []byte(`{"settings":{"compilationTarget":{"contracts/Pool.sol":"Pool"}}}`)
	if _, err := st.WriteFile("artifacts/metadata.json", metadata); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteFile("artifacts/provenance/solc-output.json", staticOutputJSON(t, []byte{0x60, 0x80})); err != nil {
		t.Fatal(err)
	}
	manifest := model.Manifest{
		SchemaVersion: model.SchemaVersion,
		Block:         model.BlockRef{ChainID: 1, Number: 123, Hash: "0xabc"},
	}
	revision := &model.SourceRevision{
		SchemaVersion: model.SchemaVersion,
		ID:            "sha256:test",
		MetadataRef:   "artifacts/metadata.json",
		Compilation:   model.Compilation{Compiler: "0.7.6+commit.7338295f"},
		Build: &model.BuildEvidence{
			Match:         true,
			SolcOutputRef: "artifacts/provenance/solc-output.json",
		},
	}
	return st, manifest, revision
}

func TestBuildStaticAnalysis(t *testing.T) {
	st, manifest, revision := staticAnalysisFixture(t)
	analysis, reasons, err := buildStaticAnalysis(st, manifest, revision)
	if err != nil {
		t.Fatalf("build static analysis: %v", err)
	}
	if len(reasons) != 0 || analysis == nil {
		t.Fatalf("reasons = %v, analysis = %+v", reasons, analysis)
	}
	if analysis.CompilationTarget != "contracts/Pool.sol:Pool" {
		t.Fatalf("target = %s", analysis.CompilationTarget)
	}
	if len(analysis.Inheritance) != 2 {
		t.Fatalf("inheritance = %+v", analysis.Inheritance)
	}
	if analysis.Inheritance[0].Contract != "Pool" || analysis.Inheritance[1].Contract != "PoolBase" {
		t.Fatalf("linearization = %+v", analysis.Linearization)
	}
	if len(analysis.Inheritance[0].Bases) != 1 || analysis.Inheritance[0].Bases[0] != "contracts/Pool.sol:PoolBase" {
		t.Fatalf("bases = %+v", analysis.Inheritance[0].Bases)
	}
	if len(analysis.Storage) != 2 || analysis.Storage[0].Slot != "0" || analysis.Storage[1].Label != "factory" {
		t.Fatalf("storage = %+v", analysis.Storage)
	}
	if _, ok := analysis.StorageTypes["t_struct(Slot0)"]; !ok {
		t.Fatalf("storage types = %+v", analysis.StorageTypes)
	}
	if _, ok := analysis.StorageTypes["t_unused"]; ok {
		t.Fatalf("unreferenced storage type was included: %+v", analysis.StorageTypes)
	}
	if len(analysis.Immutables) != 1 || analysis.Immutables[0].Name != "factory" || analysis.Immutables[0].ASTID != 12 {
		t.Fatalf("immutables = %+v", analysis.Immutables)
	}
	if len(analysis.Immutables[0].References) != 1 || analysis.Immutables[0].References[0].Start != 100 {
		t.Fatalf("immutable references = %+v", analysis.Immutables[0].References)
	}
	if len(analysis.Methods) != 2 || analysis.Methods[0].Selector == "" {
		t.Fatalf("methods = %+v", analysis.Methods)
	}
	if len(analysis.Functions) != 4 {
		t.Fatalf("functions = %+v", analysis.Functions)
	}
	if len(analysis.Imports) != 1 || analysis.Imports[0].Path != "contracts/Base.sol" {
		t.Fatalf("imports = %+v", analysis.Imports)
	}
	if analysis.Counts.Total != 6 {
		t.Fatalf("call sites = %+v", analysis.CallSites)
	}
	if analysis.Counts.Internal != 1 || analysis.Counts.External != 1 || analysis.Counts.Delegatecall != 1 ||
		analysis.Counts.Transfer != 1 || analysis.Counts.Callback != 1 || analysis.Counts.Create != 1 {
		t.Fatalf("counts = %+v", analysis.Counts)
	}
	foundModifier := false
	for _, function := range analysis.Functions {
		if function.Display == "Pool.swap(function (int256, int256, bytes calldata) external)" {
			for _, modifier := range function.Modifiers {
				if modifier == "nonReentrant" {
					foundModifier = true
				}
			}
		}
	}
	if !foundModifier {
		t.Fatalf("swap modifiers were not recorded: %+v", analysis.Functions)
	}
}

func TestBuildStaticAnalysisUnavailable(t *testing.T) {
	st, manifest, revision := staticAnalysisFixture(t)
	if _, reasons, _ := buildStaticAnalysis(st, manifest, nil); len(reasons) == 0 {
		t.Fatal("missing revision must be reported")
	}
	noBuild := *revision
	noBuild.Build = nil
	if _, reasons, _ := buildStaticAnalysis(st, manifest, &noBuild); len(reasons) == 0 {
		t.Fatal("missing build evidence must be reported")
	}
	mismatch := *revision
	copiedBuild := *revision.Build
	copiedBuild.Match = false
	mismatch.Build = &copiedBuild
	if _, reasons, _ := buildStaticAnalysis(st, manifest, &mismatch); len(reasons) == 0 {
		t.Fatal("unproven build must be reported")
	}
	noAST := *revision
	noAST.Build = &model.BuildEvidence{Match: true, SolcOutputRef: "artifacts/no-ast.json"}
	if _, err := st.WriteFile("artifacts/no-ast.json", []byte(`{"contracts":{"contracts/Pool.sol":{"Pool":{}}},"sources":{"contracts/Pool.sol":{"id":0}}}`)); err != nil {
		t.Fatal(err)
	}
	if _, reasons, _ := buildStaticAnalysis(st, manifest, &noAST); len(reasons) == 0 {
		t.Fatal("missing AST must be reported")
	}
}

func TestRunAnalyzeStatic(t *testing.T) {
	st, address := newResolvedDossier(t)
	code := []byte{0x60, 0x80}
	compiler := &fakeCompiler{binary: testBinary(), output: staticCompilerOutput(t, code)}
	if _, err := RunProvenance(context.Background(), ProvenanceParams{
		ChainID:  1,
		Address:  address,
		Store:    st,
		RunID:    "run-provenance",
		Compiler: compiler,
	}); err != nil {
		t.Fatalf("run provenance: %v", err)
	}
	chain := &fakeChain{header: testHeader(), code: code}
	result := runAnalyze(t, st, address, chain)
	if result.Static == nil {
		t.Fatalf("static result missing: %+v", result)
	}
	if result.Static.Contracts != 2 || result.Static.StorageEntries != 2 || result.Static.Methods != 2 {
		t.Fatalf("static summary = %+v", result.Static)
	}
	if result.Static.Delegatecalls != 1 || result.Static.CallSites != 6 {
		t.Fatalf("static summary = %+v", result.Static)
	}
	var manifest model.Manifest
	if err := st.ReadJSON("manifest.json", &manifest); err != nil {
		t.Fatal(err)
	}
	if contains(manifest.OpenQuestions, "static analysis not run") {
		t.Fatalf("open questions = %v", manifest.OpenQuestions)
	}
	for _, question := range manifest.OpenQuestions {
		if strings.HasPrefix(question, "static analysis unavailable") {
			t.Fatalf("open questions = %v", manifest.OpenQuestions)
		}
	}
	staticArtifacts := 0
	for _, artifact := range manifest.Artifacts {
		if artifact.Kind == "static_analysis" {
			staticArtifacts++
			if artifact.Path != staticArtifactRel {
				t.Fatalf("artifact = %+v", artifact)
			}
		}
	}
	if staticArtifacts != 1 {
		t.Fatalf("artifacts = %+v", manifest.Artifacts)
	}
	if len(manifest.ObservationIDs) != 10 {
		t.Fatalf("observations = %v", manifest.ObservationIDs)
	}
	if len(manifest.RelationIDs) != 2 {
		t.Fatalf("relations = %v", manifest.RelationIDs)
	}
	for _, relationID := range manifest.RelationIDs {
		var relation model.Relation
		if err := st.ReadJSON(relationPath(relationID), &relation); err != nil {
			t.Fatal(err)
		}
		if relation.Type != "source_dependency" {
			t.Fatalf("relation = %+v", relation)
		}
		if relation.Attributes["source_path"] == "" || relation.Attributes["symbol"] == "" {
			t.Fatalf("relation attributes = %+v", relation.Attributes)
		}
	}
	report, err := RunReport(ReportParams{
		ChainID: 1,
		Address: address,
		Store:   st,
		RunID:   "run-report",
	})
	if err != nil {
		t.Fatalf("run report: %v", err)
	}
	raw, err := os.ReadFile(st.Path(report.ReportPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "## Static structure") || !strings.Contains(string(raw), "PoolBase") ||
		!strings.Contains(string(raw), "delegatecall") {
		t.Fatalf("report did not render static structure:\n%s", raw)
	}
}

func TestClassifyLibraryCallVisibility(t *testing.T) {
	node := func() map[string]any {
		return map[string]any{
			"nodeType": "FunctionCall",
			"kind":     "functionCall",
			"src":      "1:2:0",
			"expression": map[string]any{
				"nodeType":              "MemberAccess",
				"memberName":            "foo",
				"referencedDeclaration": float64(99),
				"expression": map[string]any{
					"nodeType":         "Identifier",
					"name":             "Lib",
					"typeDescriptions": map[string]any{"typeString": "type(library Lib)"},
				},
				"typeDescriptions": map[string]any{"typeString": "function ()"},
			},
		}
	}
	for _, tc := range []struct {
		visibility string
		want       string
	}{
		{"internal", "library"},
		{"private", "library"},
		{"public", "library_delegatecall"},
		{"external", "library_delegatecall"},
	} {
		t.Run(tc.visibility, func(t *testing.T) {
			functions := map[int64]astFunction{
				99: {ContractKind: "library", Visibility: tc.visibility, Display: "Lib.foo()"},
			}
			site, ok := classifyCall(node(), "Pool.test", "contracts/Pool.sol", functions, map[int64]string{})
			if !ok || site.Kind != tc.want {
				t.Fatalf("site = %+v, ok = %t", site, ok)
			}
		})
	}
}

func TestAppendUniqueArtifactReplacesHash(t *testing.T) {
	values := []model.Artifact{{Kind: "static_analysis", Path: "artifacts/static.json", Hash: "sha256:old"}}
	values = appendUniqueArtifact(values, model.Artifact{Kind: "static_analysis", Path: "artifacts/static.json", Hash: "sha256:new"})
	if len(values) != 1 || values[0].Hash != "sha256:new" {
		t.Fatalf("artifacts = %+v", values)
	}
	values = appendUniqueArtifact(values, model.Artifact{Kind: "state", Path: "artifacts/state.json", Hash: "sha256:state"})
	if len(values) != 2 {
		t.Fatalf("artifacts = %+v", values)
	}
}

func TestStaticAnalysisRejectsOversizedOutput(t *testing.T) {
	st, manifest, revision := staticAnalysisFixture(t)
	big := make([]byte, maxStaticOutputBytes+1)
	if err := os.WriteFile(st.Path("artifacts/big.json"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	oversized := *revision
	oversized.Build = &model.BuildEvidence{Match: true, SolcOutputRef: "artifacts/big.json"}
	analysis, reasons, err := buildStaticAnalysis(st, manifest, &oversized)
	if err != nil {
		t.Fatalf("build static analysis: %v", err)
	}
	if analysis != nil || len(reasons) == 0 {
		t.Fatalf("analysis = %+v, reasons = %v", analysis, reasons)
	}
}

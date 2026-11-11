# Tooling Plan — Yangjian

Status: Draft for approval · 2026-11-10

## Runtime and core libraries

- Go 1.27.3 (darwin/arm64); go-ethereum v1.17.10 requires Go 1.25+.
- go-ethereum: `ethclient`, `rpc`, `accounts/abi`, `core/types`, `crypto`.
- Cobra for the CLI; `golang.org/x/crypto/sha3` for keccak256; `golang.org/x/time/rate` for provider rate limiting.
- Standard library: `encoding/json`, `log/slog`, `testing`.

## Capability matrix

| Capability | Options considered | Decision | Evidence / caveat |
|---|---|---|---|
| RPC reads | go-ethereum `ethclient`; raw JSON-RPC | `ethclient` + raw `rpc` fallback | `gethclient` is Geth-specific; `trace_*` has no wrapper |
| ABI | go-ethereum `accounts/abi` | `accounts/abi` | no `PackInto`; use `UnpackInto*` |
| Source verification | Sourcify v2; Etherscan v2 | Sourcify primary; Etherscan secondary | Sourcify returns metadata, standard JSON, source hashes, match modes; Etherscan needs a key and lacks those fields |
| solc compilation | Go binding; subprocess | pinned solc subprocess | no mature Go binding; verify solc-bin hashes |
| AST / storage layout / inheritance | solc standard JSON; Slither; Foundry; Surya; Go parsers | solc standard JSON | authoritative and version-pinned; AST shapes evolve |
| Call graph / access control | Slither/Surya DOT; custom AST | heuristic or deferred | no stable JSON |
| Proxy detection | Go implementation; Sourcify `proxyResolution`; Etherscan labels | Go EIP-1967/1822 slots, EIP-1167 bytecode, beacon `implementation()`; event/explorer cross-checks deferred | no established Go library; explorer labels can be false positives |
| Historical state | archive RPC; free key; local node | deferred; free key later | free tiers include archive state but not debug/trace |
| Traces | `debug_traceTransaction`; `trace_transaction`; Tenderly | `internal/trace` adapter with callTracer first and parity trace second | `debug_*` + callTracer most supported; `trace_*` is Erigon/Parity |
| Local simulation | anvil fork; geth dev; reth | optional later | anvil fork needs an archive upstream for old blocks; not a substitute for a real trace |
| Knowledge storage | JSON files; SQLite; graph/vector | file-first JSON + Markdown; SQLite later as index | reproducibility and diffability |
| Config | JSON; YAML/TOML; env only | JSON + env var references | zero extra parser dependency; schema-validatable |
| CLI | Cobra; stdlib flag; urfave/cli | Cobra | nested commands and completion |
| Logging | stdlib slog; frameworks | `log/slog` JSON to stderr | stdout reserved for the envelope |
| Testing | stdlib testing + fixtures; live-only; mocks | fixture-first; live gated | offline default |

## Provider plan

- **Sourcify v2**: primary provenance source; no key; cache and back off; record `match` versus `exact_match`.
- **Etherscan v2**: secondary cross-check; free key if configured; 3 req/s, 100k/day.
- **RPC**: provider profiles; public endpoints for current-state reads; free archive key later for historical state.
- **Traces**: `debug_traceTransaction` with callTracer, falling back to `trace_transaction` behind a capability probe. The raw provider response is preserved; unsupported methods are reported, never simulated.
- **solc-bin**: download the exact build, verify its hash, cache it.

## Explicit non-dependencies

- No Slither or Foundry in M1 (deferred; sandboxed if added).
- No MCP server in the MVP.
- No SQLite, graph, or vector database in the MVP.
- No CI in the MVP; local Makefile only.

## Risks

- Keyless public endpoints are not reliable for historical tracing; free archive access often needs a token.
- Sourcify match semantics differ from Etherscan's UI label.
- P2 may fail for contracts with missing dependencies or unavailable compiler builds.
- Provider rate limits and method availability vary; capability probes are required.

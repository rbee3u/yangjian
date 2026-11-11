# Architecture — Yangjian

Status: Draft for approval · 2026-11-10

## 1. System context

```
Claude Code (reasoning, Skills)
        │  CLI invocation, JSON envelope
        ▼
yangjian (deterministic Go CLI)
        │
        ├── RPC provider ──────┐
        ├── Sourcify v2 ───────┤
        ├── Etherscan v2 ──────┤  read-only adapters
        ├── solc-bin ──────────┘
        ▼
Local research store (JSON observations/entities, Markdown, Mermaid)
```

Claude Code composes research; Yangjian never calls an LLM. All external access is read-only and sits behind an adapter.

## 2. Modules

| Package | Responsibility |
|---|---|
| `cmd/yangjian` | Cobra commands, flags, envelope, exit codes |
| `internal/config` | JSON config, env overrides, provider profiles |
| `internal/chain` | RPC client, block references, rate limiting, retries |
| `internal/rpcprobe` | Local RPC endpoint registry, read-only capability probes, health checks |
| `internal/source` | Sourcify client, source artifacts, match modes |
| `internal/solc` | Pinned compiler manager, download/hash verification, standard JSON execution |
| `internal/model` | Observation, Contract, ContractVersion, SourceRevision, Relation, Manifest |
| `internal/target` | Target registry loading and validation |
| `internal/schema` | Runtime store-boundary validation matching the shipped JSON Schemas |
| `internal/store` | File-first store, content addressing, observation deduplication |
| `internal/dossier` | M1 staged pipeline: code, source, provenance, analyze, state, report |
| `internal/protocol` | M2 protocol dossier orchestration, topology discovery, budgets, relations |
| `internal/trace` | M4 trace adapter, normalization, observed_call relations, transaction report |
| `internal/fixtures` | Offline fixture replay and gated live acceptance tests |
| `internal/version` | Version, commit, build date |

## 3. Data flow

M1 Contract Dossier:

```
target registry / explicit input
  → runtime code + block pinning
  → Sourcify source artifacts + registry match
  → SourceRevision + compiler settings
  → provenance P2 rebuild (solc standard JSON)
  → proxy probe + static structure from the recorded solc output
  → pinned-block state reads (slot0, liquidity, ticks, pool immutables)
  → onchain_reference and source_dependency relations
  → report.md, topology.mmd, architecture.mmd, relations.json, evidence.json,
    manifest.json, observations
```

Current stage order: `code → source → provenance → analyze → state → report`.
`analyze` performs proxy detection and, when the P2 build matched, derives
inheritance/C3 linearization, storage layout, immutables, method identifiers, and
call sites from the recorded compiler output. It does not re-read the latest
source tree and does not treat static call sites as observed calls.

M2 Protocol Dossier:

```
target registry entry
  → pinned block
  → getPool(USDC, WETH, 3000) on the seeded factory + pool runtime code
  → contract dossiers for Factory, Pool, NPM, and SwapRouter02
  → protocol-owned evidence for getPool, factory(), WETH9()
  → aggregated contract relations (pool tokens/factory, source dependencies)
  → protocol/deployment entities
  → topology.mmd, architecture.mmd, protocol report, relations.json,
    evidence.json, manifest.json
```

The protocol orchestrator reuses the M1 contract pipeline per address and passes
the same pinned block to every contract dossier. It owns only relations that
cross contract-dossier boundaries; contract-level relations stay in the contract
dossiers and are aggregated in `relations.json` without copying artifacts. The
M2 run does not issue logs or trace calls, so no `observed_call` relation is
possible.

M4 Transaction Trace:

```
transaction hash
  → transaction + receipt by hash
  → debug_traceTransaction(callTracer) or trace_transaction
  → normalized call frames + endpoint code checks
  → observed_call relations + sequence diagram + transaction report
```

A trace endpoint is a `ContractID` only when `eth_getCode` returns code at the
receipt block; otherwise it is an `AccountID`. A provider without trace methods
produces a capability error rather than a simulated call graph.

## 4. Adapter boundaries

- `ChainClient`: typed block/code/storage/call reads plus a read-only raw JSON-RPC path for trace methods.
- `SourceProvider`: Sourcify primary, Etherscan secondary.
- `Compiler`: pinned solc binary and standard JSON.
- `Analyzer`: solc AST facts and proxy probes.
- `TraceProvider`: `debug_traceTransaction` callTracer first, `trace_transaction` second; capability failures are explicit.
- `Store`: observations, entities, manifests, cache.
- `Reporter`: Markdown and Mermaid.

## 5. Providers, concurrency, and cache

Each provider has a profile (endpoint, auth reference, rate limit, timeout, retry); trace capability is probed at runtime rather than assumed from the profile. The local RPC endpoint registry records which endpoints were observed to support blocks, logs, archive state, trace, and related methods, separately from their health; a capability is evidence from one probe, not a permanent property of the provider. Pinned-block responses are cached; `latest` header, code, storage, and call reads always hit the provider and are not long-term evidence. Stages run sequentially within a dossier; contract selection and recorded observation counts are bounded by the configured budgets, and every network call has a provider timeout and retry policy.

## 6. Security

Read-only; no private keys, signing, or broadcasting. Secrets come from environment variables. Solc runs as a pinned binary in a path-jailed temporary directory with resource limits; Foundry and Slither are not invoked. Untrusted source, metadata, ABI, trace, and provider output are size-limited and rendered as data, not executable content.

## 7. Output and errors

All commands emit a versioned JSON envelope on stdout; logs are `log/slog` JSON on stderr. Exit codes: 0 success, 1 operational failure, 2 usage, 3 partial success. Error codes are stable and carry `retryable`.

## 8. Distribution

Single binary built with `go build -trimpath -buildvcs`. Local Makefile only; `make install` runs `go install ./cmd/yangjian` and lands in `$GOPATH/bin` (or `$GOBIN`). No CI at this stage.

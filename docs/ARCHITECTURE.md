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
| `internal/source` | Sourcify client, source artifacts, match modes |
| `internal/solc` | Pinned compiler manager, download/hash verification, standard JSON execution |
| `internal/model` | Observation, Contract, ContractVersion, SourceRevision, Relation, Manifest |
| `internal/store` | File-first store, content addressing, observation deduplication |
| `internal/dossier` | M1 staged pipeline: code, source, provenance, analyze, state, report |
| `internal/protocol` | M2 layered topology discovery (planned) |
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
  → onchain_reference relations (source_dependency pending)
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
seeded Factory / entrypoint
  → getPool / factory reads / bounded events
  → contract dossiers for discovered contracts
  → relation graph
  → topology.mmd, protocol report, manifest
```

## 4. Adapter boundaries

- `ChainClient`: block, code, storage, call, logs, trace placeholder.
- `SourceProvider`: Sourcify primary, Etherscan secondary.
- `Compiler`: pinned solc binary and standard JSON.
- `Analyzer`: solc AST facts and proxy probes.
- `TraceProvider`: placeholder; no MVP implementation.
- `Store`: observations, entities, manifests, cache.
- `Reporter`: Markdown and Mermaid.

## 5. Providers, concurrency, and cache

Each provider has a profile (endpoint, auth reference, capabilities, rate limit, timeout, retry). Immutable responses are cached as observations; `latest` responses get a short TTL and are not long-term evidence. Work runs in a bounded worker pool with per-dossier request and time budgets.

## 6. Security

Read-only; no private keys, signing, or broadcasting. Secrets come from environment variables. Solc runs as a pinned binary in a path-jailed temporary directory with resource limits; Foundry and Slither are not invoked in M1. Untrusted source, metadata, ABI, and provider output are size-limited and rendered as data, not executable content.

## 7. Output and errors

All commands emit a versioned JSON envelope on stdout; logs are `log/slog` JSON on stderr. Exit codes: 0 success, 1 operational failure, 2 usage, 3 partial success. Error codes are stable and carry `retryable`.

## 8. Distribution

Single binary built with `go build -trimpath -buildvcs`. Local Makefile only; `make install` runs `go install ./cmd/yangjian` and lands in `$GOPATH/bin` (or `$GOBIN`). No CI at this stage.

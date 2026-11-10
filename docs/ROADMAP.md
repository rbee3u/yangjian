# Implementation Roadmap — Yangjian

Status: Draft for approval · 2026-11-10

Current status: M1 Contract Dossier and M2 Protocol Dossier are implemented. The M1 pipeline covers pool discovery, `code`, `source`, `provenance`, `analyze` (proxy detection plus static structure), `state` (pool immutables and three `onchain_reference` relations), and `report`. The latest M2 live run at block 26385739 discovered the Uniswap V3 USDC/WETH 0.3% pool from the factory, produced P2/proven for Factory, Pool, NPM, and SwapRouter02, recorded protocol relations (`getPool`, `factory`, `WETH9`), aggregated 7 `onchain_reference` relations and the contract `source_dependency` relations, and finished with manifest status `complete`. P1 metadata binding, beacon implementation resolution, Etherscan cross-check, RPC/provider caching, and `--refresh` are implemented. Remaining M1 work: JSON Schemas and fixtures/golden tests. Etherscan live acceptance is skipped until an API key is configured; the client and comparison logic are covered by offline tests.

## M0 — Design

Deliver PRD, architecture, ADRs, tooling plan, data model, workflow, CLI contract, testing and storage specs. Gate: user approval.

## M1 — Contract Dossier

Goal: given a chain and address from the target registry or explicit input, produce a reproducible dossier.

Steps:

1. Go module scaffold, Cobra command, JSON envelope, config, model, file store.
2. Chain client, provider profiles, block pinning, RPC response caching, rate limits, retries (implemented).
3. Sourcify client, source-provider response cache, and Etherscan cross-check (implemented; live Etherscan gated by an API key); source artifacts and match modes (implemented).
4. Pinned solc manager and P2 rebuild (implemented); AST inheritance/C3 linearization, storage layout, immutables, method identifiers, and call sites parsed from the recorded solc output (implemented). Resource limits currently cover timeout, input size, output size, and a static-output read cap; OS memory/CPU rlimits remain an open question.
5. Proxy detection (EIP-1967/1822/1167) with beacon `implementation()` resolution and ContractVersion update (implemented).
6. Provenance pipeline: P1 embedded-metadata hash and source/settings binding; P2 when `bytecodeHash: none`; P0 fallback with explicit unproven marker. P1 and P2 implemented.
7. Pinned-block reads: `slot0()`, `liquidity()`, `ticks(currentTick)` (implemented); pool immutables and `onchain_reference` relations (implemented).
8. Relations: `onchain_reference` (token0, token1, factory) and `source_dependency` (imports, inheritance) implemented.
9. Report, Mermaid diagrams, relations.json, manifest, evidence index (implemented).
10. Fixtures and tests; live acceptance gated by `YANGJIAN_LIVE=1`.

Definition of done:

1. Pool address discovered from the seeded Factory via `getPool(USDC, WETH, 3000)` at a pinned block, not hardcoded.
2. Runtime bytecode hash and proxy detection evidence.
3. Sourcify provenance with match mode, compiler `0.7.6+commit.7338295f`, optimizer 800; P2 attempted; P0/unproven if rebuild fails.
4. ABI, method identifiers, storage layout, inheritance/linearization, immutables, and call sites (implemented from the P2 solc output).
5. Pinned-block `slot0`, `liquidity`, `ticks(currentTick)`; historical reads marked deferred.
6. Relations as above; no `observed_call`.
7. `report.md`, `topology.mmd`/`architecture.mmd`, `relations.json`, `manifest.json`, evidence index.
8. Offline `go test ./...` passes; gated live acceptance passes when the fixture is recorded.
9. Open questions recorded (history, trace, Router source, failed P2).

## M2 — Protocol Dossier

Goal: seeded Factory/entrypoint → related contracts → topology report, without trace.

Steps:

1. Target registry entry with official Uniswap deployment URL and retrieval date (implemented).
2. Discover USDC/WETH 0.3% pool via `getPool`; bounded factory events optional (implemented without the optional event scan).
3. Contract dossiers for Factory, Pool, and NonfungiblePositionManager; Router if source is available (implemented; Router is selected from the registry).
4. Relations: Factory→Pool, Pool→tokens/factory, NPM→Factory, Router→Factory/WETH9 via pinned-block on-chain reads; source dependencies stay in contract dossiers and are aggregated; no `observed_call` (implemented).
5. Topology and architecture diagrams; protocol report; manifest; open questions (implemented).
6. Respect budgets: default 50 contracts and 1000 observations per dossier run, with depth 2 by construction (implemented).

Definition of done: the Uniswap V3 deployment topology is reproducible from evidence, with every relation typed and sourced; trace-dependent sequence diagrams are explicitly deferred. The live M2 acceptance run is recorded in `research/1/protocols/uniswap-v3/`.

## M3 — Historical state (deferred)

Enable `--block` historical reads when a free archive key or archive endpoint is configured. Acceptance items requiring old state move from deferred to active.

## M4 — Trace and observed_call (deferred)

Add a `TraceProvider` adapter (`debug_traceTransaction` + callTracer first, `trace_transaction` second). Enable `observed_call` relations, transaction reports, and sequence diagrams.

## M5 — Multi-chain (deferred)

Add Base as the second network to validate the chain-parameterized boundaries.

## M6 — Query/index (optional)

Add `yangjian query` or a SQLite derived index only when cross-dossier queries become a real bottleneck.

## Engineering conventions

- No CI for now; local Makefile is the gate.
- `make build`, `make install`, `make test`, `make test-live`, `make vet`, `make verify`, `make clean`.
- `make install` runs `go install ./cmd/yangjian`; the binary lands in `$GOPATH/bin` (or `$GOBIN`).
- `go build -trimpath -buildvcs`; `yangjian version` prints version, commit, date.
- Solc binaries are downloaded on demand and hash-verified; never vendored.

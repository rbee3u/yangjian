# Testing — Yangjian

Status: Draft for approval · 2026-11-10

## Principles

- `go test ./...` runs offline by default.
- Live provider calls are gated by `YANGJIAN_LIVE=1`.
- Fixtures pin block number, block hash, and raw provider responses.
- Golden JSON makes schema changes visible in review.
- No secrets in fixtures or golden files.

## Layers

1. **Unit tests** — IDs, envelope, error mapping, config precedence, cache keys, provenance diffing, proxy slot derivation, relation validation.
2. **Fixture tests** — recorded Sourcify/Etherscan/RPC responses replay through the pipeline; compare entities, manifest, and report structure against golden files.
3. **Live integration tests** — gated; hit providers at latest; assert invariants, not exact values.

## Fixtures

- Location: `testdata/fixtures/<provider>/<case>.json`.
- Record with `make fixtures`, which runs `yangjian fixtures record --chain 1 --target uniswap-v3`; the command discovers the pool from the factory at the pinned block, fetches its runtime code, and records the Sourcify response.
- Each fixture includes chain ID, address, block number/hash, runtime bytecode, the provider record, and `recorded_at`.
- Redact API keys and endpoint credentials.
- Large responses may be compressed, but must remain reproducible.

The current fixture is `testdata/fixtures/sourcify/uniswap-v3-pool.json`. `internal/fixtures/replay_test.go` replays it offline through the code and source stages, asserting zero source hash mismatches and exact ABI/source identities.

## Golden files

- Location: `testdata/golden/<case>.json`.
- Compare canonical JSON (stable key order, normalized timestamps).
- Update with `go test ./internal/fixtures -update`; review diffs before committing.
- Never hand-edit golden files to make tests pass.

The current golden is `testdata/golden/sourcify/uniswap-v3-pool.json`, recording the block, runtime hash, compiler, registry match, source count, ABI hash, and source revision ID.

## Error paths

Cover at least:

- RPC timeout, 429, 403, 5xx, malformed JSON.
- Missing source, Sourcify `match` versus `exact_match`, Etherscan disagreement.
- solc download failure, compiler diagnostics, P2 mismatch.
- Proxy unresolved, beacon call failure.
- Budget exceeded, context cancellation.
- Invalid config, missing target registry entry.
- Trace: method unsupported, invalid trace JSON, oversized trace, code check failure, transaction or receipt missing.

## Acceptance

M1 fixture: Uniswap V3 USDC/WETH 0.3% Pool.

- Discover the pool from `getPool(USDC, WETH, 3000)` at the pinned block.
- Assert runtime bytecode hash, proxy negative, Sourcify match mode, compiler `0.7.6+commit.7338295f`, optimizer 800.
- Assert ABI, storage layout, inheritance, `slot0`, `liquidity`, `ticks(currentTick)`.
- Assert `onchain_reference` relations to token0/token1/factory and `source_dependency` relations.
- Assert report, diagrams, `relations.json`, `manifest.json`, and evidence index.
- Assert no `observed_call` relation.

M2 protocol acceptance: run `yangjian protocol dossier` against the Uniswap V3 target and assert pool discovery from `getPool`, four contract dossiers, protocol `getPool`/`factory`/`WETH9` relations, aggregated pool relations, topology, and no `observed_call`.

M4 trace tests: fake RPC coverage for callTracer, fallback to `trace_transaction`, unsupported providers, missing receipts, truncated traces, invalid addresses, receipt status, endpoint classification, relation IDs, and artifact hashes. A live trace test requires a provider with `debug_*` or `trace_*` and is skipped on public endpoints without those methods.

Live acceptance: `YANGJIAN_LIVE=1 go test ./... -run Acceptance`; latest block; structural invariants; no historical-block claim.

## Local gates

- `make test` — offline unit + fixture tests.
- `make test-live` — gated live tests.
- `make vet` — `go vet ./...`.
- `make verify` — `govulncheck ./...` when installed.
- No CI at this stage.

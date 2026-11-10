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

- Location: `testdata/fixtures/<provider>/<case>/`.
- Record with `make fixtures` (recording command added in M1).
- Store raw responses, normalized request metadata, block number/hash, provider, and retrieval date.
- Redact API keys and endpoint credentials.
- Large responses may be compressed, but must remain reproducible.

## Golden files

- Location: `testdata/golden/<case>/`.
- Compare canonical JSON (stable key order, normalized timestamps).
- Update with `go test ./... -update`; review diffs before committing.
- Never hand-edit golden files to make tests pass.

## Error paths

Cover at least:

- RPC timeout, 429, 403, 5xx, malformed JSON.
- Missing source, Sourcify `match` versus `exact_match`, Etherscan disagreement.
- solc download failure, compiler diagnostics, P2 mismatch.
- Proxy unresolved, beacon call failure.
- Budget exceeded, context cancellation.
- Invalid config, missing target registry entry.

## Acceptance

M1 fixture: Uniswap V3 USDC/WETH 0.3% Pool.

- Discover the pool from `getPool(USDC, WETH, 3000)` at the pinned block.
- Assert runtime bytecode hash, proxy negative, Sourcify match mode, compiler `0.7.6+commit.7338295f`, optimizer 800.
- Assert ABI, storage layout, inheritance, `slot0`, `liquidity`, `ticks(currentTick)`.
- Assert `onchain_reference` relations to token0/token1/factory and `source_dependency` relations.
- Assert report, diagrams, `relations.json`, `manifest.json`, and evidence index.
- Assert no `observed_call` relation.

Live acceptance: `YANGJIAN_LIVE=1 go test ./... -run Acceptance`; latest block; structural invariants; no historical-block claim.

## Local gates

- `make test` — offline unit + fixture tests.
- `make test-live` — gated live tests.
- `make vet` — `go vet ./...`.
- `make verify` — `govulncheck ./...` when installed.
- No CI at this stage.

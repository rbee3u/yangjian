# CLI Contract — Yangjian

Status: Draft for approval · 2026-11-10

## Global behavior

- All commands emit a JSON envelope on stdout; `--text` opts into human-readable output.
- Logs go to stderr as `log/slog` JSON; `--log-level` controls verbosity.
- Exit codes: 0 success, 1 operational failure, 2 usage/config, 3 partial success.
- Read-only: the CLI never accepts private keys, never signs, and never broadcasts.
- Config precedence: CLI flag > environment > `.yangjian/config.json` (or `~/.config/yangjian/config.json`) > defaults.

## M1 commands

### `yangjian version`

Prints version, commit, build date, and Go version.

### `yangjian config init`

Writes a documented example config with provider profiles and `api_key_env` references.

### `yangjian contract dossier`

Runs the M1 pipeline or one stage.

| Flag | Description |
|---|---|
| `--chain <id>` | Chain ID; default from config |
| `--address <0x...>` | Target contract address |
| `--target <slug>` | Resolve the address from the target registry; mutually exclusive with `--address` |
| `--contract <name>` | Contract name in the registry when `--target` is used; default `factory` |
| `--block latest\|<n>` | Block to pin; default `latest` |
| `--out <dir>` | Dossier directory; default `research/<chain>/contracts/<address>` |
| `--stage <stage>` | Run one stage: `code`, `source`, `provenance`, `analyze`, `state`, `report` |
| `--refresh` | Ignore cached provider responses |
| `--json` / `--text` | Output mode; JSON is the default |
| `--log-level <level>` | `error`, `warn`, `info`, `debug` |

Examples:

```sh
yangjian contract dossier --chain 1 \
  --address 0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8 \
  --block latest --out research/1/contracts/0x8ad5... --json

yangjian contract dossier --chain 1 \
  --address 0x8ad5... --stage provenance --json
```

### `yangjian contract discover`

Resolves a factory-deployed contract for a token pair and fee tier at a pinned block.

| Flag | Description |
|---|---|
| `--chain <id>` | Chain ID; default from config |
| `--factory <0x...>` | Factory address; mutually exclusive with `--target` |
| `--target <slug>` | Resolve the factory and token symbols from the target registry |
| `--token0 <symbol\|0x...>` | First token; a symbol requires `--target` |
| `--token1 <symbol\|0x...>` | Second token; a symbol requires `--target` |
| `--fee <uint24>` | Fee tier in hundredths of a bip |
| `--block latest\|<n>` | Block to pin; default `latest` |
| `--out <dir>` | Output directory; default `research/<chain>/discoveries/<factory>/<token0>-<token1>-<fee>` |
| `--refresh` | Ignore cached RPC responses |
| `--json` / `--text` | Output mode; JSON is the default |

The command calls `getPool(address,address,uint24)` on the factory, fetches the returned address's runtime code at the same pinned block, and records three observations: block header, `eth_call`, and `eth_getCode`. It writes `discovery.json` and a manifest with `dossier_type: "discovery"`.

The result reports `factory`, `token0`, `token1`, `fee`, `block`, `pool`, `found`, `pool_code_hash`, `pool_code_size`, and the observation IDs. `meta.status` is `complete` when a pool with runtime code is found, or when the factory definitively returns the zero address; it is `partial` when the factory returns an address with no runtime code.

Example:

```sh
yangjian contract discover --chain 1 --target uniswap-v3 \
  --token0 USDC --token1 WETH --fee 3000 --block latest --json
```

### Provenance result

The `provenance` stage downloads and hash-verifies the pinned solc build, recompiles the Sourcify standard JSON input in an isolated temporary directory, masks immutables and link references, and compares the result with the pinned-block runtime bytecode. Its `data.provenance` object reports `match`, `provenance_level`, `correspondence`, `compiler_sha256`, `immutables_masked`, `link_references_masked`, `onchain_bytes`, `recompiled_bytes`, `compared_bytes`, and `mismatch_offset`.

Before compiling, the stage attempts P1 when `metadata.bytecodeHash` is not `none`: it extracts the metadata hash embedded in the runtime bytecode, compares it with the recorded metadata artifact, checks every metadata source hash against the recorded source files, and compares compiler/optimizer/EVM/bytecodeHash/remapping/library settings. The result is a `metadata_binding` observation. If P1 matches but the compiler cannot be obtained, the SourceRevision remains `P1`/`proven`.

A successful P2 comparison sets `provenance_level` to `P2` and `correspondence` to `proven`. A bytecode mismatch is a negative research result, not an operational failure: the command remains `ok: true` with `meta.status` `partial`, the SourceRevision stays `P0`/`unproven`, and the manifest records the mismatch. Compiler download or execution failures return `compiler_unavailable` or `compiler_error`.

### Analyze, state, and report results

- `analyze` probes EIP-1967 implementation/beacon/admin slots, the EIP-1822 PROXIABLE slot, and the EIP-1167 minimal-proxy bytecode pattern. A beacon proxy is resolved with `implementation()` at the same pinned block and the call is recorded. When P2 build evidence exists, `analyze` also reads the recorded solc output to extract inheritance linearization, storage layout, immutables, method identifiers, source imports, and call sites, and writes `source_dependency` relations. It updates `ContractVersion.proxy_type` and records `artifacts/proxy.json` and `artifacts/static.json`. When P2 build evidence is absent, proxy probing still runs and static structure is listed in `open_questions`.
- `source` records a Sourcify source fetch, then optionally queries Etherscan V2 when `ETHERSCAN_API_KEY` (or the configured `api_key_env`) is set. The secondary record is compared on ABI, compiler settings, and source content hashes; `match`, `mismatch`, `unavailable`, and `skipped` are recorded in `data.source.cross_check` and `artifacts/source-crosscheck.json`. A cross-check never replaces the primary match or P1/P2 provenance.
- `state` performs pinned-block `eth_call` reads for `slot0()`, `liquidity()`, `ticks(currentTick)`, and Uniswap V3 pool immutables. It records `artifacts/state.json` and writes three `onchain_reference` relations (token0, token1, factory).
- `report` writes `report.md`, `topology.mmd`, `architecture.mmd`, `relations.json`, and `evidence.json`. It renders only recorded facts and lists missing stages in `open_questions`.

`--refresh` bypasses the RPC response cache and the Sourcify/Etherscan HTTP cache. Cached entries are written under `.yangjian/cache/`; the cache key includes the provider profile, method, normalized parameters, and block number/tag. `latest` header, code, storage, and call reads are neither served from nor written to the cache.

## M2 commands

### `yangjian protocol dossier`

Builds a Protocol Dossier from a target registry entry. It pins one block, discovers the acceptance pool from the registry factory, runs Contract Dossiers for the factory, pool, NonfungiblePositionManager, and SwapRouter02 (or `swap_router` when configured), and aggregates their evidence.

| Flag | Description |
|---|---|
| `--chain <id>` | Chain ID; default from config |
| `--target <slug>` | Target registry entry; required |
| `--block latest\|<n>` | Block to pin for every discovery and contract read; default `latest` |
| `--out <dir>` | Protocol dossier directory; default `research/<chain>/protocols/<slug>` |
| `--contracts-root <dir>` | Contract dossier root; default `research/<chain>/contracts` |
| `--max-contracts <n>` | Maximum contract dossiers; `0` uses `budgets.max_contracts` (default `50`) |
| `--max-requests <n>` | Maximum observed contract-dossier requests before further dossiers are skipped; `0` uses `budgets.max_requests_per_dossier` (default `1000`) |
| `--refresh` | Ignore cached RPC and provider responses |
| `--json` / `--text` | Output mode; JSON is the default |

The command:

1. Loads the target registry entry and validates its chain.
2. Pins a block and records `eth_getBlockByNumber`.
3. Calls `UniswapV3Factory.getPool(token0, token1, fee)` at that block and fetches the returned pool's runtime code. A zero result or empty code is recorded as an open question; no address is invented.
4. Runs the M1 contract pipeline for each selected role at the same pinned block. Contract reports and manifests remain under `--contracts-root`.
5. Records protocol-owned `onchain_reference` relations for `getPool`, NPM `factory()`, Router `factory()`, and Router `WETH9()`. A mismatch between a returned address and the registry factory is recorded, not hidden. Pool `token0()`/`token1()`/`factory()` relations come from the pool's contract dossier.
6. Writes `protocol.json`, `report.md`, `topology.mmd`, `architecture.mmd`, `relations.json`, `evidence.json`, Protocol/Deployment entities, and a protocol manifest.

The manifest status is `complete` only when there are no open questions. A contract-level failure is recorded as an open question and the remaining contracts still run; no trace, log, or event is read, so the dossier never contains an `observed_call` relation.

Example:

```sh
yangjian protocol dossier --chain 1 --target uniswap-v3 --block latest --json
```

## M4 commands

### `yangjian trace transaction`

Reads one transaction trace from the configured RPC and writes a transaction dossier.

| Flag | Description |
|---|---|
| `--chain <id>` | Chain ID; default from config |
| `--tx <0x...>` | 32-byte transaction hash; required |
| `--out <dir>` | Dossier directory; default `research/<chain>/transactions/<txhash>` |
| `--refresh` | Ignore cached `eth_getCode` reads used to classify call endpoints |
| `--json` / `--text` | Output mode; JSON is the default |

The command first records `eth_getTransactionByHash` and `eth_getTransactionReceipt`, then tries `debug_traceTransaction` with `{"tracer":"callTracer"}` and falls back to `trace_transaction` only when the first method is unsupported. It preserves the raw provider response and writes normalized call frames with path, depth, call type, selector, input/output hashes, gas, value, and error fields. Every call frame with both endpoints becomes an `observed_call` relation whose `from`/`to` use `ContractID` when `eth_getCode` returns code at the receipt block and `AccountID` otherwise. Relations record `from_checked`/`to_checked`; a failed or budget-skipped check yields kind `unknown` and is listed in `open_questions` rather than being reported as a proven account. The command writes `transaction.json`, `report.md`, `sequence.mmd`, `relations.json`, `evidence.json`, and a manifest.

An execution revert is evidence (`status: 0` and call errors), not an operational failure. A provider without either trace method returns `rpc_method_unsupported`; an unreadable trace returns `trace_invalid`. Trace responses are capped at 32 MiB and 10,000 normalized call frames, with truncation recorded as an open question. No trace call is cached in the RPC response cache; the raw response is kept in the dossier.

Example:

```sh
yangjian trace transaction --chain 1 --tx 0x... --json
```

## Envelope

Success:

```json
{
  "schema_version": "1",
  "ok": true,
  "data": {
    "stage": "report",
    "manifest": "research/1/contracts/0x.../manifest.json",
    "artifacts": []
  },
  "error": null,
  "meta": {
    "command": "contract dossier",
    "run_id": "01J...",
    "chain_id": 1,
    "block_number": 12345678,
    "block_hash": "0x...",
    "duration_ms": 1234
  }
}
```

Failure:

```json
{
  "schema_version": "1",
  "ok": false,
  "data": null,
  "error": {
    "code": "source_match_failed",
    "category": "provenance",
    "retryable": false,
    "message": "recompiled bytecode does not match runtime bytecode",
    "details": {}
  },
  "meta": {}
}
```

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | operational failure |
| 2 | usage/config error |
| 3 | partial success; inspect `manifest.status` and `open_questions` |

## Error codes

| Code | Category | Retryable |
|---|---|---|
| `rpc_timeout` | transport | yes |
| `rpc_rate_limited` | transport | yes |
| `rpc_unavailable` | transport | yes |
| `rpc_method_unsupported` | capability | no |
| `block_unavailable` | capability | no |
| `source_not_found` | source | no |
| `source_match_failed` | provenance | no |
| `compiler_unavailable` | tooling | yes |
| `compiler_error` | tooling | no |
| `proxy_unresolved` | analysis | no |
| `trace_invalid` | provider | no |
| `schema_invalid` | internal | no |
| `budget_exceeded` | policy | no |
| `config_invalid` | usage | no |
| `target_not_found` | usage | no |
| `usage` | usage | no |
| `stage_not_implemented` | internal | no |
| `internal` | internal | no |

`budget_exceeded` is reserved for a hard caller-enforced cap. Protocol budget exhaustion is recorded in `manifest.open_questions` with exit code 3 instead of discarding the evidence already collected.

## Read-only guard

The chain adapter permits only read methods (`eth_chainId`, `eth_blockNumber`, `eth_getBlockBy*`, `eth_getCode`, `eth_getStorageAt`, `eth_call`, `eth_getLogs`, `eth_getTransaction*`, `eth_getTransactionReceipt`, `debug_trace*`, `trace_*`). Signing, `eth_sendRawTransaction`, `personal_*`, and `wallet_*` are rejected before any network call.

## Idempotency

- Same chain, address, block, and provider responses produce the same entity IDs.
- Observations are deduplicated by content hash.
- `--stage` reruns overwrite derived entities but preserve observations.
- Timestamps and `manifest.updated_at` may differ between runs.

## Future commands

- `yangjian query ...` (optional, after a SQLite index).

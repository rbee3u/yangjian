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

`--refresh` bypasses the RPC response cache and the Sourcify/Etherscan HTTP cache. Cached entries are written under `.yangjian/cache/`; the cache key includes the provider profile, method, normalized parameters, and block number/tag. `latest` header lookups are not served from cache.

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
| `schema_invalid` | internal | no |
| `budget_exceeded` | policy | no |
| `config_invalid` | usage | no |
| `target_not_found` | usage | no |

## Read-only guard

The chain adapter permits only read methods (`eth_chainId`, `eth_blockNumber`, `eth_getBlockBy*`, `eth_getCode`, `eth_getStorageAt`, `eth_call`, `eth_getLogs`, `eth_getTransaction*`, `eth_getTransactionReceipt`, `debug_trace*`, `trace_*`). Signing, `eth_sendRawTransaction`, `personal_*`, and `wallet_*` are rejected before any network call.

## Idempotency

- Same chain, address, block, and provider responses produce the same entity IDs.
- Observations are deduplicated by content hash.
- `--stage` reruns overwrite derived entities but preserve observations.
- Timestamps and `manifest.updated_at` may differ between runs.

## Future commands

- `yangjian protocol dossier --chain 1 --target uniswap-v3 ...` (M2).
- `yangjian trace ...` (after a trace provider).
- `yangjian query ...` (optional, after a SQLite index).

# RPC Endpoint Registry

Status: Draft for approval · 2026-11-11

## Purpose

The registry is the local source of truth for which RPC endpoints this machine
has actually probed, what each endpoint served at that time, and whether it is
healthy. It exists because a public endpoint that answers `eth_chainId` is not
necessarily able to serve `eth_getLogs`, historical state, or a trace. It also
keeps chainlist suggestions at arm's length: a suggestion becomes an endpoint
in the registry only after an explicit probe and registration step.

The registry is local state, not research evidence. A capability flag is a
point-in-time observation from this machine; it is not a provider guarantee and
it is not an audit of the endpoint.

## Location and schema

- Default path: `.yangjian/rpc-endpoints.json`
- Override: `--registry <path>` or `YANGJIAN_RPC_REGISTRY`
- The default file is ignored by Git because it is machine-local and may
  contain credential-bearing URLs from public listings.
- File mode is `0600`; text output redacts query credentials and long path
  tokens. JSON output and the registry itself keep the full URL so it can be
  used.

The file is JSON with `schema_version: "1"`:

```json
{
  "schema_version": "1",
  "chain_id": 1,
  "updated_at": "2026-11-11T03:45:00Z",
  "probe": {
    "archive_block": 12965000,
    "logs_window_blocks": 2
  },
  "endpoints": [
    {
      "name": "ethereum-rpc-publicnode-com",
      "url": "https://ethereum-rpc.publicnode.com",
      "transport": "http",
      "enabled": true,
      "source": "chainlist",
      "source_url": "https://chainlist.org/chain/1",
      "tracking": "none",
      "client_version": "reth/v2.10.0",
      "capabilities": {
        "blocks": true,
        "logs": true,
        "call": true,
        "code": true,
        "storage": true,
        "proof": true,
        "fee_history": true,
        "archive": false,
        "debug_trace": false,
        "parity_trace": false,
        "batch": true,
        "websocket": false
      },
      "health": {
        "status": "ok",
        "checked_at": "2026-11-11T03:45:00Z",
        "chain_id": 1,
        "head_block": 26389030,
        "latency_ms": 98,
        "head_lag_blocks": 0,
        "consecutive_failures": 0,
        "last_ok_at": "2026-11-11T03:45:00Z",
        "probe_errors": {
          "eth_getBalance[archive]": "pruned history unavailable"
        }
      },
      "history": [
        {
          "checked_at": "2026-11-11T03:45:00Z",
          "status": "ok",
          "head_block": 26389030,
          "latency_ms": 98
        }
      ]
    }
  ]
}
```

`notes`, `tags`, `requires_env`, and `credential_in_url` are optional. Health
history is capped at the last ten checks. An import never overwrites notes or
existing probe data.

## Capability definitions

| Capability | Probe | Meaning |
|---|---|---|
| `blocks` | `eth_getBlockByNumber("latest", false)` | latest header is readable |
| `logs` | `eth_getLogs` on WETH `Transfer` logs, head-2..head | log queries are served; a range-limit rejection still proves the method exists |
| `call` | `eth_call` WETH `totalSupply()` at latest | contract calls are served |
| `code` | `eth_getCode` WETH at latest | runtime code is served |
| `storage` | `eth_getStorageAt` WETH slot 0 at latest | storage reads are served |
| `proof` | `eth_getProof` WETH at latest | EIP-1186 account proofs are served |
| `fee_history` | `eth_feeHistory(4, "latest", [])` | fee history is served |
| `archive` | `eth_getBalance` and `eth_call` at block 12,965,000 | historical state reads are served; both must succeed |
| `debug_trace` | `debug_traceTransaction(tx, {"tracer":"callTracer"})` | Geth-style call trace is served |
| `parity_trace` | `trace_transaction(tx)` | Parity/OpenEthereum-style trace is served |
| `batch` | `[eth_chainId, eth_blockNumber]` in one HTTP request | JSON-RPC batch requests are served |
| `websocket` | WebSocket transport reached and chain/head read | this entry is a working `ws(s)` endpoint |

The trace target is a simple value transfer selected from the head block (the
first candidate among up to six transactions whose calldata is empty). Using a
recent transaction avoids requiring archive state for the trace probe. The
archive block and log window are stored in `probe` and can be overridden with
`--archive-block` and `--logs-window`.

Capabilities are transport-specific. An HTTP entry and its WebSocket sibling
are separate registry entries and can legitimately differ; a provider may
whitelist a method on one transport and not the other.

## Health status

| Status | Meaning |
|---|---|
| `ok` | chain ID and head are readable, and code, call, and storage all succeed |
| `degraded` | chain ID and head work, but at least one core read (`code`, `call`, `storage`) fails |
| `stale` | healthy reads, but the head is more than ten minutes old or lags the best head in the same check by more than `--max-lag` (default 30 blocks) |
| `rate_limited` | the provider rejected the probe with 429 or an equivalent rate-limit error before core reads succeeded |
| `unreachable` | dial, chain-ID, or head read failed |
| `wrong_chain` | the endpoint reports a chain ID other than the registry chain |
| `auth_required` | a core read returned 401/403 or an explicit authentication error |
| `unconfigured` | the URL references an environment variable that is not set |
| `unprobed` | imported but not yet checked |

An authentication or rate-limit failure on an optional capability (logs,
archive, proof, fee history, trace, batch) is recorded in `health.probe_errors`
but does not by itself downgrade an otherwise healthy endpoint. A rate limit
stops the remaining probes for that endpoint to avoid hammering it; the skipped
capabilities remain false for that check and can be re-probed later.

## Onboarding runbook

When the user supplies a new endpoint (or a chainlist URL is proposed):

1. Record provenance: URL, source (`user`, `chainlist`, or another), and any
   tracking label. Do not treat a chainlist ranking as evidence of quality.
2. Inspect the URL for embedded credentials. Prefer replacing them with
   `${ENV_VAR}` and setting the variable locally. Never paste a private key.
3. Run a dry probe:

   ```sh
   yangjian rpc probe --url "https://example.invalid/rpc" \
     --name example --source user --chain 1 --json
   ```

4. Review `observed_chain_id`, `health.status`, `capabilities`, and
   `health.probe_errors`. A non-1 chain ID is a stop condition for mainnet
   work; do not register it as usable.
5. Register only after review:

   ```sh
   yangjian rpc probe --url "https://example.invalid/rpc" \
     --name example --source user --chain 1 --register --json
   ```

   A wrong-chain or unconfigured result is stored disabled. The command never
   changes `providers.rpc.default`.

6. Re-run the checked endpoint once to establish history:

   ```sh
   yangjian rpc check --name example --json
   ```

7. Select it explicitly when needed:

   ```sh
   yangjian rpc use example --config .yangjian/config.json
   # or, without writing config:
   yangjian rpc use example --print
   ```

`rpc use` requires a matching chain ID and an `ok` status unless `--force` is
passed for `degraded` or `stale`. It writes only
`providers.rpc.default.endpoint`; an existing `YANGJIAN_RPC_URL` environment
variable still overrides the config at runtime.

## Chainlist import runbook

```sh
mkdir -p .yangjian/tmp/chainlist
curl -fsSL https://chainlist.org/rpcs.json -o .yangjian/tmp/chainlist/rpcs.json
yangjian rpc import --from-chainlist .yangjian/tmp/chainlist/rpcs.json --json        # preview
yangjian rpc import --from-chainlist .yangjian/tmp/chainlist/rpcs.json --apply --json
yangjian rpc check --json
```

Import is intentionally not a probe. It keeps HTTP and WebSocket entries,
preserves existing notes and probe data, and disables URLs whose `${VAR}`
references are missing. Chainlist is untrusted input: a URL can be stale,
rate-limited, credential-bearing, or not actually serve chain 1. Only the
probe result is evidence.

## Periodic health check runbook

```sh
# Enabled endpoints; updates health and the ten-entry history.
yangjian rpc check --json

# Also re-check deliberately disabled entries.
yangjian rpc check --all --json

# One endpoint while investigating.
yangjian rpc check --name ethereum-rpc-publicnode-com --json

# Retry endpoints that were rate-limited, leaving space between starts.
yangjian rpc check --status rate_limited --concurrency 1 --delay 3s --json

# Exercise the probe without touching the registry.
yangjian rpc check --no-save --json
```

- Exit code `0` and `meta.status: "complete"` mean every enabled selected
  endpoint is `ok`.
- Exit code `3` and `meta.status: "partial"` mean at least one enabled selected
  endpoint is not `ok`. Read `health.status`, `health.message`, and
  `health.probe_errors`; do not infer a cause without them.
- `consecutive_failures` and `last_ok_at` distinguish a transient 429 from a
  sustained outage. A single failure does not justify deleting history.
- A capability can disappear without the endpoint becoming unhealthy (for
  example, a public trace method starts returning 403). The health status stays
  `ok`; the capability flag becomes false for that check. Re-check before
  relying on a capability for a dossier.
- Retire a dead endpoint with `yangjian rpc disable <name> --note "reason"`.
  Disabling preserves the probe history and removes the endpoint from the
  default `rpc check` selection; `rpc check --all` still rechecks it. Re-enable
  it with `rpc enable <name>`. Do not delete history to hide a failure.

## Selection rules

- For M1/M2 contract research: require `blocks`, `code`, `call`, and `storage`.
  Add `logs` when the workflow needs events. Add `archive` only when pinning a
  block older than the provider's retained state.
- For M4 trace research: require `debug_trace` or `parity_trace` in addition to
  the core reads. A provider with neither cannot produce an `observed_call`
  relation; report `rpc_method_unsupported` rather than guessing.
- Prefer `ok`, zero head lag, and the lowest stable latency among endpoints with
  the needed capabilities. Do not select solely because an endpoint is first in
  a public list.
- Never use `wrong_chain`, `auth_required`, `unreachable`, or `rate_limited`
  entries for evidence. `degraded` is usable only if the specific missing read
  is not needed, and that limitation must be reported.
- Public endpoints are shared infrastructure. Keep concurrency moderate, honor
  429s, and avoid repeated broad sweeps in a short window.

## Security and trust

- Probes are read-only: no keys, signing, or broadcasting. Only allowlisted
  JSON-RPC read methods are used.
- Chainlist and provider responses are untrusted data. URLs are not executed;
  they are passed to the RPC client as endpoints.
- Credentials should live in environment variables. `requires_env` records the
  variable name, never its value. Text output redacts query credentials; JSON
  output is local and intentionally complete.
- A verified source, a healthy endpoint, or a successful trace is not an audit
  and does not prove deployment correspondence.

## Limitations

- Capability flags describe one probe at one time. Providers change whitelists,
  rate limits, and retention without notice.
- The archive check uses one fixed historical block (12,965,000 by default);
  it proves that one historical read path works, not that every historical
  block is retained.
- The logs check uses a two-block WETH window; a provider may support logs but
  reject larger ranges. Range limits are reported in the probe error detail.
- The trace probe uses one simple recent transaction. A provider may trace that
  transaction but fail on larger or older traces.
- `trace_block`, `debug_traceCall`, subscriptions, and transaction submission
  are not probed. They are outside the read-only research path.
- The registry is per chain. A file has one `chain_id`; use a separate
  `--registry` path for another chain.

## Mainnet snapshot

The table below is a point-in-time summary from the local registry, not a
permanent ranking. Re-run `yangjian rpc list --json` for current data. See
`docs/RPC_REGISTRY_MAINNET.md` for the dated snapshot.

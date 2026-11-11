# Mainnet RPC Snapshot — 2026-11-11

This is a dated snapshot of `.yangjian/rpc-endpoints.json` for chain ID 1. The
registry is the authoritative local file; this page records what one probe
sweep observed. Latency is the median successful-probe latency from this
machine and is not a benchmark. Head lag is relative to the best head observed
in the same check; an endpoint re-checked alone reports lag `0`.

Probe window: 2026-11-11 03:37–03:45 UTC, heads around 26,389,040–26,389,075.
Source list: `https://chainlist.org/rpcs.json` (70 Ethereum mainnet entries,
including 8 WebSocket endpoints). One keyed WebSocket endpoint is imported
disabled because `SWIFTNODES_API_KEY` is not set.

Summary of 69 enabled endpoints:

| Status | Count |
|---|---:|
| `ok` | 36 |
| `degraded` | 6 |
| `rate_limited` | 11 |
| `unreachable` | 10 |
| `auth_required` | 6 |
| `stale` | 0 |
| `wrong_chain` | 0 |

Capability totals across all 70 registry entries: 31 logs, 19 archive, 8
`debug_trace`, 9 `parity_trace`.

## Recommended shortlist

These are suggestions for the next research run, not permanent rankings. Use
`yangjian rpc check --name <name>` before relying on one.

- **Best general M1/M2 read path, low latency:** `ethereum-rpc-publicnode-com`
  (102 ms; logs, proof, batch; no archive or trace) and
  `eth-blockrazor-xyz` (102 ms; same capability set). Both were stable across
  the sweep.
- **Best general path with archive and logs:** `1-rpc-thirdweb-com` (236 ms),
  `eth-mainnet-public-blastapi-io` (253 ms), `mainnet-rpc-sentio-xyz` (189 ms),
  `eth-blocktailor-com` (298 ms), and `ethereum-mainnet-rpc-thyme-sh` (312 ms).
- **Best trace path:** `eth-drpc-org` (HTTP 256 ms, WS 245 ms; logs, archive,
  debug and parity trace, proof, batch), `eth-mainnet-nodereal-io` (99 ms; the
  URL embeds a chainlist demo credential), `one-valve-city-vk-demo` (HTTP and
  WS; demo path), and `eth-api-pocket-network` (HTTP; logs, archive, debug and
  parity, proof, batch). `rpc-eth-blockmachine-io` traces but has no archive;
  `ethereum-json-rpc-stakely-io` traces and has proof/batch but no logs or
  archive.
- **Lowest-latency core-only reads:** `singapore-rpc-blxrbdn-com` (84 ms),
  `uk-rpc-blxrbdn-com` and `virginia-rpc-blxrbdn-com` (88 ms),
  `eth-rpc-blxrbdn-com` (90 ms), `eth-mainnet-nodereal-io` (99 ms). These do
  not serve logs in the probe.
- **WebSocket:** `eth-drpc-org-ws` and `one-valve-city-vk-demo-ws` are the
  fullest (logs, archive, debug and parity trace); `ethereum-rpc-publicnode-com-ws`
  has logs and parity trace but no archive or debug trace.

Do not treat sibling URLs as independent redundancy:
`rpc-mevblocker-io*`, `*rpc-blxrbdn-com`, `*gateway-tenderly-co`, and
`eth-blockrazor-xyz*` are groups behind the same provider.

## `ok` endpoints

All rows report chain ID 1. `trace` is `debug`, `parity`, or both. A blank
note means the URL had no detected embedded credential.

| Name | Transport | Lag | Latency (ms) | Logs | Archive | Trace | Proof | Batch | Note |
|---|---|---:|---:|---|---|---|---|---|---|
| 0xrpc-io | http | 3 | 330 | - | - | - | yes | yes |  |
| 0xrpc-io-ws | ws | 3 | 329 | - | - | - | yes | yes |  |
| 1-rpc-thirdweb-com | http | 0 | 236 | yes | yes | - | yes | yes |  |
| api-blockvectra-com-eth-mainnet | http | 0 | 357 | - | - | - | - | yes |  |
| eth-api-pocket-network | http | 2 | 334 | yes | yes | debug+parity | yes | yes |  |
| eth-api-pocket-network-ws | ws | 1 | 338 | yes | - | - | yes | yes |  |
| eth-blockrazor-xyz | http | 4 | 102 | yes | - | - | yes | yes |  |
| eth-blockrazor-xyz-fullprivacy | http | 4 | 102 | yes | - | - | yes | yes |  |
| eth-blockrazor-xyz-maxbackrun | http | 3 | 104 | yes | - | - | yes | yes |  |
| eth-blocktailor-com | http | 1 | 298 | yes | yes | - | yes | yes |  |
| eth-drpc-org | http | 4 | 256 | yes | yes | debug+parity | yes | yes |  |
| eth-drpc-org-ws | ws | 4 | 245 | yes | yes | debug+parity | yes | - |  |
| eth-mainnet-nodereal-io | http | 6 | 99 | yes | yes | debug+parity | yes | yes | URL embeds a demo credential |
| eth-mainnet-public-blastapi-io | http | 5 | 253 | yes | yes | - | yes | yes |  |
| eth-rpc-blxrbdn-com | http | 5 | 90 | - | - | - | yes | yes |  |
| ethereum-json-rpc-stakely-io | http | 3 | 385 | - | - | debug+parity | yes | yes |  |
| ethereum-mainnet-rpc-thyme-sh | http | 1 | 312 | yes | yes | - | yes | yes |  |
| ethereum-public-blockpi-network | http | 5 | 372 | yes | - | - | yes | yes |  |
| ethereum-public-nodies-app | http | 5 | 432 | yes | yes | - | - | yes |  |
| ethereum-rpc-blockreq-com | http | 0 | 108 | yes | - | - | yes | yes |  |
| ethereum-rpc-publicnode-com | http | 0 | 102 | yes | - | - | yes | yes |  |
| ethereum-rpc-publicnode-com-ws | ws | 6 | 96 | yes | - | parity | yes | yes |  |
| mainnet-rpc-sentio-xyz | http | 2 | 189 | yes | yes | - | yes | yes |  |
| one-valve-city-vk-demo | http | 6 | 354 | yes | yes | debug+parity | yes | yes |  |
| one-valve-city-vk-demo-ws | ws | 6 | 330 | yes | yes | debug+parity | yes | yes |  |
| public-eth-nownodes-io | http | 4 | 379 | - | - | - | yes | yes |  |
| rpc-eth-blockmachine-io | http | 2 | 336 | yes | - | debug+parity | - | yes |  |
| rpc-fullsend-to | http | 2 | 280 | yes | - | - | - | yes |  |
| rpc-mevblocker-io | http | 6 | 351 | yes | yes | - | yes | yes |  |
| rpc-mevblocker-io-fast | http | 1 | 347 | yes | yes | - | yes | yes |  |
| rpc-mevblocker-io-fullprivacy | http | 1 | 345 | yes | yes | - | yes | yes |  |
| rpc-mevblocker-io-noreverts | http | 1 | 358 | yes | yes | - | yes | yes |  |
| singapore-rpc-blxrbdn-com | http | 5 | 84 | - | - | - | yes | yes |  |
| uk-rpc-blxrbdn-com | http | 5 | 88 | - | - | - | yes | yes |  |
| virginia-rpc-blxrbdn-com | http | 5 | 88 | - | - | - | yes | yes |  |
| xrpc-cl | http | 7 | 449 | - | - | - | yes | yes |  |

`one-valve-city-vk-demo` uses a demo path. `eth-mainnet-nodereal-io` and the
other chainlist URLs with embedded keys are public demo credentials; they can
expire or be rate-limited. Prefer a local environment-variable reference for
any credential you control.

## `degraded` endpoints

These answer chain ID and head, but at least one core read failed. Tenderly is
usable for logs/archive research; its trace method was rate-limited during the
sweep, so it is not a trace endpoint for this check. Flashbots blocks
`eth_call`, which makes it unusable for contract dossiers. `api.zan.top` gates
`eth_call` behind registration and returns a non-JSON-RPC error body.

| Name | Observed capabilities | Reason |
|---|---|---|
| api-zan-top-eth-mainnet | blocks, code, storage, proof, fee_history, batch | `eth_call: json-rpc error 0` (gated; raw body says registration required) |
| gateway-tenderly-co | logs, archive, core, proof, fee_history | `debug_traceTransaction: 429 Too Many Requests` |
| mainnet-gateway-tenderly-co | logs, archive, core, proof, fee_history | `debug_traceTransaction: 429 Too Many Requests` |
| mainnet-gateway-tenderly-co-ws | logs, archive, core, proof, fee_history, websocket | `debug_traceTransaction: rate limit exceeded` |
| rpc-flashbots-net | blocks, logs, code, storage, proof, fee_history, batch | `eth_call: 403 Forbidden: rpc method is not whitelisted` |
| rpc-flashbots-net-fast | blocks, logs, code, storage, proof, fee_history, batch | `eth_call: 403 Forbidden: rpc method is not whitelisted` |

## Not usable for evidence in this sweep

### Rate-limited (11)

These returned 429 or an equivalent quota error. Several worked partially
before the limit; retry with `yangjian rpc check --status rate_limited
--concurrency 1 --delay 3s`. A second paced pass recovered
`api-blockvectra-com-eth-mainnet` (now `ok`). Repeated failures are provider
quotas, not a registry bug.

| Name | Note |
|---|---|
| api-ethereum-mainnet-reth-n-dwellir-com | chain ID 429 |
| eth-api-onfinality-io | block read succeeded, `eth_getCode` 429 |
| eth-mainnet-g-alchemy-com-demo | demo key 429 |
| eth-meowrpc-com | latest block 429 |
| eth-merkle-io | chain ID 429 |
| ethereum-mainnet-gateway-tatum-io | 5 requests/minute; `eth_call` is a paid feature |
| lb-routeme-sh-evm | chain ID 429; free key required |
| public-1rpc-io | Nodies rate limit; `eth_call`/logs previously returned 410 discontinued |
| rpc-nodeflare-app | 1 request per 10 seconds without a key |
| rpc-swiftnodes-io | 2 requests/second demo plan; key required for a higher limit |
| services-tokenview-io-vipapi | chain ID 429 |

### Authentication or quota required (6)

| Name | Note |
|---|---|
| api-mycryptoapi-com | HTTP 403 |
| eth-mainnet-4everland-org-ws | WebSocket 401 |
| eth-mainnet-rpcfast-com | invalid embedded API key |
| rpc-hostdefi-com | Cloudflare 403 challenge |
| rpc-owlracle-info | 401 origin mismatch |
| virtual-mainnet-rpc-tenderly-co | plan quota exhausted |

### Unreachable (10)

| Name | Note |
|---|---|
| api-securerpc-com | connection EOF |
| cloudflare-eth-com | chain ID works, latest block returns `Internal error` |
| ethereumnodelight-app-runonflux-io | 503 from the Flux gateway |
| mainnet-eth-cloud-ava-do | connection EOF |
| rpc-blocknative-com-boost | connection EOF |
| rpc-chain49-com | 404 |
| rpc-driftguard-live | 502 from Cloudflare |
| rpc-eth-gateway-fm | 402; endpoint discontinued |
| rpc-polysplit-cloud-1 | non-JSON HTML response |
| ws-rpc-graffiti-farm-ws | dial timed out through the local proxy |

### Unconfigured (1)

| Name | Requires |
|---|---|
| rpc-swiftnodes-io-keyed | `SWIFTNODES_API_KEY` |

## Reproducing

```sh
curl -fsSL https://chainlist.org/rpcs.json -o .yangjian/tmp/chainlist/rpcs.json
yangjian rpc import --from-chainlist .yangjian/tmp/chainlist/rpcs.json --apply
yangjian rpc check --json
yangjian rpc list --json
```

The probe suite, status definitions, and the onboarding and health-check
runbooks are in `docs/RPC_REGISTRY.md`. The local registry is ignored by Git;
this snapshot is the tracked, dated summary.

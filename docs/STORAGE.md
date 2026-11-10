# Storage — Yangjian

Status: Draft for approval · 2026-11-10

## Root layout

```
research/
  <chain-id>/
    contracts/<address>/
    discoveries/<factory>/<token0>-<token1>-<fee>/
    protocols/<slug>/
targets/
  mainnet.json
.yangjian/
  config.json
  cache/
```

## Contract dossier

```
research/<chain-id>/contracts/<address>/
  manifest.json
  observations/<hash>.json
  entities/
    contracts/<chain-id>-<address>.json
    versions/<chain-id>-<address>@<block>.json
    sources/<hash>.json
    relations/<hash>.json
  artifacts/
    abi.json
    metadata.json
    stdjsoninput.json
    storagelayout.json
    state.json
    proxy.json
    static.json
    provenance/
      stdjsoninput.json
      solc-output.json
      onchain-runtime.bin
      recompiled-runtime.bin
      solc-error.txt          (only when compilation failed)
    sources/<keccak256>.sol
    sourcify-response.json
  report.md
  topology.mmd
  architecture.mmd
  relations.json
  evidence.json
```

## Discovery dossier

```
research/<chain-id>/discoveries/<factory>/<token0>-<token1>-<fee>/
  manifest.json
  observations/<hash>.json
  artifacts/
    discovery.json
```

The manifest target is the factory. Discovery dossiers do not create Contract entities; the discovered address is passed to `contract dossier` explicitly or through the registry.

## Protocol dossier

```
research/<chain-id>/protocols/<slug>/
  manifest.json
  observations/<hash>.json
  entities/
    protocols/<slug>.json
    deployments/<hash>.json
    relations/<hash>.json
  artifacts/
    discovery.json
  protocol.json
  report.md
  topology.mmd
  architecture.mmd
  relations.json
  evidence.json
```

Protocol dossiers reference contract dossiers by `(chain ID, address)`; they do not copy contract artifacts. `relations.json` aggregates protocol-owned relations with the relations read from each referenced contract dossier; `evidence.json` indexes the protocol observations and contract summaries. Contract dossiers remain the owners of their own artifacts and evidence.

## File roles

- `manifest.json` — dossier index; source of truth for status and artifact references.
- `observations/` — immutable content-addressed evidence.
- `entities/` — derived entities; rebuildable from observations.
- `artifacts/` — source, ABI, metadata, state, proxy, and provenance evidence files.
- `report.md` — human narrative with evidence references.
- `topology.mmd` / `architecture.mmd` — Mermaid sources; rendered images optional.
- `relations.json` — convenience export for agent queries; derived from entities.
- `evidence.json` — structured observation/artifact index.

## Cache

- `.yangjian/cache/` — solc binaries, RPC responses, and provider HTTP responses; not committed.
- RPC cache: `cache/rpc/<endpoint-hash>/<key-hash>.json`, keyed by method, normalized
  parameters, and block number. Only pinned-block reads and pinned headers are served
  from cache; `latest` header lookups are always fresh. `--refresh` bypasses reads.
- Source cache: `cache/providers/{sourcify,etherscan}/<key-hash>.json`, keyed by request
  method and URL. API keys are not stored in cache keys or entries.
- Cached provider output is evidence for reruns, not a substitute for re-querying when
  `--refresh` is requested.

## Fixtures

- `testdata/fixtures/` — recorded provider responses; committed.
- `testdata/golden/` — expected outputs; committed.
- Fixtures pin block number/hash and contain no secrets.

## Git policy

Commit:

- `research/` dossiers.
- `targets/` registry.
- `testdata/fixtures/` and `testdata/golden/`.
- Design docs, ADRs, schemas.

Ignore:

- `.yangjian/cache/`.
- downloaded solc binaries.
- local env files and secrets.
- temporary build artifacts.

Large observation sets should be reviewed; use Git LFS only if needed.

## Incremental updates

- Rerun a stage with `--stage`; observations deduplicate by hash.
- Derived entities are overwritten; manifest status and `open_questions` update.
- Schema migrations are explicit and versioned; never silently reinterpret old dossiers.

## Secrets

- API keys never appear in config, dossiers, fixtures, logs, or reports.
- Config references environment variable names only.
- `.env` files are ignored and never committed.

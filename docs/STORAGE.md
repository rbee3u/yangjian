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
  report.md
  diagrams/
    topology.mmd
```

Protocol dossiers reference contract dossiers by `(chain ID, address)`; they do not copy contract artifacts.

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

- `.yangjian/cache/` — solc binaries and temporary compiler directories; not committed.
- Provider response caching is not implemented in M1; chain and Sourcify reads are
  fetched fresh. `--refresh` is accepted but currently has no effect.
- The planned cache key includes chain ID, method, normalized params, and block hash/tag;
  `latest` responses will use a short TTL and will not be long-term evidence.

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

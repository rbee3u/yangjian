# Workflow — Yangjian

Status: Draft for approval · 2026-11-10

## Revised phases

The original six phases remain a coverage checklist, but execution is iterative:

1. **Target Resolution** — registry entry or explicit input; record network, block scope, source URL.
2. **Contract Evidence** — runtime code, proxy probe, source artifacts, build facts.
3. **Source Provenance** — P0–P3; bind source to deployed bytecode.
4. **Static Structure** — inheritance, storage layout, immutables, ABI, call sites.
5. **State Reads** — pinned-block `eth_call`, storage, logs.
6. **Topology and Report** — typed relations, diagrams, dossier, open questions.

Deployment discovery and source provenance iterate: an address reveals source, and source constants reveal more addresses.

## M1 — Contract Dossier

Deployment discovery is a separate command so the discovered address is never typed in by hand:

```sh
yangjian contract discover --chain 1 --target uniswap-v3 \
  --token0 USDC --token1 WETH --fee 3000 --block latest --json
```

It pins a block, calls `getPool(address,address,uint24)` on the registry factory, fetches the pool's runtime code, and writes a `discovery` dossier with the block header, call, and code observations. A zero pool is a complete negative result; a pool address with no code is `partial`.

Contract dossier command shape:

```sh
yangjian contract dossier \
  --chain 1 \
  --address 0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8 \
  --block latest \
  --out research/1/contracts/0x8ad599c3A0ff1De082011EFDDc58f1908eb6e6D8 \
  --json
```

Stages:

1. `code` — `eth_getCode`, runtime bytecode hash, block pinning, ContractVersion.
2. `source` — Sourcify fetch; record match mode and compiler settings; compare ABI, compiler settings, and source hashes with Etherscan V2 when an API key is configured, recording match/mismatch/skipped.
3. `provenance` — P1 embedded-metadata/source/settings binding when `bytecodeHash` is not `none`; P2 rebuild in all cases; P0 fallback.
4. `analyze` — proxy detection (EIP-1967, EIP-1822, EIP-1167, beacon with `implementation()` resolution). When a P2 build exists, parse the recorded solc output for the deployed contract's inheritance linearization, storage layout, immutable references, method identifiers, source imports, and call sites (internal, external, library, delegatecall, create). Emit `source_dependency` relations for imports and inheritance. Source structure is derived from the same compiler output that supports provenance, not from the latest GitHub tree.
5. `state` — pinned-block calls: `slot0()`, `liquidity()`, `ticks(currentTick)`, plus pool immutables (`token0`, `token1`, `factory`, `fee`, `tickSpacing`, `maxLiquidityPerTick`). Emits `onchain_reference` relations for the three addresses.
6. `report` — Markdown narrative, Mermaid topology/architecture diagrams, `relations.json`, `evidence.json`, manifest update.

Each stage writes observations and updates the manifest. `--stage` runs one stage and emits its envelope; later stages require earlier artifacts.

Current stage order: `code → source → provenance → analyze → state → report`.
The default pipeline runs all six. `report` renders whatever earlier stages produced and marks missing sections explicitly.

### P2 provenance evidence

The `provenance` stage:

- downloads the exact solc build from solc-bin and verifies its SHA-256 and keccak256;
- refuses standard JSON sources that use `urls`, lack inline content, or contain absolute or parent-traversal paths;
- runs the pinned binary in a fresh temporary directory with `--base-path` and `--allow-paths` restricted to that directory, a timeout, and input/output size caps;
- compares the recompiled runtime bytecode with the pinned-block on-chain bytecode after masking solc's immutable and link references;
- records the compiler hashes, exact input, raw output, raw bytecode, masking counts, and first mismatch offset.

A bytecode mismatch is a valid negative result: the dossier stays `partial`, the SourceRevision stays `P0`/`unproven`, and the mismatch is recorded in `open_questions`. A compiler or download failure is an operational error and is also recorded in the manifest. Rerunning `source` with an unchanged source revision preserves existing build evidence; rerunning `provenance` replaces it.

## M2 — Protocol Dossier

```sh
yangjian protocol dossier --chain 1 --target uniswap-v3 --block latest --json
```

1. Load the target registry entry with its official deployment source URL, retrieval date, contract addresses, and token addresses.
2. Pin one block, then call `getPool(token0, token1, fee)` on the seeded Factory at that block and fetch the pool runtime code.
3. Run Contract Dossiers at the same block for Factory, Pool, and NonfungiblePositionManager; Router if the registry provides it. Contract dossiers stay at `research/<chain>/contracts/<address>/`.
4. Build relations:
   - Factory → Pool: `onchain_reference` from the `getPool` observation.
   - Pool → token0/token1/factory: `onchain_reference` from the pool's State stage.
   - NPM → Factory and Router → Factory: `onchain_reference` from `factory()` at the pinned block.
   - Router → WETH9: `onchain_reference` from `WETH9()` at the pinned block.
   - Source imports/inheritance remain `source_dependency` relations in each contract dossier and are aggregated in the protocol's `relations.json`.
5. Emit `topology.mmd`, `architecture.mmd`, `report.md`, `relations.json`, `evidence.json`, and a protocol manifest.
6. Stop at budgets: default 50 contract dossiers and 1000 observations across contract dossiers; both are CLI flags. Discovery runs before the budget check, so a missing pool is always recorded. No factory event scan or depth-recursive crawl is performed in this version.

A contract dossier failure is recorded in `open_questions` and the remaining contracts still run. A returned address that disagrees with the registry is recorded as an open question; it is not silently trusted. No `observed_call` relation and no sequence diagram is emitted until trace access exists.

## Agent rules

- Invoke the CLI with `--json`; read the envelope and `meta.block`.
- Never invent an address; use the target registry or explicit user input.
- Never claim source match without a provenance level and evidence refs.
- Never claim an execution call without an `observed_call` relation backed by a trace.
- Record chain ID, block number/hash, address, source coordinates, and provider.
- Mark unknown or deferred facts in `open_questions`; do not substitute recent data for historical evidence.
- Respect budgets; on `budget_exceeded`, stop and report.
- Do not retry non-retryable errors; surface the code.

## Deferred workflows

- Historical state: enable `--block <n>` when a free archive key is configured.
- Trace: add `TraceProvider`, `observed_call`, transaction reports, sequence diagrams.
- L2: add Base after mainnet acceptance.

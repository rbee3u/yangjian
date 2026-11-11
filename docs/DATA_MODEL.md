# Data Model — Yangjian

Status: Draft for approval · 2026-11-10

## Principles

- Observations are immutable, content-addressed evidence.
- Entities are derived from observations and can be rebuilt.
- Every on-chain fact carries chain ID, block number, and block hash.
- Contract identity is stable; behavior and source bindings are block-versioned.
- Every derived entity carries `evidence_refs`.
- Unknown fields are tolerated on read; derived entities are rewritten from the current schema, and `schema_version` gates migrations.

## Entity graph

```mermaid
erDiagram
  MANIFEST ||--o{ CONTRACT : references
  CONTRACT ||--o{ CONTRACT_VERSION : versions
  CONTRACT_VERSION ||--o| SOURCE_REVISION : source
  CONTRACT_VERSION ||--o{ OBSERVATION : evidence
  RELATION }o--|| CONTRACT : from
  RELATION }o--|| CONTRACT : to
  RELATION ||--o{ OBSERVATION : evidence
```

## Identifiers

| Entity | ID format |
|---|---|
| Contract | `chainId:address` (e.g. `1:0x8ad5...`) |
| ContractVersion | `chainId:address@validFromBlock` |
| Account | `account:<chainId>:<address>` (trace endpoint without runtime code) |
| SourceRevision | `sha256:<hash>` over sources + compilation settings |
| Observation | `sha256:<hash>` over canonical request/response |
| Relation | `type:from:to:evidenceHash` (for `observed_call`, the evidence token is `<traceObservation>#<callPath>` so sibling calls cannot collide) |
| Manifest | file path; no synthetic ID |

No random UUIDs. Observations and artifacts are deduplicated by content hash.

## Observation

| Field | Type | Notes |
|---|---|---|
| `schema_version` | string | e.g. `1` |
| `id` | string | content hash |
| `kind` | enum | `rpc_call`, `source_fetch`, `build`, `analysis`, `proxy_probe`, `report`, `metadata_binding`, `source_crosscheck`, `static_analysis` |
| `run_id` | string | shared with logs and envelope |
| `request` | object | method, normalized params, URL class; secrets redacted |
| `response_hash` | string | hash of canonical response |
| `response_ref` | string | path under `observations/` |
| `block` | object | `{chain_id, number, hash}` for chain observations |
| `retrieved_at` | RFC3339 | retrieval timestamp |
| `source` | object | provider name, version, endpoint class |
| `error` | object | optional `{code, message}` |
| `meta` | object | optional provider-specific data |

## Contract

| Field | Type | Notes |
|---|---|---|
| `id` | string | `chainId:address` |
| `chain_id` | integer | |
| `address` | string | checksummed |
| `versions` | string[] | ContractVersion IDs |
| `labels` | string[] | optional |
| `evidence_refs` | string[] | required |
| `schema_version` | string | |

## ContractVersion

| Field | Type | Notes |
|---|---|---|
| `id` | string | `chainId:address@validFromBlock` |
| `contract_id` | string | |
| `valid_from_block` | integer | first proven block |
| `valid_from_source` | enum | `observed`, `estimated`, `deployment` |
| `valid_to_block` | integer/null | null = no later version observed; it does not assert that the version is still current |
| `runtime_bytecode_hash` | string | keccak256 |
| `proxy_type` | enum | `unknown`, `none`, `EIP-1967`, `EIP-1822`, `EIP-1167`, `beacon`, `other` |
| `implementation_address` | string/null | for proxies |
| `source_revision_id` | string/null | |
| `abi_hash` | string/null | |
| `evidence_refs` | string[] | required |
| `schema_version` | string | |

## SourceRevision

| Field | Type | Notes |
|---|---|---|
| `id` | string | content hash |
| `sources` | object[] | `{path, keccak256, content_ref}` |
| `metadata_ref` | string/null | Sourcify metadata artifact |
| `std_json_input_ref` | string/null | standard JSON input |
| `compilation` | object | compiler build, optimizer, EVM version, remappings, libraries, bytecode hash settings |
| `registry_match` | enum | `exact_match`, `match`, `none`, `unknown` |
| `provenance_level` | enum | `P0`, `P1`, `P2`, `P3` |
| `correspondence` | enum | `proven` or `unproven` |
| `registry_sources` | object[] | provider, address, retrieved_at; includes Etherscan when a cross-check fetch succeeded |
| `build` | object/null | P2 build evidence: compiler version and hashes, exact input/output refs, raw on-chain and recompiled bytecode refs, match, masked reference counts, byte lengths, first mismatch offset, warnings |
| `evidence_refs` | string[] | required |

`build` is attached after the `provenance` stage. It is not part of the SourceRevision content ID: rerunning provenance updates the evidence without changing source identity. A successful P2 comparison sets `provenance_level` to `P2` and `correspondence` to `proven`; a mismatch or compiler failure leaves `P0`/`unproven`.

## Relation

| Field | Type | Notes |
|---|---|---|
| `id` | string | `type:from:to:evidenceHash` |
| `type` | enum | `source_dependency`, `onchain_reference`, `observed_call`, `inferred_relation` |
| `from` | string | entity ID |
| `to` | string | entity ID |
| `block_scope` | object/null | `{from_block, to_block}` |
| `attributes` | object | type-specific: selector, slot, event, source path, tx hash |
| `evidence_refs` | string[] | required |
| `schema_version` | string | |

Type-specific requirements:

- `source_dependency`: source path and symbol. Import relations point from `source:<revision>#<path>` to a content-addressed source file; inheritance relations point from a derived source coordinate to its base contract coordinate. Attributes record `relation`, `source_path`, `symbol`, and `source_revision_id`.
- `onchain_reference`: method or event, block number/hash. M1 pool relations record `method`, `selector`, `address`, `block_hash`, and `observation_id`. Protocol dossiers additionally record `role`/`source_role` and, for NPM/Router factory reads, whether the returned address matches the registry factory. Protocol `relations.json` aggregates protocol-owned relations with relations read from referenced contract dossiers; contract dossiers remain the owners.
- `observed_call`: transaction hash, trace reference, call path. M4 also records call type, selector, and whether each endpoint was a contract or an account at the receipt block. `from`/`to` use `ContractID` when `eth_getCode` returned code and `AccountID` otherwise. A failed or budget-skipped code check stores the endpoint as `AccountID` with kind `unknown` and `from_checked`/`to_checked` false, plus an open question; it is not silently treated as a proven account.
- `inferred_relation`: heuristic name and rule version; never rendered as a fact.

`observed_call` is the only relation type allowed to represent execution. A source call site or an address read never becomes an `observed_call` without a trace observation.

## Artifact kinds

| Kind | Contents |
|---|---|
| `contract`, `contract_version` | Derived entities |
| `sourcify_response`, `metadata`, `abi`, `std_json_input`, `storage_layout` | Registry source artifacts |
| `etherscan_response`, `source_crosscheck` | Secondary source response and the structured comparison result |
| `p2_std_json_input`, `solc_output`, `onchain_runtime`, `recompiled_runtime`, `solc_error` | P2 build evidence |
| `proxy` | Proxy probe slots and resolved type/address |
| `static_analysis` | Derived source structure: inheritance, storage layout, immutables, methods, call sites |
| `discovery` | Factory lookup inputs, pinned block, `getPool` result, and discovered runtime code |
| `state` | Pinned-block pool state and immutables |
| `transaction`, `transaction_raw`, `receipt` | M4 transaction summary and raw provider evidence |
| `trace_raw`, `trace`, `sequence` | Raw trace, normalized call frames, and Mermaid sequence |
| `protocol`, `deployment` | M2 derived entities |
| `protocol_result` | Protocol dossier summary (`protocol.json`) |
| `report`, `topology`, `architecture`, `relations`, `evidence` | Human and machine report outputs |

Artifact paths and hashes are recorded in the manifest; `evidence.json` is a
structured index of the observations and artifacts used by the report.

### Static analysis artifact

`artifacts/static.json` is derived from the P2-matched solc output; it is not derived from the latest source tree. It records:

| Field | Type | Notes |
|---|---|---|
| `source_revision_id` | string | Revision whose build matched |
| `compilation_target` | string | `source:Contract` |
| `linearization` | string[] | C3 order computed from AST `baseContracts`, most derived first |
| `inheritance` | object[] | Contract, source, kind, abstract flag, direct bases |
| `storage` | object[] | Contract, label, slot, offset, type key, AST id |
| `storage_types` | object | Transitively referenced solc storage types |
| `immutables` | object[] | Name, type, declaring contract, AST id, byte ranges |
| `methods` | object[] | Signature and selector from `evm.methodIdentifiers` |
| `functions` | object[] | Visibility, mutability, modifiers, source location |
| `call_sites` | object[] | Caller, kind (`internal`, `external`, `library`, `library_delegatecall`, `delegatecall`, low-level, `callback`, `create`), callee, target type, source location |
| `imports` | object[] | Source file, imported path, imported symbols, source location |
| `call_site_counts` | object | Counts by kind |
| `warnings` | string[] | Incomplete or truncated analysis |

A call site is a possible call in the matched source. It must not be rendered as an execution call; only an `observed_call` relation backed by a trace may do that.

### Discovery result

A discovery dossier is not a Contract Dossier. Its target is the factory address, and its artifact records:

| Field | Type | Notes |
|---|---|---|
| `chain_id` | integer | |
| `factory`, `token0`, `token1` | address | Inputs, normalized |
| `fee` | integer | uint24 fee tier |
| `block` | object | `{chain_id, number, hash}` |
| `pool` | address | Zero address when the factory has no pool |
| `found` | boolean | True only when an address and runtime code were observed |
| `pool_code_hash` | string | `keccak256:` of runtime code; empty when no code |
| `pool_code_size` | integer | Runtime bytecode length |
| `observation_ids` | string[] | Header, `eth_call`, and optional `eth_getCode` observations |
| `artifact` | string | `discovery.json` |

## Manifest

| Field | Type | Notes |
|---|---|---|
| `schema_version` | string | |
| `dossier_type` | enum | `contract`, `protocol`, `discovery`, `transaction` |
| `target` | object | chain ID plus address, protocol slug, or transaction hash |
| `tool_version` | string | |
| `run_id` | string | |
| `status` | enum | `complete`, `partial`, `failed` |
| `block` | object | `{number, hash}` |
| `artifacts` | object[] | `{kind, path, hash}` |
| `contract_ids` | string[] | |
| `observation_ids` | string[] | |
| `source_revision_ids` | string[] | optional; one per resolved source revision |
| `relation_ids` | string[] | protocol dossiers list their own relations; contract relations stay in contract dossiers |
| `protocol_ids` | string[] | optional; protocol dossiers |
| `deployment_ids` | string[] | optional; protocol dossiers |
| `open_questions` | string[] | |
| `created_at` | RFC3339 | |
| `updated_at` | RFC3339 | |

## M2 entities

M1 is contract-centric. M2 adds two entities and references contract dossiers by address instead of duplicating them.

**Protocol**

| Field | Type | Notes |
|---|---|---|
| `id` | string | `protocol:<slug>` |
| `name` | string | |
| `aliases` | string[] | |
| `deployments` | string[] | Deployment IDs |
| `target_registry_refs` | string[] | `targets/` entries |
| `evidence_refs` | string[] | required |

**Deployment**

| Field | Type | Notes |
|---|---|---|
| `id` | string | `chainId:<slug>@<block>` |
| `protocol_id` | string | |
| `chain_id` | integer | |
| `block` | object | `{number, hash}` |
| `contract_version_ids` | string[] | |
| `evidence_refs` | string[] | required |

**Network** is configuration, not a dossier entity: chain ID, name, explorer, provider profile, and default block tag live in config and the target registry.

## Provenance levels

- **P0**: registry claim only; `correspondence` is `unproven`.
- **P1**: on-chain metadata pointer resolved and source hashes/settings checked.
- **P2**: local recompile matched after masking immutables and constructor arguments.
- **P3**: P2 plus source maps and complete build artifacts.

Contracts with `metadata.bytecodeHash = none` skip P1 and go directly to P2. If P2 fails, record P0 with `correspondence: unproven`.

## Validation rules

- Every non-Manifest entity has at least one `evidence_ref`.
- Every chain observation has `block.number` and `block.hash`.
- Chain observations are only written after the provider's `eth_chainId` matches the requested chain ID.
- `SourceRevision.registry_match` and `provenance_level` are required.
- `ContractVersion` has a runtime bytecode hash or an explicit error observation.
- Relation type-specific attributes are required as listed above.
- An `observed_call` relation must reference a successful trace observation and a transaction hash; it must not be derived from source or an address read.
- A trace endpoint uses `ContractID` only when code was observed at the receipt block; otherwise it uses `AccountID`.
- Secrets never appear in observations, manifests, or reports.
- Unknown JSON fields are tolerated on read; derived entities are rewritten from the current schema.

## JSON Schema

Files under `schemas/`:

- `observation.schema.json`
- `contract.schema.json`
- `contract_version.schema.json`
- `source_revision.schema.json`
- `relation.schema.json`
- `manifest.schema.json`
- `protocol.schema.json`
- `deployment.schema.json`

All schemas use JSON Schema draft 2020-12, require `schema_version`, and allow additional properties where forward compatibility matters. `internal/schema` enforces the same structural invariants at store write and read boundaries without a JSON Schema dependency; `internal/schema/schema_test.go` checks the shipped schema files parse and cover the entity set. The schemas are the machine-readable tool contract for tests and the coding agent.

## Storage mapping

```
manifest.json
observations/<hash>.json
entities/contracts/<chainId>-<address>.json
entities/versions/<chainId>-<address>@<block>.json
entities/sources/<hash>.json
entities/relations/<hash>.json
topology.mmd
architecture.mmd
report.md
relations.json
evidence.json
```

Protocol and transaction dossiers use the same layout additions documented in
`docs/STORAGE.md` (`entities/protocols`, `entities/deployments`,
`sequence.mmd`, `transaction.json`).

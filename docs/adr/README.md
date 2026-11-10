# Architecture Decision Records

All decisions below were made during the design grilling on 2026-11-10. Each ADR is intentionally short; the code and docs must follow them.

| # | Decision | Status |
|---|---|---|
| [0001](0001-claude-code-as-reasoning-layer.md) | Claude Code is the reasoning layer; the Go CLI is deterministic | Accepted |
| [0002](0002-mvp-scope.md) | MVP scope is Ethereum protocol research, not auditing, multi-chain delivery, or indexing | Accepted |
| [0003](0003-external-data-adapters.md) | External data access goes through replaceable adapters; archive access is deferred | Accepted |
| [0004](0004-source-provenance-levels.md) | Source provenance is graded; registry claims alone are never a source match | Accepted |
| [0005](0005-file-first-knowledge.md) | Research knowledge is file-first and Git-versioned, not database-first | Accepted |
| [0006](0006-contract-identity-and-versioning.md) | Contract identity is stable; behavior and source bindings are block-versioned | Accepted |
| [0007](0007-observation-first-model.md) | Research knowledge is built from immutable observations and a derived entity graph | Accepted |
| [0008](0008-relation-evidence-types.md) | Relations carry their evidence type; static dependencies are not observed calls | Accepted |
| [0009](0009-sourcify-primary-etherscan-secondary.md) | Sourcify is the primary provenance source; Etherscan is a secondary cross-check | Accepted |
| [0010](0010-m1-static-analysis-boundary.md) | M1 derives structural facts from solc standard JSON; Slither and Foundry are deferred | Accepted |
| [0011](0011-cli-surface.md) | The CLI starts with one high-level dossier command and stage-level reruns | Accepted |
| [0012](0012-entity-ids.md) | Entities use deterministic keys; observations and artifacts are content-addressed | Accepted |
| [0013](0013-address-centric-storage.md) | Research storage is address-centric, with protocol dossiers referencing contracts | Accepted |
| [0014](0014-json-envelope.md) | CLI output uses a uniform JSON envelope and explicit exit codes | Accepted |
| [0015](0015-m1-schema.md) | M1 uses a typed core schema with explicit evidence references | Accepted |
| [0016](0016-cli-framework-and-dependencies.md) | The CLI uses Cobra and a small pinned dependency set | Accepted |
| [0017](0017-agent-guidance.md) | Global rules live in AGENTS.md; workflows live in Skills; schemas are the tool contract | Accepted |
| [0018](0018-fixture-first-testing.md) | Tests are fixture-first and offline by default | Accepted |
| [0019](0019-target-registry.md) | Targets come from a curated evidence-backed registry or explicit input, never generated addresses | Accepted |
| [0020](0020-m2-topology-discovery.md) | M2 discovers topology in evidence order and stops at an explicit budget | Accepted |
| [0021](0021-provider-behavior.md) | Providers are profiled; immutable responses are cached; concurrency is bounded | Accepted |
| [0022](0022-solc-sandbox.md) | Solc runs as a pinned binary in a path-jailed temporary directory with resource limits | Accepted |
| [0023](0023-config-and-secrets.md) | Configuration is JSON with environment-variable references for secrets | Accepted |
| [0024](0024-error-contract.md) | Errors use stable codes and structured logs on stderr | Accepted |

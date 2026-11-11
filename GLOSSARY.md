# Yangjian

Domain language for a local-first system that builds verifiable knowledge about EVM protocols from source, chain state, and execution evidence.

## Language

**Protocol**:
A named system of smart contracts that together provide a service; its on-chain presence is one or more Deployments.
_Avoid_: Project, dapp, app

**Research Target**:
The entity a research run starts from: a protocol name, contract address, transaction hash, or source repository, together with its network and block scope.
_Avoid_: Subject, object, asset

**Deployment**:
The concrete set of Contract Versions a Protocol has on one network at a known block, as established by on-chain evidence.
_Avoid_: Instance, installation

**Contract**:
A stable on-chain identity `(chain ID, address)` whose code, source binding, and behavior are versioned by block range.
_Avoid_: Instance, account

**Contract Version**:
The implementation, source revision, ABI, and behavior binding that applies to a Contract over a block range.
_Avoid_: Snapshot, state

**Evidence**:
A recorded fact together with its provenance, sufficient for another person or tool to reproduce or audit the claim.
_Avoid_: Data, finding

**Observation**:
An immutable, content-addressed record of a retrieval or computation result that serves as evidence for derived entities.
_Avoid_: Fact, record

**Relation**:
A typed, evidence-backed link between two research entities, where the type records how the link was established.
_Avoid_: Edge, connection

**On-chain Observation**:
A fact read directly from a node at a specified block, such as bytecode, storage, logs, or a transaction trace.
_Avoid_: Live data, real data

**Static Analysis**:
A fact derived from source code or bytecode without executing against chain state.
_Avoid_: Code review, source review

**Simulation**:
An execution result produced locally, such as a fork replay, that is not itself an on-chain observation.
_Avoid_: Test, emulation

**Source Provenance**:
The evidence chain that binds a deployed runtime bytecode to a specific source revision and build configuration.
_Avoid_: Source verification, source match

**Verified Source**:
A source-code claim published by a verification registry for an address; it is evidence of publication, not proof that the source matches the deployed runtime bytecode or that the contract is safe.
_Avoid_: Audited source, trusted source

**Point-in-time Research**:
Reading state and execution evidence pinned to a specific block, rather than indexing or scanning a block range.
_Avoid_: Historical analysis, history scan

## Source Provenance Levels

**P0 Registry Claim**:
A provenance level where only a registry's verified flag and published artifacts are recorded; the binding to on-chain bytecode is not independently checked.

**P1 Metadata Binding**:
A provenance level where the on-chain solc metadata pointer is resolved and source hashes and build settings are checked locally against the runtime bytecode.

**P2 Reproducible Build**:
A provenance level where the exact compiler, settings, remappings, and libraries are used to rebuild the runtime bytecode and compare it after masking immutables and constructor arguments.

**P3 Full Reproducibility**:
A provenance level that additionally reproduces source maps and complete build artifacts.

## Relation Types

**source_dependency**:
A Relation established from source-level imports, inheritance, or static references.
_Avoid_: Call, dependency

**onchain_reference**:
A Relation established from an address stored or returned on-chain, such as an immutable, constant, factory mapping, or registry entry.
_Avoid_: On-chain call

**observed_call**:
A Relation established from an actual transaction execution trace.
_Avoid_: Static call, internal call

**inferred_relation**:
A Relation established by heuristic inference rather than direct observation or static proof.
_Avoid_: Guess, derived relation

## Registry Match Modes

**Exact Match**:
A Sourcify match mode where recompiled bytecode is identical to on-chain bytecode modulo libraries and immutables, and the embedded metadata fingerprint is identical.

**Match**:
A Sourcify match mode where recompiled bytecode corresponds to on-chain bytecode but the metadata fingerprint differs, so exact source text and paths are not proven.

## Research Artifacts

**Manifest**:
The dossier index that lists the research target, tool and schema versions, referenced entities and observations, artifacts, status, and open questions.
_Avoid_: Index file, metadata file

**Static Structure**:
Facts derived from a matched Solidity AST — inheritance linearization, storage layout, immutables, method identifiers, and possible call sites — describing source shape, not runtime behavior.
_Avoid_: Call graph, execution graph, control flow

**Build Evidence**:
The recorded compiler identity, exact compilation input and output, runtime-bytecode artifacts, masking rules, and comparison result that support a provenance level.
_Avoid_: Compile log, build output

**Contract Dossier**:
The evidence-backed research artifact for one Contract, covering provenance, ABI, selected state, relations, and open questions.
_Avoid_: Contract report, contract profile

**Discovery Dossier**:
The evidence-backed research artifact for one factory lookup at a pinned block, covering the query inputs, the returned address, and its runtime code.
_Avoid_: Pool lookup, address search

**Protocol Dossier**:
The evidence-backed research artifact for a Protocol's Deployment, covering topology, contract dossiers, and open questions.
_Avoid_: Protocol report, protocol profile

**Transaction Dossier**:
The evidence-backed research artifact for one transaction trace, covering raw and normalized call frames, observed_call relations, and a sequence diagram.
_Avoid_: Trace file, transaction report

**Observed Call**:
A Relation asserting that a call frame appeared in a transaction trace. The endpoints are Contracts when code was observed at the receipt block and Accounts otherwise.
_Avoid_: Static call, inferred call, call graph edge

## Research Operations

**Target Registry**:
A curated, evidence-backed list of protocol entrypoints, each with chain, address, official source URL, and retrieval date.
_Avoid_: Address book, allowlist

**Provider Profile**:
A named configuration for a data source, including endpoint, auth reference, capabilities, rate limits, timeout, and retry policy.
_Avoid_: Config, provider config

**Exploration Budget**:
The per-dossier limits on relation depth, contract count, and request count that bound topology discovery.
_Avoid_: Limit, cap

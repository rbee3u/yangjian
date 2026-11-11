# PRD — Yangjian

Status: Draft for approval · 2026-11-10

## 1. Problem

Protocol research today mixes model memory, partial source reading, and ad-hoc RPC calls. Conclusions are hard to reproduce, and "verified source" is often treated as proof of deployed behavior. The user needs a local-first research assistant that answers protocol questions from verifiable source, chain state, and execution evidence.

## 2. Product

Yangjian is a deterministic Go CLI plus Claude Code Skills. Claude Code is the reasoning layer; Yangjian gathers evidence, binds source to deployed bytecode, records observations, and writes dossiers. It does not embed an LLM, sign transactions, or broadcast.

## 3. Users and scenarios

Primary user: a protocol researcher working locally in Claude Code on Ethereum mainnet.

- "What is this address running, and which source revision?"
- "What is the proxy implementation at this block?"
- "Read slot0/liquidity/ticks for this pool."
- "Map Factory, Router, Pool, and PositionManager."
- Secondary (after trace access returns): "What happened in this transaction?"

## 4. Principles

- Evidence over memory; no invented addresses.
- Observed, static, and simulated facts stay separate.
- Verified source is not an audit and not deployment proof.
- Static dependency is not an observed call.
- Read-only by default; no private keys, signing, or broadcasting.
- Reproducible: chain ID, block number and hash, address, source coordinates, provider.
- Unknowns are explicit.

## 5. Scope

MVP: Ethereum mainnet; M1 Contract Dossier; M2 Protocol Dossier; M4 transaction trace when the provider supports `debug_traceTransaction` or `trace_transaction`. Multi-chain is parameterized internally; L2 support comes later. History means point-in-time reads, not an indexer.

Out of scope: vulnerability detection/audit, transaction broadcasting, historical indexing, MCP (deferred), graph/vector databases, CI (local Makefile only for now). Trace-dependent sequence diagrams are implemented behind a provider capability probe; a public RPC without `debug_traceTransaction` or `trace_transaction` cannot produce them.

## 6. Success criteria

M1 and M2 checklists live in `docs/ROADMAP.md`. M1 must produce a reproducible Contract Dossier for the Uniswap V3 USDC/WETH 0.3% pool with source provenance, ABI, storage layout, pinned-block reads, relations, report, and evidence index. M2 must produce the protocol topology report without trace.

## 7. Risks

Provider drift, Sourcify `match` versus `exact_match`, P2 rebuild failures, public RPC limits, untrusted source, and schema evolution.

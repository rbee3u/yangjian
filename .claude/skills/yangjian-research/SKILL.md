---
name: yangjian-research
description: Research EVM protocols with the local Yangjian CLI. Use when the user asks about deployed contract source, proxy state, pinned-block pool state, protocol topology, or a transaction trace, and the repository has a `yangjian` command or `cmd/yangjian`.
---

# Yangjian research workflow

Use the deterministic CLI to gather evidence; do not substitute model memory for
chain data. Read `AGENTS.md` first and follow its non-negotiable rules.

## 1. Resolve the target

- Use an address from `targets/<network>.json` or an address the user supplied.
- Never guess, synthesize, or auto-accept a search result as a contract address.
- Record chain ID, block number/hash, address, source coordinates, and provider.

## 2. Pick the command

- `yangjian contract discover --chain <id> --target <slug> --token0 <sym> --token1 <sym> --fee <n> --json`
  resolves a factory-deployed contract without hardcoding its address.
- `yangjian contract dossier --chain <id> --address <0x...> [--block <n>] --json`
  builds or reruns a Contract Dossier. Use `--stage <stage>` to rerun one stage.
- `yangjian protocol dossier --chain <id> --target <slug> [--block <n>] --json`
  builds the seeded protocol topology at one pinned block.
- `yangjian trace transaction --chain <id> --tx <hash> --json`
  records a real trace and `observed_call` relations when the provider supports it.

Always pass `--json` and read the envelope: `ok`, `data`, `error`, `meta`.
Check `meta.status`, `manifest.status`, and `open_questions` before summarizing.
A partial result (exit 3) is evidence with caveats, not a failure to hide.

## 3. Interpret evidence correctly

- Verified source is not an audit and not deployment correspondence; cite the
  provenance level (`P0`–`P3`) and `correspondence`.
- A source import, address read, or static call site is not an execution call.
  Only an `observed_call` relation backed by a trace may describe execution.
- A proxy result depends on the pinned block; do not present it as timeless.
- Historical reads require an archive-capable provider. If a method is
  unsupported or a block is unavailable, report that fact; never simulate it.
- Prefer the dossier's `report.md`, `relations.json`, and `evidence.json` over
  free-form recollection, and keep artifact references in the answer.

## 4. Stop conditions

Stop and report when an address is not in the registry and the user did not
supply one, a provider capability is missing, a budget is exhausted, or source
or trace evidence is unavailable. Do not retry non-retryable errors.

---
issue: null
title: "TypeScript migration for the hive GUI frontend"
type: enhancement
complexity: L
priority: P2
stage: DONE
shipped: 2026-08-09
---

# TypeScript migration for the hive GUI frontend

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Stage:** DONE
- **Exec plan:** [docs/exec-plans/completed/typescript-migration.md](../exec-plans/completed/typescript-migration.md)

## Problem

`cmd/hivegui/frontend/` was ~15.9k LOC of vanilla ESM JavaScript (7,316 in
`src/`, 8,454 in `test/`) with **zero** type information: no `.ts` file, no
`tsconfig.json`, no JSDoc `@param`/`@type`, no `// @ts-check`. Biome linted and
formatted it; nothing checked types, so payload-shape and refactor errors only
surfaced at runtime in the GUI.

## Desired behavior

The frontend is TypeScript, and CI rejects a type error before it can reach a
build.

## Success criteria

- Every file under `cmd/hivegui/frontend/src/` and `test/` is TypeScript.
- `tsc --noEmit` runs as a CI gate.
- No behavior change in the GUI.

## Non-goals

- Rewriting frontend architecture; this is a type-level migration.

## Notes

Decided 2026-07-25 in `docs/analysis/2026-07-19-improvement-plan/phase-2-ci-and-tooling.md` §2c,
gated behind Biome. Delivered in waves 1–7d, 2026-08-07 → 2026-08-09. Spec
written retroactively (2026-08-30) from the completed exec plan.

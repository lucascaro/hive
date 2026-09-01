---
issue: null
title: "A red CI check name should say which stage failed"
type: enhancement
complexity: M
priority: P2
stage: TRIAGE
---

# A red CI check name should say which stage failed

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** —

## Problem

`.github/workflows/ci.yml` has one `build` job, fanned out by a matrix label, so
the only check names a PR ever shows are `Build, Vet & Test (Linux)`,
`(macOS)` and `(Windows)`. Behind each sit sixteen steps: the Wails bootstrap,
a frontend build, `tsc`, two UI-lint steps, `go build`, `go vet`, staticcheck,
`govulncheck`, `go test`, the Go e2e leg, Biome, Vitest, spec discovery, and
both Playwright suites.

A developer looking at a red check therefore learns the operating system and
nothing else. Answering "is this red mine?" costs a log dive every time —
which is the same question spec 245 was raised to eliminate, approached from
the other side: 245 made the suite deterministic, this makes a failure legible.

## Desired behavior

A red check name identifies the stage that failed, so the diff-to-blame is
obvious from the PR page without opening logs.

## Success criteria

- A developer can tell from the check name alone whether a red gate implicates
  their diff. (Inherited verbatim from spec 245, where it was the one criterion
  the harness fix did not address — see that spec's `## Success criteria` note.)
- Each independently-failing concern reports under its own check name — at
  minimum: typecheck/lint, Go build+vet+test, frontend unit, e2e-mock, e2e-real.
- Total CI wall-clock and billed minutes per PR do not regress meaningfully
  against the single-job baseline. Measure before and after.

## Non-goals

- Changing what any stage tests. This is about how failures are reported, not
  coverage.
- The e2e-real suite's stability, which spec 245 owns.

## Notes

Split out of spec 245 on 2026-08-31, at its merge gate. 245's harness fix
(PR #307) satisfied its other three criteria; this one needs its own design and
was blocking a P1 unblock while `main` was red.

The design tension worth resolving first: splitting the job duplicates the
expensive bootstrap — `scripts/ci-bootstrap.sh` installs the pinned Wails CLI
and generates bindings, and the Playwright browser download is already
documented in `ci.yml` as the single biggest cost in CI (219s on Windows). A
naive split multiplies both. Options to weigh: a `needs:`-chained build-once
job publishing artifacts, more aggressive caching, or splitting only the legs
that actually fail independently often (e2e-real being the obvious first).

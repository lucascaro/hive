# The e2e-real Playwright suite fails on main and blocks every PR

- **Issue:** —
- **Type:** bug
- **Complexity:** M
- **Priority:** P1
- **Exec plan:** —

## Problem

`CI (Linux)` and `CI (macOS)` fail on `main` itself, including the current tip (`13121a0`). Over the last 8 runs on `main`: Linux failed 5, macOS failed 4, Windows failed 0 — Windows is clean because `build-windows.yml` runs only `go test ./...` and never invokes Playwright. Every failure is in the `e2e-real` suite (`playwright test --config=playwright.real.config.js`, which drives a real `hived` through `hived-ws-bridge`); the Go suites, the Vitest unit/DOM layers, and the mock-bridge `e2e` suite are all consistently green.

The recurring failures were originally recorded as clustering in three spec
files, naming all five `wheel-scroll` cases and four `scroll-codex` cases.
**That list was wrong** and is corrected below; the original is kept in the
"Original failure list (superseded)" note at the end of this section so the
CI-era evidence is not lost.

**Measured 2026-08-09 on `main` (`f5f9665`), macOS, three consecutive local
runs** (`npx playwright test --config=playwright.real.config.js`, no `CI` env).
All three runs produced the *same* 9 passed / 3 failed:

- `test/e2e-real/scroll-codex.spec.ts:215` — markers survive grid↔single toggles under continuous output
- `test/e2e-real/scroll-codex.spec.ts:262` — viewport converges to the bottom after a mode switch under continuous output
- `test/e2e-real/scroll-restream-strand.spec.ts:97` — single full-buffer session: no transient viewport-jump on threshold-crossing resizes

Everything else passes, including **all five `wheel-scroll` cases** and the
other two `scroll-codex` cases (`unscrolled user not stranded by a resize under
load`, `scrolled reader not yanked to the bottom`) — the two the original list
called out by name as the concrete thread.

Two things this measurement changes about the problem statement above:

1. **The three failures are deterministic on this machine, not flaky** — 3/3
   runs, identical set. That does not contradict the CI observations (a busy
   shared runner is a different load profile, and the original green→red flips
   across doc-only commits are still real), but it means a fix does not need a
   loaded runner to reproduce: it reproduces on an idle laptop, every time.
2. **CI has been green since the quarantine landed, not since the flake was
   fixed.** `scroll-codex`, `scroll-restream-strand` and `wheel-scroll` all carry
   `test.skip(!!process.env.CI, 'quarantined on CI — flaky setup, spec 245')`, so
   the CI leg runs **2 of the 12** e2e-real tests (`glyph-utf8`, `lifecycle`).
   The last 8 `main` runs being green is the quarantine working, and is not
   evidence toward this spec's success criteria.

Line numbers are as of the TypeScript conversion (waves 7c/7d) — the specs are
`.ts` now, and every line number in the original list predates that rename.

> **Original failure list (superseded).** Recorded against the `.js` specs before
> the TS migration: `wheel-scroll.spec.js` `:185`/`:194`/`:203`/`:215`/`:228`;
> `scroll-codex.spec.js` `:166`/`:203`/`:244`/`:300`;
> `scroll-restream-strand.spec.js` `:59`.

The failures are load- and timing-dependent, not deterministic: `scroll-codex.spec.js:244` fails with `baseY` at 1624 against an `expect(...).toBeGreaterThan(4500)` precondition — the buffer never reached the scrollback cap the assertion depends on, so the test is failing its *setup*, not its invariant. Several of these were introduced alongside the scroll/replay fixes in the current `[Unreleased]` block, which is where the flakiness likely entered.

Observed twice on PR #244 in a way that isolates the cause from any code change: Linux flipped green → red across `b404d1c`, and macOS flipped green → red across `365ec37`. **Both are documentation-only commits.** A subsequent re-run of the identical macOS commit passed with no changes at all.

## Desired behavior

CI is a trustworthy merge gate: a red check means the PR broke something. `e2e-real` either passes deterministically or is explicitly quarantined so it cannot fail a PR for reasons unrelated to that PR's diff. Whichever way it goes, a developer never has to ask "is this red mine?" — which is the state the suite is in today, and the reason it is a P1 despite being test-only.

## Success criteria

- Ten consecutive `Build, Vet & Test (Linux)` and `(macOS)` runs on unchanged
  `main` are green. (Check names as of the single-matrix `CI` workflow; they
  were `CI (Linux)` / `CI (macOS)` when this spec was written.)
- No spec depends on an unbounded "output reached the cap" precondition without either waiting for that condition deterministically or failing with a clear "precondition not met" skip.
- If any spec is quarantined rather than fixed, it is skipped explicitly with a linked follow-up, not left failing.
- A developer can tell from the check name alone whether a red gate implicates their diff.

## Non-goals

- Rewriting the scroll/replay behavior the specs cover. The production code may well be correct; this is about the harness.
- Changing what `e2e-real` tests. Coverage should not shrink as the fix.
- Windows CI, which does not run Playwright at all (arguably its own gap, but a separate one).

## Notes

Surfaced while driving PR #244 (Restart Hive) through `/hs-review-loop`. That PR's own Windows failure *was* real and was fixed; these are not. Evidence is recorded in `docs/exec-plans/completed/243-restart-hive-doesnt-reliably-restart-daemon.md` under `## CI note`.

Root cause is **not** diagnosed here — this spec records the evidence and the impact. The load-dependent `baseY` precondition in `scroll-codex.spec.js:286` is the most concrete starting thread: a runner under CPU contention produces less output in the same wall-clock window, which fits every observation, including why the failure set grows on the slower/busier runs.

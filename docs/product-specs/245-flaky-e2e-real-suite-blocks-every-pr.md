# The e2e-real Playwright suite fails on main and blocks every PR

- **Issue:** —
- **Type:** bug
- **Complexity:** M
- **Priority:** P1
- **Exec plan:** —

## Problem

`CI (Linux)` and `CI (macOS)` fail on `main` itself, including the current tip (`13121a0`). Over the last 8 runs on `main`: Linux failed 5, macOS failed 4, Windows failed 0 — Windows is clean because `build-windows.yml` runs only `go test ./...` and never invokes Playwright. Every failure is in the `e2e-real` suite (`playwright test --config=playwright.real.config.js`, which drives a real `hived` through `hived-ws-bridge`); the Go suites, the Vitest unit/DOM layers, and the mock-bridge `e2e` suite are all consistently green.

The recurring failures cluster in three spec files:

- `test/e2e-real/wheel-scroll.spec.js` — `:185` pixel-mode, `:194` line-mode, `:203` legacy `wheelDeltaY`, `:215` mouse-tracking forwards the wheel, `:228` alternate-buffer does not swallow the wheel
- `test/e2e-real/scroll-codex.spec.js` — `:166` markers survive grid↔single toggles, `:203` viewport converges after a mode switch, `:244` unscrolled user not stranded by a resize under load, `:300` scrolled reader not yanked to the bottom
- `test/e2e-real/scroll-restream-strand.spec.js` — `:59` no transient viewport-jump on threshold crossing

The failures are load- and timing-dependent, not deterministic: `scroll-codex.spec.js:244` fails with `baseY` at 1624 against an `expect(...).toBeGreaterThan(4500)` precondition — the buffer never reached the scrollback cap the assertion depends on, so the test is failing its *setup*, not its invariant. Several of these were introduced alongside the scroll/replay fixes in the current `[Unreleased]` block, which is where the flakiness likely entered.

Observed twice on PR #244 in a way that isolates the cause from any code change: Linux flipped green → red across `b404d1c`, and macOS flipped green → red across `365ec37`. **Both are documentation-only commits.** A subsequent re-run of the identical macOS commit passed with no changes at all.

## Desired behavior

CI is a trustworthy merge gate: a red check means the PR broke something. `e2e-real` either passes deterministically or is explicitly quarantined so it cannot fail a PR for reasons unrelated to that PR's diff. Whichever way it goes, a developer never has to ask "is this red mine?" — which is the state the suite is in today, and the reason it is a P1 despite being test-only.

## Success criteria

- Ten consecutive `CI (Linux)` and `CI (macOS)` runs on unchanged `main` are green.
- No spec depends on an unbounded "output reached the cap" precondition without either waiting for that condition deterministically or failing with a clear "precondition not met" skip.
- If any spec is quarantined rather than fixed, it is skipped explicitly with a linked follow-up, not left failing.
- A developer can tell from the check name alone whether a red gate implicates their diff.

## Non-goals

- Rewriting the scroll/replay behavior the specs cover. The production code may well be correct; this is about the harness.
- Changing what `e2e-real` tests. Coverage should not shrink as the fix.
- Windows CI, which does not run Playwright at all (arguably its own gap, but a separate one).

## Notes

Surfaced while driving PR #244 (Restart Hive) through `/hs-review-loop`. That PR's own Windows failure *was* real and was fixed; these are not. Evidence is recorded in `docs/exec-plans/active/243-restart-hive-doesnt-reliably-restart-daemon.md` under `## CI note`.

Root cause is **not** diagnosed here — this spec records the evidence and the impact. The load-dependent `baseY` precondition in `scroll-codex.spec.js:286` is the most concrete starting thread: a runner under CPU contention produces less output in the same wall-clock window, which fits every observation, including why the failure set grows on the slower/busier runs.

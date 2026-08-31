---
issue: null
title: "The e2e-real Playwright suite fails on main and blocks every PR"
type: bug
complexity: M
priority: P1
stage: IMPLEMENT
---

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

Everything else passes, including **all five `wheel-scroll` cases** and the other
two `scroll-codex` cases: `full scrollback: an unscrolled user is not stranded in
history by a resize under load` (now `:320`, was `:244`) and `a reader scrolled
into history is not yanked to the bottom by a resize replay` (now `:385`, was
`:300`). The first of those is the one the original list singled out as *the
concrete thread* — the `baseY`-precondition failure quoted below. It passes here,
3/3.

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

Line numbers above are as of `f5f9665`. The original list's numbers had already
drifted before the TypeScript rename — the quarantine commit (`428e43d`) and the
Biome reformat (`63a86fd`) moved `scroll-codex`'s four tests from
`166/203/244/300` to `189/236/290/355` while the file was still `.js`; the rename
(wave 7c) then added the last ~26. Do not read the old numbers as "the `.js` line,
before the rename" — they are older than that.

> **Original failure list (superseded).** Recorded against the `.js` specs before
> the TS migration: `wheel-scroll.spec.js` `:185`/`:194`/`:203`/`:215`/`:228`;
> `scroll-codex.spec.js` `:166`/`:203`/`:244`/`:300`;
> `scroll-restream-strand.spec.js` `:59`.

~~The failures are load- and timing-dependent, not deterministic:~~ **Superseded by the 2026-08-09 baseline above** — the three failures that remain are deterministic on an idle machine, and the test this paragraph describes now passes. Kept because the *mechanism* it identifies is still the best hypothesis for the CI-side behavior: `scroll-codex.spec.js:244` (the `unscrolled user is not stranded` case, today `scroll-codex.spec.ts:320`) failed on CI with `baseY` at 1624 against an `expect(...).toBeGreaterThan(4500)` precondition — the buffer never reached the scrollback cap the assertion depends on, so the test was failing its *setup*, not its invariant. Several of these were introduced alongside the scroll/replay fixes in the then-current `[Unreleased]` block, which is where the flakiness likely entered.

Observed twice on PR #244 in a way that isolates the cause from any code change: Linux flipped green → red across `b404d1c`, and macOS flipped green → red across `365ec37`. **Both are documentation-only commits.** A subsequent re-run of the identical macOS commit passed with no changes at all.

## Resolution (2026-08-24)

**Root cause: the three specs were stale, not flaky.** Each one asserted
`traceTags(page, 'replay-request') > 0` as a non-vacuity guard, in a scenario
where the tile is *following the bottom*. `decideResizeReplay`
(`src/app/session-term.ts`, via `src/lib/scroll-*`) deliberately SKIPS the
destructive full-ring replay for a follower — that skip is itself a shipped fix
(the renderer freeze / viewport thrash under live output). A healthy follower
therefore emits `replay-skip` and exactly zero `replay-request`, and the guard
fails against correct code, every time.

That is why the 2026-08-09 baseline saw them fail 3/3 on an idle machine, and
why the same set failed 10/10 here before the change. Nothing was
load-dependent about them. The genuinely load-dependent `baseY` precondition
this spec chased is a different test, and it passes.

**Fix:** the guard now counts resizes that reached the replay *decision*
(`replay-request` + `replay-skip`) rather than demanding one particular
outcome. The invariants each spec exists for — markers exactly once and in
order, viewport converges to the bottom, a follower is never stranded
mid-history — are unchanged.

**Evidence for the re-gate:**

| suite | before | after |
|-------|--------|-------|
| the 3 quarantined specs, `--retries=0`, macOS idle | 0/10 runs green | 10/10 runs green |
| full `e2e-real` with `CI=true` | 11 passed / 10 skipped | 21 passed, 3/3 runs |
| mock `e2e` with `CI=true` | 182 passed | 182 passed, 0 flaky |

**Re-gated, with two exceptions.** The file-level
`test.skip(!!process.env.CI, ...)` quarantine is removed from all three specs,
so CI runs the suite again instead of 2 of its 12 tests.

The exception is `scroll-codex.spec.ts` → *a reader scrolled into history is
not yanked to the bottom by a resize replay*, which is quarantined on CI **on
its own**, and for a different reason than the rest of this spec describes:

- It is **genuinely load-dependent**, which is what this spec originally
  suspected of all of them and which turned out to be false for the other
  three. Green locally on an idle machine across many full-suite runs; failed
  on CI macOS (run 33143976246) and CI Linux (run for `ea0e572`) with
  `viewportY == baseY == 5000`; and reproduces locally **1 run in 3** with 18
  CPU hogs running.
- Its guard is **not** stale. The assertion is correct, and under contention
  the reader really is being yanked to the bottom by a replay — the exact
  scroll-jump class this file exists to catch. So the open question is about
  the product, not the harness: does the follow-intent restore hold when the
  replay lands slowly?

The second exception is `scroll-codex.spec.ts` → *viewport converges to the
bottom after a mode switch under continuous output*, and it fails a different
way: `resizeDecisions() === 0`, meaning the ⌘G toggles reached no replay
decision at all, so the guard trips before any invariant is exercised. Seen on
CI macOS (run 33233271507) and CI Linux (run for `552824c`); green locally
across many full-suite runs.

Its cause is **not** established. The suspicion is shared-daemon state: this
suite runs one daemon for every spec file, so tile count and the replay column
baseline both depend on what earlier files left behind — a measured example is
that removing sessions *between* tests took this file from 0 failures in 6 runs
to 2. That points at harness isolation rather than the product, which is the
opposite of the load-dependent test above. It is a hypothesis, not a diagnosis,
and it should not be lifted without one.

That distinction matters for whoever picks this up. The other three failed
against correct code and needed a test fix. This one may be reporting a real
defect and needs a diagnosis, not a guard change. Ten consecutive green CI
runs remain the bar before it comes off quarantine.

`retries: 1` stays on both Playwright configs, but is now paired with
`failOnFlakyTests: !!process.env.CI`: a retry buys the diagnostics of a second
attempt, not a green check. Green means every test passed first try — which is
this spec's success criterion, made structural instead of a thing someone has
to remember to audit. A quarantine that silently outlives its cause is exactly
what happened here, and that is what the flag prevents next time.

Remaining: the first success criterion (ten consecutive green `main` runs) can
only be observed on CI, and starts from the commit that lands this.

## Resolution (2026-08-31)

The 2026-08-24 round fixed a stale vacuity guard. It did not fix the suite: `main`
went red again on 2026-08-30 at `885a2a6` — a **docs-only** commit — and stayed
red for eight consecutive `CI` runs. Nothing in a diff caused it.

### What was actually failing

Not one test. Across those runs the failure rotated between
`scroll-codex.spec.ts:247` (`markers.length` = 0), `scroll-codex.spec.ts:375`
(`baseY` = 2483/2484/506 against `> 4500`), `scroll-restream-strand.spec.ts:110`
(`baseY` ≈ 500) and both `wheel-scroll` DECSET tests (10 s `waitForFunction`
timeout). `failOnFlakyTests` then turned each one into a red gate, which is
working as intended — the tests were genuinely failing on their first attempt.

### Root cause 1 — the ws-bridge applied keystrokes out of order

`cmd/hived-ws-bridge/main.go` dispatched **every** JSON-RPC frame on its own
goroutine (`go s.dispatch(req)`). `WriteStdin` frames are a keystroke *stream*,
and a goroutine per frame only takes the write mutex in whatever order the Go
scheduler picks. Mutual exclusion is not ordering. Under CPU contention adjacent
keys swap, so the command a spec types is not the command the shell runs.

Caught directly, by printing the terminal tail when a sentinel wait timed out:

    typed:     HIVE_READY_mthhi3gn_1
    echoed:    HIVE_READY_mthhig3n1_

Two adjacent transpositions, same characters. Every downstream symptom follows:
a mangled `awk` line prints no markers (`markers.length` = 0), a mangled flood
never fills the scrollback (`baseY` ≈ 500 — the attach replay alone), a mangled
`printf '\033[?1000h'` never sets mouse-tracking mode (DECSET timeout).

**Fix:** `WriteStdin` now goes down a per-connection ordered lane — one writer
goroutine draining a buffered channel — while everything else keeps the
goroutine-per-request concurrency. (The first attempt handled it inline on the
read loop; review caught that `attachWriteFrame` backpressures whenever the pty
is not draining, which is normal in this suite, so an inline write would stall
`ResizeSession` / `CloseAttach` / `KillSession` for the whole connection.)
Ordering and teardown are pinned by `TestWriteStdinPreservesArrivalOrder` and
`TestShutdownTerminatesWhileStdinWriteIsBlocked`.

This is harness-only — the shipped GUI reaches the same daemon through the
in-process Wails binding, not this bridge. Wails does dispatch each frontend
call in its own goroutine (`internal/frontend/desktop/*/frontend.go`), so the
same hazard may exist on the product path; that is unverified and belongs in
its own spec.

### Root cause 2 — readiness waits matched *replayed* output

Every spec file drives the same long-lived shell on the same daemon, and every
fresh page attach replays that session's whole scrollback. A wait for the fixed
string `HIVE_PUMP_DONE` was therefore satisfied by an *earlier test's* copy,
replayed — before the pump it was waiting on had printed a single line. Likewise
`type('stty -echo'); waitForTimeout(200)` proved nothing: typed input is not
lost while the shell is busy, it is queued, and the scrollback specs leave
40 000-60 000 line floods running past the end of their own test.

**Fix:** `test/e2e-real/term-harness.ts`.

- `sentinel()` — every readiness marker is unique per call, so a replayed one
  cannot satisfy it.
- `settleShell()` — Ctrl-C (kills a leftover flood; a no-op at an idle prompt),
  then a typed marker round trip. Replaces the fixed sleep in all six specs.
- `waitForSentinel()` — on timeout, reports the terminal tail, which is what
  made root cause 1 visible at all.

### Root cause 3 — a precondition asserted after the scenario destroyed it

`scroll-codex.spec.ts:375` re-checked `baseY > 4500` *after* its threshold-
crossing resizes. Narrowing to 780 px wraps each flood line onto two rows, so
cap-trim discards half the logical lines, and widening back to 1200 px unwraps
what is left: 2483/2484 on both CI Linux and CI macOS, almost exactly half the
5000-line cap. It only ever passed when the flood happened to still be running
and refilled the buffer. The `expect.poll` before the resizes already
establishes the cap, which is when it matters; the second assertion is deleted.

### Evidence

Repro harness: the full suite with `CI=1 TERM=dumb`, under 18 `yes > /dev/null`
CPU hogs on an 18-core macOS host.

| | before | after |
|---|---|---|
| loaded runs | 5 failed / 15 passed, then 3 failed, then 2 failed | **21 passed, 4/4 runs** |
| wall clock per loaded run | 4.2 – 9.1 min | 2.6 – 2.8 min |
| unloaded `CI=1` run | 2 failed (first run of the session) | 21 passed |

Also green: `go vet ./...`, `go test ./...`, `vitest run` (696), the mock
`e2e` suite with `CI=1` (214 passed), `biome ci .`, `tsc --noEmit`,
`check-spec-discovery.mjs`.

### Coverage moved up, not down

`viewport converges to the bottom after a mode switch under continuous output`
is **un-quarantined**. Its comment guessed shared-daemon state; the real cause
was root cause 1, and it is green under load now.

One CI quarantine remains: `a reader scrolled into history is not yanked to the
bottom by a resize replay`. Re-checked after all three fixes, it still fails 2/2
under load with `viewportY == baseY == 5000` — the reader really is yanked. That
is a product question about the resize replay, not a harness one, and this
spec's non-goals put it out of scope. It stays skipped on CI with this note.

### Still open

The ten-consecutive-green-runs criterion can only be observed on CI, from the
landing commit forward.

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

Root cause is **not** diagnosed here — this spec records the evidence and the impact. The load-dependent `baseY` precondition originally cited as `scroll-codex.spec.js:286` (today `scroll-codex.spec.ts:366`, the cap assertion inside the `unscrolled user is not stranded` test) was the most concrete starting thread *for the CI-side flakiness*: a runner under CPU contention produces less output in the same wall-clock window, which fits every CI observation, including why the failure set grows on the slower/busier runs.

**As of the 2026-08-09 baseline that thread is no longer the best place to start.** That test passes 3/3 locally; the three that fail do so deterministically on an idle machine, so they can be debugged directly without reproducing runner contention at all. Start there.

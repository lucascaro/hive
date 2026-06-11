# Exec plan: codex scroll-jump on replay

- **Spec:** [docs/product-specs/codex-scroll-jump-on-replay.md](../../product-specs/codex-scroll-jump-on-replay.md)
- **Stage:** REVIEW
- **Branch:** codex-scroll-jump

## Research

Investigation was reproduce-first (project rule: no patches without a
reproducer). Hypotheses formed from reading the replay machinery:

- **H1 — reset queue-jump.** `handleScrollbackEvent('begin')` called
  `term.reset()` synchronously, but xterm's `write()` is async-queued:
  unparsed pre-begin live bytes parse *after* the wipe and then appear
  a second time when the replay re-streams them. Scales with output
  rate → codex-specific.
- **H2 — 8 MiB raw-ring replay storm.** Replay payload is the raw PTY
  ring; parsing several MiB takes ~seconds while the 250 ms mode-snap
  fires long before.
- **H3 — `_replayWantsBottom` has no generation token.** Two in-flight
  replays could consume each other's intent.
- **H4 — alt-screen flips in the replayed ring.**

New repro harness `test/e2e-real/scroll-codex.spec.js` (the mock-Wails
e2e layer never emits `scrollback_replay_begin/done`, so the client
replay state machine had ZERO e2e coverage before this): real hived,
two sessions (so grid toggles always cross `REPLAY_COL_THRESHOLD`),
bursty awk marker pump approximating codex output, scroll tracer
armed. The second session is created by speaking the ws-bridge's
JSON-RPC directly from Node (the bridge does not implement
`ListAgents`, so the launcher path is unavailable in this harness).

## Findings

- **Repro:** "a reader scrolled into history is not yanked to the
  bottom by a resize replay" FAILED on pre-fix code: after a
  resize-triggered replay, `viewportY === baseY` — the reader was at
  the bottom despite `_replayWantsBottom === false`.
- **Root cause:** the position is destroyed by `term.reset()` at
  replay-begin and the restream leaves the viewport tracking the
  bottom; #213's flag only suppressed the *final explicit snap*, which
  was already irrelevant. Neither H2 nor H3 nor H4 was needed to
  explain the report; H1's duplication risk is real (analysis +
  ordering test) though the integrity test alone did not trip on the
  dev machine — the fix removes the mechanism regardless.
- Marker integrity and bottom-anchoring-after-mode-switch held on the
  pre-fix code in this environment (tests kept as regression
  invariants, trace-guarded against vacuity).

## Approach

In `src/lib/scrollback.js` `handleScrollbackEvent`:

1. **begin**: capture `st._replayPrevFromBottom = baseY - viewportY`
   before any wipe; make the reset parse-ordered
   (`term.write('', () => term.reset())`) so backlog cannot repaint
   after the wipe (H1); decoder still resets at event time (decode
   order is event order).
2. **done**: consume `_replayWantsBottom` and `_replayPrevFromBottom`
   at event time; place the viewport parse-ordered
   (`term.write('', finish)`): snap to bottom when wanted (default,
   #213 semantics), else `scrollToLine(newBaseY - prevFromBottom)`
   clamped at 0 — approximate under soft-wrap changes but keeps the
   reader in place instead of at the bottom.

Plus `src/lib/scroll-debug.js`: a bounded ring tracer
(`window.__hive_scrolltrace`, gated on `hive.debug`) recording
resize / replay-arm / replay-request / begin / done / mode-snap with
viewport coordinates. Ships permanently for field diagnosis; the e2e
tests use it to prove scenarios actually fire replays.

## Tests

- Unit (`test/unit/scrollback.test.js`, rewritten around a queue-aware
  mock term): parse-ordered reset (backlog → reset → replay order),
  position capture at begin, parse-ordered snap/restore at done,
  clamp-at-0, no-write fallback, flag consumption.
- e2e-real (`test/e2e-real/scroll-codex.spec.js`): the three
  invariants above, trace-verified non-vacuous.
- Full matrix green: vitest 113, mock e2e 34 (+1 skipped), e2e-real 5.

## PR convergence ledger

<!-- Append-only. One line per /hs-review-loop iteration. PR #222. -->
- **2026-06-10 iter 1** — verdict: REQUEST_CHANGES; action: autofix+push; head_sha: 0d75ed3.
- **2026-06-10 iter 2** — verdict: REQUEST_CHANGES; action: autofix+push; head_sha: 8ad48e0.

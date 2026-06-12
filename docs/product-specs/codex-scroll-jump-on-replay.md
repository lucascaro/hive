# GUI: scrollback replays destroy the reader's scroll position (codex scroll-jump)

- **Issue:** (verbal report — no GitHub issue)
- **Type:** bug
- **Complexity:** M
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/codex-scroll-jump-on-replay.md](../exec-plans/active/codex-scroll-jump-on-replay.md)

## Problem

User report after #213/#214 and #208/#209 shipped: "Scrolling still
jumps around, mostly when using codex and switching to grid mode or
back."

Root cause (diagnosed via a new e2e-real repro, see exec plan): when a
resize or grid reflow triggers a scrollback replay while the user is
scrolled up reading history, `handleScrollbackEvent('begin')` calls
`term.reset()` — which destroys the scroll position — and the
re-stream leaves the viewport tracking the bottom. #213's
`_replayWantsBottom = false` flag only suppressed the *final explicit
snap* on replay-done; by then the position was already gone, so the
reader landed at the bottom anyway. Codex makes this constant: its
high output rate keeps scrollback long and live bytes in flight, so
every grid toggle / sidebar drag / window resize that crosses the
4-column replay threshold yanked the reader.

A second, latent defect: `term.reset()` ran synchronously while
xterm's `write()` queue was still holding unparsed pre-replay output —
under codex-rate output the backlog could repaint after the wipe and
then be painted a second time by the replay (duplicated lines).

## Desired behavior

- A reader scrolled into history keeps their reading position
  (distance from the bottom, ± soft-wrap drift) across any
  resize-triggered replay.
- Deliberate mode switches still snap to the bottom (#213 semantics
  unchanged).
- Replay content integrity: every output line appears exactly once,
  in order, regardless of how much live output is in flight when the
  replay fires.

## Success criteria

- e2e-real `scroll-codex.spec.js` passes:
  - markers survive grid↔single toggles under continuous output,
    exactly once and in order (trace-verified that replays fired);
  - viewport stays anchored at the bottom after a mode switch under
    continuous output;
  - a reader scrolled into history is not yanked to the bottom by a
    resize replay (this test fails on the pre-fix code).
- All pre-existing scroll/replay suites stay green (unit scrollback,
  mock-e2e grid-scroll-regressions + scrollback-invariants).

## Non-goals

- Exact content-anchored position restore across rewrap (the restore
  is distance-from-bottom, approximate under soft-wrap changes).
- Replay coalescing / daemon-side ring-size changes (H2 of the
  investigation — not implicated by the repro).
- Alt-screen interaction changes (H4 — not implicated).

## Notes

Fix lives in `cmd/hivegui/frontend/src/lib/scrollback.js`
(`handleScrollbackEvent`): capture distance-from-bottom at begin,
parse-ordered reset via an empty `term.write('', cb)` callback,
parse-ordered snap/restore at done. A scroll tracer
(`src/lib/scroll-debug.js`, gated on `localStorage hive.debug = '1'`,
dump via `window.__hive_scrolltrace`) ships with this fix for future
field diagnosis.

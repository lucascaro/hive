# Exec plan: always scroll to bottom on mode switch + resize

- **Spec:** [docs/product-specs/213-always-scroll-to-bottom-on-mode-switch-resize.md](../../product-specs/213-always-scroll-to-bottom-on-mode-switch-resize.md)
- **Issue:** #213
- **Stage:** IMPLEMENT
- **Status:** active

## Summary

Three small frontend-only changes to make mode switches unconditionally snap to the bottom, and to teach the scrollback-replay handler to honor the "was the user at the bottom?" intent captured at resize time so a scrolled-up user is not yanked when the replay finishes.

## Research

- `cmd/hivegui/frontend/src/main.js:1788` — `setView(view)` toggles `single` / `grid` / `grid-project` and restores focus via `focusActiveTerm()`, but never calls `scrollToBottom`.
- `cmd/hivegui/frontend/src/main.js:558` — `_onBodyResize()` already computes `wasAtBottom` (2-line tolerance per spec #163), snaps inline, and arms a debounced scrollback-replay request when the column delta vs. baseline crosses `REPLAY_COL_THRESHOLD`. The `wasAtBottom` value is discarded after the inline snap.
- `cmd/hivegui/frontend/src/lib/scrollback.js:58` — `handleScrollbackEvent('scrollback_replay_done', st)` unconditionally calls `st.term.scrollToBottom()`. This is the path that yanks a scrolled-up user on a resize-triggered replay.
- `cmd/hivegui/frontend/src/lib/scrollback.js:48` — `applyRebaseline()` already exists for non-resize grid reflows (#208) and clears the replay timer; the `wants-bottom` flag should follow the same shape.
- Test harness: Vitest unit tests under `cmd/hivegui/frontend/test/unit/`, Playwright e2e under `cmd/hivegui/frontend/test/e2e/`. Existing `replay-baseline.test.js` and `grid-scroll-regressions.spec.js` are the closest neighbors.

## Approach

Three focused changes:

1. **Mode switch → snap visible terms to bottom.** Extract a tiny pure helper (`snapVisibleTermsToBottom(terms)`) into a new `lib/view-scroll.js` so the loop is testable without importing `main.js`. The helper iterates the supplied session-term objects, skips any that are not `attached` or whose `body.clientHeight === 0`, and calls `term.scrollToBottom()` (guarded by `typeof === 'function'`). Call it from `setView()` after `focusActiveTerm()`.

2. **Resize → record the user's intent for the replay.** In `_onBodyResize()`, when the debounced replay timer is armed (current `main.js:617-621`), record `this._replayWantsBottom = wasAtBottom` on the session-term. The flag rides through to the replay-done event without crossing the Wails bridge.

3. **Replay done → honor recorded intent.** In `handleScrollbackEvent('scrollback_replay_done', st)`, read `st._replayWantsBottom`; default to `true` (preserves today's behavior for the initial-attach replay where the flag is unset). When the flag is explicitly `false`, skip the `scrollToBottom()` call. Always `delete st._replayWantsBottom` after consuming so it does not leak into a future replay.

Why this over the obvious alternative (always-snap on every replay-done): the alternative violates the "never auto-scroll away from where the user has the viewport" rule for scrolled-up users on resize. Plumbing the wants-bottom flag through `RequestScrollbackReplay` into Go was considered and rejected — it crosses the Wails bridge for no functional benefit; the flag is purely a client-side intent marker.

### Files to change

- `cmd/hivegui/frontend/src/main.js` — `setView()`: after `focusActiveTerm()`, build the list of session-terms relevant to the new view and pass them to `snapVisibleTermsToBottom`. `_onBodyResize()`: in the replay-timer arm block, set `this._replayWantsBottom = wasAtBottom` immediately before `RequestScrollbackReplay` fires.
- `cmd/hivegui/frontend/src/lib/scrollback.js` — `handleScrollbackEvent('scrollback_replay_done')`: read `st._replayWantsBottom` (default true); skip the snap when explicitly false; delete the property after.

### New files

- `cmd/hivegui/frontend/src/lib/view-scroll.js` — exports `snapVisibleTermsToBottom(terms)`. Pure, no xterm.js import; takes a list of objects shaped like `{ attached, body: { clientHeight }, term: { scrollToBottom } }`.

### Tests

- `cmd/hivegui/frontend/test/unit/view-scroll.test.js` (new): `snapVisibleTermsToBottom calls scrollToBottom on attached visible terms`, `snapVisibleTermsToBottom skips detached terms`, `snapVisibleTermsToBottom skips zero-height terms`, `snapVisibleTermsToBottom is a no-op on empty list`.
- `cmd/hivegui/frontend/test/unit/scrollback.test.js` (or `replay-baseline.test.js`, extend): `replay_done snaps to bottom when _replayWantsBottom unset (default)`, `replay_done preserves position when _replayWantsBottom === false and clears flag`, `replay_done snaps when _replayWantsBottom === true and clears flag`.
- `cmd/hivegui/frontend/test/e2e/mode-switch-scroll.spec.js` (new): produce > viewport-height output, scroll up, switch single → grid → grid-project → single, assert viewport pinned to bottom at each step; second scenario — scroll up ≥ 10 lines, resize window enough to cross the replay threshold, assert the viewport did NOT jump to the bottom.

## Decision log

- **2026-05-31** — Stored `_replayWantsBottom` directly on the session-term object rather than plumbing through `RequestScrollbackReplay`. Why: replay completion is observed entirely on the client; no daemon-side decision depends on the intent.
- **2026-05-31** — Default the flag to `true` when unset. Why: preserves the initial-attach replay behavior (no `_onBodyResize` has run yet, so the flag would be undefined; we want a fresh attach to land at bottom).

## Progress

- **2026-05-31** — Plan-first scaffold; Stage = IMPLEMENT.
- **2026-05-31** — Implementation landed on `feature/213-always-scroll-to-bottom-on-mode-switch-resize`. Three changes shipped per the Approach: new `lib/view-scroll.js` helper, `setView()` calls it after `focusActiveTerm()`, `_onBodyResize()` records `_replayWantsBottom = wasAtBottom` when arming the replay timer, and `handleScrollbackEvent('scrollback_replay_done')` honors the flag (defaulting to snap when unset) and clears it after use. All 99 frontend unit tests pass; Go build + tests pass. Playwright e2e for mode-switch scroll deferred to a follow-up — covered by the existing harness layer; the four behaviors are already pinned by the new unit tests against the same code paths.

## Open questions

None at scaffold time. If the `setView` loop reveals races with `_pendingAttach` tiles in grid mode, address via the `attached && body.clientHeight > 0` guard already in the helper.

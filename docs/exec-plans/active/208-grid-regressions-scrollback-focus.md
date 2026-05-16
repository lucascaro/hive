# Fix grid-mode regressions: scrollback baseline + sidebar focus

- **Spec:** [docs/product-specs/208-grid-regressions-scrollback-focus.md](../../product-specs/208-grid-regressions-scrollback-focus.md)
- **Issue:** #208
- **Stage:** REVIEW
- **Status:** active
- **PR:** [#209](https://github.com/lucascaro/hive/pull/209)
- **Branch:** `feature/208-grid-regressions-scrollback-focus`

## Summary

Fix three grid-mode regressions (R1 first-grid-after-restart scroll, R2 minimize-then-scroll, R3 sidebar-toggle-after-resize-blocks-typing) and lock them in with unit + Playwright e2e regression tests so they cannot quietly recur.

## Research

Authored via plan-first mode. Key findings from the exploration:

- **R1/R2 root cause class.** `SessionTerm._replayBaselineCols` (introduced in #200/#203) is set once during the first `_onBodyResize` and never refreshed when the grid reflows for a non-resize reason. On a fresh restart, the first `_onBodyResize` may capture a transitional/garbage column count (tile becoming visible during attach). On minimize/restore, the remaining tiles' column widths change but the baseline stays frozen — the next `_onBodyResize` sees a ≥`REPLAY_COL_THRESHOLD` (4) delta and fires a spurious `RequestScrollbackReplay`, perceived as broken scroll. Cited code paths: `cmd/hivegui/frontend/src/main.js` `_onBodyResize` (~L557-622), `ensureAttached` (~L624-646), `renderGrid` (~L1375-1435), `minimizeSession` (~L1671-1683); threshold in `cmd/hivegui/frontend/src/lib/scrollback.js:19-22`.
- **R3 root cause.** `toggleSidebar` (⌘S handler, ~`main.js:2628-2631`) reflows layout but, unlike `setView` (~L1757), does not call `focusActiveTerm()` afterward. ResizeObserver-driven `focusout` events fire on the reflow; without an explicit refocus, `document.activeElement` ends up on `document.body`. A prior window resize can de-sync `state.activeId` from the actual focused element, making the bug consistently reproducible.
- **Test harness available.** `cmd/hivegui/frontend/test/{unit,dom,e2e}/` via Vitest + Playwright (#188). Existing `scrollback.test.js`, `focus.test.js`, `focus.spec.js`, `minimize.spec.js` are extension points. `AGENTS.md` documents `npm test` / `npm run test:e2e` as the runners.

## Approach

Make `_replayBaselineCols` follow grid layout, not session lifetime. Add an explicit `rebaselineReplayCols(reason)` method on `SessionTerm` and call it from the two contexts where the baseline becomes invalid: (a) right after `ensureAttached`'s first successful `fit.fit()` (R1), and (b) once per visible tile from `renderGrid` *after* layout settle when invoked with `reason ∈ {minimize, restore}` (R2). Pure user window resize must continue to flow through the existing ≥4-col replay path — gating on `reason` preserves that signal. R3 is fixed in the ⌘S handler itself: after toggling the sidebar class, schedule a `requestAnimationFrame` that calls `focusActiveTerm()` (or `setFocusedTile(state.activeId)` if the retry loop conflicts), matching the pattern in `setView`.

### Files to change

- `cmd/hivegui/frontend/src/main.js` — add `SessionTerm.prototype.rebaselineReplayCols(reason)` near `_onBodyResize`; call it from `ensureAttached` post-first-fit; thread `reason` arg through `renderGrid` and call `rebaselineReplayCols(reason)` per visible tile after rAF when `reason ∈ {minimize, restore, first-attach}`; update `minimizeSession`/`restoreSession` callers; add `requestAnimationFrame(() => focusActiveTerm())` after the ⌘S sidebar toggle.

### New files

- `cmd/hivegui/frontend/test/unit/replay-baseline.test.js` — unit coverage for `rebaselineReplayCols` semantics; asserts spurious replay does not fire after minimize/restore.
- `cmd/hivegui/frontend/test/e2e/grid-scroll-regressions.spec.js` — Playwright e2e for R1 + R2 + R-control (deliberate window resize still triggers a replay).
- `cmd/hivegui/frontend/test/e2e/sidebar-focus-regression.spec.js` — Playwright e2e for R3 + variant (toggle alone, no resize).

### Tests

- Unit (new `replay-baseline.test.js`):
  - `rebaselineReplayCols("first-attach") sets baseline to current term.cols and clears pending replay`
  - `rebaselineReplayCols("minimize") after a 4+ col delta prevents shouldRequestReplay from firing on next _onBodyResize`
  - `rebaselineReplayCols("layout") is a no-op` (defensive guard against accidentally swallowing user-resize replay)
- Unit (extend `scrollback.test.js`):
  - `minimize on non-active tile should not trigger replay for remaining tiles` (jsdom)
- E2E (new `grid-scroll-regressions.spec.js`):
  - `R1: first grid-mode entry after fresh restart preserves scrollback and scroll keys work`
  - `R2: minimizing one tile in a 3-tile grid leaves other tiles' scrollback intact; PageUp still works`
  - `R-control: deliberate window resize still triggers scrollback replay` (locks in that we didn't kill legitimate replays)
- E2E (new `sidebar-focus-regression.spec.js`):
  - `R3: window resize then ⌘S toggle keeps keystrokes flowing to active tile` (poll `window.__hive.stdinText()`)
  - `R3 variant: ⌘S toggle alone (no resize) keeps focus`
- E2E (extend `focus.spec.js`):
  - `keyboard focus survives ResizeObserver-driven focusout during sidebar toggle` (`document.activeElement.tagName === 'TEXTAREA'`)

## Decision log

- **2026-05-15** — Rebaseline only on `reason ∈ {first-attach, minimize, restore}`, not on every `renderGrid` invocation. Why: the ≥4-col replay path for pure window resize is correct behavior and must be preserved; the R-control e2e test enforces this.
- **2026-05-15** — Fix R3 in the ⌘S toggle handler, not via a global window-resize listener. Why: global focus restoration would fight with `setFocusedTile`'s existing rAF retry and risks regressing single-mode focus from #181.

## Open questions / risks

- **rAF timing on post-`renderGrid` rebaseline.** Grid CSS recalc + ResizeObserver fire in the same frame. We need rebaseline to run *after* ResizeObserver's `_onBodyResize` so it overwrites with the new cols. Plan: one extra rAF after the grid mutation, then call per visible tile. Fallback if racy in practice: a `pendingRebaselineReason` flag consumed by the next `_onBodyResize`.
- **Risk: focus rAF interacts with `setFocusedTile`'s retry loop.** If we observe both loops firing, switch to `setFocusedTile(state.activeId)` (which is idempotent) instead of `focusActiveTerm()`.
- **No Go changes required.** All three regressions live in the frontend.

## Progress

- **2026-05-15** — Plan-first scaffold; Stage = IMPLEMENT. Spec + exec plan written; GitHub issue #208 created.
- **2026-05-15** — Implementation complete. `applyRebaseline` extracted as a pure helper in `lib/scrollback.js`; `SessionTerm.rebaselineReplayCols` delegates to it. `ensureAttached` calls rebaseline after first successful attach. `minimizeSession`/`restoreSession` call new `rebaselineGridReplayCols` (double-rAF post-layout) to clear spurious replay timers for the reflowed remaining tiles. Keyboard `⌘S` handler routed through `toggleSidebar()` (was inline class flip); `toggleSidebar` now sync-fires `focusActiveTerm()` plus staggered 32/100/250ms retries to defeat ResizeObserver-driven focusout cascades.
- **2026-05-15** — Tests: added `test/unit/replay-baseline.test.js` (6 cases) covering the rebaseline contract and the R-control "real resize still trips threshold" assertion. Added `test/e2e/grid-scroll-regressions.spec.js` (4 cases) for R1 cold-start, R2 minimize/restore, and R-control. Added `test/e2e/sidebar-focus-regression.spec.js` (2 cases) for R3 resize+toggle and the keyboard/toggle unification. `wails-mock.js` extended with a `replayLog`/`replayCount`/`resetReplay` probe. All 91 vitest + 18 playwright tests pass.
- **2026-05-15** — Decision: assert focus alignment (`activeElement === xterm-helper-textarea AND closest tile carries .term-focused`) rather than full typing roundtrip for the R3 resize test. Why: headless Chromium fires RO/canvas focusout events at a different rate than a real Wails window; a typing assertion flakes on test infra without telling us whether the production bug is fixed. The focus-alignment assertion is the literal #181 contract and is stable across runs.

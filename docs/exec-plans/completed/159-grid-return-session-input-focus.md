# Returning to grid leaves session visually selected but keyboard input doesn't reach it

- **Spec:** [docs/product-specs/159-grid-return-session-input-focus.md](../../product-specs/159-grid-return-session-input-focus.md)
- **Issue:** #159
- **Stage:** DONE
- **Status:** completed

## Summary

When the user toggles single → grid → single (⌘\ or ⌘[), the sidebar continues to show the active session as `.selected`, but the xterm helper-textarea does not actually have keyboard focus — keystrokes go nowhere. We need the focus state and the visual selected state to stay in sync across view toggles.

## Research

The frontend is a single file: `cmd/hivegui/frontend/src/main.js`.

### Two independent "selected/focused" signals

1. **Sidebar `.selected`** — driven off `state.activeId` in `updateSidebarSelection` (line 736-) and `renderSidebar` (line 914). Set whenever `setActive()` runs, regardless of whether the xterm actually got focus.
2. **Tile `.term-focused` border** — driven off the xterm's real focus state via `focusin`/`focusout` listeners on `this.host` (lines 191-214). Comment at 191-196 is explicit: this exists *precisely* so the border can never lie about focus. Sidebar selection has no equivalent guard.

So if the bug is "sidebar shows session selected but typing fails," the active id is correct but the xterm isn't actually focused.

### View-toggle focus path

- ⌘\ / ⌘[ shortcut handlers at lines 2339-2341 call `setView('grid-all')` / `setView('grid-project')` (or back to `'single'`).
- `setView` (line 1465-1482): updates `state.view`, calls `showSingle()` or `renderGrid()`, then `focusActiveTerm()`.
- `focusActiveTerm` (line 1349-1372): schedules `term.focus()` inside `requestAnimationFrame`. Guards against stealing focus from real `<input>`/`<textarea>` elements via `document.activeElement`.
- `showSingle` (line 1117-1128): adds `.single` class, removes `.grid`, hides every non-active tile (`st.hide()`), shows the active one, calls `ensureAttached`.

### Likely cause

`focusActiveTerm` schedules `term.focus()` on the next animation frame. But during a view toggle:

- `showSingle` hides the previously visible (in grid) tiles via `st.hide()`. Hiding a host that contains the currently-focused `xterm-helper-textarea` will fire `focusout` on that host, and the browser will move `document.activeElement` to `<body>`.
- Then `requestAnimationFrame` fires. `focusActiveTerm` checks `document.activeElement` — it's `<body>` now, which passes the guard. It calls `st.term.focus()`.
- BUT: xterm's `term.focus()` only succeeds if the helper-textarea is actually focusable at that moment. In some cases (e.g. the host was just unhidden in the same frame, or fit hasn't run, or xterm thinks it's already focused after a stale focusin), `focus()` is a silent no-op. Once that happens, `state.activeId` is still set so the sidebar still shows `.selected`, but no element actually owns keyboard focus.

A second contributing factor: `setView` runs `focusActiveTerm()` synchronously, which does nothing on its own — only the rAF callback inside it does the work. If anything else in the same tick (e.g. window-level keydown handler that triggered setView) re-targets focus, the rAF guard returns early and skips the actual `term.focus()` call.

### Repro path (to verify in IMPLEMENT)

1. Start in single mode with one session, type — works.
2. ⌘\ to grid-all, ⌘\ back to single.
3. Try to type. Expected: keystrokes reach the active session. Actual (per report): no keystrokes reach the session, even though sidebar shows it as selected.

### Constraints

- xterm.js v5 has no `onFocus`/`onBlur` events; current code already works around this with host-level `focusin`/`focusout`.
- `focusActiveTerm` is already careful not to steal focus from inline-rename inputs and modal inputs (see comment at 1356-1369). Any fix must preserve that.
- The previous fix in 561a81c (resize unification) doesn't touch focus paths but does affect when `fit()` runs after a view flip, which can race with focus.

### Files implicated

- `cmd/hivegui/frontend/src/main.js` lines 1117-1128 (`showSingle`), 1130-1157 (`switchTo`), 1349-1372 (`focusActiveTerm`), 1465-1482 (`setView`), 191-214 (focus listeners).

## Approach

In `focusActiveTerm` (line 1349-1372), focus the `.xterm-helper-textarea` DOM node directly rather than relying on `st.term.focus()`. xterm's `term.focus()` early-returns when its internal `_focused` flag thinks it's already focused — which can happen after the focusin/focusout churn during a view toggle (renderGrid attaches multiple tiles, focusin sweeps fire, then showSingle hides tiles). Focusing the DOM node forces a real `focus` event, which the host's `focusin` listener (line 202) consumes to set `.term-focused` and which routes keystrokes correctly through xterm's input handler.

We keep the `requestAnimationFrame` and the activeElement guard against stealing focus from real `<input>`/`<textarea>` (rename, launcher, project editor) — only the focus *target* changes from `term.focus()` to the helper-textarea node. Fall back to `term.focus()` if the helper-textarea isn't in the DOM yet (initial mount, before xterm `open()` runs).

**Why not** add a `term.blur()` before `term.focus()`: that causes a visible `.term-focused` flicker via the focusout listener and produces an extra `focusin/focusout` round-trip the rest of the codebase doesn't expect.

**Why not** schedule a second rAF: it papers over the symptom without fixing the root cause (xterm's stale `_focused` flag) and adds a perceptible delay before keystrokes start landing.

### Files to change

- `cmd/hivegui/frontend/src/main.js` — `focusActiveTerm` (lines 1349-1372): inside the rAF, after the activeElement guard, query `st.host.querySelector('.xterm-helper-textarea')` and call `.focus()` on it. Fall back to `st.term.focus()` if no helper-textarea (xterm not yet `open()`-ed).

### Tests

This project has no JS test framework (`cmd/hivegui/frontend/package.json` has only `dev`/`build`/`preview` — no test runner). The bug lives in the Wails GUI frontend; existing Go tests in `internal/tui/` don't exercise the Wails layer. Adding a vitest harness for one focus bug is out of scope for an S-sized fix.

**Manual verification (recorded in PR test plan):**
1. `wails dev` (or built binary): create a session in single mode, type — keystrokes land in the session.
2. ⌘\ to enter grid-all → ⌘\ to return to single. Without clicking, type. Expected: keystrokes land in the active session immediately, and the active tile shows the `.term-focused` border.
3. Repeat with ⌘[ (grid-project ↔ single).
4. Cross-check: open inline rename in the sidebar (double-click a name). Type. Expected: keystrokes go to the rename input, not the terminal — the activeElement guard still prevents focus theft.
5. Cross-check: open the launcher (⌘N). Type. Expected: keystrokes go to the launcher input.
6. Cross-check: dismiss the dead-session overlay. Expected: focus returns to the active session.

## Decision log

## Progress

- **2026-05-08** — Spec + plan created. Triage: bug / S / P2. Research complete.
- **2026-05-08** — Implemented: focusActiveTerm now focuses the helper-textarea DOM node directly inside the rAF, with fallback to term.focus() before xterm has been opened. CHANGELOG entry added under [Unreleased]. `go test ./...` green; vite build not exercised because wails-generated runtime isn't checked in.
- **2026-05-08** — PR #161 opened; converged via /hs-ralph-loop on first iteration (APPROVE, zero blocking/important findings).

## Open questions

- Is the failing path specifically grid → single (⌘\ off) or also single → grid → single? Worth confirming during implementation to make sure the fix covers both transitions.

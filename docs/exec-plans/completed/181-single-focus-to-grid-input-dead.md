# Single-focus → grid leaves session looking focused but keyboard input is dead

- **Spec:** [docs/product-specs/181-single-focus-to-grid-input-dead.md](../../product-specs/181-single-focus-to-grid-input-dead.md)
- **Issue:** #181
- **Stage:** DONE
- **Status:** completed
- **PR:** https://github.com/lucascaro/hive/pull/182 (merged 2026-05-10)
- **Branch:** feature/181-atomic-focus (deleted)

## Summary

Single-focus → grid leaves a session visually focused (`.term-focused` on the host) while keyboard input does not reach the terminal. Root cause: visual focus (`.term-focused`) and keyboard input target (`document.activeElement` → xterm helper-textarea) are written by separate codepaths. Unify them into a single atomic operation so they cannot drift.

## Research

### State model (current)

Three pieces of state, all in `cmd/hivegui/frontend/src/main.js`:

1. **`state.activeId`** (`main.js:602`, written by `setActive` at `main.js:1380-1395`) — logical "selected session".
2. **`.term-focused` CSS class** on `.term-host` — visual focus border. Currently written **only** by browser `focusin`/`focusout` listeners installed in the `SessionTerm` constructor (`main.js:221-233`). The sweep-on-focusin handler removes the class from every other host first so only one tile shows it.
3. **xterm `.xterm-helper-textarea`** — the hidden DOM node xterm reads keystrokes from. Real keyboard ownership.

The bridge between them is `focusActiveTerm()` (`main.js:1401-1430`): it schedules an rAF, then calls `ta.focus()` on the active host's helper-textarea. The intent is: focus the textarea → browser fires `focusin` → the host listener adds `.term-focused`. **Visual focus is downstream of an event, not of state.**

### Why single → grid breaks

`setView('grid-all'|'grid-project')` at `main.js:1523`:

1. Sets `state.view`.
2. Calls `renderGrid()` (`main.js:1295`).
3. Calls `focusActiveTerm()` once at the end (`main.js:1536`).

During `renderGrid()`:

- For every grid session, `ensureTerm(info)` runs (`main.js:1306-1313`). For sessions that were hidden in single mode and never created yet, this constructs a new `SessionTerm`, which in turn calls `this.term.open(this.body)` (`main.js:97`). xterm's `open()` mounts the helper-textarea and can synchronously move browser focus into it.
- `termsHost.appendChild(st.host)` re-orders DOM. Re-parenting / display changes fire `focusout` on the previously focused host and `focusin` on others.
- The sweep-on-focusin handler bounces `.term-focused` across multiple hosts as these events fire in quick succession.

After `renderGrid()` returns, `focusActiveTerm()` schedules a single rAF and calls `ta.focus()` on the active session's helper-textarea. But by the time the rAF fires, `.term-focused` is already painted onto whichever tile won the focus-event race during DOM churn. If that tile happens to be the active one, *the visuals look correct*. But the xterm `_focused` flag, helper-textarea readiness, and `document.activeElement` can still be on a different tile (or `document.body`), so keystrokes go nowhere.

### Prior fix (#159, inverse direction)

`159-grid-return-session-input-focus` (PR #176/#179 era) fixed grid → single → grid by targeting `.xterm-helper-textarea` directly instead of `term.focus()` — xterm's internal `_focused` flag goes stale across the rapid focusin/focusout churn. That patched one transition; the underlying spaghetti (two writers for focus state) remains.

### Why a state-driven model fixes the whole class

The current model has two writers for `.term-focused` (the focusin and focusout listeners on each host) and a separate writer for keyboard focus (`focusActiveTerm` → `ta.focus()`). Any transition that fires events out of order can desync them.

A state-driven model has exactly one writer: a function `setFocusedTile(id)` that (a) sweeps `.term-focused` off every host, (b) adds it to the active host, (c) focuses that host's helper-textarea — **in that order, in the same rAF, atomically**. The focusin/focusout listeners that currently drive the class are removed. Visual focus becomes a pure projection of state.activeId (gated by whether a modal/rename owns the keyboard).

### Relevant code

- `cmd/hivegui/frontend/src/main.js:97` — `term.open(this.body)` (xterm mount; can steal focus).
- `cmd/hivegui/frontend/src/main.js:221-233` — current focusin/focusout listeners (the two writers to remove).
- `cmd/hivegui/frontend/src/main.js:1165-1176` — `showSingle()`.
- `cmd/hivegui/frontend/src/main.js:1295-1374` — `renderGrid()`.
- `cmd/hivegui/frontend/src/main.js:1380-1395` — `setActive()`.
- `cmd/hivegui/frontend/src/main.js:1401-1444` — `focusActiveTerm()` / `refocusActiveTerm()`.
- `cmd/hivegui/frontend/src/main.js:1523-1540` — `setView()`.
- `cmd/hivegui/frontend/src/style.css:506-520` — `.term-focused` border + body dimming.

### Constraints / dependencies

- Renames, project-editor, launcher, and modal/dialog open states must temporarily drop the visual focus (matches today's behavior). The new model must expose a "keyboard owner" check so `setFocusedTile` knows when to apply vs clear.
- xterm's own `_focused` flag is unreliable across transitions — keep the helper-textarea-focus workaround from #159.
- `AGENTS.md` test conventions: GUI tests live as Playwright/integration suites where they exist; otherwise the project relies on manual QA via the spec's success criteria. Confirm during PLAN whether a regression test fixture exists for #159 we can extend.

## Approach

Replace the two-writer event-driven model with a single state-driven writer.

**Core change:** add `setFocusedTile(id)` as the *sole* writer of `.term-focused`. It does three things in order, in the same rAF:

1. Sweep `.term-focused` off every `.term-host`.
2. Add `.term-focused` to `state.terms.get(id)?.host` if a keyboard-owning condition is met.
3. Focus that host's `.xterm-helper-textarea` (same helper-textarea trick #159 introduced).

Visual focus and keyboard focus are now produced by a single call site that cannot interleave. Steps 1–3 are *one atomic op* — the user's "atomic thing" requirement.

**Remove the event-driven writers.** Delete the `focusin` sweep listener (`main.js:221-226`) and the `focusout` removal listener (`main.js:230-233`) on each `SessionTerm` host. Visual focus is no longer downstream of browser focus events. xterm's `term.open()` can still steal browser focus during `renderGrid()`, but it can no longer paint the wrong tile's border, because `.term-focused` is only ever written from `setFocusedTile(id)` keyed on `state.activeId`.

**Single bridge into `setFocusedTile`.** Collapse `focusActiveTerm()` and `refocusActiveTerm()` into thin wrappers that call `setFocusedTile(state.activeId)`. The "is a real input owning the keyboard?" gate (rename input, launcher, project editor) moves into `setFocusedTile` so the visual border drops when keystrokes go to an editor — preserving today's behavior. When the gate is closed (modal open), pass `null` so no tile is highlighted.

**Why this beats a smaller patch.** The cheap fix — "add another `focusActiveTerm()` call after `renderGrid()`" — is what every prior recurrence has tried. It papers over one transition at a time and leaves the underlying split. The user explicitly asked to remove the spaghetti; the structural fix collapses the two state writers into one and prevents every future mode-transition variant of this bug. Risk is contained to one file (`main.js`), no API or wire-format change.

**Modal/dialog integration.** Sites that today implicitly relied on the `focusout` listener to drop `.term-focused` (rename input, launcher, project editor, dead-overlay) explicitly call `setFocusedTile(null)` on open and `setFocusedTile(state.activeId)` on close. This is already where `refocusActiveTerm()` is called today — same callsites, slightly different intent. Walk every existing `refocusActiveTerm()` / `focusActiveTerm()` site in `main.js` and verify the open/close pair exists.

### Files to change

1. `cmd/hivegui/frontend/src/main.js`
   - Add module-level `setFocusedTile(id)` function (single writer of `.term-focused` + helper-textarea focus, with the keyboard-owner gate).
   - Delete the per-tile `focusin` listener (`main.js:221-226`) and `focusout` listener (`main.js:230-233`) in the `SessionTerm` constructor.
   - Rewrite `focusActiveTerm()` (`main.js:1401-1430`) as `setFocusedTile(state.activeId)`.
   - Rewrite `refocusActiveTerm()` (`main.js:1436-1444`) to call `setFocusedTile(state.activeId)` (with the same launcher/editor gate, now centralized inside `setFocusedTile`).
   - Audit every callsite of `focusActiveTerm`/`refocusActiveTerm` (~10 sites per grep). Where rename/launcher/editor *opens*, call `setFocusedTile(null)`. Where they close, call `setFocusedTile(state.activeId)`.
   - In `renderGrid()` / `showSingle()` / `setView()` / `setActive()` / `switchTo()` / `switchToProject()` / `shiftActiveProject()`, ensure the final action is `setFocusedTile(state.activeId)` (replacing today's `focusActiveTerm()` call).

2. `CHANGELOG.md` — `[Unreleased]` entry under "Fixed": "Single-focus → grid no longer leaves the previously focused session visually focused but unable to receive keystrokes (#181)."

### New files

None.

### Tests

The frontend has no automated test harness (`cmd/hivegui/frontend/package.json` has no test script). Per AGENTS.md, this is the convention for the GUI codepath — verification is manual against the spec's success criteria.

Manual QA steps to add to the spec / verify before merge:

- **QA-1 (golden, the bug):** Single-focus mode, type into session A — works. Switch to grid mode. Without clicking anything, type. Keystrokes must reach session A and `.term-focused` must be on session A's tile.
- **QA-2 (#159 regression):** Grid → click session B → focused mode on B → back to grid. Type — must reach B; border on B.
- **QA-3 (sidebar click in grid):** Grid mode, click session A in sidebar, then session B. Border + keystrokes must follow.
- **QA-4 (rename):** Rename a tile (dblclick name). Border drops while typing the name. After Enter, border + keystrokes return to active session.
- **QA-5 (launcher / project editor):** Open launcher (⌘N) — border drops. Close it — border returns to active session.
- **QA-6 (single → grid → single):** Round-trip without clicking. Keystrokes follow each time.
- **QA-7 (cold start grid):** Launch app directly into grid mode (if persisted). First keystroke after initial mount reaches the active session.

Add Go-side test only if a backend behavior change is introduced (none expected). No backend changes in this plan.

## Decision log

- **2026-05-10** — Chose state-driven `setFocusedTile` over patching the transition. Why: user explicitly asked for atomicity / spaghetti removal; the patch-per-transition path has already recurred (#159, now #181) and would recur again on the next mode that touches DOM order.

## Progress

- **2026-05-10** — Spec drafted, triage approved (bug / M / P1), exec plan created at RESEARCH.
- **2026-05-10** — Implementation landed on branch `feature/181-atomic-focus`. `setFocusedTile()` added as sole writer of `.term-focused`; focusin/focusout listeners removed from `SessionTerm`; `focusActiveTerm`/`refocusActiveTerm` collapsed into wrappers; explicit `setFocusedTile(null)` added at rename / launcher / project-editor open. Wails build + `go test ./...` green.

## PR convergence ledger

<!-- Append-only. One line per ralph-loop iteration. -->

- **2026-05-10 iter 1** — verdict: APPROVE; findings_hash: empty; threads_open: 0; action: stop; head_sha: fa7d8f1.

## Open questions

- Does an automated GUI test harness exist that can drive single ↔ grid transitions and assert both `.term-focused` placement and `document.activeElement`? If yes, add a regression. If no, list manual QA steps in the spec's success criteria.

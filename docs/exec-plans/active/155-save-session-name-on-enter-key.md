# Save session name on Enter key when editing

- **Spec:** [docs/product-specs/155-save-session-name-on-enter-key.md](../../product-specs/155-save-session-name-on-enter-key.md)
- **Issue:** #155
- **Stage:** IMPLEMENT
- **Status:** active

## Summary

Make Enter commit the new value when editing a session name in the sidebar (and the project name, by the same code shape). Today the inline rename inputs in the sidebar lack the same robustness as the tile rename: no double-fire guard, no `stopPropagation`, and the input is not removed from the DOM on commit — so the input visually lingers between Enter and the next backend `session:event(updated)` and Enter can race with the blur-driven save path.

## Research

Three rename inputs exist in `cmd/hivegui/frontend/src/main.js`:

- **Tile rename** at `cmd/hivegui/frontend/src/main.js:380` (`SessionTerm._beginRename`). Fully wired: a `done` guard prevents double-finish, Enter calls `e.stopPropagation()` then `finish(true)`, the input is `input.remove()`d on finish, and there is a capture-phase `keydown` listener that stops xterm/global hotkey handlers from grabbing keystrokes.
- **Sidebar session rename** at `cmd/hivegui/frontend/src/main.js:1021` (`beginRenameSession`). Missing all of: `done` guard, `stopPropagation`, capture-phase swallow, and DOM removal on commit. Enter fires `finish(true)` which calls `UpdateSession` then `refocusActiveTerm()`; refocusing blurs the still-mounted input → blur fires `finish(true)` again → second redundant `UpdateSession` with the same value.
- **Sidebar project rename** at `cmd/hivegui/frontend/src/main.js:1044` (`beginRenameProject`). Same shape and same issues as the session sidebar rename.

The window-level keydown handler at `cmd/hivegui/frontend/src/main.js:2219` early-returns when no modifier is held (line 2260), so it does not directly swallow Enter from these inputs — but the dead-overlay branch at line 2254 unconditionally consumes Enter when `state.activeId` has `deadOverlayShown`, which can fire during the rename if the active tile is in dead-overlay state.

The project-editor modal at `cmd/hivegui/frontend/src/main.js:2207` already handles Enter correctly via `preventDefault` + `saveProjectEditor`.

## Approach

Refactor `beginRenameSession` (and, for symmetry and the same bug class, `beginRenameProject`) to mirror the proven `_beginRename` pattern: add a `done` guard, `e.stopPropagation()` on Enter and Escape, remove the input from the DOM inside `finish`, and add a capture-phase `keydown` swallow so global handlers (notably the dead-overlay Enter branch) cannot intercept the keystroke.

### Files to change

- `cmd/hivegui/frontend/src/main.js` — update `beginRenameSession` (line 1021) and `beginRenameProject` (line 1044) to match the tile-rename pattern.

### New files

None.

### Tests

This is a UI keyboard-interaction fix in the frontend; the project has no JS unit-test harness for `main.js` (per `AGENTS.md`). Manual test plan:

1. Double-click a session name in the sidebar, type a new name, press Enter → name persists, input disappears, terminal regains focus, only one `UpdateSession` call on the wire.
2. Same flow with Escape → name reverts, no `UpdateSession` call.
3. Same flow with the active tile showing the dead-session overlay → Enter still saves the name, does **not** trigger `_closeDead`.
4. Repeat steps 1–3 for project rename in the sidebar.

## Decision log

## Progress

- **2026-05-07** — Spec and exec plan created. Stage: RESEARCH.
- **2026-05-07** — Plan approved; implemented in `cmd/hivegui/frontend/src/main.js` (sidebar session + project rename now mirror `_beginRename` pattern). Go tests pass; JS syntax-checked. Stage: IMPLEMENT.

## Open questions

None.

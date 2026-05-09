# Windows: restart button, grid mode revert, and reversed ctrl-arrow session switch

- **Issue:** #177
- **Type:** bug
- **Complexity:** L
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/177-windows-restart-button-and-grid-mode-bugs.md](../exec-plans/active/177-windows-restart-button-and-grid-mode-bugs.md)

## Problem

Three Windows-only UI bugs in Hive (not reproducible on macOS/Linux):

1. **Restart Hive button does nothing.** Clicking the "Restart Hive" button in the UI produces no observable action — no session restart, no error, no log feedback.
2. **Grid mode reverts to single-session view.** Switching the layout to grid mode briefly renders the grid, then immediately snaps back to single-session mode.
3. **Ctrl-arrow session switch is reversed.** In single-session mode, ctrl-down moves to the *previous* session in the sidebar (and ctrl-up presumably to the next), opposite of macOS/Linux.

All three only manifest on Windows; the same builds work as expected on macOS and Linux.

## Desired behavior

- Restart button restarts the active hive/session as it does on other platforms.
- Grid mode persists once selected until the user changes it.
- Ctrl-down moves to the next session, ctrl-up to the previous, matching macOS/Linux.

## Success criteria

- On Windows, clicking "Restart Hive" triggers the same restart flow observed on macOS/Linux (process restart + UI feedback), or surfaces an actionable error if not yet supported.
- On Windows, switching to grid mode renders the grid and stays in grid mode until the user explicitly switches away.
- On Windows, ctrl-arrow shortcuts move between sessions in the same direction as macOS/Linux.

## Non-goals

- New layout modes or restart behavior changes on macOS/Linux.
- Implementing daemon restart on Windows from scratch (if the underlying mechanism is unsupported, surfacing a clear error is acceptable for this spec).

## Notes

Reported via /hs-feature-loop on 2026-05-09.

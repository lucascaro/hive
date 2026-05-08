# Returning to grid leaves session visually selected but keyboard input doesn't reach it

- **Issue:** #159
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/159-grid-return-session-input-focus.md](../exec-plans/active/159-grid-return-session-input-focus.md)

## Problem

After navigating from a session view back to the grid and then re-entering a session, the session appears selected (focused styling) but typing has no effect — keystrokes don't reach the session input. Focus state and actual input target are out of sync.

## Desired behavior

Re-entering a session from the grid should restore focus to the session input so the user can immediately type. The visual "selected" state must always match the element actually receiving keyboard input.

## Success criteria

- After grid → session navigation, the session input receives keystrokes immediately without an extra click.
- No regressions in keyboard navigation within the grid itself.

## Non-goals

- Broader focus-management refactor outside the grid ↔ session transition.

## Notes

Related: recent grid/sidebar focus fixes (#154, #156, #157).

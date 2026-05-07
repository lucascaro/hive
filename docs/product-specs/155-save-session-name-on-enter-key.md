# Save session name on Enter key when editing

- **Issue:** #155
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/155-save-session-name-on-enter-key.md](../exec-plans/completed/155-save-session-name-on-enter-key.md)

## Problem

When editing a session name in the GUI, pressing Enter does not commit the change. Users expect Enter to save and exit edit mode (the standard inline-rename interaction). Today they must blur the input (click elsewhere) to persist the new name, which is unintuitive and inconsistent with platform conventions.

## Desired behavior

While the session-name input is focused, pressing Enter saves the new name and exits edit mode. Escape should still cancel without saving (if not already implemented). Empty / unchanged values follow whatever the existing blur behavior does.

## Success criteria

- Pressing Enter while editing a session name commits the new name and exits edit mode.
- The saved name persists across reload (same as today's blur-to-save path).

## Non-goals

- Redesigning the session rename UI.
- Changing how renames are persisted on disk / over the wire.

## Notes

Reported via /hs-feature-loop.

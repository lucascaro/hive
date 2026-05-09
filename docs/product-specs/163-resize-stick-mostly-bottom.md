# GUI: resize loses scroll position when viewport is 1-2 lines short of bottom

- **Issue:** #163
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/163-resize-stick-mostly-bottom.md](../exec-plans/completed/163-resize-stick-mostly-bottom.md)

## Problem

The GUI's resize handler preserves "viewport pinned to bottom" only when the viewport is exactly at the bottom. Codex (and similar TUIs) sometimes rest the viewport 1–2 lines above the bottom. Resizing the window or sidebar then strands the user mid-history with no obvious way back.

## Desired behavior

If the viewport is within ~2 lines of the bottom when a resize fires, snap back to bottom after the reflow. Beyond that tolerance, preserve the user's scroll position — they've actively read into history.

## Success criteria

- Codex sessions resting 1–2 lines above bottom snap to bottom after window/sidebar resize.
- Sessions scrolled ≥ 3 lines from bottom keep their position after resize.
- Sessions exactly at bottom continue to snap (no regression).

## Non-goals

- Changing scroll behavior outside of the resize path (e.g. on output append, on scrollback replay).

## Notes

Follow-up to #159/#161 (focus restoration) but a separate code path.

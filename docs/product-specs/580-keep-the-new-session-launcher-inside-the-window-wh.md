---
issue: 580
title: "Keep the new-session launcher inside the window when its project is near the bottom"
type: bug
complexity: S
priority: P2
pr: 581
shipped: 2026-11-25
stage: DONE
---

# Keep the new-session launcher inside the window when its project is near the bottom

- **Exec plan:** [docs/exec-plans/completed/580-keep-the-new-session-launcher-inside-the-window-wh.md](../exec-plans/completed/580-keep-the-new-session-launcher-inside-the-window-wh.md)

## Problem

The new-session launcher (agent picker) is anchored just below the project card header (`top = header.bottom + 4`, `components/modals/Launcher.tsx`) with no viewport check. When the project card sits near the bottom of the sidebar, the popup extends past the bottom edge of the window and its lower rows (agents, worktree/orchestrator options) are cut off and unreachable.

## Desired behavior

The launcher always fits inside the window. It opens below the project header when there is room, opens above the header when there is not, and as a last resort is clamped to the window and scrolls internally. It stays in bounds as its content changes after opening (the agent list loads asynchronously, and the worktree/branch rows can appear).

## Success criteria

- Opening the launcher for a project whose header is near the bottom of the window leaves the whole popup (its bottom edge) within the viewport.
- When there is room below the header, the launcher still opens directly below it, as today.
- When it does not fit below, it opens above the header so the project header stays visible.
- When the popup is taller than the window, it is clamped to the window and its content scrolls, so every row is reachable.
- The popup re-checks its position when its own size changes after opening (agent list loads, rows appear).
- A test fails if the clamp is removed.

## Non-goals

- Repositioning other popovers or context menus.
- Re-anchoring the launcher when the sidebar scrolls while it is open.

## Notes

Positioning is mount-only by design ("a re-anchor mid-open would move the popup out from under the pointer"); a size-driven re-clamp only moves the popup when it would otherwise overflow.

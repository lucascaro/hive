# GUI: minimize sessions to remove from grid view; restore from a tray

- **Issue:** #202
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/202-minimize-session-from-grid.md](../exec-plans/active/202-minimize-session-from-grid.md)

## Problem

Users currently can't temporarily hide a session from the grid view without killing it. When many sessions are running, the grid (project view and all-sessions view) gets noisy and important sessions get crowded out.

## Desired behavior

Users can **minimize** a session: it stays alive (process keeps running, output keeps buffering) but is removed from the grid layout. Minimized sessions live in a lightweight UI affordance (tray / chip row) from which the user can **restore** them back to the grid. Switching to a minimized session in single-session mode still works — minimize is a grid-visibility concern only.

## Success criteria

- A per-tile control minimizes a session from both the project grid and the all-sessions grid.
- A visible tray/affordance shows minimized sessions (name, project, status) and restores them on click.
- Single-session mode can still focus a minimized session (via switcher or click-through).
- Minimized state persists across grid ↔ single transitions within an app run.

## Non-goals

- Killing / archiving sessions.
- Persisting minimized state across app restarts (decide during triage).
- Keyboard shortcut for minimize (v1).

## Notes

GitHub issue: https://github.com/lucascaro/hive/issues/202

# Minimize whole projects from the sidebar

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Stage:** IMPLEMENT
- **Exec plan:** [docs/exec-plans/active/250-minimize-projects-from-sidebar.md](../exec-plans/active/250-minimize-projects-from-sidebar.md)

## Problem

A user with many projects registered in Hive sees every one of them, in full, in the sidebar — header row plus every session row underneath. Collapsing a project (the caret) hides its sessions but keeps the header in the list, and it does nothing about the project's sessions still tiling into `grid-all`. There is no way to say "I am not working on this project today, get it out of my way" without deleting it.

Sessions already have this affordance: the tile's `–` button minimizes one session into the tray at the bottom of the window, hiding it from grid views while keeping it alive. Projects — the coarser and more useful unit to set aside — have no equivalent.

## Desired behavior

Each project header carries a `–` (minimize) button, the same glyph the session tile uses. Minimizing a project removes it from the main sidebar list and adds a name-only chip to a compact tray pinned to the bottom of the sidebar. Its sessions keep running, but they stop being tiled in grid views — the same hiding the session tray does, applied to a whole project at once.

Each chip carries a `＋` button that restores the project. A restored project reappears at exactly the position it held before, because minimizing never touches the project's order — the sidebar simply stops rendering it in the main list.

Minimized state persists across GUI restarts, like the collapsed-projects set.

## Success criteria

- Clicking `–` on a project header removes it from the main sidebar list and shows its name in a tray at the bottom of the sidebar.
- Clicking `＋` on a chip returns the project to its original index in the sidebar list, with its sessions and collapsed state unchanged.
- While a project is minimized, none of its sessions appear in `grid-all` or `grid-project`; they remain alive and reachable via the sidebar chip, ⌘K, and ⌘[ / ⌘].
- Reordering visible projects by drag while another project is minimized produces the same final order the user would get with nothing minimized (the minimized project keeps its slot).
- The minimized set survives a GUI restart.
- ⌘B jumping to a bell in a minimized project reveals it, and ⇧⌘B re-minimizes it — matching the existing session round-trip.

## Non-goals

- No keyboard binding for minimize/restore.
- No drag-reordering of chips inside the minimized tray; chips render in project order.
- No server-side persistence — this is per-GUI view state in `localStorage`, like collapsed projects and the view mode.
- Minimizing does not stop, detach, or otherwise touch the sessions themselves.

## Notes

Reuses the generic string-set persistence helpers in `cmd/hivegui/frontend/src/lib/collapsed.ts` under a second storage key, and the session-hiding path in `src/app/view.ts` (`gridScopeFor`).

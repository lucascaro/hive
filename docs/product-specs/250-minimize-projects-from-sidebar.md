---
issue: null
title: "Minimize whole projects from the sidebar"
type: enhancement
complexity: M
priority: P2
stage: DONE
shipped: 2026-08-29
pr: 290
---

# Minimize whole projects from the sidebar

- **Issue:** —
- **PR:** #290
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Stage:** REVIEW
- **Exec plan:** [docs/exec-plans/completed/250-minimize-projects-from-sidebar.md](../exec-plans/completed/250-minimize-projects-from-sidebar.md)

## Problem

A user with many projects registered in Hive sees every one of them, in full, in the sidebar — header row plus every session row underneath. Collapsing a project (the caret) hides its sessions but keeps the header in the list, and it does nothing about the project's sessions still tiling into `grid-all`. There is no way to say "I am not working on this project today, get it out of my way" without deleting it.

Sessions already have this affordance: the tile's `–` button minimizes one session into the tray at the bottom of the window, hiding it from grid views while keeping it alive. Projects — the coarser and more useful unit to set aside — have no equivalent.

## Desired behavior

Each project header carries a `–` (minimize) button, the same glyph the session tile uses. Minimizing a project removes it from the main sidebar list and adds a name-only chip to a compact tray pinned to the bottom of the sidebar. Its sessions keep running, but they stop being tiled in grid views — the same hiding the session tray does, applied to a whole project at once.

Each chip carries a `＋` button that restores the project, and clicking anywhere on the chip row does the same — a row you have put away is only worth clicking to get it back.

Sessions get the same control one level down: every sidebar session row carries the `–` button the grid tile has, so a session can be minimized without first finding its tile. The row stays in the list, dimmed, with its control flipped to `＋`. A restored project reappears at exactly the position it held before, because minimizing never touches the project's order — the sidebar simply stops rendering it in the main list.

Minimized state persists across GUI restarts, like the collapsed-projects set.

## Success criteria

- Clicking `–` on a project header removes it from the main sidebar list and shows its name in a tray at the bottom of the sidebar.
- Clicking `＋` on a chip — or anywhere on the chip row — returns the project to its original index in the sidebar list, with its sessions and collapsed state unchanged.
- Clicking `–` on a sidebar session row hides that session from grid views exactly as the grid tile's control does, and the row's control flips to `＋` to restore it.
- While a project is minimized, none of its sessions appear in `grid-all` or `grid-project`; they remain alive and reachable via the sidebar chip and ⌘K. (Amended by [#252](252-keyboard-switching-skips-minimized-sessions.md): ⌘[ / ⌘] now skips minimized projects rather than reaching them.)
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

[#252](252-keyboard-switching-skips-minimized-sessions.md) supersedes the ⌘[ / ⌘] half of the reachability criterion above: a project you put in the tray is out of the keyboard rotation entirely.

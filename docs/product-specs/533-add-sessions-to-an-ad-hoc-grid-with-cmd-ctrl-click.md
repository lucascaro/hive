---
issue: 533
title: Add sessions to an ad-hoc grid with ⌘/Ctrl-click
type: enhancement
complexity: M
priority: P2
pr: 534
shipped: 2026-10-26
stage: DONE
---

# Add sessions to an ad-hoc grid with ⌘/Ctrl-click

- **Issue:** #533

## Problem

Users running several agents can only view them side by side as a whole project (⌘G) or everything (⇧⌘G). To watch a small cross-project subset, or to compare or drive a few agents at once, they flip between sessions in single view and lose sight of the others. The all-sessions grid is too noisy and the per-project grid is too narrow.

## Desired behavior

- ⌘-click (Ctrl-click on Windows and Linux) a sidebar session toggles it into an ad-hoc selection.
- By default, once two or more sessions are selected the view switches to a grid of exactly those sessions, whichever projects they belong to.
- A Settings option switches this to select-then-⌘G, where nothing changes view until ⌘G tiles the selection.
- While the ad-hoc grid is open, ⌘-click adds or removes sessions live.
- The selection is throwaway. It isn't persisted, named or saved.
- Sidebar rows show which sessions are in the selection.

## Success criteria

1. In single view with session A active, ⌘-clicking B opens a 2-tile grid of A and B, even when they are in different projects.
2. ⌘-clicking C in that grid adds a tile. ⌘-clicking B removes B's tile.
3. Dropping to fewer than 2 tiles returns to single view on the remaining session.
4. A plain click on any session behaves as today (switch, no selection change beyond clearing it).
5. ⌘G and ⇧⌘G keep their current per-project and all-sessions behavior.
6. With the setting on select-then-⌘G, ⌘-click only marks rows, and ⌘G opens a grid of exactly the marked sessions.
7. Selected rows are visibly marked in the sidebar. The setting shows its default and can be changed.
8. Sidebar drag-reorder still works and doesn't alter the selection.
9. Killing, closing or minimizing a selected session removes it from the grid.
10. Ctrl-click does the same on Windows and Linux. Unit, dom and e2e tests cover the above, and the e2e run is isolated per the e2e-real rules.

## Non-goals

- Bulk actions on the selection (kill, minimize, move).
- Shift-click range select.
- Broadcasting input to all tiles.
- Persisting or naming layouts across restarts.
- Changing per-project or all-sessions grid semantics.

## Notes

Open questions:

- Exit gesture besides ⌘G is undecided.
- Where the auto-enter vs select-then-⌘G setting lives is undecided (assumed Settings).

Assumption: the ad-hoc grid is a third scope beside project and all; the tile set is session ids, session-only. ⌘G/⇧⌘G while in it leaves/enters per-project/all as today; plain click leaves the grid. The existing `resolveView` rule (<2 tiles → single) applies.

Prior art: `lib/view.ts` (view modes), `app/selectors.ts` `gridScopeSessions`, `components/GridView.tsx`, `components/Sidebar.tsx` session row `onSelect`.

---
issue: null
title: "Fix sidebar drag-and-drop ordering and drop placeholder"
type: bug
complexity: S
priority: P2
stage: DONE
pr: 315
shipped: 2026-09-02
---

# Fix sidebar drag-and-drop ordering and drop placeholder

- **Issue:** —
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/completed/305-fix-sidebar-drag-and-drop-ordering-and-placeholder.md](../exec-plans/completed/305-fix-sidebar-drag-and-drop-ordering-and-placeholder.md)

## Problem

Dragging a session row in the sidebar does not land it where it was dropped —
it consistently ends up one slot *below* the drop point. The drop affordance is
also only a 2px accent line while the dragged row keeps occupying its original
space at 45% opacity, so nothing previews the final layout and every row below
the drag jumps at the moment of the drop.

## Desired behavior

A dropped session lands exactly at the position the drop indicator showed. While
a drag is in flight the sidebar reserves a placeholder the same size as the
dragged item, and the dragged item itself is out of the flow — so the list's
total height is unchanged and no surrounding content shifts until the drop
commits. Project cards get the same affordance as session rows.

## Success criteria

- Dragging a session onto the top half of a target row places it immediately
  above that row; onto the bottom half, immediately below it. Verified in both
  drag directions and with projects interleaved in the daemon's global order.
- A placeholder matching the dragged element's full margin box appears at the
  drop slot, for both session rows and project cards.
- The dragged element is removed from the layout flow while dragging, so the
  y-position of rows below the drop target does not change during the drag.
- A regression test exists that fails against the pre-fix ordering math.

## Non-goals

- Cross-project session drags (still an early return in `wireSessionDrag`).
- Any change to the daemon's `moveInOrder` / `reindexLocked` semantics.
- Animated placeholder transitions or drag auto-scroll.

## Notes

Root cause: `reorderDroppedSession` indexes `pretend` (dragged row removed)
with an index computed against `projSessions` (dragged row present).
`reorderDroppedProject` does not share the bug.

---
issue: null
pr: null
type: fixed
bump: patch
---
- Dragging a session in the sidebar now drops it exactly where the indicator
  showed. The drop slot was resolved against the sibling list that still
  contained the dragged row, so a row dragged downwards consistently landed
  one position too low.
- The drop indicator is now a placeholder the size of the dragged item, and
  the dragged row leaves the layout while the drag is in flight — so the
  sidebar's height stays fixed and content no longer jumps on drop. Project
  cards get the same affordance as session rows.

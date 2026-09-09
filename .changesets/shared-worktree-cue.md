---
type: added
bump: minor
issue: 384
pr: 385
---

Sessions that share a git worktree are now linked in the sidebar. They take the
colour of the session whose worktree they joined, show that colour as a bar on
the right edge of the row, and carry a count on the branch icon; the worktrees
browser names the sessions occupying each worktree instead of only counting
them. Linked sessions also sit together in the sidebar, and dragging any one of
them moves the whole group.

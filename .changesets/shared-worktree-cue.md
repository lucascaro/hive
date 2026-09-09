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
them. Linked sessions also sit together in the sidebar. Dropping one outside its
group — or pressing the reorder keys once it is at the group's edge — moves the
whole group; dropping it on another member, or pressing the reorder keys while
it has room among its own members, reorders it inside the group. Keyboard
navigation, ⌘1-9, the tray and the command palette now all follow the order the
rows are actually painted in.

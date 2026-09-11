---
type: changed
bump: patch
issue: null
pr: null
---

The compact sidebar density keeps the window title as its single line instead of
the session name. The title is what tells apart two sessions that share a
worktree — they are named after the branch — so it is the line worth keeping. A
session that has published no title still shows its name.

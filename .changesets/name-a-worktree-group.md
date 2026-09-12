---
type: added
bump: minor
---

Worktree groups can be named. Double-click a group's title in the sidebar, type a
name, and the header shows it with the branch beside it — so a group of sessions
sharing a worktree can be called "the auth refactor" instead of only being labelled
by whatever git called the branch. Emptying the field clears the name again.

The name belongs to the group, not to its sessions: nothing is renamed, and members
still carrying the branch-derived default name go on showing their window title
instead. It is stored by the daemon against the worktree, so it survives a reload
and a daemon restart, and every open window updates the moment it changes.

---
type: fixed
bump: patch
issue: 583
---

Closing a session no longer deletes a worktree holding uncommitted work when Hive cannot reach the project's git repository (project folder moved or deleted, project removed, git unavailable, a transient git failure): the worktree is kept on disk instead. A close that cannot check the worktree for changes now asks for confirmation instead of assuming it is clean.

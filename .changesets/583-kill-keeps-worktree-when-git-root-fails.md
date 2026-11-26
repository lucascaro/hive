---
type: fixed
bump: patch
issue: 583
---

Closing a session can no longer delete a worktree holding uncommitted work when Hive cannot reach the project's git repository (project folder moved or deleted, git unavailable, a transient git failure). The worktree is kept and stays in the worktree browser. A close that cannot check the worktree for changes now asks for confirmation instead of assuming it is clean.

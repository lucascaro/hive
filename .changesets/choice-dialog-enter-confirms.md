---
type: fixed
bump: patch
issue: 577
pr: 578
---
Confirmation dialogs (Unpin all, Delete idea, worktree and branch deletes, archived-session deletes) now confirm on Enter, as their `[enter]` hint said. Before, Cancel held focus, so Enter cancelled. Each hint now names the action its key performs, and follows focus when you Tab to another button. Follow-up questions that appear after a refusal and would destroy work (closing a session with uncommitted changes, removing a dirty worktree, deleting a project's ideas) still default to Cancel, and holding Enter never answers a dialog.

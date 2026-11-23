---
type: fixed
bump: patch
issue: 577
---
Confirmation dialogs (Unpin all, Delete idea, Delete project, worktree and branch deletes, archived-session deletes, Close session anyway) now confirm on Enter, as their `[enter]` hint said. Before, Cancel held focus, so Enter cancelled. Each hint now names the action its key performs, and follows focus when you Tab to another button.

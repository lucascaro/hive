---
issue: 518
type: added
bump: minor
---
- **Archive a session and bring it back later.** The command palette gains **Archive Session**, which stops the focused session but keeps its worktree and a record that never expires (unlike Close, whose undo lasts 20 closes or 7 days), and **Archived Sessions…**, a searchable list grouped by project that restores any archived session with its name, project, worktree and, where the agent supports it, its conversation. Archiving asks nothing, even when the worktree has uncommitted changes, because it never touches the worktree; deleting an archived session for good is a separate, confirmed action.

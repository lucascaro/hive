---
issue: 304
pr: null
type: added
bump: minor
---
- Undo for an accidental session close. Closing a session now leaves a
  record behind, so it can be reopened — with its name, colour, project,
  worktree and (for agents that support resume) its conversation. An
  **Undo** banner appears the moment you close something, and **⌘Z** /
  **File ▸ Reopen Closed Session** reopens the most recent close at any
  time, including after a restart. Reopening is honest about what it
  cannot bring back: scrollback is always gone, and the banner says so
  along with anything else that was lost. Closing a session and deleting
  its worktree now saves a recovery patch of the uncommitted changes
  first, so even that path is no longer a dead end.

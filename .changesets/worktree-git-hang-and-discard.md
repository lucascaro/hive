---
type: fixed
bump: patch
---

Closing or starting a session no longer freezes at "Removing worktree" when a git fetch hangs on a bad network: a timed-out git now takes its ssh/credential helpers down with it, worktree removal is time-bounded, and a cancelled half-created session now cleans up its worktree entry and branch instead of leaking them. Slow git steps are logged in `hived.log`.

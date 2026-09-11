---
type: changed
bump: minor
issue: null
pr: null
---

New sessions are no longer named after the agent they run. An auto-generated
name is now just `adjective-noun`, and a session started in a worktree takes the
branch name alone — every surface that shows a name shows the agent beside it,
so carrying it in the name said the same thing twice. Sessions created before
this change keep the names they have, and renaming is unaffected.

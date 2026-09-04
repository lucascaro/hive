---
issue: 330
pr: null
type: fixed
bump: patch
---
- Closing a session no longer moves you to a different session while the
  "Close this session anyway?" confirmation is on screen. The jump used to
  happen during the daemon's pre-flight worktree check — before you had been
  asked anything — so the dialog appeared over a neighbouring session and
  cancelling left you there. Focus now moves only once the session is really
  closing.

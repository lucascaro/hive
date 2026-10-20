---
type: added
bump: minor
---

A session can now be allowed to message its siblings: tick "May direct other sessions" in the launcher (or toggle it on a session's row, or from the command palette) and that session's agent can run `hived msg <session> <text>` to hand another agent session in the same project a note. The note arrives marked as coming from the sending session by name, is always queued behind whatever the target is doing, and plain shell sessions are refused. The grant is off by default, is yours alone to give, survives a restart, and takes effect or is revoked on the very next message.

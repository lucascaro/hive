---
type: fixed
bump: patch
issue: 451
---

New worktree sessions no longer start on stale code without telling you. When
Hive cannot reach `origin` before creating the branch — off VPN, or a fetch
that times out on a large remote — it now asks what to do instead of quietly
branching from the last state it fetched. The session waits, visible in the
sidebar, with git's own error and how old the cached state is, and you can
retry, use the cached state deliberately, or cancel. The same prompt covers a
`git worktree add` that fails, which previously dropped the worktree and
started the session in the project directory without saying so.

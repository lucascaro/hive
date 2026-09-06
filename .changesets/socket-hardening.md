---
issue: null
pr: 361
type: fixed
bump: minor
---
- The daemon socket moved out of the shared `/tmp` into a directory only
  your user can reach — `$TMPDIR/hive/` on macOS, `$XDG_RUNTIME_DIR/hive/`
  on Linux — and Hive now checks that directory's owner and permissions
  before trusting it, whether it is binding to it or dialing it. On a
  shared machine another account could previously plant a fake socket
  there and read everything you typed.
- Programs running inside a Hive session can now only report their state
  back to Hive and capture ideas for their own project (`hived idea`). They
  could previously use the same connection to create, attach to or kill
  sessions, remove worktrees, shut the daemon down, or list every session
  you have open. This raises the bar rather than sealing a boundary — an
  agent runs as you and Hive does not sandbox it — so `SECURITY.md` now says
  so plainly. Requires a daemon restart, which Hive will prompt for after
  the update.
- `hived idea list --all` is refused from inside a session, where `hived idea
  list` now shows that session's project. `--all` works from an ordinary
  shell, which it did not before — it needed a session's environment and
  was therefore unreachable in every context the moment the in-session case
  was closed.
- Hive refuses to start a second daemon against one state directory. The
  old guard was the socket file, which stopped working the moment the
  socket moved; it is now a lock on the state directory itself.

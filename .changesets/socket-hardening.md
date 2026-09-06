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
  back to Hive and capture ideas (`hive idea`). They could previously use
  the same connection to create, attach to or kill sessions, remove
  worktrees, shut the daemon down, or list every session you have open.
  Requires a daemon restart, which Hive will prompt for after the update.

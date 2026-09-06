# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Hive, please **do not** open a public GitHub Issue.

Instead:
1. Open a GitHub Issue titled **[security] brief description** and mark it confidential, or
2. Contact the maintainer directly via the email on the GitHub profile.

Please include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Any suggested fix (optional)

You can expect an acknowledgement within 72 hours.

## Scope

| Component | Notes |
|-----------|-------|
| `hived` (session daemon) | In scope |
| `hivegui` (desktop client) | In scope |
| Agent processes | Spawned as the invoking user; the agent CLI's own security is out of scope |

## Known Considerations

### Single-User Systems

Hive is designed for personal use on a single-user machine. The session
daemon (`hived`) listens on a per-user Unix socket — `$TMPDIR/hive/hived.sock`
on macOS (launchd's private per-user temp dir),
`$XDG_RUNTIME_DIR/hive/hived.sock` on Linux (falling back to the state
directory when it is unset), `%LOCALAPPDATA%\Hive\hived.sock` on Windows
(`HIVE_SOCKET` overrides all three). Both POSIX defaults fall back to the state
directory rather than to `/tmp`: Linux when `$XDG_RUNTIME_DIR` is unset, macOS
when `$TMPDIR` resolves to the shared `/tmp`. Hive creates that directory with
mode `0o700`, and every Go component — the daemon before binding, the GUI, the
menu bar, `hived idea` and the hook client before dialing — refuses a directory
that is a symlink, is not owned by the current user, or is reachable by group or
other. None of the defaults live under the world-writable `/tmp`.

### Multi-User Systems

On shared machines (e.g., a development server with multiple accounts):

- The socket directory is created `0o700` and re-verified (owner, mode, and
  that it is not a symlink) on every bind and every dial, so other users can
  neither connect to the daemon nor plant an impostor socket for Hive to
  connect to.
- Programs running inside a session inherit `HIVE_SOCKET`, which names a
  second, narrowed socket rather than the control socket. It serves state
  reports (`HELLO{mode:event}`) and the idea verbs `hive idea` needs
  (`HELLO{mode:session}` — `ADD_IDEA` and `LIST_IDEAS`, both bound to the
  caller's own session and project, plus a session snapshot narrowed to that
  session's id and project). Everything else is answered with
  `mode_not_allowed`, so what a session's child processes inherit does not
  let them create, attach to or kill sessions, remove worktrees, shut the
  daemon down, or read another project's ideas.

  **This is defence in depth, not a security boundary.** The control socket
  sits next to the events one, and both are owned by you: a process running as
  your user can find it and connect to it, whatever `HIVE_SOCKET` says. POSIX
  permissions cannot separate two processes running as the same user, and Hive
  does not sandbox the agents it launches (see *Agent Processes* below). What
  the narrowed socket buys is that the obvious path — the environment variable
  a tool was handed — no longer leads to session creation, and that an
  integration written against `HIVE_SOCKET` cannot reach those verbs by
  accident. Treat an agent you run under Hive as having your privileges,
  because it does.
- Log files (`hived.log`, `hivegui.log`) are created with mode `0o600` and
  are not readable by other users.
- Agent session output is not exposed outside the daemon process.

### Agent Processes

Agents (Claude, Codex, Gemini, …) run as child processes of `hived` with the
same permissions as the user who launched Hive. Hive does not sandbox them.

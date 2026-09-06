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
(`HIVE_SOCKET` overrides all three). Hive creates that directory with mode
`0o700`, and every component — the daemon before binding, the GUI and the menu
bar before dialing — refuses a directory that is a symlink, is not owned by the
current user, or is reachable by group or other. None of the defaults live under
the world-writable `/tmp`.

### Multi-User Systems

On shared machines (e.g., a development server with multiple accounts):

- The socket directory is created `0o700` and re-verified (owner, mode, and
  that it is not a symlink) on every bind and every dial, so other users can
  neither connect to the daemon nor plant an impostor socket for Hive to
  connect to.
- Programs running inside a session inherit `HIVE_SOCKET`, which names a
  second, events-only socket. It accepts state reports (`HELLO{mode:event}`)
  and answers every other mode with `mode_not_allowed`, so a subprocess of an
  agent cannot use the environment it was handed to create, attach to, or kill
  sessions, or remove worktrees.
- Log files (`hived.log`, `hivegui.log`) are created with mode `0o600` and
  are not readable by other users.
- Agent session output is not exposed outside the daemon process.

### Agent Processes

Agents (Claude, Codex, Gemini, …) run as child processes of `hived` with the
same permissions as the user who launched Hive. Hive does not sandbox them.

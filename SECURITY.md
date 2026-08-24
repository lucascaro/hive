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
daemon (`hived`) listens on a per-user Unix socket — `/tmp/hive-<uid>/hived.sock`
on macOS, `$XDG_RUNTIME_DIR/hive/hived.sock` on Linux,
`%LOCALAPPDATA%\Hive\hived.sock` on Windows (`HIVE_SOCKET` overrides all
three). Hive creates the containing directory with mode `0o700`, so only the
owner can reach the socket — but it does not re-check the mode of a directory
that already exists. On macOS, and on Linux without `XDG_RUNTIME_DIR`, that
directory lives under world-writable `/tmp`, so on a shared machine verify its
ownership and permissions before trusting the socket.

### Multi-User Systems

On shared machines (e.g., a development server with multiple accounts):

- The socket directory is created `0o700`, so other users cannot connect to the
  daemon directly — subject to the pre-existing-directory caveat above.
- Log files (`hived.log`, `hivegui.log`) are created with mode `0o600` and
  are not readable by other users.
- Agent session output is not exposed outside the daemon process.

### Agent Processes

Agents (Claude, Codex, Gemini, …) run as child processes of `hived` with the
same permissions as the user who launched Hive. Hive does not sandbox them.

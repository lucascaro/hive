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
| `hive` binary | In scope |
| Native mux daemon | In scope |
| Hook execution | User-controlled scripts — hooks are run as the invoking user |
| tmux backend | Security depends on the installed tmux version |

## Known Considerations

### Single-User Systems

Hive is designed for personal use on a single-user machine. The native
multiplexer daemon (`hive mux-daemon`) communicates over a Unix socket
(`~/.config/hive/mux.sock`) with permissions `0o600` — accessible only by the
owner.

### Multi-User Systems

On shared machines (e.g., a development server with multiple accounts):

- The Unix socket is restricted to the owner, so other users cannot connect to
  the daemon directly.
- Log files (`hive.log`, `mux-daemon.log`) are created with mode `0o600` and
  are not readable by other users.
- Agent session output is not exposed outside the daemon process.

### Hook Scripts

Hook scripts in `~/.config/hive/hooks/` are executed as the user who launched
Hive. Only executable files are run. Review any scripts you place there, as
they have the same permissions as the Hive process.

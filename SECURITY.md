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

## Trust model

Hive's security boundary is your user account. Everything below follows
from that.

**Who cannot reach Hive.** Nothing outside your account. A release build
opens no network listener: the daemon listens only on Unix-domain sockets
in a directory created `0o700`, and the Go clients that exchange
frames with the daemon refuse that directory unless it is a real
directory owned by you with no group or other access (see *Single-User Systems* below; on Windows the socket sits
under your profile and relies on its ACL instead). Hive registers no URL
scheme, so a web page cannot open it with a link. (`wails dev` serves the
frontend on `localhost:34115`, and the `hived-ws-bridge` test harness
listens on TCP; neither ships in the app.)

**Who can.** Any process running as you. Hive does not distinguish its
own GUI from any other program you run: a process that connects to the
daemon's control socket, which sits next to the `HIVE_SOCKET` path every
session is handed, gets the full control protocol. That includes
creating a session with any command, typing into any session, answering
an agent's permission prompt, approving a plan, and installing a plugin.

An installed plugin connects on a socket of its own, and the daemon
refuses it the user's decisions there with `mode_not_allowed`: typing
into a session (attach input and resize, `SEND_TO_SESSION`, a
`RESOLVE_PROMPT` paste), answering a permission prompt, deciding a plan
review or a worktree-setup question, stopping Hive, installing,
enabling or removing plugins, and changing another plugin's config. It
may prompt only the ACP sessions it spawned itself. The full list is in
[docs/plugins.md](docs/plugins.md#what-a-plugin-cannot-do). This is not
a sandbox: a plugin runs as you, so it can still dial the main control
socket like any other process here and get the full protocol. The
refusals keep a well-behaved plugin, and whatever text steers it, to
its role.

**Agents are processes running as you.** An agent with a shell — Claude
Code, Codex, any of them — can therefore do everything above. Usually
that is fine: you started it and it acts for you. The risk is an agent
that reads untrusted text (a web page, an issue, a README in a cloned
repo) and follows an instruction hidden in it. Because every session is
reachable from every other, one prompt-injected agent can drive all of
your sessions, including ones running with permission prompts turned
off.

**What the orchestrator grant does and does not do.** *May direct other
sessions* (spec 389) decides whether a session may message its siblings
through Hive's own messaging — `hived msg` over the session's narrowed
socket. The daemon enforces it on that socket, and an agent that uses
Hive's messaging as intended cannot direct other sessions without it.
It does **not** stop an agent that goes around Hive's messaging: an
agent with a shell can reach the control socket directly, grant or no
grant. Read the grant as a statement of intent that well-behaved agents
honour, not as a sandbox.

**What to do about it.**

- Treat every agent you run under Hive as having your privileges,
  because it does, and treat what it reads as input to all of your
  sessions, not just its own.
- Run untrusted repositories and agents you do not trust somewhere that
  cannot reach your Hive: a container or VM, or a separate macOS user
  account with its own Hive.
- Prefer each agent's own permission prompts over bypass modes for work
  that reads untrusted content.
- Review plugins before installing them; a plugin runs as you.

**What is planned.** Stage 2 is to make the control socket accept only
Hive's own clients, using a credential agents cannot read, and then to
let you switch agent-to-agent control off entirely. The problem and
open questions are in
[spec: agent-control credential and switch](docs/product-specs/554-agent-control-credential-and-switch.md).
Until it ships, the advice above is the boundary.

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
mode `0o700`, and the daemon before binding, and the GUI, the menu bar,
`hived idea` and the hook client before dialing, refuse a directory that is a
symlink, is not owned by the current user, or is reachable by group or other.
The GUI's restart probe (`cmd/hivegui/restart.go`) dials without the check: it
only connects and hangs up to learn whether the old daemon has exited, and
sends nothing. None of the defaults live under the world-writable `/tmp`. The Pi
reporter extension (`internal/agent/pi/hive.ts`) is the exception: it runs in
Pi's JavaScript tier and only ever sends state observations on the events
socket, so it dials without the check.

### Multi-User Systems

On shared machines (e.g., a development server with multiple accounts):

- The socket directory is created `0o700` and re-verified (owner, mode, and
  that it is not a symlink) before every bind and before every dial the Go
  components make — including the probe that looks for a daemon left over from
  before the socket moved. Another user can therefore neither connect to the
  daemon nor plant a socket Hive would connect to, or treat as a daemon. The
  exceptions are the Pi reporter extension, which runs in Pi's JavaScript tier
  and only sends state observations, and `hived-ws-bridge`, which is a test
  harness that dials a socket its caller names explicitly.
- Programs running inside a session inherit `HIVE_SOCKET`, which names a
  second, narrowed socket rather than the control socket. It serves state
  reports (`HELLO{mode:event}`) and a short list of session verbs
  (`HELLO{mode:session}`): the idea verbs `hive idea` needs (`ADD_IDEA`,
  `LIST_IDEAS`), an ACP session's `SUBMIT_RESULT`, a project-scoped
  `LIST_SESSIONS` / `WAIT_SESSION`, and `SEND_TO_SESSION` for a session
  you granted *May direct other sessions*. On connect it also receives a
  snapshot of its own session entry, and nobody else's. Everything else is answered
  with `mode_not_allowed`, so what a session's child processes inherit
  does not let them create, attach to or kill sessions, remove worktrees,
  shut the daemon down, or read another project's ideas.

  **This is defence in depth, not a security boundary** — see
  [Trust model](#trust-model) above.
- Log files (`hived.log`, `hivegui.log`) are created with mode `0o600` and
  are not readable by other users.
- Agent session output is not exposed outside the daemon process.

### Agent Processes

Agents (Claude, Codex, Gemini, …) run as child processes of `hived` with the
same permissions as the user who launched Hive. Hive does not sandbox them.

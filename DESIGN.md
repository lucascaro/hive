# DESIGN.md

Top-level design overview for Hive v2 — the Wails GUI + `hived` daemon. The *shape* of the project: domains, layers, cross-cutting concerns, and the architectural rules that hold everything together.

Per-decision detail belongs in `docs/design-docs/` and `docs/native-rewrite/`. This file is the map.

> v1 (TUI / Bubble Tea / tmux backend) lives on `release/v1` for bug-fix-only maintenance. Branch policy (cherry-pick only, no wholesale merges) is documented in AGENTS.md.

## Domains

- **Sessions** (`internal/session/`) — PTY lifecycle, VT parsing, scrollback buffer. One `Session` owns one child process and the in-memory terminal state.
- **Registry** (`internal/registry/`) — the daemon's source of truth for open sessions, projects, ordering, and per-session metadata (name, color, agent type). Owns on-disk persistence.
- **Wire protocol** (`internal/wire/`) — versioned IPC frames between GUI and daemon. Pure types + framing; no I/O policy.
- **Daemon** (`internal/daemon/`, `cmd/hived/`) — multi-session PTY host. Accepts Unix-socket connections, dispatches by HELLO mode (`control` / `attach` / `create`), spawns/kills sessions through the registry.
- **GUI** (`cmd/hivegui/`, `hivegui/frontend/`) — Wails desktop app. Thin client over the wire protocol; xterm.js renders terminal output, the JS layer owns sidebar/layout/agent UX.
- **Worktrees** (`internal/worktree/`) — git worktree lifecycle for agent sessions; tracks dirty state so the registry can refuse destructive operations.
- **Agents** (`internal/agent/`) — canonical agent catalog (`claude`, `codex`, `gemini`, …) and human-readable name generation.
- **Notifications** (`internal/notify/`) — platform-specific desktop notifications. Bell/audio support (`internal/audio/`) is planned but not yet implemented.

## Layers

In-process Go dependency direction is one-way:

```
Wire (types)
  ↓
Session  ·  Agent  ·  Worktree         (pure domain)
  ↓
Registry                                (persistence boundary)
  ↓
Daemon                                  (transport: Unix socket, dispatch)
```

Runtime process topology is separate from the dependency graph:

```
hived process  ⇄  hivegui process       (Unix socket; daemon survives GUI close)
                        │
                        └── Wails frontend (JS / xterm.js, in-process with hivegui)
```

- `internal/wire/` imports nothing from Hive; `internal/session/` and `internal/agent/` know nothing about persistence; `internal/registry/` knows nothing about the socket; `internal/daemon/` is the only place that owns the connection state machine.
- The GUI is a *client* of the daemon. It never opens a PTY itself, never reads daemon state files directly — both go through the wire protocol.

## Cross-cutting concerns

- **IPC** — single channel: `internal/wire/`. Every cross-process call (GUI ⇄ daemon, future remote clients) is a wire frame. No side-channel files, no shared sqlite.
- **Persistence** — owned by `internal/registry/`. The daemon main loop never writes session state directly. State location is resolved by `registry.StateDir()` — see `internal/registry/paths.go` for platform-specific paths. Writes are atomic (temp + rename).
- **Build & version** — `internal/buildinfo/` is the single source for version/commit; `cmd/version.go` and the GUI menu both read it.
- **Notifications** — `internal/notify/` is the only entry point for desktop notifications. Platform splits (`notify_darwin.go`, `notify_linux.go`, `notify_windows.go`, `notify_darwin.m`) live behind one Go interface.
- **Logging** — stdlib `log`. Daemon logs to a file under the platform state dir; GUI logs to stdout in dev, file in production.

## Hard rules

Architectural invariants. Each one should ideally be enforceable by `gc-sweep` or a custom lint.

- **Wire JSON is `snake_case` on the wire, `CamelCase` in Go.** Every field in `internal/wire/` carries an explicit `json:"snake_case"` tag. JS readers in `hivegui/frontend/` use `snake_case ?? camelCase` at the boundary.
- **The GUI never opens a PTY.** All PTY operations go through the wire protocol. Grep guard: no `os/exec`, `creack/pty`, or `internal/session` imports in `cmd/hivegui/` or `hivegui/`.
- **The registry is the only writer of persisted state.** No file writes under `registry.StateDir()` from `internal/daemon/`, `internal/session/`, or anywhere else. Atomic writes only — never partial truncates.
- **Wire mode is immutable for the connection.** Whatever mode a client picks in HELLO (`control` / `attach` / `create`) is the mode for the connection's lifetime. Daemon dispatch must reject frames that don't belong to the negotiated mode.
- **No I/O in `internal/wire/`.** Types and frame encoding only — no sockets, no filesystem, no `os.Getenv`. Keeps dependency direction clean and lets the protocol be tested in isolation.
- **Cross-platform parity is verified per release.** `notify`, `worktree`, `os_terminal`, and PTY paths all have platform splits — every release exercises macOS, Linux, and Windows builds (`scripts/release.sh`). No `runtime.GOOS == "darwin"` shortcuts in domain code; gate at the package boundary.

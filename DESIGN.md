# DESIGN.md

Top-level design overview for Hive — the Wails GUI + `hived` daemon. The *shape* of the project: domains, layers, cross-cutting concerns, and the architectural rules that hold everything together.

Per-decision detail belongs in `docs/design-docs/`. This file is the map. The GUI's visual system (tokens, themes, components) is in `docs/design-docs/ui/`.

## Domains

- **Sessions** (`internal/session/`) — PTY lifecycle, VT parsing, scrollback buffer. One `Session` owns one child process and the in-memory terminal state.
- **Registry** (`internal/registry/`) — the daemon's source of truth for open sessions, projects, ordering, and per-session metadata (name, color, agent type). Owns on-disk persistence.
- **Wire protocol** (`internal/wire/`) — versioned IPC frames between GUI and daemon. Pure types + framing; no I/O policy.
- **Daemon** (`internal/daemon/`, `cmd/hived/`) — multi-session PTY host. Accepts Unix-socket connections, dispatches by HELLO mode (`control` / `attach` / `create`), spawns/kills sessions through the registry.
- **GUI** (`cmd/hivegui/`, `hivegui/frontend/`) — Wails desktop app. Thin client over the wire protocol; xterm.js renders terminal output, and a React 19 + zustand frontend owns sidebar/layout/agent UX. Terminals stay imperative behind a stable boundary (`src/app/session-term.ts`); everything else renders from the store. Frontend conventions: [FRONTEND.md](FRONTEND.md).
- **Worktrees** (`internal/worktree/`) — git worktree lifecycle for agent sessions: create, inventory (`List`/`ListBranches`), safety status (`Inspect`), rename and removal. Tracks uncommitted and unpushed state so the registry can refuse destructive operations.
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
                        └── Wails frontend (React 19 + zustand / xterm.js,
                            in-process with hivegui)
```

- `internal/wire/` imports nothing from Hive; `internal/session/` and `internal/agent/` know nothing about persistence; `internal/registry/` knows nothing about the socket; `internal/daemon/` is the only place that owns the connection state machine.
- The GUI is a *client* of the daemon. It never opens a PTY itself, never reads daemon state files directly — both go through the wire protocol.

## Cross-cutting concerns

- **IPC** — single channel: `internal/wire/`. Every cross-process call (GUI ⇄ daemon, future remote clients) is a wire frame. No side-channel files, no shared sqlite.
- **Persistence** — owned by `internal/registry/`. The daemon main loop never writes session state directly. State location is resolved by `registry.StateDir()` — see `internal/registry/paths.go` for platform-specific paths. Writes are atomic (temp + rename). Alongside `sessions/` and `projects/`, the registry owns `closed/`: one tombstone per recently closed session (plus, for a close that deleted a worktree, a recovery patch of its uncommitted state), written before the teardown so a close can be undone. Bounded to the last 20 closes and 7 days.
- **Build & version** — `internal/buildinfo/` is the single source for version/commit; `cmd/version.go` and the GUI menu both read it. It also owns `DaemonContract`, the integer naming the compatibility generation of everything the daemon exposes; the GUI compares it (from `WELCOME`, and from `hived --version --json` on a staged update bundle) to decide between relaunching the GUI alone and restarting the daemon. See [docs/design-docs/daemon-contract.md](docs/design-docs/daemon-contract.md).
- **Notifications** — `internal/notify/` is the only entry point for desktop notifications. Platform splits (`notify_darwin.go`, `notify_linux.go`, `notify_windows.go`, `notify_darwin.m`) live behind one Go interface.
- **Logging** — stdlib `log`. Daemon logs to a file under the platform state dir; GUI logs to stdout in dev, file in production.

## Hard rules

Architectural invariants. Each one should ideally be enforceable by `gc-sweep` or a custom lint.

- **`hivebar` is a client, like the GUI.** The menu-bar agent
  (`cmd/hivebar/`, darwin only) may not open a PTY, import
  `internal/session`, or write anything under `registry.StateDir()`
  except its own lock file. Everything it knows arrives on one control
  connection. It also never spawns `hived` on its own — only as the
  explicit Restart Daemon action — so a menu bar cannot resurrect a
  daemon the user just quit.
- **A GUI reload never touches the daemon.** `App.ReloadGUI` relaunches the
  GUI process and leaves `hived` and every PTY it owns running; only
  `App.RestartDaemon` may send `FrameShutdown` or signal the daemon. Grep
  guard: no `FrameShutdown` write and no `killRunningHived` call reachable
  from the reload path. The two are told apart by the daemon contract, never
  by build ID.
- **`CLIENT_COMMAND` is a relay, not an operation.** The daemon validates the
  verb against `wire.ClientCommands` and fans it back out as
  `CLIENT_BROADCAST`; it never acts on one. The verbs describe client-side UI
  state, so the fan-out hub lives in `internal/daemon/`, not in the registry —
  the registry writes persisted state, and "relaunch your window" is not
  state.
- **Adding a wire frame does not bump `PROTOCOL_VERSION`.** The daemon refuses
  a mismatched `hello.Version` outright, so a bump makes a new client unable
  to handshake at all — including with the daemon it needs to interrogate.
  Unknown frames are logged and ignored, which is what makes additions safe.
  Bump `PROTOCOL_VERSION` only for a genuine break (a frame whose meaning
  changed, a field an old peer would misread), and bump `DaemonContract`
  alongside it.
- **Wire JSON is `snake_case` on the wire, `CamelCase` in Go.** Every field in `internal/wire/` carries an explicit `json:"snake_case"` tag. JS readers in `hivegui/frontend/` use `snake_case ?? camelCase` at the boundary.
- **The GUI never opens a PTY.** All PTY operations go through the wire protocol. Grep guard: no `os/exec`, `creack/pty`, or `internal/session` imports in `cmd/hivegui/` or `hivegui/`.
- **The registry is the only writer of persisted *session* state.** No file writes under `registry.StateDir()` from `internal/daemon/`, `internal/session/`, or anywhere else. Atomic writes only — never partial truncates. The GUI owns three files in that same directory that are *not* session state and never cross the wire: `agents.json` (custom agents, also read by hived), `window.json` (window geometry), and `update.json` (update channel + source-repo override). They follow the same temp + rename discipline. Anything the daemon must agree about goes through the wire protocol and the registry instead.
- **`SESSION_EVENT(added)` means "the entry exists", not "you may attach".**
  A session carries a lifecycle phase (`wire.Phase*`, in-memory on the daemon,
  never persisted); it is attachable only when `alive == true` **and** the
  phase is `PhaseReady`. Attaching earlier is answered with
  `wire.ErrCodeSessionStarting`, not an error the client should treat as death.
- **Attach replays scrollback, not just the visible screen.** Reattaching a
  session — GUI restart included — must restore the history above the viewport,
  not a snapshot of the current screen. The daemon sends it during the `replay`
  phase and closes it with `pty:event kind=scrollback_replay_done`; anything
  that narrows the replay to the visible rows breaks the contract.
- **A worktree outlives its sessions unless it is pristine.** `Kill` and the
  startup orphan reclaim remove a worktree only when `worktree.Inspect`
  reports no uncommitted changes AND no unpushed commits AND a resolvable
  comparison base (`Status.Pristine`). Anything else survives and is managed
  explicitly through the worktree frames. An unanswerable base counts as
  "holds work", never as "safe to delete" — the conservative direction is
  load-bearing, since the other reading silently destroys commits.
  `git worktree list` is the source of truth for what exists; the registry
  stores no worktree records of its own.
- **Control-frame handlers that shell out to git run off the read loop.**
  `CREATE_SESSION`, `KILL_SESSION`, `RESTART_SESSION`, `KILL_PROJECT`, and the
  worktree frames (`LIST_WORKTREES`, `REMOVE_WORKTREE`, `CREATE_WORKTREE`,
  `RENAME_WORKTREE`, `DELETE_BRANCH`) are
  dispatched to goroutines owned by the daemon (drained in `Close`), so a slow
  `git worktree add`/`remove`/`list` can't stall every other client request. The git
  subprocesses themselves are serialized by the registry, and its lock
  ordering rule is one-way: never take that git lock while holding `r.mu`.
- **Wire mode is immutable for the connection.** Whatever mode a client picks in HELLO (`control` / `attach` / `create`) is the mode for the connection's lifetime. Daemon dispatch must reject frames that don't belong to the negotiated mode.
- **No I/O in `internal/wire/`.** Types and frame encoding only — no sockets, no filesystem, no `os.Getenv`. Keeps dependency direction clean and lets the protocol be tested in isolation.
- **Cross-platform parity is verified per release.** `notify`, `worktree`, `os_terminal`, and PTY paths all have platform splits — every release exercises macOS, Linux, and Windows builds (`scripts/release.sh`). No `runtime.GOOS == "darwin"` shortcuts in domain code; gate at the package boundary.

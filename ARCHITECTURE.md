# Architecture

This document describes the internal structure of Hive for contributors.

## Data Hierarchy

```
Project (groups related work)
  └── Team (coordinated agent group, optional)
        └── Session (1:1 with a terminal pane)
  └── Session (standalone, no team)
```

## Package Overview

```
hive/
├── main.go                  # Entry point — initialises Cobra
├── cmd/
│   ├── root.go              # Cobra root command
│   ├── start.go             # `hive start` — launch TUI
│   ├── attach.go            # `hive attach [session-id]` — headless attach
│   ├── mux_daemon.go        # `hive mux-daemon` — internal daemon process
│   └── version.go           # `hive version`
└── internal/
    ├── tui/                 # Bubble Tea model and all UI components
    ├── mux/                 # Multiplexer abstraction layer
    │   ├── native/          # Built-in PTY backend (daemon + client)
    │   └── tmux/            # tmux backend wrapper
    ├── tmux/                # Low-level tmux CLI wrappers
    ├── state/               # Data model and reducer functions
    ├── config/              # Configuration loading, saving, and migration
    ├── escape/              # OSC 2 title sequence parser
    ├── git/                 # Git worktree helpers
    └── hooks/               # Shell hook runner
```

### `internal/tui`

The Bubble Tea root model (`app.go`) wires together all components. The UI
follows the Elm architecture: messages flow in, the model updates, and `View()`
renders the current state.

View navigation uses a **view stack** (`viewstack.go`): `PushView()` to open a
dialog/overlay, `PopView()` to close it and return to whatever was underneath.
`View()` and key dispatch both use `TopView()` to determine which view is active.
This replaces the earlier priority-ordered boolean flag cascade.

Key components:

| File | Responsibility |
|------|---------------|
| `app.go` | Root model — update loop, message routing |
| `viewstack.go` | View stack: ViewID enum, push/pop/top/has, legacy flag sync |
| `keys.go` | Key map, loaded from config |
| `messages.go` | All `tea.Msg` types used across the app |
| `layout.go` | Terminal size tracking and pane sizing |
| `persist.go` | Atomic state reads/writes with exclusive file lock |
| `watcher.go` | Background mtime poller for multi-instance live reload |
| `lock_unix.go` / `lock_windows.go` | Platform-specific advisory lock helpers |
| `components/sidebar.go` | Three-level collapsible project/team/session tree |
| `components/preview.go` | Live session output preview (ANSI passthrough) |
| `components/statusbar.go` | Breadcrumb and contextual key hints |
| `components/gridview.go` | Tiled grid overview (g/G) |
| `components/agentpicker.go` | Agent type selection menu |
| `components/teambuilder.go` | Multi-step team creation wizard |
| `components/titleedit.go` | Inline session/team rename input |
| `components/confirm.go` | Yes/no confirmation dialog |
| `styles/theme.go` | Lip Gloss styles and colour theme |

### `internal/mux`

The `Backend` interface abstracts terminal multiplexing. Two implementations exist:

- **`native/`** — built-in PTY backend. A background daemon process (`hive mux-daemon`) owns all PTY master file descriptors. The TUI communicates with it over a Unix domain socket (`~/.config/hive/mux.sock`) using a length-prefixed JSON protocol.
- **`tmux/`** — delegates to the `tmux` binary. Requires tmux to be installed.

```
TUI → state reducers → mux backend client → Unix socket → daemon → PTY processes
```

### `internal/state`

Pure data model with reducer-style update functions. No I/O. Easily testable.

| File | Contents |
|------|---------|
| `model.go` | `AppState`, `Project`, `Team`, `Session` structs |
| `store.go` | Reducer functions (`AddProject`, `AddSession`, `KillSession`, …) |
| `events.go` | Hook event type constants |

### `internal/config`

JSON configuration at `~/.config/hive/config.json`. Atomic writes (write to
`.tmp`, then `os.Rename`) prevent corruption on crash. Schema migrations are
handled in `migrate.go`.

### `internal/escape`

Parses OSC 2 (`\033]2;title\007`) and the Hive-specific title marker
(`\x00HIVE_TITLE:...\x00`) from terminal output. A background watcher polls
session output and dispatches title change messages.

### `internal/hooks`

Discovers and runs executable scripts in `~/.config/hive/hooks/` with a
5-second timeout. Supports flat files (`on-{event}`) and `.d/` directories for
multiple hooks per event. Never crashes the TUI — errors are logged.

### `internal/git`

Optional git worktree helpers for creating per-session isolated working trees.

## TUI Layout

```
┌─────────────────────────────────────────────────────────────────┐
│ ▼ project-alpha             │ ╔══════ preview ════════════════╗ │
│   ▼ [team] feature-x        │ ║                               ║ │
│     ★ orchestrator [claude] │ ║  <session output, ANSI>       ║ │
│     ○ worker-1 [claude]     │ ║  refreshed every 500ms        ║ │
│     ○ worker-2 [codex]      │ ║                               ║ │
│   ○ solo-session [gemini]   │ ╚═══════════════════════════════╝ │
│ ▶ project-beta              │                                   │
├─────────────────────────────┴───────────────────────────────────┤
│ project-alpha / feature-x / orchestrator [claude] [waiting]      │
│ q:quit  r:rename  a:attach  t:new  T:new-team  ?:help           │
└─────────────────────────────────────────────────────────────────┘
```

- **Sidebar** — 28% width, min 32 cols. Collapses to icon-only below 80 cols.
- **Preview** — remaining width. Refreshed every 500ms.
- **Status bar** — two lines: breadcrumb path + context key hints.

## Session Title System

Two sources update a session title:

1. **User rename** (`r`) — inline text input; sets `TitleSource: user`.
2. **Agent escape sequence** — OSC 2 (`\033]2;title\007`) or `\x00HIVE_TITLE:...\x00` written to the PTY. The background watcher detects this and dispatches a title change message. By default, user-set titles take precedence (configurable).

## tmux Window Naming

| Object | Name pattern |
|--------|-------------|
| tmux session | `hive-{projectID[:8]}` |
| tmux window | `{sessionID[:8]}` |
| Window title | `{Title} [{agentType}]` |

## Key Message Types (`internal/tui/messages.go`)

All inter-component communication in the TUI flows through `tea.Msg` values routed by `app.go`'s `Update()`.

| Message | Direction | Purpose |
|---------|-----------|---------|
| `SessionCreatedMsg` | mux → Update | Session spawned in multiplexer |
| `SessionKilledMsg` | mux → Update | Session window removed |
| `SessionAttachMsg` | Update → runtime | Suspend TUI, connect terminal to pane |
| `SessionDetachedMsg` | runtime → Update | User returned from attached session |
| `SessionTitleChangedMsg` | escape.Watcher → Update | Title changed (user or agent) |
| `SessionStatusChangedMsg` | Update → Update | Session status changed |
| `TeamCreatedMsg` | mux → Update | Team + sessions created |
| `TeamKilledMsg` | mux → Update | Team removed |
| `ProjectCreatedMsg` | input → Update | Project created |
| `ProjectKilledMsg` | mux → Update | Project removed |
| `ErrorMsg` | any → Update | Non-fatal error for status bar |
| `ConfirmActionMsg` | Update → Update | Requests yes/no dialog |
| `ConfirmedMsg` | Confirm → Update | User confirmed action |
| `PersistMsg` | Update → Update | Trigger state write to disk |
| `QuitAndKillMsg` | Update → runtime | Quit + kill all sessions |
| `ConfigSavedMsg` | settings → Update | Config written to disk |
| `stateWatchMsg` | watcher goroutine → Update | Periodic mtime check; triggers reload when changed by another instance |

## Multi-Instance Safety

Multiple hive processes may run against the same `~/.config/hive/` directory.
Three mechanisms keep them consistent:

| Mechanism | Where | Purpose |
|-----------|-------|---------|
| Exclusive advisory lock | `internal/tui/lock_unix.go` | Serialises concurrent writes to `state.json` via `syscall.Flock` on a companion `.lock` file; prevents bit-level corruption when two instances save simultaneously |
| Atomic rename | `internal/tui/persist.go` | Write to `state.json.tmp`, then `os.Rename` — readers always see a complete file even during a write |
| State file watcher | `internal/tui/watcher.go` | Each running TUI polls `state.json` mtime every 500 ms; when another instance writes, the TUI reloads, reconciles dead tmux windows, and refreshes the sidebar without restarting |

The watcher distinguishes its own writes from external ones by updating
`Model.stateLastKnownMtime` after each `persist()` call. Only a mtime that
advances past the last known value (and was not caused by ourselves) triggers a
reload.

## State Mutation Rules

1. `AppState` is only ever mutated inside `tui/app.go`'s `Update()`.
2. Every mutation goes through a reducer function in `internal/state/store.go`.
3. Reducers are pure functions: `func Foo(s *AppState, ...) *AppState` — no I/O.
4. After a mutation, `Update()` calls `persist()` which writes `state.json` under an exclusive lock.
5. No goroutine other than the Bubble Tea runtime touches `AppState`.

## Backend Selection

`cmd/start.go` selects the backend at startup:

```
--native flag present  →  mux/native backend
config.Multiplexer == "native"  →  mux/native backend
otherwise  →  mux/tmux backend (default)
```

The selected backend is registered with `mux.SetBackend(backend)` before
the TUI starts. All subsequent `mux.*` calls in the TUI delegate to it.

## Native Daemon Protocol

When using the native backend, a background daemon process (`hive mux-daemon`) owns all PTY file descriptors.

**Socket:** `~/.config/hive/mux.sock` (mode 0600, owner-only access)

**Framing:** 4-byte big-endian uint32 length prefix followed by JSON body. Max message size: 4 MiB.

**Request ops:** `create-session`, `session-exists`, `kill-session`, `list-sessions`, `create-window`, `window-exists`, `kill-window`, `rename-window`, `list-windows`, `capture-pane`, `capture-pane-raw`

**Daemon lifecycle:**
- Spawned by `cmd/start.go` via `os/exec` + `setsid` (fully detached)
- If daemon is already running (socket exists and responds to ping), reuse it
- Daemon exits when its socket is removed or a fatal error occurs
- Log: `~/.config/hive/hive.log`

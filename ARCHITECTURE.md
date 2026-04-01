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

Key components:

| File | Responsibility |
|------|---------------|
| `app.go` | Root model — update loop, message routing |
| `keys.go` | Key map, loaded from config |
| `messages.go` | All `tea.Msg` types used across the app |
| `layout.go` | Terminal size tracking and pane sizing |
| `persist.go` | Saving/restoring TUI state across sessions |
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

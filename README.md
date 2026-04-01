# Hive

A terminal TUI for managing multiple AI coding agent sessions across projects — Claude, Codex, Gemini, GitHub Copilot, Aider, OpenCode, and more. Native support for Claude agent teams (orchestrator + workers).

```
┌─────────────────────────────────────────────────────────────────┐
│ ▼ project-alpha             │ ╔══════ preview ════════════════╗ │
│   ▼ [team] feature-x        │ ║                               ║ │
│     ★ orchestrator [claude] │ ║  > implement the auth module  ║ │
│     ○ worker-1 [claude]     │ ║  Working on JWT validation…   ║ │
│     ○ worker-2 [codex]      │ ║                               ║ │
│   ○ solo-session [gemini]   │ ╚═══════════════════════════════╝ │
│ ▶ project-beta              │                                   │
├─────────────────────────────┴───────────────────────────────────┤
│ project-alpha / feature-x / orchestrator [claude] [waiting]      │
│ q:quit  r:rename  a:attach  t:new  T:new-team  ?:help           │
└─────────────────────────────────────────────────────────────────┘
```

## Features

- **Multiple projects** — organize work into named projects with color coding
- **Multiple sessions per project** — each session runs in a persistent terminal pane
- **Session persistence** — sessions keep running after the TUI closes; reconnect with `hive start`
- **Agent teams** — native Claude multi-agent teams with orchestrator + worker layout
- **Easy switching** — vim-style navigation (`j/k/J/K`), numbered project jumps (`1`–`9`)
- **Live preview** — 500ms refresh with ANSI color passthrough
- **Session title editing** — press `r` to rename inline, or let the agent set it via escape sequence
- **Extensible hooks** — shell scripts fired on 11 lifecycle events with rich `HIVE_*` env vars
- **Responsive** — adapts to any terminal width, sidebar collapses on narrow terminals
- **Multi-agent support** — Claude, Codex, Gemini, Copilot, Aider, OpenCode, custom commands

## Requirements

- **Go 1.25+** (to build from source)
- No other runtime dependencies — the native multiplexer backend is built in

> **tmux users:** Hive can optionally use tmux as its backend. Set `"multiplexer": "tmux"` in `config.json`.

## Installation

### Build from source

```bash
git clone https://github.com/lucascaro/hive
cd hive
go build -o hive .
sudo mv hive /usr/local/bin/   # or any directory on your PATH
```

### Verify

```bash
hive version
```

## Getting Started

### 1. Launch Hive

```bash
hive start
```

On first launch, Hive creates `~/.config/hive/` with a default `config.json` and starts the background multiplexer daemon.

### 2. Create a project

Press `n`, type a project name, press `Enter`.

### 3. Create a session

With a project selected, press `t`. A picker appears — choose your agent:

```
Select Agent
────────────
▶ [claude]   Claude (Anthropic)
  [codex]    Codex (OpenAI)
  [gemini]   Gemini (Google)
  [copilot]  GitHub Copilot CLI
  [aider]    Aider
  [opencode] OpenCode
  [custom]   Custom command

enter: select  esc: cancel
```

### 4. Attach to a session

Select a session with `j/k` and press `a` or `Enter`. The TUI suspends and you interact with the agent directly. Press **Ctrl+Q** to detach and return to Hive.

### 5. Create an agent team

Press `T` to open the team wizard. You'll be guided through:

1. Team name + goal
2. Orchestrator agent (Claude recommended)
3. Number of workers + agent type per worker
4. Shared working directory
5. Confirmation — all sessions are created and agents launched

Teams appear in the sidebar with the orchestrator marked `★`:

```
▼ [team] feature-x
  ◉ ★ orchestrator [claude]
  ○ worker-1     [claude]
  ○ worker-2     [codex]
```

Session status is shown with a colored dot in both the sidebar and grid view:
- `○` gray: idle
- `●` green: working
- `◉` amber: waiting
- `✕` red: dead

The status legend is always visible in the main status bar and in the grid view footer.

## Navigation

| Key | Action |
|-----|--------|
| `j` / `↓` | Move cursor down |
| `k` / `↑` | Move cursor up |
| `J` | Jump to next project |
| `K` | Jump to previous project |
| `1`–`9` | Jump to project by number |
| `Space` | Toggle collapse/expand project or team |
| `←` | Collapse project/team; on a session, collapses its parent |
| `→` | Expand project or team |
| `Tab` | Switch focus between sidebar and preview |
| `a` / `Enter` | Attach to selected session |
| `r` | Rename session or team inline |
| `t` | New session (opens agent picker) |
| `T` | New agent team (opens wizard) |
| `n` | New project |
| `x` / `d` | Kill selected session |
| `D` | Kill entire team |
| `g` | Grid view — current project only |
| `G` | Grid view — all projects |
| `/` | Filter sessions by name |
| `?` | Toggle help overlay |
| `H` | Toggle session keybinding reference |
| `q` | Quit (sessions keep running) |
| `Q` | Quit and kill all managed sessions |

## Grid View

Press `g` to open a tiled overview of all sessions in the current project. Press `G` to see all sessions across every project. Each tile shows a live preview of the session's output.

```
╭─────────────────╮  ╭─────────────────╮
│ ● [claude] auth │  │ ● [codex] tests  │
│ Working on JWT… │  │ Writing unit…    │
╰─────────────────╯  ╰─────────────────╯
╭─────────────────╮
│ ○ [gemini] docs │
│ Generating API… │
╰─────────────────╯
○ idle  ● working  ◉ waiting  ✕ dead
←→↑↓/hjkl: navigate   enter/a: attach   x: kill   r: rename   G: all projects   esc/g/q: exit
```

| Key | Action |
|-----|--------|
| Arrow keys / `hjkl` | Navigate tiles |
| `Enter` / `a` | Attach to selected session |
| `x` | Kill selected session |
| `r` | Rename selected session |
| `G` | Switch to all-projects view |
| `g` / `Esc` / `q` | Exit grid |

## Session Titles

### Set manually

Select a session and press `r`. Type the new title and press `Enter`.

### Set by the agent

Any agent can set the session title by writing an OSC 2 escape sequence:

```bash
# Standard xterm title sequence — works in any terminal
printf '\033]2;My new title\007'

# Or use the Hive-specific marker (invisible in terminal output)
printf '\000HIVE_TITLE:My new title\000'
```

Hive polls for these sequences in the background and updates the sidebar automatically. User-set titles take precedence by default (configurable).

## Configuration

Config file: `~/.config/hive/config.json`

```json
{
  "schema_version": 1,
  "theme": "dark",
  "preview_refresh_ms": 500,
  "agent_title_overrides_user_title": false,
  "multiplexer": "native",
  "agents": {
    "claude":   { "cmd": ["claude"] },
    "codex":    { "cmd": ["codex"] },
    "gemini":   { "cmd": ["gemini"] },
    "copilot":  { "cmd": ["copilot"] },
    "aider":    { "cmd": ["aider"] },
    "opencode": { "cmd": ["opencode"] }
  },
  "team_defaults": {
    "orchestrator": "claude",
    "worker_count": 2,
    "worker_agent": "claude"
  },
  "hooks": {
    "enabled": true,
    "dir": "~/.config/hive/hooks"
  },
  "keybindings": {
    "new_project": "n",
    "new_session": "t",
    "new_team": "T",
    "kill_session": "x",
    "kill_team": "D",
    "rename": "r",
    "attach": "a",
    "nav_up": "k",
    "nav_down": "j",
    "nav_project_up": "K",
    "nav_project_down": "J",
    "filter": "/",
    "palette": "ctrl+p",
    "help": "?",
    "quit": "q",
    "quit_kill": "Q"
  }
}
```

### Multiplexer backends

| Value | Description |
|-------|-------------|
| `"native"` (default) | Built-in Go PTY backend. No external dependencies. A background daemon (`hive mux-daemon`) keeps sessions alive after the TUI exits. |
| `"tmux"` | Uses the external `tmux` binary. Requires tmux to be installed. Detach from a session with **Ctrl+B D**. |

### Custom agents

Add any CLI tool as a custom agent by extending the `agents` map:

```json
"agents": {
  "claude": { "cmd": ["claude"] },
  "my-llm": { "cmd": ["my-llm", "--interactive", "--model", "gpt-4o"] }
}
```

When you press `t` and choose "custom", Hive will show your custom agents.

## Hooks

Hive fires shell hooks on lifecycle events. Drop an executable script into `~/.config/hive/hooks/`:

```
~/.config/hive/hooks/
├── on-session-create
├── on-session-kill
├── on-session-attach
├── on-session-detach
├── on-session-title-changed
├── on-project-create
├── on-project-kill
├── on-team-create
├── on-team-kill
├── on-team-member-add
└── on-team-member-remove
```

Every hook receives `HIVE_*` environment variables:

| Variable | Example |
|----------|---------|
| `HIVE_EVENT` | `session-create` |
| `HIVE_PROJECT_NAME` | `my-project` |
| `HIVE_SESSION_TITLE` | `orchestrator` |
| `HIVE_AGENT_TYPE` | `claude` |
| `HIVE_TEAM_NAME` | `feature-x` |
| `HIVE_TEAM_ROLE` | `orchestrator` |
| `HIVE_WORK_DIR` | `/home/user/project` |
| `HIVE_TMUX_SESSION` | `hive-abc12345` |

### Example: macOS notification on new session

```bash
#!/bin/bash
# ~/.config/hive/hooks/on-session-create
osascript -e "display notification \"$HIVE_SESSION_TITLE\" with title \"Hive: New $HIVE_AGENT_TYPE session\""
```

### Multiple hooks per event

Create a `.d/` directory for multiple hooks on one event (run alphabetically):

```
~/.config/hive/hooks/on-session-create.d/
├── 01-notify.sh
└── 02-log.sh
```

Hooks have a 5-second timeout. Non-zero exit codes are logged to `~/.config/hive/hive.log` but never crash Hive.

See [docs/hooks.md](docs/hooks.md) for the full reference.

## Agent Teams

Agent teams coordinate multiple AI agents on a shared goal. See [docs/agent-teams.md](docs/agent-teams.md) for the full guide.

**Quick start:**
1. Press `T` in a project → fill in the wizard
2. Attach to the orchestrator (`a`), describe the task
3. Switch between worker sessions with `j/k` to monitor progress
4. Workers can signal completion: `printf '\033]2;DONE: auth module\007'`

## Headless Attach

Attach to a session by name without starting the TUI:

```bash
hive attach orchestrator
hive attach session-1
hive attach <session-id>
```

## Session Persistence

Closing the TUI (`q`) does **not** stop your agents. With the native backend, a lightweight background daemon (`hive mux-daemon`) keeps all agent processes running. Re-open Hive with `hive start` to reconnect — your sessions are exactly where you left them.

To kill everything on exit, use `Q` instead of `q`.

## Data Storage

| Path | Contents |
|------|----------|
| `~/.config/hive/config.json` | User configuration |
| `~/.config/hive/state.json` | Projects, teams, sessions |
| `~/.config/hive/mux.sock` | Native multiplexer daemon socket |
| `~/.config/hive/mux-daemon.log` | Native multiplexer daemon log |
| `~/.config/hive/hooks/` | Lifecycle hook scripts |
| `~/.config/hive/hive.log` | Error and hook output log |

## Architecture

Hive is built with:

- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)** — Elm-inspired MVU TUI framework
- **[Lip Gloss](https://github.com/charmbracelet/lipgloss)** — terminal styling and layout
- **[Bubbles](https://github.com/charmbracelet/bubbles)** — text input, list, key binding components
- **[creack/pty](https://github.com/creack/pty)** — PTY allocation for the native multiplexer
- **[Cobra](https://github.com/spf13/cobra)** — CLI subcommands

The native multiplexer runs as a background daemon (`hive mux-daemon`) that owns all PTY master file descriptors. The TUI communicates with it over a Unix domain socket using a length-prefixed JSON protocol. Session state flows: TUI → state reducers → mux backend (daemon client) → daemon process → PTY processes.

```
internal/
├── tui/        # Bubble Tea model and all UI components
├── mux/        # multiplexer abstraction
│   ├── native/ # built-in PTY backend (daemon + client)
│   └── tmux/   # tmux backend wrapper
├── tmux/       # tmux CLI wrappers (used by mux/tmux backend)
├── state/      # data model and reducer functions
├── config/     # configuration loading and defaults
├── escape/     # OSC 2 title sequence parser
└── hooks/      # shell hook runner
```

## Contributing

Bug reports and pull requests welcome.

## License

MIT

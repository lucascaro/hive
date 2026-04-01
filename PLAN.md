# Hive: Implementation Plan

## Overview

**Hive** is a terminal TUI application for managing multiple AI coding agent sessions (Claude, Codex, Gemini, Copilot, Aider, OpenCode, custom) across projects, with native support for Claude agent teams (orchestrator + workers).

**Stack:** Go 1.24 + Bubble Tea + Lip Gloss + tmux backend
**Module:** `github.com/lucascaro/hive`
**Config:** `~/.config/hive/`

---

## Data Hierarchy

```
Project (groups related work)
  └── Team (coordinated agent group, optional)
        └── Session (1:1 with tmux window)
  └── Session (standalone, no team)
```

---

## Project Structure

```
hive/
├── main.go
├── go.mod
├── cmd/
│   ├── root.go          # cobra root
│   ├── start.go         # `hive start` — launch TUI
│   ├── attach.go        # `hive attach [session-id]` — headless
│   └── version.go
├── internal/
│   ├── tui/
│   │   ├── app.go                   # Bubble Tea root Model
│   │   ├── keys.go                  # Key map (from config)
│   │   ├── messages.go              # All tea.Msg types
│   │   ├── layout.go                # Pane sizing
│   │   └── components/
│   │       ├── sidebar.go           # 3-level tree
│   │       ├── preview.go           # capture-pane refresh
│   │       ├── statusbar.go         # breadcrumb + hints
│   │       ├── titleedit.go         # textinput inline editor
│   │       ├── agentpicker.go       # agent type menu
│   │       ├── teambuilder.go       # team creation wizard
│   │       └── confirm.go           # yes/no dialog
│   │   └── styles/
│   │       ├── theme.go
│   │       └── adaptive.go
│   ├── tmux/
│   │   ├── client.go     # Exec() wrapper
│   │   ├── session.go    # CreateSession, KillSession, ListSessions
│   │   ├── window.go     # CreateWindow, KillWindow, RenameWindow
│   │   ├── capture.go    # CapturePane()
│   │   └── names.go      # hive-{projectShortID} naming
│   ├── state/
│   │   ├── model.go      # Project, Team, Session, AppState structs
│   │   ├── store.go      # Reducer functions
│   │   └── events.go     # Event type constants
│   ├── config/
│   │   ├── config.go     # Config struct
│   │   ├── load.go       # Load/save (atomic write)
│   │   ├── migrate.go    # Schema migration
│   │   └── defaults.go   # Default keybindings + agent profiles
│   ├── escape/
│   │   ├── parser.go     # OSC \033]2;title\007 parser
│   │   └── watcher.go    # Background title detection
│   └── hooks/
│       ├── runner.go     # Script discovery + exec with timeout
│       ├── events.go     # Event name constants
│       └── env.go        # HIVE_* env var injection
└── docs/
    ├── hooks.md
    ├── keybindings.md
    └── agent-teams.md
```

---

## TUI Layout

```
┌─────────────────────────────────────────────────────────────────┐
│ ▼ project-alpha             │ ╔══════ preview ════════════════╗ │
│   ▼ [team] feature-x        │ ║                               ║ │
│     ★ orchestrator [claude] │ ║  <tmux capture-pane output>   ║ │
│     ○ worker-1 [claude]     │ ║  ANSI passthrough, ~500ms     ║ │
│     ○ worker-2 [codex]      │ ║                               ║ │
│   ○ solo-session [gemini]   │ ╚═══════════════════════════════╝ │
│ ▶ project-beta              │                                   │
├─────────────────────────────┴───────────────────────────────────┤
│ project-alpha / feature-x / orchestrator [claude] [waiting]      │
│ q:quit  r:rename  a:attach  t:new  T:new-team  ?:help           │
└─────────────────────────────────────────────────────────────────┘
```

- **Sidebar:** 28% width, min 32 cols. Three-level collapsible tree.
- **Preview:** Remaining width. `tmux capture-pane -p -e -J` every 500ms.
- **Status bar:** 2 lines — breadcrumb + context key hints.
- **Responsive:** Sidebar collapses to icon-only below 80 cols.

---

## Key Bindings

| Key | Context | Action |
|-----|---------|--------|
| `j`/`k` | Sidebar | Navigate up/down |
| `J`/`K` | Sidebar | Jump between projects |
| `Tab` | Any | Toggle sidebar/preview focus |
| `Enter`/`a` | Session | Attach to session |
| `Space` | Project/Team | Toggle collapse |
| `n` | Any | New project |
| `t` | Project/Team | New session (agent picker) |
| `T` | Project | New agent team (wizard) |
| `r` | Session/Team | Inline title/goal edit |
| `d`/`x` | Session | Kill session (confirm) |
| `D` | Team | Kill entire team |
| `Ctrl+P` | Any | Command palette (fuzzy) |
| `/` | Sidebar | Filter by name |
| `1`-`9` | Any | Jump to project by index |
| `?` | Any | Help overlay |
| `q` | Any | Quit (sessions persist) |
| `Q` | Any | Quit + kill all sessions |

---

## Session Title System

### A. User shortcut (`r`)
- Sidebar renders `textinput.Model` inline for the selected session/team
- `Enter` → `MsgSessionTitleChanged{TitleSource: user}` → state + tmux rename

### B. Agent escape sequence
- Agent outputs `\033]2;New Title\007` (OSC 2 standard)
- Or custom marker: `\x00HIVE_TITLE:{title}\x00`
- Background watcher polls capture-pane, dispatches title change msg
- User-set titles take precedence by default (configurable)

---

## Agent Teams

### Team creation wizard (`T`)
1. Team name + goal
2. Orchestrator agent type (default: claude)
3. Number of workers + agent type per worker
4. Shared working directory
5. Confirm → all tmux windows created, all agents launched

### Sidebar display
```
▼ [team] feature-x  [3 agents: 1 waiting, 2 idle]
  ★ orchestrator [claude] [waiting]
  ○ worker-1     [claude] [idle]
  ○ worker-2     [codex]  [idle]
```

---

## tmux Naming

- tmux session: `hive-{projectID[:8]}`
- tmux window: `{sessionID[:8]}`
- tmux window title: `{Title} [{agentType}]`

---

## Hook Events

`session-create`, `session-kill`, `session-attach`, `session-detach`,
`session-title-changed`, `project-create`, `project-kill`,
`team-create`, `team-kill`, `team-member-add`, `team-member-remove`

**Env vars:** `HIVE_EVENT`, `HIVE_PROJECT_*`, `HIVE_SESSION_*`, `HIVE_TEAM_*`,
`HIVE_AGENT_TYPE`, `HIVE_AGENT_CMD`, `HIVE_TMUX_*`, `HIVE_WORK_DIR`, `HIVE_VERSION`

Scripts in `~/.config/hive/hooks/on-{event}` or `on-{event}.d/` dir.
5-second timeout per hook. Non-zero exit codes logged, never crash TUI.

---

## Implementation Phases

| Phase | Scope |
|-------|-------|
| 1 | Skeleton: go mod, cobra, config load/save, minimal Bubble Tea model |
| 2 | tmux CRUD, state store, startup reconciliation, persistence |
| 3 | Full TUI: sidebar + preview + status bar + key map + attach/detach |
| 4 | Agent types: picker, AgentType field, badges, config profiles |
| 5 | Agent teams: Team model, wizard, 3-level sidebar, team hooks |
| 6 | Title system: inline editor, OSC 2 parser, watcher |
| 7 | Hook system: discovery, runner, env injection |
| 8 | Polish: command palette, filter, help overlay, confirmations |
| 9 | Docs + release: README, docs/, goreleaser |

---

## Verification Checklist

- [ ] `go build ./...` — clean, no CGO
- [ ] `hive start` — TUI renders with empty state
- [ ] New project → `tmux ls` shows `hive-{id}`
- [ ] New session → agent picker → session badge shown
- [ ] New team → 3 sessions in tree with `★` orchestrator
- [ ] Attach → agent CLI appears, detach → TUI resumes
- [ ] Preview refreshes every ~500ms
- [ ] Rename → sidebar + `tmux list-windows` updated
- [ ] Agent OSC 2 output → title auto-updates
- [ ] Hook fires on session creation
- [ ] `go test ./...` passes

# Hive Developer Guide

This guide covers the internals of Hive for developers and AI agents working on the codebase. For user-facing documentation see `README.md`, and for coding guidelines see `AGENTS.md`.

## Project Overview

Hive is a terminal TUI (Bubble Tea / Elm-MVU) that manages multiple AI coding agent sessions across projects. It abstracts the underlying terminal multiplexer (native PTY daemon or tmux) behind a `Backend` interface.

```
module: github.com/lucascaro/hive
Go 1.25+, zero CGO, no runtime deps (native mode) or tmux (tmux mode)
```

---

## Package Reference

### `cmd/`

Cobra CLI entry points. All commands live here.

| File | Command | Notes |
|------|---------|-------|
| `root.go` | `hive` | Initialises Cobra; `--native` flag selects backend |
| `start.go` | `hive start` | Reconciles state, selects backend, launches TUI |
| `attach.go` | `hive attach [id]` | Headless attach (no TUI) |
| `mux_daemon.go` | `hive mux-daemon` | Internal: started by `start.go`, runs the PTY daemon |
| `version.go` | `hive version` | Prints version string |

`start.go` is the most important: it calls `config.Load()`, selects a `mux.Backend`, calls `mux.SetBackend()`, loads state, reconciles orphans, then starts the Bubble Tea program with `tui.New(cfg, appState)`.

---

### `internal/state/`

Pure data model + reducer functions. **No I/O, no side-effects.** Easily unit-tested.

#### Key types (`model.go`)

```go
AppState   // single source of truth; held inside tui.Model
Project    // ID, Name, Description, Color, Directory, Teams, Sessions
Team       // ID, ProjectID, Name, Goal, OrchestratorID, Sessions, SharedWorkDir
Session    // ID, ProjectID, TeamID, TeamRole, Title, AgentType, AgentCmd, WorkDir,
           //   TmuxSession, TmuxWindow, Status, TitleSource, WorktreePath
```

Enum types: `AgentType`, `SessionStatus`, `TeamRole`, `TitleSource`, `Pane`, `GridRestoreMode`

`Team.TeamStatus()` derives aggregate status from members (any waiting → waiting; any running → running; all dead → dead).

`AppState.ActiveSession()` / `ActiveProject()` — convenience helpers.

#### Reducers (`store.go`)

All state mutations are pure functions:

```go
CreateProject(state, name, description, color, directory) (*AppState, *Project)
RemoveProject(state, projectID) *AppState
CreateSession(state, projectID, title, agentType, agentCmd, workDir, tmuxSession, tmuxWindow) (*AppState, *Session)
CreateTeamSession(state, projectID, teamID, role, ...) (*AppState, *Session)
RemoveSession(state, sessionID) *AppState
CreateTeam(state, projectID, name, goal, sharedWorkDir) (*AppState, *Team)
RemoveTeam(state, teamID) *AppState
UpdateSessionTitle(state, sessionID, title, source) *AppState
UpdateSessionStatus(state, sessionID, status) *AppState
// ... see store.go for full list
```

**Rule:** Only mutate `AppState` inside Bubble Tea's `Update()` by calling these reducers. Never mutate state fields directly.

#### Hook events (`events.go`)

`HookEvent` struct carries `Name`, `ProjectID`, `SessionID`, `TeamID`, etc. Constants like `EventSessionCreate`, `EventTeamKill`, etc. are defined here.

---

### `internal/config/`

Reads and writes `~/.config/hive/config.json`. Migrations in `migrate.go` handle schema version bumps.

#### Key functions (`load.go`)

```go
Dir() string          // ~/.config/hive  (or $HIVE_CONFIG_DIR)
ConfigPath() string   // ~/.config/hive/config.json
StatePath() string    // ~/.config/hive/state.json
LogPath() string      // ~/.config/hive/hive.log
HooksPath() string    // ~/.config/hive/hooks/
Ensure() error        // creates dirs on first run
Load() (Config, error)
Save(cfg Config) error  // atomic: write .tmp then os.Rename
```

#### `Config` struct (`config.go`)

```go
Config{
  SchemaVersion               int
  Theme                       string
  PreviewRefreshMs            int           // default 500
  AgentTitleOverridesUserTitle bool
  Multiplexer                 string        // "tmux" | "native"
  Agents                      map[string]AgentProfile  // keyed by agent name
  TeamDefaults                TeamDefaultsConfig
  Hooks                       HooksConfig
  Keybindings                 KeybindingsConfig
}
AgentProfile{ Cmd []string, InstallCmd []string }
```

`DefaultConfig()` in `defaults.go` defines the built-in agent profiles (claude, codex, gemini, etc.) and default keybindings.

---

### `internal/mux/`

Backend abstraction. Call `mux.SetBackend(b)` once at startup. All other `mux.*` functions delegate to the active backend.

#### `Backend` interface (`interface.go`)

```go
IsAvailable() bool
IsServerRunning() bool
CreateSession(session, windowName, workDir string, cmd []string) error
SessionExists(session string) bool
KillSession(session string) error
ListSessionNames() ([]string, error)
CreateWindow(session, windowName, workDir string, cmd []string) (int, error)
WindowExists(target string) bool
KillWindow(target string) error
RenameWindow(target, newName string) error
ListWindows(session string) ([]WindowInfo, error)
CapturePane(target string, lines int) (string, error)      // rendered output
CapturePaneRaw(target string, lines int) (string, error)   // with escape sequences
Attach(target string) error
DetachKey() string
```

#### Utility functions

```go
mux.SessionName(projectID string) string  // "hive-{projectID[:8]}"
mux.WindowName(sessionID string) string   // sessionID[:8]
mux.Target(session string, idx int) string  // "session:index"
```

---

### `internal/mux/native/`

Built-in PTY backend. A background daemon process (`hive mux-daemon`) owns all PTY file descriptors. The TUI communicates with it via a Unix domain socket.

#### Architecture

```
TUI process ──── Unix socket (~/.config/hive/mux.sock) ──── daemon process
  client.go                     protocol.go                  daemon.go + manager.go
```

- **`daemon.go`** — `RunDaemon()`: creates socket, handles connections, dispatches to `manager.go`
- **`manager.go`** — owns `Pane` instances (PTY + subprocess), handles all operations
- **`protocol.go`** — wire format: 4-byte big-endian length header + JSON body; `Request` and `Response` structs
- **`client.go`** — `Client` sends `Request`, reads `Response`; implements `Backend`
- **`backend.go`** — wraps `Client`, implements the `mux.Backend` interface
- **`attach.go`** — `Attach()` connects the current terminal to a PTY pane
- **`pane.go`** — `Pane` struct: PTY master fd + subprocess + output ring buffer

**Socket path:** `~/.config/hive/mux.sock` (or `$HIVE_CONFIG_DIR/mux.sock`)

**Protocol:** Each message is: `[4-byte big-endian uint32 length][JSON bytes]`. Max message 4 MiB.

**Request ops:** `create-session`, `session-exists`, `kill-session`, `list-sessions`, `create-window`, `window-exists`, `kill-window`, `rename-window`, `list-windows`, `capture-pane`, `capture-pane-raw`

---

### `internal/mux/tmux/`

Delegates to the `tmux` binary. `backend.go` implements `mux.Backend` by shelling out to `tmux` CLI commands. Uses helpers from `internal/tmux/`.

---

### `internal/tmux/`

Low-level tmux CLI wrappers. Used by `mux/tmux/`.

| File | Contents |
|------|---------|
| `session.go` | `New`, `Kill`, `Exists`, `List` |
| `window.go` | `NewWindow`, `KillWindow`, `Rename`, `List` |
| `capture.go` | `Capture`, `CaptureRaw` |
| `client.go` | `Attach` |
| `names.go` | Name/target formatting helpers |

---

### `internal/tui/`

Root Bubble Tea model and all supporting types.

#### Elm MVU Pattern

Every Bubble Tea program follows Init/Update/View:

```go
func (m Model) Init() tea.Cmd           // returns initial commands (tickers, etc.)
func (m Model) Update(tea.Msg) (tea.Model, tea.Cmd)  // handles all messages
func (m Model) View() string            // pure render from current state
```

All state mutation happens **only inside `Update()`**. Components (`Sidebar`, `Preview`, etc.) follow the same pattern at a smaller scale.

#### Key files

| File | Contents |
|------|---------|
| `app.go` | `Model` struct, `New()`, `Init()`, `Update()`, `View()` |
| `messages.go` | All `tea.Msg` types used across the app |
| `keys.go` | `KeyMap` struct, loaded from config |
| `layout.go` | `TermSizeMsg`, sizing logic, `PaneWidths()` |
| `persist.go` | `SaveState()` / `LoadState()` for `state.json` |

#### `Model` struct (abbreviated, `app.go`)

```go
type Model struct {
    cfg         config.Config
    appState    state.AppState
    keys        KeyMap
    sidebar     components.Sidebar
    preview     components.Preview
    statusBar   components.StatusBar
    titleEditor components.TitleEditor
    agentPicker components.AgentPicker
    teamBuilder components.TeamBuilder
    confirm     components.Confirm
    gridView    components.GridView
    orphanPicker components.OrphanPicker
    settings    components.SettingsView
    nameInput   textinput.Model
    inputMode   string  // "project-name" | "project-dir" | ... | ""
}
```

#### Messages (`messages.go`)

| Message | When sent |
|---------|-----------|
| `SessionCreatedMsg` | After session spawned in mux |
| `SessionKilledMsg` | After session window removed |
| `SessionAttachMsg` | Triggers attach (suspends TUI) |
| `SessionDetachedMsg` | User returns from attached session |
| `SessionTitleChangedMsg` | Title updated (user or agent) |
| `SessionStatusChangedMsg` | Session status changed |
| `TeamCreatedMsg` | After team + sessions created |
| `TeamKilledMsg` | After team removed |
| `ProjectCreatedMsg` | After project created |
| `ProjectKilledMsg` | After project removed |
| `ErrorMsg` | Non-fatal error for status bar |
| `ConfirmActionMsg` | Requests yes/no dialog |
| `ConfirmedMsg` | User confirmed an action |
| `PersistMsg` | Triggers state write to disk |
| `QuitAndKillMsg` | Quit + kill all sessions |
| `AgentInstalledMsg` | Agent install completed |
| `CleanOrphansMsg` | User confirmed orphan cleanup |
| `ConfigSavedMsg` | Config written to disk |

---

### `internal/tui/components/`

Each component follows Bubble Tea's component pattern:
- A struct holding component-local state
- `Update(tea.Msg) (ComponentType, tea.Cmd)` 
- `View() string` that renders using Lip Gloss

| File | Component | Responsibility |
|------|-----------|---------------|
| `sidebar.go` | `Sidebar` | Three-level project/team/session tree |
| `preview.go` | `Preview` | Live session output (ANSI passthrough) |
| `statusbar.go` | `StatusBar` | Breadcrumb + context key hints |
| `gridview.go` | `GridView` | Tiled grid of sessions for a project or all |
| `agentpicker.go` | `AgentPicker` | Agent type selection menu |
| `teambuilder.go` | `TeamBuilder` | Multi-step team creation wizard |
| `titleedit.go` | `TitleEditor` | Inline rename text input |
| `confirm.go` | `Confirm` | Yes/no confirmation overlay |
| `settings.go` | `SettingsView` | Settings/config overlay |
| `orphanpicker.go` | `OrphanPicker` | Orphaned tmux session cleanup overlay |

---

### `internal/tui/styles/`

`theme.go` defines all Lip Gloss styles used across the app. Import `styles.Theme` and use the exported style variables. Never hardcode colours in component files.

---

### `internal/hooks/`

Discovers and runs executable scripts in the hooks directory.

```go
hooks.Run(hooksDir string, event state.HookEvent) []error
```

- Checks `on-{event}` (flat file) and `on-{event}.d/` (directory, alphabetical order)
- Sets `HIVE_*` environment variables (see `env.go` / `docs/hooks.md`)
- 5-second timeout per script; errors are non-fatal
- Called from `tui/app.go` after state mutations

---

### `internal/escape/`

- `parser.go` — `ParseTitle(raw string) (title string, found bool)`: extracts title from OSC 2 or Hive marker sequences
- `watcher.go` — `Watcher` polls `CapturePaneRaw` every 500ms and dispatches `SessionTitleChangedMsg` when a title sequence is detected

---

### `internal/git/`

Optional git worktree helpers. `CreateWorktree(repoPath, branch string) (worktreePath string, error)` and related helpers for per-session isolated working trees.

---

## Data Flows

### 1. Startup

```
main.go: cmd.Execute()
  → cmd/start.go: config.Load()
  → select backend (--native flag or config.Multiplexer)
  → mux.SetBackend(backend)
  → load state.json (persist.LoadState)
  → reconcileState: match state.Sessions to live mux sessions, collect orphans
  → tui.New(cfg, appState)
  → tea.NewProgram(model).Run()
```

### 2. New Session Creation

```
User presses `t` (new_session key)
  → app.go Update() sets inputMode = "new-session"
  → AgentPicker shown
  → User selects agent → AgentPicker returns selected AgentType
  → app.go creates Session via state.CreateSession(...)
  → calls mux.CreateWindow(tmuxSession, windowName, workDir, agentCmd)
  → fires hooks.Run("session-create", event)
  → dispatches SessionCreatedMsg
  → app.go Update() handles SessionCreatedMsg: updates appState, dispatches PersistMsg
```

### 3. State Persistence

```
Any mutation dispatches PersistMsg
  → app.go Update() handles PersistMsg
  → persist.SaveState(appState) writes state.json (atomic: .tmp + Rename)
```

### 4. Preview Refresh

```
tea.Tick(previewRefreshMs)
  → app.go Update() calls mux.CapturePane(target, 0)
  → appState.PreviewContent = captured output
  → components/preview.go View() renders ANSI content via lipgloss
```

---

## Testing

### Approach

- **Unit tests**: `state/`, `config/`, `escape/`, `hooks/`, `git/`, `tmux/` — test individual functions, no mocking needed for pure logic
- **Component tests**: `tui/components/*_test.go` — create component, call `Update()` + `View()`, assert output strings
- **Integration**: `mux/native/manager_test.go` — tests the PTY manager with real PTYs

### Run tests

```bash
go test ./...          # all tests
go test ./internal/state/...  # specific package
go test -run TestName ./...   # specific test
```

### Common patterns

```go
// State reducer test
func TestCreateSession(t *testing.T) {
    state := &state.AppState{Projects: []*state.Project{{ID: "p1"}}}
    newState, sess := state.CreateSession(state, "p1", "my-sess", state.AgentClaude, ...)
    // assert sess fields and newState
}

// Config test with temp dir
func TestLoad(t *testing.T) {
    t.Setenv("HIVE_CONFIG_DIR", t.TempDir())
    cfg, err := config.Load()
    // assert defaults
}
```

---

## Coding Conventions

- **Imports**: stdlib → external → internal (blank line between groups)
- **Error handling**: return errors; don't log in library code; log in cmd/tui layers
- **State**: only mutate `AppState` via `state/store.go` reducers called from `Update()`
- **Lip Gloss**: all styles from `tui/styles/theme.go`; never hardcode ANSI codes
- **Comments**: only when clarification is needed; exported types/functions always have doc comments
- **No global state** except `mux.active` (set once at startup)

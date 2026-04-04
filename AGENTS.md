# Hive – AI Coding Guidelines

## Codebase Quick Reference

### Module & Build

```
module: github.com/lucascaro/hive
build:  go build ./...
test:   go test ./...
binary: hive
```

### Package Map

| Package | Path | Purpose |
|---------|------|---------|
| `main` | `main.go` | Entry point; calls `cmd.Execute()` |
| `cmd` | `cmd/` | Cobra CLI commands (`start`, `attach`, `mux-daemon`, `version`) |
| `tui` | `internal/tui/` | Bubble Tea root model (`Model`), Elm Update/View loop |
| `tui/components` | `internal/tui/components/` | All UI components (sidebar, preview, statusbar, etc.) |
| `tui/styles` | `internal/tui/styles/` | Lip Gloss colour theme and shared styles |
| `state` | `internal/state/` | Pure data model + reducer functions; no I/O |
| `config` | `internal/config/` | Load/save `~/.config/hive/config.json`; atomic writes |
| `mux` | `internal/mux/` | `Backend` interface + package-level forwarding functions |
| `mux/native` | `internal/mux/native/` | Built-in PTY daemon (Unix socket, JSON protocol) |
| `mux/tmux` | `internal/mux/tmux/` | tmux binary backend |
| `tmux` | `internal/tmux/` | Low-level tmux CLI wrappers |
| `hooks` | `internal/hooks/` | Shell hook runner (`~/.config/hive/hooks/on-{event}`) |
| `escape` | `internal/escape/` | OSC 2 / Hive title marker parser + background watcher |
| `git` | `internal/git/` | Git worktree helpers |

### Key Types

```go
// internal/state/model.go
AppState          // single source of truth for the TUI; in-process no lock needed (BubbleTea is single-threaded); cross-process safety via state.json.lock + mtime watcher
Project           // groups sessions; has ID, Name, Teams, Sessions
Team              // orchestrator + workers; has OrchestratorID, Sessions, SharedWorkDir
Session           // maps 1:1 to a mux window; has AgentType, Status, TmuxSession, TmuxWindow
AgentType         // string enum: "claude", "codex", "gemini", "copilot", "aider", "opencode", "custom"
SessionStatus     // string enum: "running", "idle", "waiting", "dead"
TeamRole          // string enum: "orchestrator", "worker", "standalone"
TitleSource       // string enum: "auto", "user", "agent"

// internal/config/config.go
Config            // user config: Agents map, Keybindings, TeamDefaults, Hooks, Multiplexer
AgentProfile      // Cmd []string, InstallCmd []string

// internal/mux/interface.go
Backend           // interface: CreateSession, CreateWindow, CapturePane, Attach, …

// internal/tui/app.go
Model             // root Bubble Tea model; holds AppState + all components
```

### Key Data Flows

**Session creation (keypress → tmux/PTY)**
```
User presses `t`
  → AgentPicker component returns selected agent
  → tui/app.go creates Session in state (state.CreateSession)
  → calls mux.CreateWindow(tmuxSession, windowName, workDir, agentCmd)
  → fires hooks.Run("session-create", event)
  → dispatches SessionCreatedMsg back to Update()
```

**Preview refresh (ticker → screen)**
```
tea.Tick(500ms)
  → mux.CapturePane(target, lines)
  → AppState.PreviewContent updated
  → components/preview.go View() renders ANSI content
```

**Title change (agent escape → sidebar)**
```
escape.Watcher polls CapturePaneRaw every 500ms
  → detects OSC 2 (\033]2;title\007) or \x00HIVE_TITLE:...\x00
  → dispatches SessionTitleChangedMsg{SessionID, Title, Source}
  → app.go Update() calls state.UpdateSessionTitle
  → sidebar re-renders with new title
```

### Common Change Patterns

**Add a new `tea.Msg` type:**
1. Define the struct in `internal/tui/messages.go`
2. Add a `var _ tea.Msg = MyMsg{}` compile-time check at the bottom
3. Handle it in `internal/tui/app.go` `Update()` switch

**Add a new TUI component:**
1. Create `internal/tui/components/mycomp.go` with a struct implementing `Update(tea.Msg) (MyComp, tea.Cmd)` and `View() string`
2. Add it as a field on `tui.Model` in `app.go`
3. Route messages to it in `app.go`'s `Update()`
4. Call `View()` in `app.go`'s `View()`

**Add a new CLI subcommand:**
1. Create `cmd/mycmd.go` with a `cobra.Command`
2. Call `mux.SetBackend(...)` in `RunE` if the command needs terminal sessions
3. Register with `rootCmd.AddCommand(myCmd)` in `cmd/root.go`

**Add a state mutation:**
1. Add a reducer function to `internal/state/store.go` — takes `*AppState` + params, mutates and returns `*AppState`
2. Call it from `tui/app.go`'s `Update()` (only place state should be mutated)

**Add a hook event:**
1. Add a constant to `internal/state/events.go`
2. Call `hooks.Run(cfg.Hooks.Dir, state.HookEvent{...})` in the relevant app.go handler
3. Document the new event in `docs/hooks.md`

### Testing Conventions

- **`internal/state/`** — pure unit tests, no I/O mocking needed
- **`internal/config/`** — tests use `t.TempDir()` for isolation
- **`internal/tui/`** — component tests use `tea.NewProgram` with a fake model or direct `Update()` calls
- **`internal/tui/` tick intervals** — always set `cfg.PreviewRefreshMs = 1` in test helpers to avoid blocking on real-time `tea.Tick` intervals (default 500ms). Tests should verify behaviour, not wait on timers.
- Run all tests: `go test ./...`
- Tests live alongside source (e.g., `model_test.go` next to `model.go`)

---


## UX Best Practices

Always apply these principles when adding or modifying UI elements in the TUI:

### Key Discoverability
- **Always show the key next to the action it triggers.** If a number, letter, or chord activates something, display it inline — e.g. `[1] ProjectName`, `(n) new`, `[enter] attach`.
- Never rely on the user memorizing bindings from the help screen alone. Hints should be visible at point of use.
- When screen space is tight, abbreviate the hint but keep it present (e.g. `[1]` rather than removing it).

### Navigation Context
- Show positional context so users know where they are: active item should be clearly marked (current `←` indicator for sessions is good; keep it).
- Numbered shortcuts (1–9 for projects) must have their number shown in the sidebar label so the mapping is obvious without opening help.

### Status Visibility
- Status dots/badges must appear on every session row — never omit them to save space.
- Agent type badges should always accompany session labels.

### Feedback on Action
- Destructive actions (kill, quit+kill) must always go through the confirm overlay — never skip it.
- Overlays must display the relevant keybinding to confirm and cancel (`y/enter` to confirm, `esc/n` to cancel).

### Consistency
- Key hints use the format `[key]` for number/symbol keys and `(key)` for letter keys — pick one format and apply it uniformly across the whole UI.
- Help text in the status bar and help overlay must stay in sync with actual bindings.

### Information Density
- Prefer showing information inline over requiring a modal/overlay for basic facts (e.g. project number, agent type, session status).
- Reserve overlays for actions that need confirmation or multi-field input.

## Documentation Maintenance

Keep project documentation accurate and up-to-date as part of every code change. Stale docs are a bug.

### Changelog (`CHANGELOG.md`)

- **Every meaningful commit or push must update the `[Unreleased]` section** of `CHANGELOG.md`.
- Classify entries under the appropriate subsection: `Added`, `Changed`, `Fixed`, `Removed`, or `Security`.
- Use concise, user-facing language — describe what changed and why it matters, not internal implementation details.
- Do **not** create a new versioned section; only append to `[Unreleased]`. Versioning happens at release time.
- Skip purely cosmetic or internal refactors that have no user-visible effect (e.g. renaming a private variable). Use judgment.

### Architecture (`ARCHITECTURE.md`)

Update `ARCHITECTURE.md` whenever a change is **significant** — meaning any of the following:
- A new package is added or an existing one is removed/renamed.
- A major interface, abstraction boundary, or data-flow path changes.
- New components are added to the TUI layer (new files in `internal/tui/components/`).
- The `cmd/` command set changes (new subcommands, removed subcommands).
- The multiplexer, state, config, or hook subsystems change in a way that affects the high-level description.

Minor changes (bug fixes, adding a field to an existing struct, small refactors) do **not** require architecture updates.

### README and other docs

- Update `README.md` when **user-visible features, CLI flags, configuration options, or default behaviour** change.
- Update the relevant file in `docs/` when the subsystem it documents changes:
  - `docs/agent-teams.md` — multi-agent team behaviour
  - `docs/hooks.md` — hook events or environment variables
  - `docs/keybindings.md` — key bindings or navigation
  - `docs/features.md` — high-level feature descriptions
  - `docs/design-decisions.md` — only when a significant architectural decision is made
- If a doc file becomes incorrect after your change, fix it in the same commit.

## Releasing

Use the release script to publish a new version:

```bash
./scripts/release.sh <version>    # e.g. ./scripts/release.sh 0.3.0
```

The script handles everything: version bump (`cmd/version.go`), changelog stamp, commit, tag, cross-compilation (darwin arm64/amd64, linux amd64/arm64, windows amd64), GitHub release with attached binaries, and push.

**Prerequisites:** clean working tree, `gh` CLI authenticated, `[Unreleased]` section in CHANGELOG.md.

**Version scheme:** [Semantic Versioning](https://semver.org/) — bump minor for new features, patch for bug fixes.

## Feature Pipeline

Hive uses a local feature tracking system in `features/` linked to GitHub issues. Features are managed via slash commands (skills) that guide each stage.

### Slash Commands

| Command | Purpose |
|---------|---------|
| `/feature-next` | Show pipeline status and recommend next action |
| `/feature-ingest <issue>` | Ingest a GitHub issue into the pipeline |
| `/feature-triage [issue]` | Classify, estimate complexity, set priority |
| `/feature-research [issue]` | Explore codebase, document findings |
| `/feature-plan [issue]` | Write implementation plan |
| `/feature-implement [issue]` | Code, test, open PR |

### Working on Features

1. **Find the next feature:** Run `/feature-next` or read `features/BACKLOG.md`. The top row in the Active table is the highest priority.
2. **Advance the stage:** Run the appropriate `/feature-*` command. It will update the feature file, BACKLOG.md, and GitHub labels.
3. **One feature at a time.** Finish the current stage before moving to the next. Do not skip stages.

### Stage Workflow

- **TRIAGE** — Classify (bug/enhancement), set complexity (S/M/L), accept or reject, set priority in BACKLOG.md.
- **RESEARCH** — Explore relevant code, document findings in the feature file's Research section. For deep dives, create `research/<slug>/RESEARCH.md`.
- **PLAN** — Write implementation steps, files to change, test strategy, risks. Get user approval before advancing.
- **IMPLEMENT** — Create branch, code, test, update CHANGELOG.md and docs per the Documentation Maintenance rules above, open PR referencing `Fixes #<number>`.
- **DONE** — After PR merge, move file to `features/completed/`, update BACKLOG.md (remove from Active, add to Completed).

### GitHub Labels

Each stage has a corresponding label applied to the GitHub issue: `triaged`, `researching`, `planned`, `implementing`. The `/feature-*` commands manage these automatically.

### Ingesting New Issues

Run `/feature-ingest <issue-number>` or manually create a feature file from the template at `features/templates/FEATURE.md`. Always set initial stage to TRIAGE.

# Hive – AI Coding Guidelines

> **Branch model (as of 2026-05-02):** `main` is **Hive v2** — the
> Wails GUI + `hived` daemon rewrite (`cmd/hivegui/`, `cmd/hived/`,
> `internal/wire/`, `internal/worktree/`). v1 (TUI, Bubble Tea, tmux
> backend) lives on `release/v1` for bug-fix-only maintenance.
> Forward-port shared-package fixes (`internal/config`, `internal/registry`,
> `internal/agent`, `internal/notify`, `internal/worktree`) from
> `release/v1` → `main` via cherry-pick; never merge wholesale.
>
> ⚠️ Sections below describing the TUI / Bubble Tea / `internal/tui/`
> still need a full rewrite for v2 — treat them as historical until
> updated. See `docs/native-rewrite/PLAN.md` for v2 architecture.

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
| `audio` | `internal/audio/` | Terminal-bell dispatcher; embedded WAVs + platform audio-tool shell-out |
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

- **TDD — tests come with every change.** Never ship a bug fix, new feature, or behaviour change without adding or updating tests that would have caught the regression or verify the new behaviour. If you're in a hurry, write the test first.
- **"Boil the lake" philosophy — do more now, not later.** When fixing a bug, also add the test that would have caught it. When adding a feature, cover the golden path and key edge cases. Do not defer test coverage to a follow-up. Address all code review feedback in the same PR rather than deferring to follow-ups. **Auto-fix every high-confidence, low-risk review finding in the same PR** — minor code-review nits (comments, constants, helper extraction, API consistency fixes that don't change behaviour) must be applied in the PR where they are raised, not left for "later." Only defer when the fix is high-risk (behaviour change, cross-cutting refactor) or low-confidence (taste, unclear improvement).
- **All changes require both unit tests and functional tests.** Unit tests verify pure logic (state reducers, helpers). Functional tests verify end-to-end behaviour through the TUI using the `flowRunner` pattern in `internal/tui/flow_test.go`.
- **`internal/state/`** — pure unit tests, no I/O mocking needed
- **`internal/config/`** — tests use `t.TempDir()` for isolation
- **`internal/tui/`** — component tests use `tea.NewProgram` with a fake model or direct `Update()` calls
- **`internal/tui/` functional tests** — use `flowRunner` from `flow_test.go`: `testFlowModel()` creates an isolated Model with mock backend; `SendKey()`/`SendSpecialKey()` simulate input; assertion helpers like `ViewContains()`, `AssertActiveSession()`, `AssertGridActive()` verify outcomes. New features must include flow tests covering the golden path and key edge cases.
- **`internal/tui/` tick intervals** — always set `cfg.PreviewRefreshMs = 1` in test helpers to avoid blocking on real-time `tea.Tick` intervals (default 500ms). Tests should verify behaviour, not wait on timers.
- **`muxtest.MockBackend`** — use `SetUseExecAttach(true)` to exercise the `tea.ExecProcess` attach path; inject `model.attachOut = &bytes.Buffer{}` to capture pre-attach escape sequences.
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

## Keybindings Policy

Every key binding change must update all four surfaces — omitting any one creates drift that confuses users and other contributors.

### Required updates for any new or changed keybinding

1. **Config field + default** — add or update the field in `KeybindingsConfig` (`internal/config/config.go`) and set the default in `internal/config/defaults.go`. Use `KeyBinding` ([]string) type so users can bind multiple keys.
2. **Settings UI field** — add a `keybindField(...)` entry in `internal/tui/components/settings.go` under the Keybindings tab.
3. **Documentation** — add or update the row in `docs/keybindings.md`.
4. **Changelog** — add a user-facing entry under `[Unreleased]` in `CHANGELOG.md` if the change affects default behavior.

### Hard-coded exceptions (NOT configurable)

These keys are intentionally hard-coded and must not be moved into config:
- `ctrl+c` — force-quit (universal safety)
- `y`, `enter`, `esc`, `n` — dialog confirm/cancel
- `d` — "don't show again" in hint overlays
- `s`, `R`, `esc` — settings modal save/reset/cancel
- Modal overlay navigation (`up/down/left/right/j/k`) in settings, help panel, and pickers

### Routing pattern

Use `key.Matches(msg, km.Action)` for all configurable bindings — never `msg.String() == "x"` for actions that should be rebindable. Literal `msg.String()` checks are reserved for the hard-coded exceptions above.

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

These skills are provided by [hivesmith](https://github.com/lucascaro/hivesmith) and installed globally as `hs-*` commands.

### Slash Commands

| Command | Purpose |
|---------|---------|
| `/hs-feature-next` | Show pipeline status and recommend next action |
| `/hs-feature-ingest <issue>` | Ingest a GitHub issue into the pipeline |
| `/hs-feature-triage [issue]` | Classify, estimate complexity, set priority |
| `/hs-feature-research [issue]` | Explore codebase, document findings |
| `/hs-feature-plan [issue]` | Write implementation plan |
| `/hs-feature-implement [issue]` | Code, test, open PR |
| `/hs-feature-loop [issue]` | Drive a feature through the full pipeline end-to-end |
| `/hs-review-pr` | Deep PR review (correctness, safety, UX, consistency) |
| `/hs-release` | Cut a release with pre-flight checks and version bump |

### Working on Features

1. **Find the next feature:** Run `/hs-feature-next` or read `features/BACKLOG.md`. The top row in the Active table is the highest priority.
2. **Advance the stage:** Run the appropriate `/hs-feature-*` command. It will update the feature file, BACKLOG.md, and GitHub labels.
3. **One feature at a time.** Finish the current stage before moving to the next. Do not skip stages.

### Stage Workflow

- **TRIAGE** — Classify (bug/enhancement), set complexity (S/M/L), accept or reject, set priority in BACKLOG.md.
- **RESEARCH** — Explore relevant code, document findings in the feature file's Research section. For deep dives, create `research/<slug>/RESEARCH.md`.
- **PLAN** — Write implementation steps, files to change, test strategy, risks. Get user approval before advancing.
- **IMPLEMENT** — Create branch, code, test, update CHANGELOG.md and docs per the Documentation Maintenance rules above, open PR referencing `Fixes #<number>`.
- **DONE** — After PR merge, move file to `features/completed/`, update BACKLOG.md (remove from Active, add to Completed).

### GitHub Labels

Each stage has a corresponding label applied to the GitHub issue: `triaged`, `researching`, `planned`, `implementing`. The `/hs-feature-*` commands manage these automatically.

### Ingesting New Issues

Run `/hs-feature-ingest <issue-number>` or manually create a feature file from the template at `features/templates/FEATURE.md`. Always set initial stage to TRIAGE.

## Skill routing

When the user's request matches an available skill, ALWAYS invoke it using the Skill
tool as your FIRST action. Do NOT answer directly, do NOT use other tools first.
The skill has specialized workflows that produce better results than ad-hoc answers.

Key routing rules:
- Product ideas, "is this worth building", brainstorming → invoke office-hours
- Bugs, errors, "why is this broken", 500 errors → invoke investigate
- Ship, deploy, push, create PR → invoke ship
- QA, test the site, find bugs → invoke qa
- Code review, check my diff → invoke review
- Update docs after shipping → invoke document-release
- Weekly retro → invoke retro
- Design system, brand → invoke design-consultation
- Visual audit, design polish → invoke design-review
- Architecture review → invoke plan-eng-review
- Save progress, checkpoint, resume → invoke checkpoint
- Code quality, health check → invoke health

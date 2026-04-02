# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Agent context documentation**: added `docs/developer-guide.md` with a full
  package-by-package reference, key data flows, testing conventions, and common
  change patterns so AI agents and new contributors can understand the codebase
  without reading every source file.
- **`doc.go` package comments**: added `doc.go` files to all 12 internal packages
  (`state`, `config`, `mux`, `mux/native`, `mux/tmux`, `tmux`, `tui`,
  `tui/components`, `tui/styles`, `hooks`, `escape`, `git`) describing each
  package's purpose, key types, and relationships.
- **`AGENTS.md` codebase reference**: added a "Codebase Quick Reference" section
  to `AGENTS.md` covering the module path, package map, key types, key data flows,
  and common change patterns (add a message, component, command, state mutation, hook).
- **`ARCHITECTURE.md` enhancements**: added sections for key message types, state
  mutation rules, backend selection logic, and the native daemon Unix socket protocol.
- **Orphan session cleanup**: on startup, hive now detects `hive-*` tmux session
  containers that have no matching project in state and no active windows (empty
  orphans from crashes or prior unclean exits). An interactive multi-select overlay
  lets the user choose which ones to remove — `↑/↓` to navigate, `space` to toggle,
  `a` to toggle all, `enter` to confirm, `esc` to skip without touching anything.
- **Auto-generated session names**: new sessions are given memorable
  adjective-noun names (e.g. `bright-spark`, `pale-snow`) instead of generic
  numbered ones, making it easier to identify sessions at a glance.

### Fixed
- `Q` (quit + kill all) now shows a confirmation dialog before executing, consistent
  with the behaviour of all other destructive actions.
- After killing a session, team, or all sessions, the corresponding tmux session
  container (one per project) is now also destroyed when its last window is removed.
  Previously, empty `hive-*` tmux containers accumulated silently in the background,
  causing the count shown by `tmux ls` to diverge from the hive UI.

## [0.1.0] — 2026-04-01

Initial public release.

### Added
- **TUI** — full-featured terminal UI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea):
  sidebar, live preview pane, status bar, and responsive layout that adapts to
  narrow terminals.
- **Projects** — group agent sessions into named, colour-coded projects.
- **Agent teams** — native multi-agent team support with orchestrator + worker
  layout; teams are collapsible in the sidebar.
- **Multiple agent backends** — Claude, Codex, Gemini, GitHub Copilot CLI, Aider,
  OpenCode, and arbitrary custom commands.
- **tmux & native backends** — use tmux (default) or the built-in native PTY daemon
  (`hive start --native`) as the multiplexer backend.
- **Session persistence** — sessions survive TUI restarts; `hive start` reconnects
  to all running agents.
- **Git worktree sessions** — create a session in a dedicated git worktree so agents
  work on isolated branches without interfering with each other.
- **Grid view** — see multiple session previews simultaneously; toggle with `g`.
- **Live preview** — 500 ms refresh with full ANSI colour passthrough and viewport
  scrolling.
- **Session title editing** — rename sessions inline with `r`; agents can also set
  titles via OSC escape sequences.
- **Extensible hooks** — shell scripts triggered on 11 lifecycle events
  (`session.create`, `session.kill`, `project.create`, `team.create`, etc.) with
  rich `HIVE_*` environment variables.
- **Vim-style navigation** — `j/k` within a project, `J/K` across projects,
  `1`–`9` to jump directly to a numbered project.
- **Keybinding customisation** — override any key in `~/.config/hive/config.json`.
- **State reconciliation** — on startup, sessions whose tmux window no longer
  exists are automatically removed from state (including git worktree cleanup).
- **`hive attach`** — attach directly to a session by tmux session/window from the
  command line.

### Fixed
- Robust ANSI sanitisation to prevent colour bleed between sessions.
- Tab expansion in preview content to prevent line wrapping artefacts.
- Frame height normalisation and hard-clamp to prevent rendering overflow.
- `GotoBottom` moved out of `View()` to avoid repeated scroll resets.
- Post-detach cursor and screen restoration (main view vs. grid view).
- Screen clearing on session switch to eliminate rendering artefacts from the
  previous session.
- Preview cache populated by status watcher so switching sessions shows content
  immediately rather than a blank pane.

[Unreleased]: https://github.com/lucascaro/hive/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/lucascaro/hive/releases/tag/v0.1.0

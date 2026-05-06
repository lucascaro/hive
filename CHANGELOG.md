# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security

- Bump `golang.org/x/crypto` to 0.45.0, picking up fixes for two
  moderate `ssh`/`ssh-agent` advisories (panic on malformed message,
  unbounded memory consumption).
- Bump `vite` (frontend dev dependency) to 8.0.10, closing a moderate
  path-traversal advisory in optimized-deps `.map` handling.

## [2.0.0] — 2026-05-05

First stable release of the v2 native rewrite. See
[2.0.0-alpha.1](#200-alpha1--2026-05-01) and
[2.0.0-alpha.2](#200-alpha2--2026-05-02) for the full v2 feature set;
the entries below are the changes since alpha.2.

### Added

- Native OS notifications for session bells.
- Native app menu mirrors every keyboard shortcut.
- Detect session-process exit and surface it via `Alive=false`.
- Resizable sidebar.
- Command palette (Cmd+Shift+K) with keybinding overhaul, delete-project, terminal title surfacing.
- Daemon version handshake with stale-daemon banner in the GUI.
- Help menu for macOS search; launcher usage-sort + digit shortcuts; drag-reorder projects.
- Project-inherited session color gradient with random palette.
- Cmd+Backspace and click-to-position in terminal.

### Changed

- Reattach renders the visible terminal state instead of replaying bytes from the scrollback.
- Dim non-focused tiles for clearer focus.
- Palette shortcut moved from Cmd+K to Cmd+Shift+K (Cmd+K alone now opens palette only).
- Branch model: `main` is v2, `release/v1` carries v1 maintenance.

### Fixed

- Focus border tracks the actually-focused session.
- Notification clicks no longer hide Hive when it's already focused.
- Inline rename inputs no longer lose focus to other handlers.
- Don't reattach to dead sessions; reset xterm on revive.
- Surface a clear error when a session fails to start.
- Terminal links remain clickable while mouse reporting is active.
- Palette focus returns correctly after closing.
- `forceWorktree` no longer leaks into the next launcher open.
- Click-to-position no longer spams cursor-move sequences.

## [2.0.0-alpha.2] — 2026-05-02

### Added

- Per-session git worktrees (port from v1): launcher checkbox, ⌥⌘N
  shortcut, ⎇ glyph in sidebar/grid, automatic cleanup with dirty-tree
  confirmation, daemon-startup orphan reclaim.
- Drag-to-reorder sessions in the sidebar (same-project drops only).
- ⌘-click URLs in a session to open them in the default browser.
- Window position and size persisted across launches.
- Daemon log file at `~/Library/Application Support/Hive/hived.log`.

### Changed

- WebGL renderer (same as VS Code) replaces the DOM renderer for major
  typing-latency wins on older Macs.
- Cursor blink off by default; smooth-scroll animation off.
- Visible grid tile borders + session-tinted title bar.
- macOS scrollbar styling overrides "Always show scrollbars".

### Fixed

- Sessions defined as shell aliases or via fnm/nvm/asdf spawn correctly
  (agents run via `$SHELL -l -i -c <cmd>`).
- Editing a session no longer reorders sessions in sidebar/grid.
- Rename via dblclick works in grid mode and on grid tile names.
- Trackpad momentum scroll capped to ±8 lines per wheel event.
- ⌘[/] navigation works for empty projects.
- Grid layout relayouts on window resize (not just on session switch).
- Two-session grid is always side-by-side.
- Launcher: agents not detected on PATH remain clickable.

## [2.0.0-alpha.1] — 2026-05-01

First alpha of the v2 native rewrite — a desktop GUI app backed by its
own session daemon, replacing the v1 tmux + Bubble Tea architecture.

### Added

- `hived` daemon owns PTYs and persists session/project metadata across
  GUI restarts.
- Wails GUI with xterm.js: full keyboard control, font scaling,
  rename/recolor, dark theme.
- Projects group sessions, each with a working directory (native folder
  picker).
- Agent launcher for Claude, Codex, Gemini, Copilot, Aider, plain shell;
  detects which are on PATH.
- Grid view: per-project (⌘G) or all sessions (⇧⌘G) with spatial arrow
  navigation; last-row gaps absorbed.
- Multi-window (⇧⌘N) — independent windows sharing the same daemon.
- Bell notifications: unfocused sessions emitting BEL pulse in
  sidebar/grid and fire an OS notification.
- No telemetry in shipped binaries.

[Unreleased]: https://github.com/lucascaro/hive/compare/v2.0.0...HEAD
[2.0.0]: https://github.com/lucascaro/hive/compare/v2.0.0-alpha.2...v2.0.0
[2.0.0-alpha.2]: https://github.com/lucascaro/hive/compare/v2.0.0-alpha.1...v2.0.0-alpha.2
[2.0.0-alpha.1]: https://github.com/lucascaro/hive/releases/tag/v2.0.0-alpha.1

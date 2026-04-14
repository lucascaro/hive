# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **In-place session input from grid view**: press `(i)` on a focused grid cell to enter input mode — keystrokes (including `Esc`, arrows, `Enter`, and common `Ctrl+` sequences) are forwarded directly to that session without a full attach. A `INPUT · C-Q` badge appears in the cell header while active. Press `Ctrl+Q` to exit input mode and return to grid navigation. The preview refresh rate is automatically tuned in input mode: the focused session polls at 50 ms while all other sessions poll at 250 ms (2× the default 500 ms background rate), giving fast feedback without hammering inactive sessions. Can be disabled by setting `disable_grid_input: true` in `~/.config/hive/config.json`.

## [0.9.0] — 2026-04-13

### Added
- **`s` sidebar view shortcut**: pressing `s` in the main view focuses the sidebar pane; pressing `s` while in the grid closes the grid and returns to the sidebar (#100).
- **`h`/`l` vim aliases for collapse/expand**: `h` collapses and `l` expands sidebar items (projects/teams), complementing the existing `←`/`→` arrow key bindings. These appear in the help overlay (#100).
- **Reset keybindings to defaults**: press `R` in Settings to restore all key bindings to their factory defaults. The reset is unsaved until you press `s` to confirm, so it can be discarded like any other settings change (#100).
- **Bell sound preview**: cycling through bell sound options in Settings → General → Bell Sound now plays each sound immediately so you can audition options without leaving settings (#94).
- **Bell volume control**: new Settings → General → Bell Volume setting lets you adjust bell playback loudness (10 / 25 / 50 / 75 / 100 %). Does not affect the `normal` system bell or `silent`; not supported on Windows (#94).
- **`bubbles/help`-powered key hints**: the status bar, grid hint bar, and help overlay now all derive from a single `KeyMap` source of truth using `charmbracelet/bubbles/help`. Hints automatically reflect any custom key bindings set in `~/.config/hive/config.json` (#79).
- **Startup View preference**: configure which view hive opens on launch — `sidebar` (default), `grid` (current-project grid), or `grid-all` (all-projects grid). Set it in Settings → General → Startup View; persisted in `~/.config/hive/config.json`. When a detach-restore is in progress (returning from an attached session), the restore takes precedence (#78).
- **Custom terminal bell sounds**: Settings → General → Bell Sound now offers six options — `normal` (current `\a` default), `bee`, `chime`, `ping`, `knock`, and `silent`. Non-default sounds are played via the platform's audio tool (`afplay` / `paplay` / `aplay` on macOS/Linux, PowerShell `SoundPlayer` on Windows) with a graceful fallback to `\a` when no player is available. `silent` suppresses audio entirely while still showing the sidebar bell indicator. Configurable per install; persisted in `~/.config/hive/config.json` (#75).
- **Bell badge in grid view**: sessions with an unacknowledged bell now show a blinking `♪` badge replacing the status dot in the grid cell header, consistent with the existing sidebar indicator (#85).

### Changed
- **Arrow-only navigation**: `↑`/`↓`/`←`/`→` are now the default navigation keys throughout the app (sidebar, grid, settings, pickers). Vim-style (`j`/`k`/`h`/`l`) and WASD aliases have been removed. Users who prefer vim keys can restore them via `"nav_up": "k"` / `"nav_down": "j"` in the `keybindings` config block (#79).
- **`d` kill-session alias removed**: sessions are killed with `x` only; the `d` alias no longer works (#79).
- **Consistent grid actions**: `?` (help), `S` (settings), `H` (tmux reference), and `q` (quit app) now work in grid view the same as in sidebar view. Previously `q` in the grid only closed the grid; it now quits the app. Use `Esc` or `g`/`G` to exit the grid without quitting (#79).
- **Settings save confirmation dialog**: saving settings (`s`) now opens a modal confirmation dialog instead of the inline status-bar prompt, making the action more visible and consistent with other destructive-action dialogs (#93).
- **Settings panel max-width**: settings panel now caps at 100 columns and is centered on wider terminals, making it easier to use without stretching across the full screen (#98).
- **Grid fills empty space**: when sessions don't evenly fill the grid layout, the last row now expands to use the remaining screen height instead of leaving a blank cell at the bottom (#89).

### Fixed
- **Arrow key navigation in sidebar**: `↑`/`↓` now always navigate the sidebar regardless of the user's configured `nav_up`/`nav_down` key. Users with saved configs from before #79 (which stored vim-style `k`/`j`) had their arrow keys silently stop working; they are now permanent aliases alongside any configured key (#100).
- **Empty preview placeholder**: preview pane now shows "Waiting for output…" instead of appearing blank when a session's captured output consists only of terminal escape sequences (e.g. cursor-reset and screen-clear codes emitted by brand-new panes) (#88).
- **Bell sounds and badges when attached to a session**: custom bell audio now plays and the sidebar/grid bell badges are set correctly when a background session rings its bell while the user is directly attached to another session. Previously the bell was silenced because hive's event loop is suspended during attachment; a background poller now handles it (#85).
- **Grid mode toggle preserves selection**: pressing `g` while in the all-projects grid (or `G` while in a single-project grid) now keeps the currently-selected session and its project, instead of resetting to a different project's first session (#80).
- **Settings save no longer blanks the screen**: confirming a settings save (`s` → `y`) now returns to the main view instead of leaving a black, unresponsive screen. The `ViewSettings` layer was being left on the view stack after the component deactivated itself, so `View()` rendered an empty string. The failure path (save error, e.g. read-only config dir) also pops the view now so the error surfaces in the statusbar instead of a black screen (#84).
- **Grid expanded-cell navigation**: arrow keys in grid view now navigate correctly when cells are expanded. Right from the last session in the last row moves to the extended cell; left returns to the previous position (#101).

## [0.8.0] — 2026-04-12

### Changed
- **Settings organized into tabs**: the Settings screen (`S`) now groups options into tabs — General, Team Defaults, Hooks, Keybindings. Switch tabs with `←`/`→` or `h`/`l`; `j`/`k` navigation, `enter/space` edit/toggle, and `s`/`esc` save/close are unchanged. Each tab remembers its own cursor and scroll position. The tab bar uses a raised-capsule design with the active tab seated into a notched baseline, so selection reads as topology rather than just color (#76).
- **Windows install docs rewrite**: restructured README and Windows getting-started guide into a single, step-by-step WSL-based walkthrough (install WSL → install tmux → download `hive-linux-amd64` → run). Removed the incoherent `hive-windows-amd64.exe` + WSL-tmux recommendation from the earlier pass. MSYS2 / Chocolatey + `hive-windows-amd64.exe` remain documented as a clearly-separated "native Windows alternative" with an explicit session-persistence caveat (follow-up to #74).

## [0.7.2] — 2026-04-12

### Changed
- **Install docs overhaul**: README and Windows getting-started guide now lead with prebuilt binaries from the Releases page (no Go toolchain required). Go 1.25+ is now called out as a build-time-only requirement. Windows tmux guidance now recommends WSL first — sessions survive terminal close and reboots, PTY semantics match Linux, and AI agent CLIs run as their native Linux builds. MSYS2 and Chocolatey remain as documented alternatives (#74).

## [0.7.1] — 2026-04-11

### Changed
- **Code refactor**: removed dead code (`GetPaneActivity`, `SendKeys`, `SessionLabel`), ~50 section separator comments, and ~40 redundant doc comments. Consolidated duplicated grid sync, sidebar navigation, and worktree setup patterns into shared helpers. Deduplicated 6 `Move*Up/Down` state reducers using a generic `swapAdjacent` helper. Removed redundant `sidebar.Rebuild` calls after `commitState` (#37).

## [0.7.0] — 2026-04-11

### Added
- **Per-session color**: each session gets its own color, shown as a gradient in the grid cell header and sidebar title (project color → session color). Colors are auto-assigned on creation and can be cycled with `v`/`V` in both sidebar and grid view (#54).

- **"What's New" changelog on update**: when Hive detects a version change on startup, it shows a scrollable overlay with the changelog entries since the last version. Press `d` to permanently suppress it, or re-enable via the "Hide What's New" toggle in Settings (#69).

### Changed
- **Improved grid selection visibility**: the selected cell now has a subtle dark background tint on the content preview area, making it easier to identify at a glance alongside the existing accent border (#68).

## [0.6.0] — 2026-04-11

### Added
- **Reorder items via keyboard**: `Shift+Up`/`Down` reorders sessions, teams, and projects in the sidebar; `Shift+Left`/`Right` reorders in grid view. Order persists across restarts (#55).

### Fixed
- **Terminal bell forwarding**: bells from sessions are now forwarded to the user's terminal with debounce to prevent bell storms. Sessions with pending bells show a `♪` badge in the sidebar until attached (#34).

## [0.5.1] — 2026-04-10

### Fixed
- **Faster attach/detach transitions**: reduced tmux process spawns from 23 to 4 by batching status-bar commands with `\;` chaining and eliminating the save/restore phase. Attach latency drops from ~1-2s to ~200-400ms (#63).

### Changed
- **Grid view arrow keys wrap between rows**: pressing right on the last cell of a row now moves to the first cell of the next row, and left on the first cell wraps to the last cell of the previous row (#53).

## [0.5.0] — 2026-04-09

### Added
- **Single-key detach shortcut**: a single key combo (default **Ctrl+Q**) now detaches from an attached session and returns you to Hive, on both the tmux and native backends. The key is configurable via the new top-level `detach_key` field in `~/.config/hive/config.json` (e.g. `"detach_key": "ctrl+x"`). On the tmux backend the standard `Ctrl+B D` sequence still works as a fallback. Existing users will see the pre-attach splash one more time on first startup so they discover the new shortcut, even if they had previously dismissed it (#41).
- **Session status detection infrastructure**: adds configurable two-tier status detection for agent sessions. For Claude, uses pane title spinner detection (Braille spinner = running) with content-diff + debounce fallback. Detection patterns (`run_title`, `wait_title`, `wait_prompt`, `idle_prompt`, `stable_ticks`) are configurable per agent via the `status` field in agent profiles. Full "waiting for input" detection deferred — Claude Code uses the same pane title for both idle and asking-a-question states (#38).
- **Live agent terminal title in attached view**: the tmux status bar now shows the running agent's terminal title (set by Claude/Codex/etc. via OSC 0/2) next to the static session header, updating in real time as the agent works (#39).
- **Live agent terminal title in grid view**: each grid cell now shows a one-line italic subtitle with the agent's current terminal title when the cell has enough room (≥ 8 rows). Untrusted OSC content is sanitized before rendering (#39).
- **Colorful UI accents**: breadcrumb separators, status-bar hint descriptions, and the worktree `⎇` badge now use the project palette for better visual differentiation (#39).

### Changed
- **Cleaner attach status bar**: the tmux status bar no longer shows the default window list when attached to a session — only the custom title and detach hint remain (#39).

### Fixed
- **Renaming projects and teams now works**: pressing `r` on a project or team in the sidebar previously accepted input but silently discarded it. The rename now persists correctly (#49).

## [0.3.0] — 2026-04-05

### Changed
- **View stack replaces flag-based view dispatch**: the TUI now uses a view
  stack (`PushView`/`PopView`) instead of scattered boolean flags to track
  which overlay is active. This fixes dialogs opened from grid view (rename,
  kill, new session) incorrectly returning to the main view instead of back
  to the grid (#46).

### Added
- **Create directories in dir picker**: press `n` or `+` in the directory picker
  to create a new subdirectory without leaving hive (#45).

## [0.2.1] — 2026-04-04

### Fixed
- **Selected session persists across views**: the active session now stays
  selected when switching between sidebar, grid view, and after attach/detach.
  Previously the sidebar cursor would reset on view transitions.
- **Active project syncs on grid selection**: selecting a session from a
  different project in the all-projects grid now updates `ActiveProjectID`,
  so the next project-scoped grid shows the correct project.
- **Preview updates on grid exit**: switching sessions via grid exit now
  restarts preview polling for the newly selected session.
- **Background rebuilds don't steal cursor**: sidebar rebuilds from
  background events (title/status watchers) no longer forcibly move the
  cursor away from project or team rows the user is navigating.

## [0.2.0] — 2026-04-04

### Fixed
- **No terminal flash on attach/detach**: transitioning between grid view and
  full-screen session no longer briefly flashes the pre-hive terminal content.
  The attach script now uses its own alternate screen buffer to hide the main
  terminal during the transition.
- **Grid previews resume after detach**: returning from a full-screen attached
  session to the grid view now correctly restarts preview polling, so tiles
  show live output instead of stale content.
- **No more duplicate branch name in sidebar**: worktree sessions whose title
  matches the branch name now show just the `⎇` badge instead of
  "my-branch ⎇ my-branch".
- **Grid mode restored after tmux detach**: when attaching to a session from the
  grid view using the tmux backend, detaching now correctly returns to the grid
  instead of falling back to the main sidebar view.

### Changed
- **Mouse enabled by default**: tmux sessions now have mouse support turned on
  at creation time, allowing scrolling through output with the mouse wheel.
  Hold Shift to select text for copy-paste.

### Added
- **Multi-instance support**: multiple hive windows can now run simultaneously
  against the same config directory without corrupting each other's state.
  Each running instance polls `state.json` every 500 ms and reloads automatically
  when another instance makes a change — new projects and sessions appear in all
  windows within half a second. Writes are serialised with an exclusive advisory
  lock (`state.json.lock`) so concurrent saves never produce a torn file.
  Dead sessions discovered during reload are reconciled against the live tmux
  backend, and any associated git worktrees are cleaned up automatically.
  The advisory lock file (`state.json.lock`) is created with mode 0600
  (owner read/write only), consistent with `state.json`.
- **Interactive directory picker**: new project creation uses a full-screen directory
  browser (`bubbles/list`) instead of a plain text input. Navigate with `↑/↓` or `j/k`,
  descend into a directory with `enter`, go up with `h`/`←`, confirm the current directory
  with `.`, and cancel with `esc`. Press `/` to enter filter mode, then type to fuzzy-search
  subdirectories; press `esc` to clear the filter.

### Fixed
- **Mouse clicks broken after detach**: returning from an attached tmux session no
  longer disables mouse support in the TUI. The bubbletea `RestoreTerminal()` path
  does not re-enable mouse after `ExecProcess`; the fix explicitly re-enables mouse
  cell motion when the attach completes.
- **Stale preview after detach**: returning from an attached session no longer briefly
  shows outdated preview content from a different session. The preview pane clears
  immediately and shows a placeholder until fresh content arrives.
- **Directory picker shows only directories**: the picker now filters to subdirectories
  only at read time — no files or grayed-out entries appear in the list.
- **Directory picker height**: the list now fills the available overlay height instead of
  defaulting to zero rows.
- **Preview border alignment**: fixed inconsistent border alignment in preview and grid
  views caused by zero-width Unicode characters (e.g. `U+200B` ZERO WIDTH SPACE) in
  captured terminal content. These characters have zero display width but occupy real
  byte space, causing lipgloss padding to produce inconsistent line widths. The fix
  strips these characters during preview sanitization.
- **Screen flicker**: removed unnecessary `tea.ClearScreen` calls and the associated
  `pendingPreviewClear` flag that were added as defensive measures against the
  (now fixed) border alignment bug. Bubble Tea's incremental renderer handles
  redraws correctly without explicit clears.

### Added
- **Grid cell worktree indicator**: grid cells now show an inline `⎇` badge when a
  session runs in a git worktree; if the branch name differs from the session title the
  full branch name is displayed (e.g. `⎇ feat/my-branch`).

### Changed
- **Directory picker for new projects**: migrated from a plain text input to a custom
  `DirPicker` built on `bubbles/list`. The working-directory step uses a full-screen
  directory browser — navigate with `↑/↓`, open directories with `enter`, go up with
  `h`/`←`/`backspace`, confirm the current directory with `.`, press `/` to filter,
  and cancel with `esc`. Styled to match the hive accent colour theme.
- **Grid cell single-line header**: the two-line header (title row + project subtitle
  row) has been merged into one line — status dot, agent badge, session title, project
  name, and worktree badge all appear on a single header line, giving each cell one
  extra row of terminal-output preview.
- **Session attach via in-process overlay**: when using the tmux backend, pressing
  Enter on a session no longer quits and restarts the TUI. Instead the TUI suspends
  in place using `tea.ExecProcess` while the session is attached. When you detach,
  the TUI resumes immediately with all state intact — no disk reload, no flicker.
  - On **tmux ≥ 3.2** the session opens as a floating `tmux display-popup` overlay
    (95 % width, 90 % height) that sits on top of the TUI. Closing the popup (or the
    agent exiting) returns you directly to the TUI without any full-screen switch.
  - On older tmux or when running outside a tmux session, a plain full-screen
    `tmux attach-session` is used, still with the no-restart improvement.
  - The **native PTY backend** is unaffected and continues to use the existing
    quit+restart flow.

### Added
- **Getting-started guides**: added `docs/getting-started-macos.md` and
  `docs/getting-started-windows.md` — self-contained, platform-specific guides
  that walk new users from installing prerequisites through their first agent session.
  README "Getting Started" section now links to both guides.
- **Mouse support**: left-click any sidebar item to select it (session) or toggle
  collapse (project/team); left-click the preview pane to focus it and activate the
  displayed session; left-click a grid cell to move the cursor to that cell; scroll
  wheel navigates the sidebar, scrolls the preview, and moves the grid cursor.
- **Grid preview newest content**: grid cell previews now show the most-recent
  output (bottom of scrollback) instead of the oldest lines from the top, matching
  the behaviour of the single-session preview pane.
- **Grid cell project label**: each grid cell now shows a muted subtitle line with
  the project name, so every tile displays project name + session name + agent type
  without needing to consult the sidebar.
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
- **Insecure file permissions hardened**: `state.json`, `usage.json`, and `hive.log`
  are now written with mode `0o600` (owner read/write only) instead of `0o644`.
  Additionally, `config.Ensure()` — called at every startup — now runs
  `FixPermissions()` to retroactively tighten permissions on any of these files that
  were created with overly-broad modes by an older version of Hive. This prevents
  other OS users on shared machines from reading project metadata.
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

[Unreleased]: https://github.com/lucascaro/hive/compare/v0.9.0...HEAD
[0.9.0]: https://github.com/lucascaro/hive/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/lucascaro/hive/compare/v0.7.2...v0.8.0
[0.7.2]: https://github.com/lucascaro/hive/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/lucascaro/hive/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/lucascaro/hive/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/lucascaro/hive/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/lucascaro/hive/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/lucascaro/hive/compare/v0.4.0...v0.5.0
[0.3.0]: https://github.com/lucascaro/hive/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/lucascaro/hive/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/lucascaro/hive/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/lucascaro/hive/releases/tag/v0.1.0

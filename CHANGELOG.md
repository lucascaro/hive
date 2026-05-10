# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- macOS: "Restart Hive" killed hived but the new GUI never appeared.
  `spawnNewGUI` re-execed the inner Mach-O directly, which spawns a
  process but doesn't go through LaunchServices, so the relaunched
  Wails/WebKit window never gained focus (often never rendered). When
  running inside a `.app` bundle we now relaunch via `open -n
  <bundle.app>`; `-n` forces a fresh instance even while the dying
  parent is still around. Dev builds (binary outside a bundle) keep
  the existing re-exec path.
- GUI: switching from single-focus to grid mode no longer leaves the
  previously focused session visually highlighted but unable to receive
  keystrokes. Visual focus (`.term-focused`) and keyboard focus
  (xterm helper-textarea) are now reconciled atomically through a
  single state-driven writer, so they can no longer drift across view
  transitions, modal open/close, or rename input. (#181)

## [2.2.1] — 2026-05-10

### Fixed

- Windows: the "Restart Hive" button in the daemon-stale banner now
  actually restarts hived. Previously the platform stub returned
  `restart not supported on this platform` and the relaunch never
  fired. Windows now reads `<sock>.pid`, verifies the recorded pid is
  hived.exe via `tasklist`, and TerminateProcess-es it (matching the
  unix SIGKILL fallback). Stale pidfiles whose pid was recycled to an
  unrelated process are removed without signaling. (#177)
- Windows / Linux: toggling grid mode (`Ctrl+G` / `Ctrl+Enter`) no
  longer flashes the grid then snaps back to single-session view, and
  `Ctrl+Up` / `Ctrl+Down` / `Ctrl+Left` / `Ctrl+Right` no longer feel
  reversed. The native menu's keyboard accelerators were firing
  alongside the in-window JS keyboard listener, so every shortcut ran
  twice. The native menu is now darwin-only — JS owns every shortcut
  on Windows and Linux. macOS is unchanged. (#177)
- GUI: huge-text flash on grid → zoom → session switch (regression of
  the #168 fix). Synchronously fitting in `show()` updates xterm's
  cols/rows, but xterm-webgl's renderer schedules the canvas pixel
  resize on rAF, so the next paint frame composited the stale
  (grid-cell-sized or last-zoom-sized) canvas CSS-stretched into the
  new body. `show()` now hides the body via `visibility: hidden`
  across the rAF gate, then reveals on the next frame once the
  renderer has caught up; `hide()` and `destroy()` cancel any
  in-flight reveal so the next show starts clean. (#176)

## [2.2.0] — 2026-05-09

### Added

- VT snapshot conformance corpus
  (`internal/session/testdata/conformance/`) and a `scripts/vtcapture`
  PTY-recording tool. The corpus pins the byte-exact snapshot output
  for the fixture set against the current backend (hinshun/vt10x);
  CI fails on any unintentional drift. Synthetic fixtures cover RGB,
  reverse-video, alt-screen, scrollback eviction, OSC 8, and CJK wide
  chars, with chunk boundaries declared so per-Write contracts (like
  the eviction heuristic) are exercised faithfully. This is Phase 0
  of the in-house VT emulator plan and is independently valuable as a
  regression floor.
- `HIVE_SOCKET` and `HIVE_STATE_DIR` environment variables override the
  daemon socket path and state directory respectively. Setting both
  lets you run an isolated dev daemon (and dev GUI build) alongside a
  production one without touching its sessions or registry. Export the
  variables in every process that talks to the daemon (the daemon
  itself and any client — GUI or CLI); a client without them will
  dial or spawn the platform-default daemon instead. The platform
  defaults are unchanged when the variables are unset.

### Fixed

- Restarting a Claude session before any message had been sent failed
  with `No conversation found with session ID: <uuid>`. Claude only
  writes the transcript jsonl after the first user message, so a fresh
  pinned session has no on-disk record for `claude --resume <id>` to
  find. Restart/Revive now stat the expected transcript path
  (`~/.claude/projects/<encoded-cwd>/<id>.jsonl`) and re-pin the same
  id via `claude --session-id <id>` when it doesn't exist, preserving
  the Hive entry id ↔ claude conversation id mapping.
- An isolated dev daemon (`HIVE_STATE_DIR` set) no longer reaps
  worktrees owned by the canonical/prod daemon at startup. The
  on-disk `<project>/.worktrees/` namespace is shared across daemon
  instances even when registries are isolated, so the orphan-worktree
  reclaim now runs only when `HIVE_STATE_DIR` is unset. Iso runs that
  leak their own worktrees on SIGKILL still get reaped on the next
  prod-daemon startup, matching the existing orphan contract.
- GUI: Keyboard shortcuts no longer accept both ⌘ and Ctrl on macOS.
  Platform-adaptive bindings (⌘T, ⌘N, ⌘W, ⌘1–9, etc.) now require
  ⌘ on macOS and Ctrl on Windows/Linux exclusively, matching the
  native menu accelerators. Conversely, Ctrl+\` (open OS terminal)
  is now Ctrl-only on every platform — on macOS ⌘\` is reserved for
  native window cycling, so it's no longer swallowed by the app.
- Restart: Restarting a Claude, Codex, Gemini, or Copilot session no
  longer reattaches to a sibling's conversation when multiple sessions
  share a worktree or cwd. For Claude and Gemini, Hive pins each
  session to its entry id at first launch (`--session-id <uuid>`) and
  resumes via `<agent> --resume <uuid>`. For Codex and Copilot (which
  have no flag to inject an id at launch), Hive captures the
  agent-generated session UUID from
  `~/.codex/sessions/.../rollout-*.jsonl` (codex) or
  `~/.copilot/session-state/<uuid>/workspace.yaml` (copilot) shortly
  after spawn, matching the spawn cwd to disambiguate concurrent
  sessions. The pinned id is persisted on the session metadata so
  daemon restart respawns each session against its own conversation
  rather than collapsing back to "most recent in cwd". Restart is now
  unambiguous regardless of how many siblings live in the same
  directory. Aider retains today's behavior (no resume command
  available). (#165, #172)
- GUI: Toggling between grid and single view (⌘\, ⌘[) now reliably
  returns keyboard focus to the active session. Previously the
  sidebar still showed the session as selected but keystrokes were
  dropped because xterm's internal focus flag was stale after the
  view-toggle's focusin/focusout churn — focusing the helper-textarea
  DOM node directly bypasses the stale flag and fires a real focus
  event. (#159)
- GUI: Resize no longer strands the user mid-history when the viewport
  is 1–2 lines short of the bottom. Codex (and similar TUIs) sometimes
  leave the viewport just above the bottom; the resize handler now
  treats anything within 2 lines of bottom as "at bottom" and re-snaps
  after reflow. Deliberate scrollback (3+ lines up) is still preserved.
  (#163)
- Session snapshot: 24-bit RGB foreground/background colors now
  round-trip across GUI reattach. Previously `writeColor` dropped the
  RGB-encoded `vt10x.Color` to default, so modern prompts (starship,
  p10k) and TUIs (Claude, Codex, lazygit) came back uncolored until
  the app repainted. Truecolor SGR (`38;2;R;G;B` / `48;2;R;G;B`) is
  now emitted for the RGB range; sentinels still fall through. (#144)
- Session reattach now preserves scrollback above the visible viewport.
  Lines that scrolled off the top of a running session reappear in the
  GUI's scrollback after a restart, restoring the contract that PR #141
  inadvertently broke when it switched the reattach repaint to a vt10x
  visible-screen snapshot. Up to 500 evicted rows per session are kept
  with their SGR styling intact. (#143)
- GUI: Pressing Enter while editing a session or project name in the
  sidebar now reliably commits the new name and exits edit mode,
  matching the tile-rename behavior. Previously the input could linger,
  fire `UpdateSession` twice via the blur path, or be swallowed by the
  dead-session overlay's Enter handler. (#155)

## [2.1.0] — 2026-05-07

### Added

- GUI: Restart Session command (palette + File menu) recycles the
  active session's agent process in place. The sidebar slot, name,
  color, order, and worktree are preserved; the agent is relaunched
  with its resume flag (`claude --continue`, `codex resume --last`,
  etc.) so the prior conversation is picked back up. Useful for
  picking up new skills/config without losing state.
- GUI: ⌘P duplicates the active session into the same cwd/worktree;
  ⇧⌘P opens the launcher pinned to that cwd to pick a different tool.
  The duplicate adopts the source's worktree (no nested `.worktrees/*`
  is created), shows the worktree badge in the sidebar, and the
  worktree directory is only cleaned up when the last session in it
  is killed. New entries also appear in the command palette and the
  File menu.
- GUI: in-app "Update available" banner. The desktop app now polls
  GitHub releases on load and every 6h, surfacing a banner with a
  one-click link to the release page when a newer tagged build
  exists. Manual trigger via File → "Check for Updates…". Untagged
  dev builds skip the check.

### Changed

- Sidebar: more visible selected-session styling. Selection now uses an
  18% session-color tint plus a 3px left accent bar (full row height),
  replacing the prior 6% white overlay and 2px right-edge line.

### Fixed

- GUI: after Restart Session, keyboard focus now returns to the
  resumed terminal instead of leaving the window without a focused
  element. The reattach path on `pty:disconnect` + `session:event(updated, alive)`
  now calls `focusActiveTerm()` for the active session.
- Session start: when a saved session's working directory no longer
  exists, fail with a clear error naming the missing directory instead
  of the misleading `fork/exec <shell>: no such file or directory`
  Go produces on `chdir` failure (which sent users hunting for a
  missing shell when the real cause was a deleted project directory).

## [2.0.1] — 2026-05-05

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

[Unreleased]: https://github.com/lucascaro/hive/compare/v2.2.1...HEAD
[2.2.1]: https://github.com/lucascaro/hive/compare/v2.2.0...v2.2.1
[2.2.0]: https://github.com/lucascaro/hive/compare/v2.1.0...v2.2.0
[2.1.0]: https://github.com/lucascaro/hive/compare/v2.0.1...v2.1.0
[2.0.1]: https://github.com/lucascaro/hive/compare/v2.0.0...v2.0.1
[2.0.0]: https://github.com/lucascaro/hive/compare/v2.0.0-alpha.2...v2.0.0
[2.0.0-alpha.2]: https://github.com/lucascaro/hive/compare/v2.0.0-alpha.1...v2.0.0-alpha.2
[2.0.0-alpha.1]: https://github.com/lucascaro/hive/releases/tag/v2.0.0-alpha.1

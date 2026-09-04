# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Reload GUI** — pick up a new GUI build without restarting the daemon.
  Every running shell and agent keeps going, with its scrollback intact.
  Available from **File ▸ Reload GUI**, the command palette, and the
  menu bar. Hive decides whether a restart is really needed by comparing
  the two builds' *daemon contract*: only a change the daemon actually
  exposes costs you a full restart now, so a frontend-only build no
  longer kills your sessions. The stale-daemon banner follows the same
  rule, and no longer nags about a daemon that is simply a different
  build of the same behaviour.
- Hive now has a macOS menu-bar icon. It shows the running daemon's
  version, how many sessions are open across which projects, and which
  ones are waiting on you — and it keeps working when every window is
  closed. Click a session to jump straight to it (Hive opens if it
  isn't running), or use it to reload the GUI, restart the daemon, or
  check for updates. Settings gains a "Start at login" toggle for it,
  which needs a signed build; until then the menu bar appears whenever
  the daemon or a window starts.

### Changed
- Sessions that ring the terminal bell are now tracked by the daemon
  rather than by each window on its own. Every window agrees on which
  sessions want you, a window that was closed or reloaded still learns
  what rang while it was away, and focusing a session clears the flag
  everywhere at once.
- **File ▸ Restart Hive…** is now **Restart Daemon… (ends all sessions)**.
  It sits next to the new Reload GUI item, which looks similar and costs
  nothing, so the destructive one names its cost.
- The update button now says what applying it will cost. A GUI-only
  update reads **Reload** and applies without a confirmation prompt,
  because it ends nothing; one that replaces the daemon reads **Restart**
  and still warns first. Hive tells them apart by asking the staged
  build's daemon for its contract before you commit to anything.
- Settings is now split into tabs — Agents, Appearance and Updates, plus Menu
  bar on macOS — instead of one long scroll. It opens on Agents, so the
  custom-agent list is the first thing you see however many agents you have,
  and the theme picker and update channel are one click away rather than a
  scroll away. Switching tabs never discards an edit in progress, and the tabs
  are keyboard-navigable with the arrow keys.
- The terminal tile's chrome — its header, the dead-session card and the
  loading panel a starting session shows — now renders from the same React
  tree as the rest of the app, and the frontend's last imperative DOM
  primitives are gone with it. The terminal itself is untouched: hosts are
  still reparented rather than recreated, so scrollback, WebGL slots and PTY
  attachments survive every repaint exactly as before. No visible change; the
  markup, classes and keyboard behaviour are identical.

## [2.6.0] — 2026-09-04

### Added
- **Reload GUI** — pick up a new GUI build without restarting the daemon.
  Every running shell and agent keeps going, with its scrollback intact.
  Available from **File ▸ Reload GUI**, the command palette, and the
  menu bar. Hive decides whether a restart is really needed by comparing
  the two builds' *daemon contract*: only a change the daemon actually
  exposes costs you a full restart now, so a frontend-only build no
  longer kills your sessions. The stale-daemon banner follows the same
  rule, and no longer nags about a daemon that is simply a different
  build of the same behaviour.
- Hive now has a macOS menu-bar icon. It shows the running daemon's
  version, how many sessions are open across which projects, and which
  ones are waiting on you — and it keeps working when every window is
  closed. Click a session to jump straight to it (Hive opens if it
  isn't running), or use it to reload the GUI, restart the daemon, or
  check for updates. Settings gains a "Start at login" toggle for it,
  which needs a signed build; until then the menu bar appears whenever
  the daemon or a window starts.

### Changed
- Sessions that ring the terminal bell are now tracked by the daemon
  rather than by each window on its own. Every window agrees on which
  sessions want you, a window that was closed or reloaded still learns
  what rang while it was away, and focusing a session clears the flag
  everywhere at once.
- **File ▸ Restart Hive…** is now **Restart Daemon… (ends all sessions)**.
  It sits next to the new Reload GUI item, which looks similar and costs
  nothing, so the destructive one names its cost.
- The update button now says what applying it will cost. A GUI-only
  update reads **Reload** and applies without a confirmation prompt,
  because it ends nothing; one that replaces the daemon reads **Restart**
  and still warns first. Hive tells them apart by asking the staged
  build's daemon for its contract before you commit to anything.
- Settings is now split into tabs — Agents, Appearance and Updates, plus Menu
  bar on macOS — instead of one long scroll. It opens on Agents, so the
  custom-agent list is the first thing you see however many agents you have,
  and the theme picker and update channel are one click away rather than a
  scroll away. Switching tabs never discards an edit in progress, and the tabs
  are keyboard-navigable with the arrow keys.
- The terminal tile's chrome — its header, the dead-session card and the
  loading panel a starting session shows — now renders from the same React
  tree as the rest of the app, and the frontend's last imperative DOM
  primitives are gone with it. The terminal itself is untouched: hosts are
  still reparented rather than recreated, so scrollback, WebGL slots and PTY
  attachments survive every repaint exactly as before. No visible change; the
  markup, classes and keyboard behaviour are identical.

## [2.5.0] — 2026-09-03

### Added
- In-app updates. Settings now carries an update channel: **Release**
  follows tagged versions, **Latest** follows the tip of your source
  checkout (auto-detected when Hive is running from one, or pointed at a
  directory you pick). Hive still only *checks* in the background —
  nothing is downloaded or built until you press **Update**. The button
  reports progress while it works and turns into **Restart** when the new
  build is ready; pressing it swaps the app in place and relaunches on
  the new version. Release downloads are verified against a SHA-256
  manifest now published with every release, and a mismatch is discarded
  rather than installed. The in-place swap is macOS-only for now —
  Windows and Linux keep the existing Download link.
- Undo for an accidental session close. Closing a session now leaves a
  record behind, so it can be reopened — with its name, colour, project,
  worktree and (for agents that support resume) its conversation. An
  **Undo** banner appears the moment you close something, and **⌘Z** /
  **File ▸ Reopen Closed Session** reopens the most recent close at any
  time, including after a restart. Reopening is honest about what it
  cannot bring back: scrollback is always gone, and the banner says so
  along with anything else that was lost. Closing a session and deleting
  its worktree now saves a recovery patch of the uncommitted changes
  first, so even that path is no longer a dead end.
- Added twelve theme presets ported from popular editor palettes: Dracula,
  Nord, Gruvbox Dark, Tokyo Night, Catppuccin Mocha, One Dark, Neon (the
  classic Monokai palette), Solarized Dark and Light, Catppuccin Latte, and
  GitHub Dark and Light. They appear under a "Community" heading in
  Settings › Appearance, which now groups the theme list into Hive / Native /
  Community. Each one repaints the whole app and every open terminal, ANSI
  palette included, the same way the built-in presets do.

  Unlike the built-in presets, the community palettes ship at their published
  upstream values rather than being adjusted to meet Hive's contrast bar, so
  some of their text sits below WCAG AA. They are opt-in; the presets you can
  land on without choosing one are unaffected.
- Added a "Check for updates" button to the sidebar header, next to the
  "New project" (+) button. It runs the same check the macOS app menu's
  "Check for Updates…" item ran, and reports the result in the usual update
  banner — up to date, update available, or check failed. Until now that check
  had no in-window trigger at all, and none whatsoever outside macOS.

- Changed the "New project" (+) button to the app's standard icon-button
  styling so it matches its new neighbour. At rest it is now flat and its
  glyph is dimmed, the same as every other icon button in the app; hovering
  restores the full-strength glyph and brings the background back, now with
  the same short fade the app's other icon buttons use.
- ⌘⏎ (Ctrl+Enter off macOS) in a grid view now focuses the active session,
  switching to single view on the tile you navigated to. The binding is
  deliberately one-way — single → grid stays on ⌘G / ⇧⌘G — so that in single
  view the key falls through to the terminal, where Claude and Codex bind
  Cmd+Enter themselves.
- Theme presets groundwork. `localStorage['hive.theme']` accepts `classic`
  (default), `hive-dark`, `hive-light`, `system`. No visual change by
  default, except that the terminal cursor and selection now use the accent
  colour.

### Changed
Settings now shows its confirm and cancel key hints (`[esc]` / `[enter]`) in the dialog footer, like every other overlay.
- Rewrote the desktop GUI's frontend in React 19 with a zustand store, region by
  region, replacing ~13k lines of hand-written DOM bookkeeping. The app looks and
  behaves identically — this is an internal change — but the sidebar, chrome,
  modals and grid now repaint only the parts that actually changed instead of
  rebuilding a whole region on every update. Terminals are untouched: xterm keeps
  its own imperative lifecycle, and no terminal is ever recreated by a re-render.
- Reskinned chrome: notice banners, a real status bar with inline mode
  shortcuts, grid tile headers with state icons, and consistent launcher and
  command-palette rows.
- Every dialog — Settings, the worktree browser, the project editor, the help
  overlay and the confirm prompts — is now built on one shell with consistent
  Escape, backdrop and focus behaviour, and one set of form fields.
- New Settings > Appearance: pick a theme preset (System, Hive Dark, Hive
  Light, Classic) and override any design token by hand. Changes apply as you
  make them, reach open terminals, and are remembered.
- Terminal colours now follow the theme: each preset carries its own ANSI
  palette, so Hive Light no longer renders program output in colours tuned
  for a dark background.
- **New look by default.** Hive now follows your system light/dark setting out
  of the box instead of starting on the v2.4 pure-black theme. The v2.4
  appearance is still there as Settings > Appearance > Classic.
- Three more presets: Native Dark, Native Light and Terminal, each with its own
  terminal colour palette. Every preset — and every ANSI colour on a light
  background — is checked against WCAG AA contrast in CI, so no theme ships
  with text you cannot read.
- IBM Plex Sans and JetBrains Mono are bundled with the app, so the Hive themes
  look the same on macOS, Windows and Linux instead of falling back to whatever
  the machine happens to have. The terminal font follows the theme too.
- GUI icon sprite. The GUI's controls and session-state indicators are now
  SVG icons instead of Unicode symbols, so they render identically on every
  platform. Session state reads as a shape as well as a colour: a triangle
  for running, a diamond when the agent needs you, a dotted ring while
  starting, a square when exited, and a cross on error.
- The sidebar is rebuilt on the design system: two-line rows (name over the
  live window title, or its state when there is none), project cards,
  geometric state icons, and `[n]` hints showing which session ⌘1–9 selects.
  Exited sessions stay in the list, struck through, with restart and kill on
  hover. Minimum sidebar width is now 220px.

### Fixed
- Fixed the `e2e-real` test harness, which had been failing on `main` for
  reasons unrelated to any diff. `hived-ws-bridge` dispatched every JSON-RPC
  frame on its own goroutine, so under CPU contention adjacent `WriteStdin`
  keystrokes reached the pty out of order and the commands the specs typed
  were not the commands the shell ran. `WriteStdin` is now applied in arrival
  order. Test-only: the shipped GUI does not use this bridge.
- Re-instated the CI quarantine on the `e2e-real` test "viewport converges to
  the bottom after a mode switch", which PR #307 lifted on insufficient
  evidence. It failed CI macOS on both attempts with the same
  `resizeDecisions() === 0` symptom it was originally quarantined for. Test-only.
- Keyboard switching now skips what you minimized. ⌘↑ / ⌘↓ step over
  sessions in the tray and sessions whose project is minimized — they no
  longer pull you back into a project you put away, or drop you out of a
  grid view when they do. ⌘[ / ⌘] likewise cycles only projects still in
  the sidebar. A minimized project stays reachable from its tray chip,
  the sidebar, and ⌘K.
- Fixed raw escape characters in the update banner. Build output shown in
  the update banner is now plain text — ANSI colour codes, cursor-control
  sequences and carriage-return redraws from `build.sh` no longer leak
  through as literal `ESC[32m` garbage.
- Fixed the layout of minimized projects in the sidebar. A minimized
  project now spans the full width of its tray with the restore `+`
  pinned to the right edge, and clicking anywhere on the row restores
  the project instead of only the project name.
- Fixed keyboard focus being silently lost in the sidebar and the grid. A
  daemon `session:event` update — one arrives on every phase step, on every
  surviving session after a kill, and whenever the agent-session-id capture
  poll lands, up to 30s after a session starts — rebuilt the whole sidebar,
  destroying whatever the user had focused. `renderGrid` had the same problem
  from re-parenting every tile on every repaint. Session updates now patch the
  existing rows in place, the grid reorders only when the order actually
  moved, and both paths restore focus if a genuine rebuild moves it.
- The worktree browser, the launcher, the command palette, the help overlay and
  the dead-session overlay follow the chosen theme. They carried 31 hard-coded
  colours, so on a light preset the worktree list painted near-black rows on a
  white panel.
- State colours are text now, not just icon fills, so they are held to WCAG AA
  on every ground they are painted on: `hive-light`'s "running", "attention" and
  "error" darken, and `native-dark`'s "error" lightens, so the worktree status
  lines, the merged badge and the destructive action stay readable under every
  preset.
- The merged badge loses its green tint; its text and border carry it.
- Dragging a session in the sidebar now drops it exactly where the indicator
  showed. The drop slot was resolved against the sibling list that still
  contained the dragged row, so a row dragged downwards consistently landed
  one position too low.
- The drop indicator is now a placeholder the size of the dragged item, and
  the dragged row leaves the layout while the drag is in flight — so the
  sidebar's height stays fixed and content no longer jumps on drop. Project
  cards get the same affordance as session rows.
- `build.sh` now fails with install instructions when the `wails` CLI on
  `PATH` does not match the version pinned in `scripts/ci-bootstrap.sh`,
  instead of silently building against a stale toolchain after a Wails
  bump.
- Added `scripts/check-changeset.sh`, a local mirror of the changesets CI
  gate that can be installed as a `pre-push` hook.
- Closing a session no longer moves you to a different session while the
  "Close this session anyway?" confirmation is on screen. The jump used to
  happen during the daemon's pre-flight worktree check — before you had been
  asked anything — so the dialog appeared over a neighbouring session and
  cancelling left you there. Focus now moves only once the session is really
  closing.
Renaming a worktree no longer loses the edit when the daemon repaints the list underneath it.

## [2.4.0] — 2026-08-29

### Added

- Projects can be minimized out of the sidebar. The `–` button on a
  project header — the same glyph that minimizes a session tile — drops
  the whole project to a compact, name-only list at the bottom of the
  sidebar, and hides its sessions from grid views. The sessions keep
  running and stay reachable from the chip, the command palette and
  ⌘[ / ⌘]. A `＋` on the chip puts the project back exactly where it was:
  minimizing never touches project order, so restoring cannot shuffle
  your list, and reordering the projects you left visible produces the
  same result it would with nothing minimized. The set survives a
  restart. Clicking anywhere on a minimized project's row brings it
  back, not just the `＋`.
- Sessions can be minimized from the sidebar too. Every session row
  carries the same `–` control the grid tile has, so a session can be
  pushed out of the grid without first finding its tile. A minimized
  row dims and its control flips to `＋` to bring the session back.

- The sidebar now shows what each session is actually doing. Under every
  session name sits a second, quieter line carrying the window title the
  running program publishes — the task Claude is working on, the command
  a shell is running, the file an editor has open. It updates live, and
  it is there for every session in the list, including ones you have
  never opened in this window and ones that were already running before
  you launched the app. Sessions whose program sets no title look exactly
  as they did before, with no empty space reserved and no change in row
  height.

- Worktrees are now something you can see and manage. A per-project
  worktree browser (⌘E, the ⎇ button on a project row, or "Worktrees…"
  in the command palette) lists every git worktree in the project along
  with what it holds — which sessions are running in it, whether it has
  uncommitted changes, and how many commits it has that aren't pushed —
  plus the local branches that have no worktree at all. From it you can
  start a session in an existing worktree to pick that work back up,
  create a worktree for a branch that was left stranded, rename a
  worktree (which renames its branch and moves its directory to match),
  and delete the ones you're done with. Deleting is deliberately hard to
  do by accident: it is refused outright while a session is running in
  the worktree, and uncommitted changes, unpushed commits, or a branch
  with no remote to compare against each require a confirmation that
  names exactly what would be lost. The branch is kept by default when a
  worktree is deleted, so the commits stay recoverable; deleting it too
  is a separate opt-in.

- Closing a session with uncommitted changes now offers to delete the
  worktree as part of the close, alongside keeping it. The clean-up
  option is marked destructive and is never the default.

- The worktree browser offers **Continue** as well as **New session**.
  Continue starts the agent with its path-scoped resume, picking up the
  last conversation in that worktree; New session starts a fresh one.

- Orphaned branches can be deleted from the worktree browser, not just
  given a worktree. Deleting a branch that still holds unmerged commits
  says how many and requires a second confirmation.

- The worktree browser now recognises squash merges. A branch merged
  with GitHub's "Squash and merge" is not part of the default branch's
  history — its commits were rewritten — so the browser used to report
  finished work as unmerged, sort it to the top as "may still hold
  work", and ask you to force-delete it past a warning about losing
  commits. Such branches now carry a **merged** badge, sort down with
  the rest of the finished work, and delete without the destructive
  confirmation. Detection compares the branch's changes against the
  default branch's history, and consults GitHub for the merges that
  comparison cannot see (which needs the `gh` command; without it, or
  offline, everything still works — some squashes just keep reading as
  unmerged). A branch you have committed on since its merge is never
  treated as merged, whatever GitHub says about it.

- Deleting a branch from the worktree browser can now delete it on the
  remote too. It is a separate button, never the default, and it only
  appears for a branch that actually tracks a remote.

- Worktree and branch rows now show the tip commit's subject line. The
  branch name says what the work was called; the subject says what is
  actually in it, which is what you need when deciding whether a branch
  can go.

- The new-session popup can now name the worktree's branch. Ticking
  "Create in git worktree" reveals a branch-name field; leave it empty to
  keep the previous behaviour of an auto-generated adjective-noun name.

- Sessions now show what they are doing while they start. Opening a session
  used to leave the tile blank — often for many seconds when a git worktree
  had to be created — and then hand you a terminal already painting your
  shell's startup output. The session's area now shows a loading panel with
  the live step list (registered → fetching origin → creating the worktree →
  starting the agent) and only reveals the terminal once it has settled, so
  you land on a clean prompt and can see which step is slow. Restarting a
  session says "Restarting…" the same way.

- The new-session popup (⌘T) now filters as you type. A filter box sits at
  the top of the agent list and narrows it to agents whose name contains what
  you typed. The `1`–`9` row shortcuts still work while the box is empty; once
  you start typing, digits go into the query and the row numbers disappear
  rather than advertising a shortcut that no longer fires. Escape still closes
  the popup, and every opening starts with an empty filter.

- Worktree sessions now inherit the project's agent config. A fresh `git
  worktree add` only materializes tracked files, so an untracked
  `.claude/skills` (or `.agents/skills`, `commands`, `hooks`, `plugins`,
  `output-styles`, `settings.local.json`) stayed behind in the main
  checkout and the agent in the worktree couldn't see any of it. Hive now
  symlinks those entries into each new worktree — symlinks, so a skill you
  add or edit in the main checkout shows up in every worktree
  immediately. Anything already present is left alone, so a committed
  `.claude/settings.json` is never clobbered, and per-checkout state in
  the same directories (lock files, task queues) is deliberately not
  shared. Where symlinks aren't available — Windows without Developer
  Mode or elevation — the entries are copied instead, which works but
  won't pick up later edits from the main checkout. The linked config
  doesn't count as uncommitted work, so a pristine worktree session still
  closes without a "discard changes?" prompt.
- GUI: back / forward navigation between sessions, like an editor or a
  browser. **⌃-** returns to the session you were in before this one and
  **⌃⇧-** goes forward again (**Ctrl+Alt+-** / **Ctrl+Alt+Shift+-** on
  Windows and Linux, where Ctrl+- is already zoom out). History records
  every way of switching — clicking a session in the sidebar, clicking a
  tile in grid view, ⌘1–9, ⌘arrows, ⌘B, or the app switching you itself —
  so "take me back to what I was working on" always works. A session you
  had minimized is restored on the way in; sessions killed while you were
  away are skipped. Also available in the command palette as
  **Go Back** / **Go Forward**.

### Fixed

- A session could go solid black when you entered grid mode or switched
  between sessions in a grid, and stayed black until you resized the
  window. Focusing a terminal made the browser scroll it out of its own
  box to bring the (invisible) input target into view, so the text was
  still there, just parked above the visible area. Nothing is scrolled
  on focus any more.

- The worktree browser kept showing a branch it had already deleted. A
  delete that got partway — the local branch removed, deleting it on
  the remote refused — reported the failure and stopped there, so the
  list was never repainted and the row stayed until the panel was
  reopened. Every worktree action now repaints the list afterwards,
  whether it succeeded, was refused, or half succeeded.

- Restarting hive briefly showed "The process failed to start." on the
  first session you were looking at, which then started working on its
  own a moment later. The daemon starts answering clients before it has
  respawned the shells of the sessions restored from the last run, and
  those sessions were reported as finished rather than as still
  starting, so the session you had focused painted an ended-session
  overlay. They now report as starting until their shell is up, and a
  restored session repaints itself without needing you to click away and
  back.

- The worktree browser was slow to open on a repository with a lot of
  branches — several seconds, during which the panel sat empty. It now
  opens in about a second on a repository with 169 branches, by asking
  git for every branch's state in one go instead of once per branch,
  and by running the remaining checks at the same time as each other
  rather than one after another.

- A long list of branches used to squeeze the worktree list out of the
  worktree browser: with a hundred branches the worktrees were a few
  pixels tall and effectively unreachable. The panel is now a single
  list — worktrees first, then the branches without one — with headings
  that stay put as you scroll.

- Reordering sessions sometimes moved the wrong one, or nothing at all.
  A session's order number is the position the daemon splices at, but
  closing a session used to leave a hole in the sequence rather than
  closing it up. The next session opened then reused a number another
  session still held, which made the sidebar sort ambiguous, and every
  move after that landed a slot or more off — or clamped to the bottom
  of the list. Order is now always recomputed from the actual position,
  and the sessions that shift are told about it; restarting Hive
  repairs a list that had already drifted. Deleting a project had the
  same hole and the same fix.

- Closing a session could delete the directory you were working in. When
  a project's working directory is itself a git worktree — which is how
  hive is often run, from inside its own `.worktrees/` — a session there
  claimed that directory as hive's own, and closing the last session
  removed it along with everything committed in it. Hive now only ever
  deletes worktrees it created, and never the directory a project lives
  in. Introduced during this release; no tagged version shipped it.

- Worktrees are no longer deleted on the strength of a check that could
  not be completed. A permissions or I/O error while inspecting a
  worktree, or a failure to compare a branch against its remote, now
  count as "may hold work" instead of silently reading as clean.

- Modal dialogs no longer strand the keyboard. Tab now stays inside the
  worktree browser, the settings form, the help overlay and any
  confirmation dialog instead of walking into the page behind them
  (where the next stop is a hidden terminal, so keystrokes went
  somewhere invisible). Closing one returns focus to the terminal, and
  cancelling a dialog or an inline rename returns it to the control that
  opened it. Escape inside a rename cancels the edit rather than closing
  the surrounding panel.
- Closing a session in grid mode now repaints the grid. The only repaint on
  the removal path fired when the *active* session was the one closed, so
  closing any other tile left the layout with a hole where it used to be.

- Closing a session no longer leaves a broken tile behind. The daemon
  announces the teardown up front, so focus moves to the next session
  immediately and the closing tile is dimmed instead of frozen — and the red
  `[attach failed: …]` / `[hived: no_such_session: …]` lines that used to be
  painted into a pane on its way out are gone.

- Creating or closing a session no longer freezes the rest of the app. Both
  ran on the daemon's single control connection loop, so every other action
  in every window — renaming, switching projects, opening another session —
  waited behind `git fetch`, `git worktree add`, or `git worktree remove`.
  That work now runs off the loop, with git serialized so concurrent
  sessions in one repo can't collide.

- GUI: ⌘← and ⌘→ (and ⇧⌘← / ⇧⌘→) now move the cursor to the start / end of
  the line in focused mode, the way they do in any macOS terminal. Grid view
  still uses all four arrows to move between tiles. Two things were in the
  way: the app swallowed the keys, and on macOS they were also registered as
  native menu accelerators that consumed them before the app ever saw them —
  those four duplicate menu items are gone. Since a terminal has no text
  selection to extend, the shifted pair moves the cursor rather than
  selecting.
- GUI: ⌘G, ⇧⌘G and ⌘Enter no longer enter a "grid" holding a single tile —
  a view that looks like focused mode but loses its keybindings. They stay
  in focused mode instead, and a grid that shrinks to one tile (the other
  session killed or minimized) returns to focused mode on its own.
- GUI: ⇧⌘↑ / ⇧⌘↓ now move a session one row at a time within its project,
  wrapping at the ends, and never disturb another project. They previously
  sent a position from the project's own list while the daemon read it as a
  position in its global list, so a session in any project but the first
  jumped to an unrelated spot — and sessions created alternately across two
  projects would not move at all.
- GUI: a new or duplicated session (⌘T / ⌘P) now appears directly beneath the
  session it was created from, instead of at the very bottom of the sidebar.
- Codex sessions no longer lose their session ID when Hive happens to read
  the rollout file while codex is still writing it. The file was rejected
  for good on the first unreadable poll, so nothing was captured and the
  session couldn't be resumed after a restart.
- GUI: fixed a startup slowdown that degraded to a freeze when several
  full-screen agent sessions (Claude, etc.) were open in grid view. On
  attach the daemon replayed each session's entire multi-MB scrollback
  ring into the terminal — tens of MB parsed on the main thread across
  all tiles at once, ballooning webview memory until it stalled.
  Full-screen (alt-screen) sessions now replay only a one-screen snapshot
  (no scrollback, which they don't use anyway); normal-screen sessions
  still get their full history. Grid tiles also now attach lazily (active
  tile first, the rest on idle) instead of all at once.
- GUI: recover the control connection automatically. A dropped
  connection to the daemon (sleep/wake, a daemon upgrade) previously left
  the window frozen with no way back except a full restart; it now
  reconnects with backoff and re-syncs.
- GUI: cap repeated WebGL context-loss recovery so a flapping GPU context
  can't spin into a busy loop that freezes the window; the tile falls back
  to the DOM renderer instead. Also bound the number of simultaneous WebGL
  contexts so a many-tile grid can't exhaust the browser limit (which
  showed as magenta blocks in place of terminal text).

### Changed

- ⌘Enter (Ctrl+Enter elsewhere) no longer toggles between grid and
  single view. It was a duplicate of ⌘G, and because the app swallowed
  the chord before the terminal saw it, it was unusable inside an agent
  session. The chord is now unbound and left to the session. ⌘G and
  ⇧⌘G still toggle the project grid and the all-sessions grid, and
  Shift+Enter still inserts a newline without submitting.

- CI is a real gate again. The three scroll/replay end-to-end specs that had
  been quarantined since spec 245 are back on, so CI runs 20 of the 22
  real-daemon tests instead of 2 of the 12 it had then. Most of them were
  never flaky: they demanded that a resize fire a scrollback replay, in a
  scenario where the terminal is following the bottom and the replay is
  deliberately skipped — so they failed against correct code, every time.
  Two tests stayed behind and are documented in spec 245: one is genuinely
  load-dependent, the other never reaches the state it means to assert on.
  A retry no longer turns a failed test into a green check either; it only
  buys a second trace.

- Prompts that risk losing work no longer use a native OS alert. Closing
  a session with uncommitted changes, and deleting a worktree or branch,
  now open an in-app dialog that spells out what is at stake and offers
  the real choices as buttons — deleting a worktree can keep its branch
  or remove both, and cancelling is always available and pre-selected.
  The old alert could only ask yes/no, so the branch question had to be
  asked separately and there was no way to back out of it.

- Closing a session no longer destroys its worktree unless the worktree
  is empty. Previously the worktree was removed as soon as its last
  session closed — force-closing a session with uncommitted changes threw
  that work away, and any worktree not currently held by a session was
  also force-removed the next time the daemon started. A worktree with
  uncommitted changes or unpushed commits now outlives its session and
  stays listed in the new worktree browser, where removing it is an
  explicit, confirmed act. Worktrees with nothing in them are still
  cleaned up automatically on close and at startup, so throwaway sessions
  don't leave directories behind.

- Logs: `hived.log` and `hivegui.log` now rotate at 8 MiB (one prior
  generation kept) instead of growing unbounded. Added always-on
  diagnostic breadcrumbs — a main-thread heartbeat plus per-attach
  replay/timing lines — to make future freezes diagnosable from the logs.

## [2.3.0] — 2026-07-19

### Added

- Agents: define your own tools. A new Settings screen (`⌘,` on macOS, also
  under **File ▸ Settings…** and in the command palette; `Ctrl+,` or the
  command palette elsewhere) lets you add custom
  agents with a name, command line, and color — for example a `claude-lite`
  that runs `claude --model haiku`. Custom agents appear in the `⌘T` launcher
  alongside the built-ins, with the same PATH availability check, and persist
  and revive across restarts. Definitions live in `agents.json` in the Hive
  state directory and can be hand-edited. Built-in agent ids cannot be
  redefined, and custom agents do not support conversation resume — Restart
  re-runs their command rather than continuing the prior session.

- GUI: `⌘B` jumps to the next session that rang the bell (the pulsing
  ones), cycling through them and wrapping — no more hunting for the
  flashing row with the mouse. `⇧⌘B` jumps back to the session you were
  working in before the first `⌘B` — so a round of bells can bounce you
  through several sessions and one keystroke still returns you to the
  work you interrupted. A minimized session that rings its bell is
  restored on the way in and returned to the tray on the way back. Both are in the ⌘/ help overlay, the
  command palette, and the macOS Session menu (`Ctrl+B` / `Ctrl+Shift+B`
  on Windows and Linux).

- Agents: [Pi](https://pi.dev/) (`@earendil-works/pi-coding-agent`) is now
  a launchable agent. Hive pins each session's id via `pi --session-id`, so
  Restart resumes the exact conversation by id — unambiguous even when
  sibling sessions share a worktree (same guarantee as Claude/Gemini).
- GUI: keyboard-shortcuts help overlay on `⌘/`, listing every binding —
  including the ones no menu shows (terminal-level `Ctrl+Shift+C/V/A`
  copy/paste/select-all, `⇧↩` newline, launcher digit keys, sidebar
  resizer keys). Also reachable from the command palette ("Keyboard
  Shortcuts") and the macOS Help menu. The palette's shortcut column
  and the overlay render from one shared table (`lib/shortcuts.js`)
  so they cannot drift, and non-mac platforms now see `Ctrl+`-style
  hints instead of mac glyphs.
- GUI: actionable empty states. First run shows "No sessions yet" with
  a New session button; an empty project and an all-minimized grid get
  their own hints — replacing the bare CSS placeholder text (or a
  fully blank pane in single mode).
- GUI: sidebar project collapse state persists across launches
  (`localStorage`, `hive.collapsedProjects`; pruned when projects are
  deleted).
- GUI: the agent launcher opens instantly with a "Loading agents…" row
  (then "No agents found" when the list is empty) instead of popping
  in fully-formed a beat after the keypress; creating a session
  flashes "creating session…" in the status bar.
- GUI: accessibility pass — the sidebar footer hint no longer claims
  the palette is on ⌘K (it is ⇧⌘K); project collapse carets are real
  buttons with `aria-expanded` and keyboard focus rings; the dead
  session overlay is an `alertdialog`; palette input, tile headers,
  and tray chips carry aria-labels; keyboard-focus rings on sidebar,
  tile, tray, and banner controls.

- GUI: minimize sessions to hide them from grid views. Each tile in
  the project-grid and all-sessions-grid now has a small minimize
  button on the right of its header (`–`). Minimized sessions are
  removed from the grid and appear as chips in a tray below the
  terminals; clicking a chip restores the session to the grid and
  switches to it. Minimized sessions stay alive (output keeps
  buffering) and remain reachable in single-session mode via the
  sidebar, command palette, and `⌘[/]` cycling. State is per-app-run
  (not persisted across launches). (#202)
- Frontend test harness. `scripts/test.sh` runs four layers: Go
  (existing), Vitest unit tests for the new `cmd/hivegui/frontend/src/lib/`
  pure modules, jsdom DOM tests, and a Playwright E2E suite that
  drives the GUI through an in-browser Wails-mock bridge
  (`test/e2e/wails-mock.js`). Grid layout math (`computeGridDims`,
  `buildGridLayout`, `computeSpatialMove`), the visibility gate for
  xterm `fit()`, the snake/camel wire normalizer, and the
  cross-platform `cmdOrCtrl` helper are now covered as pure
  functions — the regression classes behind the recent ctrl-arrow,
  grid-mode-revert, and canvas-resize bugs all have direct unit
  tests. Linux CI runs every layer on every PR.
- Daemon integration tests covering update/restart/resize and
  multi-control-conn broadcast fan-out.
- GUI: last view (single / grid-project / grid-all) persists across
  launches. Previously the GUI always booted into single-focus mode
  regardless of how it was left; now it restores the mode you last
  used. Stored in `localStorage` under `hive.view`. (#187)

### Changed

- GUI: `Ctrl+?` now opens the keyboard-shortcuts panel on Windows and
  Linux, alongside the existing `Ctrl+/`. macOS already accepted both
  `⌘?` and `⌘/` via the Help menu item; those platforms have no native
  menu, so only `Ctrl+/` worked there.
- GUI: the sidebar footer now shows the running version and build of
  `hive` and `hived` instead of the keyboard-shortcut hints. It shows a
  single line when both binaries come from the same build, and expands
  to one line each — highlighted — when they differ, making a stale
  daemon visible at a glance rather than only via the restart banner.
  The shortcut hints are still available via the command palette
  (`⇧⌘K`) and the keyboard-shortcuts overlay (`⌘/`).
- GUI: the Debug menu is relabeled "Toggle Debug Trace" / "Copy Debug
  Trace" (was "Scroll Debug" / "Scroll Trace"); the captured trace now
  covers grid relayout, focus, keydown routing, a main-thread heartbeat,
  and inbound daemon traffic — not just scrolling.

### Fixed

- GUI: "Restart Hive" now actually replaces the daemon — or says why it
  can't. It asks `hived` to exit over the control connection Hive
  already holds, then confirms the socket has gone quiet before
  relaunching, so the new window can no longer come back attached to
  the daemon it was supposed to replace (the stale-build banner
  returning immediately after a "restart" was this bug). Signalling the
  pid recorded in `<sock>.pid` remains as a fallback, but the pidfile is
  no longer trusted as proof: three of its paths could report success
  having killed nothing. If the daemon survives both attempts, the
  restart fails visibly in the banner instead of quitting into a broken
  state. Restarts are also no longer stalled ~5s by a liveness check
  that could never observe the daemon exiting.
- GUI: `hived` exiting no longer deletes a *newer* daemon's pidfile,
  which left Restart Hive with no handle on the running daemon at all.
- GUI: **Restart Hive…** is now in the File menu and the command
  palette. It was previously reachable only from the daemon-stale
  banner, so when the GUI and daemon builds matched there was no way to
  restart Hive from inside the app.
- GUI: errors are written to `hivegui.log` in the Hive state directory,
  next to `hived.log`. Under LaunchServices the GUI's stderr goes to
  `/dev/null`, so failures previously left no trace anywhere on disk.
- GUI: renaming a session from its tile header no longer throws a
  `ReferenceError` and silently discards the new name. The tile
  rename's commit handler called `UpdateSession` without it being
  imported, so the rename input closed but the daemon call never
  fired.
- GUI: full-screen agent sessions (claude/codex/pi and other alternate-
  screen TUIs) no longer freeze for seconds on resize or a single↔grid
  toggle. Such a resize requested a full scrollback replay — the daemon
  re-streams its entire raw byte ring (toward an 8 MB cap on a long-lived
  session) and the GUI parsed it in one synchronous write — but the
  alternate screen has no user-facing scrollback and the program repaints
  itself from SIGWINCH, so the replay was pure cost. It is now skipped on
  the alternate screen; normal-buffer (shell) sessions still replay.

- GUI: scrollback replays no longer destroy the reading position
  ("scrolling jumps around with codex"). When a window resize, sidebar
  drag, or grid reflow triggered a scrollback replay while the user
  was scrolled up reading history, the replay's buffer reset dumped
  them at the bottom — the #213 don't-snap flag only suppressed the
  final explicit snap, after the position was already gone. The replay
  now captures the reader's distance from the bottom before the wipe
  and restores it once the replay has fully parsed. The reset and the
  final viewport placement are also parse-ordered now (xterm's write
  queue is async), so under codex-rate output a replay can no longer
  interleave with unparsed live bytes and paint duplicated lines.
  Deliberate mode switches still snap to the bottom. Adds a scroll
  tracer (`localStorage hive.debug = '1'`, dump
  `window.__hive_scrolltrace`) for field diagnosis, and a real-daemon
  e2e suite (`scroll-codex.spec.js`) that reproduces the bug on the
  old code and locks the invariants.
- GUI: action failures are no longer silent. Copying/pasting inside a
  terminal, creating/duplicating/restarting/closing a session, renaming
  or recoloring or reordering sessions and projects, saving a project,
  and opening the agent launcher now show an error in the status bar
  when the underlying daemon call fails (e.g. connection lost) —
  previously the click just did nothing, with no trace. Status-bar
  errors are guaranteed a minimum visibility window (1.5s), pulse red,
  auto-hide (errors 6s, info 2.5s), revert to the persistent status
  ("connected" / session name) instead of going blank, and are
  announced to screen readers (`role="status"`, `aria-live="polite"`).
  Also fixed: opening the launcher when its sidebar anchor row isn't in
  the DOM could throw and leave the launcher unopened with no feedback.
- GUI: Enter now commits and Escape now cancels inline renames (session
  rows, project rows, and tile headers). A capture-phase
  `stopPropagation()` listener on the rename input was cancelling the
  same input's bubble-phase Enter/Escape handler (DOM dispatch skips
  the bubble invocation once the stop-propagation flag is set), so
  Enter did nothing and Escape couldn't cancel — the rename only ever
  committed when the input lost focus, including after an attempted
  Escape. The handlers now live in the single capture-phase listener.
- Daemon diagnosability hardening: failures that were previously
  swallowed are now logged. Registry metadata-persist failures (12
  sites — session/project entries and order indexes) warn with the
  operation that lost the write; dropping a slow session/project event
  listener warns instead of silently desyncing the GUI (and the event
  buffers grew 16→64 so a many-session reorder can't overflow a
  healthy listener); pidfile cleanup failures are logged at shutdown.
  Worktree creation failures now report every attempted strategy
  (joined errors instead of last-attempt-only), and existing-branch
  detection uses `git rev-parse` exit codes instead of matching git's
  locale-dependent error text. The `hived-ws-bridge` test shim rejects
  malformed JSON-RPC params instead of running handlers on zeroed
  structs, and serializes concurrent frame writes per daemon
  connection so racing writers can't corrupt the wire stream.
- GUI: keystrokes typed immediately after switching to a grid view are no
  longer dropped. Switching to grid reparents the active tile and triggers
  async resize/fit on neighbour tiles, both of which momentarily blurred the
  active terminal to `<body>`; a character typed in that sub-frame gap was
  silently lost (e.g. `hello` → `ello`). A synchronous focus guard now
  reclaims the terminal the instant it blurs during the post-switch settle
  window, and the focus-retry loop no longer re-focuses an already-focused
  textarea (which cleared pending input). (#186)
- GUI: `Shift+Enter` in an agent session now inserts a newline instead
  of submitting, so you can compose multi-line prompts for Claude/Codex.
  xterm sends a bare `\r` for `Shift+Enter` and drops the Shift, so the
  agents couldn't distinguish it from plain Enter and submitted. The
  custom key handler now intercepts `Shift+Enter` and writes Ctrl+J
  (`\x0a`) — the newline byte both agents accept with no terminal
  configuration. Plain Enter still submits; `⌘/Ctrl+Enter` still toggles
  grid-project view. Works on all platforms. (#217)
- GUI: scrolling discipline on mode switch and resize. Switching
  display modes (focused / grid / grid-project) now always snaps
  every visible tile to the bottom — mode toggles are deliberate
  user actions, so the previous "land wherever the buffer happened
  to be" behavior is gone. Resize keeps the existing 2-line
  sticky-bottom tolerance (#163). The viewport is never automatically
  moved away from where the user has placed it: when a window resize
  triggers a scrollback replay while the user is reading deliberate
  scrollback, the replay no longer yanks them to the bottom on
  completion. (#213)
- GUI: three grid-mode regressions and lock-in tests. (1) The first
  time entering grid mode after restart no longer mis-anchors the
  scrollback replay baseline against xterm's 80-column default,
  preventing repeated spurious replays as DPR/fit jitter crosses the
  threshold. (2) Minimizing or restoring a session no longer triggers
  spurious scrollback replays in the remaining tiles when their
  column widths reflow. (3) After resizing the window, toggling the
  sidebar with `⌘S` no longer strands keystrokes on `document.body`
  — the keyboard handler now routes through `toggleSidebar()` and
  the toggle re-asserts keyboard focus the same way `setView` does
  after grid/single transitions. Adds unit coverage for the rebaseline
  helper (`applyRebaseline`) and Playwright e2e regressions for all
  three (plus an R-control case asserting that legitimate window-resize
  replays still fire). (#208)
- GUI: scrollback no longer renders at the narrow grid width after a
  single → grid → single transition, and live output no longer
  overwrites already-scrolled lines. The daemon now keeps a per-session
  raw-byte ring buffer (8 MiB) and re-streams it through a new
  `REQUEST_REPLAY` wire frame whenever the GUI's column count changes
  by ≥4 cols; the frontend `term.reset()`s on a `scrollback_replay_begin`
  event so the replay paints onto a clean buffer. Initial attach uses
  the same path, so reattach scrollback is no longer capped at the old
  500-row vt10x history. (#200)
- Worktrees: surface a log warning when `git fetch origin` fails or
  `origin/HEAD` is not configured, instead of silently falling back to
  local HEAD (or worse, silently basing a new worktree on a stale
  cached `origin/HEAD`). Closes the spec gap in #192's "remote
  unreachable" success criterion.
- Worktrees now branch from `origin/<default-branch>` (typically
  `origin/main`) instead of local HEAD. Previously, creating a
  session-backed worktree from a stale checkout produced a worktree
  on outdated code, wasting agent work on rebases and rediscovering
  already-fixed bugs. `internal/worktree.CreateWorktree` performs a
  best-effort `git fetch origin` and resolves
  `refs/remotes/origin/HEAD` before `git worktree add`. Repos with no
  remote, offline environments, or unreachable upstreams fall back to
  HEAD-based branching transparently. (#192)
- GUI: switching from single-focus mode to grid mode no longer leaves
  the active session visually highlighted but unable to receive
  keystrokes (regression of #181). Single → grid is the only transition
  that fires a synchronous `display:none` flip on the active tile
  during `renderGrid`'s parent class swap, which blurs the xterm
  helper-textarea; ResizeObserver-driven xterm fits on newly-visible
  neighbour tiles then re-blurred it ~10ms after the rAF that #181
  used to re-focus. `setFocusedTile` now polls across up to 8 frames
  and re-establishes focus whenever `document.activeElement` drifts
  off the target's helper-textarea, idempotently. The gate decision
  is extracted into a pure `decideFocusAction` helper with unit
  tests, and the regression class is now covered by Playwright E2E
  via a real xterm + Wails-mock harness. (#186)
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
- Windows: starting a session with an agent (Claude, Gemini, Copilot,
  …) opened a bare `cmd.exe` prompt instead of running the agent. The
  session spawner unconditionally invoked `<shell> -l -i -c <line>`,
  but on Windows the shell falls back to `cmd.exe`, which treats
  `-l -i -c` as garbage and drops the command. The spawn path now
  branches on GOOS: Unix keeps `<shell> -l -i -c <line>` (load-bearing
  for fnm/nvm/asdf PATH setup); Windows uses `%ComSpec% /C <line>`
  with cmd.exe-aware quoting, which also correctly handles `.cmd`
  shims like `claude.cmd`. (#183)
- GUI: terminal text no longer gets stuck displaying garbled glyphs in
  long-lived sessions until the user resizes the window. The xterm.js
  WebGL renderer can lose its GL context (browsers cap simultaneous
  contexts process-wide, so many-tile sessions can trigger this) or
  silently invalidate its glyph atlas on device-pixel-ratio changes
  and visibility transitions. Each session tile now recovers from
  context loss by re-attaching the WebGL addon (or falling back to the
  DOM renderer) and forcing a full repaint, and clears the glyph atlas
  on DPR changes and on becoming visible again. The GL context is also
  disposed explicitly when a tile is destroyed to reduce pressure on
  the per-process context cap. (#190)

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

[Unreleased]: https://github.com/lucascaro/hive/compare/v2.6.0...HEAD
[2.6.0]: https://github.com/lucascaro/hive/compare/v2.5.0...v2.6.0
[2.5.0]: https://github.com/lucascaro/hive/compare/v2.4.0...v2.5.0
[2.4.0]: https://github.com/lucascaro/hive/compare/v2.3.0...v2.4.0
[2.3.0]: https://github.com/lucascaro/hive/compare/v2.2.1...v2.3.0
[2.2.1]: https://github.com/lucascaro/hive/compare/v2.2.0...v2.2.1
[2.2.0]: https://github.com/lucascaro/hive/compare/v2.1.0...v2.2.0
[2.1.0]: https://github.com/lucascaro/hive/compare/v2.0.1...v2.1.0
[2.0.1]: https://github.com/lucascaro/hive/compare/v2.0.0...v2.0.1
[2.0.0]: https://github.com/lucascaro/hive/compare/v2.0.0-alpha.2...v2.0.0
[2.0.0-alpha.2]: https://github.com/lucascaro/hive/compare/v2.0.0-alpha.1...v2.0.0-alpha.2
[2.0.0-alpha.1]: https://github.com/lucascaro/hive/releases/tag/v2.0.0-alpha.1

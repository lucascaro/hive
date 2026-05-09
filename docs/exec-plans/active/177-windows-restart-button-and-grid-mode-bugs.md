# Windows: restart button, grid mode revert, and reversed ctrl-arrow session switch

- **Spec:** [docs/product-specs/177-windows-restart-button-and-grid-mode-bugs.md](../../product-specs/177-windows-restart-button-and-grid-mode-bugs.md)
- **Issue:** #177
- **Stage:** PLAN
- **Status:** active

## Summary

Fix three Windows-only UX bugs in the hivegui:
1. The "Restart Hive" daemon-banner button errors with "restart not supported on this platform" because `killRunningHived` is a stub on Windows.
2. Toggling grid mode flashes the grid then reverts, because the native menu's `Ctrl+G` accelerator and the JS `Ctrl+G` keyboard listener both fire — toggling twice.
3. `Ctrl+Up`/`Ctrl+Down` (and likely all Ctrl+arrow / `Ctrl+G` / `Ctrl+Enter`) misbehave for the same root cause: native menu accelerators on Windows double-fire alongside the JS keyboard handler.

The unifying fix for #2 and #3 is to scope the native menu (with accelerators) to macOS — where it is the visible menu bar that owns shortcuts — and let the JS keyboard handler be the single source of truth on Windows/Linux. The `menu.go` header comment already states the menu is for "the native macOS menu," so this is a latent platform-gating bug exposed when the same menu is built on Windows.

For #1, implement a real Windows kill path that reads `<sock>.pid`, verifies the process is `hived.exe` via `tasklist`, then calls `os.Process.Kill` (which Windows implements as `TerminateProcess`).

## Research

### Bug 1 — restart button no-op on Windows

- `cmd/hivegui/frontend/index.html:13` — the "Restart Hive" button (`#daemon-banner-restart`) only shows when the daemon-stale banner is visible.
- `cmd/hivegui/frontend/src/main.js:1748–1772` — click handler calls `RestartDaemon()`; on error, calls `setStatus(...)` and `showDaemonBanner(...)` with the failure message. So the action *does* surface a message — the user-perceived "does nothing" likely refers to the absence of a restart, not absence of feedback.
- `cmd/hivegui/app.go:239–259` — `RestartDaemon()` calls `killRunningHived(hdaemon.SocketPath())`, then `spawnNewGUI`, then `wruntime.Quit`.
- `cmd/hivegui/restart_unix.go` — full implementation: read `<sock>.pid`, signal-0 probe, `pidLooksLikeHived` via `ps -o comm=`, SIGTERM, escalate to SIGKILL after 3s.
- `cmd/hivegui/restart_windows.go:11–13` — stub returns `errors.New("restart not supported on this platform")`. **Root cause.**
- `cmd/hived/main.go:75–77` — daemon writes pidfile at `<sock>.pid` on every platform, so the read path works on Windows.
- `cmd/hivegui/spawn_windows.go` — already implements daemon launch on Windows (Wails GUI relaunch path is platform-agnostic too).

### Bug 2 — grid mode flashes then reverts

- `cmd/hivegui/menu.go:74` — `view.AddText("Toggle Project Grid", keys.CmdOrCtrl("g"), emit("menu:toggle-project-grid"))`.
- `cmd/hivegui/menu.go:9–13` — header comment: *"wires every keyboard shortcut in the GUI into the native macOS menu."* Yet `cmd/hivegui/main.go:59` registers the menu unconditionally on every platform.
- `cmd/hivegui/frontend/src/main.js:2382–2388` — JS `keydown` listener also handles `Cmd/Ctrl+G` and toggles the view.
- `cmd/hivegui/frontend/src/main.js:2500` — `'menu:toggle-project-grid': toggleProjectGrid` registered handler.
- On Windows, the WinC native accelerator path (`internal/frontend/desktop/windows/menu.go:68` → `acceleratorToWincShortcut`) does NOT consume the keystroke before the WebView sees it — so both fire, and `setView('grid-project')` immediately followed by `setView('single')` produces the visible flash-then-revert.
- The macOS menu eats the keystroke at AppKit level, which is why the bug is Windows-only.

### Bug 3 — ctrl-arrow session switch reversed

- `cmd/hivegui/menu.go:82–85` — Ctrl+Down → next, Ctrl+Up → prev, Ctrl+Right → next, Ctrl+Left → prev.
- `cmd/hivegui/frontend/src/main.js:2423–2438` — JS `keydown` handler: ArrowUp/ArrowDown → `moveActiveSession(±1)`.
- Same double-dispatch as Bug 2. The user perceives "reversed" because two switches with `(idx + delta + n) % n` semantics, when the active session is at index 0 and delta=+1, lands at index 2 instead of 1 — but with two sessions it lands back at 0, etc. With three sessions and active at 0: +1+1=2 (visually "the previous one" if the sidebar wraps). Whatever the exact arithmetic, the user's net experience is "wrong direction."
- Note: Wails/winc keymap (`internal/frontend/desktop/windows/keys.go:170–173`) maps `up/down/left/right` to the correct VK codes, so this is not a Wails translation bug.

### Platform-gating precedent in the codebase

- `cmd/hivegui/restart_*.go`, `cmd/hivegui/spawn_*.go` — already use `//go:build windows` / `!windows` split files.
- We can apply the same split to `menu.go` (move full impl to `menu_darwin.go`, add a stub `menu_other.go` returning a minimal menu with no accelerators).

## Approach

**Bug 1 (restart) — implement the Windows kill path.** Mirror the unix implementation: read `<sock>.pid`, verify the pid is alive and named `hived.exe` using `tasklist /FI "PID eq <pid>" /FO CSV /NH`, then use `os.FindProcess(pid).Kill()` (Windows: `TerminateProcess`). Skip the SIGTERM/grace-period dance — Windows has no in-band soft-kill for a detached process group spawned with `DETACHED_PROCESS`. Best-effort wait for exit (poll `OpenProcess`-equivalent via `os.FindProcess + Signal(0)` analog, or just sleep briefly and re-check pidfile) so the GUI's reconnect doesn't race the dying socket. Reject and clean up stale pidfiles whose pid no longer maps to `hived.exe`, matching the unix safety guarantee.

**Bug 2 + Bug 3 (double-dispatch) — split menu.go by platform.** Move the current `buildAppMenu` body to `menu_darwin.go` (`//go:build darwin`). Add `menu_other.go` (`//go:build !darwin`) that returns `nil` — Wails treats a nil app menu as "no native menu," and the JS keyboard handler already covers every shortcut. This eliminates the double-dispatch on Windows and Linux without losing any user-facing functionality there. macOS keeps its menu bar (which it needs anyway for native conventions like Cmd+Q, About, etc.).

**Why this approach over the obvious alternative.** The "obvious" alternative is to keep one menu and have the JS handler check `if (e.fromMenu)` or detect Windows and bail. That requires plumbing a flag through every shortcut, is fragile, and does not address the fact that Windows users get no visible menu bar from these accelerators anyway (Wails on Windows shows menus only when explicitly given, but the items are not surfaced in a way that maps to user expectations). Splitting at the menu construction boundary is one file and zero conditionals in hot paths.

### Files to change

1. `cmd/hivegui/restart_windows.go` — replace stub with a real implementation: read `<sock>.pid`, verify via `tasklist`, kill via `os.FindProcess`, wait briefly for exit. Mirror the unix file's docstrings for the safety story.
2. `cmd/hivegui/menu.go` → rename to `cmd/hivegui/menu_darwin.go` and add `//go:build darwin` at top. Body unchanged.
3. `cmd/hivegui/menu_other.go` (new) — `//go:build !darwin`; declares `func buildAppMenu(_ *App) *menu.Menu { return nil }`.
4. `CHANGELOG.md` — add three entries under `## [Unreleased]` → `### Fixed`.

### New files

- `cmd/hivegui/menu_other.go` — non-darwin stub returning `nil` so the JS keyboard handler owns all shortcuts.
- `cmd/hivegui/restart_windows_test.go` — unit test for `pidLooksLikeHived` parsing of `tasklist` CSV output (table-driven; does not invoke tasklist).

### Tests

Per `AGENTS.md`, every change ships with tests. Build-tagged tests for Windows-only code are still compiled and run on Windows in CI; on macOS dev they're skipped by the build tag.

- **`cmd/hivegui/restart_windows_test.go`** (new, `//go:build windows`):
  - `TestPidLooksLikeHived_ParsesTasklistCSV` — given canned `tasklist` CSV strings (hived.exe match, mismatch, empty, malformed), the parser returns the expected bool.
  - `TestKillRunningHived_NoPidfile_ReturnsNil` — pointing at a temp dir with no pidfile returns nil.
  - `TestKillRunningHived_StalePidfile_RemovesFile` — write a pidfile pointing at pid 1 (System), confirm the function does not kill and removes the stale file.
- **Manual verification on Windows** (cannot be automated from a macOS dev box):
  - Click "Restart Hive" button — Hive process restarts cleanly, no error banner, daemon comes back up.
  - Press `Ctrl+G` — view enters grid mode and stays there until pressed again.
  - Press `Ctrl+Down` repeatedly — active session advances forward through the sidebar; `Ctrl+Up` advances backward.
  - Press `Ctrl+Enter`, `Ctrl+Shift+G`, `Ctrl+Right`, `Ctrl+Left` — all behave identically to macOS.
- **Regression check on macOS** (`go test ./...` + manual): native menu bar still present, every menu item still works, every shortcut still works. (No code change to the darwin path beyond the file rename.)

## Decision log

- **2026-05-09** — Combine all three Windows bugs into one PR. Why: bugs 2 and 3 share a root cause (native-menu/JS double-dispatch) and a single fix (platform-gate the menu); shipping them together avoids two PRs touching the same file.
- **2026-05-09** — Use `os.Process.Kill` (TerminateProcess) instead of CTRL_BREAK_EVENT. Why: hived is spawned with `DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP` but does not install a console control handler, so CTRL_BREAK has no defined effect. Hard-kill matches the Windows GUI restart convention and the SIGKILL fallback already in `restart_unix.go`.
- **2026-05-09** — Verify pid identity with `tasklist` (no extra dependencies) rather than `gopsutil` or syscall-level `CreateToolhelp32Snapshot`. Why: `tasklist` ships with every Windows install, has stable CSV output, and the unix path uses the analogous `ps`. Symmetry > library churn.

## Progress

- **2026-05-09** — Spec + plan created via /hs-feature-loop.
- **2026-05-09** — Triage classified as bug / L / P2.
- **2026-05-09** — Research complete; root causes identified.
- **2026-05-09** — Plan drafted and entering implementation.

## Open questions

- Wails v2 may show an empty menu bar slot on Windows when `buildAppMenu` returns `nil`. If so, drop a single label-only menu (no accelerators) instead. Verify on first Windows test.

# Add Quit, Restart and Start-at-Login controls to the menu bar

- **Spec:** [docs/product-specs/551-add-quit-restart-and-start-at-login-controls-to-the.md](../../product-specs/551-add-quit-restart-and-start-at-login-controls-to-the.md)
- **Issue:** #551
- **Status:** active

## Summary

hivebar's menu has no way to stop or restart just the menu-bar process, and
login autostart is only toggleable from the GUI's Settings. This plan adds
three items to the fixed-pool menu — Quit Menu Bar, Restart Menu Bar, and a
Start at Login checkmark — without touching daemon semantics. The full *why*
is in the spec.

## Research

### Relevant code

- `cmd/hivebar/menu.go:23-110` — fixed pool: every item is created once in
  `Ready()` and only retitled/shown/hidden afterwards (appending re-orders the
  menu; see the type comment). New items must be created in `Ready()` in their
  final position. Footer is `Open Hive / Reload GUI / Restart Daemon… /
  Check for Updates…`, separator, `Quit Hive` (`menu.go:104-109`).
- `cmd/hivebar/menu.go:205-248` — `watchFooter` is one goroutine selecting on
  every footer `ClickedCh`; `confirmQuit` calls `ShutdownDaemon` then
  `systray.Quit()`. The new Quit path is `systray.Quit()` alone.
- `cmd/hivebar/main.go` — `claimSingleton()` (flock on `hivebar.lock`, 
  `singleton.go`) then `systray.Run`. `release` is `defer`red, so it runs when
  `systray.Run` returns after `systray.Quit()`. A second instance that loses
  the flock exits quietly — which is exactly what makes **Restart** racy: a
  relauncher started before the current process released the lock would lose.
- `cmd/hivebar/actions.go` — `hiveAppBundle()` (hivebar → enclosing
  `Hive.app`), `OpenGUI`, `spawnHived`, `Confirm` (osascript dialog). No
  existing "relaunch myself" or error-surfacing (non-confirm) dialog helper.
- `internal/menubar/menubar_darwin.go:35-62` — `Spawn()` launches hivebar with
  `open -a <hivebar.app>`; `open` (not exec of the inner binary) is mandatory
  because LaunchServices must own a bundle for it to get a status item.
  hived and hivegui both call it on boot, so a plain Quit lasts until the
  next daemon/GUI launch (accepted in the spec).
- `cmd/hivegui/loginitem_darwin.go` — `MenuBarLoginItemStatus()` /
  `SetMenuBarLoginItem(enable)` via cgo `SMAppService loginItemServiceWithIdentifier`,
  identifier `com.wails.hivegui.hivebar`. **Resolved against the calling
  process's bundle**: works from hivegui inside Hive.app, fails with "Invalid
  argument" from anywhere else. hivebar.app is itself the helper, so it cannot
  call this on itself.
- `cmd/hivegui/frontend/src/components/modals/Settings.tsx:306-353` — status is
  re-read on modal open (not tracked while closed), so flipping the item from
  the menu bar is picked up the next time Settings opens. No Settings change
  is needed for criterion 3.
- `cmd/hivegui/main.go:99` — `printVersion(os.Stdout, os.Args[1:])` runs before
  `wails.Run`, `menubar.Spawn()` and window setup, and returns. A second
  early-exit subcommand has precedent and never creates a window or single-
  instance lock (those live inside `wails.Run`).
- `cmd/hivebar/client.go:262-285` — existing hivebar → daemon commands
  (`FocusSession`, `ReloadGUIs`, `ShutdownDaemon`, `CheckForUpdates`) via
  `wire.FrameClientCommand`; GUI-routed alternative to the subcommand below.
- Tests: `cmd/hivebar/{model,client,singleton}_test.go` are darwin-only; the
  menu itself is not unit-tested beyond `TestMenuHoldsUpdatesUntilReady`
  (`model_test.go:276`), which uses `Menu.Update` before `Ready`.

### Constraints / dependencies

- **Login-item toggle must run in the Hive.app bundle context**, and `hived`
  must stay cgo-free (cross-compiles; `DESIGN.md`), so the daemon cannot own it.
  Two candidate routes: (A) hivebar execs `Hive.app/Contents/MacOS/hivegui
  --menubar-login-item=<status|enable|disable>`, a headless subcommand that
  calls the existing cgo functions and exits; (B) route a new wire command to a
  running GUI, which only works while a GUI window is open. **A fits the
  stated motivation** (a menu bar you are fixing is exactly when no GUI is
  running) and needs no wire change, so no `DaemonContract` bump.
- The checkmark must be truthful (criterion 4): read status on a **timer or
  on menu open**, not once. systray (fyne.io/systray v1.12.2) has no
  "menu will open" callback on darwin → needs a bounded decision (see Open
  questions).
- `HIVE_STATE_DIR`-isolated runs never spawn hivebar (`Spawn`); tests of the
  real thing stay on the darwin build and out of CI e2e-real.
- Windows/Linux: hivebar is darwin-only (`main_other.go`); no change.

### Prior lessons

No prior lessons matched (brain search for the feature terms returned
nothing).

### Conventions card

From `AGENTS.md`:

- Build: `./build.sh` (macOS .app) — and **plain `wails build`, never `-s`**.
  Tests: `scripts/test.sh [go|unit|dom|e2e]`. Go: `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`
  and `for os in darwin linux windows; do GOOS=$os staticcheck ./... ; GOOS=$os go vet ./... ; done`.
- Shell out only via `internal/proc` (`TestNoDirectExecOnWindows`) — hivebar
  already uses `os/exec` directly in `actions.go`; stay in that file's pattern
  (hivebar is darwin-only) and check the test's allowlist.
- TDD: a test ships with every behavior change. Go tests beside source.
- Changeset: `.changesets/<slug>.md` (`type: added`, `bump: minor`); never edit
  `CHANGELOG.md`. A user-visible **feature** also adds a `site/features.json`
  entry with `since: "Unreleased"`.
- README: `cmd/hivebar/README.md` documents "what it deliberately does not
  implement" and the layout table — update it. Root `README.md` only if
  menu-bar items are listed there.
- `DESIGN.md` only for structural changes — none expected.
- `DaemonContract` bump only for daemon-side behavior; none here under route A.
- Validate by exit code, not grepped output; run Playwright with `CI=1` if the
  frontend is touched (it should not be).

## Approach

Three new fixed-pool items in hivebar's footer, in `Ready()`, final order:

    Open Hive / Reload GUI / Restart Daemon… / Check for Updates…
    ───
    Start at Login (checkmark) / Restart Menu Bar / Quit Menu Bar
    ───
    Quit Hive            (unchanged: confirm, kills daemon)

**Quit Menu Bar** — `systray.Quit()` only. No confirm, no daemon call. `main`'s
deferred `release()` drops the flock on return from `systray.Run`.

**Restart Menu Bar** — spawn a *new* instance first, then quit: run
`open -n -a <this hivebar.app> --args -restart`, then `systray.Quit()`.
`-n` is required (without it LaunchServices activates the still-running
instance instead of launching). The new process parses `-restart` in `main`
and passes `wait` to `claimSingleton`, which polls the flock (50 ms, 5 s cap)
instead of failing immediately; the old process releases it on exit. No shell,
no sleep. If the old instance never exits the new one gives up quietly after 5 s
(same exit-quietly rule as today). Own bundle path = `enclosingAppBundle(os.Executable())`.

Restart quits **only if the spawn succeeded**: `restartMenuBar(run runner, quit func())`
calls `quit()` iff `run(open …)` returned nil; on error it logs and shows the
`Notify` alert and the current bar keeps running. All hivegui execs (probe, set)
and the `open` use `exec.CommandContext` with a 5 s timeout; a timeout counts as
a failed call. Footer clicks that exec (toggle, restart) run in their own
goroutine, never on the `watchFooter` select loop, so Quit Menu Bar and the other
items stay responsive during a slow exec or an open alert; a `toggling` flag
drops a second toggle click while one is in flight. No alert is shown once quit
has begun (`quitting` flag set before `systray.Quit()`).

**Start at Login** — SMAppService only works from the Hive.app bundle context
(`cmd/hivegui/loginitem_darwin.go` doc), and hived must stay cgo-free, so:
add a headless early-exit mode to hivegui, handled in `main()` next to
`printVersion` and before `setupLogFile`/`menubar.Spawn`/`wails.Run`:
`hivegui --menubar-login-item=status|enable|disable`. `status` prints the
existing `MenuBarLoginItemStatus()` string (`enabled`, `not-registered`,
`requires-approval`, `not-found`, `unsupported`); `enable`/`disable` call
`SetMenuBarLoginItem` and on error print the message to stderr and exit 1.
hivebar execs `<Hive.app>/Contents/MacOS/hivegui` with that flag
(`hiveAppBundle()` already resolves the bundle). No wire change → no
`DaemonContract` bump; the coupling is hivebar↔hivegui inside one bundle, built
together. The probe prints a sentinel line `hive-login-item: <status>`; any other output (e.g. an older hivegui that ignores the flag — the 5 s timeout then kills the stray GUI) is a probe failure → item hidden. All footer handlers (including existing `confirmRestart`/`confirmQuit`) go through a pure-forwarding `dispatch`, so no dialog blocks Quit Menu Bar; `toggling` is cleared before the failure alert is shown.

Menu state is a pure function `loginItemView(status string, err error) view`
(hidden for `not-found`/`unsupported`/probe failure; checked iff `enabled`;
title suffix "— approve in System Settings" for `requires-approval`).
`requires-approval` click does **not** call enable again: it opens System
Settings → Login Items (`open x-apple.systempreferences:com.apple.LoginItems-Extension`)
and re-probes on the next menu open. `enabled` click → disable; `not-registered`
click → enable.
Refresh: once in `Ready()` (async), on every `systray.TrayOpenedCh` receive,
and after each toggle. The receive loop is a separate goroutine from the probe
worker and only sets a size-1 `dirty` flag (non-blocking), so an open signal
arriving mid-probe causes exactly one more probe rather than being lost
(`TrayOpenedCh` is unbuffered and the darwin send is non-blocking). Results are
applied under the existing Menu mutex. On toggle failure show a native alert with the stderr text (new `Notify` helper in
`actions.go`, osascript `display dialog … buttons {"OK"}`), then re-probe so the
checkmark reflects reality. Settings already re-reads status on open
(`Settings.tsx:306`), so no frontend change.

Rejected: wire command to a running GUI (fails exactly when no GUI is up, needs
wire + 3 clients + contract bump); polling on a ticker (operator chose
check-on-open).

### Files to change
1. `cmd/hivebar/menu.go` — three items in `Ready()`; click cases in `watchFooter`; `TrayOpenedCh` watcher; `applyLoginItem(view)`.
2. `cmd/hivebar/actions.go` — `relaunchSelf()`, `Notify()`, `hivegui path` helper.
3. `cmd/hivebar/main.go` — `-restart` flag → `claimSingleton(wait)`.
4. `cmd/hivebar/singleton.go` — `claimSingleton(wait time.Duration)` polling variant.
5. `cmd/hivegui/main.go` — call `handleMenuBarLoginItemFlag` before anything else.
6. `cmd/hivebar/README.md` — document items, `-restart`, the hivegui probe; update layout table.
7. `.changesets/551-menu-bar-quit-restart-login.md` (`added`, `minor`).
8. `site/features.json` — entry with `blurb`, `status: "shipped"`, `since: "Unreleased"` (enforced by `whats-new.test.ts`; run `scripts/test.sh unit`).
9. `DESIGN.md` hard rule "`hivebar` is a client" (lines ~59-68) — allow the headless `hivegui --menubar-login-item` probe subprocess and the `-restart` self-relaunch; check `docs/design-docs/control-plane.md` for hivebar menu listings and fix if present.
10. `cmd/hivegui/main.go` also: `os.Exit(code)` when handled.

### New files
- `cmd/hivebar/loginitem.go` — `loginItemView`, `probeLoginItem`/`setLoginItem` over an injectable runner.
- `cmd/hivegui/loginitem_cli.go` — `handleMenuBarLoginItemFlag(args, stdout, stderr, backend) (handled bool, code int)`; backend interface satisfied by `*App` on darwin and a stub elsewhere.

### Tests
- `cmd/hivegui/loginitem_cli_test.go`: `TestLoginItemFlagStatusPrintsBackendValue`, `TestLoginItemFlagEnableDisableCallBackend`, `TestLoginItemFlagErrorGoesToStderrExit1`, `TestLoginItemFlagIgnoresOtherArgs` (returns handled=false so normal startup proceeds).
- `cmd/hivebar/loginitem_test.go`: `TestLoginItemViewMatrix` (each status → visible/checked/title), `TestToggleLoginItemEnablesWhenOffAndDisablesWhenOn`, `TestToggleFailureReturnsStderrAndReprobes`, `TestProbeFailureHidesItem`.
- `cmd/hivebar/singleton_test.go`: `TestClaimSingletonWaitsForRelease` (holder releases at 200 ms; wait variant succeeds, no-wait fails), `TestClaimSingletonWaitGivesUp` (injected 100 ms cap, not 5 s).
- `cmd/hivebar/actions_test.go`: `TestRelaunchCommandUsesNewInstanceFlag` (argv is `open -n -a <app> --args -restart`); `TestRestartQuitsOnlyWhenSpawnSucceeds` (fake runner nil → quit called once; fake runner error → quit NOT called, Notify called); `TestRunWithTimeoutKillsSlowCommand` (the single production wrapper `runWithTimeout(d, name, args...)` over real `exec.CommandContext`, run against a real `sleep 5` with a 50 ms cap; asserts a deadline error within 1 s; mutation: delete the context → fails); `TestRequiresApprovalClickOpensSystemSettings` (runner argv is the LoginItems URL, not `enable`).
- `cmd/hivebar/menu_pool_test.go`: `TestAddMenuItemOnlyInsideReady` — go/parser over **every** non-test `.go` file in `cmd/hivebar`; fails on any call to `systray.AddMenuItem|AddMenuItemCheckbox|AddSeparator` (or `*MenuItem.AddSubMenuItem`) whose enclosing func is not `(*Menu).Ready`. Pins criterion 6 for new files too.
- `cmd/hivebar/menu_click_test.go`: `TestSlowToggleDoesNotBlockFooterLoop` — with a blocking fake runner mid-toggle, a Quit Menu Bar click still reaches `quit()` within 1 s (footer dispatch extracted to a testable `dispatch(click)` that spawns handlers).
- `cmd/hivebar/loginitem_test.go` adds `TestOpenSignalDuringProbeTriggersOneMoreProbe` (dirty-flag).
- `cmd/hivegui/main_order_test.go`: `TestLoginItemFlagRunsBeforeStartup` — go/parser over `main.go`'s `main()` body; asserts the `handleMenuBarLoginItemFlag` call precedes `setupLogFile`, `logLaunch`, `menubar.Spawn` and `wails.Run`. Mutation check: moving it below `menubar.Spawn()` must fail the test.
- Existing `TestMenuHoldsUpdatesUntilReady` must still pass.

### Verification
- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./cmd/hivebar/... ./cmd/hivegui/...` exit 0.
- `for os in darwin linux windows; do GOOS=$os go vet ./cmd/... ./internal/... || exit 1; done` and staticcheck per OS.
- `scripts/test.sh go` exit 0; `scripts/check-daemon-contract.sh origin/main HEAD` exit 0 (nothing under internal/{wire,daemon,session,registry}).
- Real build: `./build.sh`; then manual smoke on the .app (macOS only):
  `Hive.app/Contents/MacOS/hivegui --menubar-login-item=status` prints a known status and exits 0 with no window;
  record `pgrep hived`; `open` hivebar; Restart Menu Bar → new hivebar PID in `hivebar.lock`, same hived PID; Quit Menu Bar → no hivebar process, hived PID unchanged, GUI sessions still attached;
  toggle Start at Login and confirm `…status` flips and Settings → Start at login agrees when reopened.
  **These smoke rows are mandatory, not best-effort**: criteria 1–3 are only proven here (`open -n` of the nested helper and the real lock handoff are invisible to unit tests). Clicks are driven through System Events UI scripting; if accessibility is not granted for the driving terminal, the rows are listed in the PR as "operator must run" and `/hs-merge-gate` must not pass them as verified. Also assert the probe is side-effect free: after `hivegui --menubar-login-item=status`, no new hivegui/hivebar process exists and `hivegui.log` is untouched.
- Mutation checks (stage the fix first; never `git checkout --`): (a) make `claimSingleton(wait)` ignore `wait` → `TestClaimSingletonWaitsForRelease` fails; (b) drop the `-restart` → `wait` plumbing in `cmd/hivebar/main.go` → a `TestMainRestartFlagEnablesWait` (flag parsing extracted to `parseFlags(args)`) fails; (c) move the hivegui flag call below `menubar.Spawn()` → `TestLoginItemFlagRunsBeforeStartup` fails; (d) quit unconditionally in `restartMenuBar` → `TestRestartQuitsOnlyWhenSpawnSucceeds` fails.

### Open questions / risks
- `open -n` of a nested LoginItems helper must be proven on a real build (menubar.Spawn uses `open -a` without `-n`).
- `TrayOpenedCh` is a non-blocking send on a size-? channel; a burst of opens may drop signals — acceptable, next open refreshes.
- hivegui subcommand adds ~100 ms exec per probe; async so the menu never blocks.
- `TestNoDirectExecOnWindows` allowlist: hivebar already uses `os/exec` (darwin-only); confirm still green.
- Unsigned dev build: enable fails ("Invalid argument") → alert shows reason, checkmark stays off (criterion 4).

## Second opinion

Independent reviewer (hs-reviewer agent), two rounds. Round 1: `revise`, confidence 7 — six must-fix items (Restart quit unconditionally; argv-only restart test; vacuous pool test; no exec timeouts / blocking footer loop; requires-approval semantics; no proof the hivegui flag runs before startup), all applied. Round 2: `revise`, confidence 7 — one must-fix (timeout test exercised only a fake; now a `runWithTimeout` wrapper tested against real `sleep`), applied. Applied nice-to-haves: probe sentinel line, `toggling` cleared before alert, all handlers via `dispatch`, DESIGN.md/features.json items. Not applied: nothing material. Presented to the operator with no unresolved must-fix.

## Decision log

- **2026-11-14** — Login-item toggle via headless `hivegui --menubar-login-item` subcommand, not a wire command. Why: works with no GUI open (exactly when the bar is stuck), no DaemonContract bump. Operator chose.
- **2026-11-14** — Checkmark refreshed on menu open (`systray.TrayOpenedCh`) and after each toggle, not polled. Why: operator choice; the hook exists in systray v1.12.2.
- **2026-11-14** — Triage: enhancement / M / P2, confirmed by operator. Why: new fixed-pool items, relaunch logic, cross-process login-item toggle.
- **2026-11-14** — Quit Menu Bar does not confirm; it is non-destructive and reversible. Why: operator choice at brainstorm; confirm overlay rule targets destructive actions.
- **2026-11-14** — Quit is not sticky: hived/hivegui respawn it on next launch. Why: operator confirmed this is the "restart" path; `HIVE_NO_MENUBAR` stays the hard off.

## Progress

- **2026-11-14** — Spec #551 ingested and triaged; research complete.
- **2026-11-14** — Plan drafted, two second-opinion rounds, approved by operator.
- **2026-11-14** — Implemented on `feature/551-menubar-controls`. `scripts/test.sh go unit` green; vet + staticcheck clean on darwin/linux/windows; 8 mutation checks killed (the first draft of two were flawed mutations or a hang-prone test; both fixed).
- **2026-11-14** — Real-build smoke (isolated `HIVE_STATE_DIR`): `hivegui --menubar-login-item=status` printed `hive-login-item: enabled`, exit 0, no new process, no state-dir writes; `bogus` → exit 2. `open -n … --args -restart` started a second hivebar that waited, then took the lock once the first exited (same lock-file PID handoff Restart relies on). **Not verified: the actual menu clicks (Quit/Restart/Start at Login) — System Events scripting was refused (Automation not authorized, -1743), and the enable/disable write path was not exercised so as not to change the operator's real login item. Operator must run these rows.**

## Open questions

None blocking. `open -n` of the nested helper is proven only by the mandatory real-build smoke.

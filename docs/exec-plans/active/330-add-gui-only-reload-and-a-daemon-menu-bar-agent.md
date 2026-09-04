# Add GUI-only reload and a daemon menu-bar agent

- **Spec:** [docs/product-specs/330-add-gui-only-reload-and-a-daemon-menu-bar-agent.md](../../product-specs/330-add-gui-only-reload-and-a-daemon-menu-bar-agent.md)
- **Issue:** —
- **Branch:** `feature/330-gui-only-reload-menubar`
- **PR:** [#333](https://github.com/lucascaro/hive/pull/333)
- **Status:** active

## Summary

Add a `buildinfo.DaemonContract` integer that says when a GUI-only reload is
safe, wire a reload path that relaunches every GUI window while leaving `hived`
and its PTYs alone, and add `hivebar` — a macOS menu-bar agent that makes the
daemon visible and controllable independent of the GUI. Authored via plan-first
mode; the design decisions below were taken interactively with the operator.

## Research

- `cmd/hivegui/app_control.go` — `RestartDaemon` sends `FrameShutdown`, falls
  back to `killRunningHived`, probes the socket, then `spawnNewGUI` + `Quit`.
  Its tail (close conns, relaunch, quit) is exactly the GUI-only reload, minus
  the kill. `daemonVersionEvent` computes staleness from build IDs alone.
- `cmd/hivegui/spawn_unix.go`, `window_unix.go` — `hived` and each GUI window
  are started via `startDetached` (`Setsid: true`); on macOS the GUI relaunch
  is `open -n <bundle>` with `HIVE_LAUNCH_DIR` carrying the launch dir. `hived`
  has no idle exit, so it already survives a GUI quit.
- `cmd/hivegui/app_calls.go:318` — `OpenNewWindow` is `spawnNewGUI`, so every
  window is its own process. A per-window reload would leave siblings running
  old code.
- `internal/buildinfo/` — `BuildID()` (git revision, falls back to the embedded
  `vcs.revision`) and `Version()` ("dev" when unstamped). Both stamped by
  `build.sh` via `-ldflags` into `hivegui` and `hived` from one build.
- `internal/wire/control.go` — `Hello`/`Welcome` already carry `build_id`, and
  `Welcome` carries `release`. `internal/wire/frame.go` — frame ids run to
  `0x1f`; `PROTOCOL_VERSION = 1`.
- `internal/daemon/daemon.go:452` — strict `hello.Version != PROTOCOL_VERSION`
  rejection. The `default:` arm of `handleControlFrame` only logs unknown
  frames, leaving the connection alive.
- `internal/registry/events.go` — the `Subscribe`/broadcast/slow-listener-drop
  shape to copy for a command hub.
- `cmd/hivegui/update*.go` (~3300 lines) — staging, bundle swap, then
  `restartDaemonFn`. `frontend/src/lib/update-state.ts` is a pure, unit-tested
  reducer feeding both the banner and Settings.
- `cmd/hivegui/menu_darwin.go` — native menu; every item emits `menu:<action>`.
  Already has "Restart Hive…" with no accelerator, deliberately.
- No tray/systray code exists anywhere in the repo; Wails v2 has no menu-bar
  API. `build.sh` has no `codesign` step, so the bundle ships unsigned.
- External: `fyne.io/systray` v1.12.2 (Jun 2026, actively maintained).
  `MenuItem.Remove()` is a real native removal; submenus, checkboxes and
  runtime `SetTitle()` all supported; `Run()` locks the OS thread itself and
  must be called from `main()`; macOS requires an `.app` bundle.

## Approach

**Compatibility is a contract integer, not a build ID.** `BuildID` is a git
revision, so comparing it treats a CSS tweak as a stale daemon. A new
`buildinfo.DaemonContract` is bumped by hand only when a GUI built against the
new tree cannot correctly drive a daemon built against the old one, and a CI
guard fails any PR that changes daemon-side code without touching it. Rejected:
per-binary semver (two tags, two changelogs, and the strings still do not
answer "can this pair talk"), and hashing the `hived` binary (any unrelated
recompile forces a needless full restart, killing every session).

**`PROTOCOL_VERSION` stays at 1.** The daemon rejects a mismatched
`hello.Version` outright, so bumping it would stop a new GUI from handshaking
with an old daemon — and it could then never read the contract it needs to
decide anything. Unknown control frames are only logged, so adding frames is
backward compatible. A future genuine protocol break bumps both.

**The reload fans out through the daemon.** One generic
`FrameClientCommand`/`FrameClientBroadcast` pair plus a small command hub in
`internal/daemon`, rather than a frame pair per action: `reload_gui` and
`focus_session` both need it now and `hivebar` will want more. The hub lives in
`daemon`, not `registry` — commands are transient fan-out, and the registry is
the writer of persisted state only (DESIGN.md hard rule).

**The menu bar is its own bundle.** `hivebar.app` (LSUIElement) talks to
`hived` over the existing control wire. Rejected: embedding the tray in `hived`
(pulls AppKit and cgo into a headless daemon and breaks the Linux build) and in
`hivegui` (it would vanish exactly when it is needed — during a reload, or
after the GUI is quit).

### Phase 1 — Daemon contract

New `internal/buildinfo/contract.go`:

```go
// DaemonContract is the compatibility generation of everything the
// daemon exposes: wire frames, session semantics, registry layout.
// Bump it whenever a GUI built against the new tree cannot correctly
// drive a daemon built against the old one. Do NOT bump for GUI-only
// or cosmetic daemon-side changes — a needless bump costs the user
// every running session.
const DaemonContract = 1

// Identity is what `hived --version --json` prints and what Welcome
// carries, so a staged bundle can be interrogated before it is applied.
type Identity struct{ Release, BuildID string; DaemonContract int }
```

`wire.Welcome` gains `DaemonContract int \`json:"daemon_contract,omitempty"\``.
Omitempty keeps a pre-contract daemon parsing cleanly; `0` means "unknown" and
always forces a full restart. The daemon fills it in both `Welcome`
constructions. `cmd/hived` gains a `--version` flag that prints `Identity` as
JSON and exits — that is how the GUI reads the contract of a staged bundle it
has not installed.

`daemonVersionEvent` gains contract comparison, and `DaemonStaleEvent.Severity`
becomes:

| Severity | Meaning | Offered action |
|---|---|---|
| `match` | same build ID | none (clears the banner) |
| `reloadable` | build IDs differ, contracts equal and non-zero | none — compatible, and the footer says so (see the decision log) |
| `mismatch` | contracts differ, or either is 0 | Restart Daemon — ends all sessions |
| `unknown` | a build ID is missing | Restart Daemon |

`ConnectControl` also detects the raw `daemon refused connection: server speaks
vN` string produced by the strict version check and routes it to the `mismatch`
banner, so a genuine protocol break lands the user on "Restart Daemon" instead
of a dead connection.

### Phase 2 — Reload fan-out

Two new frames: `FrameClientCommand 0x20` (C to S) and `FrameClientBroadcast
0x21` (S to C), carrying `wire.ClientCommand{Cmd, SessionID}`. A new
`internal/daemon/commands.go` hub (~40 lines) copies the shape of
`internal/registry/events.go`, slow-listener drop and log line included.
Control connections subscribe to it alongside their registry subscriptions;
`handleControlFrame` validates `Cmd` against an allowlist (unknown returns an
error and is not fanned out) and publishes.

`controlReadLoop` handles `FrameClientBroadcast`: `reload_gui` calls
`a.ReloadGUI()`, anything else is emitted to the frontend as `client:command`.
A per-process `reloading` flag guards against a broadcast storm.

`App.ReloadGUI()` is the tail of `RestartDaemon` — extracted into a shared
`relaunchSelf()` — and nothing else. It must never send `FrameShutdown` and
never call `killRunningHived`. `App.RequestReloadAllGUIs()` writes
`FrameClientCommand{reload_gui}`; the daemon fans it back to every window
including the caller.

Menu (`cmd/hivegui/menu_darwin.go`, File): add "Reload GUI" emitting
`menu:reload-gui` with no accelerator (⌘R is a reload reflex users would fire
mid-agent-run; "Restart Hive…" set this precedent), and relabel "Restart Hive…"
to "Restart Daemon… (ends all sessions)". Per the AGENTS.md Keybindings Policy
both appear in the ⌘/ help overlay and the command palette, and README's action
list is updated.

### Phase 3 — Update-path clarity

After staging, `update_apply_darwin.go` runs
`<staged>.app/Contents/MacOS/hived --version --json` and records the contract.
`UpdateInfo` gains `restartKind: "gui" | "full"`. `update-state.ts` is pure and
already unit-tested, so the label derives there: `gui` reads "Reload GUI" with
"Your sessions keep running."; `full` reads "Restart Hive" with "This ends all
running sessions.". `banners.ts` renders the same split for the stale-daemon
banner, and apply routes to `RequestReloadAllGUIs()` or `RestartDaemon()`.

### Phase 4 — `cmd/hivebar` (darwin)

A pure wire client: no PTY, no `internal/session`, no registry writes — the
same layering rule the GUI obeys.

| File | Purpose |
|---|---|
| `main.go` | `systray.Run` from `main()`; wires client to model to menu |
| `client.go` | control conn, handshake, reconnect; consumes the PROJECTS/SESSIONS snapshot and SESSION_EVENT stream that already arrive on handshake |
| `model.go` | pure snapshot-to-menu-model; all logic here so it is testable without a status bar |
| `menu.go` | model to systray items; `Remove()` and rebuild on change |
| `singleton_darwin.go` | flock on `<stateDir>/hivebar.lock`, so a double spawn is a no-op |
| `loginitem_darwin.go` / `_other.go` | cgo `SMAppService` register / unregister / status |
| `icon/` | 22px template PNG via `SetTemplateIcon`, follows light and dark |

Menu content: a header line with release, build and contract state; a summary
line (`Sessions: N across M projects · K need attention`, using
`internal/activity`); a submenu per project listing sessions, where clicking one
sends `focus_session`; then "Check for Updates…", "Reload GUI", "Restart
Daemon…", "Open Hive", "Quit Hive".

> `ponytail:` "Check for Updates…" delegates to the GUI — it launches or
> focuses `hivegui` and asks it to open the update flow, rather than
> duplicating the ~3300 lines in `cmd/hivegui/update*.go`. The GUI is the thing
> being replaced and has to restart anyway, so it is the right owner. Upgrade
> path if this ever needs to work headless: lift `update*.go` into
> `internal/update` and have both binaries call it.

`build.sh` builds `hivebar` universal, wraps it in a minimal `.app`
(`LSUIElement=1`, id `com.wails.hivegui.hivebar`) and installs it to
`hivegui.app/Contents/Library/LoginItems/hivebar.app`. Both `cmd/hived` and
`cmd/hivegui` spawn it best-effort on boot (darwin only, singleton-guarded,
skipped by `HIVE_NO_MENUBAR=1`), locating it the way `locateHived` works.
A Settings toggle, "Start Hive menu bar at login", calls `SMAppService` through
cgo and surfaces its real error when the build is unsigned.

### Phase 5 — Contract CI guard

`scripts/check-daemon-contract.sh` fails when a non-test file under
`internal/{wire,daemon,session,registry}/` or `cmd/hived/` changed over
`BASE...HEAD` while the `DaemonContract` value did not. Bypass label:
`daemon-contract-override`. The job in `.github/workflows/changesets.yml` is
modelled on `block-generated-edits`, and fixtures plus a self-test live under
`scripts/testdata/`, following the `ui-lint.sh` pattern already in CI.

### Files to change

- `internal/wire/control.go` — `Welcome.DaemonContract`; new `ClientCommand`
- `internal/wire/frame.go` — `FrameClientCommand` / `FrameClientBroadcast`
- `internal/daemon/daemon.go` — fill `DaemonContract` in both `Welcome`s;
  subscribe control conns to the command hub; handle `FrameClientCommand`
- `cmd/hived/main.go` — `--version --json`; best-effort `hivebar` spawn
- `cmd/hivegui/app_control.go` — severity split, `ReloadGUI`,
  `RequestReloadAllGUIs`, `relaunchSelf`, broadcast handling in
  `controlReadLoop`
- `cmd/hivegui/menu_darwin.go` — "Reload GUI"; relabel "Restart Hive…"
- `cmd/hivegui/locate.go` — `locateHivebar`
- `cmd/hivegui/main.go` — best-effort `hivebar` spawn on boot
- `cmd/hivegui/update_apply_darwin.go`, `update.go`, `update_action.go` —
  staged-bundle contract read, `restartKind`, apply routing
- `cmd/hivegui/frontend/src/lib/update-state.ts` — reload vs restart labels
- `cmd/hivegui/frontend/src/app/banners.ts`,
  `app/version-footer.ts` — new severities
- `cmd/hivegui/frontend/src/app/keyboard.ts`, `lib/keymap.ts`,
  `components/modals/Settings.tsx` — palette, help overlay, login-item toggle
- `build.sh` — build and bundle `hivebar`
- `.github/workflows/changesets.yml` — contract-guard job
- `DESIGN.md`, `AGENTS.md`, `README.md` — see Docs below

### New files

- `internal/buildinfo/contract.go` — `DaemonContract`, `Identity`
- `internal/daemon/commands.go` — client-command fan-out hub
- `cmd/hivebar/` — `main.go`, `client.go`, `model.go`, `menu.go`,
  `singleton_darwin.go`, `loginitem_darwin.go`, `loginitem_other.go`,
  `icon/`, `build/darwin/Info.plist`, `README.md`
- `scripts/check-daemon-contract.sh` plus `scripts/testdata/` fixtures
- `docs/design-docs/daemon-contract.md` — why an integer and not semver or a
  binary hash; why `PROTOCOL_VERSION` stays at 1

### Tests

Go:

- `internal/wire/wire_test.go` — `TestWelcomeRoundTripCarriesDaemonContract`,
  `TestWelcomeOmitsDaemonContractWhenZero`, `TestClientCommandRoundTrip`,
  `TestProtocolVersionUnchangedByClientCommand`
- `internal/daemon/commands_test.go` — `TestCommandHubFansOutToAllSubscribers`,
  `TestCommandHubDropsSlowSubscriber`, `TestCommandHubUnsubscribeStopsDelivery`
- `internal/daemon/control_frame_test.go` —
  `TestClientCommandBroadcastsToEveryControlConn`,
  `TestClientCommandRejectsUnknownCmd`,
  `TestUnknownFrameFromNewerClientKeepsConnAlive`
- `cmd/hived/main_test.go` — `TestVersionFlagPrintsIdentityJSON`
- `cmd/hivegui/reload_test.go` (new; seam `spawnNewGUIFn` / `quitFn` the way
  `restartDaemonFn` is seamed in `update_action.go` — an unseamed test re-execs
  the test binary) — `TestReloadGUISendsNoShutdownFrame`,
  `TestReloadGUINeverKillsHived`, `TestReloadGUIClosesAttachConns`,
  `TestControlReadLoopHandlesReloadCommand`,
  `TestControlReadLoopForwardsFocusSessionToFrontend`,
  `TestReloadIsIdempotentUnderBroadcastStorm`
- `cmd/hivegui/version_event_test.go` —
  `TestDaemonVersionEventReloadableWhenContractsMatch`,
  `TestDaemonVersionEventMismatchWhenContractsDiffer`,
  `TestDaemonVersionEventMismatchWhenContractAbsent`
- `cmd/hivegui/update_apply_darwin_test.go` — `TestStagedBundleContractRead`,
  `TestRestartKindGuiWhenContractsMatch`,
  `TestRestartKindFullWhenContractsDiffer`
- `cmd/hivebar/model_test.go` — `TestMenuModelGroupsSessionsByProject`,
  `TestMenuModelCountsAttention`, `TestMenuModelRendersContractMismatch`
- `cmd/hivebar/singleton_test.go` — `TestSingletonLockRefusesSecondInstance`
- `cmd/hivegui/locate_test.go` — `TestLocateHivebarPrefersLoginItemsBundle`

Frontend:

- `test/unit/update-state.test.ts` — reload-vs-restart label and status line per
  `restartKind`
- `test/dom/banners.test.ts` — `reloadable` offers Reload; `mismatch` offers
  Restart with the session warning
- `test/e2e` (mock Wails bridge) — `menu:reload-gui` calls `ReloadGUI`, not
  `RestartDaemon`

Shell: `scripts/testdata/` fixtures driving `check-daemon-contract.sh`.

### Docs

- `DESIGN.md` (structural, so required) — `cmd/hivebar/` package row and its
  layering rule; the new frame pair; the `DaemonContract` concept.
- `AGENTS.md` — the contract-bump rule in the change-patterns section; the
  package table row.
- `README.md` — menu-bar section; the File-menu changes.
- `.changesets/*.md` — `added` (menu bar, Reload GUI); `changed` (stale-daemon
  banner, update button wording).

## Decision log

- **2026-09-03** — Compatibility signal is a hand-bumped `DaemonContract`
  integer. Why: build IDs are git revisions and flag cosmetic changes as stale;
  per-binary semver does not answer "can this pair talk"; a binary hash forces
  a needless full restart on any unrelated recompile.
- **2026-09-03** — `wire.PROTOCOL_VERSION` stays at 1. Why:
  `internal/daemon/daemon.go:452` rejects a mismatched `hello.Version`
  outright, so a bumped GUI could not handshake with an old daemon and could
  never read the contract it needs; unknown frames are only logged, so adding
  frames is backward compatible.
- **2026-09-03** — Reload fans out to every window via the daemon rather than
  reloading only the window that asked. Why: each window is its own process, so
  a local reload leaves siblings running old code against a new daemon.
- **2026-09-03** — One generic command frame pair rather than a pair per
  action. Why: `reload_gui` and `focus_session` both need it now, and `hivebar`
  will want more.
- **2026-09-03** — The command hub lives in `internal/daemon`, not
  `internal/registry`. Why: commands are transient fan-out; the registry is the
  writer of persisted state only.
- **2026-09-03** — The menu bar ships as its own `hivebar.app` rather than
  living in `hived` or `hivegui`. Why: AppKit in a headless daemon breaks the
  Linux build; a GUI-hosted tray disappears exactly during a reload or after a
  quit, which are the moments it exists for.
- **2026-09-03** — `SMAppService` login-item registration ships as an opt-in
  Settings toggle with spawn-on-boot as the working path. Why: `build.sh` has
  no `codesign` step and registration is reported to fail for unsigned bundles
  (`Status Error 78`, `-67054`); the toggle is honest about the error and works
  the day Hive is signed.
- **2026-09-03** — "Check for Updates…" in the menu bar delegates to the GUI.
  Why: the GUI owns ~3300 lines of update logic and has to restart anyway;
  duplicating it in `hivebar` buys nothing.
- **2026-09-03** — The updater compares the staged daemon's contract against
  the RUNNING daemon's, not against this GUI's constant. Why: after the swap it
  is the staged GUI driving the running daemon, and the staged GUI carries the
  staged contract; using this GUI's would be correct only by coincidence.
- **2026-09-03** — Scope added: attention ("which sessions want you") moved from
  the GUI store into the daemon, with a stateful bell scanner in
  `internal/session`, `SessionInfo.needs_attention`, a new `attention` event
  kind, and client-driven clearing through `UPDATE_SESSION`. Why: the menu bar
  holds no attach connection, so the flag was unreachable — and it was already
  wrong, with each window keeping its own answer. Operator chose this over
  shipping the menu bar without the count. First `DaemonContract` bump (1 → 2),
  which exercises the new CI gate for real.
- **2026-09-03** — `PROTOCOL_VERSION` still not bumped despite four new frames
  and two new fields. Why: all additive, and the daemon's strict HELLO check
  makes a bump actively harmful (see Research).
- **2026-09-03** — The menu is a FIXED pool of items, created once and only
  ever retitled, shown or hidden. Why: `systray.AddMenuItem` appends, so
  rebuilding the dynamic half re-added it below the static footer and the
  menu visibly reordered itself — and the daemon emits a `title` event per
  shell prompt redraw, so that happened many times a second. The project
  name moved onto each row because a submenu tree cannot be a fixed pool
  without one pool per project.
- **2026-09-03** — hivebar coalesces publishes on a 400ms trailing window,
  copying `internal/session`'s title throttle. Why: same cause as above —
  nothing in a menu is worth showing at a child process's redraw rate — and
  a trailing window means the final state of a burst still lands.
- **2026-09-03** — The stale-daemon banner stays SILENT on `reloadable`,
  reversing an earlier choice. Why: equal contracts means compatible, so a
  differing daemon build is unactionable — reloading cannot change which
  build hived is, so the banner reappeared immediately after a successful
  reload and could never clear. The sidebar footer already renders both
  builds on two lines without the mismatch styling, which is the right
  surface for a fact the user cannot act on. The banner's Reload action was
  then dead and was removed; Reload GUI remains on the File menu, the
  palette, the menu bar and the update flow. Operator chose this over
  keeping the button.
- **2026-09-03** — `SMAppService` registration verified working on an
  ad-hoc-signed bundle, contradicting the pre-implementation research. Why it
  matters: the toggle was designed around an expected failure, and the code
  comments and Settings copy asserted a signing requirement that does not
  exist. What actually governs it is the CALLING bundle — registration fails
  with "Invalid argument" from a bare binary and succeeds from inside
  `Hive.app`.

## Progress

- **2026-09-03** — Plan-first scaffold; stage = IMPLEMENT (set in spec
  frontmatter). Spec has no GitHub issue (operator chose local-only).
- **2026-09-03** — Phase 1 landed: `DaemonContract`, `Welcome.daemon_contract`,
  `hived --version [--json]`, the four-way severity split, and
  `wire.ErrProtocolMismatch` routed to the banner.
- **2026-09-03** — Phase 2 landed: `CLIENT_COMMAND`/`CLIENT_BROADCAST`, the
  daemon command hub, `App.ReloadGUI` / `RequestReloadAllGUIs`, and the two
  File-menu items.
- **2026-09-03** — Phase 3 landed: staged-bundle contract probe,
  `UpdateInfo.RestartKind`, and the Reload/Restart split in the update button
  and the stale-daemon banner.
- **2026-09-03** — Phase 4a landed (scope added mid-implementation, see the
  decision log): attention moved into the daemon; `DaemonContract` 1 → 2.
- **2026-09-03** — Phase 4b landed: `cmd/hivebar`, `internal/menubar`,
  `build.sh` bundling, and the Settings login-item toggle.
- **2026-09-03** — Verified on a real `./build.sh` bundle: hivebar launches,
  takes the singleton lock, connects to the running daemon, and the operator
  confirmed the icon and menu render correctly.
- **2026-09-03** — Phase 5 landed: `scripts/check-daemon-contract.sh`, its
  six-case self-test, and the `daemon-contract` CI job.
- **2026-09-03** — Rebased onto `origin/main` after v2.5.0 shipped; one
  conflict in `events.ts` where main had narrowed the closing-phase check,
  resolved in main's favour.
- **2026-09-03** — Operator reported the menu bar reordering itself on every
  daemon event. Root-caused and fixed (see the decision log); operator
  confirmed it holds still.

## Open questions

- Does `cmd/hivegui/window_state.go` restore behave when several windows
  relaunch at once via `open -n`? Each reloaded window gets a new PID.
  Not reachable by an automated test — needs the manual multi-window pass in
  the spec's verification section.

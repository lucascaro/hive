---
issue: null
title: "Add GUI-only reload and a daemon menu-bar agent"
type: enhancement
complexity: L
priority: P2
stage: IMPLEMENT
---

# Add GUI-only reload and a daemon menu-bar agent

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/330-add-gui-only-reload-and-a-daemon-menu-bar-agent.md](../exec-plans/active/330-add-gui-only-reload-and-a-daemon-menu-bar-agent.md)

## Problem

Every Hive change costs a full restart. `App.RestartDaemon`
(`cmd/hivegui/app_control.go`) stops `hived`, waits for its socket to go quiet,
relaunches the GUI and quits — which terminates every PTY and every running
agent. Most changes are frontend-only, so that price is almost always paid for
nothing.

The machinery for a cheaper path already exists and is unused: `hived` is
spawned detached (`cmd/hivegui/spawn_unix.go`) and has no idle exit, so it
already survives a GUI quit. What is missing is a trustworthy answer to "is
this daemon compatible with this GUI?". The GUI compares build IDs — git
revisions — so a CSS tweak reads as a stale daemon and demands a full restart.

Separately, the daemon has no surface of its own. When the GUI is closed there
is no way to see whether `hived` is running, what version it is, what sessions
it is holding, or to restart it.

## Desired behavior

A GUI-only change relaunches the GUI and leaves every session running, with
scrollback intact. A change that genuinely alters daemon behavior still asks
for a full restart, and says plainly that sessions will end. The self-update
flow makes the same distinction before the user commits to it.

A menu-bar icon is present whenever the daemon is, independent of the GUI. It
shows the daemon's version and compatibility state, a summary of open sessions
and which need attention, a clickable list of those sessions, and actions to
reload the GUI, restart the daemon, check for updates, open Hive, and quit.

## Success criteria

- A frontend-only rebuild offers "Reload GUI"; taking it relaunches every open
  window and leaves all sessions running with scrollback intact.
- A rebuild that bumps the daemon contract offers "Restart Daemon" instead, and
  names the consequence (all sessions end) before the user commits.
- The self-update flow reports which of the two an available update will
  require, before it is applied, by reading the staged bundle's contract.
- With multiple windows open, a reload relaunches all of them — no window is
  left running the old binary against the new daemon.
- A menu-bar icon appears on macOS whenever `hived` is running, including when
  the GUI has been quit, and shows daemon version, contract state, and a
  session summary.
- Clicking a session in the menu bar focuses it in the GUI, launching the GUI
  first if it is not running.
- Restarting the daemon and reloading the GUI both work from the menu bar.
- CI fails a PR that changes daemon-side behavior without bumping the contract,
  unless the bypass label is applied.

## Non-goals

- Code signing and notarization of the app bundle. `SMAppService`
  login-item registration turns out to work on an ad-hoc-signed bundle
  (verified against a real `build.sh` build), so signing is not a
  prerequisite for this feature — it remains its own separate work.
- A menu bar on Windows or Linux. The reload path itself is cross-platform.
- Hot-reloading frontend assets without relaunching the process. The frontend
  is embedded in the `hivegui` binary, so new frontend code means a new
  process.
- Independent semantic versions or release tags per binary. One release version
  still covers the whole app.
- Running the self-update entirely from the menu bar without the GUI. The menu
  bar hands off to the GUI, which owns the update flow.

## Notes

- The compatibility signal is a hand-maintained `buildinfo.DaemonContract`
  integer rather than per-binary semver or a hash of the daemon binary: semver
  strings do not answer "can this pair talk", and a hash forces a needless full
  restart on any unrelated recompile.
- `wire.PROTOCOL_VERSION` deliberately stays at 1. The daemon rejects any
  mismatched `hello.Version` outright (`internal/daemon/daemon.go:452`), so
  bumping it would stop a new GUI from handshaking with an old daemon at all —
  and it could then never read the contract it needs to decide anything.
  Unknown control frames are only logged, so adding frames is backward
  compatible.
- `fyne.io/systray` v1.12.2 is the menu-bar library. It requires an `.app`
  bundle on macOS, which is why `hivebar` ships as its own bundle inside
  `hivegui.app/Contents/Library/LoginItems/`.

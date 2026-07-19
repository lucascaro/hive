# Restart Hive doesn't reliably restart the daemon

- **Issue:** —
- **PR:** #244
- **Shipped:** 2026-07-19 (c220c67)
- **Type:** bug
- **Complexity:** M
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/completed/243-restart-hive-doesnt-reliably-restart-daemon.md](../exec-plans/completed/243-restart-hive-doesnt-reliably-restart-daemon.md)

## Problem

Clicking **Restart Hive** in the daemon-stale banner relaunches the GUI but leaves the original `hived` running: the stale-build banner reappears immediately, the sidebar version footer is unchanged, and the old daemon PID is still alive. The restart path (`RestartDaemon`, `cmd/hivegui/app.go:319`) treats a pidfile at `<sock>.pid` as its only handle on the daemon and treats `killRunningHived` returning `nil` as proof the socket is free — but three of that function's `nil` returns (missing pidfile, process-name mismatch, discarded post-SIGKILL wait) are compatible with a daemon that is still listening. Nothing probes the socket afterward, so the relaunched GUI's `dialOrSpawn` reconnects to the very daemon the user asked to replace, and the failure is completely silent: the GUI writes no log file at all under LaunchServices.

Two further defects compound it. `hived` runs as a direct child of `hivegui` and is never `Wait()`ed on, so after SIGTERM it becomes a zombie — `proc.Signal(0)` still succeeds, `waitForExit` can never observe the exit, and every restart burns its full 5s escalation budget for nothing. And the daemon-stale banner is the *only* trigger for `RestartDaemon` anywhere in the app, so when the GUI and daemon builds match there is no way to restart Hive at all — no menu item, no palette entry, no shortcut.

## Desired behavior

Restarting Hive actually restarts the daemon, or says so plainly when it cannot. The GUI asks the running daemon to shut down over the control connection it already holds, confirms the socket has gone quiet, and only then relaunches itself — so the new window can never come up attached to the old daemon. When shutdown over the wire is not possible the existing SIGTERM-by-pid path remains as a fallback, and if the daemon is *still* alive after both attempts the restart fails loudly in the banner and leaves the current window working rather than quitting into a broken state. Restart is reachable at any time from **File ▸ Restart Hive…** and the command palette, not only when a build mismatch happens to raise the banner. Every restart step is recorded in a `hivegui.log` next to the existing `hived.log`, so a recurrence can be diagnosed instead of re-guessed.

## Success criteria

- Invoking Restart Hive replaces the running daemon: `<sock>.pid` holds a new PID afterward, and the sidebar version footer reflects the newly spawned `hived`.
- Restart still works with the pidfile deleted (`rm /tmp/hive-$UID/hived.sock.pid`) — the in-band shutdown path does not depend on it.
- When the daemon survives both the in-band and signal paths, `RestartDaemon` returns an error, the banner shows it, and the GUI does **not** relaunch or quit.
- **Restart Hive…** appears in the File menu and the command palette regardless of whether the GUI and daemon builds match.
- A restart completes promptly rather than stalling ~5s on the zombie-liveness timeout.
- `~/Library/Application Support/Hive/hivegui.log` contains one readable line per restart step, including which kill channel succeeded and the socket-probe result.

## Non-goals

- Having `RestartDaemon` spawn the replacement `hived` itself; respawn stays a side effect of the relaunched GUI's `dialOrSpawn`.
- Hardening the macOS `open -n` relaunch (`cmd/hivegui/window_unix.go:33-45`), whose failure is currently unobservable. Real, but a separate bug.
- Reaping the daemon child properly (`cmd/hivegui` never `Wait()`s); the socket probe makes the zombie irrelevant to this fix.

## Notes

Root cause was **not** reproduced — the live state during triage was healthy, so the bad state is state-dependent. Two hypotheses were tested and ruled out: `ps -o comm=` truncating the long bundle path (single-column output is not truncated), and `hived` ignoring SIGTERM (`hived.log` shows clean `shutting down` handling). The fix is therefore designed to hold regardless of which `nil` path fires, and the new GUI log exists so the next occurrence is explainable.

Prior art: [177-windows-restart-button-and-grid-mode-bugs](177-windows-restart-button-and-grid-mode-bugs.md) fixed the Windows stub, and `CHANGELOG.md:250` fixed the macOS `open -n` relaunch. Both patched the same function without making its "returned nil ⇒ socket is free" contract verifiable.

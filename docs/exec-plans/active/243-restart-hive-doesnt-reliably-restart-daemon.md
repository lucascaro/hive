# Restart Hive doesn't reliably restart the daemon

- **Spec:** [docs/product-specs/243-restart-hive-doesnt-reliably-restart-daemon.md](../../product-specs/243-restart-hive-doesnt-reliably-restart-daemon.md)
- **Issue:** —
- **PR:** #244
- **Branch:** feature/243-restart-hive-doesnt-reliably-restart-daemon
- **Status:** active

## Summary

Make the Restart Hive action verifiable, recoverable, and reachable. The GUI asks the daemon to exit over the control connection it already holds, confirms the socket is dead before relaunching, falls back to the existing SIGTERM-by-pid path, and fails loudly instead of quitting into a state where the new GUI reattaches to the old daemon. Adds a File-menu and palette entry so restart is invokable without a build mismatch, and a `hivegui.log` so the next failure is diagnosable.

## Research

Authored via plan-first mode; findings from tracing the full path.

- `cmd/hivegui/frontend/src/app/banners.js:41-72` — `wireDaemonBanner`; the click handler (`Confirm` → `RestartDaemon` → catch/`showDaemonBanner`) is the **only** trigger for `RestartDaemon` in the whole frontend. `manualUpdateCheck` (`:202`) is the precedent for exporting a banner action and wiring it to a menu + palette entry.
- `cmd/hivegui/app.go:319-341` — `RestartDaemon`: close conns → `killRunningHived` → `spawnNewGUI` → `wruntime.Quit`. No verification between kill and relaunch.
- `cmd/hivegui/restart_unix.go:31-105` — pidfile-only kill. Returns `nil` on missing pidfile (`:36`), name mismatch after deleting the pidfile (`:55-60`), and a discarded post-SIGKILL wait (`:73-75`). Windows returns an error in the equivalent spot (`restart_windows.go:83-86`).
- `cmd/hivegui/app.go:836-851` — `dialOrSpawn`: dials first, only spawns `hived` on dial failure. A surviving daemon is silently reused.
- `cmd/hived/main.go:84-88,101-105` — pidfile written after `daemon.New` wins the bind race; `removePidfile` deletes unconditionally, so a late-exiting daemon can delete a live one's pidfile.
- `internal/daemon/daemon.go:37-45,129-146,298-370` — `Daemon` struct, `Run`'s ctx-cancel → `ln.Close()` goroutine, and the existing inbound control-frame dispatch. Adding a shutdown frame is a small extension of machinery that already exists.
- `internal/wire/frame.go:40-71` — frame type table; next free discriminator is `0x15`.
- Observed via `ps -Ao pid,ppid,command`: `hived` (63467) is a **direct child** of `hivegui` (63452), and `startDetached` (`window_unix.go:56-64`) never `Wait()`s. After SIGTERM the daemon is a zombie, `proc.Signal(0)` keeps succeeding, and `waitForExit` (`restart_unix.go:96`) always burns its full budget. A zombie holds no socket, so a socket probe is the correct liveness test.
- Test precedent: `internal/daemon/daemon_test.go:454` drives control frames over a real socket; `cmd/hivegui/restart_windows_test.go:69-140` shows the `probeFn` seam pattern for testing `killRunningHived`; `cmd/hivegui/frontend/test/dom/attention-jump.test.js:22` shows the DOM-test bridge stub.
- Ruled out during triage: `ps -o comm=` path truncation (single-column output is untruncated at 85 chars) and `hived` ignoring SIGTERM (`hived.log` shows clean `shutting down`).

## Approach

Stop treating the pidfile as authoritative. The **socket** is the liveness authority; the existing control connection is the primary kill channel; the pidfile/signal path is demoted to a fallback.

`RestartDaemon` becomes: send `FrameShutdown` → `socketDead`? → else `killRunningHived` → `socketDead`? → else return an error **without** relaunching or quitting. `spawnNewGUI` + `Quit` are reached only once the socket is confirmed dead, which makes it structurally impossible for the new GUI to reattach to the old daemon.

Chosen over the obvious alternative (keep signalling by pid, add an `lsof -t <sock>` fallback to find the listener when the pidfile is gone) because in-band shutdown removes the pid/name/pidfile guesswork from the happy path entirely, reuses control-frame machinery that already exists, and does not shell out. Verify-only — probing the socket and reporting failure without a recovery channel — was rejected: in exactly the broken state it turns the button into a guaranteed dead-end and relabels the bug instead of fixing it.

### Files to change

- `internal/wire/frame.go` — add `FrameShutdown FrameType = 0x15` (C → S, control, empty payload) and its `String()` case.
- `internal/daemon/daemon.go` — add `shutdown chan struct{}` + `sync.Once` to `Daemon`; `Shutdown()` closes it; `Run`'s goroutine waits on `ctx.Done()` **or** `shutdown` before `ln.Close()` (`Accept` then returns `net.ErrClosed` and `Run` returns nil, so `d.Close()` and `removePidfile` still run); dispatch `wire.FrameShutdown` → `d.Shutdown()`.
- `cmd/hivegui/app.go` — rewrite `RestartDaemon` per the sequence above; send the frame on the live control conn before closing it; `log.Printf` each step.
- `cmd/hivegui/restart_unix.go` — per-branch `log.Printf`; still-alive after the SIGKILL wait returns an error instead of `nil`.
- `cmd/hivegui/restart_windows.go` — per-branch `log.Printf` only.
- `cmd/hived/main.go` — `removePidfile` reads the file and deletes only when the contents match `os.Getpid()`.
- `cmd/hivegui/main.go` — tee `log` output to `filepath.Join(registry.StateDir(), "hivegui.log")`, mirroring `cmd/hived/main.go:54-59`.
- `cmd/hivegui/frontend/src/app/banners.js` — extract the click-handler body into an exported `restartHive()`; the button calls it.
- `cmd/hivegui/menu_darwin.go` — `file.AddText("Restart Hive…", nil, emit("menu:restart-hive"))` next to "Check for Updates…" (`:61`). No accelerator — too destructive for a hotkey.
- `cmd/hivegui/frontend/src/main.js` — palette command `{ id: 'restart-hive', name: 'Restart Hive…', run: restartHive }` + the `menu:restart-hive` binding.
- `CHANGELOG.md` — via `/hs-changelog-update`.

`banners.js:62-67` already renders a `RestartDaemon` rejection in the banner, which is exactly what the new failure path produces — no change needed there.

### New files

- `cmd/hivegui/restart.go` — `socketDead(sock string, budget time.Duration) bool`, polling `net.Dial("unix", sock)`. Platform-neutral (Go supports unix sockets on Win10+); if it proves unreliable on Windows, gate the probe to unix and keep the existing Windows error path.

### Tests

- `internal/daemon/daemon_test.go` — `TestShutdownFrameStopsDaemon`: send `FrameShutdown` over a real socket, assert `Run` returns nil and the socket stops accepting.
- `cmd/hivegui/restart_test.go` (new) — `TestSocketDead_LiveListener`, `TestSocketDead_AfterClose`.
- `cmd/hivegui/restart_unix_test.go` (new) — mirror `restart_windows_test.go` via a name/probe seam: no pidfile, stale pidfile removed, dead pid, invalid pid, plus `TestKillRunningHived_StillAliveAfterKillErrors`.
- `cmd/hived/main_test.go` — `TestRemovePidfileKeepsForeignPid`.
- `cmd/hivegui/frontend/test/dom/restart-hive-palette.test.js` (new) — palette entry invokes the mocked `RestartDaemon`.

## Decision log

- **2026-07-19** — Applied both IMPORTANT review findings rather than stopping on the COMMENT verdict. Why: AGENTS.md requires auto-fixing high-confidence low-risk findings in the same PR, and finding #1 falsified a claim in the code's own doc comment.

- **2026-07-18** — In-band `FrameShutdown` over the control conn as the primary kill channel, pidfile/SIGTERM demoted to fallback. Why: the pidfile is the thing that fails, and the daemon already dispatches inbound control frames.
- **2026-07-18** — Numbered 243 rather than max-prefix+1 (241). Why: this repo's spec numbers mirror GitHub PR numbers, and 241/242 are shipped PRs.
- **2026-07-18** — `RestartDaemon` logs a `killRunningHived` error instead of returning it. Why: with hived as an unreaped child, the signal-based wait reports "still alive" for a zombie that has already released the socket, so returning that error would abort every restart. The socket probe is the arbiter; the kill error is diagnostic only.
- **2026-07-18** — `removePidfile`'s ownership check changed existing test expectations (`main_test.go` wrote a hardcoded `"123"`). Updated them to write `os.Getpid()` and added the foreign-pid case, rather than weakening the check.

## Progress

- **2026-07-18** — Plan-first scaffold; stage = IMPLEMENT.
- **2026-07-19** — Review iter 2 cleared four IMPORTANT items (writeMu bypass on the shutdown frame, a log line claiming an in-band attempt that never happened, `println` bypassing the new logfile, and a test reading the real state dir) plus the two Greptile MINORs (socketDead budget overrun, probe-dial EOF spam in hived.log). All three bot review threads replied to and resolved.
- **2026-07-19** — Review iter 1 cleared both IMPORTANT findings: RestartDaemon no longer tears down conns before confirming the daemon died (the error path has to leave a working window, and there is no reconnect route), and restartHive is re-entrancy-guarded now that the menu and palette reach it. Both have regression tests; the Go one was mutation-checked against the pre-fix ordering.
- **2026-07-19** — PR #244 opened; stage = REVIEW.
- **2026-07-18** — Implemented on `feature/243-restart-hive-doesnt-reliably-restart-daemon`. `./build.sh` green; `internal/daemon` shutdown tests, `cmd/hivegui` restart tests, `cmd/hived` pidfile tests, and 253 frontend vitest tests all pass.

## PR convergence ledger

<!-- Append-only. One line per /hs-review-loop iteration. -->

- **2026-07-19 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: (4 IMPORTANT, distinct from iter 1); threads_open: 0 (3 bot threads replied + resolved); action: autofix+push; head_sha: 69ef50a.
- **2026-07-19 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 495bdfec73271419d2f24d2560fdb9f0290d885f1efa99fb4bad82abac6f883c; threads_open: 0; action: autofix+push (2 IMPORTANT applied despite COMMENT-stop, per AGENTS.md boil-the-lake); head_sha: 8a0e349.

## Open questions

- None blocking.
- Windows `net.Dial("unix", …)` reliability is the one unknown; the fallback (gate the probe to unix) is identified and cheap. `GOOS=windows go vet ./cmd/hivegui/` is clean.
- Theoretical restart-path race, judged unreachable and deliberately not guarded: `socketDead` returns true when the listener closes, but the old daemon's `os.Remove(d.sock)` runs slightly later in `Daemon.Close()`'s deferred teardown. If a *new* hived ever bound before those defers finished, the old daemon's `os.Remove` would delete the new socket file. New-GUI startup (Wails + WebView + JS, seconds) dwarfs the old daemon's teardown (~100ms), and `daemon.New`'s stale-socket handling compensates. Revisit only if a restart ever comes up with no socket.

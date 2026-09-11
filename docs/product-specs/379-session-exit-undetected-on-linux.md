---
issue: 379
pr: null
title: "Session exit is never detected on Linux"
type: bug
complexity: S
priority: P1
stage: TRIAGE
---

# Session exit is never detected on Linux

- **Issue:** [#379](https://github.com/lucascaro/hive/issues/379)
- **Type:** bug
- **Complexity:** S (the fix is small; the risk is in who owns `done`)
- **Priority:** P1

## Problem

On Linux, a session whose child process exits **on its own** is never
noticed by the daemon. The entry stays `alive`, keeps its running
glyph, and `watchSessionExit` never runs — so the exit is never
recorded, `Entry.sess` is never nil'd, and the post-spawn
agent-session-id capture is never cancelled.

Quit Claude from inside its own TUI and Hive still shows the session as
live. Only an explicit Kill from the GUI reflects reality. macOS is
unaffected; Windows was not measured.

## Cause

`internal/session/session.go` — `readLoop` is the only thing that
closes `Session.done`, and it closes it on a **failed PTY master
read**:

```go
func (s *Session) readLoop() {
	defer close(s.done)
	for {
		n, err := s.ptmx.Read(buf)
		...
		if err != nil { s.fanoutClose(); return }
	}
}
```

Exit is therefore inferred entirely from the read failing. On Linux
that read does not fail after the child exits, so the loop blocks
forever and `done` is never closed. Nothing observes the process
itself — there is no `cmd.Wait()` anywhere on this path.

`internal/registry`'s `watchSessionExit` blocks on `<-sess.Done()`, so
every consequence of an exit is unreachable on Linux.

## Evidence

Measured three ways, all agreeing:

1. The GitHub Actions Linux runner — a registry test waiting for a
   `/usr/bin/true` session to be seen as exited timed out at 10s, while
   macOS and Windows passed.
2. `docker run golang:1.27.1` over this repo — same timeout.
3. `docker run golang:1.27.1` over a clean `origin/main` checkout, with
   no feature branch in the tree, using only pre-existing code: `select`
   on `sess.Done()` with a 5s timeout never fires.

Both an instant `/usr/bin/true` and a 0.3s sleeper reproduce it, so it
is not a race with a too-fast child.

## Why it was not caught

Every existing test that ends a session calls `Kill`, which closes the
PTY from our side — that *does* make the read fail, so the exit path
runs and the tests pass. Nothing covers a child exiting by itself,
which is the ordinary way a user ends an agent session.

## Desired behavior

A session whose child exits by any means — killed by us, quit from
inside the TUI, or crashed — reaches the same observable state on every
platform: `alive: false`, the exited glyph, the tombstone written, the
capture goroutine cancelled.

## Success criteria

- A registry test spawns a session that exits on its own (not via
  `Kill`) and observes the entry become not-alive, and it runs on
  Linux — not skipped there.
- `Session.done` is closed exactly once whichever signal arrives first.
- Windows is measured and either passes or gets its own criterion.

## Non-goals

- Reworking the PTY read loop or the VT mirror.
- Changing what a client sees on exit; only when it sees it.

## Notes

- Suggested direction, not a decision: wait on the process as well as
  the PTY — a goroutine on `cmd.Wait()` closing `done` on whichever
  event lands first. The two paths must agree on who closes it;
  `readLoop` closes unconditionally today, so a second closer panics
  without a `sync.Once` or a dedicated exit goroutine owning the close.
- Found while implementing 337 phase 3 (PR #377). That feature's
  "session died before the opening prompt could be delivered" branch
  hangs off `watchSessionExit` and is therefore inert on Linux until
  this is fixed; its test was rewritten to drive the same state through
  `Kill` so the PR does not depend on this work.

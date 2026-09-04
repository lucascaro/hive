---
issue: null
title: "Move SocketPath out of internal/daemon so clients stop linking the daemon"
type: enhancement
complexity: S
priority: P3
stage: TRIAGE
---

# Move SocketPath out of internal/daemon so clients stop linking the daemon

- **Issue:** —
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3

## Problem

Every client that wants to talk to the daemon needs one string: the path of
the Unix socket. That string lives in `internal/daemon` (`daemon.SocketPath()`,
`internal/daemon/socket.go`), so reaching it means importing the package that
owns the connection state machine — which in turn pulls in `internal/session`
and `creack/pty`.

`cmd/hivebar` uses it in exactly two places (`client.go`, `actions.go`) and
does nothing else with the daemon package. It is a menu-bar agent: it never
opens a PTY, and `DESIGN.md` has a hard rule saying so. The rule is written as
a grep guard on *direct* imports, so this passes — but a status-bar app
statically linking the PTY host is not what the rule is trying to describe.

`cmd/hivegui` has the same shape and has had it far longer, so this is not new
with the menu bar; the menu bar just made it a second offender and easier to
see.

Surfaced by `/hs-merge-gate` on spec 330 — recorded there under
[Open questions](../exec-plans/completed/330-add-gui-only-reload-and-a-daemon-menu-bar-agent.md).

## Desired behavior

A client can resolve the daemon's socket path without importing
`internal/daemon`. `cmd/hivebar` and `cmd/hivegui` link the wire protocol and
whatever they genuinely need — not the session host.

## Success criteria

- `SocketPath()` (and `EnsureSocketDir`, if it travels with it) live in a leaf
  package that imports nothing from Hive beyond the standard library.
- `go list -deps ./cmd/hivebar` no longer contains `internal/session` or
  `github.com/creack/pty`.
- `internal/daemon` re-exports or calls the new location, so no caller has to
  change how it spells the path and no behavior moves.
- The platform-specific path rules (`$HIVE_SOCKET`, `XDG_RUNTIME_DIR`, the
  `/tmp/hive-<uid>/` fallback, the Windows branch) are preserved exactly, with
  their tests moving alongside them.
- `DESIGN.md`'s hard rule for `cmd/hivebar` is restated in terms of what the
  binary links, now that it can be.

## Non-goals

- Changing the socket path itself, or any of its platform rules. This is a
  move, not a redesign — a changed path would strand running daemons.
- Reworking `cmd/hivegui`'s other uses of `internal/daemon`. It legitimately
  uses more of that package than hivebar does; only the socket-path import is
  in scope.
- Turning the grep-guard rules in `DESIGN.md` into a real build-time check.
  Worth doing, but it is its own piece of work.

## Notes

- Candidate homes: `internal/wire` (already the shared client-facing package,
  and already imported by everything that speaks the protocol) or a new
  `internal/sockpath`. `internal/wire` avoids adding a package for one
  function; a separate package keeps `wire` free of filesystem and
  platform-path concerns. Decide at triage.
- Call sites, from `grep -rln 'SocketPath()' cmd/ internal/`:
  `cmd/hivebar/client.go`, `cmd/hivebar/actions.go`,
  `cmd/hivegui/app_control.go`, `cmd/hived/main.go`, and
  `internal/daemon/{daemon,socket}.go`. Only the first three are clients that
  would stop linking the daemon; `cmd/hived` and `internal/daemon` are the
  daemon and can keep importing it either way.
- Confirmed with `go list -deps ./cmd/hivebar`, which today lists both
  `internal/session` and `github.com/creack/pty`.

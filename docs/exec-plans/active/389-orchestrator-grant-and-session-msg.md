# Orchestrator grant: a session you name can message its siblings

- **Spec:** [docs/product-specs/389-orchestrator-grant-and-session-msg.md](../../product-specs/389-orchestrator-grant-and-session-msg.md)
- **Design:** [docs/design-docs/agent-orchestration.md](../../design-docs/agent-orchestration.md)
- **Issue:** —
- **Branch:** `feature/389-orchestrator-grant`
- **PR:** —
- **Status:** active

## Summary

Add a persisted per-session `Orchestrator` flag, let the GUI set it,
and let a `ModeSession` connection from a granted session send
`SEND_TO_SESSION` to a sibling in its own project, with the daemon
stamping provenance. `hived msg` learns to work from inside a session.
Written for a fresh session to pick up: research cites `main` as of
2026-09-10; re-verify names against `main` before writing code, and
**start by reading how 338 actually shipped** — this plan assumes the
frames, error codes and `hived msg` it describes.

## Research

- `internal/daemon/daemon.go` — `serveControl` resolves
  `restricted`, `ownSessionID`, `ownProjectID` once at handshake;
  `sessionModeFrames` is the allowlist a `ModeSession` connection may
  use; `controlOps` carries the restriction into `handleControlFrame`.
  This is where the grant check lives.
- `internal/daemon/socket.go:~164` — the events socket that
  `HIVE_SOCKET` names; serves `ModeEvent` and `ModeSession` only.
- `internal/registry/registry.go:83` `Entry` — add the flag next to
  `Agent`; `internal/registry/persist.go` `MetaFile` — persist it.
- `internal/wire/control.go` — `CreateSpec` (:40), `SessionInfo`
  (:152), `UpdateSessionReq` (:417), `Error`/`ErrCode*` block. 338
  adds `SendToSessionReq`, `SentResp`, `FrameSendToSession`.
- `internal/registry/deliver.go` (from 338) — the three delivery
  strategies; provenance prefix is applied before strategy selection.
- `cmd/hived/idea.go` — `ideaEnv`, `ideaDial`, the `ModeSession`
  handshake and `CheckSocketDir` guard; `cmd/hived/msg.go` (from 338)
  — the control-socket client to extend.
- `cmd/hivegui/frontend/src/components/modals/Launcher.tsx`,
  `components/SessionRow.tsx` (context menu), `store/store.ts`,
  `bridge.ts`, `cmd/hivegui/app_calls.go` — the create/update path
  the checkbox and toggle ride.
- `internal/buildinfo` — `DaemonContract` bump (wire changes).

## Approach

### Wire and registry

- `CreateSpec.Orchestrator bool json:"orchestrator,omitempty"`,
  `UpdateSessionReq.Orchestrator *bool`, `SessionInfo.Orchestrator
  bool`. `Entry.Orchestrator`, `MetaFile.Orchestrator` — persisted
  because it is user intent, not derived state.
- `ErrCodeNotOrchestrator = "not_orchestrator"`.
- `DaemonContract++`.

### Daemon

- `sessionModeFrames[FrameSendToSession] = true`.
- In the `FrameSendToSession` arm, when `ops.restricted`:
  1. look up the caller entry by `ownSessionID`; if missing or
     `!Orchestrator` ⇒ `not_orchestrator`. Read the flag from the
     registry on every frame, not from a value cached at handshake —
     that is what makes revocation immediate.
  2. resolve `req.SessionID` as an id, else as an exact `Name`,
     among sessions whose `ProjectID == ownProjectID`; none ⇒
     `not_found`; equals caller ⇒ the nearest existing invalid-arg
     code.
  3. call the registry with `from = ownSessionID`. On a control
     connection `from = ""`.
- One log line per send, both caller kinds (spec wording).

### Provenance

`registry.SendToSession(id, text, queue, fromID)`: when `fromID != ""`
prefix `Message from Hive session "<from.Name>": ` before choosing a
strategy, so all three paths carry it. If the Claude inbox message
shape captured by 338 has a sender field, fill it with the same string
instead of prefixing — decide when 338's fixture exists.

### `hived msg`

`msg.go` (from 338) chooses its socket the way `idea.go` does: if
`HIVE_SESSION_ID`/`HIVE_SOCKET` are set, dial `HIVE_SOCKET` in
`ModeSession` and send the target as given (the daemon resolves
names); otherwise the control socket as 338 wrote it. On
`not_orchestrator` print: `this session may not direct other sessions
— enable "May direct other sessions" in the launcher or the session's
menu`.

### GUI

- Launcher: checkbox *May direct other sessions*, default off, not
  remembered; sets `orchestrator` on the create.
- `SessionRow` context menu: same label as a checked item; sends
  `UpdateSession{orchestrator}`.
- Row glyph when `orchestrator` is true. Pick a glyph consistent with
  the idea glyph; no new keybinding.

### Files to change

- `internal/wire/control.go` — fields, error code.
- `internal/buildinfo/contract.go` — bump.
- `internal/registry/registry.go`, `persist.go`, `deliver.go` — flag,
  persistence, provenance.
- `internal/daemon/daemon.go` (or wherever 338 put the send arm) —
  allowlist, grant check, name resolution, log line.
- `cmd/hived/msg.go` — session-socket path.
- `cmd/hivegui/app_calls.go`, frontend `bridge.ts`, `store/store.ts`,
  `components/modals/Launcher.tsx`, `components/SessionRow.tsx`.
- `docs/design-docs/control-plane.md` — one sentence under the
  `event` hello mode section pointing at agent-orchestration.md for
  what `ModeSession` may now do.

### New files

None expected. If the grant check does not fit next to the existing
`restricted` branch cleanly, a small `internal/daemon/orchestrate.go`
is fine.

### Tests

- `internal/daemon`: `TestSessionModeSendRequiresGrant`,
  `TestSessionModeSendRevokedMidConnection`,
  `TestSessionModeSendOtherProjectNotFound`,
  `TestSessionModeSendSelfRefused`, `TestSessionModeSendResolvesName`.
- `internal/registry`: provenance prefix on each strategy;
  `TestMetaFileRoundTripsOrchestrator`.
- `cmd/hived`: `msg` picks the session socket when in a session;
  the `not_orchestrator` message.
- Frontend: launcher checkbox and row toggle round-trip (existing
  DOM-test pattern); glyph render.

## Decision log

- **2026-09-10** — Name resolution for `hived msg` happens in the
  daemon, project-scoped, rather than giving granted sessions a
  session list. Why: keeps phase 2's read surface a separate decision
  with its own gate.
- **2026-09-10** — Flag persisted in `MetaFile`. Why: a grant is user
  intent, like `Agent`; losing it on daemon restart would silently
  break an orchestration the user set up.

## Progress

- **2026-09-10** — Plan written. Not started.

## Open questions

- Whether 338 shipped `hived msg` at all, and with which socket. If
  not, this plan owns `msg.go` entirely.
- Provenance for the Claude inbox path: prefix vs sender field —
  depends on the message shape 338 captures.
- Exact glyph and context-menu placement — implementor's call, match
  `SessionRow`'s existing idea glyph.

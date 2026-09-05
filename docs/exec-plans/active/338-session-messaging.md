# Session messaging: hand a session a message, get told when it idles

- **Spec:** [docs/product-specs/338-session-messaging.md](../../product-specs/338-session-messaging.md)
- **Design:** [docs/design-docs/control-plane.md](../../design-docs/control-plane.md)
- **Issue:** —
- **Branch:** `feature/338-session-messaging`
- **PR:** —
- **Status:** active

## Summary

One control frame, `SEND_TO_SESSION`, with three delivery strategies
chosen by the target's agent (Claude inbox socket, Pi extension inbox,
typed-on-idle), plus a one-shot "notify me when idle" flag on the
registry entry. Depends on spec 336 for `idle`, the `event` mode and
the embedded Pi extension. Written so each phase is one agent run.

## Research

- Spec 336 exec plan — `agentstate.Machine`, `Entry.state`, ticker,
  `internal/agent/pi/hive.ts`, `Def.SpawnArgs`. This plan extends all
  four.
- `internal/session/session.go` — the PTY input path the attach mode
  uses for `FrameData` C→S (find `Write`/`Input` on `Session`); the
  typed strategy calls it directly from the registry.
- `internal/registry/create.go` (after 337) — `pendingPrompt` typed on
  first idle; the "queue until idle" strategy generalises it to a
  small FIFO `pendingInputs []string` drained one per idle transition
  with a 300 ms grace.
- `internal/wire/control.go:415-450` — `Error` + `ErrCode*` constants;
  add `session_busy`, `session_dead`, `delivery_failed`.
- `internal/notify/` — desktop notification entry point; the daemon
  already calls it for attention. Ping-when-idle uses the same call.
- Claude registry file: `~/.claude/sessions/<pid>.json`
  (`sessionId`, `messagingSocketPath`, `pid`, `status`). Located via
  `os.UserHomeDir()`; on Windows the path is a named pipe
  (`messagingSocketPath` still names it). Protocol: connect, optionally
  `{"type":"auth","token":...}` (skip), then one JSON line — the
  message object. The exact shape of the message line is **not** in
  the public docs; capture it once by running `claude --debug` in two
  sessions and sending a message (open question 1). Store the captured
  shape as a fixture with a comment naming the Claude version.
- Pi extension API: `pi.sendUserMessage(text)` (always triggers a turn;
  queues while streaming), `pi.sendMessage({..., deliverAs:"steer"|"followUp", triggerTurn})`,
  `ctx.isIdle()`, `session_shutdown` for cleanup. `node:net` Unix
  server for the inbox.
- `cmd/hived/main.go` subcommands (`hook`, `idea`) — `msg` is the
  third, same client-mode shape.
- GUI: sidebar context menu (`components/SessionRow.tsx` /
  `Sidebar.tsx`), modal pattern (`components/modals/`), status bar
  (`components/StatusBar.tsx`), keymap tables.

## Approach

### Wire

- `FrameSendToSession 0x29` (C→S), `FrameSent 0x2a` (S→C).
  `SendToSessionReq{SessionID, Text, Queue bool}`,
  `SentResp{SessionID, Delivery string}` with `Delivery ∈ {socket, extension, typed, queued}`.
  `Text` capped at 64 KiB (Claude's own cap is ~1 MB; ours is smaller
  on purpose — this is a note, not a transcript).
- `UpdateSessionReq` gains `NotifyWhenIdle *bool json:"notify_when_idle,omitempty"`;
  `SessionInfo` gains `NotifyWhenIdle bool` (in-memory, like
  `NeedsAttention`) so every client renders the bell glyph.
- `ErrCodeSessionBusy`, `ErrCodeSessionDead`, `ErrCodeDeliveryFailed`.
- `DaemonContract++`.

### Delivery strategies (`internal/registry/deliver.go`)

```go
type deliverer interface {
    // Deliver returns the wire.Delivery* string on success.
    Deliver(ctx context.Context, e *Entry, text string) (string, error)
}
```

Chosen in `registry.SendToSession(id, text, queue)` by `e.Agent`:

- **`claudeInbox`** (`internal/agent/claude_inbox.go`, domain layer —
  no registry import): `FindInbox(sessionID string) (path string, err)`
  scans `~/.claude/sessions/*.json` (never `*.key`), unmarshals only
  `sessionId`, `messagingSocketPath`, `pid`; checks the pid is alive
  (`syscall.Kill(pid, 0)` on Unix; `OpenProcess` on Windows); returns
  the path. `Send(path, text)` dials with 2 s timeout, writes the
  captured message shape (fixture-driven, open question 1) as one JSON
  line, waits for EOF or 1 s, closes. Errors ⇒ fall back to `typed`
  with a log line, so a Claude version that changes the registry shape
  degrades instead of breaking.
- **`piInbox`**: the 336 extension gains a `net.createServer` on
  `<StateDir>/pi/inbox/<HIVE_SESSION_ID>.sock` at `session_start`,
  removed at `session_shutdown`; each connection = one UTF-8 message
  terminated by EOF; on receipt `pi.sendUserMessage(text)`. Registry
  side: dial that path, write, close. If the socket is missing (old
  extension, Windows) ⇒ fall back to `typed`.
- **`typed`**: if `e.state.Snapshot().State != idle` ⇒ `queue ? enqueue :
  ErrCodeSessionBusy`. Else write `text + "\r"` to the PTY. Multi-line
  text: replace `\n` with `\r` only for the typed path, documented as a
  ceiling (`ponytail:` comment).
- Dead session (`!Alive`) ⇒ `ErrCodeSessionDead` before any strategy.
- **Version gate**: `claudeInbox` is attempted only when 336's
  `claudeVersion()` is within `[minInboxVersion, maxKnownBadInboxVersion)`;
  otherwise `typed` directly, no dial. Both internal surfaces this
  strategy touches (registry JSON shape, message line) are isolated in
  `claude_inbox.go` with the fixture pinned to a Claude version; the
  design doc's "Surviving Claude Code's churn" table is the contract.

Registry exposes `SendToSession`; daemon arm dispatches it to a
goroutine (it dials sockets) drained in `Close`, like the git frames.

### Ping when idle

- `Entry.NotifyWhenIdle bool`. `UpdateSession` with the pointer set
  flips it and broadcasts `SessionEventUpdated`.
- In the same place the registry reacts to a state change (336's
  `ApplyAgentEvent`/`Tick`/hook callbacks all funnel through one
  `onStateChanged(e, prev, next)`), if `e.NotifyWhenIdle && prev == working
  && next ∈ {idle, exited, error}` ⇒ `notify.Send(title, e.LastSummary)`,
  clear the flag, broadcast. Notification title:
  `"<session> is <idle|done|failed>"`; body = `LastSummary` or empty.

### `hived msg` (`cmd/hived/msg.go`)

`hived msg <session-name-or-id> <text…>` — resolves a name via
`LIST_SESSIONS` (exact, then unique prefix), sends `SEND_TO_SESSION`
with `queue: true`, prints the delivery kind. Lets a Pi session (or a
shell) message any other session with the same semantics the GUI has.

### GUI

- **Message sheet** (`components/modals/MessageSession.tsx`): single
  multiline input, target name in the title, Enter sends, ⇧Enter
  newline. On `ERROR session_busy` the sheet stays open and shows
  "Working — queue until idle?" with a Queue button that resends with
  `queue: true`. On `SENT` show a status-bar toast ("Delivered to
  *name* via Claude inbox" / "Queued for *name*"), close, return focus.
- Entry points: ⇧⌘M on the focused session; sidebar row context menu
  "Message…".
- **Ping toggle**: context menu "Notify me when idle" (checkbox state
  from `notify_when_idle`); ⌥⌘B toggles for the focused session. Row
  shows a small bell glyph while set; clears when the daemon clears it.
- Keymap tables doc updated.

### Files to change

- `internal/wire/control.go`, `frame.go` — frames, req/resp, error codes,
  `UpdateSessionReq.NotifyWhenIdle`, `SessionInfo.NotifyWhenIdle`.
- `internal/buildinfo/contract.go` — bump.
- `internal/registry/registry.go` — `NotifyWhenIdle`, `onStateChanged`,
  `pendingInputs` drain.
- `internal/daemon/daemon.go` — `FrameSendToSession` arm (goroutine),
  `UpdateSession` passthrough.
- `internal/agent/pi/hive.ts` — inbox server.
- `cmd/hived/main.go` — `msg` subcommand.
- `cmd/hivegui/app_calls.go` — `SendToSession`, `SetNotifyWhenIdle` bindings.
- Frontend: `bridge.ts`, `store/store.ts`, `app/keyboard.ts`,
  `components/SessionRow.tsx`, `Sidebar.tsx`, `StatusBar.tsx`.
- `docs/product-specs/keyboard-keymap-tables.md`, `DESIGN.md` (the
  `.key` rule; the Pi inbox dir).

### New files

- `internal/registry/deliver.go`, `deliver_test.go`
- `internal/agent/claude_inbox.go`, `claude_inbox_test.go`,
  `testdata/claude-inbox/message.json` (captured shape + version)
- `cmd/hived/msg.go`, `msg_test.go`
- `cmd/hivegui/frontend/src/components/modals/MessageSession.tsx` + test
- `scripts/check-no-claude-keys.sh` (grep guard, wired into `scripts/test.sh`)
- The drift probe (which shipped as `cmd/hived/claude_probe_test.go`
  behind `HIVE_PROBE_CLAUDE=1`, NOT as the `scripts/probe-claude.sh`
  this line originally assumed) gains a 338 section: assert
  `~/.claude/sessions/<pid>.json` for the probe session still carries
  `sessionId`, `messagingSocketPath`, `pid`; send one message over the
  socket and assert the `-p` session's output echoes it.

### Tests

- `agent`: `TestFindInboxMatchesSessionID`, `TestFindInboxIgnoresDeadPid`,
  `TestFindInboxNeverOpensKeyFiles` (fake home dir with a `.key` file
  whose read would panic via a test-only `os.Open` wrapper — or simpler:
  the grep guard script), `TestSendWritesOneLine` against a fake Unix
  server.
- `registry`: `TestSendTypedRequiresIdle`, `TestSendQueuedDrainsOnIdle`,
  `TestSendDeadSession`, `TestClaudeFallsBackToTyped`,
  `TestNotifyWhenIdleFiresOnce`, `TestNotifyWhenIdleIgnoresIdleToIdle`.
- `daemon`: `TestSendToSessionOffReadLoop` (a blocked deliverer must
  not stall another client's `LIST_SESSIONS`).
- `cmd/hived`: `TestMsgResolvesUniquePrefix`, e2e typed path with a
  shell session.
- Pi extension: `node --test internal/agent/pi/hive.test.mjs` — fake
  `pi` object, open the inbox socket, assert `sendUserMessage` called.
  Skipped in Go test when `node` is absent; required on the Linux CI leg.
- Frontend: reducer tests; Playwright mock e2e for the sheet (busy →
  queue path) and the bell toggle.

### Phasing

1. Wire + registry `SendToSession` with typed/queued only + `hived msg`
   + GUI sheet. Shippable, agnostic.
2. Ping-when-idle (registry + GUI). Small; can ride with 1.
3. Claude inbox strategy (after open question 1).
4. Pi inbox strategy.

## Decision log

- **2026-09-04** — Claude messages go through Claude's own inbox socket,
  never the PTY. Why: they arrive with the right provenance ("from
  another session"), respect the receiver's inbound controls, and never
  corrupt a TUI mid-render.
- **2026-09-04** — Every richer strategy falls back to `typed`, never to
  an error. Why: the agnostic floor must always work; a Claude/Pi
  upgrade that breaks a strategy should degrade visibly, not block.
- **2026-09-04** — No message history. Why: the receiving agent's
  transcript already holds it; a second copy is a sync problem.

## Progress

- **2026-09-04** — Spec and plan written; stage PLAN.

## Open questions

1. **Exact JSON line Claude expects on its inbox socket.** Public docs
   describe the socket, the optional auth line, and the 30 s
   first-line timeout, but not the message object's fields. Capture
   from two `claude --debug` sessions before phase 3. If the shape
   proves private/unstable, phase 3 becomes "typed with a leading
   `[from hive]` marker" and the decision log records why.
2. **Sender name.** Claude shows `Message from @<name>`; the name comes
   from the sender's registry entry. Hive has no entry. Either the
   message line carries a display name field (likely) or it shows as
   unnamed. Resolve with question 1.

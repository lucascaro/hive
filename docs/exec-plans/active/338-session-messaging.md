# Session messaging: hand a session a message, get told when it idles

- **Spec:** [docs/product-specs/338-session-messaging.md](../../product-specs/338-session-messaging.md)
- **Design:** [docs/design-docs/control-plane.md](../../design-docs/control-plane.md)
- **Issue:** —
- **Branch:** `feature/338-claude-inbox`
- **PR:** [#515](https://github.com/lucascaro/hive/pull/515)
- **Status:** active
- **Phase:** 2 of 3

## Summary

One control frame, `SEND_TO_SESSION`, with three delivery strategies
chosen by the target's agent (Claude inbox socket, Pi extension inbox,
typed-on-idle), plus a one-shot "notify me when idle" flag on the
registry entry. Depends on spec 336 for `idle`, the `event` mode and
the embedded Pi extension. Written so each phase is one agent run.

## Research

- Spec 336 **as shipped** (not as planned — it merged as #338/#341/#344/
  #348/#350 and the names moved; verified against `main` 2026-09-06):
  `agentstate.Machine` + `Entry.state`, the ticker,
  `internal/agent/pi/hive.ts`, `Def.SpawnArgs`. This plan extends all
  four. Four call-outs where this plan still names things 336 did not
  ship under those names — see **Plan freshness** below before writing
  code.
- `internal/session/session.go` — the PTY input path the attach mode
  uses for `FrameData` C→S (find `Write`/`Input` on `Session`); the
  typed strategy calls it directly from the registry.
- `internal/registry/create.go` (after 337) — `pendingPrompt` typed on
  first idle; the "queue until idle" strategy generalises it to a
  small FIFO `pendingInputs []string` drained one per idle transition
  with a 300 ms grace.
- `internal/wire/control.go:~580` — `Error` + `ErrCode*` constants (336’s
  `SessionInfo` fields pushed the block down from :415);
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
- **Version gate**: 336 shipped this as `claudeVersionSupportsHooks()`
  with `minHooksVersion = "2.1.0"` and `maxKnownBadHooksVersion = ""`
  (`internal/agent/claude.go:128,167,175`) — there is no
  `claudeVersion()`, and `minInboxVersion` /
  `maxKnownBadInboxVersion` do not exist. Copy the shape, add inbox
  constants of its own: `claudeInbox` is attempted only when the parsed
  version is within `[minInboxVersion, maxKnownBadInboxVersion)`;
  otherwise `typed` directly, no dial. Both internal surfaces this
  strategy touches (registry JSON shape, message line) are isolated in
  `claude_inbox.go` with the fixture pinned to a Claude version; the
  design doc's "Surviving Claude Code's churn" table is the contract.

Registry exposes `SendToSession`; daemon arm dispatches it to a
goroutine (it dials sockets) drained in `Close`, like the git frames.

### Ping when idle

- `Entry.NotifyWhenIdle bool`. `UpdateSession` with the pointer set
  flips it and broadcasts `SessionEventUpdated`.
- In the same place the registry reacts to a state change. 336 shipped
  that funnel as `announceStateLocked(e *Entry, prev agentstate.Snapshot,
  reason string)` (`internal/registry/registry.go:474`) — not
  `onStateChanged(e, prev, next)`, and it carries a `reason` string
  rather than the next snapshot (read `e.state.Snapshot()` for that).
  If `e.NotifyWhenIdle && prev == working && next ∈ {idle, exited,
  error}` ⇒ notify, clear the flag, broadcast. Notification title:
  `"<session> is <idle|done|failed>"`; body = `LastSummary` or empty.
- **Open: who fires that notification.** This plan says the registry
  calls `notify.Send`. Neither half survives contact with `main`: the
  function is `notify.Notify(title, subtitle, body, tag)`, and
  `internal/notify` is imported by exactly one file, `cmd/hivegui/app.go`.
  336 recorded that as a deliberate boundary — the GUI fires
  notifications on the event it receives, the daemon never imports
  `internal/notify` — so having the registry notify would invert it, and
  would also fire nothing at all for a user running `hived` with no GUI
  attached, which is not obviously wrong but is a different product
  decision. Resolve before Phase 3: either broadcast a session event and
  let the GUI notify (consistent with 336, no daemon-side notification
  when headless), or argue explicitly for the daemon owning it and
  record the reversal in the decision log.

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
- `docs/product-specs/478-decompose-keyboard-ts-into-per-scope-keymap-tables.md`, `DESIGN.md` (the
  `.key` rule; the Pi inbox dir).

### New files

- `internal/registry/deliver.go`, `deliver_test.go`
- `internal/agent/claude_inbox.go`, `claude_inbox_test.go`,
  `testdata/claude-inbox/message.json` (captured shape + version)
- `cmd/hived/msg.go`, `msg_test.go`
- `cmd/hivegui/frontend/src/components/modals/MessageSession.tsx` + test
- `scripts/check-no-claude-keys.sh` (grep guard, wired into `scripts/test.sh`)
- (Not built in phase 2 — deferred with the manual real-Claude check; the probe file itself predates phase 2.) The drift probe (which shipped as `cmd/hived/claude_probe_test.go`
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

## Plan freshness

Checked against `main` on 2026-09-06, after all of 336 merged. This plan
was written before 336 shipped, and four things it names did not ship
under those names. Each is corrected in place above; collected here so a
worker sees them before picking a phase.

| Plan said | `main` has | Where |
|---|---|---|
| `onStateChanged(e, prev, next)` | `announceStateLocked(e, prev, reason)` | `registry.go:474` |
| `notify.Send(title, body)` from the registry | `notify.Notify(title, subtitle, body, tag)`, imported only by `cmd/hivegui/app.go` | layering decision, see Approach |
| `claudeVersion()`, `minInboxVersion`, `maxKnownBadInboxVersion` | `claudeVersionSupportsHooks()`, `minHooksVersion`, `maxKnownBadHooksVersion` | `claude.go:128,167,175` |
| `control.go:415-450` for `ErrCode*` | the block is at ~`:580` | `control.go` |

Two dependencies to weigh before starting, neither a defect in this plan:

- **Frame ids assume 337 ships first.** This plan takes `0x29`/`0x2a`;
  337 claims `0x23`–`0x28`. `main` runs to `0x22`. If 338 goes first,
  either take `0x23`/`0x24` and let 337 renumber, or keep the gap
  deliberately and say so — an unexplained hole in the id space is the
  kind of thing a later reader "tidies up".
- **The `pendingInputs` FIFO generalises 337's `pendingPrompt`**, which
  does not exist yet (337 is unstarted). Phase ordering has to account
  for building the thing it generalises, or build the FIFO first and
  let 337 adopt it.

`DaemonContract` is 4 on `main` (336 took it to 4); this plan's bump
makes it 5.

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

- **2026-09-06** — refreshed against `main` after all of 336 merged
  (#338/#341/#344/#348/#350). No implementation yet. Four stale symbol
  names corrected in place and collected in **Plan freshness**; the one
  that needs a decision rather than a rename is who fires the
  ping-when-idle notification, since 336 deliberately kept
  `internal/notify` out of the daemon.

- **2026-09-04** — Spec and plan written; stage PLAN.

## Approved plan — Phase 1 of 3 (2026-10-13)

Supersedes the Approach, Files to change, New files, Tests and Phasing
sections above for Phase 1; they remain as history. Phase 2 = Claude inbox
socket (needs the message shape captured from a real `claude --debug`
pair); Phase 3 = Pi extension inbox. Where REVISION 1/2 below conflicts with
the draft body, the revision wins.

Existing (stale) plan: docs/exec-plans/active/338-session-messaging.md — this draft supersedes its Approach/Phasing.

## Freshness corrections vs the existing plan (verified on main today)
- Next free frame id is 0x46 (0x45 = FrameSetSessionKind is last, internal/wire/frame.go:260). Plan said 0x29/0x2a — taken.
- DaemonContract is 26 (internal/buildinfo/contract.go:240) -> bump to 27. Plan said 5.
- 337 shipped (pendingPrompt/typedPrompt exist: internal/registry/create.go:137, registry.go:208/752). Generalise it, don't build it.
- `acp`-kind sessions exist (spec 496): Registry.PromptACP(id, text, origin) (internal/registry/acp.go:650) already takes an `origin` principal recorded on the transcript. A 4th delivery strategy `acp` is trivial and gives provenance for free. Maps ErrACPBusy -> session_busy.
- Keybindings are data now (AGENTS.md Keybindings Policy): chords go in src/lib/bindings.ts, command in src/app/commands.ts, MENU_COMMANDS + menu_darwin.go + cmd/hivegui/testdata/menu-default-accelerators.json, row in src/lib/shortcuts.ts, README Keybinds table. Existing plan's `app/keyboard.ts` and "spec 478 doc" lines are stale.
- Notification layering: internal/notify is imported only by cmd/hivegui/app.go; daemon/registry never import it.

## Phase split (spec has 6 success criteria; they span 3 strategies)
- Phase 1 (this PR): wire + registry SendToSession with strategies typed / queued / acp; `hived msg`; GUI message sheet; ping-when-idle (flag + GUI notification); `.key` grep guard; log line `send_to_session caller=... from=... to=... delivery=...`.
- Phase 2: Claude inbox socket (blocked on capturing the undocumented message shape via `claude --debug`; needs a real Claude).
- Phase 3: Pi extension inbox.
Plan header gets `Phase: 1 of 3`; gate records per-phase PASS (spec holds at GATE; manual reset for phase 2).

## Approach (Phase 1)
Wire: FrameSendToSession 0x46 (C->S), FrameSent 0x47 (S->C). SendToSessionReq{session_id,text,queue}, SentResp{session_id,delivery in typed|queued|acp}. Text cap 64 KiB. ErrCodeSessionBusy/SessionDead/DeliveryFailed. UpdateSessionReq.NotifyWhenIdle *bool; SessionInfo.NotifyWhenIdle bool (in-memory). DaemonContract 27. Snake_case tags; update the 3 clients in lock-step (cmd/hivegui/app_calls.go, cmd/hived-ws-bridge/main.go, internal/wire/testclient) + JS snake_case ?? camelCase readers. NOT in sessionModeFrames (a session socket must not send; that is spec 389).
Registry (internal/registry/deliver.go): SendToSession(id,text,queue). Dead -> session_dead. acp kind -> PromptACP(id,text,wire.OriginUser) (ErrACPBusy -> session_busy unless queue... acp queue unsupported in v1: return session_busy). PTY kinds: state != idle -> queue ? append to Entry.pendingInputs : session_busy; idle -> write text+"\r" (sanitised via typedPrompt rules) to the PTY; delivery typed. pendingInputs FIFO drained one per idle transition (300ms grace), in the same place pendingPrompt is resolved (registry.go ~752). Drain must be cleared on exit/kill.
Daemon: arm runs the registry call in a goroutine tracked by the existing wg drained in Close (so a slow PTY write cannot stall the read loop).
Ping when idle: Entry.NotifyWhenIdle set via UpdateSession. In announceStateLocked (registry.go:708), when flag && prev==working && next in {idle,exited,error}: clear flag and broadcast SessionEventUpdated with a new field/marker so the GUI fires notify.Notify (GUI owns notifications, consistent with 336's boundary; no notification for headless hived). DECISION FOR OPERATOR (see below).
hived msg (cmd/hived/msg.go): `hived msg <name-or-id> <text...>`; resolve via LIST_SESSIONS exact then unique prefix; queue:true; print delivery. Control socket only (session socket is 389).
GUI: MessageSession modal (Enter send, Shift+Enter newline; session_busy -> "Working - queue until idle?" button resends queue:true; status-bar toast on SENT). Entry points: sidebar row context menu "Message..." and "Notify me when idle" (checkbox), bell glyph on row; chords Mod+Shift+M and Mod+Alt+B as bindings/commands (not hard-coded), shown via shortcutLabel.
Docs: DESIGN.md (new frames; `.key` rule), docs/design-docs/control-plane.md, README keybinds, .changesets/338-session-messaging.md (minor), site/features.json entry since:"Unreleased".

## Files to change
internal/wire/{frame.go,control.go}; internal/buildinfo/contract.go; internal/registry/{registry.go,deliver.go(new)}; internal/daemon/daemon.go; cmd/hived/{main.go,msg.go(new)}; cmd/hived-ws-bridge/main.go; internal/wire/testclient; cmd/hivegui/{app_calls.go,menu_darwin.go,testdata/menu-default-accelerators.json}; frontend src/{lib/bindings.ts,lib/shortcuts.ts,app/commands.ts,store/*,bridge.ts,components/SessionRow.tsx,components/modals/MessageSession.tsx(new),StatusBar.tsx}; scripts/check-no-claude-keys.sh(new, wired into scripts/test.sh); README.md; DESIGN.md; docs/design-docs/control-plane.md; .changesets/; site/features.json.

## Tests
registry: TestSendTypedRequiresIdle, TestSendQueuedDrainsOnIdle (one per idle transition), TestSendQueueClearedOnExit, TestSendDeadSession, TestSendACPUsesPromptACP, TestSendACPBusy, TestNotifyWhenIdleFiresOnce, TestNotifyWhenIdleIgnoresIdleToIdle.
daemon: TestSendToSessionOffReadLoop (blocked deliverer does not stall another client's LIST_SESSIONS); TestSendToSessionRefusedOnSessionMode (not in sessionModeFrames); log line greppable.
cmd/hived: TestMsgResolvesUniquePrefix; e2e typed path with a shell session (text lands only after idle; working -> session_busy unless queue).
frontend: unit/reducer; dom for glyph; playwright mock e2e for sheet busy->queue path and bell toggle; every-shortcut sweep covers new chords automatically.
Contract: scripts/check-daemon-contract.sh origin/main HEAD passes.

## Verification
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./internal/... ./cmd/hived/...; scripts/test.sh; for os in darwin linux windows; do GOOS=$os go vet ./... ; done (+ staticcheck per AGENTS.md); scripts/check-daemon-contract.sh origin/main HEAD; scripts/check-no-claude-keys.sh; CI=1 playwright via scripts/test.sh e2e.

## Risks / open
- Typed path into an arbitrary TUI can corrupt a prompt being edited; mitigated by idle-only + documented ceiling.
- Multi-line text on typed path: newline->CR (ceiling, ponytail comment).
- Phase 2 hinges on an undocumented Claude protocol.
- Spec success criteria for Claude/Pi paths are deferred to phases 2/3 (per-phase gate).

---
# REVISION 1 (applies round-1 must_fix; these override anything above that conflicts)

1. **Queue drain.** Drain hooks into `announceStateLocked` (registry.go:708), NOT ResolvePrompt (registry.go:752). It runs under r.mu, so it only *schedules*: on a qualifying transition to idle it starts a `time.AfterFunc(300ms)` stored on the Entry (`drainTimer`). At fire time (no lock held for the PTY write): take r.mu, re-check entry still present, `sess != nil`, state == idle and queue non-empty, pop ONE item, release r.mu, write to PTY. If the session went working again during the grace, do nothing (next idle re-arms). Timer is stopped and queue cleared on exit, kill and restart (Restart nils e.sess). Queue cap: 16 items / 64 KiB total; over cap -> `delivery_failed`.
2. **Goroutine tracking.** Use `Daemon.runOp` (daemon.go ~186; refuses work after opsClosed; ops intentionally not waited in Shutdown, comment daemon.go:145-150). Only the PTY write needs it; PromptACP is synchronous and cheap. Test: Close with an in-flight delivery neither hangs nor panics.
3. **Plugin exposure (security-sensitive -> Opus review before merge).** SEND_TO_SESSION lands in ControlRequestFrames and is therefore plugin-callable (docs/plugins.md:225-245; TestEveryFrameClassified wire/plugins_test.go:51). Decision for v1: **refuse for plugin principals** with `ErrCodeModeNotAllowed` (use `ops.principal`, daemon.go:1567 pattern; a `plugin:` prefix check), document the refusal in docs/plugins.md, test it. Rationale: typing into a PTY is the confused-deputy case in docs/design-docs/agent-orchestration.md; plugins get it only via a later explicit capability. Also: not in sessionModeFrames (daemon.go:1343) — real test `TestSendToSessionRefusedOnSessionMode`.
4. **Enumerated sites added to Files to change:** wire/frame.go frame-name String() switch (~405-420) and ControlRequestFrames/response classification (~265-285); internal/daemon/control_frame_test.go bad_payload table (:70-86); daemon handleControlFrame switch (~1562); docs/plugins.md row; wire SessionEvent kinds (control.go:524-553), JS event handlers, DESIGN.md event list.
5. **Idle semantics (typed path), stated plainly:** "idle" is whatever agentstate reports — hook-driven for Claude/Pi, but a 2 s no-output heuristic for shells/unhooked agents (registry.go:706-716). So typed delivery means "after output has been quiet 2 s", not "the program is reading input". Documented in the sheet copy ("sent when the session is quiet") and DESIGN notes. E2E asserts BOTH: nothing reaches the PTY while the shell is producing output (negative), and text lands after quiet (positive), plus queue-then-exit (queue dropped, no write).
6. **PTY sanitiser (new, not typedPrompt).** `sanitizeInput(text)`: reject (bad_payload) if empty after trim or > 64 KiB; strip all C0 control bytes except `\n` and `\t`, strip DEL and ESC (so no escape-sequence or bracketed-paste-terminator injection); `\n` -> `\r` on write; no whitespace collapsing, no Windows `%`/`"` stripping. Table test incl. ESC[201~ injection, CRLF, tabs, 64 KiB boundary.
7. **Ping-when-idle semantics.** Flag logic runs in announceStateLocked *before* the `cur==prev` early return is irrelevant: we key on `prev.State == working` (waiting/attention states do NOT count as working) and `cur.State ∈ {idle, exited, error}`; fires on the first such transition only, clears the flag in the same locked section. Cleared on Restart; in-memory only (dies with daemon; survives GUI reload since it lives in registry). Marker: new `SessionEvent` kind `idle_ping` (enumerated-sites treatment as in item 4), carrying title/body source (`LastSummary`). Every attached GUI receives it; double-notification is avoided by passing the session id as the `notify.Notify` tag so the OS collapses duplicates. Headless hived never pings (GUI owns `internal/notify`, per 336); changeset and features.json blurb say "in the app". **Operator decision flagged.**
8. **Verification made real.** (a) `TestNoClaudeSessionKeyReads` — a Go test (runs in `go` layer/CI, not just scripts/test.sh) that walks non-test *.go and *.ts/*.tsx sources and fails on any string literal that contains both `.claude` and `sessions` AND ends a path segment with `.key` (regex `\.claude[^"\x60]*sessions[^"\x60]*\.key|sessions/\*\.key`); with a planted-violation self-test fixture proving it fires. No shell script. (b) Log line asserted: daemon test captures the logger and matches `send_to_session caller=(control|session) from=\S* to=\S+ delivery=\w+`; `from` for control path = "" / principal from `ops.principal`. (c) Contract: `scripts/check-daemon-contract.sh origin/main HEAD` AND `scripts/check-daemon-contract-selftest.sh`.
9. **ACP queue.** `queue:true` on an `acp` session is NOT supported in v1: PromptACP's ErrACPBusy -> `session_busy` regardless of queue; the GUI hides the "queue until idle" button when kind=acp (shows "busy — try again"). Tests: TestSendACPBusyIgnoresQueue (registry), dom test button hidden for acp.
10. Spec amendment in this PR: success criterion lists `SENT{delivery: socket|extension|typed|queued}`; add `acp`. `hived msg` resolves by exact id or exact name only (no prefix match — typed input into the wrong session is a footgun) and prints the resolved target before sending. Check DEFAULT_APP_BINDINGS / RESERVED_CHORDS for collisions before fixing Mod+Shift+M / Mod+Alt+B; menu-default-accelerators.json only if a menu item is added.

---
# REVISION 2 (round-2 must_fix; overrides conflicting text)
11. **Exit path.** watchSessionExit (registry.go:1577-1620) does Exit()/logStateLocked/manual broadcast and never calls announceStateLocked. Extract one locked helper `settleSendStateLocked(e, prev, cur)` that (a) evaluates the ping rule and (b) on exit/kill/restart stops `drainTimer` and clears `pendingInputs`; call it from announceStateLocked AND watchSessionExit (where e.sess=nil). Tests: TestNotifyWhenIdleFiresOnExit, TestSendQueueClearedOnExit (asserts timer stopped). Verify `StateError` name exists in agentstate before listing it.
12. **Key-read guard.** TestNoClaudeSessionKeyReads flags per FILE (non-test .go/.ts/.tsx): contains a `".claude"` token AND a `"sessions"` token AND a `.key` token (any literal ending/containing `.key` or `"*.key"`). Self-test fixture plants the realistic split form `filepath.Join(home, ".claude", "sessions", id+".key")` plus the single-literal form and asserts both are caught.
13. Nice-to-haves adopted: Registry.Close stops all drain timers; fire path tolerates and logs a write error; plugin-refusal test runs over a real plugin socket; sheet copy notes the bell is lost on daemon restart.


### Conventions card (added 2026-10-13, from AGENTS.md)

Checks (verify each by exit code, not output grep). Toolchain: `export GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`.
- Bootstrap (fresh worktree, needed for typecheck): `./scripts/ci-bootstrap.sh`
- Tests, all layers: `scripts/test.sh` (run Playwright with `CI=1`)
- Static: `for os in darwin linux windows; do GOOS=$os go vet ./... ; GOOS=$os staticcheck ./... ; done`
- Frontend: `cd cmd/hivegui/frontend && npm run typecheck && npx biome ci .`; `scripts/ui-lint.sh`
- Gates: `scripts/check-daemon-contract.sh origin/main HEAD`, `scripts/check-daemon-contract-selftest.sh`, `scripts/check-changeset.sh`
- Conventions: wire JSON snake_case (JS readers `snake_case ?? camelCase`); update all 3 wire clients in lock-step (hivegui app_*.go, hived-ws-bridge, wire/testclient); shell-outs via `internal/proc` only; keybindings are data (bindings.ts/commands.ts/shortcuts.ts, README, menu accelerators json); changeset `.changesets/<slug>.md` + `site/features.json` entry `since: "Unreleased"`; never write a literal skip-ci marker in commit/PR text.

### Spec success criteria -> phase

| Criterion | Phase |
|---|---|
| SEND_TO_SESSION / SENT frames, error codes | 1 (delivery enum gains `acp`; amend spec) |
| Typed path e2e, busy vs queue | 1 |
| Ping-when-idle fires once, flag clears; GUI glyph | 1 |
| `.key` source-tree guard | 1 |
| Claude inbox: fake-socket test + manual checklist | 2 |
| Pi extension inbox: node test + manual checklist | 3 |

## Second opinion

Independent reviewer (general-purpose subagent), 2 rounds.
- Round 1: `revise`, confidence 8, 8 must-fix (drain site, runOp, plugin exposure, enumerated sites, idle semantics, PTY sanitiser, ping-flag semantics, vacuous verification, ACP queue). All applied -> REVISION 1.
- Round 2: `revise`, confidence 8, 2 must-fix (exit path never reaches announceStateLocked; key-read guard evadable by split literals). Both applied -> REVISION 2. Not re-reviewed (one revise round allowed).

## Decision log (additions)

- **2026-10-13** — Ping-when-idle notification is fired by the GUI from a new `idle_ping` event; daemon never imports `internal/notify` (336 boundary). Headless hived never pings. Operator-approved.
- **2026-10-13** — Plugins are refused `SEND_TO_SESSION` in v1 (`mode_not_allowed` via `ops.principal`): typing into a PTY is the confused-deputy case. Operator-approved.
- **2026-10-13** — Delivered as `Phase: 1 of 3`; per-phase gate. Operator-approved.
- **2026-10-13** — Queue is unsupported for `acp` sessions (`session_busy`); GUI hides the queue button for them.

## Progress (Phase 1 implementation, 2026-10-13)

- Shipped on `feature/338-session-messaging`: wire frames `SEND_TO_SESSION`/`SENT` (0x46/0x47), `notify_when_idle`, `idle_ping` event, DaemonContract 27; `internal/registry/deliver.go` (typed / queued / acp, PTY sanitiser, bounded queue, settle helper shared by announce and exit); daemon arm via `runOp`, plugin refusal; `hived msg` (exact id or name); GUI sheet (⇧⌘M), bell (⌥⌘B + row), `idle_ping` notification; plugin SDK frame table; docs, changeset, features.json.
- Verified: `go test ./...`, `-tags e2e ./cmd/hived`, per-GOOS vet + staticcheck, `npm run typecheck`, `biome ci`, unit + dom (1245), Playwright (551 passed; one `every-shortcut` plugin-view cell failed once under full-suite load and passed 12/12 in isolation), ui-lint, daemon-contract + selftest, test-hooks-stripped.
- Mutation-checked: exit-path settle, ping rule, one-per-idle drain, ACP queue offer, session:sent close, idle_ping notify, plugin SDK parity.

## Decision log (implementation additions)

- **2026-10-13** — The app has no sidebar context menu, so the plan's "context menu" entry points became: ⇧⌘M and ⌥⌘B chords, command-palette rows, and a bell button on the session row's hover actions (plus a passive bell glyph while armed). No new menu surface invented.
- **2026-10-13** — Typed delivery also refuses while an opening prompt is still pending (`session_busy`): a fresh agent may be on a startup gate that Enter would answer (the lesson behind spec 337's paste-not-type).
- **2026-10-13** — The plugin SDK (`plugins/sdk/hive-plugin.mjs` + vendored webhook copy) is a fifth wire client with drift tests; the plan's lock-step list missed it and the Go suite caught it.

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

## PR convergence ledger

- **2026-10-13 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 6abdefe7bbea14653a4d050a4d396854e25cdcf9bf56bda9fb742c93beb9b3a6; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: ad46a1a.
- **2026-10-14 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 656aec67f4bb1a281462e99985327668417df2cc60c447aabdf2131c827f5962; threads_open: 1; action: autofix+push; head_sha: ece5b70.
- **2026-10-14 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 9e8bfca.
- **2026-10-14 iter 1 (phase 2)** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 8f40c548e34f1b789ff7e03af9a02d920c25eb3bd032f50a9d0b5949ceb83a15; threads_open: 0; action: escalated:risky-fix-needs-human-decision (worker pushed test-only 69d0a7ff; the two risky hardening fixes applied by orchestrator in 992431d0); head_sha: 69d0a7ff.
- **2026-10-14 iter 2 (phase 2)** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: 3cdac4c83550cdbb1ea6973b34b32cdea6a3731a2bca741ef6fe7249e6b80bcc; threads_open: 0; action: stop (autofix+push d8b7bdc5 closed 2 threads first); head_sha: d8b7bdc5.

## Gate verdict

- **2026-10-14** — verdict: PASS; phase: 1/3; checks: 6 passed / 0 failed / 0 followups / 2 deferred; followups: none; one-line: typed/queued/acp delivery, `hived msg`, GUI modal and ping-when-idle verified; Claude and Pi inbox criteria deferred to phases 2 and 3.
  - 2026-10-14 dimensions:
    - acceptance — PASS — Claude inbox and Pi inbox criteria DEFERRED (phase > 1); typed, ping, key-file guard, GUI and `hived msg` verified by tests
    - non-goals — PASS
    - doc accuracy — PASS — three minor wording/count nits (README, changeset, icons.md) found by the validator and fixed in the PR before the verdict

## Approved plan — Phase 2 of 3 (2026-10-14): Claude inbox socket

## Captured protocol (claude 2.1.300, verified by hand 2026-10-14)
- Registration: ~/.claude/sessions/<pid>.json {sessionId, pid, messagingSocketPath=/tmp/cc-socks/<pid>.sock}. sessionId == Hive entry id (Hive passes --session-id).
- Socket dir 0700, socket 0600, owned by user. Claude debug log: "auth line optional here"; token lives only in the child's env (CLAUDE_CODE_MESSAGING_TOKEN) which Hive never reads.
- Framing: one JSON object per '\n'-terminated line; connection with no complete line in 30s is closed.
- Message line: {"type":"user","message":{"role":"user","content":"<text>"}}
- Verified: auth-less line to an idle session -> message appeared, nc exit 0.

## Approach
New delivery strategy `socket` (spec's name) tried FIRST for claude-agent PTY sessions, before typed/queued. It works whether Claude is idle or mid-turn (Claude reads it between tool calls), so no queue and no busy refusal. Any failure (no registry file, socket missing/not-a-socket/not owned by us, dial/write error, version gate) falls through to the existing typed/queued path. Never reads *.key (existing guard test stays green: lookup globs only *.json).
Dial happens outside r.mu with a short timeout (2s) so a wedged socket cannot stall the registry.
Content is json.Marshal'd from the already-sanitised text, so multi-line notes arrive intact (better than typed path's CR-per-line).

## Files
1. internal/agent/claude_inbox.go (+_windows stub, build tag !windows): FindClaudeInbox(sessionsDir, sessionID) (sockPath string, ok bool) — globs <sessionsDir>/*.json, matches sessionId, requires pid alive-or-socket exists, os.Lstat Mode&ModeSocket, uid == os.Getuid(); SendClaudeInbox(sockPath, text string, timeout) error — dial unix, write line, close.
2. internal/agent/claude.go: minInboxVersion = 2.1.300 (only verified version) + cached `claude --version` probe via internal/proc (reuse existing probe if one exists for hooks).
3. internal/registry/deliver.go: in SendToSession PTY branch, after pendingPrompt check and before the queue logic: if e.agent is claude && inboxFn != nil, drop r.mu, call inboxFn(id,text); success -> return wire.DeliverySocket. Failure -> re-take lock path unchanged. Registry gets an injectable `inbox func(id, text string) error` (default wires agent funcs; tests inject fake).
4. internal/wire/control.go: const DeliverySocket = "socket" (+ comment). Additive string value; no new frame.
5. internal/buildinfo/contract.go: bump DaemonContract (daemon-side behaviour).
6. Clients: cmd/hived/msg.go prints delivery (check no enum switch); cmd/hivegui frontend toast mapping for delivery strings (add "socket" -> "delivered"); ws-bridge/testclient pass strings through (verify, no change expected).
7. Docs: docs/design-docs/control-plane.md (surface table: message line now verified on 2.1.300, auth confirmed optional, fallback unchanged); spec 338 criteria wording; DESIGN.md only if needed; .changesets/338-claude-inbox.md (minor); site/features.json entry blurb updated (since Unreleased already exists from phase 1 — edit, don't duplicate).
New: internal/agent/testdata/claude_inbox_line.json (version-pinned fixture of the exact line).

## Tests
- agent: TestSendClaudeInboxWritesExactLine (fake unix listener asserts bytes == fixture, incl. multi-line + quotes escaping); TestFindClaudeInboxMatchesSessionID; TestFindClaudeInboxRejectsNonSocket (regular file at path); TestFindClaudeInboxRejectsForeignOwner (skip if not root/uid change impossible -> test the uid check via injected stat); TestFindClaudeInboxIgnoresKeyFiles (dir with .key files only -> not found, and .key never opened: assert via unreadable 0000 .key file not erroring); existing TestNoClaudeSessionKeyReads stays green.
- registry: TestSendClaudeUsesSocketWhenWorking (working state -> delivery socket, nothing written to PTY); TestSendClaudeFallsBackToTypedOnInboxError; TestSendClaudeFallsBackToQueuedWhenBusyAndInboxDown; TestSendNonClaudeNeverUsesInbox; TestSendSocketNotUnderLock (blocked fake inbox does not block another registry call).
- opt-in real probe: NOT built in phase 2 (needs a live Claude); the manual real-Claude check stays on the checklist.
- frontend: unit test for delivery->toast mapping includes socket.

## Verification
GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test ./internal/agent/... ./internal/registry/... ./cmd/hived/...; per-GOOS go vet + staticcheck (windows stub must compile); scripts/check-daemon-contract.sh origin/main HEAD; scripts/test.sh unit dom; manual: real hived+claude, `hived msg <claude-session> hi` while idle and while working -> arrives, delivery=socket.

## Risks
- Internal Claude surface may drift: mitigated by version gate, fixture, fallback to typed.
- Message provenance in Claude's UI unknown (spec expected "Message from @hive:"); the captured line has no sender field. Phase 2 does not claim provenance; manual check will record how it renders.
- Socket path from a stale registry file for a recycled pid: sessionId match + socket-owned-by-us + dial success; worst case falls back.

### Revisions from second opinion (round 1, revise, confidence 7; all must_fix applied)
- Skip socket when `pendingInputs` non-empty or `pendingPrompt` set; set `typedAt` on socket success.
- Match registry `sessionId` on `Entry.AgentSessionID` (fallback `Entry.ID`); field is `Entry.Agent`.
- `FindClaudeInbox` returns all candidates; send tries each.
- After failed dial, re-run deliver.go checks block (no reuse of stale `e`/`sess`).
- Frontend `message-session.ts:95-99` gets a `socket` branch; toast mentions bypass-mode hold (spec line 90).
- Replace vacuous `.key` test; fixture captured from real claude 2.1.300; probe hard-asserts arrival (superseded: the probe was not built in phase 2; see Tests).
- Spec criterion "Message from @hive:" edited to "record how it renders" in this PR.
- Reuse `claudeVersionProbe` (claude.go ~429-470) with a `claudeVersionAtLeast`. Reviewer re-run skipped for cost (operator informed).

## Second opinion
Independent hs-reviewer: revise, confidence 7, must_fix applied above (one round; no re-run).

# Stuck wire client can stall a session or hang daemon shutdown

- **Spec:** [docs/product-specs/461-stuck-wire-client-can-stall-a-session-or-hang.md](../../product-specs/461-stuck-wire-client-can-stall-a-session-or-hang.md)
- **Issue:** #461
- **Status:** completed
- **PR:** #462
- **Branch:** feature/461-stuck-wire-client

## Summary

A wire client that stops reading its socket can block the daemon: an attach
client stalls its session's PTY drain (the agent freezes), a control client
blocks every writer on its connection and pins `d.ops`, which hangs
`Daemon.Close()`, and a control client whose event subscription overflowed is
left connected but silently desynced. This plan makes the daemon disconnect a
client that stops reading, so it never blocks a session, other clients, or
shutdown, and makes a dropped subscription close the connection so the client
reconnects and re-snapshots.

## Research

### Relevant code

**Attach path (session stall)**
- `internal/session/session.go:313-331` `deliver` — writes each PTY chunk to
  every sink synchronously **under `s.mu`**. On a sink error it only removes
  the sink; it closes nothing.
- `internal/daemon/daemon.go:1811-1859` `frameSink` — `Write` and
  `writeReplay` serialize on `f.mu`; neither sets a write deadline. Lock order
  is always `s.mu` → `f.mu`. `writeReplay` runs *inside* `s.mu` (via
  `SubscribeWithAtomicReplay` at 1765 and `EmitAtomicReplay` at 1795), so a
  replay to a stuck client stalls the session too.
- `internal/daemon/daemon.go:1703` `serveAttach` — WELCOME written raw (1740),
  then `frameSink` registered; the read loop handles DATA/RESIZE/REQUEST_REPLAY.
  After the handshake, `frameSink` is the **single choke point** for attach
  writes.
- Everything that takes `s.mu` waits behind a blocked sink: readLoop
  (`deliver`, `noteTitle`, `noteBell`), `flushTitle`, `Set*Hook`,
  `fanoutClose`, `SubscribeWithAtomicReplay` (a second window attaching hangs),
  the unsubscribe closure (other clients' `serveAttach` defers hang), and
  `EmitAtomicReplay`. Knock-on: `Close()` kills the child but readLoop is stuck
  in `deliver`, so `done` never closes. That hangs `registry.Restart` at
  `<-sess.Done()` (`internal/registry/registry.go:1388`) and
  `watchSessionExit` (1457).
- Not blocked: `Resize` (ptmx + `vt.mu` only), `Write` (keystrokes), `Close`.

**Control path (fan-out, runOps, shutdown)**
- `internal/daemon/daemon.go:920` `serveControl`. It subscribes to five
  sources before WELCOME (944-953). `connMu` + `writeJSON` (968-973) serialize
  all writes with no deadline. The fan-out goroutine (976-1057) returns on
  **any** closed listener channel (`!ok`) or write error; `stop` is closed by a
  defer (1058). The read loop (1124-1142) sets a read deadline only for
  ModeSession.
- Every control writer goes through `writeJSON`: the fan-out, inline replies,
  and `runOp` goroutines (`daemon.go:148`; create 1299, kill 1313,
  transcripts 1355/1363, restore 1374, restart 1423, kill project 1466,
  worktrees 1510-1545). One stuck `Write` holds `connMu`, so every other writer
  queues: the read loop stops reading and runOps pin `d.ops`.
- `daemon.go:529-561` `Close()`: `stopOps` → `d.ops.Wait()` → close
  `d.clients`. A runOp blocked in `writeJSON` never returns, because the conn
  close that would free it runs after the wait. Closing a unix `net.Conn`
  unblocks a pending `Write`. `d.clients` holds control, attach, create and
  ModeSession conns; ModeEvent and planreview conns are untracked by design.
- `internal/registry/events.go:64-79` `broadcastLocked` (buffer 64): on
  overflow it logs "client is desynced until it resubscribes", deletes the
  listener and **closes the channel**. Same copy in activity (`events.go:~150`,
  buffer 128), `ideas.go:302`, `projects.go:681-688`, and
  `internal/daemon/commands.go:71-83` (buffer 8). Nothing ever resubscribes:
  the snapshot is only sent at handshake.

**Existing deadline patterns**
- `planreview.go:52/61` — the only daemon-side `SetWriteDeadline`
  (`eventReadDeadline = 2s`, `daemon.go:705`).
- `sessionModeIdleDeadline` (`daemon.go:713`) is a `var` so tests can shrink
  it, which is the pattern to copy for new timeouts.
- There is no `net.Conn` wrapper or bounded byte-queue helper anywhere in
  `internal/`.

**Clients on disconnect**
- Control EOF: hivegui `controlReadLoop` (`cmd/hivegui/app_control.go:427-456`)
  emits `control:disconnect`, and the frontend (`frontend/src/app/events.ts:1089-1103`)
  calls `reconnectControl()` with backoff and gets a fresh snapshot. hivebar
  (`cmd/hivebar/client.go:80-88`) redials every 2s. ws-bridge
  (`cmd/hived-ws-bridge/main.go:574-601`) emits the same `control:disconnect`.
  **All three already recover** once the daemon closes the conn.
- Attach EOF: `attachReadLoop` (`cmd/hivegui/app_attach.go:76-84`) emits
  `pty:disconnect`. The frontend (`events.ts:1052-1072`) sets
  `needsReattach = true` but only re-opens on a later
  `session:event(updated, alive=true)` (`events.ts:953-960`) or a session
  switch (`view.ts:120`). **Gap:** a daemon-initiated drop of a still-alive
  session produces no `updated` event, so the visible tile stays detached
  until the user switches away and back.

**Transport**
- AF_UNIX on every platform, Windows included (`socket.go:17-20`,
  `daemon.go:270/276`). `SetWriteDeadline` works on all daemon-side conns.
  ws-bridge dials hived over unix too.

### Tests and helpers to reuse
- `internal/session/session_test.go` — fake sink `bufSinkMu` (18-29),
  `TestSessionEchoAndPersistsState` (47). A blocking fake sink is easy to add.
- `internal/daemon/daemon_test.go` — `startTestDaemon` (41), `dial` (65),
  `handshake` (74), `readUntilReplayDone` (101), `drainFor` (132),
  `readControlFrame` (151), `awaitSessionEvent` (169), `skipOnWindows` (33);
  `TestAttachReattachReplay` (200), `TestRequestReplay` (784).
- `control_subscribe_test.go` `TestControlSubscribesBeforeWelcome` (23) drives
  `serveControl` over `net.Pipe`, a zero-buffer conn that makes a
  "never reads" client deterministic.
- `shutdown_test.go`, `boot_test.go` `TestCloseStopsBootWork` (203),
  `commands_test.go` `TestCommandHubDropsSlowSubscriber` (72),
  `registry/persist_logging_test.go` `TestBroadcastDropsAndWarnsOnSlowListener` (123).

### Constraints / dependencies
- **DaemonContract:** currently 17. The change adds no frames or fields, and
  old and new GUIs both recover from EOF, so **no bump**. The PR still touches
  `internal/{daemon,session,registry}`, so it needs the
  `daemon-contract-override` label (a claim, per
  `docs/design-docs/daemon-contract.md`).
- Attach replay atomicity (`writeReplay` inside `s.mu`) must be preserved:
  any queue must carry replay and live bytes in one ordered stream.
- Blocks #460 (plugins are just another wire client).

### Prior lessons
- No prior lessons matched (`brain-search` for write deadline / slow consumer /
  daemon shutdown returned nothing).

### Conventions card
- **Build:** `./build.sh` (macOS .app); Go-only: `go build ./...`
- **Tests:** `scripts/test.sh [go|unit|dom|e2e]`; `e2e-real`: `npm run test:e2e:real`
- **CI toolchain:** `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod)`
- **Static analysis per GOOS:** `for os in darwin linux windows; do GOOS=$os staticcheck ./... ; GOOS=$os go vet ./... ; done`
- **Contract gate:** `scripts/check-daemon-contract.sh <base> <head>`
- TDD: every behaviour change ships with the test that would have caught it.
  Go tests live beside source. e2e tests must isolate `HIVE_SOCKET` and
  `HIVE_STATE_DIR`.
- User-visible change: add `.changesets/<slug>.md` (`type: fixed`,
  `bump: patch`). Never edit `CHANGELOG.md` or `docs/product-specs/index.md`.
- Shell-outs go through `internal/proc`, never `os/exec` (unlikely to apply here).

## Approach

Policy: **the daemon never blocks on a client's write for more than a bound; a
client that stops reading is disconnected; every disconnected client
reconnects and re-snapshots.**

1. **Attach: per-sink bounded queue + writer goroutine** (`frameSink`).
   - `frameSink` owns an ordered FIFO of already-encoded frames and one writer
     goroutine. `Write` (live DATA, called by `session.deliver` under `s.mu`)
     and `writeReplay` (Begin → chunks → Done, called inside `s.mu` by the
     atomic-replay helpers) only **encode + enqueue** under `f.mu` and signal
     the writer. They never touch the conn, so `s.mu` is never held across
     socket I/O.
   - Ordering is unchanged: both enqueue paths still run under `s.mu`, so the
     replay/live interleaving that `SubscribeWithAtomicReplay` guarantees is
     preserved by FIFO order.
   - **Bound:** queued bytes may not exceed `attachBacklogLimit` (8 MiB) plus
     the length of the most recent replay, because a replay can be up to about
     `ringCap` (8 MiB) and must always fit. On overflow the sink closes the
     conn and returns an error, so `deliver` drops it.
   - **Deadline:** the writer sets `SetWriteDeadline(now + clientWriteTimeout)`
     (10s) before each write. On error it closes the conn. Then `serveAttach`'s
     `ReadFrame` fails, `unsub()` runs, and the client sees EOF.
   - `Write` **copies** `p` into the encoded frame. The PTY `readLoop` reuses
     its buffer, and the writer now runs after `s.mu` is released.
   - The replay allowance **drains**: `budget = attachBacklogLimit + the
     replay bytes still queued`, so it returns to the base limit once the
     replay has been written.
   - The limits and the timeout are **captured into the sink when it is
     constructed**, not read from package vars on every write. This avoids
     test races.
   - **The writer has an explicit stop path.** `serveAttach` does
     `defer sink.stop()` on every exit, including a `SubscribeWithAtomicReplay`
     error. `stop` marks the sink closed, wakes the idle writer, and closes the
     conn. Without it, an idle writer would wait forever on a signal nobody
     sends.
   - `Close()` (called by `fanoutClose` on PTY exit) is **graceful**: it marks
     the sink closing, and the writer drains the queue (still deadline-bounded)
     and then closes the conn. That keeps today's behaviour where the final
     output before exit reaches the client.
   - Why not a deadline alone: a stuck client would still freeze the agent,
     and every other viewer of that session, for the whole deadline while
     `s.mu` is held. The issue's bar is "never blocks a session".
2. **Control: deadline + close on write failure** (`serveControl.writeJSON`).
   Set `SetWriteDeadline(now + clientWriteTimeout)` before each write. On
   error, close the conn: a timed-out write may have left a partial frame, so
   the stream is unusable. Queued writers on `connMu` then fail fast, the read
   loop exits, and runOps finish. A queue is unnecessary here, because nothing
   shared is held across the write. Only this conn's own goroutines wait, and
   now for a bounded time.
3. **Dropped subscription closes the conn.** When the control fan-out
   goroutine exits for any reason (a closed listener channel from registry,
   project, idea or activity overflow, a commands-hub overflow, or a write
   error), it closes the conn. The client gets EOF and reconnects: hivegui,
   hivebar and ws-bridge all already redial and receive a fresh snapshot.
   Update the "desynced until it resubscribes" log wording in
   `registry/events.go`, `ideas.go`, `projects.go` and `daemon/commands.go` to
   say the client is disconnected.
4. **Shutdown order.** In `Daemon.Close()`, close and nil `d.clients`
   *before* `d.ops.Wait()`. The listeners stay open until after the wait, as
   today. `serve` and `serveEventsOnly` already reject new conns once
   `d.clients` is nil (`daemon.go:630`, 762). Closing the listeners early
   would make the socket go quiet while the state lock is still held, so a GUI
   Restart could spawn a replacement that fails with `ErrAlreadyRunning`. In-flight ops still complete their work; they only lose the
   reply, which a closing daemon could not deliver anyway. `serve` already
   rejects new conns once `d.clients` is nil.
5. **Handshake writes.** A helper `writeBounded(conn, t, v)` sets
   `SetWriteDeadline(now + clientWriteTimeout)` **immediately before** each
   pre-handshake write:
   - the version-mismatch and unknown-mode errors in `serve` (657/695);
   - `create_failed` (687);
   - the `serveEventsOnly` errors (747/779);
   - the `serveAttach` errors and WELCOME (1706-1744);
   - the `serveControl` WELCOME (955).
   The deadline is **not** set at accept, because ModeCreate runs a synchronous
   `git worktree add` before its WELCOME, which could outlast any fixed
   deadline set at accept time.
6. **GUI: reattach a dropped tile whose session is still alive.**
   - `ensureAttached` gets an `_attaching` in-flight guard, held across the
     await. It returns an outcome of `'attached' | 'deferred' | 'failed'`, and
     accepts `{ quiet }` to suppress the red `[attach failed]` text on
     timer-driven retries.
   - `needsReattach` is cleared **when `OpenSession` succeeds**, inside
     `ensureAttached`. The per-term backoff resets on
     `scrollback_replay_done`.
   - The `updated` handler's reset-and-reattach becomes gated on
     `needsReattach && alive && !attached && !_attaching`. A title or rename
     `updated` can no longer wipe a terminal mid-attach. That handler now uses
     the shared `reattachIfVisible(st)` helper.
   - `pty:disconnect` (non-closing) schedules `reattachIfVisible` with
     backoff: 500ms, doubling, capped at 5s. The timer does nothing when the
     tile is attached, attaching, not alive, closing, or hidden; hidden tiles
     reattach via switchTo.
   - On `'failed'` the timer reschedules. On `'deferred'` it does not, because
     the `_pendingAttach` re-entry (setPhase or resize) owns that path.
   - A dead-but-listed session gets WELCOME and then EOF, so the GUI backs
     off until `alive=false` arrives. This is acceptable, and the design doc
     notes it.
7. **Guard a pre-existing race the timer makes likelier.**
   `fanoutClose` sets `s.closed = true` under `s.mu`, and
   `SubscribeWithAtomicReplay` checks that flag under `s.mu`, returning an
   error without registering the sink. It must not check `done`, because
   `done` closes *after* `fanoutClose` and leaves a gap. Today an attach that lands between
   `fanoutClose` and Restart swapping `e.sess` (`registry.go:1387-1391`)
   registers a sink on a dead session, and nothing ever closes it.

No wire changes → **no `DaemonContract` bump** (old and new GUIs both recover
from EOF); PR carries the `daemon-contract-override` label.

### Files to change

1. `internal/daemon/daemon.go`:
   - Rewrite `frameSink` (queue, writer, `stop`, graceful `Close`), and add
     `defer sink.stop()` in `serveAttach`.
   - Add the `clientWriteTimeout` and `attachBacklogLimit` vars.
   - Add a `writeBounded` helper and use it at every pre-handshake write.
   - Give `serveControl.writeJSON` a deadline and close the conn on error.
   - Close the conn when the fan-out exits.
   - In `Close()`, close clients before `ops.Wait`.
   - Add a `d.createSession` func field, defaulting to `d.reg.Create`, as the
     test seam for slow creates.
   - Fix the `writeReplay` comment (1826-1838).
2. `internal/session/session.go`: add the `closed` flag (set in
   `fanoutClose`, checked in `SubscribeWithAtomicReplay`), and fix the
   `SubscribeWithAtomicReplay` doc comment (455-460).
3. Log wording only:
   - `internal/registry/events.go`, `ideas.go` and `projects.go`.
   - `internal/daemon/commands.go:78`.
   - The comment in `internal/registry/persist_logging_test.go:138`.
4. `cmd/hivegui/frontend/src/app/session-term.ts`:
   - `ensureAttached` gets the `_attaching` guard, an outcome return value,
     the `{quiet}` option, and clears `needsReattach` on success.
   - Add `reattachAttempts` and `reattachTimer`, and clear the timer on
     dispose.
5. `cmd/hivegui/frontend/src/app/events.ts`: add `reattachIfVisible`, gate
   the `updated` branch, add the `pty:disconnect` backoff, and reset the
   backoff on `scrollback_replay_done`.
6. `DESIGN.md`: a new hard rule, **"The daemon never blocks on a client."**
   Every write to a client conn is bounded by a deadline or a bounded queue,
   and no conn write happens under `Session.mu`. A client that stops reading
   is disconnected and must reconnect and re-snapshot.
7. `docs/design-docs/slow-client-policy.md`, plus a row in
   `docs/design-docs/index.md`.
8. `.changesets/stuck-client-disconnect.md`: `fixed`, `patch`.
9. The spec's Desired behavior, Success criteria and Non-goals (text below).

### New files

- `internal/daemon/framesink_test.go` — unit tests over `net.Pipe`.
- `internal/daemon/slow_client_test.go` — daemon-level integration tests.
- `cmd/hivegui/frontend/test/dom/pty-disconnect-reattach.test.ts`.
- `docs/design-docs/slow-client-policy.md`.
- `.changesets/stuck-client-disconnect.md`.

### Tests

`internal/daemon/framesink_test.go` (all over a zero-buffer `net.Pipe`):
- `TestFrameSinkWriteNeverBlocksOnStalledReader` — nobody reads. With a
  shrunk limit, `Write` calls past the limit all return, asserted with a
  generous 5s bound so it doesn't flake under `-race`. Past the limit `Write`
  returns an error and the conn is closed.
- `TestFrameSinkCopiesInput` — write a buffer, mutate it, write again. The
  reader sees both original payloads.
- `TestFrameSinkReplayBudgetDrains` — after a large replay has been written,
  the live limit is back to base, so a live backlog above the base limit
  disconnects.
- `TestFrameSinkStopWakesIdleWriter` — attach, detach while idle, and the
  goroutine count returns to baseline (polled with a timeout).
- `TestFrameSinkWriteDeadlineDisconnects` — `clientWriteTimeout` = 50ms, one
  `Write`, nobody reads. The conn closes within about 1s, and a later `Write`
  errors.
- `TestFrameSinkPreservesReplayThenLiveOrder` — enqueue live A, then replay R
  (chunked), then live B. The reader decodes exactly A, Begin, R chunks, Done,
  B.
- `TestFrameSinkReplayLargerThanLimitIsAccepted` — a replay bigger than
  `attachBacklogLimit` is delivered in full, not dropped.
- `TestFrameSinkCloseFlushesQueuedOutput` — enqueue N frames, then `Close`.
  The reader gets all N and then EOF.

`internal/daemon/slow_client_test.go` (real daemon, `startTestDaemon`,
`skipOnWindows` where the existing attach tests do):
- `TestStalledAttachClientDoesNotStallSession` — client A attaches and never
  reads; client B attaches and reads. The session floods more than the
  backlog limit plus socket buffers, then prints an END marker. B sees END
  within 10s, and A's conn is closed by the daemon (a read drains, then EOF).
  **This fails on today's code:** B never sees END because `deliver` is stuck
  on A.
- `TestStalledAttachClientDoesNotBlockNewAttach` — while A is stalled mid
  flood, a fresh attach's WELCOME and replay-done arrive within the bound.
  Today it hangs on `s.mu` in `SubscribeWithAtomicReplay`.
- `TestStalledControlClientIsDisconnected` — `serveControl` over `net.Pipe`
  (pattern from `TestControlSubscribesBeforeWelcome`) with a shrunk timeout.
  After WELCOME the client stops reading and events are published. The conn
  closes within the bound, and `serveControl` returns.
- `TestDroppedSubscriptionClosesControlConn` — `net.Pipe` with a large
  timeout. The client stops reading and at least 9 client commands are
  published (commands-hub buffer is 8), so the subscription is dropped. The
  client then reads: it gets the buffered frames, then EOF. Today it gets
  no EOF (read blocks until the test timeout).
- `TestCloseDoesNotHangOnStuckClientReply` — a large timeout, so only the
  reorder can free it. On a daemon from `startTestDaemon`, run
  `go d.serve(ctx, pipeServer)` so the conn is tracked in `d.clients`. The
  client sends HELLO, reads WELCOME, sends LIST_WORKTREES, and then stops
  reading. The snapshot write blocks on the zero-buffer pipe, and the runOp
  (1510) queues on `connMu`. `d.Close()` returns within 2s. Today it hangs on
  `ops.Wait`.
- `TestSlowCreateStillGetsWelcome` — `clientWriteTimeout` = 100ms, and
  `d.createSession` wraps the real create with a 300ms sleep. ModeCreate
  still receives WELCOME.
- `TestSubscribeAfterFanoutCloseFails` (session package) — call
  `fanoutClose()` directly, before `done` closes. `SubscribeWithAtomicReplay`
  must return an error and register no sink. A `done`-based guard fails this
  test.

`cmd/hivegui/frontend/test/dom/pty-disconnect-reattach.test.ts` (fake timers,
drives the real `pty:disconnect` handler captured from the mocked `EventsOn`):
- An alive, visible session calls `ensureAttached` after 500ms.
- A closing phase never reattaches.
- A dead session never reattaches.
- If the `updated(alive)` event arrives first, the timer no-ops (called once,
  not twice).
- Repeated disconnects back off: 500, 1000, 2000 ms.
- If the first `OpenSession` rejects, the next tick retries and succeeds, and
  the timer-driven failure writes no red text.
- An `updated(alive)` that arrives between a successful attach and
  replay-done does **not** call `term.reset()`.
- A `'deferred'` outcome (phase not ready) schedules no retry: after 10s of
  fake time, `OpenSession` has not been called again.

### Verification

- `GOTOOLCHAIN=go$(sed -n 's/^go //p' go.mod) go test -race ./internal/daemon/ ./internal/session/ ./internal/registry/`
- `scripts/test.sh go unit dom e2e`
- `(cd cmd/hivegui/frontend && npm run typecheck && npx biome ci .)`
- `(cd cmd/hivegui/frontend && npm run test:e2e:real)`. This is isolated by
  the harness. ws-bridge forwards attach frames synchronously, so the flood
  specs are where a too-tight limit would show up.
- `for os in darwin linux windows; do GOOS=$os staticcheck ./... && GOOS=$os go vet ./... ; done`
- Revert-check: run the two integration tests with the old `frameSink`, the
  old `Close` order and the old fan-out to confirm they fail
  (on a scratch branch or a temporary WIP commit, never `git stash`).
- `scripts/check-daemon-contract.sh origin/main HEAD` is expected to fail
  without the override label. Record that as a deliberate override claim.

### Risks

- **Limits.** 10s write timeout and 8 MiB live backlog. The GUI's Go attach
  reader forwards straight to Wails events and is a fast reader, so healthy
  clients never get near either. Both are `var`s, so they are easy to tune.
- **Memory.** The worst case per attach is about 16 MiB (a replay plus the
  backlog) before disconnect. Acceptable.
- **Goroutine per attach.** The writer exits when the conn closes. The test
  asserts no leak: a goroutine count stays stable after detach.
- **Restart race.** An attach to a session that has already closed out now
  errors right after WELCOME. The GUI sees EOF and backs off, and the
  `updated(alive)` event then reattaches cleanly.
- Ruled out: a global writer pool (more complex, no gain) and dropping frames
  instead of disconnecting (it silently corrupts the terminal).

## Second opinion

- **Round 1 (revise, confidence 8), 4 must-fix items, all applied:**
  1. A handshake deadline set at accept broke slow ModeCreate. The deadline
     is now set right before each write.
  2. The idle writer goroutine leaked. It now has an explicit `stop` path.
  3. The Close test could not have exercised the fix. It now runs through
     `d.serve`, so the conn is tracked.
  4. A failed GUI reattach stranded the tile. Failed attaches now retry.

  Nice-to-haves applied:
  - copy the input bytes;
  - a replay budget that drains;
  - limits captured when the sink is created;
  - stale comments and index row;
  - e2e, e2e-real, typecheck and biome in verification;
  - no `git stash`;
  - a generous timing bound in the stall test;
  - the dead-session attach guard.
- **Round 2 (revise, confidence 8), 3 must-fix items, all applied without a
  third review, per the loop's one-rerun rule:**
  1. Keeping `needsReattach` set until replay finished let an `updated`
     event wipe the terminal. The flag is now cleared when `OpenSession`
     succeeds, and the `updated` handler is gated on `!attached &&
     !_attaching`.
  2. `ensureAttached` had no in-flight guard and no outcome. It now has an
     `_attaching` flag and returns attached / deferred / failed, and a
     deferred outcome does not trigger a retry.
  3. The done-guard now keys on a `closed` flag that `fanoutClose` sets, not
     on `done`.

  Nice-to-haves applied: listeners are no longer closed early (that risked
  `ErrAlreadyRunning` on Restart), retry failures are quiet, the stale
  Files list and risk text are fixed, and the create seam now has a name.

## Decision log

- **2026-09-25** — Triage: bug, M, P1. Why: it freezes live agent sessions and
  hangs shutdown, and it blocks #460.
- **2026-09-25** — Attach uses a bounded queue plus a write deadline, not a
  deadline alone. Why: a deadline alone still freezes the agent for the whole
  deadline while `s.mu` is held. The operator asked for "the safe and proper
  way".
- **2026-09-25** — GUI auto-reattach ships in this PR. Why: the operator
  chose it. Without it, a daemon-dropped tile stays detached until the user
  switches sessions.
- **2026-09-25** — No `DaemonContract` bump; the PR carries the
  `daemon-contract-override` label. Why: there is no wire change, and old and
  new GUIs both recover from EOF.
- **2026-09-25** — Plan approved in chat, after the HTML page timed out with
  no response.
- **2026-09-25** — `clientWriteTimeout` / `attachBacklogLimit` became
  `atomic.Int64` instead of plain vars. Why: tests shrink them while daemon
  goroutines from the same test still read them, and `-race` flagged it.
- **2026-09-25** — `runOp` now takes `opsMu` and refuses work once `Close`
  starts draining. Why: `-race` flagged `ops.Add` from a live read loop
  racing `ops.Wait` in `Close`. That is a WaitGroup misuse, made reachable
  by the new test and latent before it.
- **2026-09-25** — `ensureAttached` shares one in-flight dial (`_attachInFlight`)
  instead of using a boolean guard. Why: a concurrent caller then gets the real
  outcome, so the backoff only re-arms on an actual failure.

## Progress

- **2026-09-25** — Ingested, triaged, researched.
- **2026-09-25** — Implemented. `go`/`unit`/`dom`/`e2e` layers green (635 unit,
  1021 dom, 468 e2e), `e2e-real` 28 passed. Revert-checked: each daemon fix
  fails its own test when undone (sink, Close order, fan-out close, control
  deadline, session guard), and 4 of 9 reattach dom tests fail without the
  backoff scheduling.
- **2026-09-25** — Review loop converged at iter 3 (APPROVE, 0 threads).
  Iter 1 escalated two design points (repeat-replay cap, quiet-flag join).
  The operator chose the recommended option for both, and both were
  implemented alongside two bot-found GUI races: a disconnect arriving
  mid-dial, and an alive-path failure that never re-armed the retry. After
  the APPROVE, a test-only commit added the one remaining MINOR:
  `TestFrameSinkSecondQueuedReplayUnderCapGetsNoAllowance`, revert-checked.

## Open questions

## PR convergence ledger

- **2026-09-25 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: aee505ab0a4972a40b9a640370c4e88d5404a4b785a665c0c5f5b9ccee21fcc9; threads_open: 3; action: escalated:risky-fix-needs-human-decision; head_sha: 3dec9dd1.
- **2026-09-25 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: b21bf5e8d7a949eb01c12111d4009f4553db0b6a8c131c9d4cb53bf8c8bede28; threads_open: 0; action: autofix+push; head_sha: 6eb70e21.
- **2026-09-25 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 60b266b5.

## Gate verdict

- **2026-09-25** — verdict: PASS; phase: —; checks: 19 passed / 0 failed / 0 followups; followups: none; one-line: all six success criteria demonstrated by passing tests, four non-goals respected, docs and changeset accurate.
  - 2026-09-25 dimensions:
    - acceptance — PASS — 4 stall/Close/dropped-subscription tests pass under -race; 12 reattach DOM tests pass; no diff under internal/wire or internal/buildinfo
    - non-goals — PASS — registry/commands changes are wording only; no throttling, no plugin work; new attach tests skip on Windows
    - doc accuracy — PASS — changeset valid (regression_of: declared-absent); DESIGN.md rule and slow-client-policy.md match the code; no stale comments; generated files untouched; README and features.json correctly unchanged

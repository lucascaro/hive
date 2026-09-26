# Slow-client policy: the daemon never blocks on a client

- **Issue:** #461
- **Code:** `internal/daemon/clientwrite.go`, `serveControl` / `serveAttach` / `Close` in `internal/daemon/daemon.go`, `SubscribeWithAtomicReplay` in `internal/session/session.go`, `reattach` / `scheduleReattach` in `cmd/hivegui/frontend/src/app/events.ts`

## The rule

The daemon's writes to a client connection are bounded, and a client that
stops reading is disconnected. Every client already reconnects on EOF and
starts over from a fresh snapshot, so hanging up is how a lagging client
gets back to a correct view. Waiting for it, or leaving it connected without
events, never does that.

The rule matters most for plugins (#460). Once the socket is open to code we
don't ship, "a client stopped reading" becomes a normal event rather than a
rare bug.

## What used to go wrong

| Path | Failure |
|---|---|
| Attach | `session.deliver` wrote each PTY chunk to every sink synchronously while holding `Session.mu`. One full socket stopped the PTY drain, so the agent froze, other viewers of that session froze, and new attaches hung. |
| Control | Writes serialize on a per-connection `connMu` and had no deadline. A stuck client parked its fan-out goroutine, its read loop, and every runOp replying to it. |
| Shutdown | `Close()` waited on `d.ops` before closing client connections, so an op blocked writing to a stuck client hung the whole shutdown. |
| Dropped subscription | When a registry listener overflowed, the daemon closed its channel and the fan-out goroutine returned. The connection stayed up, still answering requests but sending no more events, and the client had no way to tell. |

## Mechanisms

- **Attach: a bounded queue and a writer goroutine** (`frameSink`).
  - `Write` and `writeReplay` run under `Session.mu`, so they only copy,
    encode and enqueue. Neither touches the socket.
  - One writer goroutine per attach drains the queue, and each write has a
    deadline.
  - Replay and live output share one FIFO. Both are enqueued under
    `Session.mu`, so the atomic-replay ordering holds.
  - The queue is capped at `attachBacklogLimit` (8 MiB) plus whatever replay
    is still queued. A replay can be as large as the scrollback ring and is
    always accepted; its allowance shrinks as it drains. Live output past the
    cap disconnects the client.
  - `Close()` is graceful and drains the queue, so the last output before a
    PTY exits still arrives. `stop()` hangs up immediately.
    `defer sink.stop()` in `serveAttach` means the writer never outlives the
    connection.
- **Every other write: a deadline, and close on failure** (`writeBounded`).
  - The deadline is set immediately before each write, not when the
    connection is accepted. ModeCreate runs a synchronous
    `git worktree add` before its WELCOME, and an accept-time deadline
    could expire during a slow create.
  - A failed or timed-out write may have left half a frame on the wire, so
    the connection is closed. The writers queued on `connMu` behind it then
    fail fast.
- **The fan-out closes the connection when it exits**, whatever the reason:
  a dropped subscription, a write error, or teardown. A dropped client
  therefore reconnects instead of drifting out of sync.
- **Shutdown** hangs up on clients *before* `ops.Wait()`.
  - In-flight ops still finish their work; they just lose the reply.
  - `runOp` refuses new work once draining starts. `opsMu` orders
    `ops.Add` against `ops.Wait`.
  - The listeners stay open until after the wait. Closing them early
    would make the socket go quiet while the state lock is still held,
    and a GUI Restart that spawns a replacement at that point would get
    `ErrAlreadyRunning`.
- **Session guard.** `fanoutClose` sets `fannedOut` under `Session.mu`, and
  `SubscribeWithAtomicReplay` refuses once it is set. Without that, an
  attach arriving mid-Restart would register a sink that nothing ever
  closes. The guard can't key on `done`, because `done` closes *after*
  `fanoutClose`.

## Limits

| Knob | Default | Why |
|---|---|---|
| `clientWriteTimeout` | 10s | A reading client takes a frame in microseconds. Ten seconds without one means it has stopped reading. |
| `attachBacklogLimit` | 8 MiB | Far above anything a reading client accumulates. The worst-case memory per attach before a disconnect is about 16 MiB (a full replay plus the backlog). |

Both are atomics so tests can shrink them. They're not user settings: nothing
healthy comes near either one.

## GUI side

- **Control disconnect.** Nothing new is needed: `reconnectControl()`
  redials with backoff, and the daemon pushes a fresh snapshot.
- **Attach disconnect.** This needed new code.
  - Before, `pty:disconnect` only set `needsReattach`. The tile reattached on
    the next `session:event(updated, alive=true)`, which Restart Session
    sends and a daemon hang-up does not.
  - `pty:disconnect` now also arms a backoff timer: 500 ms, doubling, capped
    at 5 s, and reset when a replay completes.
  - The timer and the alive event share `reattach()`. Whichever runs second
    finds the tile attached or attaching and stops.
  - `ensureAttached` shares one in-flight dial and reports its outcome
    (`attached` / `deferred` / `failed`). Only `failed` re-arms the timer;
    `deferred` belongs to the setPhase or resize re-entry.
  - `needsReattach` is cleared on a successful dial. A failed dial therefore
    leaves the flag set for the next attempt, and an `updated` event
    arriving mid-attach can't wipe the terminal.
  - A dead session that is still listed accepts the attach and then hangs up
    at once, so the tile backs off until `alive=false` arrives.

## Alternatives rejected

- **A deadline alone on attach writes.** A stuck client would still freeze
  the agent, and every other viewer of that session, for the whole deadline
  while `Session.mu` is held.
- **Dropping frames instead of disconnecting.** That silently corrupts the
  terminal. A disconnect costs one replay and leaves the client correct.
- **A shared writer pool.** More moving parts and no benefit over one
  goroutine per attach.

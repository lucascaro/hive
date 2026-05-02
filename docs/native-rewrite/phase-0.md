# Phase 0 — Decision & Spike

**Goal:** de-risk the two scariest pieces of the native rewrite *before* committing real engineering. Both spikes are throwaway. Anything that survives must be re-implemented properly in Phase 1.

**Duration:** 1 week (5 working days). Hard cap. If it slips, that's signal — stop and re-evaluate the plan.

**Exit gate:** both spikes meet acceptance criteria on **macOS and Linux**, and a Windows cross-build at minimum compiles. Framework choice is then locked. If either spike feels ugly or fights the framework, halt and reconsider the plan (likely fallback: native PTYs but keep Bubble Tea).

---

## Spike A — `hived` proto: PTY ownership + reattach

**Question being answered:** can a Go daemon own a PTY, survive client disconnect, and let a fresh client reattach with intact state?

If the answer is no or messy, the entire architecture is wrong.

### Scope

A throwaway program in `spikes/spike-a-daemon/`:

- Single binary `hived-spike` that on first run forks itself into the background and listens on a Unix socket (`$XDG_RUNTIME_DIR/hived-spike.sock`, fallback `/tmp/hived-spike-$UID.sock`).
- On startup the daemon spawns one shell (`$SHELL` or `/bin/bash`) attached to a PTY using `github.com/creack/pty`.
- A second binary `hivec-spike` connects to the socket. It:
  - Puts local terminal in raw mode.
  - Pipes stdin → socket → daemon → PTY master.
  - Pipes PTY master → daemon → socket → stdout.
  - Sends a window-resize message on SIGWINCH.
- Quit `hivec-spike` (Ctrl+\\), the daemon and shell keep running. Reconnect — same shell, same shell state (same `pwd`, env vars set in the prior session).
- Daemon keeps a 4 KB ring buffer of recent PTY output and replays it on attach so the new client sees recent context.

### Out of scope (deliberately)

- Multiple sessions
- Authentication
- Pretty protocol; raw bytes + a tiny length-prefixed control channel is fine
- Persistence across daemon restart (only client restart)
- Windows (note feasibility only)

### Acceptance criteria

1. Start daemon, attach `hivec-spike`, run `cd /tmp && export FOO=bar`.
2. Quit client. `ps` shows daemon and child shell still alive.
3. Reattach. `pwd` prints `/tmp`. `echo $FOO` prints `bar`.
4. Resize the terminal mid-session — shell sees the new size (`stty size` reports it).
5. Run `vim`, type some text, quit client mid-edit, reattach — vim is on the same screen. (Replay buffer is enough for "looks fine"; full restoration is Phase 1.)
6. macOS + Linux both pass.

### Success signals to watch for

- Code is **boring**. If the IPC layer needs goroutine gymnastics or you're fighting `pty` package quirks, that's a tell.
- Latency feels native (no perceptible input lag locally).

### Failure signals (halt the plan)

- Reattach loses meaningful state (e.g. terminal is corrupted, alt-screen apps unrecoverable).
- PTY package has cross-platform gotchas we can't work around.
- Resize semantics are ambiguous.

---

## Spike B — Wails + xterm.js round-trip

**Question being answered:** can a Wails app embed xterm.js, pipe bytes to a Go-side PTY, and feel like a real terminal?

If xterm.js or Wails is going to fight us, better to know now.

### Scope

A throwaway Wails project in `spikes/spike-b-wails/`:

- `wails init` with vanilla JS frontend (avoid framework setup tax for a spike).
- Frontend: full-window xterm.js instance + xterm-addon-fit.
- Backend: on app start, spawn a shell on a PTY (`creack/pty` again).
- Bridge:
  - Frontend → backend: forward keystrokes via Wails binding `WriteStdin(data string)`.
  - Backend → frontend: emit Wails events with PTY output chunks; frontend writes them into xterm.js.
  - Resize: frontend computes cols/rows from xterm fit addon, calls `Resize(cols, rows)`.
- Quit and relaunch the GUI — fresh shell each time is fine; persistence is Spike A's question.

### Out of scope

- Connecting to `hived` (separate concern; Phase 1 unifies)
- Sidebar, multiple sessions
- Theming, fonts, polish
- Scrollback persistence

### Acceptance criteria

1. App launches, terminal is interactive within ~1s.
2. Run `vim`, `htop`, `less` on a long file — rendering is correct, no visual artifacts.
3. Resize the window — terminal reflows; `stty size` and `vim`'s view both update.
4. Run `cat /dev/urandom | head -c 1M | xxd` — output streams smoothly, no UI hang.
5. Mouse selection + copy works (xterm.js default).
6. Unicode + emoji render correctly.
7. macOS + Linux both pass. Windows: cross-compile succeeds; smoke test if hardware available.

### Success signals

- xterm.js handles the hard stuff (escape sequences, scrollback, mouse) with no custom parsing.
- Wails IPC throughput is fine for `cat` of a megabyte.
- Bundle size and cold start are reasonable (< 100 MB binary, < 2s startup).

### Failure signals (halt the plan)

- xterm.js misrenders common TUI apps.
- Wails event throughput chokes on heavy output (look for backpressure handling — chunk + drop or chunk + buffer, decide here).
- Windows path is clearly broken.

---

## Day-by-day budget

| Day | Work                                                                 |
|-----|----------------------------------------------------------------------|
| 1   | Spike A skeleton: daemon, socket, single PTY, raw passthrough.       |
| 2   | Spike A: detach/reattach, resize, replay buffer, acceptance run.     |
| 3   | Spike B skeleton: Wails init, xterm.js, PTY bridge.                  |
| 4   | Spike B: resize, throughput stress, Unicode/mouse, acceptance run.   |
| 5   | Cross-platform check (Linux + Windows cross-build), write decision.  |

If a spike isn't passing acceptance by end of its allotted day, that's a signal — extend by at most one day, then halt and write up what was learned.

---

## Deliverables

1. **`spikes/spike-a-daemon/`** — throwaway code, ignored by main build, with a one-page README documenting how to run and what was learned.
2. **`spikes/spike-b-wails/`** — same.
3. **`docs/native-rewrite/phase-0-report.md`** — written at end of Phase 0:
   - Did each spike pass? Which acceptance criteria failed/were partial?
   - Surprises (good and bad).
   - Final framework decision (Wails confirmed, or pivot to X).
   - Wire protocol v0 sketch informed by Spike A learnings.
   - Any plan revisions for Phase 1+.
4. **Decision:** GO / NO-GO / PIVOT. Documented in the report. If GO, Phase 1 starts on the next working day.

## Rules

- **No reuse pressure.** Both spikes are written to be deleted. Resist tidying or generalizing.
- **No feature creep.** If you find yourself adding a sidebar to Spike B, stop.
- **Daily timebox.** End each day with the day's work either passing or written up as a blocker — don't let one bad afternoon eat the week.
- **Test on real hardware.** Both macOS and a Linux box (or VM). Don't trust CI alone for UX questions.

## What Phase 0 explicitly does *not* answer

- Multi-session protocol shape (Phase 1).
- How agents launch inside sessions (Phase 3).
- How splits/tabs render (Phase 4).
- Persistence across **daemon** restart, not just client restart (Phase 1+).
- Auto-start of daemon at login (Phase 6).

These are deferred on purpose. Phase 0 only answers "is the foundation viable?"

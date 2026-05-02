# Phase 1 — Single persistent session, GUI client

**Status:** In progress
**Branch:** `main` (was `silent-light`)
**Inputs:** `phase-0-report.md` (framework lock + protocol-v0 sketch)

## Goal

End-to-end native stack:

1. Open vim in the Hive GUI.
2. Quit the GUI.
3. Relaunch the GUI.
4. **Vim is still on screen, cursor in the right place.**

That requirement alone forces every architectural decision in this
phase: a long-lived `hived` daemon owning the PTY, a real wire protocol
between daemon and GUI, and disk-backed scrollback so a new GUI session
can paint a meaningful screen on attach.

## Out of scope (defer to later phases)

- Multiple sessions (Phase 2)
- Sidebar UI, session naming/coloring (Phase 2)
- Agent integration (Phase 3)
- Splits / tabs / grid (Phase 4)
- launchd / systemd / Task Scheduler integration (Phase 6)
- Code signing, installers (Phase 6)

Phase 1 is one window, one shell, persistent across GUI restart, on
macOS + Linux. Windows is a stretch — the daemon already cross-builds
(per Phase 0); the GUI is gated on Wails-on-Windows availability.

## Architecture

```
+------------------------+        Unix socket          +-------------------------+
| Hive GUI (Wails)       |    JSON-framed protocol     | hived (Go daemon)       |
| - xterm.js             |  ─────────────────────►     | - owns PTY              |
| - sends keystrokes     |  ◄─────────────────────     | - in-memory live ring   |
| - renders pty bytes    |                             | - disk-backed scrollback|
| - handshake, attach    |                             | - single session (P1)   |
+------------------------+                             +-------------------------+
            │                                                       │
            │ on launch: if no socket, spawn detached hived         ▼
            └─────────────────────────────────────────►       child shell
```

**Lifecycle invariant:** the daemon outlives the GUI. The GUI is just
the latest client. Closing the GUI closes its socket; the daemon stays
running.

## File layout (new)

```
cmd/
  hived/                    daemon binary
    main.go
  hive/                     GUI binary (Wails)
    main.go, app.go
    frontend/               xterm.js + bindings to App
internal/
  wire/                     protocol v0
    frame.go                framing (1 B type + 4 B BE len + payload)
    control.go              JSON control messages (HELLO, etc.)
    wire_test.go
  session/
    session.go              one Session = one PTY + state
    scrollback.go           in-memory live ring (replayed on attach)
    persist.go              disk-backed scrollback file
  daemon/
    daemon.go               socket listener, client handling
    socket.go               platform-aware socket path + dial/listen
spikes/                     phase-0 reference code, untouched
```

The Phase-0 spikes stay where they are as documentation. Production
code does **not** import them.

## Wire protocol v0

Frame:

```
+-------+-------------+--------------+
| type  | len (BE u32)| payload      |
| 1 B   | 4 B         | len B        |
+-------+-------------+--------------+
```

Cap on payload: 1 MiB (drop connection on overflow).

| Code | Name      | Direction | Payload                                          |
|------|-----------|-----------|--------------------------------------------------|
| 0x01 | HELLO     | C → S     | JSON `{ "version": 0, "client": "hive/0.x" }`    |
| 0x02 | WELCOME   | S → C     | JSON `{ "version": 0, "session_id": "...", "cols": …, "rows": … }` |
| 0x03 | DATA      | both      | raw bytes (PTY stdin / stdout)                   |
| 0x04 | RESIZE    | C → S     | JSON `{ "cols": …, "rows": … }`                  |
| 0x05 | EVENT     | S → C     | JSON `{ "kind": "scrollback_replay_done" \| "exit" }` |
| 0x06 | ERROR     | S → C     | JSON `{ "code": "...", "message": "..." }`       |

Why JSON for control, raw for DATA: DATA is volume-heavy and would pay
a parse tax for nothing. Control frames are rare and benefit from
self-description and forward-compatibility.

**Handshake.** Client connects → sends HELLO → server replies WELCOME →
server immediately starts streaming scrollback as DATA frames → server
sends EVENT `{kind:"scrollback_replay_done"}` so the client knows the
replay is over → server begins live-streaming new PTY output as DATA.
Client may send RESIZE before or after handshake; server applies it
once the PTY is up.

**Versioning.** A WELCOME with a version below the client's minimum is
a fatal mismatch — client closes and shows an error. Higher versions
are forward-compatible if both peers ignore unknown JSON fields.
Frame *types* may only be added monotonically; never reused.

## Scrollback

Two layers:

1. **Live ring (in-memory)** — last ~256 KiB of raw PTY output. Cheap
   to maintain; replayed on attach for "look the same" feel.
2. **Disk-backed log (append-only file)** — under
   `~/.local/state/hive/sessions/<id>/scrollback.log` on Linux,
   `~/Library/Application Support/Hive/sessions/<id>/scrollback.log`
   on macOS, `%LOCALAPPDATA%\Hive\sessions\<id>\scrollback.log` on
   Windows. Truncated when it exceeds 10 MiB (rolled to `.old`).
   The disk log is what survives a daemon restart (Phase 1 first cut
   does not implement daemon-restart resume; the file exists for
   forensics + Phase 2).

On attach, the daemon replays whichever is larger (within budget):
the live ring or the tail of the disk log up to ~256 KiB. For Phase 1,
in-memory live ring is enough to satisfy "vim survives GUI restart".

## Daemon lifecycle (Phase 1 minimum)

- `hived` binary takes flags: `--socket <path>` (default platform-aware),
  `--shell <path>` (default `$SHELL` or platform default).
- Single-instance: socket-bind acts as the lock. Second `hived` exits.
- Foreground process (Phase 1). The GUI launches it with
  `cmd.Start()` plus platform-specific detach (setsid on Unix,
  `CREATE_NEW_PROCESS_GROUP` on Windows) so that GUI quit does not
  cascade.
- Daemon logs to `~/.local/state/hive/hived.log` (or platform-equivalent).
- No daemon-side keep-alives in Phase 1; if the daemon crashes, the
  user re-launches the GUI which re-spawns it (new shell, no state).

## Milestones (within Phase 1)

| # | Goal | Done when |
|---|------|-----------|
| 1.1 | `internal/wire` package | unit tests pass; HELLO/WELCOME round-trip |
| 1.2 | `cmd/hived` skeleton | bind socket, accept connection, complete handshake, echo DATA back |
| 1.3 | PTY plumbed in | type into a netcat-like probe → bytes reach the shell, output streams back |
| 1.4 | Live scrollback ring + replay | reattach replays last 256 KiB before live stream resumes |
| 1.5 | `cmd/hive` (Wails) talks to daemon | xterm.js renders a real shell session via the daemon |
| 1.6 | GUI auto-spawns daemon | first-run launches daemon as detached child |
| 1.7 | Disk-backed scrollback | log file written; replay falls back to disk tail if ring is empty |
| 1.8 | **Acceptance: vim survives GUI restart** | E2E test passes on macOS + Linux |

Each milestone is a separate commit (or PR). Milestones 1.1–1.4 are
backend-only and unit-testable end-to-end; 1.5+ require human-in-the-
loop GUI verification.

## Acceptance test (E2E)

Manual, run on macOS and Linux:

1. Build: `go build ./cmd/hived && cd cmd/hive && wails build`
2. Launch the GUI. Daemon auto-starts.
3. Run `vim hello.txt`, type `Hello, world!`, leave cursor on the comma.
4. Cmd-Q the GUI. Confirm `hived` is still running (`pgrep hived`).
5. Relaunch the GUI.
6. **Pass criteria:**
   - vim is on screen with `hello.txt` open
   - the typed text is visible
   - cursor is on the comma (or close — within one cell)
   - typing `iX<Esc>` inserts an `X`, confirming live wiring works
7. Kill `hived` manually. Relaunch GUI — gets a fresh shell. (Phase 1
   does not promise daemon-restart resume.)

## Risks (carried from Phase 0 plus new)

| Risk | Mitigation |
|---|---|
| Wails event throughput choked by JSON-encoded control + raw DATA mixing | Only DATA is hot-path; control is rare. Spike B already exercised raw DATA throughput. |
| Replay storm on attach (sending 256 KiB DATA in one frame) | Chunk replay into 16 KiB frames; emit `scrollback_replay_done` after the last chunk. |
| Daemon-orphan when GUI never re-launches | Phase 1 accepts this. Phase 6 adds proper service supervision. |
| Disk scrollback fills disk on a chatty session | 10 MiB cap with rotation; revisit in Phase 2. |
| GUI auto-spawn races on simultaneous launches | Use socket-bind as the lock — second daemon exits cleanly. |

## What this unblocks

After Phase 1, the architecture is real and the rest of the plan is
filling in features on top of a working foundation:
- Phase 2 (multi-session) becomes a protocol extension + UI work.
- Phase 3 (agents) is process-spawn-with-extra-env on top of an
  existing session API.
- Phase 4 (layout) is GUI work; the daemon barely changes.

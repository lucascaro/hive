# Phase 2 — Multiple sessions + sidebar

**Status:** In progress
**Branch:** `main` (was `silent-light`)
**Inputs:** `phase-1.md`, `phase-1` working code (commits `403523c`, `0a39cb1`)

## Goal

End-to-end multi-session experience that survives GUI restart:

1. Open 5 sessions, give each a name and color, run something distinct
   in each (vim in one, claude in another, a long-running tail, etc.).
2. Quit the GUI.
3. Relaunch. **All 5 sessions are listed in the sidebar with their
   names, colors, and scrollback intact.** Click any one to attach.

This is where the rewrite starts paying for itself — clean session
lifecycle, no `tmux send-keys` workarounds, real per-session metadata
the daemon owns.

## Out of scope

- Splits / tabs / grid layout (Phase 4)
- Agent integration (Phase 3)
- Disk-backed scrollback for daemon-restart resume — still deferred
  (in-memory ring is sufficient for "survives GUI restart")
- launchd / systemd / Task Scheduler (Phase 6)

## Wire protocol v1

Phase 1 shipped v0 with auto-attach. v1 makes attachment explicit and
adds control frames for managing sessions. Protocol version bumps to
1; the existing hivegui must update its HELLO accordingly. We do not
keep v0 compatibility — only one client exists.

### Connection modes

A connection chooses its mode in the HELLO:

```json
{ "version": 1, "client": "hivegui/0.2", "mode": "control" }
```

```json
{ "version": 1, "client": "hivegui/0.2", "mode": "attach", "session_id": "abc..." }
```

```json
{ "version": 1, "client": "hivegui/0.2", "mode": "create", "create": { "name": "scratch", "color": "#7a3", "cols": 132, "rows": 41 } }
```

| Mode    | Daemon behavior                                                              |
|---------|------------------------------------------------------------------------------|
| control | Accepts LIST/CREATE/KILL/RENAME/UPDATE frames; never streams DATA            |
| attach  | Attaches to the named session: replay → live DATA stream                     |
| create  | Creates a new session with the given metadata, then enters attach behavior   |

`attach` to an unknown ID returns ERROR. `create` with a duplicate name
is allowed (names are user-editable, not unique keys).

### New frame types (added monotonically; type byte > 0x06)

| Code | Name             | Mode    | Direction | Payload                                            |
|------|------------------|---------|-----------|----------------------------------------------------|
| 0x07 | LIST_SESSIONS    | control | C → S     | `{}` (no fields yet)                               |
| 0x08 | SESSIONS         | control | S → C     | `{ "sessions": [SessionInfo, ...] }`               |
| 0x09 | CREATE_SESSION   | control | C → S     | `{ "name": "...", "color": "...", "cols":…, "rows":… }` |
| 0x0a | KILL_SESSION     | control | C → S     | `{ "session_id": "..." }`                          |
| 0x0b | UPDATE_SESSION   | control | C → S     | `{ "session_id": "...", "name?":"...", "color?":"...", "order?":N }` |
| 0x0c | SESSION_EVENT    | control | S → C     | `{ "kind": "added" \| "removed" \| "updated", "session": SessionInfo }` |

`SessionInfo`:

```json
{
  "id": "uuid",
  "name": "scratch",
  "color": "#7a3",
  "order": 0,
  "created": "2026-04-30T12:34:56Z",
  "alive": true
}
```

Control connections receive `SESSION_EVENT` whenever any session changes,
so a sidebar that holds an open control connection sees real-time
updates without polling.

### Existing frames in v1

- `HELLO` / `WELCOME` — payload extended (mode, session_id, etc.)
- `DATA`, `RESIZE`, `EVENT`, `ERROR` — unchanged

## Session metadata + persistence

The daemon owns the metadata. Each session's data lives at:

- macOS: `~/Library/Application Support/Hive/sessions/<id>/`
- Linux: `${XDG_STATE_HOME:-~/.local/state}/hive/sessions/<id>/`
- Windows: `%LOCALAPPDATA%\Hive\sessions\<id>\`

Files per session directory:

```
session.json    -- metadata: name, color, order, created
scrollback.log  -- (Phase 2 placeholder; populated when 1.7 lands)
```

A top-level `sessions/index.json` lists session order. The daemon
keeps the list authoritative in memory; the JSON files are written
on every metadata change (small, infrequent — synchronous fsync is
fine).

Phase 2 first cut keeps the in-memory ring as the actual scrollback;
the directory exists for forensics and Phase 1.7 resumption.

## File layout (delta from Phase 1)

```
internal/
  registry/                NEW
    registry.go            map[id]*entry, ordered slice, mutex
    persist.go             read/write session.json + index.json
    paths.go               platform state dir
  wire/                    extended (new frame types, mode field)
  session/                 unchanged
  daemon/                  rewritten dispatch: control vs attach mode
cmd/hivegui/
  app.go                   adds: ListSessions, CreateSession, KillSession,
                                  RenameSession, UpdateSessionColor,
                                  ReorderSessions, SwitchSession
  frontend/src/            sidebar + per-session xterm
    main.js                terminal manager (one xterm per session)
    sidebar.js             session list, rename, color picker
    style.css              layout
```

## GUI architecture

```
+---------+---------------------------------------+
| sidebar |  active session terminal              |
|         |  (xterm.js)                           |
| [scr]   |                                       |
| [edit]  |                                       |
| [logs]  |                                       |
| [+ new] |                                       |
+---------+---------------------------------------+
```

- Sidebar is a fixed left panel ~180px wide. Each item: a vertical
  color stripe (gradient) + name + small dot if alive. Selected
  item has heavier weight (per `feedback_session_color_ux`: color
  must remain visible when selected).
- Keyboard: ⌘1..⌘9 jumps to nth session; ⌘N new session; ⌘W kills
  active session; F2 / double-click renames inline; ⇧⌘↑/↓ reorders.
  Per `feedback_session_color_ux`: keybinds visible in sidebar
  tooltip / hint area.
- One `Terminal` instance per session, rendered in absolutely-
  positioned divs; only the active div has `display:block`. This
  preserves xterm's internal scrollback across switches.

## Connection model

Control connection: opened on GUI launch, kept alive for the lifetime
of the window. Receives SESSION_EVENT for live sidebar updates.

Per-session attach connection: opened on first switch to a session,
kept alive while the session is open in the GUI. Closing a session in
the UI closes its connection (the daemon-side session continues
running unless explicitly killed).

So a GUI showing 5 attached sessions has 6 connections to hived:
1 control + 5 attach. Cheap on a Unix socket.

## Milestones

| #   | Goal | Done when |
|-----|------|-----------|
| 2.1 | wire v1 frame types + mode-aware Hello/Welcome | unit tests round-trip every new frame |
| 2.2 | `internal/registry` package | tests for create/kill/rename/reorder + persistence |
| 2.3 | daemon: control-mode dispatch (LIST/CREATE/KILL/UPDATE) | E2E test issues each command, checks events |
| 2.4 | daemon: attach-mode = old single-session path, but keyed by id | E2E reattach test with two sessions |
| 2.5 | GUI sidebar (read-only): list + select; control conn + per-session conn | manual smoke; builds; selecting switches xterm |
| 2.6 | GUI sidebar (write): create / kill / rename / color / reorder | manual smoke for each |
| 2.7 | Persistence on disk (session.json + index.json) | kill daemon → restart → metadata returns (no scrollback) |
| 2.8 | **Acceptance: 5 named/colored sessions survive GUI restart** | manual on macOS + Linux |

2.1–2.4 are backend, autonomously testable. 2.5–2.8 are GUI work.

Daemon-restart resume of *scrollback* is still Phase 1.7 territory —
not a 2.x milestone. But 2.7 covers metadata-only resume so a daemon
restart at least gives the user back their named-session list (the
sessions themselves are gone; the user can clean up the index or we
mark them `alive: false`).

## Acceptance test (E2E, manual)

1. Build `hived` and `hivegui`.
2. Launch GUI. Default: one session named "main" (or whatever the
   daemon picks).
3. Create four more via ⌘N. Rename each:
   - "edit" (orange)
   - "logs" (blue) — run `tail -f /var/log/system.log` (or any chatty
     command)
   - "claude" (purple) — run `claude`
   - "vim" (green) — `vim hello.txt`, type, leave cursor mid-line
   - leave "main" (default color) running a `htop` or `top`
4. Quit GUI.
5. Confirm `hived` is still alive: `pgrep hived`.
6. Relaunch GUI.
7. **Pass criteria:**
   - Sidebar shows all 5 sessions in the same order
   - Names + colors match
   - Clicking each session attaches and shows scrollback
   - The vim session's cursor is on the line we left it (within ~1 cell)
8. Kill `hived` manually. Relaunch GUI. Sidebar shows the 5 names but
   each session is marked `alive: false`. New attach to a dead session
   either revives it (Phase 2) or surfaces an error and lets user
   re-create. **Decision (Phase 2 first cut):** dead sessions are
   archived — sidebar shows them dimmed; clicking offers to recreate
   under the same name/color.

## Risks

| Risk | Mitigation |
|------|------------|
| Many connections multiply Wails IPC chatter | Each connection is a goroutine; xterm.js writes are batched. Spot-check; revisit if profile shows pain. |
| Sidebar-driven concurrent kill while active connection is mid-replay | Daemon serializes per-session: kill closes the PTY, fan-out closes all sinks; client connection sees disconnect. |
| Metadata file corruption on crash | Atomic write: write to `*.tmp`, fsync, rename. |
| Order field skew if two clients reorder simultaneously | Last-write-wins; daemon assigns final order on any UPDATE that touches `order`. |

## What this unblocks

- Phase 3 (agents): a session is just a "spawn this command instead of
  $SHELL" via CREATE_SESSION's payload. All the lifecycle is already
  built.
- Phase 4 (layout): tabs/splits become a frontend-only concern. The
  daemon doesn't change.

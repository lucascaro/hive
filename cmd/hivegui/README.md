# hivegui

The Hive desktop client for Phase 1. A Wails app that hosts an
xterm.js terminal and connects to `hived` (the session daemon) over
a Unix socket. If `hived` isn't running, the GUI auto-spawns it as
a detached child.

See `docs/native-rewrite/phase-1.md` for the role of this binary.

## Build

Requires Go 1.22+, Node 18+, and the Wails CLI:

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

From the repo root:

```sh
go build -o build/hived ./cmd/hived
cd cmd/hivegui && wails build
```

Place the `hived` binary next to the `hivegui` binary so that the
auto-spawn lookup finds it. On macOS the GUI binary is at
`cmd/hivegui/build/bin/hivegui.app/Contents/MacOS/hivegui`; copy
`hived` into that same `MacOS/` directory. On Linux/Windows the
binaries live next to each other in `build/bin/`.

Override path explicitly with the `HIVED` env var if needed.

## Run

```sh
open cmd/hivegui/build/bin/hivegui.app   # macOS
./cmd/hivegui/build/bin/hivegui          # Linux
.\cmd\hivegui\build\bin\hivegui.exe      # Windows
```

A black window opens with a shell session in it. The status text in
the lower-right shows `connecting…` → `session <id> • replay` →
`session <id> • live`.

## Phase 1 acceptance test (manual)

1. Build and launch as above.
2. Run `vim hello.txt`, type `Hello, world!`, leave the cursor on the
   comma.
3. Cmd-Q the GUI. Confirm `hived` is still running:

   ```sh
   pgrep -lf hived
   ```

4. Relaunch the GUI.
5. Pass criteria:
   - vim is on screen with `hello.txt` open
   - the typed text is visible
   - cursor is on the comma (or within ~1 cell)
   - typing `iX<Esc>` inserts an `X`, confirming live wiring
6. Kill `hived` manually and relaunch the GUI — you get a fresh shell.
   (Phase 1 does not promise daemon-restart resume; see PLAN.md
   Phase 2+.)

## Architecture

```
hivegui (Wails)              hived
+-------------------+        +-------------------+
| App.Connect       |◄──────►| socket            |
| App.WriteStdin    |        | session.Session   |
| App.Resize        |        |   ↓ PTY           |
| event: pty:data   |        |   ↓ shell         |
+-------------------+        +-------------------+
        ▲                              │
        │ xterm.js                     ▼
                                 child shell (vim, claude, ...)
```

Frontend state machine:

- `replay` phase: every `pty:data` event is painted into xterm.js
  but treated as historical. (Today this is a no-op distinction;
  Phase 2 may suppress cursor flicker during replay.)
- `live` phase: same, but new events. The transition is the
  `pty:event` `kind=scrollback_replay_done` notification from the
  daemon.

## Known limitations (Phase 1)

- One session, one window. Multi-session is Phase 2.
- No sidebar, no naming, no reordering.
- Daemon-restart resume not supported; only GUI-restart resume.
- No agent integration. Phase 3.
- No splits, tabs, or grid. Phase 4.
- launchd / systemd / Task Scheduler integration is Phase 6 — for now
  the GUI auto-spawns the daemon on demand.

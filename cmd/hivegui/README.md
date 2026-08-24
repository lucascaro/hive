# hivegui

The Hive desktop client. A Wails app that hosts xterm.js terminals and
connects to `hived` (the session daemon) over a Unix socket. If `hived`
isn't running, the GUI auto-spawns it as a detached child.

See `DESIGN.md` for the role of this binary and the wire protocol it
speaks, and the root `README.md` for build and release instructions.

## Binary layout

`hived` must sit next to the `hivegui` binary so the auto-spawn lookup
(`locate.go`) finds it. On macOS that is
`cmd/hivegui/build/bin/hivegui.app/Contents/MacOS/`; on Linux and
Windows the two live together in `build/bin/`. `./build.sh` handles
this. Override the lookup with the `HIVED` env var.

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

Attach is two-phased: a `replay` phase, where `pty:data` events carry
the daemon's scrollback snapshot, then `live`, entered on the
`pty:event` `kind=scrollback_replay_done` notification from the daemon.

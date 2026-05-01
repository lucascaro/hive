# Hive

A native desktop app for managing multiple AI coding agent sessions —
Claude, Codex, Gemini, Copilot, Aider, plain shells — across projects.

> **This branch (`silent-light`) is the v2 native rewrite.** The legacy
> tmux-backed TUI lived on `main`; v2 ships its own daemon + GUI.
> See `docs/native-rewrite/` for design docs (PLAN.md plus per-phase
> notes).

## Status

**Alpha.** The v2 architecture is feature-complete for daily use; the
download is pre-release. Latest release:
[v2.0.0-alpha.1](https://github.com/lucascaro/hive/releases/tag/v2.0.0-alpha.1).

What works:
- Multi-session daemon (`hived`) — sessions persist across GUI restart
- Wails-based desktop GUI with xterm.js — full keyboard control,
  font scaling, dark theme
- Projects (name, color, working dir) — sidebar tree
- Agent launcher (Claude, Codex, Gemini, Copilot, Aider, shell)
- Grid view: per-project (⌘G) or all-sessions (⇧⌘G), spatial arrow nav
- Multi-window (⇧⌘N) — independent windows share the same daemon
- BEL → desktop notification + visual pulse on non-focused sessions

Not yet shipping: scrollback resume across daemon restart, splits
inside grid cells, workflows / agent teams, code signing.

## Build

Requires Go 1.22+, Node 18+, and the Wails CLI:

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Then from the repo root:

```sh
./build.sh                    # macOS .app at cmd/hivegui/build/bin/hivegui.app
./build.sh --open             # build then launch
./build.sh --zip --version v2.0.0-alpha.2   # also write release/<name>.zip
```

For Windows or Linux, build the GUI and the daemon separately:

```sh
# Windows (cross-build from macOS works)
( cd cmd/hivegui && wails build -platform windows/amd64 )
GOOS=windows GOARCH=amd64 go build -o cmd/hivegui/build/bin/hived.exe ./cmd/hived

# Linux (build natively)
( cd cmd/hivegui && wails build -platform linux/amd64 )
GOOS=linux GOARCH=amd64 go build -o cmd/hivegui/build/bin/hived ./cmd/hived
```

`hivegui` and `hived` must live in the same directory; the GUI
auto-spawns the daemon at startup.

## Layout

```
cmd/
  hived/           # session daemon
  hivegui/         # Wails GUI client
internal/
  wire/            # protocol v1 (frame format + JSON control messages)
  session/         # one PTY + scrollback
  registry/        # sessions + projects + persistence
  daemon/          # socket listener + dispatch
  agent/           # built-in agent launcher catalog
docs/
  native-rewrite/  # v2 design docs (PLAN.md, phase-{0..5}.md)
spikes/            # phase-0 reference code (kept for posterity)
build.sh           # macOS universal build
```

## Keybinds

| Key | Action |
|---|---|
| ⌘N | New session (agent launcher) |
| ⇧⌘N | New window |
| ⌘W | Kill active session |
| ⇧⌘W | Close this window |
| ⇧⌘P | New project |
| ⌘G / ⇧⌘G | Per-project grid / all-sessions grid |
| ⌘Enter | Toggle grid / single |
| ⌘arrows | Spatial nav (grid) / session nav (single) |
| ⌘[ / ⌘] | Previous / next project |
| ⌘1–9 | Jump to nth session |
| ⌘= / ⌘- / ⌘0 | Font size up / down / reset |
| ⌘S | Toggle sidebar |

## Contributing

See `AGENTS.md` for repo-wide rules and `CONTRIBUTING.md` for the
contribution flow. The native rewrite has its own design docs under
`docs/native-rewrite/`.

## License

MIT — see `LICENSE`.

# Hive

A native desktop app for managing multiple AI coding agent sessions —
Claude, Codex, Gemini, Copilot, Aider, Pi, plain shells — across projects.

## Status

**Released.** The architecture is stable and in daily use. Latest release:
[v2.3.0](https://github.com/lucascaro/hive/releases/tag/v2.3.0).

What works:
- Multi-session daemon (`hived`) — sessions persist across GUI restart
- Wails-based desktop GUI with xterm.js — full keyboard control,
  font scaling, dark theme
- Projects (name, color, working dir) — sidebar tree
- Agent launcher (Claude, Codex, Gemini, Copilot, Aider, Pi, shell)
- Grid view: per-project (⌘G) or all-sessions (⇧⌘G), spatial arrow nav
- Multi-window (⇧⌘N) — independent windows share the same daemon
- BEL → desktop notification + visual pulse on non-focused sessions

- In-app updates (macOS): pick a release or latest-commit channel in
  Settings, then Update → Restart

Not yet shipping: scrollback resume across daemon restart, splits
inside grid cells, workflows / agent teams, code signing and
notarization, platform installers, in-app updates on Windows/Linux.

## Build

Requires Go 1.25.14+, Node 20+, and the Wails CLI:

```sh
scripts/ci-bootstrap.sh  # installs the pinned Wails CLI + generates bindings
```

Then from the repo root:

```sh
./build.sh                    # macOS .app at cmd/hivegui/build/bin/hivegui.app
./build.sh --open             # build then launch
./build.sh --zip --version v2.3.0   # also write release/<name>.zip
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

## Updating

Hive checks for updates in the background — once on launch and every
six hours — and shows an "Update available" banner. The check is also
reachable manually from **File → Check for Updates…**. Which updates it
looks for depends on the channel you pick in **Settings → Updates**:

| Channel | Checks | Applying it |
|---------|--------|-------------|
| **Release** (default) | A newer tagged release than the running version | Downloads the macOS zip, verifies its SHA-256 against the published `checksums.txt`, unpacks it |
| **Latest** | Whether your source checkout's upstream branch has commits the running build doesn't | `git pull --ff-only` then `./build.sh` in that checkout |

The release check is a single anonymous `GET` to
`api.github.com/repos/lucascaro/hive/releases/latest` (no identifying
data beyond a `User-Agent: hivegui/<build-id>` header). On the release
channel, untagged dev builds — anything built without `./build.sh
--version <tag>` — skip the check entirely and never call out; the
latest channel is the one built for those.

The latest channel pulls and *executes* code, so it refuses any checkout
whose tracked branch does not come from this repository, and runs git
with hooks disabled.

The latest channel needs to know where your checkout is. Hive finds it
by walking up from its own binary, which works for a locally built app;
for an installed `Hive.app` you point at the directory yourself. It
refuses to pull over a dirty working tree or a detached HEAD.

The SHA-256 manifest is an **integrity** check, not a provenance one: it
is published in the same release as the zip, and the bundle is neither
signed nor notarized, so it catches a truncated or corrupted download —
not a compromised release. Signing and notarization are tracked
separately.

**Nothing is downloaded or built until you press Update.** The button
shows progress while it works, then becomes **Restart** — that step
replaces the installed app and relaunches it, restarting `hived` so both
halves come from the same build. That restart terminates every running
shell and agent — save your work before pressing it.

Applying an update in place is macOS-only. On Windows and Linux the
banner keeps its Download button, which opens the release page.

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
DESIGN.md          # architecture map
build.sh           # macOS universal build
```

## Keybinds

| Key | Action |
|---|---|
| ⌘T / ⇧⌘T | New session (agent launcher) / in a git worktree |
| ⌘P / ⇧⌘P | Duplicate session / duplicate choosing the tool |
| ⌘N | New project |
| ⇧⌘N | New window |
| ⌘W | Kill active session |
| ⇧⌘W | Close this window |
| ⌘G / ⇧⌘G | Per-project grid / all-sessions grid |
| ⌘↑ / ⌘↓ | Previous / next session (focused) / move between tiles vertically (grid) |
| ⌘← / ⌘→ | Spatial nav between tiles in grid view. In focused mode they move the cursor to the start / end of the line (macOS; ⇧⌘← / ⇧⌘→ do the same). On Windows and Linux use Ctrl+← / Ctrl+→ for word-wise movement, as in any terminal |
| ⇧⌘↑ / ⇧⌘↓ | Move the session up / down within its project (wraps) |
| ⌃- / ⌃⇧- | Back / forward through recently visited sessions (Ctrl+Alt+- / Ctrl+Alt+Shift+- on Windows and Linux, where ⌃- is zoom) |
| ⌘B | Next session needing attention (rang the bell) |
| ⇧⌘B | Jump back to where you were before the first ⌘B |
| ⌘[ / ⌘] | Previous / next project |
| ⌘E | Worktrees in the active project (browse, resume, delete) |
| ⌘1–9 | Jump to nth session |
| ⌘= / ⌘- / ⌘0 | Font size up / down / reset |
| ⌘S | Toggle sidebar |
| ⇧⌘K / ⌘/ | Command palette / keyboard-shortcuts overlay |

Full list in the app: **⌘/**. (Ctrl replaces ⌘ on Windows and Linux.)

## Contributing

See `AGENTS.md` for repo-wide rules, `DESIGN.md` for the architecture
map, and `CONTRIBUTING.md` for the contribution flow.

## License

MIT — see `LICENSE`.

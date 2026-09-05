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
- Per-session state in the sidebar and tile headers — working, idle,
  waiting for you, exited — derived from what the terminal renders, so
  every agent and plain shell gets it. Claude sessions get a more
  precise answer via Claude Code hooks: every session Hive spawns sets
  `HIVE_SESSION_ID` / `HIVE_SOCKET` in the child's environment, and
  `hived hook` (invoked by hooks Hive wires through `claude --settings`)
  reports prompts, turn-end, and permission prompts back to the daemon
  over that socket. Pi sessions get the same precision through a small
  extension Hive ships with the daemon and loads with `pi -e` — nothing
  to install, and inert when you run `pi` outside Hive

- In-app updates (macOS): pick a release or latest-commit channel in
  Settings, then Update → Reload (or Restart, when the daemon changed)
- **Reload GUI** — picks up a new GUI build without touching `hived`,
  so every running shell and agent survives
- Menu-bar agent (macOS) — daemon version, session list and attention
  count, with reload / restart / update actions, live even with every
  window closed

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

## The menu bar (macOS)

Hive puts an icon in the menu bar. It reports on the **daemon**, not on
any window, so it keeps working with every window closed — which is
also the only time you can't ask the GUI. It shows:

- the running `hived`'s version and build, and a warning line when that
  daemon is too old for this build to drive;
- how many sessions are open across how many projects, and how many are
  waiting on you;
- every session, `project · session`, with a dot on the ones that rang.
  Clicking one jumps straight to it, launching Hive if it isn't running.

Plus **Reload GUI**, **Restart Daemon…** (confirmed — it ends every
running shell and agent), **Check for Updates…**, **Open Hive** and
**Quit Hive**.

It starts on its own whenever `hived` or a window does, so in normal use
it is simply there. `Settings ▸ Menu bar ▸ Start at login` additionally
registers it with macOS so it appears at login before either has run.
Set `HIVE_NO_MENUBAR=1` to suppress it; an isolated run
(`HIVE_STATE_DIR` set, e.g. `scripts/dev-iso.sh`) never starts one.

The agent ships inside the app bundle at
`hivegui.app/Contents/Library/LoginItems/hivebar.app` (whatever you
renamed the bundle to on install). It is a client like
the GUI — it talks to the daemon over the wire protocol and never opens
a PTY of its own.

## Reloading vs restarting

Picking up a new build of Hive does not have to cost you your sessions.
`hived` outlives the GUI, so when only the GUI has changed, **File ▸
Reload GUI** relaunches every window and leaves every shell and agent
running, with its scrollback intact. It needs no confirmation because it
destroys nothing.

**File ▸ Restart Daemon…** is the other half, and it does end every
running session — so it confirms first.

Hive decides which you need by comparing the two builds' *daemon
contract*, an integer that changes only when the daemon's observable
behaviour does. A frontend-only build leaves it untouched, so a rebuild
no longer costs you a restart. See
[docs/design-docs/daemon-contract.md](docs/design-docs/daemon-contract.md).

## Updating

Hive checks for updates in the background — once on launch and every
six hours — and shows an "Update available" banner. You can also run the
check yourself: the ⤓ button in the sidebar header, next to **+**, or
**File → Check for Updates…** on macOS. Either way the result lands in
the same banner, including "up to date" and check failures. Which
updates it looks for depends on the channel you pick in
**Settings → Updates**:

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
shows progress while it works, then becomes either **Reload** or
**Restart**, and the difference matters:

- **Reload** — the update only changes the GUI. Hive relaunches its
  windows and leaves `hived` running, so every shell and agent keeps
  going. Nothing is lost and there is no confirmation prompt.
- **Restart** — the update changes the daemon too. Hive replaces
  `hived`, which terminates every running shell and agent. This one
  confirms first; save your work before pressing it.

Hive works out which by asking the staged build's `hived` for its
*daemon contract* (`hived --version --json`) and comparing it to the
running daemon's. Only a change the daemon actually exposes bumps that
contract, so a frontend-only release costs you nothing.

Applying an update in place is macOS-only. On Windows and Linux the
banner keeps its Download button, which opens the release page.

## Layout

```
cmd/
  hived/           # session daemon
  hivegui/         # Wails GUI client
  hivebar/         # macOS menu-bar agent (client, like the GUI)
internal/
  wire/            # protocol v1 (frame format + JSON control messages)
  session/         # one PTY + scrollback
  registry/        # sessions + projects + persistence
  daemon/          # socket listener + dispatch
  agent/           # built-in agent launcher catalog
  buildinfo/       # version, build id, and the daemon contract
  menubar/         # starts hivebar from hived and hivegui (no-op off macOS)
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
| ⌘Z | Reopen the last closed session |
| ⇧⌘W | Close this window |
| ⌘G / ⇧⌘G | Per-project grid / all-sessions grid |
| ⌘⏎ | Grid: focus the active session (single view) |
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

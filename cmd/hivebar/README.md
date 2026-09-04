# hivebar — Hive's macOS menu-bar agent

A status-bar item showing what the daemon is and what it is holding,
with actions to reach it.

## Why it is a separate process

`hived` outlives the GUI — it is spawned detached and has no idle exit —
so with every window closed there was no way to see whether it was
running, what version it was, what sessions it held, or to restart it.

It could have lived inside either existing binary. Both were rejected:

- **In `hived`** — a status item is AppKit, which means cgo and a
  main-thread run loop inside a headless daemon, and a broken Linux
  build.
- **In `hivegui`** — it would vanish during a GUI reload and after a
  quit, which are exactly the moments it exists for.

## What it is allowed to do

A pure wire client, the same rule the GUI obeys (see DESIGN.md): no
PTY, no `internal/session`, no registry writes. Everything it knows
arrives on one control connection; the only thing it touches in the
state dir is its own lock file.

It also does **not** spawn `hived`. A menu bar that started a daemon
merely by existing would resurrect one the user had just quit, at
login, every time. `hivebar` reports; the GUI starts things. The one
exception is Restart Daemon, where starting a replacement is the whole
point of the action the user clicked.

## Running it

`go run ./cmd/hivebar` shows **nothing**. macOS will not give a status
item to a process LaunchServices did not start as a bundle, so the dev
loop is:

```sh
./build.sh
open cmd/hivegui/build/bin/hivegui.app/Contents/Library/LoginItems/hivebar.app
```

`build.sh` installs it at
`hivegui.app/Contents/Library/LoginItems/hivebar.app`. That path is not
decoration: it is where macOS looks for an embedded login-item helper,
which is what makes the "Start at login" toggle in Settings work.

That toggle works on today's ad-hoc-signed builds — verified against a
real one. The widely-repeated claim that an embedded login item needs a
Developer ID and a matching Team ID did not hold here. What *does*
matter is the calling bundle: `SMAppService` resolves the helper
relative to the process that asks, so registration succeeds from
`hivegui` inside `Hive.app` and fails with "Invalid argument" from a
bare binary in a dev tree.

Both `hived` and `hivegui` start it on boot (`internal/menubar`), so in
normal use it appears on its own. Several attempts racing is the
expected case; the flock in `singleton.go` decides who wins. Set
`HIVE_NO_MENUBAR=1` to suppress that, and note that an isolated run
(`HIVE_STATE_DIR` set — `scripts/dev-iso.sh`, the e2e-real harness)
never starts one.

Logs land in `hivebar.log` beside `hived.log` in the state dir; a
bundled agent has no terminal to write to.

## What it deliberately does not implement

**Updates.** "Check for Updates…" opens the GUI and asks it to run the
check, rather than duplicating the ~3300 lines of staging,
verification and bundle-swapping in `cmd/hivegui/update*.go`. The GUI
owns that flow and is the thing being replaced. If this ever needs to
work with no GUI at all, lift `update*.go` into `internal/update` and
have both binaries call it.

## Layout

| File | Purpose |
|------|---------|
| `main.go` | singleton claim, wiring, `systray.Run` |
| `client.go` | control connection, reconnect loop, outbound commands |
| `model.go` | **pure** snapshot → menu model; everything testable lives here |
| `menu.go` | model → systray items, click routing |
| `actions.go` | launching the GUI, restarting the daemon, native confirms |
| `singleton.go` | the flock that keeps one icon in the menu bar |
| `assets/` | 22pt template PNG (black + alpha; macOS recolours it) |

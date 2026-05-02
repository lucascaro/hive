# Spike B — Wails + xterm.js + PTY round-trip

Throwaway code for Phase 0 of the native rewrite. Answers: can a Wails
desktop app embed xterm.js, pipe bytes to/from a Go-side PTY, and feel
like a real terminal?

See `docs/native-rewrite/phase-0.md` for the acceptance criteria.

## Build

Requires Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)
and Node 18+.

```sh
cd spikes/spike-b-wails
wails build
open build/bin/spike-b-wails.app    # macOS
```

For dev with hot reload:

```sh
wails dev
```

## What it does

- On launch, opens a window with a full-window xterm.js terminal.
- Frontend mounts xterm, computes grid size, calls `StartShell(cols, rows)`.
- Go spawns `$SHELL` (or `/bin/bash`) on a PTY at that size and pumps
  output back to the frontend as `pty:data` events (base64 for
  binary safety).
- Keystrokes flow the other direction: `term.onData` → `WriteStdin`.
- Window resize refits xterm and calls `Resize` on the PTY (TIOCSWINSZ).

## Manual acceptance run

1. `wails build && open build/bin/spike-b-wails.app`
2. Terminal interactive within ~1s.
3. `vim`, type, `:q!` — rendering correct.
4. `htop` (if installed) — full-screen TUI renders + updates.
5. `less /usr/share/dict/words`, scroll.
6. Resize the window — xterm reflows; `stty size` reports new dims.
7. Throughput: `cat /dev/urandom | head -c 1M | xxd | head -10000`
   streams smoothly, no UI hang.
8. Mouse selection + cmd-c copies text.
9. Unicode/emoji: `printf 'café 🌮\n'` renders correctly.
10. Quit window — child shell is killed (no orphans).

Repeat on Linux with a Linux-native Wails build.

## Known limitations (deliberate)

- Single shell, single window
- No persistence — closing the window kills the shell. (That's Spike A's
  question.)
- No connection to `hived`. Phase 1 unifies the two spikes.

## What this validates

- Wails IPC throughput is sufficient for terminal-rate output
- xterm.js handles VT/ANSI parsing, scrollback, mouse, true color
  without custom code
- `creack/pty` interop with the Wails event loop is clean
- Cold-start latency and bundle size are tolerable on macOS

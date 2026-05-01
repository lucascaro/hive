# Phase 0 — Report

**Status:** GO (with Windows caveats noted below)
**Date:** 2026-04-30
**Branch:** `silent-light`

## Decision

**Proceed to Phase 1 with the planned architecture: `hived` daemon + Wails GUI.**

Both spikes hit their core acceptance criteria. Scope was extended
mid-phase to validate Windows feasibility for Spike A; the port is in
place and binaries cross-build cleanly, but full runtime validation on
real Windows hardware is still a manual TODO.

## Spike A — daemon + PTY + reattach

**Result:** PASS on macOS (host). Cross-builds for Linux + Windows.

| Acceptance criterion | Status |
|---|---|
| Daemon owns the PTY, survives client disconnect | ✅ verified by automated smoketest |
| State (cwd + env) preserved across reattach | ✅ verified by automated smoketest |
| 4 KiB replay buffer delivers recent context on attach | ✅ verified by automated smoketest |
| Resize propagates to shell (TIOCSWINSZ → `stty size`) | ✅ verified by automated smoketest (41×137) |
| Vim mid-edit detach/reattach | ⏳ manual (require interactive TTY) |
| Mouse + Unicode | ⏳ manual |
| Linux native run | ⏳ manual (cross-build OK) |
| Windows native run | ⏳ manual (ConPTY port done, needs hardware) |

The `spike-a-smoketest` binary (also throwaway) speaks the framed
protocol directly without a TTY — it runs in CI/dev shells.

### Surprises

1. **Kitty keyboard protocol leakage.** First detach key was Ctrl+\,
   single byte. Worked in plain `bash`, failed in `claude` and recent
   `vim`. Cause: those TUIs send `\e[>1u` (kitty keyboard enable). In
   tmux that's harmless because tmux is a terminal emulator and absorbs
   it. `hivec-spike` is a passthrough — the enable sequence reached the
   user's outer terminal, after which Ctrl+\ arrived as a multi-byte
   CSI escape and the single-byte detect never fired.

   **Fix:** added a small streaming filter
   (`internal/proto/kittyfilter.go`) on the PTY→stdout path that strips
   kitty-keyboard CSI sequences (`\e[<…u`, `\e[>…u`, `\e[=…u`, `\e[?u`)
   while letting unrelated CSI through. With the filter in place,
   single-byte Ctrl-Q detach is robust.

   **Implication for Phase 1:** the production client must own enough
   terminal-emulation logic to absorb (or pass through with intent)
   the modes the remote app sets. If we go the GUI route (Wails +
   xterm.js), the GUI is itself a real terminal emulator and this
   problem disappears for the GUI client. It is still relevant if we
   ever ship a CLI client.

2. **`creack/pty` is POSIX-only.** Confirmed during Windows scoping.
   Switched Spike A to `github.com/aymanbagabas/go-pty`, which exposes
   a single `Pty` interface backed by Unix PTYs on POSIX and ConPTY on
   Windows. API maps cleanly to the original use; daemon code lost a
   few lines net.

3. **Unix sockets work on Windows 10+ (1803+).** Confirmed by
   cross-compile; runtime test pending. Means we may not need a
   separate named-pipe transport for Phase 1 if we drop support for
   pre-1803 Windows (a reasonable stance — those builds are out of
   Microsoft support).

### What did not get tested

- Real Linux + real Windows hardware. Cross-builds succeeded for
  linux/amd64, linux/arm64, windows/amd64, windows/arm64; the user has
  the test pack at `/tmp/hive-phase0-test.zip` to verify on their
  hardware.
- Long-running stability — only short smoketests.
- Behavior under high-rate output (megabytes/sec). Spot-checked but
  not measured.

## Spike B — Wails + xterm.js

**Result:** PASS on macOS (host). Builds; window opens; PTY child
process spawns; visual rendering deferred to manual review.

| Acceptance criterion | Status |
|---|---|
| `wails build` succeeds end-to-end (bindings, frontend, app) | ✅ |
| App launches, stays alive, spawns child shell | ✅ verified |
| Vim / htop / less render correctly | ⏳ manual |
| Resize reflows | ⏳ manual |
| 1 MB throughput stress (`cat /dev/urandom \| head -c 1M \| xxd`) | ⏳ manual |
| Mouse selection + copy | ⏳ manual |
| Unicode + emoji | ⏳ manual |
| Linux build | ⏳ user must build on Linux |
| Windows build | ⏳ user must build on Windows (Wails does not cross-compile cleanly) |

xterm.js handles VT/ANSI parsing, scrollback, mouse, and true color
with no custom emulator code on our side. Cold start on macOS is
under 1s. Bundle size is reasonable (Wails .app under 30 MB).

### Surprises

1. **Wails's bindings generator is silent on backend method removal.**
   Removing `Greet` and adding new methods regenerated `App.js`
   correctly during `wails build`. No surprises; just noting.

2. **Cross-compiling Wails to Windows from macOS is not a free
   `GOOS=windows`.** Wails wraps a webview (WebView2 on Windows,
   WebKit on macOS) and the build embeds platform-specific assets.
   Phase 6 will need a CI matrix with native Windows runners.

## Framework decision

**Locked: Wails v2 + xterm.js + creack/pty (Unix) / go-pty (cross-platform daemon).**

Specifically:
- **Daemon**: `github.com/aymanbagabas/go-pty` for cross-platform PTY
  (Unix PTY + Windows ConPTY behind one interface). Unix sockets on
  POSIX; Unix sockets on Windows 10 1803+ (or named pipes if we drop
  legacy Windows support — TBD in Phase 1).
- **GUI**: Wails v2 + xterm.js. Frontend is vanilla JS for the spike;
  Phase 1+ may switch to a small framework (Svelte / Solid) for state
  management as the sidebar lands. Backend stays Go.
- **Wire protocol**: keep the framed protocol shape from Spike A
  (1-byte type + 4-byte BE length + payload), with the lessons below
  applied to v0 of the real protocol.

### Wire protocol v0 sketch (informed by Spike A)

```
+-------+-------------+--------------+
| type  | len uint32  | payload      |
| 1 B   | 4 B BE      | len B        |
+-------+-------------+--------------+
```

Frame types (provisional):

| Code | Name        | Direction | Payload                                     |
|------|-------------|-----------|---------------------------------------------|
| 0x01 | DATA        | both      | raw bytes (PTY stdin/stdout)                |
| 0x02 | RESIZE      | C → S     | `uint16 cols, uint16 rows` BE               |
| 0x03 | HELLO       | C → S     | `uint16 protocol_version, JSON capabilities`|
| 0x04 | WELCOME     | S → C     | `uint16 protocol_version, JSON server info` |
| 0x05 | ATTACH      | C → S     | `JSON { session_id }` (Phase 2+)            |
| 0x06 | LIST        | C → S     | `JSON {}` returns sessions (Phase 2+)       |
| 0x07 | EVENT       | S → C     | `JSON { kind, ... }` (session lifecycle)    |
| 0x08 | ERROR       | S → C     | `JSON { code, message }`                    |

Notes:
- HELLO/WELCOME goes first on every connection so we can rev the
  protocol independently of clients.
- Multi-session support (ATTACH/LIST) does not exist in Spike A but
  the wire format anticipates it so Phase 2 doesn't need a v1.
- Payload encoding: bytes for DATA; JSON for control frames. Mixing
  is fine because the type byte selects the decoder.
- Framing cap: 1 MiB. Anything larger is an attacker / bug.

### Open questions for Phase 1

1. **Daemon lifecycle.** Spike A is foreground only. Phase 1 needs
   the daemon to start on demand (GUI launches it) and persist across
   GUI close. Decision: GUI launches `hived` as a detached child if
   no socket is found; user-level launchd / systemd-user comes in
   Phase 6.
2. **Persistence across daemon restart.** Out of scope for Phase 1
   first cut; revisit in Phase 2 (with multi-session, a "scrollback
   on disk" story is more useful anyway).
3. **Authentication on the socket.** POSIX file perms (0600 on socket)
   is sufficient for v1; revisit if we ever expose `hived` to other
   users on the same machine.
4. **Backpressure.** Spike B writes PTY output directly to a Wails
   event stream. Under heavy load (huge cat) we may need to chunk
   and yield. Spot-check OK; instrument in Phase 1.

## Plan revisions

None to the overall plan. One scope addition to Phase 6: cross-platform
release verification must include a Windows native CI runner; cannot
be done from macOS.

## Deliverables

- `spikes/spike-a-daemon/` — daemon + client + smoketest, all builds
  for darwin/linux/windows × amd64/arm64.
- `spikes/spike-b-wails/` — Wails app, builds locally on macOS;
  `README.md` documents how to build on Linux + Windows.
- `docs/native-rewrite/PLAN.md` — overall epic plan.
- `docs/native-rewrite/phase-0.md` — Phase 0 detailed plan.
- `docs/native-rewrite/phase-0-report.md` — this file.
- `/tmp/hive-phase0-test.zip` — portable test pack with prebuilt
  binaries (six platforms) + source + README.

## Next

Start Phase 1 — single persistent session, real protocol. See `PLAN.md`.

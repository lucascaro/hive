# Remote Claude Code via Hive — Design

**Date:** 2026-06-01
**Status:** Approved (brainstorming complete; ready for implementation plan)

## Goal

Be able to power on the Windows PC remotely and drive Claude Code on it
from a phone or laptop while away from home, using a terminal-only
connection. Crucially, the remote terminal session should be the *same*
Hive-managed session that the desktop GUI shows — one continuous session
whether driven locally (GUI) or remotely (SSH), not a separate silo.

## Decisions (and rejected alternatives)

- **Terminal-only access**, not graphical remote desktop. (Windows 11
  Home can't host RDP anyway; SSH is lighter and fits a CLI agent.)
- **Tailscale** for the secure network path — not self-hosted WireGuard.
  Rationale: zero router config, immune to CGNAT, built on WireGuard,
  free for personal use. Trades a little independence for reliability.
- **Native Windows OpenSSH Server** as the SSH endpoint — not Tailscale
  SSH (its server side is Linux-only). Tailscale still carries the
  connection; nothing is exposed to the public internet.
- **Wake-on-LAN from the Pi.** A sleeping/off PC isn't reachable over
  the internet, so the always-on Pi (same LAN, wired) sends the magic
  packet. This is the one job only the Pi can do.
- **Hive replaces tmux for persistence.** `hived` already keeps PTY
  sessions alive across client disconnects/GUI restarts — the exact
  property tmux would provide. The only missing piece is a terminal
  client; we build it.

## Part 1 — `hive` CLI attach client

### Why it's small
The daemon already exposes everything needed over its Unix socket:
- `internal/wire` defines the full framed protocol (v1): `FrameHello`,
  `FrameWelcome`, `FrameData` (raw PTY bytes), `FrameResize`,
  `FrameEvent`, `FrameError`, `FrameListSessions`/`FrameSessions`, and
  `FrameRequestReplay` (re-streams scrollback on attach).
- `Hello.Mode` supports `control`, `attach`, and `create`.
- `daemon.SocketPath()` is exported and resolves the platform-default
  socket (Windows: `%LocalAppData%\Hive\hived.sock`).

So the client is a new consumer of existing APIs. **No daemon changes.**

### New binary: `cmd/hive/`
Ships next to `hived.exe` / `hivegui.exe` (same directory requirement
already documented for the GUI/daemon pair).

#### Command: `hive ls`
1. Dial `daemon.SocketPath()`.
2. Send `Hello{Version: 1, Client: "hive-cli/<ver>", Mode: control}`.
3. Send `FrameListSessions` (empty `ListSessionsReq`).
4. Read `FrameSessions` → print a table: session id, name, project,
   agent, alive/dead.
5. Close.

#### Command: `hive attach [session-id]`
1. Resolve target session:
   - id given → use it.
   - no id, exactly one session → attach it.
   - no id, multiple → print the `ls` table and exit asking for an id
     (no fuzzy/interactive picker in v1 — YAGNI).
2. Dial socket → `Hello{Version: 1, Mode: attach, SessionID: id}`.
3. Read `Welcome{Cols, Rows}`.
4. Put local stdin/stdout in **raw mode** via `golang.org/x/term`
   (works on Windows conhost and over an SSH ConPTY-allocated pty).
5. Send `FrameRequestReplay` so scrollback repaints — reconnecting
   shows history, not a blank screen. Handle the
   `EventScrollbackReplayBegin` / `…Done` boundary events.
6. Two concurrent pumps:
   - socket → stdout: `FrameData` written raw; `FrameEvent` handled;
     `FrameError` surfaced; EOF/closed conn → exit.
   - stdin → socket: bytes wrapped in `FrameData`, except the detach
     key sequence.
7. On local terminal resize → send `FrameResize{Cols, Rows}`. (Unix:
   `SIGWINCH`; Windows: poll `term.GetSize` on a short ticker, since
   Windows has no SIGWINCH.)
8. **Detach keybind `Ctrl-A d`** (tmux-style): restore terminal, close
   the connection, exit 0 — session stays alive in the daemon.
9. Always restore terminal mode on exit (defer), including on error.

### Platform notes
- Go supports `AF_UNIX` on Windows 10+; dialing the socket works the
  same as on Linux/macOS.
- Raw mode + size queries via `golang.org/x/term` (already an
  acceptable dep; confirm in `go.mod` during planning).

### Build
- Add `cmd/hive` to `build.sh` and to the Windows/Linux cross-build
  lines in `README.md`.
- `update-hive.bat` / `update-hive.ps1` pull release binaries, so
  `hive.exe` lands alongside the others once it's in the release zip.

## Part 2 — Remote access plumbing (guided setup)

Performed on the devices with step-by-step guidance; little of this is
repo code.

### Tailscale
- Install on PC (Windows), Pi (Raspberry Pi OS), phone, and laptop.
- One tailnet; enable MagicDNS for stable names (`mypc`, `mypi`).

### Windows OpenSSH Server
- Enable the OpenSSH Server optional feature; set the service to start
  automatically.
- Public-key auth; install the client's public key into
  `%ProgramData%\ssh\administrators_authorized_keys` (admin account) or
  `~/.ssh/authorized_keys`.
- Reachable only over the Tailscale interface; not exposed to LAN or
  internet.
- Default shell can be set to PowerShell.

### Wake-on-LAN
- **BIOS/UEFI:** enable "Wake on LAN" / "Power On by PCIe"; disable
  ErP/EuP deep-sleep if present.
- **Windows NIC (wired Ethernet):** Device Manager → adapter → Power
  Management: allow device to wake the computer + only a magic packet;
  Advanced: enable "Wake on Magic Packet".
- **Disable Fast Startup** (Control Panel → Power → "Choose what the
  power buttons do") — it silently breaks wake-from-shutdown.
- **Pi:** `sudo apt install wakeonlan`; `wake-pc` script holding the
  PC's MAC address (`wakeonlan AA:BB:CC:DD:EE:FF`).
- Sleep (S3) wakes most reliably; confirm wake-from-full-shutdown (S5)
  works during testing and fall back to Sleep if the board won't.

## End-to-end flow (from phone)
```
ssh mypi 'wake-pc'      # Pi sends the magic packet; PC powers on
# wait ~20–30s for boot + services
ssh mypc                # direct, over Tailscale
hive attach             # rejoin the live Hive session, scrollback intact
```
A laptop convenience wrapper `connect-pc` can chain wake → wait-for-ping
→ `ssh mypc hive attach` into one command (nice-to-have, not required).

## Build & verification order
1. Build `hive ls` + `hive attach`; verify against the **local** daemon
   (no network) — prove the client streams, resizes, replays, detaches.
2. Enable Windows OpenSSH; verify `ssh mypc hive attach` on the LAN.
3. Configure Wake-on-LAN; verify the Pi wakes the PC from sleep, then
   from shutdown.
4. Install Tailscale everywhere; verify the full flow from off-network.

## Out of scope (YAGNI)
- Graphical remote desktop / viewing the Hive GUI remotely.
- Public-internet SSH exposure, port-forwarding, dynamic DNS.
- A web UI for waking the PC (a one-line SSH command suffices).
- Interactive/fuzzy session picker in `hive attach` v1.
- WSL2 + tmux (Hive's daemon supersedes the need).

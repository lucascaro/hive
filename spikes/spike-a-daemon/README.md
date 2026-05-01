# Spike A — `hived` daemon + PTY + reattach

Throwaway code for Phase 0 of the native rewrite. See
`docs/native-rewrite/phase-0.md` for the question this spike answers
and the acceptance criteria.

## Build

From the repo root:

```sh
go build -o /tmp/hived-spike ./spikes/spike-a-daemon/cmd/hived-spike
go build -o /tmp/hivec-spike ./spikes/spike-a-daemon/cmd/hivec-spike
```

## Run

In one terminal, start the daemon (foreground; quit with Ctrl+C kills the
daemon and its shell):

```sh
/tmp/hived-spike
```

In another terminal, attach the client:

```sh
/tmp/hivec-spike
```

Detach: **Ctrl-Q**. The daemon and its shell keep running. Run
`hivec-spike` again to reattach — the daemon replays its 4 KiB ring
buffer so you see recent output. To send a literal Ctrl-Q to the remote
shell, press **Ctrl-Q twice**.

> **How Ctrl-Q stays single-byte even with claude/vim running.** Modern
> TUIs (Claude Code, recent vim, Helix) emit `\e[>1u` to enable the
> kitty keyboard protocol on whatever terminal is hosting them. In that
> mode every keystroke arrives as a multi-byte CSI escape rather than a
> raw control byte — which would silently swallow our single-byte detach.
> tmux dodges this implicitly because tmux is itself a terminal emulator
> and absorbs the enable sequence. `hivec-spike` is a passthrough, not
> an emulator, so we explicitly **filter out kitty-keyboard CSI
> sequences** from the PTY→stdout stream (see
> `internal/proto/kittyfilter.go`). The local outer terminal therefore
> never enters extended-keyboard mode, and Ctrl-Q remains 0x11.

## Acceptance check (manual)

1. Start daemon, attach client, run `cd /tmp && export FOO=bar`.
2. Detach (`Ctrl-Q`). Confirm daemon + shell are alive: `ps -p $(pgrep -f hived-spike)` and check children.
3. Reattach. Run `pwd` → `/tmp`, `echo $FOO` → `bar`. ✅
4. Resize the terminal mid-session and run `stty size` — sees new size. ✅
5. `vim`, type, detach mid-edit, reattach — vim is on the same screen. ✅
6. Run `claude` (or any TUI that uses kitty keyboard mode), then `Ctrl-Q` — still detaches. ✅
7. Repeat on Linux. Cross-build and run on Windows (uses ConPTY + AF_UNIX, requires Win10 1803+).

## Wire protocol (throwaway, see `internal/proto/proto.go`)

```
+-------+-------+--------+----------------+
| type  | len (uint32 BE) | payload (len) |
| 1B    |          4B    |     N B       |
+-------+-------+--------+----------------+
```

Types:
- `0x01 DATA` — bidirectional. Client→server: keystrokes. Server→client: PTY output (replay buffer included on attach).
- `0x02 RESIZE` — client→server. Payload is `uint16 cols, uint16 rows` BE.

## Out of scope (deliberately)

- Multiple sessions
- Auth
- Daemon survives across **daemon** restart (only across client restart)
- Pretty error handling
- Production daemon lifecycle (launchd / systemd / Task Scheduler) — for
  the spike, run `hived-spike` manually in a terminal

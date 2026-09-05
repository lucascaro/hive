# State-machine fixtures

Deterministic recordings of a real `claude` binary's raw PTY output, used
by `internal/session/state_fixture_test.go` to table-test the heuristic
tier of `internal/agentstate` against ground truth instead of guesswork.

Each `.bin` file is a stream of records:

```
uint32 BE  offset_ms   // ms since the recorded process started
uint32 BE  chunk_len
byte[chunk_len] data    // raw bytes read from the PTY at that offset
```

## How they were recorded

- `claude` version: **2.1.261**
- Terminal size: **80x24**
- Recorder: a small Go program (`github.com/aymanbagabas/go-pty`, the
  same PTY lib `internal/session` already depends on — see
  `spawn_other.go`) that spawns a command in a PTY, writes scripted
  stdin at scheduled ms-offsets, and dumps every PTY read as one of the
  records above. Not checked into the repo (scratch tool); the script
  below is exact enough to rebuild it from `main.go`'s doc comment if
  needed, or ask for the recorder from the job that produced these
  fixtures.
- Each recording ran in its own fresh `git init`-ed temp directory (so
  the statusline's `repo`/`branch` segments have something to show) and
  a fresh statusline config with the `user_host` segment disabled, so no
  hostname/username lands in the bytes:
  `claude --settings '{"statusLine":{"type":"command","command":"sh ~/.claude/statusline-command.sh"}}'`
  with `STATUSLINE_CONFIG_FILE` pointing at that config.
- The child process env was stripped of `CLAUDE_CODE_MESSAGING_*`,
  `CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_CODE_SESSION_ID`,
  `CLAUDE_CODE_ENTRYPOINT`, and any `ANTHROPIC_API_KEY*` before spawning
  — recording ran nested inside another Claude Code session, and those
  variables would otherwise let the child talk back to the parent's
  control channel or leak its session id into the recording. All four
  fixtures below were captured with this filtering in place (an earlier,
  unfiltered test recording briefly contaminated a `~/.claude/projects/`
  cache entry with the parent session's title; that cache entry was
  deleted, nothing was committed from it).
- A first launch in a brand-new directory needs its "do you trust this
  folder" dialog navigated once: the recorder sends down-arrow then
  Enter ~1.5-1.8s after launch to pick "Yes, I trust this folder" (the
  default-selected option is "No, exit" — a bare Enter exits
  immediately, which is how the first two recording attempts were lost).
  Trust then persists for that directory for the rest of that recording.

Script format fed to the recorder (`afterMs:literal;afterMs:literal`,
via a bash `$'...'` string so `\e`/`\r` are real bytes, not the literal
two-character sequences `\e`/`\r`):

- **claude-idle-prompt.bin** — trust dialog nav, then nothing until
  `10500:/exit\r`. Captures ~7.9s of an untouched prompt. Re-recorded
  with a recorder that answers device-status queries (see next section):
  it still comes back byte-free during the idle window.
- **claude-typing.bin** — trust dialog nav, then one character every
  200ms starting at 6500ms (`h`,`e`,`l`,`l`,`o`), then
  `10300:/exit\r` (3s after the last character).
- **claude-streaming.bin** — trust dialog nav, then at 6500ms
  `reply with exactly the word pong and nothing else\r`, then
  `18500:/exit\r` (~12s later, well past the reply settling).
- **claude-permission.bin** — **not recorded**. The task asked for
  `run the shell command \`ls\` using the Bash tool\r` at 6500ms,
  waiting ~12s for a permission prompt, then Escape + `/exit`. Against a
  real claude 2.1.261, `ls` via the Bash tool executed immediately with
  no prompt, both under the default permission mode and under
  `--permission-mode default`. See `TestFixturePermissionSkipped` in
  `state_fixture_test.go`.

## Re-recording

1. Rebuild the recorder (spawn in an 80x24 `go-pty`, script writes at
   scheduled offsets, dump `(offset_ms, len, bytes)` records — see the
   format above).
2. `git init` a fresh temp dir per fixture, disable the `user_host`
   statusline segment via `STATUSLINE_CONFIG_FILE`, and strip
   `CLAUDE_CODE_*`/`ANTHROPIC_API_KEY*` from the child's env as described
   above.
3. Navigate the trust dialog once per fresh directory (down-arrow, then
   Enter, ~1.5-1.8s after launch).
4. Run the scripts above, inspect the `.bin` for hostname/username/token
   strings before replacing the checked-in fixture (`strings -a
   fixture.bin | grep -i "$(whoami)\|$(hostname -s)"` should be empty),
   and update the offsets referenced in `state_fixture_test.go`'s
   comments if timings shifted.

## Device-status queries: the recorder answers them, and it didn't matter

`claude-idle-prompt.bin`'s idle window (roughly 2.65s-10.5s into the
recording) contains **zero bytes** — no `ESC[6n`/`ESC[?6n`, nothing at
all. The first theory was that this recorder's PTY never answered
claude's device-status queries (cursor-position report, `ESC[6n` /
`ESC[?6n`; primary device attributes, `ESC c`) the way a real
interactive terminal (e.g. the Hive GUI's attached xterm.js) does, and
that claude gives up polling once nothing answers — which would make
the fixture unrepresentative of the case `VT.ScreenDigest`'s doc comment
describes ("an idle Claude Code session writes an ESC[?6n cursor-position
query every 200ms forever").

The recorder (`main.go` in the scratch job, not checked into this repo)
was changed to watch every PTY read for `ESC[6n`, `ESC[?6n`, and
`ESC[c`, and write back a canned cursor-position report
(`ESC[24;1R`) or primary-device-attributes reply (`ESC[?62;22c`)
for each one found, same as a real terminal would. `claude-idle-prompt.bin`
was then re-recorded end-to-end (fresh `git init` temp dir, trust dialog
navigated, same script/timings as before).

Result: claude 2.1.261 sends **zero** `ESC[6n`/`ESC[?6n` queries
anywhere in the recording — not during idle, not at startup, not ever.
It does send `ESC[c` three times, but all three land during the
trust-dialog/startup sequence (≤2.27s into the recording, before the
screen settles) and it never asks again once idle. So the DSR-answering
theory doesn't hold either, at least not for this claude version /
PTY library / non-interactive recording harness: the idle window stays
byte-free even when every device-status query claude actually sends is
answered correctly. `VT.ScreenDigest`'s doc comment describes a real
measured behavior somewhere (interactive terminal, different
version/config, or a query this recorder still doesn't match) but this
recorder — now answering DSR/DA1 — can't reproduce it. The machine and
VT are unchanged; the test asserts what was actually recorded and calls
this out rather than papering over it — see the task's report for more.

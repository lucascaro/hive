# ACP probe

Evidence harness for [docs/design-docs/acp-workflows.md](../../docs/design-docs/acp-workflows.md)
(spec 492). It runs one scripted ACP session against a pinned adapter and
records, per agent, whether these work:
- MCP injection
- permission prompts
- plan and tool-call streaming
- `session/load`
- reopening the session headless
- reopening it in a Hive PTY

No dependencies: Node ≥ 22 and `python3` (stdlib `pty`) only.

## Run it

```bash
node scripts/acp-probe/probe.mjs claude   # or codex, pi
node scripts/acp-probe/check-doc.mjs docs/design-docs/acp-workflows.md
node --test 'scripts/acp-probe/*.test.mjs'
```

`probe.mjs` writes `results/<agent>-<UTC date>.json`. Then update the doc's
capability table and verdict to match: `check-doc.mjs` (run in CI) fails until
they agree. Re-run whenever a pinned version in `agents.mjs` changes.

**Run agents one at a time.** Three probes in parallel once made Codex skip the
injected tool (finding F3 in the design doc).

## Cost and side effects — read before running

- **Model spend:** two prompts per agent (the session prompt and one headless
  resume), with no retries. The agents run under your existing CLI login; the
  probe never reads credentials.
- **What it writes outside the repo.** It never deletes any of these.
  - **Session stores.** One new session per run lands in each agent's normal
    store: `~/.claude/projects/<encoded temp cwd>/`, `~/.codex/sessions/`,
    `~/.pi/agent/sessions/`, plus pi-acp's `~/.pi/pi-acp/session-map.json`.
    The script prints the temp cwd.
  - **Folder trust.** When a CLI shows its folder-trust dialog, the probe
    answers it with the key the dialog offers. The CLI then records trust for
    that temp dir in its own config (for example `~/.claude.json` or Codex's
    config). The probe itself edits no config file.
- **Detached daemons.** An interactive `codex resume` starts a managed
  `codex app-server` daemon that outlives the probe and holds the thread's
  writer lock (F2). The results file reports how many appeared
  (`detached_daemons_after_pty`). The probe does not kill them: they are
  yours, and Codex restarts them on demand.
- **Update nags.** Codex's update dialog defaults to running `curl … | sh`. The
  probe answers it with Esc and never presses Enter while it is on screen
  (`blocksEnter` in `agents.mjs`).
- **Raw logs.** Full JSON-RPC traffic and stderr go to an untracked
  `$TMPDIR/acp-probe-raw-*` dir, printed at exit. The results file keeps only
  the fields `whitelist()` allows (see the tests).
- **Inherited markers.** Variables that mark the parent session
  (`CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, `HIVE_SOCKET`, …) are removed
  from the children's environment. Hive's daemon spawns agents without them,
  and `HIVE_SOCKET` would otherwise send hook events to your real daemon.

## Files

| File | Role |
|---|---|
| `probe.mjs` | JSON-RPC client, the scripted session, the verdict logic, and the results whitelist |
| `agents.mjs` | pinned adapters, Hive `ResumeArgs` mirrors (with `internal/agent` citations), dialog rules |
| `submit-mcp.mjs` | stdio MCP server exposing `submit_result`; records calls out-of-band |
| `pty-run.py` | stdlib PTY host (macOS `script(1)` refuses a piped stdin) |
| `check-doc.mjs` | asserts that the design doc's table and verdict match `results/` |
| `testdata/fake-agent.mjs` | scriptable fake ACP agent for the tests |

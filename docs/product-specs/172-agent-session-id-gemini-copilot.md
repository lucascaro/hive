# Pin/capture agent session id for Gemini and Copilot

- **Issue:** #172
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/active/172-agent-session-id-gemini-copilot.md](../exec-plans/active/172-agent-session-id-gemini-copilot.md)

## Problem

#166 fixed the duplicate-cwd Restart collision for Claude (pin via
`--session-id`) and Codex (capture from rollout file), but explicitly
deferred Gemini and Copilot. Hive sessions sharing a cwd or worktree
that run Gemini or Copilot still collide on Restart: clicking Restart
on session B runs `gemini --continue` / `copilot --resume` in the
shared cwd, and the agent CLI picks "most recent in cwd" — usually
the sibling, not session B.

⌘P duplicate (#149) makes this a real-day-one collision for users
running multiple Gemini or Copilot sessions in one project.

## Desired behavior

- Restart on a Gemini or Copilot session that shares a cwd with another
  Hive session reattaches to *that* session's conversation, not a
  sibling's.
- Daemon restart (`hived` reboot) revives each Gemini/Copilot session
  against its own conversation.
- Non-duplicated sessions are unaffected.

## Success criteria

- Two Gemini sessions in the same project: type "alpha" in one, "beta"
  in the other, restart each, each agent reports back its own word.
- Same for two Copilot sessions.
- `gemini --session-id <UUID>` (caller-pinned) and `copilot
  --resume=<UUID>` (post-capture) round-trip across daemon restart.

## Non-goals

- Aider: no `--resume` command exists; stays on plain `Cmd`.
- Cross-machine session sync.
- Backfilling pre-existing Gemini/Copilot sessions whose ids were
  never captured (they retain today's path-scoped resume; collision
  risk only goes away on next Create).

## Notes

- Verified empirically: `gemini --session-id <UUID>` accepts an
  arbitrary caller UUID, and `gemini --resume <UUID>` resumes by full
  UUID. (Smoke test: pinned session under `/tmp/gemini-test`, resumed
  successfully.)
- Verified via `copilot --help`: `--resume=<session-id>` resumes by
  id; no `--session-id`-style flag for caller pinning. Inspection of
  `~/.copilot/session-state/<UUID>/workspace.yaml` shows a `cwd:`
  field — matches the cwd-disambiguation strategy already used in
  `codex.go`.
- This finishes the work scoped out of #166. The "track Gemini/
  Copilot/Codex as a follow-up issue" decision-log line in
  `docs/exec-plans/completed/165-restart-session-wrong-session.md`
  closes when this lands.

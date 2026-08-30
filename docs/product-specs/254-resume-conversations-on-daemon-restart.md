---
issue: null
title: Resume conversations on daemon restart
type: enhancement
complexity: M
stage: TRIAGE
---

# Resume conversations on daemon restart

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P3
- **Stage:** TRIAGE
- **Exec plan:** [docs/exec-plans/active/254-resume-conversations-on-daemon-restart.md](../exec-plans/active/254-resume-conversations-on-daemon-restart.md)

## Problem

When `hived` restarts (manually or via `RestartDaemon`), persisted sessions are
revived but the agent process starts fresh — the prior conversation is lost from
the user's point of view. The Restart Session feature already plumbs per-agent
resume commands (`ResumeCmd` on `agent.Def`); daemon restarts do not use them.

## Desired behavior

After a daemon restart, each revived session comes back attached to the same
agent conversation it had before, not a blank one. Sessions that share a project
cwd each resume their own conversation, not a common most-recent one.

## Success criteria

- Restarting `hived` with an active Claude session and reattaching shows the
  prior conversation, not a fresh prompt.
- Two sessions duplicated onto the same cwd resume distinct conversations.
- Agents with no usable conversation ID (Aider, Copilot) still revive, starting
  fresh, with no error.

## Non-goals

- Rebuilding conversation history for agents that expose no resume ID.
- Changing the already-shipped Restart Session path (it can adopt the per-session
  ID later).

## Notes

Migrated from legacy `features/active/resume-on-daemon-restart.md` (stage
TRIAGE). Locally numbered 254 — no GitHub issue exists.

Flipping `Revive` to use `ResumeCmd` is trivial but wrong for duplicated
sessions: `claude --continue` / `codex resume --last` resume the most recent
conversation *in the cwd*, not the most recent conversation for that specific
hive session. Doing it right needs a per-hive-session conversation ID.

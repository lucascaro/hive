---
issue: null
title: Resume conversations on daemon restart
type: enhancement
complexity: M
priority: P3
pr: 166
shipped: 2026-05-08
stage: DONE
---

# Resume conversations on daemon restart

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P3
- **Stage:** DONE
- **Exec plan:** [docs/exec-plans/completed/254-resume-conversations-on-daemon-restart.md](../exec-plans/completed/254-resume-conversations-on-daemon-restart.md)

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

**Closed 2026-10-03 as already shipped.** The behaviour landed in #166
(2026-05-08), before this spec was migrated: `Registry.Revive`
(`internal/registry/registry.go`) resumes `Entry.AgentSessionID` through
`def.ResumeArgs`, and falls back to plain `def.Cmd` when there is no id. The
id is per Hive session (pinned with `SessionIDFlag` at create, or captured
post-spawn for codex), so duplicated sessions on one cwd resume distinct
conversations. Covered by `TestReviveUsesResumeArgsForPinnedClaude`,
`TestCreatePinsAgentSessionIDForClaude`, `TestAgentSessionIDPersistsAcrossReload`
and `TestRestartUsesCapturedAgentSessionIDForCodex` in
`internal/registry/registry_test.go`. #494 fixed Claude transcript lookup for
cwds containing `_`, which this path depends on.

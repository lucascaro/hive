# Feature: Resume conversations on daemon restart

- **GitHub Issue:** —
- **Stage:** TRIAGE
- **Type:** enhancement
- **Complexity:** M
- **Priority:** —
- **Branch:** —

## Description

When `hived` restarts (manually or via `RestartDaemon`), persisted
sessions are revived but the agent process starts fresh — the prior
conversation is lost from the user's POV. The Restart Session feature
already plumbs per-agent resume commands (`ResumeCmd` on `agent.Def`);
this extends that to `Registry.Revive` so daemon restarts also recover
state.

## Why this is not a one-liner

Flipping `Revive` to use `ResumeCmd` is trivial but **wrong for
duplicated sessions**: `claude --continue` / `codex resume --last`
resume the most recent conversation *in the cwd*, not the most
recent conversation for that specific hive session. Two hive sessions
sharing a project cwd (e.g. via ⌘P duplicate) would both revive onto
the same agent conversation.

To do it right we need a per-hive-session conversation ID.

## Plan sketch

1. Capture the agent's conversation ID at runtime (each agent stores
   it differently — Claude writes JSONL under `~/.claude/projects/<cwd>/`,
   Codex similar). Probably a per-agent `ResumeIDLocator` func that
   scans the on-disk store for the most recent file modified since
   the session started.
2. Persist `ConversationID` on `registry.MetaFile` (and the in-memory
   `Entry`).
3. Extend each `agent.Def` with a `ResumeWithIDCmd(id) []string`
   builder so resume can be `claude --resume <id>` instead of
   `--continue`.
4. `Revive` uses `ResumeWithIDCmd(e.ConversationID)` when set,
   falling back to plain `Cmd` (fresh start, no resume).
5. `Restart` (already shipped) can opt into the same per-session ID
   path once it's wired.

## Open questions

- Aider and Copilot may not expose a usable conversation ID. Acceptable
  to leave them on plain `Cmd` for revive.
- When does the locator run? Probably on session exit / write activity,
  not at revive time (the agent process is already gone by then).

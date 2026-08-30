# Resume conversations on daemon restart

- **Spec:** [docs/product-specs/254-resume-conversations-on-daemon-restart.md](../../product-specs/254-resume-conversations-on-daemon-restart.md)
- **Issue:** —
- **Branch:** —
- **Status:** active

## Summary

Extend the existing per-agent resume plumbing (`ResumeCmd` on `agent.Def`, added
for Restart Session) to `Registry.Revive`, so a daemon restart recovers each
session's own agent conversation rather than starting fresh.

## Research

Carried over from the legacy feature file; not yet re-verified against current
code. Relevant pieces named there:

- `agent.Def` — already carries `ResumeCmd`, used by Restart Session.
- `Registry.Revive` — revives persisted sessions, currently always plain `Cmd`.
- `registry.MetaFile` / in-memory `Entry` — where a conversation ID would persist.

## Approach

Per-hive-session conversation ID, rather than the one-line `Revive` → `ResumeCmd`
change. The one-liner is wrong for duplicated sessions: `claude --continue` and
`codex resume --last` resume the most recent conversation *in the cwd*, so two
hive sessions sharing a project cwd (⌘P duplicate) would both revive onto the
same agent conversation.

Plan sketch:

1. Capture the agent's conversation ID at runtime. Each agent stores it
   differently — Claude writes JSONL under `~/.claude/projects/<cwd>/`, Codex
   similar. Likely a per-agent `ResumeIDLocator` func that scans the on-disk
   store for the most recent file modified since the session started.
2. Persist `ConversationID` on `registry.MetaFile` and the in-memory `Entry`.
3. Extend each `agent.Def` with a `ResumeWithIDCmd(id) []string` builder, so
   resume is `claude --resume <id>` instead of `--continue`.
4. `Revive` uses `ResumeWithIDCmd(e.ConversationID)` when set, falling back to
   plain `Cmd` (fresh start, no resume).
5. `Restart` (already shipped) can opt into the same per-session ID path once
   it is wired.

## Open questions

- Aider and Copilot may not expose a usable conversation ID. Acceptable to leave
  them on plain `Cmd` for revive.
- When does the locator run? Probably on session exit / write activity, not at
  revive time — the agent process is already gone by then.

## Decision log

- **2026-08-30** — Migrated from `features/active/resume-on-daemon-restart.md`
  to the `docs/` layout; spec numbered 254 locally. Why: legacy `features/`
  pipeline is retired; no GitHub issue exists for this item.

## Progress

- Not started. Stage TRIAGE — research above predates the current tree and needs
  re-verification before planning.

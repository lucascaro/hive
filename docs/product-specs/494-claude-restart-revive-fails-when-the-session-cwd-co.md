---
issue: 494
title: "Claude Restart/Revive fails when the session cwd contains \"_\""
type: bug
complexity: S
priority: P1
pr: 497
shipped: 2026-10-03
stage: DONE
---

# Claude Restart/Revive fails when the session cwd contains "_"

- **Issue:** #494
- **Type:** bug
- **Complexity:** S
- **Priority:** P1
- **Exec plan:** [docs/exec-plans/completed/494-claude-restart-revive-fails-when-the-session-cwd-co.md](../exec-plans/completed/494-claude-restart-revive-fails-when-the-session-cwd-co.md)

## Problem

`encodeClaudeProjectDir` (`internal/agent/claude.go`) folds only `/`, `.` and
`:` to `-`, but Claude folds **every** non-alphanumeric character of the cwd
when it names `~/.claude/projects/<dir>/`. For a cwd containing `_` (every
macOS `$TMPDIR`, many repo names) or a space, `claudeSessionExists` misses the
transcript, `claudeResumeArgs` falls back to `claude --session-id <id>`, and
Claude exits with `Error: Session ID <id> is already in use.` Restart and
Revive of such a session fail, and transcript search finds nothing. It is
also follow-up 0 (finding F1) in `docs/design-docs/acp-workflows.md`.

## Desired behavior

Restart and Revive of a Claude session resume its conversation whatever
characters the cwd contains, and transcript lookup finds the same file Claude
wrote.

## Success criteria

- `encodeClaudeProjectDir` matches Claude's own encoder: every UTF-16 code
  unit outside `[A-Za-z0-9]` becomes `-`, and a result longer than 200
  characters is cut to 200 plus `-` and the base-36 hash Claude appends.
- A test covers a cwd with `_` and a space, a non-ASCII cwd, and a cwd long
  enough to be truncated, with expected values derived from Claude's encoder.
- `claudeResumeArgs` returns `--resume` for a cwd containing `_` when the
  transcript exists on disk (test against a temp HOME, not a stub).
- The ACP probe's mirror (`scripts/acp-probe/agents.mjs`) no longer documents
  the divergence as current behaviour.

## Non-goals

- Symlink resolution of the cwd (e.g. macOS `/tmp` vs `/private/tmp`).
- Migrating or relocating existing transcripts.
- Any other agent's directory encoding (pi keeps `.`; untouched).

## Notes

Found by `scripts/acp-probe` (spec 492, PR #493) on claude 2.1.288. Claude's
encoder, read from the 2.1.288 bundle: `k(e)=e.replace(/[^a-zA-Z0-9]/g,"-")`;
if longer than 200, `k(e).slice(0,200)+"-"+Math.abs(h(e)).toString(36)`,
where `h` is the Java-style `(h<<5)-h+charCode|0` over the raw cwd.

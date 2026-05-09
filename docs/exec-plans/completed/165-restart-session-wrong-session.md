# Restart loads the wrong session when multiple share a worktree/directory

- **Spec:** [docs/product-specs/165-restart-session-wrong-session.md](../../product-specs/165-restart-session-wrong-session.md)
- **Issue:** #165
- **Stage:** DONE
- **Status:** completed

## Summary

When two or more Hive sessions live in the same cwd/worktree (e.g. ⌘P duplicate, or `--use-worktree=false` adoption), restarting one of them runs an agent CLI's "continue most recent" command in that directory, which the agent CLI itself disambiguates by mtime — not by the Hive session id. The user clicks Restart on B and ends up reattached to A's conversation. The fix is to make the resume command session-id-specific.

## Research

### Restart flow (id-based at the Hive layer — correct)

- `cmd/hivegui/frontend/src/main.js:2171` — `restartActiveSession()` reads `state.activeId` and calls `RestartSession(s.id)`. ID-based.
- `cmd/hivegui/app.go` — `RestartSession(id)` sends `FrameRestartSession` with the id.
- `internal/daemon/daemon.go` — handles `FrameRestartSession`, calls `d.reg.Restart(req.SessionID)`.
- `internal/registry/registry.go:559` — `Registry.Restart(id)` looks up by id, tears down the PTY, calls `Revive(id, opts)`.
- `internal/registry/registry.go:477` — `Revive(id, opts)` re-spawns in the entry's slot. ID-based.

The registry never confuses two entries; the restart targets the right Hive Entry.

### The actual bug — agent ResumeCmd is path-scoped, not session-scoped

`internal/agent/agent.go:60–86` — every agent's `ResumeCmd` is a "continue last in cwd" command:

- Claude: `claude --continue` (continues most recent conversation in cwd)
- Codex: `codex resume --last` (most recent recorded session)
- Gemini: `gemini --continue`
- Copilot: `copilot --resume`

`Registry.Restart` (registry.go:590-602) builds opts with `def.ResumeCmd` and inherits the entry's worktree path as cwd (set in `Revive`, registry.go:511-513). When two Hive entries share that worktree path, both invoke an identical `claude --continue` in the same dir — claude attaches to whichever conversation it considers latest, which is the one most recently active, regardless of which Hive session was clicked.

This matches the report exactly.

### How sessions end up sharing a cwd

- `Registry.Create` (registry.go:301-310) explicitly **adopts** another session's worktree path when a new non-`UseWorktree` session's cwd matches an existing entry's `WorktreePath` in the same project. The adoption path is intentional (⌘P duplicate keeps the worktree alive until the last session exits).
- A user can also create two regular sessions whose cwds happen to be the same project root, no worktree at all — both ResumeCmd's land in the same dir.

### Per-id resume IS supported by the major agents

- Claude: `claude --resume <session-id>` resumes a specific conversation by uuid; `claude --session-id <uuid>` lets the *caller* choose the id at first launch (verified via `claude --help`).
- Codex: `codex resume <SESSION_ID>` accepts a UUID/thread name (verified via `codex resume --help`).
- Gemini, Copilot, Aider: not yet verified. Plan must either degrade gracefully or document the gap.

### Constraints / dependencies

- `Entry` is persisted via `persistEntryLocked` (registry.go:367, 450). Adding an `AgentSessionID` field requires a small persistence-format extension; readers must tolerate missing fields for old entries (already the JSON convention here).
- `Def.ResumeCmd` is currently a static `[]string`. To inject an id we either move resume-command construction into a function on `Def` (e.g. `def.ResumeArgs(sessionID string) []string`) or build it at the Restart call site keyed on agent ID.
- For agents without per-id resume support, we need an explicit fallback (use `Cmd` to start fresh, or keep `ResumeCmd` and accept the ambiguity with a warning).
- Setting `--session-id` at first spawn (Claude only) is the cleanest path: Hive picks the UUID, persists it on the Entry, and uses it on every restart. Codex needs a different approach — capture its UUID after first run (parse from `~/.codex/sessions/` or similar) — which is more invasive.

## Approach

Make the agent's resume command session-id-specific by embedding the Hive entry id (already a UUID, generated at `internal/registry/registry.go:266` via `google/uuid`) into both the first-launch command and the resume command, for any agent whose CLI supports it.

Why this beats alternatives:

- Forcing each session into its own subdirectory would break the explicit `WorktreePath` adoption logic (`registry.go:301-310`) that lets ⌘P duplicates share a worktree.
- Capturing each agent's auto-generated session id post-spawn (parsing `~/.claude/projects/`, `~/.codex/sessions/`, …) is fragile and per-agent.
- Pre-assigning the Hive id at spawn is a single-line change for Claude (the only agent verified to expose `--session-id`). Codex/Gemini/Copilot retain current behavior — documented as a known limitation and tracked for follow-up.

The agent catalog (`internal/agent/agent.go`) gains two optional fields per `Def`:

- `SessionIDFlag string` — appended to `Cmd` at first spawn as `[flag, hiveID]`. Empty = not supported, no-op.
- `ResumeArgs func(id string) []string` — produces the resume argv for a given id. When nil or id is empty, restart falls back to `ResumeCmd` (today's behavior).

Scope: wire Claude. Codex/Gemini/Copilot left unchanged in this PR (open question below).

### Files to change

1. `internal/agent/agent.go` — add `SessionIDFlag string` and `ResumeArgs func(string) []string` to `Def`. Set them on `IDClaude`: `SessionIDFlag: "--session-id"`, `ResumeArgs: func(id string) []string { return []string{"claude", "--resume", id} }`.
2. `internal/registry/registry.go`:
   - In `Create` (around the existing `cmd` resolution at line 384-389): after agent lookup, if `def.SessionIDFlag != ""`, append `[def.SessionIDFlag, id]` to `cmd`. The Hive `id` (line 266) is already a UUID, reusable directly.
   - In `Restart` (line 591-597): if `def.ResumeArgs != nil`, call `def.ResumeArgs(id)`; else use `def.ResumeCmd`; else `def.Cmd`. The id is already in scope as the function argument.
3. `internal/agent/agent_test.go` (new tests, file may exist) — verify Claude's Def has the new fields and `ResumeArgs(uuid)` returns the expected argv.
4. `internal/registry/registry_test.go` — three regression tests (named below).
5. `CHANGELOG.md` — `[Unreleased]` entry: "Fixed: restarting one of multiple sessions sharing a worktree no longer reattaches to a sibling's conversation (Claude only; codex/gemini/copilot tracked separately)."

### New files

None.

### Tests

- `internal/agent/agent_test.go::TestClaudeDefSupportsSessionID` — asserts `Def{IDClaude}.SessionIDFlag == "--session-id"` and `ResumeArgs("abc") == ["claude","--resume","abc"]`.
- `internal/registry/registry_test.go::TestCreateAppendsSessionIDForClaude` — creates a claude session, asserts `session.Options.Cmd` ends with `[--session-id, <entry.ID>]`. (Requires session.Start to be observable in test — use existing test patterns; if session.Start is hard to intercept, expose the cmd via a test-only hook or assert via a fake `session.Starter`. Use whatever pattern is already in use for `TestRegistryCreate*`.)
- `internal/registry/registry_test.go::TestRestartUsesResumeArgsForClaude` — restart on a claude entry produces argv `[claude, --resume, <entry.ID>]`, NOT `[claude, --continue]`.
- `internal/registry/registry_test.go::TestRestartFallsBackToResumeCmdForCodex` — restart on a codex entry still produces `[codex, resume, --last]` until codex support lands.
- `internal/registry/registry_test.go::TestTwoClaudeSessionsSameWorktreeRestartIndependent` — create two claude entries adopting the same `WorktreePath`; restarting each yields argv with that entry's distinct id.

If `session.Start` is not currently observable from registry tests, the smallest test-friendly seam is a package-level `var startSession = session.Start` so tests can swap it. Mention this in the Decision log if introduced.

## Decision log

- **2026-05-08** — Reuse the Hive entry UUID as the agent session id rather than adding `Entry.AgentSessionID`. Why: registry id is already a uuid (registry.go:266); avoids a persistence-format change and a backfill story for existing entries.
- **2026-05-08** — Scope this PR to Claude. Why: Claude is the only agent with a verified `--session-id`-at-spawn flag. Codex supports `resume <UUID>` but its id is auto-generated, requiring post-spawn capture — out of scope. Track Gemini/Copilot/Codex as a follow-up issue.

## Decision log

## Progress

- **2026-05-08** — Spec drafted, triage approved (bug, S → reconsider M during plan), research complete: root cause is path-scoped ResumeCmd colliding when sessions share cwd.
- **2026-05-08** — Implemented Claude path: agent.Def gains SessionIDFlag + ResumeArgs; registry pins entry id at spawn and resumes by id. Added 5 tests via package-level startSession seam. All internal tests pass.
- **2026-05-08** — PR #166 opened. Converged via /hs-ralph-loop in 1 iteration (APPROVE).

## Open questions

- Do we widen scope to all four agents now, or ship the fix for Claude+Codex (the two with verified per-id resume) and document the rest as known-limited?
- Is the cleanest path to use `--session-id <uuid>` at *first launch* (only Claude supports this directly), or to capture each agent's auto-generated id after spawn?

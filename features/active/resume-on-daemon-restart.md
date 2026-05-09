# Feature: Resume conversations on daemon restart

- **GitHub Issue:** —
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P1
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

## Research

### Relevant code

- `internal/agent/agent.go:28-35` — `Def` already carries `Cmd` and
  `ResumeCmd` ([]string). Built-ins set `ResumeCmd` for Claude
  (`claude --continue`), Codex (`codex resume --last`), Gemini
  (`gemini --continue`), Copilot (`copilot --resume`). Aider has none.
- `internal/registry/registry.go:477-545` — `Revive` rebuilds opts.Cmd
  from `def.Cmd` only (line 498). It never consults `ResumeCmd`. This
  is the gap.
- `internal/registry/registry.go:559-633` — `Restart` already uses
  `def.ResumeCmd` (line 592-596) and is the model to follow.
- `internal/daemon/daemon.go:87-95` — daemon startup loops persisted
  sessions and calls `reg.Revive(info.ID, cfg.BootstrapSession)`. This
  is the entry point that needs to recover conversation state.
- `internal/registry/persist.go:11-22` — `MetaFile` JSON written to
  `<session_dir>/session.json`. Needs a new optional `conversation_id`
  field.
- `internal/registry/registry.go:38-50` — in-memory `Entry` mirrors
  `MetaFile`; needs matching `ConversationID` field.

### Agent on-disk conversation stores

- **Claude:** `~/.claude/projects/<encoded-cwd>/<UUID>.jsonl`. Filename
  is the conversation ID. Multiple per cwd.
- **Codex:** `~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<UUID>.jsonl`.
  UUID at filename tail is the rollout/session ID.
- **Gemini / Copilot:** locators TBD; `--continue` / `--resume` resume
  the most recent in cwd, which is the duplicate-session collision
  hazard the spec calls out.
- **Aider:** no resume command. Leave on plain `Cmd`.

### Constraints

- Resume by ID needs to be a per-agent builder (CLI flag varies) —
  extend `agent.Def` with a `ResumeWithIDCmd func(id string) []string`
  rather than baking the flag into a string template.
- Locator must run while the agent is still writing (not at revive
  time — process is gone). Best window: shortly after start, then
  refresh on session-exit. A simple heuristic: scan the agent's
  on-disk store for files created/modified after the session's
  `Created` timestamp and pick the newest. Persist the ID once found.
- Two hive sessions sharing a cwd (⌘P duplicate) must not collide.
  Recording the ID at first-write time, before the second duplicate
  starts, is sufficient if locator runs eagerly post-start. Race:
  if both start in quick succession before either has written, both
  may attribute the same JSONL file. Acceptable risk for v1; document
  it.
- Worktree-backed sessions: cwd differs per session, so the
  `--continue` collision doesn't apply for them. Keep ID-based resume
  uniformly anyway for consistency.

## Plan

### Approach

Ship both slices, Slice A first (extracted helper makes B's diff
trivial), then Slice B in a follow-up PR.

**Slice A — extract `resumeArgsForEntry` and wire it into `Revive`.**
Pull the ResumeCmd-or-Cmd selection out of `Restart` (registry.go:591-597)
into a private helper `resumeArgsForEntry(e *Entry) []string`. Call it
from both `Restart` and `Revive`. This eliminates the duplication
before Slice B adds a third arm to the conditional. Document the
duplicate-cwd collision as a known caveat for non-ID-aware agents.

**Slice B — per-session conversation ID.** Adds (a) a parallel
`agent.ResumeArgsWithID(id ID, convID string) []string` *function*
(not a `Def` field, keeping `Def` plain data — see CQ-1), (b) a
parallel `agent.ConversationIDLocator` registry keyed by agent ID,
(c) `ConversationID` on `Entry`/`MetaFile`, (d) a locator that runs
on Create *and* re-runs on session activity (write events) and on
Close — not only post-spawn (see ARCH-1), (e) `Revive` and `Restart`
both prefer ResumeArgsWithID when an ID is persisted.

### Failure-mode policy

Resume-by-ID failure is **fail-loud**. If `claude --resume <id>`
errors at spawn (stale ID, agent CLI moved, layout changed), the
entry stays dead with `LastError` set. User sees the error in the
sidebar and clicks Restart Session. No silent fallback chain — that
would mask broken installs and resume-by-ID regressions.

### Files to change (Slice A)

1. `internal/registry/registry.go` —
   - Add private helper `resumeArgsForEntry(e *Entry) []string`
     returning `def.ResumeCmd` when set, else `def.Cmd`. (Slice B
     will extend it to prefer `ResumeArgsWithID`.)
   - In `Restart` (lines 591-597), replace the inline conditional
     with a call to `resumeArgsForEntry`.
   - In `Revive` (lines 496-500), call `resumeArgsForEntry` when
     `opts.Cmd` is empty.
   - Update the `Revive` doc comment (lines 458-476) to describe
     the resume behavior and the duplicate-cwd caveat. Drop the
     "starts blank" reference to Phase 1.7.
2. `internal/registry/registry_test.go` (new file
   `resume_args_test.go` preferred) —
   - `TestResumeArgsForEntry_UsesResumeCmd_WhenSet` — pure unit test
     against the helper, no PTY, no `session.Start`.
   - `TestResumeArgsForEntry_FallsBackToCmd_WhenResumeEmpty` — Aider
     and shell.
   - `TestResumeArgsForEntry_UnknownAgent` — empty argv (Revive
     callers fall back to opts.Cmd).
   - `TestRevive_UsesResumeCmd_Integration` — exercises full
     `Revive` with shell agent only (avoids LLM CLI dependence).

### Files to change (Slice B)

3. `internal/agent/agent.go` — **no change to `Def` shape**. Keep
   `Def` as plain data.
4. `internal/agent/resume.go` (new) — exports
   `ResumeArgsWithID(id ID, convID string) []string`. Returns
   `["claude","--resume",convID]` for Claude,
   `["codex","resume",convID]` for Codex. Empty slice for agents
   that don't support ID-based resume (callers fall through to
   `ResumeCmd`).
5. `internal/agent/locator.go` (new) — defines
   `Locator func(agentDataRoot, cwd string, since time.Time) (string, error)`
   and a registry `var locators = map[ID]Locator{...}` plus exported
   `LocatorFor(id ID) Locator`. Implement `claudeLocator` (scans
   `<agentDataRoot>/projects/<encoded-cwd>/*.jsonl`, returns newest
   filename-without-extension whose mtime ≥ `since`) and
   `codexLocator` (walks `<agentDataRoot>/sessions/YYYY/MM/DD/`
   bounded by depth=4, returns trailing-UUID of newest file ≥
   `since`). Both accept `agentDataRoot` for testability — TEST-2.
   Production callers pass `os.UserHomeDir()/.claude` and
   `os.UserHomeDir()/.codex`.
6. `internal/registry/persist.go` — add
   `ConversationID string \`json:"conversation_id,omitempty"\``
   to `MetaFile`.
7. `internal/registry/registry.go` —
   - Add `ConversationID string` to `Entry`.
   - Round-trip ConversationID in load (lines 208-214, 229-235) and
     `persistEntryLocked` (line 902-915).
   - In `Create`, after `session.Start`, spawn a single locator
     goroutine that polls every 500ms for up to 5s. If the locator
     returns "" the goroutine bails silently.
   - **Also** invoke the locator on session-write activity (hook
     into the `session.Session` write path, or refresh on a debounced
     timer while the entry is alive) and once more on Close — to
     cover the "user stayed idle for 2h" case (ARCH-1). Cheapest
     concrete approach: a single per-Registry janitor goroutine on a
     30-second tick that walks alive entries with empty ConversationID
     and runs their locator.
   - Extend `resumeArgsForEntry` to prefer
     `agent.ResumeArgsWithID(id, e.ConversationID)` when both
     are set; fall through to `ResumeCmd`/`Cmd`.
   - Resume-by-ID failure: no fallback at the registry level. Let
     `session.Start` error propagate; Revive sets `LastError` and
     broadcasts; user clicks Restart from the GUI.
8. `internal/registry/resume_args_test.go` — extend with
   `TestResumeArgsForEntry_PrefersResumeArgsWithID_WhenIDSet` and
   `TestRestart_FallsBackToResumeCmd_WhenNoConvID` (TEST-3
   regression for Slice B touching Restart).
9. `internal/registry/registry_test.go` (or
   `conversation_id_test.go`) — `TestMetaFile_ConversationID_Roundtrip`,
   `TestCreate_LocatorPersistsID_OnFound`,
   `TestCreate_LocatorTimeout_LeavesIDEmpty`.
10. `internal/agent/locator_test.go` (new) —
    `TestClaudeLocator_PicksMostRecentSinceStart`,
    `TestClaudeLocator_LayoutMissing_ReturnsEmptyNoError`,
    `TestCodexLocator_NestedDateDirs`,
    `TestLocator_SymlinkLoop_Bounded` (defensive against ARCH-2
    layout drift).

### Tests (consolidated)

Slice A:
- `TestResumeArgsForEntry_UsesResumeCmd_WhenSet`
- `TestResumeArgsForEntry_FallsBackToCmd_WhenResumeEmpty`
- `TestResumeArgsForEntry_UnknownAgent`
- `TestRevive_UsesResumeCmd_Integration` (shell agent only)

Slice B:
- `TestResumeArgsForEntry_PrefersResumeArgsWithID_WhenIDSet`
- **`TestRestart_FallsBackToResumeCmd_WhenNoConvID`** — REGRESSION
- `TestMetaFile_ConversationID_Roundtrip`
- `TestCreate_LocatorPersistsID_OnFound`
- `TestCreate_LocatorTimeout_LeavesIDEmpty`
- `TestClaudeLocator_PicksMostRecentSinceStart`
- `TestClaudeLocator_LayoutMissing_ReturnsEmptyNoError`
- `TestCodexLocator_NestedDateDirs`
- `TestLocator_SymlinkLoop_Bounded`
- `TestJanitor_RefreshesIDForLongIdleSession` — covers ARCH-1
  (locator re-runs while session is alive)

### Known limitations / risks (post-revision)

- **ARCH-2 (filesystem coupling).** `~/.claude/projects/` and
  `~/.codex/sessions/` layouts are owned by Anthropic / OpenAI and
  can change without warning. Mitigation: locator returns "" on
  layout mismatch, revive falls through to `ResumeCmd`. Not bulletproof
  but graceful. v2 follow-up: detect layout-version markers if/when
  upstream adds them.
- **Locator wrong-UUID race.** Two duplicated sessions starting
  within milliseconds of each other before either has written may
  attribute the same JSONL. Documented v1 limitation. Two-phase
  confirmation (size grew between two locator hits) is a possible
  v2 hardening.
- **Aider stays on plain `Cmd`.** No resume command at all for Aider.
  Documented in CHANGELOG.
- **Gemini / Copilot use `--continue` / `--resume` only**, so
  duplicate-cwd collisions remain for those agents post-Slice-B.
  Documented.

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| CEO Review | `/plan-ceo-review` | Scope & strategy | 0 | — | not run |
| Codex Review | `/codex review` | Independent 2nd opinion | 0 | — | not run |
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 1 | CLEAR (PLAN) | 9 issues raised, 8 incorporated, 1 deferred to v2 (ARCH-2 layout-version detection); 1 critical gap acknowledged as v1 limitation (locator wrong-UUID race) |
| Design Review | `/plan-design-review` | UI/UX gaps | 0 | — | not applicable (backend) |
| DX Review | `/plan-devex-review` | Developer experience gaps | 0 | — | not applicable |

- **UNRESOLVED:** 0
- **VERDICT:** ENG CLEARED — ready to implement.

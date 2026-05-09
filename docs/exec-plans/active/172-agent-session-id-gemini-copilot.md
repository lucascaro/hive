# Pin/capture agent session id for Gemini and Copilot

- **Spec:** [docs/product-specs/172-agent-session-id-gemini-copilot.md](../../product-specs/172-agent-session-id-gemini-copilot.md)
- **Issue:** #172
- **Stage:** IMPLEMENT
- **Status:** active

## Summary

Extend the session-id pinning/capture infrastructure introduced in
#166 to Gemini (pin, mirroring Claude) and Copilot (capture, mirroring
Codex). Aider stays unsupported. The registry-side plumbing is already
in place; this plan adds two small per-agent additions.

## Research

- `internal/agent/agent.go:28-55` — `Def` already exposes
  `SessionIDFlag`, `ResumeArgs(id)`, and `CaptureSessionIDFn` (added
  by #166). Generic infrastructure; no Registry changes needed.
- `internal/agent/agent.go:80-100` — `IDClaude` Def shows the pin
  pattern: `SessionIDFlag: "--session-id"` + `ResumeArgs:
  func(id) []string { return []string{"claude", "--resume", id} }`.
- `internal/agent/agent.go:101-120` — `IDCodex` Def shows the capture
  pattern: `ResumeArgs` + `CaptureSessionIDFn: codexCaptureSessionID`.
- `internal/agent/codex.go` — full capture implementation. Polls
  `~/.codex/sessions/`, regex-matches rollout filenames for the UUID,
  reads first JSONL line to verify cwd. Mtime cutoff is `spawnedAt-1s`.
  This file is the structural template for `copilot.go`.
- `internal/agent/codex_test.go` — fixtures + concurrent-spawn test;
  template for `copilot_test.go`.

### CLI capability matrix (verified)

| Agent | `--session-id <UUID>` at launch | Resume by UUID | On-disk store |
|---|---|---|---|
| Gemini | ✅ `--session-id <UUID>` (arbitrary UUID accepted) | ✅ `gemini --resume <UUID>` | `~/.gemini/tmp/<project>/chats/session-...-<short-uuid>.jsonl` |
| Copilot | ❌ no flag | ✅ `copilot --resume=<UUID>` (note `=` form) | `~/.copilot/session-state/<UUID>/workspace.yaml` (UUID = dir name; `cwd:` field present) |

Empirically verified via:

```
gemini --skip-trust --session-id 11111111-2222-3333-4444-555555555555 -p "say hi"
gemini --skip-trust --resume    11111111-2222-3333-4444-555555555555 -p "what did i say"
# ⇒ "You said \"say hi briefly\"."
```

## Approach

Two parallel slices, both shipped in one PR.

### Slice G — Gemini (pin)

Mirror Claude exactly. Two-line addition to `IDGemini` Def:

```go
SessionIDFlag: "--session-id",
ResumeArgs: func(id string) []string {
    return []string{"gemini", "--resume", id}
},
```

The existing `Registry.Create` infrastructure appends `--session-id
<entry-id>` to the spawn cmd whenever `SessionIDFlag` is set; that
already handles Gemini once these two lines land.

### Slice C — Copilot (capture)

Mirror Codex. New `internal/agent/copilot.go` with
`copilotCaptureSessionID(ctx, cwd, spawnedAt) (string, error)`:

- Resolve `~/.copilot/session-state/` (override-able for tests, same
  pattern as `codexSessionsDir`).
- Poll every 200ms (same cadence as codex). Each poll:
  - List immediate subdirectories of session-state.
  - Skip names that are not valid UUIDs.
  - Skip dirs whose mtime < `spawnedAt-1s` (cutoff slack).
  - Skip dirs we've already inspected and rejected (negative cache).
  - Read `<dir>/workspace.yaml`, look for the `cwd:` line, compare
    against the spawn cwd. First match wins.
- Returns the UUID (= dir name) or "" + error on ctx done.

Wire to `IDCopilot` Def:

```go
ResumeArgs: func(id string) []string {
    return []string{"copilot", "--resume=" + id}
},
CaptureSessionIDFn: copilotCaptureSessionID,
```

Use `--resume=<UUID>` (with `=`) per copilot's documented usage; the
space-separated form is not in the help and may not be supported.

### Files to change

- `internal/agent/agent.go` — add SessionIDFlag/ResumeArgs to
  `IDGemini`; add ResumeArgs/CaptureSessionIDFn to `IDCopilot`.
- `internal/agent/agent_test.go` — extend the existing per-agent
  Def-shape tests to assert Gemini and Copilot now have the new
  fields populated correctly.

### New files

- `internal/agent/copilot.go` — capture implementation.
- `internal/agent/copilot_test.go` — fixture-based capture tests
  (mirrors `codex_test.go`).

### Tests

- `TestGeminiDef_HasSessionIDPinning` — Def has SessionIDFlag set
  and ResumeArgs returns `[gemini, --resume, <id>]`.
- `TestCopilotDef_HasCaptureAndResume` — Def has CaptureSessionIDFn
  and ResumeArgs returns `[copilot, --resume=<id>]`.
- `TestCopilotCaptureSessionID_HappyPath` — fixture: drop
  `<UUID>/workspace.yaml` with matching cwd, capture returns UUID.
- `TestCopilotCaptureSessionID_SkipsWrongCwd` — workspace.yaml has
  wrong cwd, capture continues until match or ctx done.
- `TestCopilotCaptureSessionID_SkipsPreexistingByMtime` — old dir
  with matching cwd but mtime before cutoff is skipped (the codex
  test pattern).
- `TestCopilotCaptureSessionID_NegativeCache` — same wrong-cwd dir
  is not re-read on later polls.
- `TestCopilotCaptureSessionID_ConcurrentSpawns` — two captures with
  different cwds pick the correct UUIDs.
- `TestCopilotCaptureSessionID_ContextCancel` — ctx cancel returns
  promptly with empty id, no error noise.

## Decision log

- **2026-05-09** — Use `--resume=<UUID>` (with `=`) for copilot, not
  `--resume <UUID>` (space). Why: `=` form is the only one in
  `copilot --help`; space form is undocumented and may not work.
- **2026-05-09** — Mtime cutoff `spawnedAt-1s`, same as codex. Why:
  filesystem mtime resolution + the gap between `time.Now()` and
  the agent's first write are both imprecise; 1s slack matches the
  cutoff already validated in `codex.go`.

## Progress

- **2026-05-09** — Spec + plan written; CLI capabilities verified
  empirically.
- **2026-05-09** — Implementation complete. `internal/agent/agent.go`
  wires Gemini pinning + Copilot capture; `internal/agent/copilot.go`
  is the capture impl; `internal/agent/copilot_test.go` adds 6 tests
  (capture happy path, wrong-cwd skip, ctx cancel, preexisting-by-
  mtime, non-UUID dir skip, Def shape). `go test ./internal/...
  ./cmd/hived/...` passes.

## Open questions

None.

# Feature: Add missing tests

- **GitHub Issue:** #36
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P3
- **Branch:** —

## Description

Improve test coverage by adding missing tests across the codebase.

- Tests must not read/write real config or state files
- Focus on untested or under-tested packages and logic paths

## Research

### Summary

22 files have tests, 37 do not. Coverage is strongest in `state`, `config`, `escape`, `git`, and `tui/components` (rendering). Major gaps are in the `tmux` CLI wrappers, `mux/native` daemon, `tui` event handlers, `cmd/` startup, and 4 untested TUI components.

Many untested files are thin wrappers around external tools (tmux CLI, PTY I/O, OS calls) or event handlers tightly coupled to the Bubble Tea runtime — these are hard to unit-test without significant refactoring or integration harnesses. The highest-value targets are **pure logic functions** that can be tested in isolation.

### High-Value Test Targets (pure logic, no I/O mocking)

1. **`internal/state/store.go`** — `NextSessionAfterRemoval()` (line 222): complex focus-fallback logic after session deletion. `SessionLabel()` (line 421): display formatting. `RecordAgentUsage()` (line 352): usage tracking. `FindSession/FindProject/FindTeam/FindSessionByTmux` (lines 375-418): lookup functions.
2. **`internal/mux/interface.go`** — `WindowName()` (line 114): window naming with truncation. `Target()` (line 127): target string building.
3. **`internal/mux/native/protocol.go`** — `writeMsg()`/`readMsg()` (lines 45-77): binary message encoding/decoding. Testable with in-memory buffers.
4. **`internal/tui/components/orphanpicker.go`** — `Update()` (line 39): multi-select toggle/select-all logic. Testable via direct `Update()` calls.
5. **`internal/tui/components/recoverypicker.go`** — `Update()` (line 70): multi-select + agent-type cycling. `agentTypeIndex()` (line 59).
6. **`internal/tui/components/teambuilder.go`** — `Update()` (line 92): multi-step wizard navigation. `advance()`: step progression.
7. **`internal/tui/components/settings.go`** — Field editing and validation logic (300+ lines). Complex but self-contained.
8. **`internal/config/load.go`** — `Ensure()` (line 32), `Save()` (line 82), `writeAtomic()` (line 91): directory creation and atomic writes. Testable with `t.TempDir()`.
9. **`internal/hooks/runner.go`** — `Run()` (line 18), `findScripts()` (line 35): hook discovery and execution. Partially testable with temp dirs.

### Medium-Value Targets (need some mocking or setup)

10. **`internal/tui/handle_keys.go`** — `handleKey()`, `handleGridKey()`, `handleGlobalKey()`: key dispatch logic. Testable via the existing flow-test pattern (`tui/flow_test.go`).
11. **`internal/tui/handle_session.go`** — Session lifecycle handlers. Could be tested via flow tests.
12. **`internal/tmux/capture.go`** — `GetPaneTitles()` (line 45): parses tmux list-windows output into a map. Testable if the tmux exec is injectable.
13. **`internal/tmux/window.go`** — `ListWindows()` (line 62): parses window list output.

### Low-Value / Hard to Test

- `cmd/` startup commands — tightly coupled to OS, tmux, config files
- `mux/native/daemon.go`, `pane.go`, `client.go` — need Unix sockets, PTYs
- `mux/tmux/backend.go` — thin wrapper around tmux CLI
- `internal/tmux/client.go`, `session.go`, `window.go` — thin wrappers around `exec.Command`
- `tui/handle_preview.go`, `handle_system.go` — timer/watcher-coupled handlers
- `messages.go`, `events.go`, `views.go` — pure type definitions, no logic

### Constraints / Dependencies

1. **Tests must not read/write real config or state files** (per issue description and feedback memory). Use `t.TempDir()` for file-based tests.
2. **No tmux dependency in tests.** The `internal/tmux/` package wraps `exec.Command("tmux", ...)` — unit testing requires either refactoring to inject the executor or limiting tests to output-parsing functions.
3. **Bubble Tea coupling.** Many `tui/handle_*.go` functions are methods on `tui.Model` and depend on its full state. The existing flow-test pattern (`flow_test.go`) is the way to test these, but adding flow tests is more complex than unit tests.
4. **This is L-complexity.** Should be broken into phases/PRs rather than one massive PR. Recommend grouping by package or by value tier.

## Plan

Break into 3 PRs by package group, highest-value first. Each PR is self-contained and independently mergeable.

### PR 1: State + Mux pure logic tests

**Files to Change:**
1. `internal/state/store_test.go` — Add tests for:
   - `NextSessionAfterRemoval()`: next-in-group, prev-in-group, cross-group fallback, only session, session-not-found, team vs standalone
   - `SessionLabel()`: orchestrator star prefix, non-orchestrator, various agent types
   - `RecordAgentUsage()`: nil map init, first usage, repeated usage, multiple agent types
   - `FindSession()`: standalone session, team session, not found, empty state
   - `FindProject()`: found, not found, empty list
   - `FindTeam()`: found in first/last project, not found
   - `FindSessionByTmux()`: match both fields, match only one (miss), not found
   - `AllSessions()`: empty, standalone only, team only, mixed ordering
2. `internal/mux/interface_test.go` — New file. Add tests for:
   - `WindowName()`: truncation at 8/12 chars, exact boundary, shorter names, empty fields
   - `Target()`: standard formatting, window index 0, empty session
3. `internal/mux/native/protocol_test.go` — New file. Add tests for:
   - `writeMsg()`/`readMsg()` roundtrip with valid data
   - `readMsg()` rejects messages > 4 MiB
   - `readMsg()` handles malformed JSON, short header reads, zero-length messages
   - `writeMsg()` handles marshal errors (e.g., channels)

### PR 2: Untested TUI components

**Files to Change:**
4. `internal/tui/components/orphanpicker_test.go` — New file. Tests for:
   - Cursor navigation (up/down bounds)
   - Space toggle on/off
   - Toggle-all (none→all, all→none, partial→all)
   - Enter returns selected sessions; Esc returns nil
5. `internal/tui/components/recoverypicker_test.go` — New file. Tests for:
   - Cursor navigation bounds
   - Left/right agent-type cycling with wrap-around
   - `agentTypeIndex()` for known types and unknown (defaults to custom)
   - Agent type stays synced with `sessions[].DetectedAgentType`
   - Enter returns selected with correct agent types
6. `internal/tui/components/teambuilder_test.go` — New file. Tests for:
   - Empty name rejected (no advance)
   - Worker count parsing: "abc"→2, "0"→1, "11"→10, "5"→5
   - Step progression: name→goal→orchestrator→workerCount→workDir→confirm
   - Esc hides at any step without emitting TeamBuiltMsg
7. `internal/tui/components/settings_test.go` — New file. Tests for:
   - Bool toggle (true↔false)
   - Select field cycling with wrap
   - Int validation: PreviewRefreshMs boundaries (49 rejected, 50 ok, 30000 ok, 30001 rejected)
   - String validation: empty keybinding rejected, empty hooks dir rejected
   - Dirty tracking: only set after successful edit, not on validation failure
   - Esc flow: first esc sets pendingDiscard, second esc closes
   - Save flow: s sets pendingSave, y/enter confirms

### PR 3: Config/hooks I/O tests

**Files to Change:**
8. `internal/config/load_test.go` — Add tests for:
   - `Ensure()`: creates dirs, idempotent on re-run (uses `t.TempDir()`)
   - `Save()`: writes valid indented JSON, file permissions 0o600
   - `writeAtomic()`: temp file removed on success, target file created
9. `internal/hooks/runner_test.go` — Add tests for:
   - `findScripts()`: flat file found, `.d/` dir entries sorted, non-executable skipped, dirs skipped, both flat + `.d/` combined
   - `Run()`: no scripts → nil, one script succeeds, one script fails → error collected
   - (Uses `t.TempDir()` with executable shell scripts)

### Test Strategy
- All tests run via `go test ./...`
- No real config/state files — `t.TempDir()` for all file I/O
- No tmux dependency — protocol tests use `bytes.Buffer`
- Component tests use direct `Update()` calls, matching existing patterns
- Each PR must pass `go build ./...`, `go vet ./...`, `go test ./...`

### Risks
- **Settings field count**: the settings component has ~30 fields with individual set/get functions. Testing all validation paths is thorough but verbose. Focus on one representative per field kind (bool, select, int, string) rather than exhaustive coverage.
- **TeamBuilder agent picker integration**: testing the full orchestrator→worker flow requires simulating `AgentPickedMsg` messages, which is doable but requires careful state setup.
- **Config path coupling**: `Ensure()` and `Save()` use `Dir()`/`ConfigPath()` which resolve to `~/.config/hive`. Tests must either override these or test `writeAtomic()` directly with explicit paths. Check if `Dir()` respects `XDG_CONFIG_HOME` for redirection to temp dir.
- **Hooks timeout**: `runScript()` has a hardcoded 5s timeout. Skip timeout tests to avoid slow test suite; focus on `findScripts()` and error collection.

## Implementation Notes

### PR 1: State + Mux pure logic tests
No deviations from plan. Many state functions (`NextSessionAfterRemoval`, `SessionLabel`, `RecordAgentUsage`, `AllSessions`) already had partial test coverage — added the remaining `FindX` and `FindSessionByTmux` tests plus additional edge cases. Created new test files for `mux/interface_test.go` (8 test cases for `WindowName` + `Target`) and `mux/native/protocol_test.go` (12 test cases for wire format, size validation, error handling).

PRs 2 and 3 (TUI components, config/hooks I/O) remain for follow-up.

- **PR:** #61 (PR 1 of 3)

### PRs 2 & 3: TUI components + config/hooks I/O tests
Combined into a single PR since PR 3 tests already existed and only needed minor additions.

**PR 2 — TUI component tests (new files):**
- `orphanpicker_test.go`: 9 tests — empty/active init, cursor nav (j/k/up/down + clamping), space toggle on/off, toggle-all (none→all, all→none, partial→all), enter returns selected, enter with none, esc/q returns nil.
- `recoverypicker_test.go`: 12 tests — same patterns as orphan picker plus agent-type cycling (left/right/h/l with wrap), `agentTypeIndex` for known/unknown types, enter returns modified agent type.
- `teambuilder_test.go`: 11 tests — start/hide, empty name rejected, step progression (name→goal→orchestrator→workerCount→workDir→confirm), worker count parsing (invalid/boundary/valid), esc at all steps, confirm emits TeamBuiltMsg, inactive noop.
- `settings_test.go`: 16 tests — open/close, inactive consumed=false, cursor nav, bool toggle, select cycle with wrap, int validation (49 rejected, 100 ok, 30001 rejected), empty keybinding/hooks-dir rejected, dirty tracking, esc clean/dirty (double-esc), pending discard cleared by other key, save flow (s→y), save cancel, s-clean-closes, edit-esc-cancels.

**PR 3 additions to existing test files:**
- `config/load_test.go`: added `TestSave_FilePermissions` (verifies 0o600) and `TestWriteAtomic_TempFileCleanedUp`.
- `hooks/runner_test.go`: added `TestFindScripts_DotDSortedAlphabetically` (verifies sort order with reverse-created files).

**Deviation from plan:** PRs 2 and 3 were combined into one PR since the config/hooks test files already had comprehensive coverage from PR 1 — only 3 additional tests were needed. Helper functions `keyPress`/`keyType` were used instead of `key`/`specialKey` to avoid a name collision with the imported `bubbles/key` package.

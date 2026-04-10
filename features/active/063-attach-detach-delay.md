# Feature: Attach/detach delay can exceed one second

- **GitHub Issue:** #63
- **Stage:** IMPLEMENT
- **Type:** bug
- **Complexity:** M
- **Priority:** P1
- **Branch:** —

## Description

When attaching to or detaching from a session, there is a noticeable delay that can be longer than a second. This makes the transition feel sluggish and unresponsive.

## Research

### Summary

The 1+ second delay is caused by **sequential tmux command execution** in the attach script. The script saves 10 tmux options (`show-option` × 10), sets 11 new options (`set-option` × 11), and restores them all on detach. Each tmux invocation has ~50-100ms IPC overhead, totaling **1-2 seconds on attach** and **0.5-1 second on detach**.

### Timing Breakdown

| Phase | Operation | Est. Latency |
|-------|-----------|-------------|
| **Attach** | `tmux show-option` × 10 (save old values) | 500-1000ms |
| **Attach** | `tmux set-option` × 11 (apply theme) | 550-1100ms |
| **Attach** | `tmux bind-key` (detach key) | 50-100ms |
| **Detach** | `tmux set-option` restore × 10 (trap handler) | 500-1000ms |
| **Detach** | TUI rebuild (sidebar, grid, preview) | 30-150ms |
| **Detach** | Preview polling restart delay | ~200ms |

### Relevant Code

- `internal/mux/tmux/attach_script.go:49-140` — **Primary bottleneck.** Generates a shell script with 21+ sequential tmux commands. The `show-option` loop (lines 110-115) and `set-option` loop (lines 119-135) are the main delay sources.
- `internal/mux/tmux/attach_script.go:98-107` — Trap handler generates 10+ sequential restore commands that run on detach.
- `internal/tui/views.go:192-214` — `doAttach()` — orchestrates screen mode switch (line 209) and spawns the script via `tea.ExecProcess`.
- `internal/tui/handle_session.go:73-89` — `handleAttachDone()` — post-detach cleanup: grid restore, sidebar rebuild, preview reset, polling restart.
- `internal/tui/operations.go:250-281` — `attachActiveSession()` — fires async hook, dispatches attach.
- `internal/tui/helpers.go:227-239` — `fireHook()` — hooks run in goroutines but have 5s timeout.
- `internal/mux/native/attach_unix.go:29-100` — Native backend attach (no script overhead, likely faster).

### Constraints / Dependencies

- **tmux IPC is inherently serial.** The tmux server processes commands one at a time, so true parallelism of tmux commands within a single client is not possible. However, batching commands into fewer invocations (e.g., a single `tmux` call with multiple commands separated by `\;`) can drastically reduce process spawn overhead.
- **Status bar theming is the reason for the tmux options.** Hive customizes the tmux status bar during attach to show session info. Removing this would eliminate the delay but lose the feature.
- **The trap handler must be reliable.** Restore logic runs in a shell trap — complex solutions risk leaving the user's tmux in a broken state if the script is interrupted.

## Plan

Eliminate the save phase entirely and batch remaining tmux commands using `\;` chaining. Hive owns the `hive-*` session, so there are no user-customized status bar values to preserve — on detach, simply `set-option -u` (unset) all overrides to return to server defaults.

This reduces 23 tmux process invocations to 4, and greatly simplifies the script.

### Approach

**Drop the save phase entirely.** The 10 `tmux show-option` calls (and the `had_*`/`old_*` shell variables) exist to restore user values on detach. But Hive creates and owns the `hive-*` session — the user never customizes its status bar. On detach, we can simply unset the overrides with `set-option -u`, which returns them to the server/global defaults from `~/.tmux.conf`.

**Chain the override phase into 1 invocation.** Replace 11 separate `tmux set-option` calls with:
```
tmux set-option -t S status on \; set-option -t S status-position top \; ...
```

**Chain the restore (trap) into 1 invocation.** Replace 10 conditional if/else restores with:
```
tmux set-option -u -t S status \; set-option -u -t S status-position \; ...
```
No conditionals needed — just unset everything.

**Result: 4 tmux invocations total** (was 23):
1. `tmux bind-key` (detach key)
2. `tmux set-option \; set-option \; ...` (apply theme — 1 process)
3. `tmux attach-session` (enter session)
4. Trap: `tmux set-option -u \; set-option -u \; ...` (unset theme — 1 process)

### Files to Change

1. `internal/mux/tmux/attach_script.go` — Rewrite `buildAttachScript()`:
   - **Remove** the `had_*` flag initialization loop (lines 72-75)
   - **Remove** the save phase `show-option` loop (lines 110-115)
   - **Replace** the 11 individual `set-option` lines (lines 118-135) with a single chained `tmux` command using `\;`
   - **Simplify** the trap handler (lines 97-107): replace 10 conditional if/else restores with a single chained `tmux set-option -u` command — no shell variables needed
   - Keep: bind-key (line 82), attach-session (line 137), alt-screen printf (line 66)

2. `internal/mux/tmux/attach_script_test.go` — Update tests:
   - Remove assertions about `had_status` flags (no longer generated)
   - Update `TestBuildAttachScript_StatusBarShape` to check for chained `\;` format
   - Add a test counting tmux invocations (expect 4)
   - Keep: bind-key ordering, quoting, detach hint, alt-screen assertions

### Test Strategy

- **Unit tests:** Update `attach_script_test.go` to verify:
  - Script contains exactly 4 lines starting with `tmux` (bind-key, chained set-option, attach-session) + 1 chained `set-option -u` in the trap
  - The chained set-option contains all 10 status bar options
  - No `show-option`, `had_*`, or `old_*` in the script (save phase removed)
  - Existing quoting/ordering tests still pass (adjusted for new format)
- **Existing runtime test:** `attach_script_runtime_test.go` provides end-to-end coverage
- **Manual testing:** Attach/detach, verify status bar themes correctly and returns to defaults on detach

### Risks

- **tmux version compatibility:** `\;` chaining works in tmux 2.6+ (2017). The project already assumes modern tmux.
- **`set-option -u` behavior:** Unsetting options returns them to the global/server default from `~/.tmux.conf`. If a user has customized status bar options at the server level, they will be correctly restored. If they had session-level overrides on the `hive-*` session (unlikely since Hive creates it), those would be lost. Acceptable.
- **Partial failure:** If the chained set-option fails partway, some options may be applied and others not. Acceptable — the status bar is cosmetic and the next attach will re-apply.

## Implementation Notes

<Filled during IMPLEMENT stage.>

- **PR:** —

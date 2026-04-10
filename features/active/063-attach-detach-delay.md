# Feature: Attach/detach delay can exceed one second

- **GitHub Issue:** #63
- **Stage:** PLAN
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

<Filled during PLAN stage.>

### Files to Change
1. `path/to/file.go` — <what and why>

### Test Strategy
- <how to verify>

### Risks
- <what could go wrong>

## Implementation Notes

<Filled during IMPLEMENT stage.>

- **PR:** —

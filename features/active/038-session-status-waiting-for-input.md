# Feature: Session status does not detect "Waiting for input" state

- **GitHub Issue:** #38
- **Stage:** IMPLEMENT
- **Type:** bug
- **Complexity:** M
- **Priority:** P2
- **Branch:** —

## Description

The session status indicator fails to detect or display the "Waiting for input" state, which is the most common and important status for interactive AI agent sessions.

When a session is idle and waiting for user input, the status should clearly reflect "Waiting for input" (or equivalent). Currently, the status does not recognize this state, making it difficult to tell at a glance which sessions need attention.

## Research

### Root Cause

`WatchStatuses()` in `internal/escape/watcher.go:47-65` only compares pane content snapshots and returns either `StatusRunning` (content changed) or `StatusIdle` (content unchanged). It has **no logic to detect `StatusWaiting`** despite `StatusWaiting` being defined in the enum and fully supported by the UI (amber `◉` dot, team status aggregation, etc.).

A session showing a prompt like `>` or `?` is indistinguishable from any other idle session.

### Status Detection Pipeline

1. `scheduleWatchStatuses()` (`handle_preview.go:118-130`) — builds `map[sessionID]→tmuxTarget`, polls at 2x `PreviewRefreshMs`
2. `escape.WatchStatuses()` (`watcher.go:47-65`) — captures last 50 lines per pane, diffs against previous snapshot → `StatusRunning` or `StatusIdle`
3. `handleStatusesDetected()` (`handle_session.go:111-134`) — updates state, rebuilds sidebar if changed
4. `state.UpdateSessionStatus()` (`store.go:174-182`) — sets `sess.Status` and `LastActiveAt`

### UI Already Supports Waiting

- `styles/theme.go:87-90` — `DotWaiting` renders amber `◉`
- `model.go:185-211` — `Team.TeamStatus()` aggregates with priority: waiting > running > idle > dead
- Sidebar renders status dots per session

### Approach: Two-Tier Detection with Combined Heuristics

#### Tier 1: Pane title detection (preferred)

Some agents set the tmux pane title (`#{pane_title}`) with status-encoding prefixes. Read via `tmux display-message -t <target> -p '#{pane_title}'`.

**Observed behavior (live hive sessions):**

| Agent | Pane title when waiting | Pane title when running | Useful? |
|-------|------------------------|------------------------|---------|
| Claude | `✳ <task description>` | `⠁⠂⠃...⠿ <task>` (Braille spinner) | Yes |
| Codex | Static (`hive-demo`, inherited) | Same | No |
| Copilot | Static (`GitHub Copilot`) | Same | No |
| Gemini/Aider/OpenCode | Unknown — not tested | Unknown | TBD |

Agents can define title-based status patterns in config:
```json
"status": {
  "wait_title": "^✳",
  "run_title": "^[⠁-⠿]"
}
```

Built-in defaults for known agents; configurable for custom agents.

#### Tier 2: Content + metadata heuristic fallback

For agents without informative pane titles, combine multiple signals for higher confidence:

1. **Content-diff** (existing) — content changed → `StatusRunning`, unchanged → candidate for idle/waiting
2. **Prompt pattern matching** — when content is stable, check last non-blank line for prompt patterns (configurable per agent, e.g. `"wait_prompt": "^>>> $"`). Match → `StatusWaiting`.
3. **Stability window / debounce** — require content to be unchanged for N consecutive polls before transitioning running→idle or idle→waiting. Prevents flicker from brief pauses during output.
4. **Cursor position heuristic** — tmux exposes `#{cursor_y}` and `#{pane_height}`. If cursor is on the last content line and content is stable, more likely waiting for input. (Observed: `cursor_flag=1` on Codex sessions at prompt.)

No single heuristic is conclusive, but combining them yields higher confidence:
- Content stable + prompt pattern match → high confidence waiting
- Content stable + cursor at bottom + no prompt pattern → likely waiting (lower confidence)
- Content stable + none of the above → idle

#### Config structure

Add optional `StatusDetection` to `AgentProfile`:
```go
type StatusDetection struct {
    WaitTitle   string `json:"wait_title,omitempty"`   // regex on pane title
    RunTitle    string `json:"run_title,omitempty"`     // regex on pane title
    WaitPrompt  string `json:"wait_prompt,omitempty"`  // regex on last line of content
    StableTicks int    `json:"stable_ticks,omitempty"` // polls before idle→waiting (default 2)
}
```

Built-in defaults for known agents; users override for custom agents.

#### Detection priority

1. Title match (if configured and title is dynamic) → immediate status
2. Content changed → `StatusRunning`
3. Content stable for < `StableTicks` → `StatusRunning` (debounce)
4. Content stable + prompt pattern match → `StatusWaiting`
5. Content stable + cursor heuristic → `StatusWaiting` (lower confidence)
6. Content stable, no signals → `StatusIdle`

### Relevant Code
- `internal/escape/watcher.go:47-65` — **the bug**: only returns Running/Idle, never Waiting
- `internal/tui/handle_preview.go:118-130` — schedules status watcher, builds target map (needs agent type + config info)
- `internal/tui/handle_session.go:111-134` — processes status results (no changes needed)
- `internal/state/model.go:45-53` — `StatusWaiting` already defined
- `internal/state/model.go:185-211` — team aggregation already handles waiting
- `internal/tui/styles/theme.go:87-90` — waiting dot already styled
- `internal/config/config.go:21-24` — `AgentProfile` needs `StatusDetection` field
- `internal/mux/interface.go:51-58` — `Backend` interface (may need `GetPaneTitle` method)
- `internal/tmux/` — low-level tmux wrappers (need pane title + cursor position commands)

### Constraints / Dependencies
- Pane title behavior varies by agent and version — title detection is opt-in per agent, not assumed
- Braille spinner chars for Claude span U+2801–U+283F — need a robust regex range
- `cursor_flag` and `cursor_y` from tmux are cheap to read (single `display-message` call) but add latency per session if polled individually — consider batching via `list-windows -F`
- Polling interval (1s default) means detection has ~1s latency — acceptable
- Debounce window adds additional latency (N × poll interval) before waiting is detected — keep N small (2–3)

## Plan

Two-tier detection: pane title matching (fast, reliable for Claude) with a combined-heuristic fallback (content diff + prompt pattern + debounce) for agents without informative titles. Regex patterns are configurable per agent with built-in defaults.

### Files to Change

**Config layer (no dependencies):**

1. `internal/config/config.go` — Add `StatusDetection` struct with `WaitTitle`, `RunTitle`, `WaitPrompt`, `StableTicks` fields. Add it as a field on `AgentProfile`.
2. `internal/config/defaults.go` — Add built-in `StatusDetection` to Claude profile: `WaitTitle: "^✳"`, `RunTitle: "^[⠁-⠿]"`, `StableTicks: 2`. Other agents get `StableTicks: 3` (longer debounce, no title signals).
3. `internal/config/migrate.go` — Backfill `StatusDetection` from defaults for existing configs (same pattern as `InstallCmd` backfill on lines 15-22).

**Backend layer (depends on config for types):**

4. `internal/tmux/capture.go` — Add `GetPaneTitles(tmuxSession string) (map[string]string, error)` using `list-windows -F "#{window_index}\t#{pane_title}"` — single tmux exec for all panes, returns `map[target]→title`.
5. `internal/mux/interface.go` — Add `GetPaneTitles(session string) (map[string]string, error)` to `Backend` interface + package-level forwarding function.
6. `internal/mux/tmux/backend.go` — Implement `GetPaneTitles` delegating to `tmux.GetPaneTitles`.
7. `internal/mux/native/backend_unix.go` + `backend_windows.go` — Stub returning `nil, nil`.
8. `internal/mux/muxtest/mock.go` — Add `paneTitles` map, `SetPaneTitle` setter, implement `GetPaneTitles` filtering by session prefix.

**Watcher (core logic):**

9. `internal/escape/watcher.go` — Main change. New types and extended `WatchStatuses`:
   - Add `SessionDetectionCtx` struct holding compiled regexes (`WaitTitleRe`, `RunTitleRe`, `WaitPromptRe`) and `StableTicks int`.
   - Extend `StatusesDetectedMsg` to include `Titles map[string]string`.
   - New `WatchStatuses` signature adds params: `titles`, `stableTicks`, `detection` maps.
   - Detection logic per session:
     1. Title regex match → immediate status (tier 1)
     2. Content changed → `StatusRunning`
     3. Content stable for < StableTicks → `StatusRunning` (debounce)
     4. Content stable + `WaitPromptRe` matches last non-empty line → `StatusWaiting`
     5. Content stable + no signals → `StatusIdle`
   - Add `matchesLastLine(content string, re *regexp.Regexp) bool` helper.

**TUI integration:**

10. `internal/tui/app.go` — Add `stableTicks map[string]int` and `detectionCtxs map[string]escape.SessionDetectionCtx` fields to `Model`. Initialize in `New()`. Add `buildDetectionCtxs(agents)` helper that compiles regexes from config once at startup. Map is keyed by agent type name (e.g. "claude").
11. `internal/tui/handle_preview.go` — Update `scheduleWatchStatuses()` to: (a) batch-read pane titles via `mux.GetPaneTitles(mux.HiveSession)`, (b) build per-session detection map by looking up `sess.AgentType` in `m.detectionCtxs`, (c) pass titles, stableTicks, detection to new `WatchStatuses` signature.
12. `internal/tui/handle_session.go` — Update `handleStatusesDetected()` to track `stableTicks`: increment when content unchanged, reset to 0 when changed.

**Note:** Cursor position heuristic (`#{cursor_y}`) is deferred to a follow-up — the title + prompt + debounce combination should provide good coverage. Adding cursor data later is straightforward (extend `GetPaneTitles` format string to include `#{cursor_y}\t#{pane_height}`).

### Test Strategy
- Unit tests in `internal/escape/watcher_test.go` for all 5 detection paths: title wait match, title run match, content changed, stable+prompt match, stable+idle fallback. Pure function tests with mock data — no tmux needed.
- Unit test in `internal/config/migrate_test.go` for `StatusDetection` backfill.
- Unit test for `matchesLastLine` edge cases (empty content, trailing whitespace, ANSI codes in last line).
- Mock backend test for `GetPaneTitles` session filtering.
- Manual: run hive with Claude sessions, observe ✳ → waiting (amber dot), spinner → running (green dot). Test with Codex/Copilot to verify fallback works.

### Risks
- **Regex compilation from user config** — bad regex silently falls through to tier 2. Could log a warning on startup for visibility.
- **Pane title read timing** — titles are read in `scheduleWatchStatuses` (at schedule time, not tick time). ~1s lag is acceptable; moving inside tick would couple watcher to tmux specifics.
- **Agent title format changes** — Claude could change spinner chars in future versions. Built-in patterns are overridable via config, and the Braille range `[⠁-⠿]` covers the full block.
- **Native backend** — returns empty titles, falls through to tier 2 naturally.

## Implementation Notes

<Filled during IMPLEMENT stage.>

- **PR:** —

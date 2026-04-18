# Feature: Quick-reply to permission prompts from grid view without focusing

- **GitHub Issue:** #122
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P1
- **Branch:** —

## Description

In grid mode, when a session is waiting for input on a simple permission prompt (e.g. "1. Accept", "2. Always", "3. No"), the user should be able to send a quick reply directly from the grid view without having to attach/focus on the session first. This would significantly speed up workflows where multiple agents are running and frequently need simple yes/no/always confirmations.

## Research

### Relevant Code
- `internal/tui/components/gridview.go:299-420` — `Update()` handles all grid key input; input mode forwards all keys to tmux at line 307-324; number keys are currently unhandled in navigation mode (fall through to line 419 "consume all keys")
- `internal/tui/components/gridview.go:104-127` — `GridView` struct; `inputMode bool` at line 121; `sessions []*state.Session` with `Cursor int` for focus tracking
- `internal/tui/components/gridview.go:265` — `Selected()` returns the focused session
- `internal/tui/components/gridview.go:540-628` — Cell header rendering with status dot, bell badge, agent badge, INPUT badge overlay pattern
- `internal/tui/components/gridview.go:27-35` — `GridKeys` struct for configurable bindings
- `internal/state/model.go:45-52` — `SessionStatus` type; `StatusWaiting` = "waiting"
- `internal/state/model.go:129` — Session struct has `Status SessionStatus` field
- `internal/escape/watcher.go:54-56,148-171` — Status detection using `WaitTitleRe` and `WaitPromptRe` regexes; already reliably detects waiting state
- `internal/mux/interface.go` + `internal/mux/tmux/backend.go` — `SendKeys(target, keys)` sends raw bytes to tmux pane
- `internal/tui/flow_grid_input_test.go` — Comprehensive test patterns for input mode: enter/exit, key forwarding, mock backend usage

### Constraints / Dependencies
- Number keys (1-9) are not bound to anything in grid navigation mode — safe to intercept
- The feature should only activate when the focused session's status is `StatusWaiting`
- Must coexist with existing input mode (`i` key) — quick-reply is a shortcut that avoids entering full input mode
- Status detection is already reliable via agent config regexes; no new detection logic needed
- Multi-agent support: works regardless of agent type since status detection is agent-config-driven

## Plan

When the focused session has `StatusWaiting` and the user presses a number key (1-9) in grid navigation mode, send that digit + Enter to the session via `mux.SendKeys()`. This is a one-shot action — no mode change needed.

Waiting cells get a distinct border color (`ColorWarning` / amber, matching the waiting status dot) so the user can instantly see which sessions accept quick-reply.

### Files to Change

1. `internal/tui/components/gridview.go` — Add quick-reply handling in `Update()`:
   - Between the input-mode block (line 324) and the switch statement (line 331), add a check: if `!gv.inputMode` and the key is a digit 1-9 and the focused session has `StatusWaiting`, call `mux.SendKeys(target, digit+"\n")` and return. This sends the digit plus Enter.
   - Add a `QuickReplyEnabled bool` field (parallels `InputEnabled`), defaulting to true.
   - In `renderCell()`: when the session has `StatusWaiting` and `QuickReplyEnabled` and the cell is not already selected/dimmed, set the border color to `styles.ColorWarning` (amber). This gives waiting cells a visible amber border that matches the waiting status dot color.

2. `internal/tui/handle_keys.go` — In `handleGridKey()`:
   - Before delegating to `m.gridView.Update(msg)` at line 239, check if the key is a digit 1-9 and the focused session is waiting. If so, let `gridView.Update()` handle it (it already consumes all keys).
   - No changes needed here — the gridView.Update() handles everything internally.

3. `internal/config/config.go` — Add `DisableQuickReply bool` field to `Config` (parallels `DisableGridInput`), so users can opt out.

4. `internal/tui/app.go` — When constructing `GridView`, set `QuickReplyEnabled = !cfg.DisableQuickReply`.

### Test Strategy

- `internal/tui/flow_grid_quick_reply_test.go`:
  - `TestGridQuickReply_SendsDigitAndEnter` — Set session status to `StatusWaiting`, press "1" in grid nav mode, verify `mock.LastSentKeys == "1\n"` and `SendKeys` called once.
  - `TestGridQuickReply_IgnoredWhenNotWaiting` — Session status is `StatusRunning`, press "1", verify `SendKeys` not called.
  - `TestGridQuickReply_IgnoredInInputMode` — Enter input mode first, press "1", verify the key is forwarded as a plain "1" (no appended Enter), same as existing input mode behavior.
  - `TestGridQuickReply_DisabledByConfig` — Set `DisableQuickReply=true`, session is waiting, press "1", verify `SendKeys` not called.
  - `TestGridQuickReply_AllDigits` — Test digits 1-9 all work when session is waiting.

- `internal/tui/components/gridview_test.go`:
  - `TestGridView_WaitingBorderHighlight` — Render a cell with `StatusWaiting` and `QuickReplyEnabled=true`, verify the border uses the waiting/amber color.
  - `TestGridView_NoBorderHighlightWhenNotWaiting` — Same but with `StatusRunning`, verify standard border color.

### Risks

- **Number keys used for other purposes**: Currently number keys are unused in grid nav mode (confirmed by reading `Update()`), so no conflict.
- **Accidental input**: If a session is wrongly detected as "waiting" and the user presses a number, it sends unexpected input. Mitigated by the status detection already being mature and tested.
- **Multi-digit replies**: Some prompts may need numbers > 9. For those, the user falls back to full input mode (`i`). This is a reasonable trade-off for v1.

## Implementation Notes

- Quick-reply triggers on both `StatusWaiting` AND `StatusIdle` because Claude sessions don't always reach `StatusWaiting`. The amber border highlight only applies to `StatusWaiting` to avoid being too noisy.
- Added `WaitPrompt: "^\s*[1-4]\.\s"` to Claude's default agent config so numbered permission prompts are now detected as `StatusWaiting`.
- Added schema migration v6→v7 to backfill `WaitPrompt` for existing Claude configs.
- Added `ResetCounts()` method to `muxtest.MockBackend` to support the `AllDigits` test.
- Quick-reply check in `gridview.go` is placed before the nav switch block, after the input-mode block, so it runs only in navigation mode.
- Border highlight uses `styles.ColorWarning` (amber) to match the existing waiting status dot color.

- **PR:** [#123](https://github.com/lucascaro/hive/pull/123)

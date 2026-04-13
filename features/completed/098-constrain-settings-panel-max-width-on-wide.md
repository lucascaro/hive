# Feature: Constrain settings panel max-width on wide screens

- **GitHub Issue:** #98
- **Stage:** DONE
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P1
- **Branch:** —

## Description

The settings panel currently expands to fill the full terminal width on wide screens, making it harder to use — form fields stretch to extreme widths, reducing readability and ergonomics. The panel should have a sensible maximum width so it stays compact and usable regardless of terminal size. Content should be centered or left-anchored within the available space.

## Research

The settings panel uses its full `Width` field for every rendered element. Width is set to `TermWidth` before each render. A simple max-width cap with centering solves the problem with minimal code change.

### Relevant Code
- `internal/tui/components/settings.go:329` — `View()` computes `w := sv.Width` and uses it for all sub-elements (header, tab strip, body, footer). Capping `w` here and centering the final output in `sv.Width` is the complete fix.
- `internal/tui/app.go:404` — sets `m.settings.Width = m.appState.TermWidth` immediately before calling `m.settings.View()`. No change needed here.

### Constraints / Dependencies
- None. The fix is entirely within `View()` in a single file. No state, no messages, no other components involved.

## Plan

Cap the rendered settings panel at a sensible max width and center it within the terminal.

### Files to Change
1. `internal/tui/components/settings.go` — In `View()`, save the full terminal width as `fullW := sv.Width`, then cap `w` at a new `maxSettingsWidth = 100` constant. After building the panel with `lipgloss.JoinVertical`, wrap the result with `lipgloss.Place(fullW, h, lipgloss.Center, lipgloss.Top, panel)` to center it horizontally on wide terminals.
2. `CHANGELOG.md` — Add `[Unreleased]` entry: "Settings panel now has a maximum width and is centered on wide terminals."

### Test Strategy
- `internal/tui/components/settings_test.go` — Add `TestSettingsViewMaxWidth`: open a `SettingsView` with `Width=200, Height=40`, call `View()`, assert that `lipgloss.Width(view) == 200` (outer wrapper is full width) but that the visible panel content does not exceed `maxSettingsWidth + padding`.
- Run `go test ./...` — all existing tests must pass.

### Risks
- `renderTabStrip` uses `w` for its full-width fill — works fine since `w` is still ≥ any real field width.
- `lipgloss.Place` pads with spaces to fill `fullW`; this is the expected TUI behavior for centering and harmless for the outer terminal viewport.

## Implementation Notes

- Added `maxSettingsWidth = 100` const in `settings.go`.
- In `View()`, saved `fullW := sv.Width` before capping `w`; used `lipgloss.Place(fullW, h, lipgloss.Center, lipgloss.Top, panel)` to center when `fullW > w`.
- Added `TestSettingsViewMaxWidth` in `settings_test.go`: verifies outer line width equals `sv.Width` and trimmed content lines stay within `maxSettingsWidth`.
- No deviations from the plan.

- **PR:** #99

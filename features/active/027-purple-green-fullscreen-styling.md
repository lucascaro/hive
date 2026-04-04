# Feature: Use more purple/green styling for full-screen mode title

- **GitHub Issue:** #27
- **Stage:** IMPLEMENT
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** —

## Description

The full-screen (attach) mode status bar currently uses a plain dark gray style. Update it to use the app's purple/green accent colors for a more distinctive look that matches the rest of the theme.

## Research

The full-screen attach mode status bar is configured in `buildAttachScript()` in
`internal/tui/app.go` (line ~2828). It sets tmux `status-style` with hardcoded
hex colors. The app's theme colors are defined in `internal/tui/styles/theme.go`.

### Relevant Code
- `internal/tui/app.go:2828` — tmux status-style for attach mode
- `internal/tui/styles/theme.go` — theme color constants (ColorAccent `#7C3AED`, ColorText `#F9FAFB`)

### Constraints / Dependencies
- Colors are passed as tmux style strings, not lipgloss — must use hex codes directly

## Plan

Change the status bar background from dark gray (`#1F2937`) to the app's purple
accent (`#7C3AED`), keeping white text. Single-line change.

### Files to Change
1. `internal/tui/app.go` — update `status-style` bg color to `#7C3AED`

### Test Strategy
- Build succeeds
- Visual verification: attach a session and confirm purple status bar

### Risks
- None — cosmetic change only

## Implementation Notes

Changed `status-style` bg from `#1F2937` to `#7C3AED` (Option A: purple bg, white text).

- **PR:** —

# Feature: Organize settings into tabbed categories

- **GitHub Issue:** #76
- **Stage:** RESEARCH
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P1
- **Branch:** —

## Description

The settings screen has grown long — General, Team Defaults, Hooks, and a large Keybindings section all scroll through one flat list, making navigation and discovery difficult. Reorganize into a tabbed layout where each category (General, Team Defaults, Hooks, Keybindings, and future additions) is a separate tab the user can switch between with Left/Right arrows (or h/l).

Within each tab, j/k navigation and edit/toggle behavior stay the same. The current category headers become the tabs themselves.

## Research

<Filled during RESEARCH stage.>

### Relevant Code
- `internal/tui/components/settings.go` — single source; all rendering, navigation, and entry building lives here. Current `settingEntry` has `isHeader`/`header`/`field`; this becomes a grouping mechanism for tabs.

### Constraints / Dependencies
- Must preserve existing keybindings within a tab (j/k, enter/space, s, esc).
- Need a horizontal tab bar that fits narrow terminals (truncate or scroll tab strip if needed).
- Scroll offset should be per-tab so switching tabs doesn't lose position.

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

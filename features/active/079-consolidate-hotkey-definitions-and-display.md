# Feature: Consolidate hotkey definitions and display between sidebar and grid modes

- **GitHub Issue:** #79
- **Stage:** RESEARCH
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P3
- **Branch:** —

## Description

Hotkey handling and display is currently duplicated and inconsistent between sidebar mode and grid mode — the same action may use different keys, appear in one help view but not the other, or be formatted differently. This leads to drift as new keys are added.

Consolidate hotkey definitions into a single source of truth using the `bubbles/key` package (`key.Binding` with built-in Help metadata), so each action is defined once with its keys + description. Both the sidebar and grid views should render their help/hotkey hints from the same bindings, automatically staying in sync. Mode-specific bindings (e.g. grid-only reorder keys) should still be expressible, but shared bindings should not be redefined.

## Research

<Filled during RESEARCH stage.>

### Relevant Code
- `internal/tui/keys.go` — existing `KeyMap` with `key.Binding` entries; should be the single source of truth
- `internal/tui/components/statusbar.go` — hardcoded hint lists (`hints := []hint{...}`) per view; should derive from `KeyMap` via `key.Binding.Help()`
- `internal/tui/views.go` / `layout.go` — view-mode selection that drives which hints show

### Constraints / Dependencies
- Must preserve the help overlay golden test (`testdata/TestGolden_HelpOverlay.golden`) or update it deliberately
- Grid-only bindings (reorder shift+left/right) must still be expressible separately from shared bindings

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

# Feature: Preserve selected project and session when toggling between grid views (g/G)

- **GitHub Issue:** #80
- **Stage:** RESEARCH
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Branch:** —

## Description

When in the all-projects grid view (`G`) and pressing `g` to switch to the single-project grid, the currently selected project is not preserved — the view doesn't land on the project of the currently selected session. The selection should round-trip: toggling `g` ↔ `G` should always keep the same project and session selected, so the user stays oriented when switching between overview and per-project views.

## Research

<Filled during RESEARCH stage.>

### Relevant Code
- `internal/tui/handle_keys.go` — grid view toggle handler; needs to carry current project + session across modes
- `internal/tui/keys.go` — `GridOverview` binding
- `internal/tui/flow_grid_test.go` — existing grid flow tests; add coverage for g↔G round-trip

### Constraints / Dependencies
- Must still work when the selected session has no project context (edge case: fresh state)

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

# Feature: Selected session should persist across views and attach/detach

- **GitHub Issue:** #35
- **Stage:** RESEARCH
- **Type:** bug
- **Complexity:** M
- **Priority:** P2
- **Branch:** —

## Description

The currently selected session is lost when switching between the main view, grid view, or when attaching to and detaching from a session.

The selected session should remain consistent across:
- Switching from main view to grid view and back
- Attaching to a session and then detaching
- Any other view transitions

Currently, the selection resets when transitioning between views, forcing the user to re-navigate to the session they were working with.

## Research

<Filled during RESEARCH stage.>

### Relevant Code
- `path/to/file.go` — <why it matters>

### Constraints / Dependencies
- <anything blocking or complicating this>

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

# Feature: Terminal bell only sounds in sidebar view, not when attached to session

- **GitHub Issue:** #85
- **Stage:** RESEARCH
- **Type:** bug
- **Complexity:** M
- **Priority:** P2
- **Branch:** —

## Description

The alarm/terminal bell only sounds when the user is out of a session (appears to be sidebar view only). When attached to a session, bell events from the running agent do not produce audio.

Expected: bell events should sound regardless of which view the user is in — sidebar, grid, or attached to a session.

## Research

<Filled during RESEARCH stage.>

### Relevant Code
- `internal/audio/bell.go` — bell audio playback
- `internal/escape/watcher.go` — bell detection via escape sequences
- `internal/tmux/capture.go` — tmux pane capture path

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

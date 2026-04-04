# Feature: Terminal flashes previous content when attaching/detaching sessions

- **GitHub Issue:** #30
- **Stage:** TRIAGE
- **Type:** bug
- **Complexity:** S | M | L
- **Priority:** —
- **Branch:** —

## Description

When attaching to or detaching from a session, the terminal briefly flashes the content that was displayed before hive was started. This creates a jarring visual glitch during transitions.

### Steps to Reproduce

1. Have some content visible in the terminal
2. Start hive
3. Attach to a session (or detach from one back to grid view)
4. Observe a brief flash of the pre-hive terminal content

### Expected Behavior

Transitions between grid view and full-screen sessions should be seamless without flashing the underlying terminal content.

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

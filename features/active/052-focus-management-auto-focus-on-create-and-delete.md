# Feature: Focus management: auto-focus on session create and smart fallback on delete

- **GitHub Issue:** #52
- **Stage:** RESEARCH
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Branch:** —

## Description

When creating a new session, focus doesn't move to it automatically — the user has to navigate to it manually. When deleting a session, focus stays on a stale position instead of moving to a logical neighbor.

### On session create
The newly created session should be focused automatically.

### On session delete
Focus should fall back in this priority order:
1. Next session in the same group (team or standalone list)
2. Previous session in the same project
3. Next session overall
4. Previous session overall
5. Nothing (no sessions remain)

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

# Feature: Persist user preferences (e.g. start in grid mode)

- **GitHub Issue:** #78
- **Stage:** TRIAGE
- **Type:** bug | enhancement
- **Complexity:** S | M | L
- **Priority:** —
- **Branch:** —

## Description

The grid-mode toggle resets each session. Add a lightweight user-preferences store so choices like "start in grid mode" persist across reloads.

Consider making it general — a `preferences` object keyed by setting name — rather than a one-off flag, so future toggles (theme, density, default view, etc.) can use the same mechanism.

**Acceptance:**
- Toggling grid mode persists and is restored on next load
- Storage mechanism is reusable for other preferences

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

# Feature: Rework dialog system to use a view stack

- **GitHub Issue:** #46
- **Stage:** RESEARCH
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P3
- **Branch:** —

## Description

Dialogs currently have custom logic to decide which view to return to after they close. This leads to bugs — for example, renaming a session while in grid view incorrectly returns to the main view instead of back to the grid.

Rework the dialog system to use a view stack:

- When a dialog opens, it is **pushed** on top of the current view
- When a dialog closes, it is **popped** and the user returns to whatever view was underneath
- No per-dialog custom "return to" logic — the stack handles it automatically

This would fix the rename-in-grid-view bug and prevent similar navigation issues in the future.

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

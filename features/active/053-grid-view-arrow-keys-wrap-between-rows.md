# Feature: Grid view: arrow keys should wrap between rows

- **GitHub Issue:** #53
- **Stage:** RESEARCH
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P3
- **Branch:** —

## Description

In grid view, pressing right-arrow on the rightmost cell in a row does nothing. Similarly, pressing left-arrow on the leftmost cell does nothing.

### Expected behavior
- **Right-arrow on last cell in a row**: move to the first cell of the next row (if it exists).
- **Left-arrow on first cell in a row**: move to the last cell of the previous row (if it exists).

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

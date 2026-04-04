# Feature: Terminal bell does not produce audible sound

- **GitHub Issue:** #34
- **Stage:** RESEARCH
- **Type:** bug
- **Complexity:** S
- **Priority:** P4
- **Branch:** —

## Description

The terminal bell (`\a` / BEL character) does not produce an audible sound when triggered from within hive sessions.

When a session emits a bell character (e.g., on tab completion failure, command error, or explicit `echo -e '\a'`), the user should hear an audible notification or see a visual bell, depending on their terminal configuration. Currently, no sound is produced.

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

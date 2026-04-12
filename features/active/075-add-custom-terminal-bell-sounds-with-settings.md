# Feature: Add custom terminal bell sounds with settings option

- **GitHub Issue:** #75
- **Stage:** RESEARCH
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P1
- **Branch:** —

## Description

The terminal bell currently plays a single default sound (emits `\a` to the real terminal). Users should be able to choose from a selection of bell sounds in settings to personalize their notification experience and better distinguish mux alerts from other system sounds.

Proposed options:
- **Normal bell** (default) — classic terminal beep (`\a`)
- **Bee buzz** — playful buzzing sound
- **Chime** — soft melodic chime
- **Ping** — short, crisp notification tone
- **Knock** — subtle knock sound
- **Silent** — visual-only indication (no sound)

The sound choice should be configurable via the settings UI and persisted across sessions.

## Research

<Filled during RESEARCH stage.>

### Relevant Code
- `internal/tui/handle_session.go` — current bell forwarding (`\a` write to stdout, 500ms debounce)
- `internal/tui/components/settings.go` — settings UI; add a new `fieldSelect` entry under "General"
- `internal/config/load.go` — config schema; add `BellSound` field
- `internal/escape/watcher.go` — where bell events originate via `StatusesDetectedMsg.Bells`

### Constraints / Dependencies
- Playing custom audio requires either: (a) bundling WAV/OGG assets + a Go audio library (e.g., `oto`, `beep`), (b) shelling out to platform tools (`afplay` on macOS, `aplay`/`paplay` on Linux), or (c) sticking with `\a` and varying it synthetically (limited).
- Cross-platform: macOS, Linux, (Windows?) support expected.
- Asset licensing for sound files must be verified.
- Must remain responsive — sound playback should not block the TUI render loop.

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

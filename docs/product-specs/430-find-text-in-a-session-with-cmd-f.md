---
issue: 430
title: "Find text in a session with ⌘F"
type: enhancement
complexity: M
priority: P2
stage: DONE
superseded_by: 431
---

# Find text in a session with ⌘F

> **Superseded by [#431](431-search-an-agent-session-s-transcript-history.md).** Merged rather than dropped: ⌘F now drives both sources, selected by terminal buffer type. This spec's reviewed terminal-layer findings are folded into 431's exec plan.

- **Issue:** [#430](https://github.com/lucascaro/hive/issues/430)

## Problem

Finding a string in a running session means scrolling and reading. Hive has no in-terminal find at all — ⌘F is unbound, and no search addon is installed. The operator's workaround is to scroll the tile by hand looking for the line, or leave Hive entirely and open the log somewhere else. This gets worse the longer a session runs and the more of the 5000-line buffer is filled.

## Desired behavior

⌘F (Ctrl+F off macOS) opens a compact search box in the top-right of the focused session. Typing searches incrementally — every keystroke re-runs the search, highlights every match in the terminal, distinguishes the active one, and shows a live `n/total` count. ⏎ / ⇧⏎ and next/prev buttons move between matches; the viewport jumps to each. Esc or the box's close button dismisses it and returns focus to the terminal. The box closes on session switch. It works the same whether the session fills the window or is one tile in the grid, and it works on alt-screen TUI agents (Claude Code and friends), where the corpus is necessarily just the visible screen.

## Success criteria

1. ⌘F on macOS / Ctrl+F elsewhere opens the search box on the focused session; the box does not appear on any other session.
2. Typing into the box updates highlights and the `n/total` count on every keystroke, with no explicit submit.
3. The count is accurate against a session with a known number of occurrences, and updates when new output adds or trims matches.
4. ⏎, ⇧⏎, and the next/prev buttons each advance the active match and scroll it into view; the count's `n` follows.
5. Esc and the close button both dismiss the box and restore keyboard input to the terminal.
6. Switching to another session dismisses the box.
7. Opening search on a session that was following the bottom, jumping to a match, then closing, returns the viewport to the bottom. New output during a search does not yank the viewport away from the active match.
8. Search functions on an alt-screen agent session, matching against the visible screen.
9. Typing in a session that is streaming heavy output shows no latency regression attributable to search being open.
10. The binding appears in the ⌘/ help overlay, the command palette, and the macOS menu, consistent with the existing shortcut drift surface.

## Non-goals

- **Cross-session search.** Only the focused session. No searching unfocused tiles, minimized sessions, or every session at once.
- **Searching beyond the terminal buffer.** The corpus is what xterm holds — 5000 lines, less after a cap trim, one screenful in alt-screen. No daemon-side scrollback query, no index, no log file reading. Text that scrolled past the cap is not findable.
- **Match option toggles.** Plain substring, case-insensitive only. Case-sensitive, regex, and whole-word are deliberately deferred — the box should leave room for them, but none ships here.
- **Non-terminal surfaces.** Sidebar, launcher, settings, activity/agent views. Not closing the door on reuse later, but nothing outside a session terminal is in scope.
- **Search history, saved searches, or replacing text.**

## Notes

**HELD at PLAN (2026-09-17).** Not approved for implementation. The alt-screen ceiling recorded below was measured during planning and the operator judged it fatal to the feature as scoped: an in-terminal find box searches one screenful on agent sessions. Planning also established that the daemon's 8 MiB per-session ring *does* retain alt-screen bytes (`internal/session/vt.go:166-171`), so history search is more feasible than this spec's non-goals imply. That brainstorm produced spec [431](431-search-an-agent-session-s-transcript-history.md), which covers agent-session history via transcripts; the two are intended to coexist (⌘F stays in-terminal for what the buffer holds, 431 gets its own binding). See [docs/design-docs/session-history-search.md](../design-docs/session-history-search.md) and the exec plan's `## Hold`.

Two deviations were identified during planning and would need this spec amended before any implementation: criterion 1's "Ctrl+F elsewhere" must become Ctrl+Shift+F (plain Ctrl+F is `0x06`, readline's `forward-char`), and the Desired-behavior claim of grid-tile parity conflicts with the operator's single-view-only decision. Left unamended because the feature is held.

**Open question carried from brainstorm.** The alt-screen ceiling — a hit count bounded by one screenful, no scrollback to search — may prove too limiting in practice for exactly the agent sessions the operator most wants to search. Recorded, not solved in this spec.

**Grounding facts from brainstorm:**

- xterm.js 5.5.0 with fit / web-links / webgl addons only; no `@xterm/addon-search` installed.
- xterm scrollback cap is 5000 lines (`cmd/hivegui/frontend/src/app/session-term.ts:278`).
- ⌘F is currently unbound in both JS and `cmd/hivegui/menu_darwin.go`.
- Binding changes have a documented 5-file drift surface — see the header of `cmd/hivegui/frontend/src/lib/shortcuts.ts`.

**Complexity note.** Rated M rather than S because criterion 7 touches viewport-follow machinery with a scarred history ([163](163-resize-stick-mostly-bottom.md), [213](213-always-scroll-to-bottom-on-mode-switch-resize.md), [codex-scroll-jump-on-replay](codex-scroll-jump-on-replay.md)). The search itself is nearly free; detach-then-restore is where the work is.

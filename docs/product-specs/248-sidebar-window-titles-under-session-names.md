# Show terminal window titles under session names in the sidebar

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Stage:** IMPLEMENT
- **Exec plan:** [docs/exec-plans/active/248-sidebar-window-titles-under-session-names.md](../exec-plans/active/248-sidebar-window-titles-under-session-names.md)

## Problem

The sidebar identifies a session only by the name the user typed when creating it. That name is fixed at creation and says nothing about what the session is doing *now* — whether Claude is mid-task, whether a build is running, whether the shell is idle at a prompt. The only way to find out is to switch to the session and look, which defeats the point of having a list of them.

Every agent TUI and most interactive programs already publish exactly this information as the terminal window title via OSC 0/2. Hive captures it (`cmd/hivegui/frontend/src/app/session-term.ts:576`) and renders it in the grid tile header (`.tile-term-title`), but the sidebar — the one surface that shows *all* sessions at once — ignores it.

## Desired behavior

Each sidebar session row shows the session's current terminal window title on a second line beneath the session name, in a visual language that reads as clearly subordinate: smaller, dimmer, and truncated with an ellipsis. The session name stays the primary label; the title is the status line under it.

Titles are live — when the running program changes its title, the row updates without the user touching anything. Titles are available for **every** session in the list, including ones the user has never opened in this window and ones inherited from a daemon that outlived the GUI.

Rows with no title stay exactly as they are today: a single-line row, no reserved empty space, no layout shift as titles come and go.

## Success criteria

- With a Claude Code session running a task in project A and the GUI focused on project B, project A's sidebar row shows Claude's current window title without the user ever having switched to that session.
- Quitting and reopening the GUI while the daemon keeps running restores the titles in the sidebar immediately, without attaching to each session.
- A session whose program sets no title (or sets a title identical to the session name) renders as a one-line row, visually identical to today.
- Changing the title in a running program (e.g. `printf '\033]0;hello\007'`) updates that session's sidebar row within one event round-trip.
- A very long title truncates with an ellipsis inside the sidebar width and does not widen the sidebar, wrap, or push the swatch/worktree glyph out of the row.
- Hovering the title shows the full untruncated string as a tooltip.
- Dead sessions do not show a stale title.

## Non-goals

- Changing the grid tile header's existing title rendering.
- Persisting titles to disk across a daemon restart.
- Any user setting to hide or reformat the title.
- Parsing or interpreting the title's content (no "extract the task name from Claude's title" heuristics).
- Showing titles anywhere other than the sidebar and the existing tile header.

## Notes

`vt10x` already tracks the OSC title (`Terminal.Title()`, `state.go:174`) and the daemon already feeds every session's PTY bytes through its `VT` regardless of client attachment (`internal/session/session.go:205`), so the daemon-side source costs a read and a change-diff rather than a new parser.

The frontend-only alternative — reading the existing `state.terms.get(id)?.termTitle` — was rejected at triage: `state.terms` is populated lazily (`view.ts:82`, `renderGrid`), so in single view it is empty for exactly the unopened sessions the user wants to inspect, and it resets on every GUI restart.

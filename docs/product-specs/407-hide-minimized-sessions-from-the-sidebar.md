---
issue: 407
title: "Hide minimized sessions from the sidebar"
type: bug
complexity: S
priority: P2
stage: IMPLEMENT
---

# Hide minimized sessions from the sidebar

- **Issue:** #407
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/407-hide-minimized-sessions-from-the-sidebar.md](../exec-plans/active/407-hide-minimized-sessions-from-the-sidebar.md)

## Problem

A minimized session (e.g. calm-thorn) still renders as a dimmed row in the sidebar, but ⌘↑/⌘↓ skips it because keyboard navigation excludes hidden sessions (#252). The result is a visible row that cannot be reached from the keyboard, which reads as a focus bug.

## Desired behavior

The sidebar lists the sessions ⌘↑/⌘↓ can focus. Minimizing a session takes its row out of the sidebar, the same way minimizing a project takes its card out; the minimized-session tray above the status bar and ⌘K remain the ways to get it back. The one exception is the session you are currently on: if it is minimized, its row stays (marked minimized, with the restore control) so the selection is never invisible, and it leaves once you move to another session.

## Success criteria

- A minimized session that is not the active session has no row in the sidebar.
- A minimized session that is the active session keeps its row, marked minimized with its restore control; the row disappears once another session becomes active.
- Restoring a minimized session (tray chip, row restore control, ⌘B, back/forward) puts its row back at its original position.
- A project card's session count and attention summary still include its minimized sessions.
- Every sidebar row other than the active one is a session ⌘↑/⌘↓ can land on.

## Non-goals

- ⌘1–9 stays a positional index into the full ordered session list (#252); hints are not renumbered.
- No change to the minimized-session tray, the ⌘K palette, grid scoping, or the ⌘↑/⌘↓ walk.
- No per-card "N minimized" disclosure or other new sidebar affordance.

## Notes

Amends [#250](250-minimize-projects-from-sidebar.md), which kept a minimized session's row in the list dimmed, and the sidebar half of [#252](252-keyboard-switching-skips-minimized-sessions.md)'s non-goals.

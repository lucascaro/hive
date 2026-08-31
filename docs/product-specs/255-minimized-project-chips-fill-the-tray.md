---
issue: null
title: "Minimized project chips fill the tray with a right-aligned restore"
type: bug
complexity: S
priority: P2
stage: GATE
pr: 300
---

# Minimized project chips fill the tray with a right-aligned restore

- **Issue:** —
- **PR:** #300
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Stage:** GATE
- **Exec plan:** [docs/exec-plans/active/255-minimized-project-chips-fill-the-tray.md](../exec-plans/active/255-minimized-project-chips-fill-the-tray.md)

## Problem

The minimized-projects tray at the bottom of the sidebar reuses the chip
component built for the horizontal minimized-*sessions* tray, where a
240px cap keeps pills from running away. In a vertical, full-width list
that cap is wrong: a chip stops short of the sidebar edge, the restore
`+` sits immediately after the project name instead of on the right edge,
and the empty space between the name and the tray edge is dead — clicking
it does nothing, even though putting a project away means the only reason
to click its chip is to get it back.

## Desired behavior

A minimized project reads as a full-width row: the chip spans the tray,
its restore `+` is pinned to the right edge, and a click anywhere on the
row restores the project.

## Success criteria

- A project chip's width matches the tray's content width; no 240px cap.
- The restore `+` is flush with the chip's right edge.
- Clicking the slack between the project name and the `+` restores the project.
- The horizontal minimized-*sessions* tray keeps its 240px chip cap.

## Non-goals

- Restyling the minimized-sessions tray or the chip component itself.
- Changing what restore does, or the chip's attention/bell behavior.

## Notes

Follows #250, which introduced the minimized-projects tray.

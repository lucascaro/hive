---
issue: 442
pr: 443
title: "Add a Help button and Help modal to the sidebar header"
type: enhancement
complexity: M
priority: P2
stage: GATE
---

# Add a Help button and Help modal to the sidebar header

- **Issue:** #442
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/442-add-a-help-button-and-help-modal.md](../exec-plans/active/442-add-a-help-button-and-help-modal.md)

## Problem

The sidebar header carries three icon controls — new project (`+`), check for updates
(download), and What's new (gift). None of them is help. The only in-app guidance is the
keyboard-shortcuts overlay bound to ⌘/, and that binding is itself undiscoverable: nothing
on screen points at it. A user who has never read the README has no way to learn from
inside the app what a project, a session, a worktree or an agent is, or where to report a
bug.

## Desired behavior

A fourth icon button sits in the sidebar header, rightmost — after the gift. Clicking it
opens a Help modal that explains the core concepts in a few lines, shows the ⌘/ binding
next to a row that opens the keyboard-shortcuts overlay, and offers links out to the
README, the docs, the issue tracker and the releases page. The modal also opens from the
command palette. No new keybinding is introduced; ⌘/ still opens the shortcuts overlay
directly.

## Success criteria

- A `help`-icon button with the accessible name "Help" renders in the sidebar header, after
  the What's new (gift) button in DOM order.
- Clicking it opens a modal titled "Help"; Escape closes it and focus returns to the active
  terminal.
- The modal contains a Concepts section defining project, session, worktree and agent.
- The modal contains a "Keyboard shortcuts" row displaying the ⌘/ binding; activating it
  closes Help and opens the existing shortcuts overlay.
- The modal contains external links (README, docs, report an issue, releases) that open in
  the system browser via the `OpenURL` bridge rather than navigating the webview.
- The modal opens from the command palette as well as the button.
- The sidebar header still lays out without overflow at its minimum width with four icons.

## Non-goals

- No new keybinding for the Help modal, and no change to ⌘/.
- No change to the keyboard-shortcuts overlay's own content or title.
- No multi-section getting-started walkthrough or tutorial flow.
- No daemon, wire-protocol or persistence change.

## Notes

The `help` icon already exists in the sprite (`cmd/hivegui/frontend/src/lib/icons.svg`,
`#hv-help`, registered in `lib/icon-sprite.ts`) and is unused today.

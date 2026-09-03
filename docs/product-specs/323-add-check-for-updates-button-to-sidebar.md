---
issue: 323
title: "Add a check for updates button to the sidebar"
type: enhancement
complexity: S
priority: P2
pr: 325
stage: REVIEW
---

# Add a "check for updates" button to the sidebar

- **Issue:** #323
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/323-add-check-for-updates-button-to-sidebar.md](../exec-plans/active/323-add-check-for-updates-button-to-sidebar.md)

## Problem

Hive can already check for updates on demand, but the only manual trigger is the
macOS app menu's "Check for Updates…" item, which dispatches
`menu:check-for-updates` to `manualUpdateCheck()`. That menu is invisible on
non-macOS and undiscoverable in general — there is no in-window affordance for a
user to ask "am I on the latest build?". The machinery is all there; only the
button is missing.

## Desired behavior

The sidebar header carries a second small icon button immediately next to the
existing "New project" (+) button. Clicking it runs the same manual update check
the menu item runs, and the existing update banner reports the outcome —
checking, up to date, update available, or check failed.

## Success criteria

- A visible, keyboard-focusable button sits in the sidebar header immediately
  next to the "New project" button, with an accessible name of
  "Check for updates".
- Clicking it invokes the same manual update check path the menu item uses, and
  the update banner surfaces every outcome including "up to date" and failures.
- Rapid repeated clicks do not fire parallel GitHub API calls.
- The button is built from the shared `iconButton()` primitive and the icon
  sprite, carries no bespoke CSS, and visually matches the adjacent
  "New project" button.
- The sidebar brand still truncates with an ellipsis at narrow widths.

## Non-goals

- No new keybinding and no command-palette entry.
- No change to the update backend (`cmd/hivegui/update.go`), the 6-hour
  background check loop, or the update banner's own behavior.
- No porting of the sidebar header to React; that belongs to the in-flight React
  rewrite's own phase.

## Notes

Scaffolded via `/hs-feature-loop --full-auto plan`. TRIAGE / RESEARCH / PLAN were
satisfied by a full-auto reviewer pass (`verdict: approve`, confidence 8) after
one revision round.

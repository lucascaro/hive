---
issue: 327
title: "GUI: Cmd+Enter in grid mode focuses the active session"
type: enhancement
complexity: S
priority: P2
stage: IMPLEMENT
---

# GUI: Cmd+Enter in grid mode focuses the active session

- **Issue:** #327
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/327-cmd-enter-grid-focus-session.md](../exec-plans/active/327-cmd-enter-grid-focus-session.md)

## Problem

In a grid view (⌘G per-project, ⇧⌘G all-sessions) you navigate between tiles with ⌘-arrows, but zooming into the tile you landed on requires pressing the same grid shortcut that got you there — ⌘G or ⇧⌘G, and you have to remember which. There is no single "focus this one" key.

Cmd/Ctrl+Enter is currently unbound in the app. Spec #217 documented it as the grid-project toggle, but that binding has since moved to ⌘G/⇧⌘G, leaving ⌘⏎ free.

## Desired behavior

While a grid view is active, ⌘⏎ (Ctrl+Enter off macOS) switches to single view on the active tile's session. In single view the key is left alone: it is not swallowed and reaches the terminal, because agent CLIs bind Cmd+Enter themselves. This is the one-way half of the toggle #217 removed — hence "partly reverts".

## Success criteria

- In `grid-project` or `grid-all`, ⌘⏎ / Ctrl+Enter switches the view to `single` and the event is consumed (`preventDefault`).
- The session shown in single view is the one that was active in the grid; keyboard focus lands on its terminal.
- In `single` view, ⌘⏎ / Ctrl+Enter is **not** consumed and does not change the view — it reaches the terminal.
- ⇧⌘⏎ is not claimed in any view.
- ⌘G / ⇧⌘G continue to toggle grids unchanged; Shift+Enter still inserts a newline (#217) unchanged.
- The binding appears in the ⌘/ shortcuts overlay and the README shortcut table.

## Non-goals

- Making ⌘⏎ a two-way toggle (single → grid stays on ⌘G / ⇧⌘G).
- Adding a native macOS menu item or command-palette entry for it.
- Changing which session is active — ⌘⏎ only changes the view.
- Any change to Shift+Enter newline handling.

## Notes

Handler lives in the ⌘/Ctrl branch of the capture-phase window keydown listener in `cmd/hivegui/frontend/src/app/keyboard.ts`. `setView('single')` (`src/app/view.ts:410`) already restores terminal focus. Shortcut text is generated from `src/lib/shortcuts.ts`, whose header documents the five-file drift surface for a binding change.

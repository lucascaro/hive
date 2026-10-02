---
issue: 477
title: Make every app shortcut configurable, with collision resolution
type: enhancement
complexity: L
priority: P2
stage: REVIEW
pr: 490
---

# Make every app shortcut configurable, with collision resolution

- **Issue:** #477
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Exec plan:** [477-make-every-app-shortcut-configurable-with-collisio.md](../exec-plans/active/477-make-every-app-shortcut-configurable-with-collisio.md)

## Problem

Every Hive command has a fixed shortcut. Users with muscle memory from iTerm, VS Code or tmux can't adapt Hive to it. Users whose agent, shell or tmux needs a key Hive has claimed can't take that key back. The only workaround today is to live with the clash or avoid the command's shortcut altogether. Plugin shortcuts that collide with core ones are dropped silently, so a user never learns why a plugin's key does nothing.

## Desired behavior

- A new Shortcuts tab in Settings:
  - It lists every app command, grouped the same way as the help overlay, with a search box.
  - Plugin commands sit in their own section per plugin.
  - Terminal-editing shortcuts (⌘←/→, ⌘⌫, ⌥⌦/⌘⌦, Shift+Enter) appear in a read-only group, so you can find them but not edit them.
- Setting a shortcut:
  - You set one by pressing it.
  - A command can have several shortcuts or none. A command with none still works from the palette and menu.
  - You can reset one command, or everything, to the defaults.
- Collisions:
  - A new shortcut can collide with another core or plugin command in a scope where both can fire. When it does, both rows are highlighted with a "conflicts with X" note.
  - You can reassign (the other command loses that shortcut) or cancel. You can't save while a conflict remains.
  - Keys the OS reserves can't be bound. Keys that terminals and agents use (Ctrl+A/E/J/F/U…) give a warning but can be bound.
- Where a change shows up:
  - It takes effect immediately, with no restart.
  - Every surface that shows a shortcut shows the user's current one: help overlay, command palette, macOS native menu, inline [key]/(key) hints and button tooltips.
- Upgrades: when a Hive upgrade changes a default, your custom bindings are never changed. If a new default collides with one of yours, that command ships unbound and is flagged in the tab.
- Import/export:
  - You can export your keymap to a file and import one.
  - An import first shows a preview with each problem row marked. Unknown commands and OS-reserved keys are skipped, and conflicts go through the same resolver. Nothing changes until you confirm.

## Success criteria

1. After rebinding any core or plugin command in Settings → Shortcuts, the new shortcut triggers it with no reload, and the old shortcut no longer does.
2. After a rebind, the help overlay, command palette, macOS menu, and every inline hint or tooltip for that command show the new shortcut. The old one appears nowhere.
3. Binding a shortcut that another command in an overlapping scope already uses highlights both rows with a conflict message. You can't save until you reassign or cancel. No keypress ever fires two commands.
4. A plugin shortcut that collides with a core one shows up as a conflict in the tab rather than being dropped silently.
5. Trying to bind an OS-reserved shortcut is refused with an explanation. Binding a terminal-reserved one shows a warning and is allowed.
6. A command can be left with no shortcut, or given several, and each case works as shown.
7. Custom bindings survive GUI reload, daemon restart and app upgrade.
8. An upgrade whose new default collides with a custom binding leaves the custom binding alone, ships that command unbound, and flags it in the tab.
9. Export produces a file that import accepts. Importing a file with conflicts, unknown commands or OS-reserved keys shows a preview first and changes nothing until you confirm.
10. Terminal-editing shortcuts are listed in the tab and can't be edited.
11. A user who never opens the Shortcuts tab sees no change in behaviour, hints or menu.

## Non-goals

- Terminal-editing shortcuts can't be rebound.
- Keys inside overlays (arrows, Enter/Esc) can't be rebound, and neither can Ctrl+C. These stay the AGENTS.md hard-coded exceptions.
- No multi-step shortcut sequences (⌘K then ⌘S).
- No syncing a keymap between machines; the only way to move one is manual import/export.
- No per-project keymaps.
- The keymap-tables refactor ([478-decompose-keyboard-ts-into-per-scope-keymap-tables.md](478-decompose-keyboard-ts-into-per-scope-keymap-tables.md)) is not part of this spec. It is a separate prerequisite that ships first.
- The README documents the defaults only, not per-user keymaps.

## Notes

- **Open question:** does one keymap apply on every platform, or is there one per OS? This decides whether a keymap exported on macOS (⌘) means anything when imported on Windows/Linux (Ctrl).
- **Depends on:** [478-decompose-keyboard-ts-into-per-scope-keymap-tables.md](478-decompose-keyboard-ts-into-per-scope-keymap-tables.md) (per-scope keymap tables refactor), which must ship first.
- **Context:** the `lib/shortcuts.ts` header lists five drift surfaces (`app/keyboard.ts` + `lib/keymap.ts`, `lib/shortcuts.ts`, the palette table in `main.tsx`, the `menu_darwin.go` native menu, `README.md`). On macOS, ⌘ chords reach the webview as native menu events, not keydowns. Plugin chord collisions today resolve as core-wins silently (`lib/plugin-api.ts`).
- Originated from `/hs-brainstorm` (2026-09-30).

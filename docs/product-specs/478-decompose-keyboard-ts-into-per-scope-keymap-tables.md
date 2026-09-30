---
issue: 478
title: "Decompose keyboard.ts into per-scope keymap tables"
type: enhancement
complexity: L
priority: P2
stage: IMPLEMENT
---

# Decompose keyboard.ts into per-scope keymap tables

- **Issue:** #478
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/478-decompose-keyboard-ts-into-per-scope-keymap-tables.md](../exec-plans/active/478-decompose-keyboard-ts-into-per-scope-keymap-tables.md)

## Problem

`cmd/hivegui/frontend/src/app/keyboard.ts` is one ~820-line capture-phase window
handler holding every binding in the app plus the precedence between them:
choice dialog, project editor, settings, worktrees, help overlay, command
palette, launcher, inline rename, dead-session overlay, then the global chords.
The precedence is expressed as the order of a long `if`/`else if` chain, so
adding a binding means reading the whole chain to find where it may go, and
"which scope owns this key?" has no answer shorter than the file.

## Desired behaviour

Separate the four concerns that `keyboard.ts` currently fuses:

- **Commands**: one registry of `id → run`, shared by keydowns, the native
  menu (`menu:*` events) and the command palette, so the three paths can't
  drift. Core commands and plugin commands both register into it.
- **Bindings**: chord strings as data (`'Mod+Shift+K'`, per-platform
  `{ mac, other }`) that map to command ids, parsed and matched by one pure
  module.
- **Scopes**: one per modal or mode, each holding its bindings and its
  ownership rule (owns the keyboard, owns only matched keys, or text input).
  They are composed in **one** explicit precedence list.
- **Dispatcher**: the capture-phase listener, reduced to walking that list.

Operator decisions (2026-09-30): precedence stays a single central list;
the bus covers keys, menu and palette; bindings are chord strings with a
platform map; matching is exact. The one exception is overlay Escape/Enter,
which keeps ignoring modifiers.

## Non-goals

- **No behaviour change beyond the listed tightenings**, and none in the order
  scopes are consulted (the current order encodes shipped bug fixes). The
  operator-approved changes (2026-09-30) are:
  - (A) ⌥/Alt is no longer ignored on ⌘/Ctrl chords.
  - (B) ⇧⌘E and ⇧⌘S no longer act as ⌘E/⌘S. That includes ⇧⌘E closing an
    open worktree browser.
  - (D) The help chord needs exactly the platform modifier. On macOS ⌃/ no
    longer closes the shortcuts overlay or the Help modal; on Windows/Linux
    Meta+/ and Ctrl+Meta+/ no longer do.
  - (E) The palette's "Capture Idea…", "Ideas…" and "Keyboard Shortcuts" run
    the same toggle-and-gate command as their key and menu twins.
  - Everything else that ignores Shift today, or ignores modifiers on overlay
    Escape/Enter, stays loose, stated explicitly in the binding data.
- No move to per-component key handling. That was considered and rejected during
  the React rewrite: it re-derives precedence in several places, which is the
  regression risk this refactor exists to reduce, not add.
- No rebinding, no new bindings, no keymap UI, and no shortcut-label
  derivation (help overlay and palette labels stay in `lib/shortcuts.ts`).
  Those belong to #477.
- Feature modules do not self-register core commands at import. Registration
  happens through the central catalog, so module mocks in tests cannot
  silently drop a binding.

## Constraints

- The handler must stay a **single capture-phase window listener** — it has to
  beat inline-rename's `stopPropagation`.
- Modal precedence reads the store, never DOM classes.
- Per AGENTS.md › Keybindings Policy, any binding change must update
  `src/lib/keymap.ts`, the help overlay, the command palette and the README —
  but this refactor changes no binding, so those surfaces should not move.

## Success criteria

- Precedence is data (an ordered list of scopes), not control flow, and a test
  asserts the scope order explicitly.
- Keydown, menu event and palette entry for the same action run the same
  command id; the `menuActions` table and the palette table in `main.tsx` are
  gone.
- Bindings are chord strings parsed by one matcher. The `isHelpOverlayKey`,
  `navHistoryKey`, `activityKey` and `findKey` predicates are replaced by
  data, and their edge cases are ported as tests over the bindings.
- `test/dom/keyboard-precedence.test.tsx`, `keyboard-arrows.test.ts` and the
  other keyboard dom suites pass with only import-path / init-name edits. The
  suites for APIs that are removed (`chordMatches`, the keymap predicates,
  `initCommandPalette({commands})`) are ported to the new API case for case.
  The e2e and e2e-real keyboard specs pass unmodified.
- Each of the changes A, B, D and E has a test, and a changeset records them. A differential test
  checks the new matcher against a frozen copy of today's matchers over the
  key × modifier × platform matrix, and allows only the A, B and D cells to differ.

## Context

Filed by Phase 6 of the React UI rewrite ([spec](react-ui-rewrite.md)) as a
named debt item.

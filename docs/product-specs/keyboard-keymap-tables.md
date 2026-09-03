---
issue: null
title: "Decompose keyboard.ts into per-scope keymap tables"
type: enhancement
complexity: M
priority: P3
stage: TRIAGE
---

# Decompose keyboard.ts into per-scope keymap tables

- **Issue:** —
- **Type:** enhancement
- **Complexity:** M
- **Priority:** P3
- **Exec plan:** —

## Problem

`cmd/hivegui/frontend/src/app/keyboard.ts` is one ~820-line capture-phase window
handler holding every binding in the app plus the precedence between them:
choice dialog, project editor, settings, worktrees, help overlay, command
palette, launcher, inline rename, dead-session overlay, then the global chords.
The precedence is expressed as the order of a long `if`/`else if` chain, so
adding a binding means reading the whole chain to find where it may go, and
"which scope owns this key?" has no answer shorter than the file.

## Desired behaviour

Per-scope keymap tables (one per modal/mode), composed in an explicit precedence
list, with the dispatcher reduced to walking that list.

## Non-goals

- **No behaviour change of any kind**, including the order in which scopes are
  consulted. The current order encodes shipped bug fixes.
- No move to per-component key handling. That was considered and rejected during
  the React rewrite: it re-derives precedence in several places, which is the
  regression risk this refactor exists to reduce, not add.
- No rebinding, no new bindings, no keymap UI.

## Constraints

- The handler must stay a **single capture-phase window listener** — it has to
  beat inline-rename's `stopPropagation`.
- Modal precedence reads the store, never DOM classes.
- Per AGENTS.md › Keybindings Policy, any binding change must update
  `src/lib/keymap.ts`, the help overlay, the command palette and the README —
  but this refactor changes no binding, so those surfaces should not move.

## Success criteria

- Precedence is data (an ordered list of scopes), not control flow.
- The order is provably identical to today's — the existing precedence tests
  (`test/dom/keyboard-precedence.test.tsx`) pass unmodified, and a test asserts
  the scope order explicitly.
- `test/dom/keyboard-arrows.test.ts` and the e2e keyboard specs pass unmodified.

## Context

Filed by Phase 6 of the React UI rewrite ([spec](react-ui-rewrite.md)) as a
named debt item.

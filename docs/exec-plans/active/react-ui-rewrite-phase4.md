# React UI rewrite — Phase 4: Modals B + keyboard reads the store

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Scope

New files: `src/components/modals/Worktrees.tsx` (port of the 581-line `src/app/modals/worktrees.ts`, reusing the pure logic in `src/lib/worktrees.ts` — two distinct files), `ProjectEditor.tsx`, `CommandPalette.tsx`, `HelpOverlay.tsx`, `ChoiceDialog.tsx` (rendered from store state `choiceDialog: {question, options} | null` — fixes the forgot-to-unregister keyboard-strand hazard by construction).

Files to change / delete: `src/app/modals/worktrees.ts`, `project-editor.ts`, `command-palette.ts`, `help-overlay.ts`, `choice-dialog.ts`, `registry.ts` — deleted (open/close wrappers over actions kept where callers need them). `src/app/keyboard.ts` — per-modal `.hidden` DOM queries become store reads; **precedence ladder copied verbatim**: inline-rename → choice dialog → launcher → project editor → command palette → settings → worktrees → help → dead-session → app bindings. `src/app/modals/focus-trap.ts` moves to `src/lib/focus-trap.ts`.

## Success criteria

What `/hs-merge-gate` validates for THIS phase.

- Worktrees, project editor, command palette, help overlay and choice dialog
  render from React into the same ids; `modals/registry.ts` is deleted.
- `keyboard.ts` reads modal state from the store instead of querying `.hidden`
  in the DOM, and its precedence ladder is **verbatim** the legacy order:
  inline-rename → choice dialog → launcher → project editor → command palette →
  settings → worktrees → help → dead-session → app bindings, pinned by a
  table-driven test over all 9 layers.
- The keyboard handler is still registered capture-phase.
- The choice dialog is rendered from store state, so a forgotten unregister
  cannot strand the keyboard — proven by an open → answer → cleanup test.
- `modals/focus-trap.ts` has moved to `src/lib/focus-trap.ts`.
- The port reuses `src/lib/worktrees.ts` rather than re-deriving its logic.

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

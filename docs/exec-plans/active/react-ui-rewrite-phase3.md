# React UI rewrite — Phase 3: Modals A: launcher + settings

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Scope

New store state: `modals: ModalId[]` stack + `openModal/closeModal/anyModalOpen` actions. Legacy `modals/registry.ts` keeps working for still-legacy modals; `anyModalOpen()` ORs both sources until Phase 4.

New files: `src/components/modals/ModalShell.tsx` (root-id passthrough, `.hidden` toggling from store, focus trap via existing `focus-trap.ts` helpers, Enter/Esc per AGENTS.md, visible confirm/cancel key hints), `Launcher.tsx` (faithful port of the 680-line `launcher.ts` incl. search via `src/lib/shortcuts.ts`, stacking, open-generation token semantics — do not "improve" flows mid-port), `Settings.tsx` (473-line port: theme picker via `src/theme/theme.ts`, font size, custom agents CRUD, update settings via `src/lib/update-state.ts`).

Files to change: `src/app/modals/launcher.ts`, `settings.ts` deleted; their `openX/closeX` exports become thin wrappers over store actions (callers in `keyboard.ts`/`events.ts`/`main.ts` keep compiling). `index.html` — launcher/settings markup reduced to empty root divs with the same ids.

## Success criteria

What `/hs-merge-gate` validates for THIS phase.

- Launcher and settings render from React into the same root ids, and the
  `.hidden` visibility contract is unchanged.
- The launcher's search (`src/lib/shortcuts.ts`), stacking, and open-generation
  token semantics are ported faithfully — no flow is "improved" mid-port.
- Settings keeps theme picking via `src/theme/theme.ts`, font size, custom-agent
  CRUD, and update settings via `src/lib/update-state.ts`.
- `ModalShell` acquires and releases the existing focus trap, handles Enter/Esc
  per AGENTS.md, and shows visible confirm/cancel key hints.
- `openLauncher` / `openSettings` / `closeSettings` still exist as callable
  exports, so `keyboard.ts`, `events.ts` and `main.ts` compile unchanged.
- `anyModalOpen()` reports correctly while the legacy registry still owns the
  not-yet-migrated modals.

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

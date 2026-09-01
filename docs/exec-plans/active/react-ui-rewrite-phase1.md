# React UI rewrite — Phase 1: Sidebar island

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Scope

First React root; exercises the whole pattern (lists, selection, drag reorder, inline rename, dblclick). React reconciliation with stable keys fixes the documented dblclick-eating rebuild bug (`sidebar.ts:150`).

New files (`src/components/`):
- Primitives ported from `src/ui/*` with identical markup: `Button.tsx`, `Icon.tsx`, `IconButton.tsx`, `Kbd.tsx`, `Chip.tsx`, `SessionRow.tsx`, `ProjectCard.tsx`. Companion `updateX()` patch fns are not ported — props replace them.
- `Sidebar.tsx` — renders `#projects` UL contents + `#minimized-projects`; selector-subscribes to `projects/sessions/activeId/collapsed/minimizedProjects/attention/phaseById/aliveById`; `key={project.id}` / `key={session.id}`.
- `useInlineRename` / `useDragReorder` — hooks wrapping `src/lib/reorder.ts` and the flow from `src/app/inline-rename.ts`, defined locally inside `Sidebar.tsx`; split into own files only if a second consumer appears.

No `roots.ts` module: each island phase calls `createRoot(el).render(node)` in `main.ts` and pushes the Root handle into a local array there (Phase 6 unmounts them when collapsing to the single root).

Files to change / delete:
- `src/main.ts` — mount sidebar root instead of `initSidebar()`.
- `src/app/sidebar.ts` — deleted. Sidebar-resizer drag becomes `useSidebarResize.ts` hook (or stays a small imperative module wired in `main.ts`; implementer picks one and notes it in the PR).
- `src/app/events.ts`, `view.ts`, `keyboard.ts`, **`session-term.ts` (calls `updateSidebarSelection()` at :617, imported at :93)** — remove all `renderSidebar()/updateSidebarSelection()/updateSidebarTitles()` calls and their deps seams.
- `src/ui/session-row.ts`, `project-card.ts`, `chip.ts` — deleted (no remaining callers). `button/icon/icon-button/kbd/banner` stay until Phase 2.
- `src/app/dom.ts` — drop `projectsUL`/`minimizedProjectsUL` singletons.

## Success criteria

What `/hs-merge-gate` validates for THIS phase.

- `#projects` and `#minimized-projects` are rendered by a React root; the
  markup is byte-identical on ids, `hv-*` classes and data-attributes.
- `src/app/sidebar.ts` is deleted, along with `src/ui/session-row.ts`,
  `project-card.ts` and `chip.ts` — no remaining callers (`rg` clean).
- No module calls `renderSidebar` / `updateSidebarSelection` /
  `updateSidebarTitles` any more, including `session-term.ts`.
- Double-click-to-rename works: the row's DOM node survives the re-render
  between the two clicks (this is the bug the old rebuild caused, so it is a
  required test, not an observation).
- Drag reorder still routes through `src/lib/reorder.ts`.
- `src/app/dom.ts` no longer exports the `projectsUL` / `minimizedProjectsUL`
  singletons.

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

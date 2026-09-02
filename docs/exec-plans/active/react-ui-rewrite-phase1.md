# React UI rewrite — Phase 1: Sidebar island

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **PR:** #317
- **Branch:** `react-phase1-sidebar`
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

## Brief

Written 2026-09-02 against `236086d`, before implementation, per the master
plan's Execution model.

### Mount points

`#projects` (a `<ul>`) and `#minimized-projects` (a `<div role="toolbar">`) are
siblings inside `#sidebar`, separated by `#sidebar-hints` and the resizer, so
one root cannot span both. **One `createRoot(#projects)`** renders the project
`<li>`s; the chip tray is reached from the same component tree with
`createPortal(chips, #minimized-projects)`. One store subscription, one render
pass, and the tray's `.hidden` toggle (which React cannot own — the class sits
on the portal *container*) is a `useEffect` on the chip count, mirroring
`sidebar.ts:175`.

### Component inventory (`src/components/`)

| File | Ports | Notes |
|---|---|---|
| `Icon.tsx` | `src/ui/icon.ts` `icon()` / `stateIcon()` | `<Icon name size?>` + `<StateIcon state>`; calls `ensureSprite()` per render, same as `icon()` does today. `src/ui/icon.ts` stays (Phase 2 deletes it). |
| `IconButton.tsx` | `src/ui/icon-button.ts` | Same throw-on-empty-label guard. |
| `Kbd.tsx` | `src/ui/kbd.ts` | One line. |
| `Chip.tsx` | `src/ui/chip.ts` | Project chips only this phase. |
| `SessionRow.tsx` | `src/ui/session-row.ts` | `sessionRow()` + `applyState()` collapsed into one render; no `updateX()` twin. |
| `ProjectCard.tsx` | `src/ui/project-card.ts` | Ditto; `body` becomes `{children}`. |
| `Sidebar.tsx` | `src/app/sidebar.ts` | Cards + rows + tray portal + drag + rename. |

### Deviations from the phase plan (with reasons)

1. **`src/ui/chip.ts` is NOT deleted.** `src/app/view.ts:30,646` still builds the
   minimized-*session* tray (`#minimized-tray`) with it, and that tray is Phase
   2's chrome island. `Chip.tsx` and `chip()` coexist for one phase; Phase 2
   deletes the imperative one. `session-row.ts` and `project-card.ts` *are*
   deleted — nothing else calls them.
2. **`Button.tsx` is not ported.** No React consumer exists until Phase 2's boot
   state; `src/ui/button.ts` is still used imperatively by `app/dom.ts`. Porting
   it now would be an unused file.
3. **`aliveById` / `phaseById` are not subscribed.** `sessionState()` reads
   `alive` and `phase` off the `SessionInfo` itself; the two maps are
   transition-detection bookkeeping for `events.ts` and nothing in the sidebar
   reads them. Subscribed instead: `currentProjectId`, `view`, `gridProjectId`,
   which `activeProjectId()` does read.
4. **`test/dom/selectors.test.ts` needs no rewrite.** It exercises
   `nextAttentionId()` against the state facade with no DOM and no sidebar
   import; the master plan's Phase 1 test list mis-scoped it.
5. **`test/dom/sidebar-focus.test.ts` IS rewritten** (missing from the master
   plan's list): it drives `renderSidebar` / `updateSidebarRows` directly, both
   of which are deleted. It becomes an RTL suite asserting the property that
   made `preserveFocus` necessary — focus survives a store update — which React
   gives structurally.
6. **The sidebar resizer stays the imperative IIFE in `main.ts`.** The phase
   plan left this to the implementer: it drives `--sidebar-width` on `#app`
   (outside every React root) via pointer capture, reads nothing React renders,
   and already routes its persistence through `store.setSidebarWidth`. A
   `useSidebarResize` hook would move code without changing it.

### Inline rename: reuse `beginInlineRename` verbatim

`app/inline-rename.ts` owns the module-level `active` handle that
`keyboard.ts` consults through `inlineRenameActive()` / `cancelInlineRename()`.
Re-implementing the flow as React state would have to re-export that handle, so
the port keeps calling `beginInlineRename({ mount: input => nameEl.replaceWith(input), … })`
against the React-rendered name node, exactly as `sidebar.ts:562-586` does.

This is safe because React never inserts a sibling *around* either name node:
`.hv-session-row__name` and `.hv-session-row__sub` are the static children of
`.hv-session-row__text`, and the project header's five children
(chevron/swatch/name/count/actions) are all unconditional. React reconciles
per-parent, so a detached name node is never used as an `insertBefore`
reference. Text updates during an open rename land on the detached node and are
discarded — the same behaviour as today's `applyState`, which skips a name that
is not in the DOM.

### `renderEmptyState()` fan-out

`renderSidebar()` and `updateSidebarSelection()` each ended in
`deps.renderEmptyState()`, so deleting them silently drops the empty-state
repaint from every path that called them. Every removed call becomes a direct
`renderEmptyState()`:

| File | Lines | Was |
|---|---|---|
| `view.ts` | 105, 176, 412, 419, 466, 753 | `updateSidebarSelection()` |
| `view.ts` | 538, 563, 590, 600 | `renderSidebar()` |
| `events.ts` | 184, 193, 236 | `updateSidebarSelection()` |
| `events.ts` | 282, 322, 393, 422, 532 | `renderSidebar()` |
| `events.ts` | 435 | `updateSidebarTitles()` — pure repaint, drops out |
| `events.ts` | 531 | `updateSidebarRows()` |
| `keyboard.ts` | 500 | `updateSidebarSelection()` |
| `session-term.ts` | 623 | `updateSidebarSelection()` |

`view.ts` calls it locally. `keyboard.ts` and `session-term.ts` already import
from `./view.js` — the symbol joins those imports. `events.ts` is deliberately
kept out of view.ts's import graph, so `renderEmptyState` joins `EventsDeps`
and is injected from `main.ts` like `renderGrid` / `renderMinimizedTray`.
Where the call would immediately follow `deps.renderMinimizedTray()` (which
already ends in `renderEmptyState()`), it is dropped rather than duplicated.

### Tests

Rewritten to RTL, same class/data-attribute assertions:
`ui-session-row.test.tsx`, `ui-project-card.test.tsx`, `ui-chip.test.tsx`,
`sidebar-title.test.tsx`, `minimize-project.test.tsx`, `attention-icon.test.tsx`,
`sidebar-focus.test.tsx`.

New: `sidebar-dblclick-rename.test.tsx` (the row's DOM node is the *same node*
across a store update landing between the two clicks — the bug the old rebuild
caused), `sidebar-reorder.test.tsx` (drop above/below routes through
`lib/reorder.ts`'s `dropTargetIndex` and calls `UpdateSession` with its result).

`@testing-library/user-event` stays unadded: `fireEvent.dblClick` and synthetic
`DragEvent`s with a stub `dataTransfer` cover both new suites.

## Decision log

- **One root on `#projects`, chips through `createPortal`.** `#projects` and
  `#minimized-projects` are separated by other markup, so no single root spans
  both, and a second root would mean a second subscription and a second render
  pass over the same data. The portal keeps one component, one subscription and
  one pass. The tray's `.hidden` class lives on the portal *container*, outside
  React's tree, so a `useEffect` on the chip count applies it — the one place
  the island reaches out of React, and the direct replacement for
  `sidebar.ts:175`.
- **Deps stay injected as props from `main.ts`.** `initSidebar(deps)` existed
  because a direct `view.ts` / `keyboard.ts` import from the sidebar closes an
  import cycle; that is still true of `Sidebar.tsx`. The modals
  (`openLauncher` / `openWorktrees` / `openProjectEditor`) keep being imported
  directly, exactly as `sidebar.ts` did.
- **`activeProjectId()` is subscribed as a derived string**
  (`useAppStore(() => activeProjectId())`). It reads `currentProjectId`, `view`,
  `gridProjectId`, `activeId`, `projects` and `sessions` off the state facade
  rather than taking them as arguments; selecting the computed id means the
  selector re-runs on every store change but only a *different* id re-renders.
  Rejected: subscribing to all six slices (re-renders on unrelated churn) and
  re-deriving the precedence rules inside the component (a second copy of a
  tested function).
- **Inline rename keeps calling `beginInlineRename`.** `app/inline-rename.ts`
  owns the module-level handle `keyboard.ts` consults via
  `inlineRenameActive()` / `cancelInlineRename()`; a React-state rename would
  have to re-export that. The mount/unmount pair does `replaceWith` on the
  React-rendered name node, which is safe because React never inserts a sibling
  around either name node (both sit among unconditional children), so a
  detached name is never used as an `insertBefore` reference. Text updates
  during an open rename land on the detached node and are discarded — the same
  behaviour as `applyState`, which skipped a name that was not in the DOM.
- **The colour swatch is an uncontrolled input.** A controlled `value` would
  snap the swatch back to the stored colour on every unrelated re-render while
  the user is still dragging inside the native picker — a real behaviour change,
  since the imperative row only rewrote the value when session data arrived. A
  ref + an effect keyed on `s.color` reproduces that exactly. Pinned by
  `ui-session-row.test.tsx` › "keeps a mid-edit swatch value…".
- **Every deleted `renderSidebar()` / `updateSidebarSelection()` call became
  `renderEmptyState()`.** Both ended in `deps.renderEmptyState()`, so deleting
  them silently drops the empty-state repaint from every path that called them.
  `renderEmptyState` joins `EventsDeps` for `events.ts` (kept out of view.ts's
  import graph on purpose) and joins the existing `./view.js` import in
  `keyboard.ts` and `session-term.ts`. Where the call would immediately follow
  `deps.renderMinimizedTray()` — which already ends in `renderEmptyState()` —
  it was dropped rather than duplicated.
- **`main.ts` mounts with `createElement`, not JSX.** Renaming it to
  `main.tsx` would touch `index.html`'s entry and `tsconfig`'s `include` for one
  call site. Phase 6 collapses the islands into a single root and can rename it
  then, with a reason.
- **Deviations 1–6 from the phase plan** are recorded in the Brief above with
  their reasons: `src/ui/chip.ts` survives (view.ts's session tray still uses
  it), `Button.tsx` is not ported (no React consumer yet), `aliveById` /
  `phaseById` are not subscribed (nothing in the sidebar reads them),
  `selectors.test.ts` needed no rewrite, `sidebar-focus.test.ts` did (and was
  missing from the plan), and the sidebar resizer stays imperative in `main.ts`.
- **`ui-chip.test.ts` is kept alongside the new `ui-chip.test.tsx`.** Deleting
  the test for `src/ui/chip.ts` while that module still draws the
  minimized-session tray would leave live code uncovered. Both assert the same
  markup contract; Phase 2 deletes the imperative pair together.
- **`@testing-library/user-event` still not added.** `fireEvent.dblClick`,
  `fireEvent.keyDown` and a synthetic `drop` Event carrying a stub
  `dataTransfer` cover the dblclick-rename and drag-reorder suites. The stub's
  `getData` honours the requested key, like a real `DataTransfer` — the session
  row's drop handler does not gate on `types` (it never did), it asks for its
  own payload and gets `''` when the drag carries something else.

## Progress

**2026-09-02** — Implemented on `react-phase1-sidebar`, branched from
`236086d`.

- New: `src/components/{Icon,IconButton,Kbd,Chip,SessionRow,ProjectCard,Sidebar}.tsx`.
- Deleted: `src/app/sidebar.ts`, `src/ui/session-row.ts`, `src/ui/project-card.ts`.
- Changed: `src/main.ts` (mounts the root, drops `initSidebar`),
  `src/app/{view,events,keyboard,session-term}.ts` (the `renderEmptyState`
  fan-out), `src/app/dom.ts` (drops the `projectsUL` /
  `minimizedProjectsUL` singletons).
- Tests: `ui-session-row`, `ui-project-card`, `sidebar-title`,
  `attention-icon`, `sidebar-focus` and `minimize-project` rewritten to RTL;
  new `ui-chip.test.tsx`, `sidebar-dblclick-rename.test.tsx`,
  `sidebar-reorder.test.tsx`, and a shared `sidebar-harness.tsx`.
- Docs: `docs/design-docs/ui/components.md` now says where the React and
  imperative primitives each live; `icons.md`'s `src/app/sidebar.ts` reference
  replaced with the card's `data-action="edit"` control.

Verification, all from a clean bootstrap of this worktree:

| Check | Result |
|---|---|
| `npm run typecheck` | clean |
| `npm run ci` (biome) | 0 errors, 10 warnings — all pre-existing |
| `scripts/ui-lint.sh --strict` | 0 violations |
| `scripts/ui-lint.sh --contrast` | 18 presets, 0 failures |
| `npx vitest run` (unit + dom) | 73 files, 821 tests passed |
| `CI=1 npx playwright test` (e2e mock) | 258 passed, 31 skipped — **zero spec files changed** |
| `CI=1 npm run test:e2e:real` | 22 passed, 2 skipped |
| `scripts/test.sh go` | pass |

No flake-baseline comparison was needed: nothing failed. `scripts/test.sh go`
first reported `TestEmbeddedAssetsIncludeWebfonts` /
`TestEmbeddedAssetsIncludeFontLicences` failing — `scripts/ci-bootstrap.sh`
seeds `frontend/dist` with a placeholder, and those two read the real embedded
assets. Green after `npm run build`; unrelated to this phase, and worth knowing
for the next fresh worktree.

No changeset: the phase is behaviour-preserving and carries `no-changeset`, per
the spec's Notes. The one changeset for the whole rewrite lands in Phase 6.

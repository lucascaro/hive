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
- `src/ui/session-row.ts`, `project-card.ts` — deleted (no remaining callers). `chip.ts` stays too (see Brief › Deviation 1: view.ts's minimized-session tray), along with `button/icon/icon-button/kbd/banner`, until Phase 2.
- `src/app/dom.ts` — drop `projectsUL`/`minimizedProjectsUL` singletons.

## Success criteria

What `/hs-merge-gate` validates for THIS phase.

- `#projects` and `#minimized-projects` are rendered by a React root; the
  markup is byte-identical on ids, `hv-*` classes and data-attributes.
- `src/app/sidebar.ts` is deleted, along with `src/ui/session-row.ts` and
  `project-card.ts` — no remaining callers (`rg` clean). **`src/ui/chip.ts`
  is explicitly NOT part of this criterion** (see Brief › Deviation 1):
  `src/app/view.ts` still builds the minimized-*session* tray with it, and
  that tray is Phase 2's chrome island. Phase 2 deletes it.
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

### Review round 1 (2026-09-02)

`/hs-review-loop` iteration 1 returned **COMMENT** — no BLOCKING findings, zero
unresolved threads, and the frozen DOM contract independently re-verified (e2e
green, zero spec edits). Strict mode off means that verdict converges the loop,
but all four IMPORTANT findings were real and were fixed rather than waved
through:

1. **RTL never cleaned up between tests.** `vitest.config.js` sets
   `globals: false`, so `@testing-library/react`'s auto-`afterEach(cleanup)`
   never registers — every `render()` left a mounted root behind, and
   `inline-rename.ts`'s module-level `active` editor leaked across tests, which
   would have let a regression in the second-and-later rename-open path pass.
   `test/dom/setup-rtl.ts` now runs `cancelInlineRename()` then `cleanup()` in
   an explicit `afterEach`.
2. **Nothing was memoized**, so every store write re-rendered every row —
   undoing exactly what `updateSidebarTitles()` (spec 248) was added to
   prevent, and missing this rewrite's stated performance goal. `SessionItem`
   is now `memo`'d, and the three bound callbacks it took were replaced by the
   referentially stable `sidebar` prop bag (a fresh `() => switchTo(id)` per
   render would have defeated the memo). `ProjectItem` is deliberately left
   unmemoized: a card is a header plus five icon buttons, its attention count
   derives from the whole attention set, and the rows beneath it are now
   insulated. New `test/dom/sidebar-render-scope.test.tsx` pins the scope by
   counting `SessionRow` renders — it fails 4/5 with the `memo` removed.
3. **Six live source comments still cited deleted modules.** Repointed:
   `drag-placeholder.ts` (×2), `preserve-focus.ts`, `term-title.ts`,
   `reorder.ts`, `modals/launcher.ts`, `events.ts`. AGENTS.md: stale docs are a
   bug.
4. **`src/components/**/*` was missing from `tsconfig.json`'s `include`** — the
   silent-failure trap that file's own header warns about in capitals. Added.

MINOR items also applied: the two `events.ts` comments that overstated the
re-render scope now describe what the memo actually does; `IconButton`'s
declared-but-never-rendered `children` prop is gone; the unreachable
`else deps.renderEmptyState()` in the `session:event` handler is folded away
(`added` and `title` both return above it); and this plan's Success criteria no
longer demand `chip.ts`'s deletion, which contradicted Brief › Deviation 1 and
would have failed `/hs-merge-gate`. `drag-placeholder.ts`'s now-vestigial
`resolve()` is left in place with a comment saying so — retiring it belongs to
Phase 6, with the last imperative render path.

Re-verified after the fixes: typecheck clean · biome 0 errors / 10 pre-existing
warnings · ui-lint 0 violations · 74 files, 826 vitest tests · 258 e2e passed /
31 skipped, still zero spec edits · 22 e2e-real passed / 2 skipped.

### Review round 2 (2026-09-02)

Iteration 2 verified all four round-1 fixes as correct (including that the new
`sidebar-render-scope.test.tsx` genuinely fails with the `memo` stripped) and
returned **COMMENT** with two IMPORTANT and four MINOR items, on a different
findings hash — the loop is converging, not stalling.

**Applied:**

- **`preserveFocus`'s rebuild branch was dead code.** Its only caller is
  `view.ts:332`, which *reparents* tiles, so `root.contains(keep)` always
  short-circuits before the rebuild path; the sidebar was the only thing that
  ever replaced nodes, and the `hv-session-row__*` / `data-action` matchers
  only ever matched sidebar markup. Verified by reading the caller, not taken
  on the reviewer's word. `matcherFor` and the relocate-by-`data-sid` tail are
  deleted; the header comment now says why they went.
- **The tray `.hidden` toggle is a `useLayoutEffect`.** `#minimized-projects`
  carries a `border-top` and padding, and a passive effect applies the class
  after paint — restoring the last minimized project could flash one frame of
  an empty bordered stripe. The deleted `renderMinimizedProjects()` toggled it
  in the same block that emptied the tray; this is the same synchrony.
- MINOR: `src/ui/icon.ts`'s `stateIcon` comment no longer claims the sidebar
  row as a consumer, and every primitive in `docs/design-docs/ui/components.md`
  now names its implementing file(s) — previously only three of the six ported
  ones did.

**Investigated and NOT applied — "sidebar reorder blurs focus to `<body>`":**
not reproducible. A raw `insertBefore` of a focused node does blur, in jsdom as
in a browser (measured), but React's reconciler moves the minimum set of
children and the rows that stay keep both their node *and* their parent, so
nothing is detached and nothing is blurred. Measured directly: with focus on a
row's kill button and the list reordered underneath it, `document.activeElement`
is still that button, on every arrangement tried. A focus-restore layout effect
was written, then deleted — it could not be made to fail without it, and
untestable speculative behaviour code is worse than the gap it guesses at. What
landed instead is `sidebar-focus.test.tsx` › "keeps focus on a row control
across a reorder that moves it", which pins the property that makes the layer
unnecessary and fails if React ever starts rebuilding rows.

**Investigated and NOT applied — stale `renderSidebar` / `preserveFocus`
comments inside `test/e2e/*.spec.ts` and `test/dom/drag-placeholder.test.ts`:**
correct, but touching an e2e spec — even a comment — costs the crispest
evidence this phase has, that not one spec file changed. Left for Phase 6,
which deletes the last imperative render path and can fix the comments in the
same sweep.

Re-verified after these changes: typecheck clean · biome 0 errors / 10
pre-existing warnings · ui-lint 0 violations · 74 files, 828 vitest tests · 258
e2e passed / 31 skipped, still zero spec edits · 22 e2e-real passed / 2 skipped.

## PR convergence ledger

Append-only, one line per `/hs-review-loop` iteration. Built by hand because
this feature's plans are named rather than `<NNN>`-prefixed, so the skill's
plan lookup does not find them.

- **2026-09-02 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 9398f44440be87d2a4269103246c29cb5f4720dfdb24a7d122a5cb1fe91d232e; threads_open: 0; action: stop (converged; 4 IMPORTANT applied by hand — see Review round 1); head_sha: eac2fda.
- **2026-09-02 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 88cdfafb994c18aadba6f53db181d0f1aa5ca4280fd9a0f2afa04c3c6d23e7c3; threads_open: 0; action: stop (converged; 2 IMPORTANT + 2 MINOR applied, 1 MINOR investigated and refuted, 1 deferred — see Review round 2); head_sha: 9b68a26.

### Reconstructed for the merge gate (2026-09-03)

`/hs-merge-gate`'s cold-start guard reads this ledger and requires its **latest**
entry to carry `verdict: APPROVE|COMMENT`, `threads_open: 0` and `action: stop`.
The entries above predate that requirement and use this project's own wording
(`action: converged`, or a bare post-loop note), so the gate would refuse a plan
that had in fact converged. The line below restates the final state of that
convergence in the vocabulary the gate parses. It adds no new claim: every fact
in it is taken from the entries above and from `gh pr view`. Appended, never
rewritten — the ledger is append-only.

- **2026-09-03 reconciliation** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 88cdfafb; threads_open: 0; action: stop; head_sha: 9b68a26. Restates iter 2 above (the loop's last iteration) for the gate's parser; PR #317 merged 2026-09-02 as `950dfaf`.

## Gate verdict

_Not run._ Phase 1's PR (#317) merged on 2026-09-02 (`950dfaf`) before this feature
adopted `/hs-merge-gate`, so there is no gate record for it. Left empty rather
than back-filled: a gate verdict asserts that someone walked the success
criteria against the diff, and writing one now without doing that would be a
claim, not a record. `/hs-merge-gate` has a degraded post-merge path that can
still be run against the merge commit if this phase needs a verdict on file.

# React-ify SessionTerm's tile chrome — Phase 1: portal infrastructure + the tile header

- **Master plan:** [329-react-ify-sessionterms-tile-chrome.md](329-react-ify-sessionterms-tile-chrome.md)
- **Spec:** [docs/product-specs/329-react-ify-sessionterms-tile-chrome.md](../../product-specs/329-react-ify-sessionterms-tile-chrome.md)
- **Issue:** —
- **Branch:** `feature/329-tile-chrome-phase1`
- **PR:** —
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Summary

Stand up the portal boundary and move the tile header onto it. After this phase
the header, its state icon, its two icon buttons and the inline rename all
render from React; the overlays are untouched and still imperative, so
`src/ui/icon.ts` stays alive for Phase 2.

## Approach

Master plan's **Approach**. This phase builds the mount points, the membership
subscription and the `tileChrome` slice, then ports one region through them.

### Files to change

- `src/store/terms.ts` — monotonic version counter bumped in `setTerm` /
  `deleteTerm` / `clearTerms`; `subscribeTerms(cb)` and `useTermIds()` built on
  `useSyncExternalStore`. The `Map` and its values stay out of the store.
- `src/store/store.ts` — `tileChrome: Record<string, TileChromeState>` slice
  plus `setTileTitle`, `setTilePhase`, `setTileDead`, `setTileRenaming`,
  `dropTileChrome`. Phase 1 only writes `termTitle` and `renaming`; the
  remaining fields are declared here so Phase 2 adds no slice churn.
- `src/app/session-term.ts` — constructor keeps `host`, an **empty**
  `div.tile-header`, `body`, and appends a style-less `div.tile-overlays` after
  `body`. The header construction block (243–317) is deleted along with the
  `tileState` / `tileName` / `tileWorktree` / `tileProject` / `tileTermTitle` /
  `tileActions` / `tileMinimize` fields. `refreshStateIcon()` becomes a no-op
  forwarding to the store (kept as a method: `events.ts` calls it directly on
  the bell paths). `_renderTermTitle()` and `_beginRename()` become store
  writes. `setInfo()` stops patching header nodes. `destroy()` calls
  `dropTileChrome(this.info.id)` before `host.remove()`.
- `src/components/App.tsx` — mount `<TileChromeHost />`.
- `src/app/state.ts` — `TermTile`'s header-node fields removed from the type;
  the dom-test stubs that set them updated in the same commit.

### New files

- `src/components/TileChrome.tsx` — `TileChromeHost` (membership subscription,
  one portal per live term) and `TileHeader` (state icon, name, worktree
  `IconButton`, term title, project, actions, and the rename `input`).

### Tests

- `test/dom/tile-chrome.test.tsx`
  - `renders header children in contract order` — state icon, `.tile-name`,
    `.tile-worktree`, `.tile-term-title`, `.tile-project`,
    `.tile-actions > .tile-minimize`.
  - `hides the worktree marker without a branch` — absent `worktree_branch` →
    `hidden`; present → `title` names the branch.
  - `hides the term title until one arrives` — empty `termTitle` → `hidden`, so
    the `::before` separator cannot render a lone `·`.
  - `repaints one tile on a bell` — an `attention` flip re-renders that id's
    header only, and calls no `ensureAttached()`.
  - `leaves the header box at 28px before React fills it` — a freshly
    constructed `SessionTerm` has an empty `.tile-header` whose
    `getBoundingClientRect().height` already matches a filled one.
- `test/dom/tile-rename.test.tsx`
  - `commits on enter` — `input.tile-name-input` appears, Enter calls
    `UpdateSession`.
  - `cancels on escape` — no call, span restored.
- Updated: any dom test stubbing `TermTile`'s removed header fields.

## Invariants

Every phase honours the master plan's **Invariants** section.

## Verification

Per the master plan's **Verification** block. `e2e` and `e2e-real` specs are run
unmodified; `chrome.spec.ts`, `theme.spec.ts`, `minimize.spec.ts`,
`worktrees.spec.ts` and `silent-failures.spec.ts` are the header's acceptance
gate.

## Success criteria

- The tile header renders from `TileChrome.tsx`; `session-term.ts` creates no
  header child and holds no header node fields.
- `store/terms.ts` exposes membership reactively; no `SessionTerm` value is in
  the store.
- The DOM contract table in the master plan holds; no `e2e` or `e2e-real` spec
  is edited.
- `src/ui/icon.ts` and `src/ui/icon-button.ts` still exist (the overlays and
  `banners.ts` still use them) — deleting them is Phase 2's criterion, not this
  one.

## Decision log

## Progress

- **2026-09-03** — Scaffolded from the approved plan-first plan.

## Open questions

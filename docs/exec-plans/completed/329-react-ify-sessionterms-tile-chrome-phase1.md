# React-ify SessionTerm's tile chrome — Phase 1: portal infrastructure + the tile header

- **Master plan:** [329-react-ify-sessionterms-tile-chrome.md](329-react-ify-sessionterms-tile-chrome.md)
- **Spec:** [docs/product-specs/329-react-ify-sessionterms-tile-chrome.md](../../product-specs/329-react-ify-sessionterms-tile-chrome.md)
- **Issue:** —
- **Branch:** `feature/329-tile-chrome-phase1`
- **PR:** [#329](https://github.com/lucascaro/hive/pull/329)
- **Status:** completed

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
  - `renders the session name, project and state icon`.
  - `follows the live phase, not the phase on the payload` — the render
    half of the pair `session-phase.test.ts` owns the write half of.
  - `renders nothing for a term with no chrome state`.
- `test/dom/tile-chrome.test.tsx` › `tile rename` (same file, same
  scaffold — the rename is three assertions, not a suite)
  - `opens an editor on double-click and commits on enter` —
    `input.tile-name-input` appears, Enter calls `UpdateSession`.
  - `cancels on escape without calling the daemon`.
  - `does not commit an unchanged name`.
- Updated: `test/dom/session-phase.test.ts`'s state-icon pair, which
  asserted `st.tileState.dataset.state` on a node that no longer exists.
  It now asserts the published phase; the icon assertion moved to
  `tile-chrome.test.tsx`.

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

- **2026-09-03** — The rename reuses `app/inline-rename.ts` imperatively
  from a React `onDoubleClick`, exactly as `components/Sidebar.tsx`
  already does, rather than becoming a declarative `renaming` flag in the
  store. Why: that module already owns focus restoration, the
  capture-phase key shield and the keyboard.ts handshake; a second,
  declarative copy of the dance would be the drift the module exists to
  prevent. `TileChromeState.renaming` was dropped from the slice.
- **2026-09-03** — `refreshStateIcon()` deleted rather than kept as a
  no-op. Why: both its inputs (`attention`, the tile's phase) are now
  store fields the component subscribes to, so `events.ts`'s two bell
  call sites had nothing left to do. Root cause, not a shim.
- **2026-09-03** — The tile's project LABEL renders from the store's
  project list; `setProject()` keeps only the `--project-color` custom
  property. Why: the store already holds the authoritative projects, so
  publishing the name again would be a second copy that can disagree. The
  CSS variable stays imperative because it is a style on the host element
  SessionTerm owns.
- **2026-09-03** — `notifyTerms()` un-exported after review. Why: the
  comment justifying the export described dom tests that do not exist —
  nothing outside `store/terms.ts` writes through `termsMap()`, all
  twenty call sites are reads. Export it when a direct mutator actually
  appears.
- **2026-09-03** — `IconButton` gained `onMouseDown`, `hidden` and
  `title`. Why: the tile header needs all three (mousedown shielding so
  minimize does not also select the tile; a worktree marker that is
  present but not always applicable; a tooltip that says more than the
  accessible name). Adding them to the primitive beat hand-rolling a
  second icon button, which components.md forbids.

## Progress

- **2026-09-03** — Scaffolded from the approved plan-first plan.
- **2026-09-03** — Gate NEEDS_FOLLOWUP, then PASS: `components.md`'s
  causal claim about why `src/ui/` survives was stale in a way this PR
  itself caused. Rewritten to name each file's real remaining caller
  (`session-term.ts`'s overlays for `icon.ts`, `banners.ts` for
  `icon-button.ts`) and to record that `ensureSprite()` moves rather than
  dies. Fixed on the branch, not filed as debt — the PR was open.
- **2026-09-03** — Implemented on `feature/329-tile-chrome-phase1`.
  Order: terms membership subscription → `tileChrome` slice → component →
  strip the imperative header → call sites → tests. Green at each step.
  Verification: `npm run typecheck`, `biome ci .` (12 warnings, 1 info,
  0 errors — the pre-existing `noUselessFragments` notice on
  `ProjectEditor.tsx` is a notice, not an error), `scripts/test.sh unit
  dom` (552 passed), `scripts/test.sh e2e` (260 passed, 31 skipped,
  specs unmodified), `npm run test:e2e:real` (22 passed, 2 skipped),
  `ui-lint.sh --strict` (0 violations), `ui-lint.sh --contrast`
  (0 failures), `go build ./...`.

## Gate verdict

- **2026-09-03** — verdict: NEEDS_FOLLOWUP; checks: 2 dimensions passed /
  0 failed / 1 followup; followups: none (PR open, so the fix landed in
  this PR rather than as tracked debt); one-line: doc accuracy caught a
  stale causal claim in `docs/design-docs/ui/components.md` — it still
  said `icon.ts` and `icon-button.ts` "survive only because
  session-term.ts builds the terminal tile imperatively", which this PR
  made false (`session-term.ts` no longer imports `iconButton` at all;
  `app/banners.ts` is its sole remaining caller).
  - 2026-09-03 dimensions:
    - acceptance — PASS — all 9 phase criteria and the master plan's
      invariants observed: the 8 imperative header fields are gone with
      no dangling references, `useTermIds`/`subscribeTerms` make
      membership reactive while `TileChromeState` holds plain data only,
      `git diff --stat main...HEAD -- test/e2e test/e2e-real` is empty,
      `TileChrome.tsx` has no effect hook and no `ensureAttached` call,
      the constructor's empty `.tile-header` is appended before any React
      pass, and `applyXtermTheme`/`applyFontSize` still iterate
      `allTerms()`. `tile-chrome.test.tsx` 11/11; `tsc --noEmit` clean.
    - non-goals — PASS — React owns no part of the xterm lifecycle (no
      `@xterm` or `webgl-budget` import in any `src/components/` file),
      `keyboard.ts` is a 0-line diff, no CSS Modules, both overlays are
      still `document.createElement`, `src/ui/` and
      `wireCheckUpdatesButton()` are intact, `.changesets/` is empty, and
      no Go outside `cmd/hivegui/frontend/` is touched.
    - doc accuracy — NEEDS_FOLLOWUP — the `components.md` sentence above.
      Everything else accurate: `Icon.tsx`'s and `terms.ts`'s header
      comments hold, `FRONTEND.md`/`DESIGN.md` are imprecise but not
      false (Phase 2 finishes them), the plan matches the diff, and the
      `no-changeset` framing is correct — the diff is a render-path swap
      behind an unchanged DOM contract.
- **2026-09-03** — verdict: PASS; checks: 3 dimensions passed / 0 failed
  / 0 followups; followups: none; one-line: the `components.md` claim was
  rewritten in this PR to name each primitive's real remaining caller, so
  the followup that produced the NEEDS_FOLLOWUP is closed on the branch
  rather than deferred.

## PR convergence ledger

Append-only, one line per `/hs-review-loop` iteration.

- **2026-09-03 iter 1** — verdict: COMMENT; mergeable: MERGEABLE;
  findings_hash: 96cd2e341ac5fe67020a6b47c3bb6ec85be279979ba1d2b83e47b26abfca1ac4;
  threads_open: 0; action: stop; head_sha: f9f5e8e. No BLOCKING. The one
  IMPORTANT (test isolation) and one MINOR (dead export) were applied in
  the same PR per AGENTS.md's boil-the-lake rule rather than deferred.

## Open questions

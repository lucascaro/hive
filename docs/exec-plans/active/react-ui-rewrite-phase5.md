# React UI rewrite — Phase 5: Grid shell

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **PR:** —
- **Branch:** `feature/react-phase5-grid-shell`
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Scope

React owns orchestration/subscription; DOM operations stay imperative in one layout effect, ported verbatim. Do NOT re-express `renderGrid` as JSX children — its operation order encodes bug fixes.

New files:
- `src/components/GridView.tsx` — subscribes to a derived *layout signature* (`view | activeId | gridProjectId | the ordered ids of the sessions the scope tiles`); renders nothing; `useLayoutEffect` → `applyGridLayout()`. Raw `sessions` and `attention` are deliberately NOT dependencies — see the master plan's Phase 5 brief for why each one would repaint at a rate today's code does not.
- `src/app/grid-layout.ts` — `applyGridLayout(snapshot)` / `applySingle(snapshot)`: current `renderGrid()`/`showSingle()` bodies extracted intact — grid template before attach, reparent-not-recreate, out-of-scope tiles keep their DOM node, inline `gridRow` spans, `rebaselineGridReplayCols()` double-rAF, `attachDeferred` idle staggering, the `#terms` ResizeObserver + rAF coalescing, `setView`'s 250ms bottom-snap. Hosts come from `src/store/terms.ts`.

Files to change: `src/app/view.ts` — render paths deleted; survivors (app-title timer, `focusActiveTerm` interop, nav helpers) move next to callers or into `grid-layout.ts`. `src/app/events.ts` — remaining `deps.renderGrid()` calls removed; `pty:*` handlers keep writing directly to SessionTerm instances via the registry (data plane bypasses React, as today). `src/app/session-term.ts` — imports only.

## Success criteria

What `/hs-merge-gate` validates for THIS phase.

- `GridView` renders no DOM of its own; all layout work happens in one
  `useLayoutEffect` calling `applyGridLayout()`.
- The grid template is set **before** any attach — pinned by a spy-sequence
  test, because reversing it brings back the double-scrollback-restream jump.
- Tiles are reparented, never recreated: the same DOM node identity survives a
  re-render, and an out-of-scope tile keeps its node.
- `rebaselineGridReplayCols()`'s double-rAF, `attachDeferred`'s idle staggering,
  the `#terms` ResizeObserver + rAF coalescing, `setView`'s 250ms bottom-snap and
  `focusActiveTerm`'s 8-frame retry are ported verbatim.
- `ensureAttached()` is called no more often than on today's paths.
- The `pty:*` data plane still writes straight to SessionTerm via the registry,
  bypassing React.
- The e2e suite passes 3× consecutively, and a `wails build` smoke covers
  attach, resize, view toggle, background-tile scroll, minimize/restore and
  theme switch.

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

## Known spec-edit exception (carried from Phase 0 review) — not needed

Phase 0's review flagged `test/e2e/nav-history.spec.ts:100`
(`window.__hive_state?.minimized.add(id)`, an in-place mutation of a store Set
the reference equality cannot see) as breaking in "the first phase that
subscribes to `minimized`" — Phase 2's `MinimizedTray`, and again in Phase 5's
`GridView`.

Phase 2 got there first and converted the line to an assignment
(`s.minimized = new Set([...s.minimized, id])`), which routes through
`setMinimized`. It is already correct for this phase's subscriber, so Phase 5
spends no spec edit at all: **zero e2e specs change in this PR.**

## Decision log

**2026-09-03 — `attention` is not a GridView dependency, though this plan's
Scope line listed it.** Verified against the code: a bell never called
`renderGrid()` — `events.ts:174/177/184/226` and `focus.ts:54` patch the
`attention` class straight onto the host. Subscribing would run a full layout
pass per bell, and every pass calls `ensureAttached()` on every in-grid tile,
which re-latches follow-bottom (invariant 2) — a background tile parked in
history would be dragged to the bottom at bell rate. `applyGridLayout()` still
sets the class during a pass, reading `attention` non-reactively, exactly as
`renderGrid()` did. Same reasoning retires raw `sessions` as a dependency: an
`updated` event replaces the array at the child program's redraw rate, and
today only a removal or a reorder repaints — both of which move the signature,
while a rename moves neither.

**2026-09-03 — store writes with post-layout work are wrapped in `flushSync`.**
`switchTo` and `setView` do their focus and bottom-snap work after the repaint;
React would otherwise land the effect in a microtask after they returned, so
`snapVisibleTermsToBottom` would measure a tile the grid had not laid out —
inverting invariants 3 and 4. `view.ts`'s `withLayout()` carries a depth guard
because these commands call each other (`switchTo` →
`fallBackToSingleIfActiveHidden` → `setView`), and React does not allow a
nested flush. Same pattern the six modals adopted in Phases 3–4.

**2026-09-03 — `isSessionHidden()` stays in `view.ts`.** It reads nothing but
the store, so it is not layout code; moving it next to `gridScopeFor()` would
have pulled `grid-layout.ts` (and its module-scope `#terms` ResizeObserver)
into `keyboard.ts`'s import graph, which four dom tests mock `view.js`
specifically to avoid.

**2026-09-03 — `GridView` mounts on a new empty `#grid-root`, not `#terms`.**
It renders `null`, and `#terms`'s children are SessionTerm hosts React must
never own. The element is `hidden` (so `display: none`, not an `#app` grid
item, and every region's explicit row placement is untouched) and Phase 6
removes it with the island array.

**2026-09-03 — accepted behaviour delta.** `switchTo(id)` where `id` is already
active in a grid view used to run a full `renderGrid()`, re-anchoring every
background tile to the bottom. It now repaints nothing — no signature change —
while the active tile still gets its explicit `snapVisibleTermsToBottom([st])`.
Strictly fewer `ensureAttached()` calls, which is the direction the success
criterion allows.

## Progress

**2026-09-03** — Implemented. New `src/app/grid-layout.ts` (`applyGridLayout`,
`applySingle`, `attachDeferred`, `_ric`, the layout cache +
`currentGridLayout()`/`spatialTarget()`, `gridScopeFor`, `gridScopeSessions`,
`rebaselineGridReplayCols`, the `#terms` ResizeObserver — all moved verbatim
bar the renames and the `state.terms` → `store/terms.ts` registry swap) and
`src/components/GridView.tsx`. `view.ts` keeps the commands and lost every
`renderGrid()`/`showSingle()` call; `events.ts` lost its `renderGrid` deps
field and both calls; `session-term.ts`'s mousedown is `setActive` alone;
`main.ts` mounts the island on the new `#grid-root`.

Tests: new `test/dom/grid-layout.test.tsx` (8 cases — template-before-attach as
a spy sequence, reparent-not-recreate by node identity, out-of-scope tile keeps
its node, plus GridView renders no DOM, repaints on a scope change, and does
NOT repaint on a bell or a rename). `grid-reorder-focus.test.ts` and
`minimize-project.test.tsx` repoint to the new entry points;
`events-focus.test.ts` drops the `renderGrid` dep. `view-floor`,
`xterm-reflow`, `attention-jump`, `attention-jump-integration` and dom
`nav-history` needed no change after all — they mock `view.js` or drive it
through commands that still exist.

Verification (all from a fresh worktree after `./scripts/ci-bootstrap.sh`):
`npm run typecheck` clean, `biome ci .` clean, `scripts/ui-lint.sh --strict` 0
violations, unit 403 passed, dom 539 passed (51 files), go all packages ok,
e2e 258 passed / 31 skipped **three consecutive runs**, `e2e:real` 22 passed.
No changeset: behaviour-preserving, `no-changeset` label per the master plan.

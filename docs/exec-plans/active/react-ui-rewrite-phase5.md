# React UI rewrite — Phase 5: Grid shell

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Scope

React owns orchestration/subscription; DOM operations stay imperative in one layout effect, ported verbatim. Do NOT re-express `renderGrid` as JSX children — its operation order encodes bug fixes.

New files:
- `src/components/GridView.tsx` — selector-subscribes to `sessions/view/activeId/gridProjectId/minimized/attention`; renders nothing; `useLayoutEffect` → `applyGridLayout()`.
- `src/app/grid-layout.ts` — `applyGridLayout(snapshot)` / `applySingle(snapshot)`: current `renderGrid()`/`showSingle()` bodies extracted intact — grid template before attach, reparent-not-recreate, out-of-scope tiles keep their DOM node, inline `gridRow` spans, `rebaselineGridReplayCols()` double-rAF, `attachDeferred` idle staggering, the `#terms` ResizeObserver + rAF coalescing, `setView`'s 250ms bottom-snap. Hosts come from `src/store/terms.ts`.

Files to change: `src/app/view.ts` — render paths deleted; survivors (app-title timer, `focusActiveTerm` interop, nav helpers) move next to callers or into `grid-layout.ts`. `src/app/events.ts` — remaining `deps.renderGrid()` calls removed; `pty:*` handlers keep writing directly to SessionTerm instances via the registry (data plane bypasses React, as today). `src/app/session-term.ts` — imports only.

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

## Known spec-edit exception (carried from Phase 0 review)

`test/e2e/nav-history.spec.ts:100` does
`window.__hive_state?.minimized.add(id)` — an **in-place** mutation of a store
Set, the one pattern the store's reference equality cannot see.

It is correct today and stays correct through Phase 1: the facade getter returns
the live Set, and with no component subscribed to `minimized` the following
render picks the change up. **It stops working in the first phase that
subscribes to `minimized`** — Phase 2's `MinimizedTray` selector, and again in
Phase 5's `GridView`.

Deliberately NOT fixed in Phase 0: the migration's safety proof is that the e2e
specs never change, and editing one to chase a latent issue would have spent
that proof on a non-issue. When the subscriber lands, this is the **one
sanctioned spec edit** — `window.__hive.store.minimizeSession(id)` (or the
equivalent action exposed on the test global) instead of the raw `.add`. It is
NOT a DOM-contract break, so the "a spec edit means the contract broke" rule
does not apply to this line. Note it in that phase's PR description as the
signed-off exception the master plan's Tests section requires.

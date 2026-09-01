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

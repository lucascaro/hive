---
issue: null
title: "Incremental React 19 rewrite of the hivegui frontend"
type: enhancement
complexity: L
priority: P2
pr: 324
shipped: 2026-09-03
stage: DONE
---

# Incremental React 19 rewrite of the hivegui frontend

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P2
- **Stage:** DONE
- **Exec plan:** [docs/exec-plans/completed/react-ui-rewrite.md](../exec-plans/completed/react-ui-rewrite.md)

## Problem

The hivegui frontend is ~13k lines of vanilla TS across a dozen modules, each
imperatively owning a fixed region of `index.html`. State is one exported
mutable object (`src/app/state.ts`) with no subscribe/notify: daemon event
handlers mutate it and then call render functions by hand. Every render is
therefore a whole-region rebuild — the sidebar does `innerHTML = ''` and
reconstructs every row, which eats dblclick pairs and forces two parallel patch
paths to exist alongside it just to avoid killing listeners. `renderGrid()` is a
hand-written keyed reconciler whose order of operations silently encodes several
shipped bug fixes, so nobody can safely touch it.

The cost is paid twice: performance (a title event from a busy PTY rebuilds the
whole sidebar) and maintainability (three rendering idioms, manual render
bookkeeping at every call site, and no way to add a component without picking
one of them).

## Desired behavior

One rendering paradigm. Regions re-render from selector-scoped subscriptions
instead of manual `renderX()` calls, reusable components replace the
props-in/element-out functions plus their companion `updateX()` patch twins, and
the terminal layer stays imperative behind a stable keyed boundary because its
timing constraints are load-bearing.

Nothing the user sees changes. Same pixels, same keybindings, same behavior,
same DOM contract.

## Success criteria

- The 30 Playwright e2e specs and the e2e-real suite pass **unmodified** at every
  phase. A spec edit means the DOM contract broke.
- Ids, `hv-*` BEM classes, data-attributes and the `.hidden` modal-visibility
  class are byte-identical to today's output.
- `window.__hive_state` / `window.__hive` keep their exact shapes.
- Each of the 7 phases is independently green on the full verification block and
  ships as its own PR, leaving the app shippable.
- All legacy render code and the `src/app/state.ts` compat layer are deleted by
  the final phase; `rg` finds no orphaned exports.
- `scripts/ui-lint.sh --strict` passes and covers `.tsx`.
- Terminals are never unmounted and remounted — reparent only, stable
  `key={sessionId}`, so the 8-slot process-wide WebGL budget is never churned.

## Non-goals

- Any visual redesign. Token CSS, themes and every `hv-*` class name stay as-is.
- React-ifying `SessionTerm` / xterm. Filed as debt in the final phase.
- CSS Modules or any styling migration. Filed as debt.
- New e2e specs, selector changes, or introducing `data-testid` (considered and
  rejected — the unmodified specs are the safety proof).
- Any behavior, keybinding or UX change.
- Go, Wails or wire-protocol changes; `bridge.ts` and the Vite specifier
  substitution stay untouched.
- SSR, routing, Suspense/concurrent features, React Compiler.

## Notes

Seven phases, each its own PR and its own detailed plan, mirroring the
`ui-design-system` master/phase plan layout (completed 2026-08-31, PR #312):

- Phase 0 — zustand store + React tooling, no rendering change
- Phase 1 — sidebar island
- Phase 2 — chrome island (status bar, banners, boot/empty state, tray, footer)
- Phase 3 — modals A (launcher, settings)
- Phase 4 — modals B + keyboard reads the store
- Phase 5 — grid shell (highest risk, smallest diff)
- Phase 6 — single root, legacy deletion, docs

Phase PRs are behaviour-preserving and carry the `no-changeset` label; the final
phase adds the one changeset for the whole rewrite.

The flake baseline that every phase compares its failures against lives at
`.plans/react-rewrite-flake-baseline.md` (gitignored scratch; a copy goes in each
PR description).

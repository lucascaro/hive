# React UI rewrite — Phase 6: Single root, legacy deletion, docs

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Scope

- `src/components/App.tsx` composes Sidebar + chrome + modals + GridView; `src/main.tsx` replaces `src/main.ts` (bootstrap order: theme → store hydrate from localStorage → wire daemon events → mount root → freeze heartbeat). The island-root array in `main.ts` is unmounted and removed. `src/app/state.ts` compat getter deleted here — the `window.__hive_state` exposure (Playwright API, permanent) moves into `src/store/store.ts` under the same env gates.
- `index.html` keeps: theme-stamp script, stylesheet links, static boot overlay, top-level region ids React renders into. Modal placeholder divs collapse into React-rendered nodes with the same ids.
- Delete remaining legacy: `src/app/view.ts`, `src/app/el.ts` (keep `mustEl` only if `grid-layout.ts` needs it), residual deps seams, unused `src/ui/` files. `rg` for dead exports — zero orphaned code.
- Docs per AGENTS.md: fill in `FRONTEND.md` (currently an empty template — stack, component conventions, state flow, testing patterns); update `DESIGN.md` frontend paragraph (structural change); update `docs/design-docs/ui/README.md` workflow section (primitives are now React components).
- `.changesets/react-ui-rewrite.md` (`type: changed`, `bump: patch`) noting the internal rewrite. Phase PRs 1–6 use the `no-changeset` label (behaviour-preserving).
- File debt specs via the feature pipeline: (a) SessionTerm React-ification; (b) CSS Modules migration — step 1 is the e2e testid/selector strategy; (c) `keyboard.ts` decomposition into per-scope keymap tables, order preserved.

#

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

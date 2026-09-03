# React UI rewrite — Phase 6: Single root, legacy deletion, docs

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **Branch:** `feature/react-phase6-single-root`
- **PR:** [#324](https://github.com/lucascaro/hive/pull/324)
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

> **Three lines of this Scope were written in Phase 0 and are stale.**
> `src/app/view.ts` and `src/app/el.ts` are both live and stay — see the
> **Phase 6 brief** in the [master plan](react-ui-rewrite.md#phase-briefs),
> which supersedes them and records what was deleted instead.

## Scope

- `src/components/App.tsx` composes Sidebar + chrome + modals + GridView; `src/main.tsx` replaces `src/main.ts` (bootstrap order: theme → store hydrate from localStorage → wire daemon events → mount root → freeze heartbeat). The island-root array in `main.ts` is unmounted and removed. `src/app/state.ts` compat getter deleted here — the `window.__hive_state` exposure (Playwright API, permanent) moves into `src/store/store.ts` under the same env gates.
- `index.html` keeps: theme-stamp script, stylesheet links, static boot overlay, top-level region ids React renders into. Modal placeholder divs collapse into React-rendered nodes with the same ids.
- Delete remaining legacy: `src/app/view.ts`, `src/app/el.ts` (keep `mustEl` only if `grid-layout.ts` needs it), residual deps seams, unused `src/ui/` files. `rg` for dead exports — zero orphaned code.
- Docs per AGENTS.md: fill in `FRONTEND.md` (currently an empty template — stack, component conventions, state flow, testing patterns); update `DESIGN.md` frontend paragraph (structural change); update `docs/design-docs/ui/README.md` workflow section (primitives are now React components).
- `.changesets/react-ui-rewrite.md` (`type: changed`, `bump: patch`) noting the internal rewrite. Phase PRs 1–6 use the `no-changeset` label (behaviour-preserving).
- File debt specs via the feature pipeline: (a) SessionTerm React-ification; (b) CSS Modules migration — step 1 is the e2e testid/selector strategy; (c) `keyboard.ts` decomposition into per-scope keymap tables, order preserved.

#

## Success criteria

What `/hs-merge-gate` validates for THIS phase.

- One React root; `src/main.tsx` replaces `src/main.ts`, with bootstrap order
  theme → hydrate store → wire daemon events → mount → freeze heartbeat.
- `src/app/state.ts`'s compat facade is deleted and the `window.__hive_state`
  exposure has moved into `src/store/store.ts` under the same env gates, with an
  unchanged shape.
- All legacy render code is gone — the stranded `src/ui/` primitives
  (`button.ts`, `field.ts`, `kbd.ts`) and their dom tests — and `rg` finds no
  orphaned exports. (`view.ts` is not legacy render code; the deps seams were
  audited and kept, both recorded in the master plan's Decision log.)
- The freeze heartbeat still reads real state every second; the theme-stamp
  script is still inline in `index.html` ahead of first paint.
- `FRONTEND.md` is filled in; `DESIGN.md` and `docs/design-docs/ui/README.md`
  are updated.
- A changeset is added for the whole rewrite.
- Debt specs are filed: SessionTerm React-ification, CSS Modules (step 1 = the
  e2e testid/selector strategy), and `keyboard.ts` decomposition.
- **The spec's own `## Success criteria` are the gate at this phase** — this is
  where the whole-migration checklist must pass.

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

## Progress

- **2026-09-03** — Implemented on `feature/react-phase6-single-root`. JIT brief
  written into the master plan first, per the execution model. Order: facade
  deletion → dead `src/ui/` primitives → single root → docs → changeset → debt
  specs. Green at each step; the single root's first run failed 12 e2e specs on
  duplicated ids (portals append where island roots cleared), fixed in
  `main.tsx`.

## Gate verdict

<Filled by `/hs-merge-gate`.>

## PR convergence ledger

<Append-only. Maintained by hand for this feature — see the master plan's
Gating convention.>

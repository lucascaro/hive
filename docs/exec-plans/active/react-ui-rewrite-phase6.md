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

Append-only. Maintained by hand for this feature — `/hs-review-loop` finds a
plan by an `<NNN>`-prefixed name, which this feature's plans do not have (see
the master plan's Gating convention).

- **2026-09-03 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash:
  `6d2bc40e…`; threads_open: 0; action: fixes applied (COMMENT with strict off
  and zero threads is a stop, but all 7 IMPORTANT and 5 MINOR findings were
  high-confidence and low-risk, so they were applied here per AGENTS.md's
  boil-the-lake rule rather than deferred); head_sha: 601763c.
  - **The load-bearing one:** `tsconfig.json` still listed the deleted
    `src/main.ts`. Nothing imports the entry point, so the rewritten
    composition root was outside the tsc program entirely and this plan's
    "typecheck clean" was vacuous for it. Fixed, with a comment on the include
    array; `tsc --noEmit` is clean with `main.tsx` genuinely in the program.
  - `App.tsx` had zero dom coverage behind a comment claiming otherwise. Added
    `test/dom/app-root.test.tsx`, which mounts against the **real**
    `index.html` (`?raw`) and asserts no id occurs twice — closing the finding
    that `main.tsx`'s hardcoded pre-paint clear list had nothing keeping it in
    sync with the document. Includes a negative control.
  - `App.tsx` portalled into `pageEl()` (a cast that yields `null`;
    `createPortal(node, null)` throws, and with one root that takes the whole
    tree down). Switched all 13 targets to `mustEl`.
  - Docs: `components.md` still documented the deleted `ui/{button,kbd}.ts`;
    `FRONTEND.md` claimed `src/lib` is DOM-free (seven modules are not); the
    Phase 6 brief still described the `#app`-owning design that was superseded
    during implementation. All three corrected.
  - Coverage the deleted `ui-field.test.ts` had been the last to assert:
    `groupPresets()` optgroup bucketing (2 cases in `settings.test.tsx`) and
    the project editor's field/label a11y contract (2 in
    `project-editor.test.tsx`).
  - **CI caught one of the fixes:** the new dom test stripped `index.html`'s
    `<script>` tags with a regex, which CodeQL flagged as two HIGH alerts
    (`js/bad-tag-filter`, `js/incomplete-multi-character-sanitization`) — the
    hand-rolled-HTML-sanitization shape, wrong for upper-case tags and for
    nesting. Replaced with `DOMParser` + `querySelectorAll('script').remove()`,
    which is exact and leaves no regex behind. Test-only code, but the rule is
    right about the pattern.

- **2026-09-03 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash:
  `acf3a0b6…`; threads_open: 0; action: fixes applied, then stop; head_sha:
  e86be94. The pass re-verified every iteration-1 fix (tsconfig include,
  `mustEl` portal targets, the new dom test's negative control and its
  `PRE_PAINT_SEEDED` drift check, the DOMParser replacement) and independently
  confirmed the `#grid-root` → `#react-root` rename is complete, no import of
  the deleted `src/ui/*` survives, and every remaining `app/state.js` import is
  type-only. Three findings left, all documentation:
  - the brief still said "fourteen islands" (fifteen shipped — `App.tsx`
    already said fifteen two files over);
  - `globals.d.ts` and `tsconfig.json` still credited `app/state.ts` with
    gating `__hive_state`, which this phase moved to `store/store.ts`;
  - the CSS-Modules spec inherited "30 Playwright specs" from the Phase 0
    plan; there are 31. The master plan's Summary now states the invariant
    (no phase edits a spec) rather than a count that drifts.


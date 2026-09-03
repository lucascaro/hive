# React UI rewrite — Phase 6: Single root, legacy deletion, docs

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **Branch:** `feature/react-phase6-single-root`
- **PR:** [#324](https://github.com/lucascaro/hive/pull/324)
- **Status:** completed

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

- **2026-09-03** — verdict: FAIL; checks: 2 dimensions passed / 1 failed / 0
  followups; followups: none (PR open, so the fix landed in this PR rather than
  as tracked debt); one-line: doc accuracy failed on `FRONTEND.md`'s `src/lib`
  row, which a review-loop fix had overcorrected from "DOM-free" to seven
  DOM-touching modules when only three are.
  - 2026-09-03 dimensions:
    - acceptance — NEEDS_FOLLOWUP — every spec and phase-plan criterion passed;
      the single followup was a timing artefact (the CI matrix was mid-run on
      `be762ad`, which the stage-advance commit had just restarted), not a
      defect. Independently re-verified both documented deviations: `view.ts` /
      `el.ts` are live and correctly kept, and exactly nine `init*` deps seams
      exist and were correctly kept.
    - non-goals — PASS — 7/7. No theme/token/`hv-*` change, no SessionTerm
      React-ification, no CSS Modules, no new e2e spec and zero `data-testid`
      repo-wide, no Go/Wails/wire change, no SSR/routing/Suspense/Compiler.
    - doc accuracy — FAIL — `FRONTEND.md:31` claimed seven DOM-touching
      `src/lib` modules tested under `test/dom`; `focus.ts`,
      `renderer-recovery.ts`, `scroll-debug.ts` and `freeze-heartbeat.ts` make
      no `document`/`window` call and run in the node-only `unit` project.

- **2026-09-03** — verdict: PASS; checks: 3 dimensions passed / 0 failed / 0
  followups; followups: none; one-line: FRONTEND.md's `src/lib` row corrected in
  `969afd6`, doc accuracy re-run clean, and the acceptance dimension's only
  followup closed by CI finishing 12/12 green on that head.
  - 2026-09-03 dimensions:
    - acceptance — PASS — all seven spec Success criteria and every phase-plan
      criterion verified with evidence: zero `.spec.ts` edits across
      `main...HEAD`; `hiveStateView` exposes the same 19 fields and
      `test_window_hive_state_shape_unchanged` passes; the `state` facade is
      gone and an orphaned-export sweep over `src/` found no dead code;
      `ui-lint.sh --strict` passes and globs `.tsx`; `#terms` is outside
      React's tree and `grid-layout.ts` still reparents; bootstrap order,
      inline theme stamp, docs, changeset and the three debt specs all present.
    - non-goals — PASS — unchanged from the first run.
    - doc accuracy — PASS — the three corrected claims re-verified by command
      (not re-read), plus a full regression sweep of `DESIGN.md`, the UI design
      docs, the changeset, the debt specs, the plan family and `README.md`.

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

- **2026-09-03 converged** — iteration 2's findings applied in `49aafaf`; all 12
  CI checks green, zero review threads, MERGEABLE. Loop stops here (COMMENT with
  strict off and no open threads). Spec advanced `IMPLEMENT` → `GATE`; the master
  plan's Gating convention holds the spec at `IMPLEMENT` across phases 1–6 and
  sends it to `DONE` only after this phase's gate, so `REVIEW` is skipped rather
  than back-filled.

- **2026-09-03 iter 3** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash:
  empty; threads_open: 0; action: stop; head_sha: be762ad. Iteration 2's three
  documentation findings applied in `49aafaf`; nothing outstanding. `COMMENT`
  with strict mode off and zero unresolved threads is the loop's stop condition
  (`/hs-review-loop` §2 step 5), so the loop ends here.

## Progress

- **2026-09-03 — Gate FAIL (first run).** Two of three dimensions passed
  (non-goals 7/7; acceptance passed every spec and phase-plan criterion, with
  one FOLLOWUP that was an artefact of timing — the CI matrix was mid-run on
  `be762ad`, which the stage-advance commit had just restarted). Doc accuracy
  FAILed on one claim, and it was a botched fix rather than an original error:
  iteration 1 of the review loop corrected `FRONTEND.md`'s "src/lib is DOM-free"
  claim by listing **seven** DOM-touching modules; only **three** are
  (`focus-trap.ts`, `preserve-focus.ts`, `drag-placeholder.ts`). The other four
  — `focus.ts`, `renderer-recovery.ts`, `scroll-debug.ts`, `freeze-heartbeat.ts`
  — make no `document`/`window` call and are tested in the node-only `unit`
  vitest project, so the row also misstated where they are tested. Cause: the
  grep behind the iteration-1 fix matched comments and type names
  (`HTMLElement`, `Element`) rather than real calls. Corrected here, together
  with two figures the same validator flagged: `FRONTEND.md`'s "~20 tsc errors"
  without the wailsjs bindings (36 today, now stated as "a few dozen"), and the
  two debt specs' line counts, which this PR's own edits had moved
  (session-term.ts 1705 → 1710, keyboard.ts 806 → 819; both now approximate).


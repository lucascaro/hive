# React UI rewrite — master plan

- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **Status:** active

## Summary

Migrate the hivegui frontend (`cmd/hivegui/frontend`, ~13k lines vanilla TS + xterm.js under Wails) to React 19 with a zustand store, region by region across 7 PRs, each independently green on the full test suite. Goals: performance (selector-scoped re-renders instead of whole-region rebuilds) and maintainability (one render paradigm, reusable components, no manual render bookkeeping). Terminals (`session-term.ts`) stay imperative behind a stable keyed boundary. The id/`hv-*`-class/data-attribute DOM contract is frozen so all 30 Playwright e2e specs pass unmodified throughout.

## Context

Original ask: "rewriting the UI in React. Goal is performance and maintainability. Proper reusable components, and good integration with styling. … cheaper agents should be able to implement it, every step should be safe, well written, proper, not break anything. Incremental update rather than a one shot rewrite, must capture all the ui."

Current architecture (verified 2026-08-31 against main @ 62db856):

- `index.html` is a static skeleton of fixed-id regions (`#sidebar`/`#projects`, `#terms`, `#status`, `#launcher`, `#settings`, `#worktrees`, `#project-editor`, `#command-palette`, `#help-overlay`, `#empty-state`, `#boot-state`, `#minimized-tray`), each imperatively owned by a separate module.
- State is one exported mutable object (`src/app/state.ts`) with **no subscribe/notify**; daemon event handlers in `src/app/events.ts` mutate it and call render functions by hand. `terms: Map<sessionId, SessionTerm>` holds imperative terminal objects.
- `renderGrid()` (`src/app/view.ts:255`) is a hand-written keyed reconciler: reparents terminal hosts, never recreates; its order of operations encodes shipped bug fixes. The sidebar (`src/app/sidebar.ts`) does full `innerHTML=''` rebuilds plus two parallel patch paths kept to avoid killing dblclick/listeners.
- `src/ui/*` primitives (button, icon, icon-button, kbd, chip, banner, session-row, project-card) are props-in/element-out functions with companion `updateX()` patch fns — they map ~1:1 to React components.
- `src/lib/*` is pure, fully unit-tested, and survives untouched.
- Styling: token design system (`src/theme/tokens.css`, `themes.css`, 12 component CSS files) + legacy `style.css` (1259 lines). `hv-*` BEM classes and data-attributes are the TS↔CSS contract; `scripts/ui-lint.sh --strict` (CI) bans raw hex/px font sizes/icon Unicode.
- Tests: `test/unit` (34 files, pure lib), `test/dom` (32 vitest jsdom files, class/data-attr-coupled but calling imperative constructors), `test/e2e` (30 Playwright specs vs Wails mock, selecting ids + `hv-*` classes — **zero data-testid in the repo**), `test/e2e:real` (real daemon via ws bridge).
- `src/bridge.ts` must stay a direct child of `src/` — `vite.config.js` literally substitutes its wailsjs import specifiers for the test harnesses.

Ambiguities resolved with the user via two structured question rounds; see Decisions.

## Non-goals

- No visual redesign — pixel-identical output; token CSS, themes, and all `hv-*` class names unchanged.
- No SessionTerm/xterm React-ification (explicit debt item, filed in Phase 6).
- No CSS Modules / styling migration (explicit debt item).
- No new e2e specs, no selector changes, no `data-testid` introduction (considered and rejected — see Decisions).
- No behaviour, keybinding, or UX changes of any kind.
- No Go/Wails/wire-protocol changes; `bridge.ts` and the Vite specifier substitution untouched.
- No SSR, routing, Suspense/concurrent features, or React Compiler adoption.

## Decisions

- **React 19** — best ecosystem/docs/cheap-agent familiarity; overhead irrelevant at this scale. Rejected: Preact+compat because compat gaps (scheduling/refs timing) sit exactly where the xterm timing landmines live; Solid because weaker agent familiarity.
- **SessionTerm stays imperative** behind a ref/keyed boundary; React-ification is a follow-up debt item. Rejected: React-owned xterm lifecycle because it re-fights every documented timing fix.
- **zustand v5** (vanilla store + React hook) — readable/writable from non-React code during coexistence; selector subscriptions give the perf win. Rejected: hand-rolled `useSyncExternalStore` store (more code, same result); Context+reducers (state trapped inside React breaks incremental migration and Playwright's `window.__hive_state`).
- **Keep token CSS + `hv-*` BEM + data-attr contract verbatim**; CSS Modules deferred as debt. Rejected: CSS Modules now / Tailwind because both break every e2e selector and the ui-lint conventions for no migration benefit.
- **Region islands, merged into one root at the end.** Rejected: single root from day one (risky first step); per-component mounts inside legacy containers (listener-boundary mess).
- **React Testing Library rewrites per phase**, keeping the same class/data-attr assertions. Rejected: render-to-container shim (act() warnings, tests stay imperative-shaped).
- **Keep the single capture-phase window keydown handler** (`src/app/keyboard.ts`); its modal-precedence checks read the store instead of querying DOM classes. Precedence order copied verbatim. Rejected: per-component key handling (re-derives precedence, regression risk).
- **Full absorption, one PR per phase (7 PRs)**, each green on the full suite. Rejected: stopping at coexistence (two paradigms forever); ~15 per-component PRs (CI/review overhead).
- **No testids-first pre-phase.** The unmodified e2e specs are the safety proof that the DOM contract (which is also the CSS contract) is preserved; current selectors (`[data-sid]`, `[data-action]`, `[data-state]`, ids) are already semantic hooks. Testids land as step 1 of the future CSS Modules debt item instead. Accepted instead: a **flake baseline** in Phase 0 (see below).

## Approach

Strangler migration by region. Phase 0 makes state observable (zustand) with zero rendering change; each subsequent phase mounts one React root on an existing region, deletes that region's legacy renderer *in the same PR* (never both live — double-render risk), and rewrites that region's dom tests to RTL. Phase 6 collapses the islands into a single root and deletes all legacy render code. Reused as-is: all of `src/lib/*` (grid math, reorder, shortcuts, worktrees, update-state, keymap, platform, focus helpers), `src/theme/*`, `src/app/session-term.ts`, `src/bridge.ts`, `src/app/modals/focus-trap.ts` helpers.

### Execution model (per phase)

The executing session runs on Opus and orchestrates; it does not hand-write mechanical work. Per phase:

1. **JIT phase brief (Opus, do first):** read the then-current code and expand this plan's phase section into a concrete brief appended under `## Phase briefs` in this file — Phase 0: full action inventory (name, signature, fields touched, localStorage key) + per-file list of mutation sites (grep `state.` writes); Phases 3/4: exact markup contract per modal (element tree, ids, classes, data-attrs) extracted from the legacy module before porting; Phase 5: verbatim-move list (function → destination file, lines that change, everything else untouched).
2. **Delegate implementation to cheaper subagents when efficient:** mechanical, well-specified chunks → Sonnet (component ports from a markup contract, RTL test rewrites, call-site conversions from the action inventory); trivial edits (config, dep pins, glob additions, renames) → Haiku. One bounded task per subagent with the brief + the Invariants section pasted in; never an open-ended "migrate the region" prompt.
3. **Keep on Opus (do not delegate):** the Phase 0 store design, `grid-layout.ts` extraction and everything timing-related in Phase 5, keyboard precedence changes in Phase 4, and all review/merge decisions.
4. **Gate:** Opus runs the full Verification block itself and compares against the flake baseline before opening each PR.

### Invariants (every phase — violating any reintroduces a shipped bug)

1. **Selector contract frozen.** React output byte-compatible on ids, `hv-*` classes, data-attrs, and the `.hidden` modal-visibility class. A Playwright spec edit = the contract broke = fix the component, not the spec.
2. **`ensureAttached()` is not effect-idempotent** (`session-term.ts:1352` — re-latches follow-bottom every call). Effects must not call it more often than today's paths.
3. **Grid template before attach** (`view.ts:276`) or the double-scrollback-restream jump returns. `rebaselineGridReplayCols()` double-rAF (`view.ts:601`) ported verbatim.
4. **Focus timing:** `focusActiveTerm` 8-frame retry (`src/app/focus.ts:233`); `setView`'s hard 250ms bottom-snap delay (`view.ts:750`). Keep both.
5. **WebGL budget:** 8 process-wide slots (`src/lib/webgl-budget.ts`), acquired in SessionTerm constructor, released in `destroy()`. Never unmount/remount a mounted terminal — stable `key={sessionId}`, reparent only.
6. **Keyboard handler stays capture-phase** (must beat inline-rename's `stopPropagation`).
7. **`window.__hive_state` / `window.__hive` shapes unchanged** (Playwright API), backed by `store.getState()` + terms registry.
8. **Freeze heartbeat** (`main.ts:366`) keeps reading real state (terms count, view) every second.
9. **Theme-stamp inline script stays in `index.html`** (pre-first-paint), outside React.
10. `scripts/ui-lint.sh --strict` applies to `.tsx` — extend its globs in Phase 0.
11. AGENTS.md UX rules hold: key hints at point of use, destructive actions via confirm overlay showing confirm/cancel keys, one channel per fact, no colour-only state.

All paths below relative to `cmd/hivegui/frontend/` unless rooted.


## Phases

Each phase is its own PR and its own detailed plan. A phase's plan is written
just-in-time, against the then-current code, immediately before that phase is
implemented — the briefs deliberately do not all exist up front.

| Phase | Plan | PR | State |
|---|---|---|---|
| 0 — store + tooling | [phase0](react-ui-rewrite-phase0.md) | — | implemented, in review |
| 1 — sidebar island | [phase1](react-ui-rewrite-phase1.md) | — | not started |
| 2 — chrome island | [phase2](react-ui-rewrite-phase2.md) | — | not started |
| 3 — modals A | [phase3](react-ui-rewrite-phase3.md) | — | not started |
| 4 — modals B + keyboard | [phase4](react-ui-rewrite-phase4.md) | — | not started |
| 5 — grid shell | [phase5](react-ui-rewrite-phase5.md) | — | not started |
| 6 — single root + deletion | [phase6](react-ui-rewrite-phase6.md) | — | not started |

## Tests

RTL = `@testing-library/react`. All rewritten tests keep asserting the same classes/data-attrs as today. Playwright e2e + e2e-real: **zero spec changes across the whole migration** (any exception needs explicit sign-off in the PR description).

- Phase 0 — `test/unit/store.test.ts` :: `test_setSessions_replaces_reference_and_notifies`, `test_toggleCollapsed_persists_to_localStorage`, `test_markAttention_immutable_set_update`, `test_minimizeProject_and_restore_roundtrip`, `test_window_hive_state_shape_unchanged` (asserts every field Playwright reads exists).
- Phase 1 — RTL rewrites: `test/dom/ui-session-row.test.tsx`, `ui-project-card.test.tsx`, `ui-chip.test.tsx`, `sidebar-title.test.tsx`, `minimize-project.test.tsx`, `attention-icon.test.tsx`, `selectors.test.tsx`. New: `test/dom/sidebar-dblclick-rename.test.tsx` (row node identity survives re-render between the two clicks), `test/dom/sidebar-reorder.test.tsx`.
- Phase 2 — RTL rewrites: `ui-banner.test.tsx`, `ui-button.test.tsx`, `ui-icon.test.tsx`, `ui-icon-button.test.tsx`, `ui-state-icon.test.tsx` (imports from `src/ui/icon.js`, deleted this phase), `update-banner.test.tsx`, `boot-state.test.tsx`, `restart-hive.test.tsx` (imports `restartHive/isDaemonRestarting/initBanners` from `src/app/banners.js`, gutted this phase). New: `test/dom/status-bar.test.tsx` (setStatus/flash/modeHint render + clear).
- Phase 3 — RTL rewrites: `launcher.test.tsx`, `settings.test.tsx`, `settings-updates.test.tsx`. New: `test/dom/modal-shell.test.tsx` (hidden-class contract, trap acquire/release, Esc close, key hints visible).
- Phase 4 — RTL rewrites: `worktrees.test.tsx`, `focus-trap.test.tsx`, `keyboard-arrows.test.tsx`. New: `test/dom/choice-dialog.test.tsx` (open → answer → auto-cleanup, keyboard never stranded), `test/dom/keyboard-precedence.test.tsx` (table-driven over all 9 layers, store-backed ladder matches legacy order).
- Phase 5 — New: `test/dom/grid-layout.test.tsx` (spy-sequence asserts template-set-before-attach; reparent not recreate — same node identity across renders; out-of-scope tile keeps its DOM node). Update to the new entry points: `view-floor.test.ts`, `xterm-reflow.test.ts`, and the three that import `initView` from `src/app/view.js` (deleted this phase) — `attention-jump.test.ts`, `attention-jump-integration.test.ts`, `test/dom/nav-history.test.ts`.
- Phase 6 — New: `test/dom/app-shell.test.tsx` (single root mounts all regions, ids present). Sweep: no test imports a deleted module.

## Verification

Every phase PR, from the repo root (fresh worktree: `./scripts/ci-bootstrap.sh` first for wailsjs bindings):

```bash
cd cmd/hivegui/frontend && npm run typecheck && npm run ci && cd ../../..
scripts/ui-lint.sh --strict
scripts/test.sh unit dom e2e go
cd cmd/hivegui/frontend && npm run test:e2e:real
```

Compare any failures against `.plans/react-rewrite-flake-baseline.md` (Phase 0 artifact). Phases 5–6 additionally: run the e2e suite 3× (timing flake), and a manual smoke via `wails build` (never `-s`): attach, resize, view toggle, scroll up in a background tile, minimize/restore, theme switch.

## Review log

- **2026-08-31** — 3-way review (grounding/gaps/YAGNI) against main @ 62db856. Grounding: all paths, symbols, line claims, and verification commands check out; fixed the Worktrees.tsx source-file conflation (`src/app/modals/worktrees.ts` is the 581-line port source, `src/lib/worktrees.ts` is the reused 278-line lib); added missing `attentionReturnId` to the Phase 0 field list. Gaps: added `session-term.ts` (`updateSidebarSelection` caller at :617) to Phase 1's change list; added orphaned tests `restart-hive` + `ui-state-icon` to Phase 2 and `attention-jump`, `attention-jump-integration`, dom `nav-history` to Phase 5; added `hive.sidebarWidth` to Phase 0's persistence migration. YAGNI: dropped `src/store/hooks.ts` (hook exported from store.ts) and `src/roots.ts` (plain `createRoot` calls + local array in main.ts); inlined `useInlineRename`/`useDragReorder` into Sidebar.tsx until a second consumer exists; Phase 2 store no longer duplicates `src/lib/status.ts` flash timing, `empty-state.ts` model, or `minimized.ts` tray filtering (all derived via selectors/lib calls); `@testing-library/user-event` now conditional on fireEvent proving insufficient; anchored `state.ts` compat-layer deletion to Phase 6 with `window.__hive_state` moving to store.ts.

## Progress

**2026-09-01** — Migrated from `~/.hivesmith/plans/2026-08-31-react-ui-rewrite.md`
into hivesmith bookkeeping, following the `ui-design-system` master/phase layout.
Phase 0 implemented and committed (`dc249dc`); see
[phase0](react-ui-rewrite-phase0.md) for its brief, flake baseline and decision log.

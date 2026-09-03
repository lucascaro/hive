# React UI rewrite — Phase 0: foundations (zustand store + React tooling)

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **PR:** #311
- **Branch:** `react-phase0-store`
- **Status:** completed

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Scope

**Flake baseline first, before any code change:** run the full Verification block 3× on a clean checkout; record per-run failures (suite, spec file, error one-liner) in `.plans/react-rewrite-flake-baseline.md` in the repo (gitignored scratch — commit a copy into the PR description). Known pre-existing flake (e.g. the e2e-real Linux failures tracked on main) goes in the ledger. Every later phase compares its failures against it: in the ledger = pre-existing; not in it = your phase broke it, fix before merge.

New files:
- `src/store/store.ts` — `createStore` (zustand/vanilla) holding the data state from `AppState`: `projects`, `sessions`, `collapsed`, `minimizedProjects`, `minimized`, `attention`, `dismissedDead`, `attentionRestored*`, `attentionReturnId`, `aliveById`, `phaseById`, `activeId`, `currentProjectId`, `view`, `gridProjectId`, `fontSize`, `nav`. Every mutation = named action doing an immutable replace of the touched slice (new Set/Map/array — zustand equality is reference-based). localStorage persistence (view/collapsed/minimizedProjects/fontSize, plus `hive.sidebarWidth` from `main.ts:295-347`) moves into the owning actions.
- `src/store/terms.ts` — non-reactive `Map<string, SessionTerm>` registry (`getTerm/setTerm/deleteTerm/termCount/allTerms`). SessionTerm instances never enter reactive state.
- `useAppStore` (zustand `useStore` bound to the vanilla store) is exported from `store.ts` — no separate hooks file.

Files to change:
- `package.json` — add `react`, `react-dom`, `zustand`; devDeps `@vitejs/plugin-react`, `@types/react`, `@types/react-dom`, `@testing-library/react`, `@testing-library/jest-dom`; bump `jsdom` (currently `^25.0.0`) as needed for React 19 + RTL. Pin exact versions. Add `@testing-library/user-event` only if `fireEvent` proves insufficient (likely candidates: dblclick-rename, drag-reorder — Phase 1 brief decides).
- `vite.config.js` — add `@vitejs/plugin-react`; keep the bridge.ts literal specifier substitution intact.
- `tsconfig.json` — `"jsx": "react-jsx"`.
- `src/app/state.ts` — thin compat layer: `state` getter delegating to `store.getState()` + terms registry (read paths keep compiling), `window.__hive_state` exposure under the same env gates with identical shape.
- All mutating call sites switch to actions: `src/app/events.ts`, `view.ts`, `sidebar.ts`, `keyboard.ts`, `undo-close.ts`, `session-term.ts`, `modals/*.ts`, `main.ts`, `banners.ts`. Legacy manual `renderX()` calls stay exactly where they are this phase.
- `scripts/ui-lint.sh` (repo root) — include `*.tsx` in scanned globs.
- `test/dom/*` — mechanical updates only where tests mutated `state` directly.

## Success criteria

What `/hs-merge-gate` validates for THIS phase.

- `src/store/store.ts` and `src/store/terms.ts` exist; every data field of the old
  `AppState` lives in the store, and `SessionTerm` instances live only in the
  non-reactive registry.
- No `src/` file mutates a store-owned collection in place or assigns
  `state.x` / `state.x[i]` directly — every write goes through a named action.
- An action that changes nothing does not notify subscribers.
- The `state` facade preserves the exact `window.__hive_state` field list and
  types, pinned by a test that asserts against the facade (not the store).
- No rendering change: every legacy `renderX()` call site is where it was.
- `scripts/ui-lint.sh --strict` and the vitest globs cover `.tsx`.

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Brief

Written 2026-08-31 against `760bce4`, before implementation, per the master
plan's Execution model.

> **Amended during implementation.** The table below is the brief as written
> before the work; two rows changed in flight and are corrected here —
> `setProjects` split into `setProjects` + `applyProjectList`, and `setNav` was
> added to back the facade's writable `nav`. See the Decision log for why.

**Store shape** (`src/store/store.ts`, zustand/vanilla `createStore`). Data fields exactly as `AppState` minus `terms`:
`projects, sessions, collapsed, minimizedProjects, attention, attentionReturnId, attentionRestored, attentionRestoredProjects, nav, minimized, aliveById, phaseById, dismissedDead, activeId, currentProjectId, view, gridProjectId, fontSize`, plus `sidebarWidth: number`.

`nav` is the exception: `NavHistory` is mutated in place by `src/lib/nav-history.ts` (`pushNav`/`pruneNav`) and nothing renders from it. It stays a stable object reference in the store, mutated in place exactly as today. Documented in the file, not a reactive slice.

`terms` leaves `AppState` entirely and lives in `src/store/terms.ts` as a plain non-reactive `Map` (`getTerm/setTerm/deleteTerm/termCount/allTerms/clearTerms`). SessionTerm instances never enter reactive state.

**Action inventory.** Every action does an immutable replace of the slices it touches (new Set/Map/array). Persistence lives in the owning action.

| Action | Signature | Fields written | localStorage | Legacy call sites replaced |
|---|---|---|---|---|
| `setProjects` | `(list: ProjectInfo[]) => void` | `projects` only — never prunes | — | the `state.projects` facade setter, dom tests |
| `applyProjectList` | `(list: ProjectInfo[]) => void` | `projects`, `currentProjectId` (first if unset), `collapsed`+`minimizedProjects` (pruned to live ids) | `hive.collapsedProjects`, `hive.minimizedProjects` (only if pruning changed them) | `events.ts:256-274` (`project:list`) |
| `addProject` | `(p: ProjectInfo) => void` | `projects` (append-if-absent, re-sorted by `order`), `currentProjectId` if unset | — | `events.ts:298-300` |
| `updateProject` | `(p: ProjectInfo) => void` | `projects` (replace by id, re-sorted) | — | `events.ts:306` + `:322` |
| `removeProject` | `(id: string) => void` | `projects`, `collapsed`, `minimizedProjects`, `currentProjectId` (→ first remaining when it was this one) | both collapse keys on change | `events.ts:302-309` |
| `setCurrentProjectId` | `(id: string \| null) => void` | `currentProjectId` | — | `view.ts:168,444`, `focus.ts:52` |
| `setSessions` | `(list: SessionInfo[]) => void` | `sessions` | — | `events.ts:381` |
| `pruneToLiveSessions` | `() => void` | `minimized`, `aliveById`, `phaseById` (drop ids absent from `sessions`) | — | `events.ts:392-400` |
| `addSession` | `(s: SessionInfo) => void` | `sessions` (append-if-absent) | — | `events.ts:430` |
| `updateSession` | `(s: SessionInfo) => void` | `sessions` (replace by id) | — | `events.ts` `title` + `updated` paths |
| `removeSession` | `(id: string) => void` | `sessions` | — | `events.ts:466` |
| `setAlive` | `(id: string, alive: boolean) => void` | `aliveById` | — | `events.ts:335` |
| `setSessionPhase` | `(id: string, phase: string) => void` | `phaseById` | — | `events.ts:336` |
| `forgetSession` | `(id: string) => void` | `aliveById`, `phaseById`, `dismissedDead`, `minimized` (delete id from each) | — | `events.ts:460-463` (one notify, not four) |
| `addAttention` | `(id: string) => void` | `attention` | — | `events.ts:162,215` |
| `clearAttentionFor` | `(id: string) => boolean` | `attention`; returns whether it was present | — | `events.ts:171`, `focus.ts:48` |
| `setAttention` | `(ids: Set<string>) => void` | `attention` | — | tests |
| `setAttentionReturnId` | `(id: string \| null) => void` | `attentionReturnId` | — | `keyboard.ts:500,539` |
| `addAttentionRestored` | `(id: string) => void` | `attentionRestored` | — | `keyboard.ts:506` |
| `addAttentionRestoredProject` | `(pid: string) => void` | `attentionRestoredProjects` | — | `keyboard.ts:509` |
| `clearAttentionRestored` | `() => void` | both restored sets | — | `keyboard.ts:559-560` |
| `addDismissedDead` | `(id: string) => void` | `dismissedDead` | — | `session-term.ts:1627` |
| `clearDismissedDead` | `(id: string) => void` | `dismissedDead` | — | `events.ts:203,353` |
| `toggleCollapsed` | `(pid: string) => void` | `collapsed` | `hive.collapsedProjects` | `sidebar.ts:317-318` |
| `minimizeProject` | `(pid: string) => void` | `minimizedProjects` | `hive.minimizedProjects` | `view.ts:582` |
| `restoreProject` | `(pid: string) => void` | `minimizedProjects` | `hive.minimizedProjects` | `view.ts:604` |
| `minimizeSession` | `(id: string) => void` | `minimized` | — | `view.ts:527` |
| `restoreSession` | `(id: string) => void` | `minimized` | — | `view.ts:559` |
| `setActiveId` | `(id: string \| null) => void` | `activeId` | — | `focus.ts:54`, `view.ts:177,463`, `events.ts:476` |
| `setView` | `(v: ViewMode, persist = true) => void` | `view` | `hive.view` when `persist` | `view.ts:750-755` |
| `setGridProjectId` | `(pid: string \| null) => void` | `gridProjectId` | — | `view.ts:99,169,445,757` |
| `setFontSize` | `(n: number) => void` | `fontSize` (already clamped by caller) | `hive.fontSize` | `session-term.ts:1651,1660`; the `localStorage.setItem` at `:1645` leaves `applyFontSize()` |
| `setSidebarWidth` | `(w: number) => void` | `sidebarWidth` (clamped 220–480) | `hive.sidebarWidth` | `main.ts:312,347` |

Naming collision note: `minimizeSession/restoreSession/minimizeProject/restoreProject` also exist as orchestrating functions in `view.ts` (they repaint, hand off focus, enforce the view floor). The store actions are the state half only; `view.ts` keeps its functions and calls the store action where it used to mutate. Import the actions under a `store.` namespace at those sites to keep the two apart.

**Compat layer** (`src/app/state.ts`): `export const state` becomes an object with getters delegating to `store.getState()` for every data field and to the terms registry for `terms`. Read paths (~200 sites) keep compiling untouched. `AppState`, `SessionInfo`, `ProjectInfo`, `TermTile` type exports stay here — they are imported all over and moving them is churn with no benefit. `saveCollapsed`/`saveMinimizedProjects`/`loadSaved*` stay exported (tests import them) but delegate to the store's persistence. `window.__hive_state` exposure stays in `state.ts` this phase (moves to `store.ts` in Phase 6) and keeps the identical shape — the compat object satisfies it.

**Per-file mutation sites to convert** (writes only; reads untouched):
- `src/app/events.ts` — :159,162,171,203,215,256,258,268,273,298,300,302,303,304,308,322,335,336,353,381,393,396,399,430,460,461,462,463,466,470,476
- `src/app/view.ts` — :99,168,169,177,444,445,463,527,559,582,604,750,757
- `src/app/focus.ts` — :48,52,54
- `src/app/keyboard.ts` — :500,506,509,539,559,560
- `src/app/sidebar.ts` — :317,318
- `src/app/session-term.ts` — :1627,1645(persist moves),1651,1660,1669(terms registry)
- `src/main.ts` — :312,347 (sidebar width)
- `test/dom/*` — the files that assign `state.x` directly in setup: `attention-icon`, `attention-jump`, `attention-jump-integration`, `grid-reorder-focus`, `keyboard-arrows`, `launcher`, `minimize-project`, `nav-history`, `selectors`, `session-phase`, `sidebar-focus`, `sidebar-title`, `undo-close`, `view-floor`, `events-focus`. Mechanical: `state.x = v` → the matching action / a `resetStore(partial)` test helper.
- `test/e2e/wails-mock.ts`'s `state` is the mock daemon's own state — unrelated, do not touch.

Deliberately unchanged this phase: every `renderX()` call stays exactly where it is; no component, no React root, no rendering change.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

### Flake baseline (Phase 0 artifact)

3 runs of the full block on a clean `760bce4`. **Zero frontend failures in 9
suite-runs.** `go test ./...` failed all 3 runs and was diagnosed, not guessed:
the agent shell runs under **Rosetta 2** (`sysctl.proc_translated=1`, `arch`=i386,
an x86_64 `go` binary, no native arm64 toolchain), and the PTY/goroutine-heavy
packages deadlock at 0% CPU inside the Rosetta runtime. The failing package set
moves between runs, so no individual test is at fault; CI is green on this commit
on native runners. Owner decision: **frontend suites gate locally, Go covered by
CI.** Full ledger: `.plans/react-rewrite-flake-baseline.md`.

### Result — all gates match the baseline

| Gate | Baseline | Phase 0 |
|---|---|---|
| `npm run typecheck` | 0 errors | 0 errors |
| `biome ci .` | 10 warnings, exit 0 | 10 warnings, exit 0 |
| `scripts/ui-lint.sh --strict` | 0 violations | 0 violations |
| vitest unit + dom | 720 tests | 737 (+17 store tests) |
| Playwright e2e | 218 passed / 9 skipped | 218 passed / 9 skipped, zero spec edits |
| Playwright e2e:real | 24 passed | 24 passed |

## Decision log

**`nav` is not a reactive slice.** `lib/nav-history.ts` mutates `NavHistory` in
place and nothing renders from it; copy-on-write would mean rewriting a tested
pure module for no gain. It stays a stable reference in the store, documented in
both files.

**The compat facade got setters, not just getters.** Not in the plan. It means
the dom test files needed edits only where they mutate a collection *in place* —
the case reference-equality cannot see — so 8 test files changed instead of 15.

**`setProjects` and `applyProjectList` are separate.** Found by regression: the
first implementation folded the `project:list` pruning into `setProjects`, so the
facade's `state.projects = […]` setter wiped the persisted collapse/minimize sets
wherever a render path or test assigns a project list before the daemon has
spoken. Only the event may prune. A test pins the split.

**jsdom stays on `^25.0.0`.** The plan anticipated a bump for React 19 + RTL;
bumping to 30 broke all four `worktrees.test.ts` inline-rename tests, because
jsdom 30 changes focus/blur timing and `beginInlineRename` cancels itself before
its input mounts. RTL 16 + React 19 do not need the bump.

**Tooling widened to `.tsx` now, not in Phase 1.** vitest `include` globs and
`scripts/ui-lint.sh`, plus `src/components` pre-added to the ui-lint glyph
targets — so a Phase 1 `.tsx` suite cannot vanish silently, which is the trap
`vitest.config.js`'s own comment warns about.

**`@vitejs/plugin-react` is deliberately NOT installed.** It was added first,
then removed: its dev-only Fast Refresh preamble is injected as the *first*
inline `<script>` in `<head>`, which displaces the theme-stamp script that
`test/e2e/theme.spec.ts` reads via `document.head.querySelector('script:not([src])')`
— breaking a spec on all three platforms without touching a line of app code.
Vite compiles `.tsx` natively through esbuild using `tsconfig`'s
`"jsx": "react-jsx"`, so the plugin buys only HMR. Verified before removal with
a throwaway `.tsx` component + RTL test: both compile and render with no plugin.
Phase 1 gets Fast Refresh only if someone first makes the theme-stamp script
addressable by id, which is a spec change and needs its own sign-off.

**No subagent delegation.** The master plan's Execution model allows it "when
efficient"; the orchestrating session already held the full read of every
mutating file, so a handoff would have paid to re-read what was already in
context. Phases 1–4 are the natural delegation points.

## Progress

**2026-09-01** — Implemented and committed (`dc249dc`). Flake baseline recorded;
store + registry + compat facade landed; all `src/` mutation sites converted to
actions; 8 dom test files converted off in-place mutation; 17 store tests added.
All gates green and matching the baseline.

## PR convergence ledger

Append-only, one line per `/hs-review-loop` iteration.
- **2026-09-01 iter 2** — verdict: pending re-review; mergeable: MERGEABLE; findings_hash: (iter-1 findings addressed by hand); threads_open: 0; action: fixes pushed (no-op-action silencing, `__hive_state` shape guard on the facade, index-assignment sweep, persistence-load + subscribe coverage, `setNav`); head_sha: c4c132f.
- **2026-09-01 iter 3** — verdict: pending re-review; mergeable: MERGEABLE; findings_hash: n/a; threads_open: 0; action: CI-failure fix + merge main (`@vitejs/plugin-react` removed: its dev Fast Refresh preamble displaced the theme-stamp script that #310's new `theme.spec.ts` reads); head_sha: deded18.
- **2026-09-01 iter 4** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: (empty — 0 BLOCKING, 0 IMPORTANT); threads_open: 0; action: stop (converged). The one MINOR (`byOrder` allocating unconditionally, defeating the no-op-no-notify contract) was fixed rather than carried; head_sha: cd66176.

## Gate verdict

- **2026-09-01** — verdict: NEEDS_FOLLOWUP; checks: 6 passed / 0 failed / 1 followup; followups: none filed (deferred by design, tracked by the master plan's phase table); one-line: Phase 0 satisfies every success criterion that is checkable at this phase; the legacy-deletion criterion cannot close until Phase 6.
  - 2026-09-01 dimensions:
    - acceptance — PASS (1 criterion deferred) — e2e specs unmodified and green (230 passed / 9 skipped, `CI=1`); no `.tsx`/`.css`/`.html` touched, so ids/`hv-*`/data-attrs/`.hidden` are byte-identical; `window.__hive_state` field list preserved 1:1 by the facade and pinned by `test_window_hive_state_shape_unchanged`; tsc clean, 773 vitest tests, ui-lint 0 violations; SessionTerm instances kept out of reactive state in `store/terms.ts`. Deferred: "all legacy render code and the `state.ts` compat layer deleted, no orphaned exports" — Phase 6 by design; `state.ts` is *intended* to be the compat facade during Phase 0.
    - non-goals — PASS — net diff touches no `*.css`, `src/theme/**`, `index.html`, `*.go`, `go.mod/go.sum`, or `src/bridge.ts`; `vite.config.js` byte-identical to main (the wails specifier substitution is untouched, still first with `enforce: 'pre'`); no `data-testid` anywhere; no e2e spec added or modified; `session-term.ts` carries only call-site rerouting, no React.
    - doc accuracy — PASS — every action in the brief's inventory table exists in `store.ts` with the stated signature, fields and localStorage key; every Decision log claim verified against source (`jsdom ^25.0.0`, no `@vitejs/plugin-react` in either `package.json` or `vite.config.js`, the `setProjects`/`applyProjectList` split, `setNav`, facade setters); store header comments match the implementation after the review-time no-op fix; all relative links between spec and phase plans resolve; `no-changeset` label matches AGENTS.md's carve-out for internal refactors. `FRONTEND.md` left as the unfilled scaffold, which the master plan explicitly defers to Phase 6.

**Note on the net diff.** `@vitejs/plugin-react` was added in `dc249dc` and removed again in `deded18`, so the PR's *net* change to `vite.config.js` is nil and the plugin never appears in `package.json`'s final state. The Decision log entry explains why it must not come back without first making the theme-stamp script addressable by id.

- **2026-09-01 (re-gate)** — verdict: PASS; checks: 6 passed / 0 failed / 0 followups; followups: none; one-line: re-gated against **this phase's** `## Success criteria` after adopting per-phase gating; the whole-migration criterion that produced the earlier NEEDS_FOLLOWUP now belongs to Phase 6, where the spec's criteria are the gate.
  - 2026-09-01 dimensions: unchanged from the entry above (acceptance / non-goals / doc accuracy all PASS); only the checklist the acceptance dimension is measured against changed, from the spec's finished-migration criteria to this phase's own.

## Post-gate: merge of main @ `afc430c` (design-system phase 6, PR #312)

Brought the branch current after #312 landed while #311 sat open awaiting merge.
Conflict surface was four files; only `scripts/ui-lint.sh` needed hand
resolution.

- `scripts/ui-lint.sh` — both sides edited the glyph-target list. Resolution
  keeps main's removal of `$FE/src/style.css` (deleted by #312's CSS split) and
  this phase's addition of `$FE/src/components`. The `--include='*.tsx'` glyph
  scan and main's new `--contrast` mode are independent and both survive.
- `src/app/session-term.ts`, `src/app/state.ts`, `src/main.ts` — auto-merged.
  #312's `applyXtermTheme()` arrived iterating `state.terms.values()` through
  the compat facade; converted to `allTerms()` to match its sibling
  `applyFontSize`, and `ensureTerm`'s lookup to `getTerm`. `session-term.ts` now
  has **no** references to the `state.terms` facade at all.
- New CI gate `ui-lint.sh --contrast` (WCAG AA per preset) passes: 6 presets,
  0 failures. This phase adds no CSS, so it was never at risk — but it is now
  part of the per-phase verification block.

Re-verified on the merged tree: tsc clean, biome at the baseline 10 warnings,
ui-lint + contrast 0 violations, 774 vitest tests, 234 e2e (up from 230 — #312's
new specs included and passing unmodified, 19 skipped are its snapshot-gated
ones), 24 e2e-real. The gate verdict above stands.

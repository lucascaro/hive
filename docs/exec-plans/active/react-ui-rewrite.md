# React UI rewrite — master plan

- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **PR:** [#324](https://github.com/lucascaro/hive/pull/324) — the phase currently in flight (Phase 6). This field tracks the open phase PR, because the spec's `Exec plan:` link points here and `/hs-merge-gate` resolves the plan through it; the per-phase PRs are in the table under [Phases](#phases).
- **Branch:** `feature/react-phase6-single-root`
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

Strangler migration by region. Phase 0 makes state observable (zustand) with zero rendering change; each subsequent phase mounts one React root on an existing region, deletes that region's legacy renderer *in the same PR* (never both live — double-render risk), and rewrites that region's dom tests to RTL. Phase 6 collapses the islands into a single root and deletes all legacy render code. Reused as-is: all of `src/lib/*` (grid math, reorder, shortcuts, worktrees, update-state, keymap, platform, focus helpers), `src/theme/*`, `src/app/session-term.ts`, `src/bridge.ts`, and the focus-trap helpers (moved to `src/lib/focus-trap.ts` in Phase 4).

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


## Gating convention

This feature ships as 7 PRs against one spec, so the spec's `## Success criteria`
describe the **finished** migration and cannot pass until Phase 6. Gating every
phase against them would wave the same unmet criterion through six times and
make the gate meaningless.

So: **each phase plan carries its own `## Success criteria`, and
`/hs-merge-gate` validates a phase PR against that phase's plan.**

**Running the gate on this feature takes one manual step.** The gate resolves
its target from the spec's frontmatter and the spec's `Exec plan:` link, which
points here — so it lands on the master plan, whose success criteria are
Phase 6's. Point it at the phase plan under test instead (its `PR:`, `Branch:`,
`## Success criteria`, `## PR convergence ledger` and `## Gate verdict` sections
are all in the shape the gate expects). The same is true of the ledger the gate
demands: `/hs-review-loop` writes it into a plan it finds by an `<NNN>`-prefixed
name, which this feature's plans do not have, so every phase's ledger here is
maintained by hand. The spec's
criteria are the gate for Phase 6 only, where they must all pass.

Consequences:
- The spec's frontmatter `stage:` stays `IMPLEMENT` while phases 1–6 ship — it
  advances to `DONE` only after the Phase 6 gate. This is exactly the shape
  `ui-design-system` turned out to have: its spec sat at `IMPLEMENT` across
  phases 1–5 and went `DONE` when phase 6 shipped (PR #312, 2026-08-31).
- A phase's own plan moves to `docs/exec-plans/completed/` when its gate passes;
  the master plan moves at the end.
- Every phase still gets `/hs-review-loop` convergence and a full green CI run
  before merge — the per-phase gate is in addition to that, not instead of it.

## Phases

Each phase is its own PR and its own detailed plan. A phase's plan is written
just-in-time, against the then-current code, immediately before that phase is
implemented — the briefs deliberately do not all exist up front.

| Phase | Plan | PR | State |
|---|---|---|---|
| 0 — store + tooling | [phase0](../completed/react-ui-rewrite-phase0.md) | #311 | **merged** |
| 1 — sidebar island | [phase1](react-ui-rewrite-phase1.md) | #317 | **merged** |
| 2 — chrome island | [phase2](react-ui-rewrite-phase2.md) | #318 | **merged** |
| 3 — modals A | [phase3](react-ui-rewrite-phase3.md) | #319 | **merged** (PR merged 2026-09-02; its gate has not been recorded, so the plan stays in `active/` for `/hs-merge-gate`) |
| 4 — modals B + keyboard | [phase4](../completed/react-ui-rewrite-phase4.md) | #320 | **merged** (2026-09-03, `d794caa`); gate PASS |
| 5 — grid shell | [phase5](../completed/react-ui-rewrite-phase5.md) | #321 | **merged** (2026-09-03, `b9ca655`); gate PASS |
| 6 — single root + deletion | [phase6](react-ui-rewrite-phase6.md) | #324 | **in flight** |

**Carried into Phase 6's deletion sweep** (each verified to have zero production
importers at the phase that stranded it — they are reachable only from their own
dom tests, so they cost nothing but a reader's time until then):

*(Both items below were carried out in Phase 6.)*

- `src/ui/button.ts`, `src/ui/field.ts`, `src/ui/kbd.ts` — stranded by Phase 4,
  which ported their last callers (the four remaining imperative modals) to
  `components/Button.tsx`, hand-written field markup and `components/Kbd.tsx`.
  Their `test/dom/ui-{button,field}.test.ts` go with them. Still live and NOT on
  this list: `src/ui/icon.ts` (3 importers) and `src/ui/icon-button.ts`
  (`session-term.ts`).
- `src/app/state.ts`'s compat facade and `window.__hive_state`, per the Phase 0
  review.

## Tests

RTL = `@testing-library/react`. All rewritten tests keep asserting the same classes/data-attrs as today. Playwright e2e + e2e-real: **zero spec changes across the whole migration** (any exception needs explicit sign-off in the PR description).

- Phase 0 — `test/unit/store.test.ts` :: `test_setSessions_replaces_reference_and_notifies`, `test_toggleCollapsed_persists_to_localStorage`, `test_markAttention_immutable_set_update`, `test_minimizeProject_and_restore_roundtrip`, `test_window_hive_state_shape_unchanged` (asserts every field Playwright reads exists).
- Phase 1 — RTL rewrites: `test/dom/ui-session-row.test.tsx`, `ui-project-card.test.tsx`, `ui-chip.test.tsx`, `sidebar-title.test.tsx`, `minimize-project.test.tsx`, `attention-icon.test.tsx`, `selectors.test.tsx`. New: `test/dom/sidebar-dblclick-rename.test.tsx` (row node identity survives re-render between the two clicks), `test/dom/sidebar-reorder.test.tsx`.
- Phase 2 — RTL rewrites: `ui-banner.test.tsx`, `ui-button.test.tsx`, `ui-icon.test.tsx`, `ui-icon-button.test.tsx`, `ui-state-icon.test.tsx` (imports from `src/ui/icon.js`, deleted this phase), `update-banner.test.tsx`, `boot-state.test.tsx`, `restart-hive.test.tsx` (imports `restartHive/isDaemonRestarting/initBanners` from `src/app/banners.js`, gutted this phase). New: `test/dom/status-bar.test.tsx` (setStatus/flash/modeHint render + clear).
- Phase 3 — RTL rewrites: `launcher.test.tsx`, `settings.test.tsx`, `settings-updates.test.tsx`. New: `test/dom/modal-shell.test.tsx` (hidden-class contract, trap acquire/release, Esc close, key hints visible).
- Phase 4 — RTL rewrite: `worktrees.test.tsx` — **45 → 52 cases**: 44 of the
  original 45 kept, one ("does nothing if the browser closed while the dialog
  was open") replaced because it asserted nothing, and 8 added across the
  review — the mid-edit repaint, two keyboard-strand-on-close cases, and five
  covering the answers that delete a branch on a remote, which had no coverage
  at all. (Derivation: `grep -cE "^\s*it\("` on each side of `main...HEAD`.) Planned as rewrites but neither turned out to need one — corrected here at the gate rather than left as a forecast the shipped code contradicts: `focus-trap.test.ts` exercises pure helpers whose signatures did not change, so it took an import repoint (and two cases for the new nullable container) and stays plain jsdom; `keyboard-arrows.test.ts` covers arrow routing *below* the modal ladder and was not touched at all. New: `test/dom/choice-dialog.test.tsx` (open → answer → auto-cleanup, keyboard never stranded), `test/dom/keyboard-precedence.test.tsx` (table-driven over all 9 layers, store-backed ladder matches legacy order), `test/dom/inline-rename.test.ts` (the identity guard a React cleanup depends on), and dom coverage for the three modals that had none: `command-palette`, `project-editor`, `help-overlay`.
- Phase 5 — New: `test/dom/grid-layout.test.tsx` (10 cases: spy-sequence asserts
  template-set-before-attach; reparent not recreate — same node identity across
  passes; out-of-scope tile keeps its DOM node; and on the React half, GridView
  renders no DOM, repaints on a scope change and on a reorder, swaps to the
  single-tile layout on a view change, does NOT repaint on a bell or a rename,
  and re-attaches a reselected active tile without running a pass). Repointed to the new entry points: `grid-reorder-focus.test.ts`
  (calls `applyGridLayout()`), `minimize-project.test.tsx` (`gridScopeFor` from
  `grid-layout.js`) and `events-focus.test.ts` (drops the `renderGrid` dep).
  Forecast here as needing updates but in the event not touched, because they
  mock `view.js` or drive it through commands that survive: `view-floor.test.ts`,
  `xterm-reflow.test.ts`, `attention-jump.test.ts`,
  `attention-jump-integration.test.ts`, `test/dom/nav-history.test.ts`.
  `src/app/view.ts` itself is NOT deleted this phase — Phase 6's scope owns
  that; what goes here is its render half.
- Phase 6 — New: `test/dom/app-shell.test.tsx` (single root mounts all regions, ids present). Sweep: no test imports a deleted module.

## Known spec-edit exceptions

Exactly one is sanctioned, surfaced by the Phase 0 review and carried into the
phase plans that hit it: `test/e2e/nav-history.spec.ts:100` mutates the store's
`minimized` Set in place. It worked until a component subscribed to `minimized`: Phase 2's tray got there
first and converted the line to an assignment (`s.minimized = new Set([...])`,
which routes through `setMinimized`), so Phase 5's grid found it already
correct and changed no spec. It was not a DOM-contract break. Every other spec
edit still means the contract broke.

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
[phase0](../completed/react-ui-rewrite-phase0.md) for its brief, flake baseline and
decision log.

## Phase briefs

### Phase 2 — chrome island (written 2026-09-02 against `main` @ 950dfaf)

**Regions and their roots.** Six islands, each mounted on the id the region
already owns; React renders the *innards*, and the container-level class the
legacy code toggled is applied by a `useLayoutEffect` — the same pattern
Phase 1 used for the `#minimized-projects` portal, and for the same reason
(the class sits outside React's tree and a passive effect would paint one
stale frame).

| Root container | Component | Container-level state kept by effect |
|---|---|---|
| `#banners` (new, `display: contents`) | `Banners` | — |
| `#status` | `StatusBar` | `.error` |
| `#boot-state` | `BootState` | `.hidden` |
| `#empty-state` | `EmptyState` | `.hidden`, `data-kind` |
| `#minimized-tray` | `MinimizedTray` | `.hidden` |
| `#sidebar-hints` | `VersionFooter` | `.mismatch` |

`#banners` is the one markup addition. The three banners are direct children of
the `#app` grid today (`banner.css` places `[data-slot='daemon']` on row 1 and
`[data-slot='update']` on row 2), so a wrapper would collapse them into one row.
`#banners { display: contents; }` in `layout.css` keeps every existing row rule
literal. Render order inside it is undo-close, daemon, update — the order
`initBanners()` + `initUndoClose()` produce by prepending today.

**Store additions** (`src/store/store.ts`, one store — Phase 0 already rejected
splitting it):

- `status: { text, isError }` — the *rendered output* of `lib/status.ts`'s
  `createStatus`, which stays in `app/dom.ts` with its `render` callback writing
  `setStatusText`. `FLASH_MIN_MS` semantics are not reimplemented.
- `modeHint: ModeHint[]` — written by `setModeHint`.
- `bootState: { text, onRetry } | null`.
- `banners: Record<BannerSlot, BannerData>` where `BannerData` is
  `{ text, visible, data?, actions? }`. **Only the data that changes.** The
  static structure — kind, element id, action ids/labels/handlers — lives in
  `Banners.tsx`, which imports the handlers from `banners.ts` / `undo-close.ts`.
  Threading action descriptors and callbacks through the store would be a
  second copy of `ui/banner.ts`'s API for no gain.

**Deviation from this plan's file list (see Decision log).** The five `src/ui/*`
primitives cannot all be deleted here; four have live consumers no phase of this
migration removes. `banner.ts` does go, because the undo-close banner is ported
in this phase too.

**Sanctioned spec edit.** `test/e2e/nav-history.spec.ts:100` — `MinimizedTray`
subscribes to `minimized`, so the in-place `.add` stops rendering. Changed to
the store action, as the master plan pre-authorised.

### Phase 3 — modals A: launcher + settings (written 2026-09-02 against `main` @ fff838f)

**Roots.** Two islands, same shape as Phase 2: React renders the innards, the
container keeps its id and its container-level classes are applied from a
`useLayoutEffect`.

| Root container | Component | Container-level state kept by effect |
|---|---|---|
| `#launcher` (exists in `index.html`, `class="hidden" role="menu"`) | `Launcher` | `.hidden`, inline `left`/`top` |
| `#settings` (**new in `index.html`**, `class="hv-dialog hidden"`) | `Settings` via `ModalShell` | `.hidden` |

`#settings` is the one markup addition. Today `settings.ts` builds the whole
dialog with `ui/dialog.ts` and `initSettings()` appends it to `#app`; a React
island needs a mount node that exists before the root is created, so the root
div moves into `index.html` with the attributes `dialog()` set on it
(`id`, `class="hv-dialog hidden"`, `role="dialog"`, `aria-modal="true"`,
`aria-labelledby="settings-title"`). Everything inside it is `ModalShell`'s.

**Markup contract extracted from the legacy modules** (the e2e specs assert on
all of it, unmodified):

- `#launcher` › `input.launcher-search` (`aria-label="Filter agents"`,
  `placeholder="Filter agents…"`, `autocomplete=off`) · `label.launcher-worktree`
  (`input[type=checkbox]` + `<span>`, `.disabled` when the project is not a git
  repo) · `input.launcher-branch` (`aria-label="Worktree branch name"`,
  `.hidden` while the toggle is off) · `div.launcher-list` ›
  `div.launcher-item[data-selected][data-available=false][style=--agent-color]`
  › `span.agent-num` › `kbd.hv-kbd` · `span.agent-dot` · `span.agent-name` ·
  `span.install-tag[title]`. Empty/loading rows are `div.launcher-empty`
  ("No agents match" / "No agents found") and `div.launcher-loading`
  ("Loading agents…"). **Order matters**: search, worktree row, branch box, list.
- `#settings` › `.hv-dialog__panel#settings-panel[data-size=md]` ›
  `header.hv-dialog__header` › `h3.hv-dialog__title#settings-title` +
  `span.hv-dialog__title-suffix`, `button.hv-icon-btn.hv-dialog__close#settings-close`;
  `div.hv-dialog__body` › `#settings-scroll` (agents then appearance) and
  `section#settings-updates`; `footer.hv-dialog__footer` ›
  `div.hv-dialog__actions` › `#settings-cancel`, `#settings-save`.
  Inside: `#settings-agents-list` › `div.settings-agent-row` ›
  `span.hv-swatch` › `input[type=color]`, `input.settings-agent-name`,
  `input.settings-agent-cmd`, `button.settings-agent-delete`; `#settings-agent-add`;
  `p#settings-error.hv-field-error.settings-error`; `#settings-theme`,
  `#settings-overrides` + `p#settings-overrides-error`;
  `#settings-update-channel`, `#settings-source-repo-row` (`.hidden` off the
  latest channel) › `#settings-source-repo` + `#settings-source-repo-browse`,
  `p#settings-source-repo-hint`, `#settings-update-action` +
  `#settings-update-status`. Hint paragraphs are `p.settings-hint`.

**Store additions** (`src/store/store.ts`): `modals: ModalId[]` with
`openModal`/`closeModal`/`isModalOpen`. The stack is the *render* signal —
which React modal is mounted-visible — and nothing more.

**Deviation from the plan's `anyModalOpen()` line (see Decision log).** No
OR-ing of two sources. `registry.ts` already answers `anyModalOpen()` off the
`.hidden` class of every registered root, and both React roots keep their root
element registered and keep toggling `.hidden` from the store. The DOM class
stays the single source of truth for "a modal owns the keyboard", so
`focus.ts`, `session-term.ts` and every `getElementById(...).classList` gate in
`keyboard.ts` are untouched.

**Ported behaviour that is easy to lose** (each has a test):
open-generation token (`ListAgents` / `IsGitRepo` staleness), `openToken`
(settings load staleness), the overrides debounce (150 ms) and the source-repo
probe debounce (250 ms) *including their cancellation on close*, digit
shortcuts only while the raw query is empty and not in the branch box,
`mousedown` `preventDefault` on everything but the two text boxes, `focusout`
close, the document-level outside-click close with its
`.hv-project-card__actions` / `[data-opens-launcher]` exemptions, re-entrant
`openSettings()` not wiping a draft, `loadFailed` blocking Save, and
agents-before-update-settings save order.

### Phase 4 — modals B + keyboard reads the store (written 2026-09-02 against `main` @ 588673b)

**Roots.** Five islands. `#command-palette` already exists in `index.html`
with its input and list; the other four dialogs are built at import time by
`ui/dialog.ts` today and their roots move into `index.html` with exactly the
attributes `dialog()` stamps (`class="hv-dialog hidden"`, `role`,
`aria-modal="true"`, `aria-labelledby="<id>-title"`) — same reason as Phase 3's
`#settings`: a React root needs a mount node that exists before the store says
the modal is open.

| Root container | Component | Container-level state kept by effect |
|---|---|---|
| `#worktrees` (**new in `index.html`**) | `Worktrees` via `ModalShell` | `.hidden` |
| `#project-editor` (**new**) | `ProjectEditor` via `ModalShell` | `.hidden` |
| `#help-overlay` (**new**) | `HelpOverlay` via `ModalShell` | `.hidden` |
| `#choice-dialog` (**new**, `role="alertdialog"`) | `ChoiceDialog` via `ModalShell` | `.hidden` |
| `#command-palette` (exists) | `CommandPalette` | `.hidden` |

`#choice-dialog` stops being built per question. That is the point of the phase:
the per-question element had to be `unregisterModal`'d on close or
`anyModalOpen()` answered true forever and stranded the keyboard. A static root
whose visibility is a store field cannot forget.

**Markup contract extracted from the legacy modules** (the e2e specs assert on
all of it, unmodified):

- `#worktrees` › `#worktrees-panel[data-size=lg]` › header with
  `h3#worktrees-title` ("Worktrees") + `span.hv-dialog__title-suffix` ›
  `span#worktrees-project` (`· <project name>`, empty when the project is
  unnamed), `button#worktrees-close.hv-icon-btn.hv-dialog__close`; body ›
  `#worktrees-empty.worktrees-empty` (`.hidden` when a payload rendered) ›
  `div.worktrees-empty-card` › `span#worktrees-empty-spinner.phase-spinner`
  (`.hidden` when not spinning) + `span#worktrees-empty-text`, and
  `#worktrees-body` › `section#worktrees-section-trees` (`h4` "Worktrees",
  `#worktrees-list`) + `section#worktrees-section-branches` (`h4` "Branches with
  no worktree", `p.worktrees-hint` "Create a worktree to pick this work back
  up.", `#worktrees-branches`); footer hints `[esc]` close · `(r)` refresh.
  Rows: `div.worktree-row[data-kind][data-path]` (worktrees) and
  `div.worktree-row[data-branch]` (branches) › `div.worktree-main` ›
  `span.worktree-name[title=path]` + `span.worktree-status` +
  optional `span.worktree-subject[title]`; optional
  `span.worktree-badge` / `span.worktree-badge.merged`; `div.worktree-actions` ›
  plain `<button type=button [title] [data-opens-launcher] [.danger] [disabled]>`.
  The rename input is `input.worktree-rename[aria-label="New branch name"]`
  inside `.worktree-main`.
- `#project-editor` › `#project-editor-panel[data-size=sm]` › `h3#project-editor-title`
  ("New project" / "Edit project"), `button#project-editor-close`; body › three
  `ui/field.ts` rows — `#project-editor-name`, then `div.cwd-row` ›
  `#project-editor-cwd` + `button#project-editor-browse` ("Browse…"), then the
  `colorInput` wrapper (`--swatch` custom property) › `#project-editor-color`;
  footer actions `#project-editor-cancel`, `#project-editor-save`.
- `#help-overlay` › `#help-overlay-panel[data-size=lg]` › `h3#help-overlay-title`
  ("Keyboard shortcuts"), `button#help-overlay-close`; body ›
  `#help-overlay-groups` › one `<section>` per `shortcutGroups({isMac})` group ›
  `h4` + `dl` › `dt` › `kbd.hv-kbd` (via `Kbd`) and `dd` › label. Footer hint
  `[esc]` close. No actions.
- `#choice-dialog.choice-dialog[role=alertdialog]` › `#choice-dialog-panel[data-size=sm]`
  › `h3#choice-dialog-title`, **no close button**; body ›
  `p.choice-dialog-detail`, `ul.choice-dialog-bullets` › `li`,
  `p.choice-dialog-note` (each rendered only when present); footer actions ›
  `button[data-choice=<value>]` in spec order, the danger ones additionally
  `.danger` (on top of `Button`'s own `data-kind="danger"`).
- `#command-palette` (unchanged markup) › `#command-palette-input` +
  `#command-palette-list` › `div.palette-item[data-selected]` ›
  `span.palette-name` + `span.palette-shortcut` › `kbd.hv-kbd`.

**Store additions** (`src/store/store.ts`):

- `ModalId` grows to `'launcher' | 'settings' | 'project-editor' |
  'command-palette' | 'worktrees' | 'help'`, with payloads
  `{ id: 'project-editor'; editing: ProjectInfo | null }` and
  `{ id: 'worktrees'; projectId: string; projectName: string }`. The palette and
  the help overlay carry nothing.
- `worktreesPayload: WorktreesPayload | null` — the daemon's last inventory for
  the open project, written by `handleWorktreesPayload`, cleared on open. The
  module keeps the stale-reply filter (`readProjectIdOf(payload) !== projectId`
  → ignore) because it is protocol logic, not rendering.
- `choiceDialog: { spec: ChoiceSpec; seq: number } | null` — separate from the
  `modals` stack because it is mounted over any of them and because its answer
  travels back through a promise the openers await.

**`anyModalOpen()` moves into the store; `modals/registry.ts` and
`src/ui/dialog.ts` are deleted.** Phase 3 kept the DOM class as the single
source of truth precisely because the legacy modals had no store entry. After
this phase every modal does, so `anyModalOpen()` becomes
`modals.length > 0 || choiceDialog !== null` and `focus.ts` / `session-term.ts`
import it from the store. `ui/dialog.ts`'s last four callers are ported here, so
the primitive goes with them — `ModalShell` is its React replacement and
`test/dom/modal-shell.test.tsx` its test. `test/dom/ui-dialog.test.ts` is
deleted with it; `docs/design-docs/ui/components.md` › dialog now documents
`ModalShell`.

**Keyboard ladder** (`src/app/keyboard.ts`) — order copied verbatim, each layer's
`.hidden` query replaced by the store read next to it:

| # | Layer | Was | Becomes |
|---|---|---|---|
| 1 | inline rename | `inlineRenameActive()` | unchanged (not a modal) |
| 2 | choice dialog | `choiceDialogOpen()` | `choiceDialogOpen()`, now a store read |
| 3 | launcher | `!launcherEl.classList.contains('hidden')` | `isModalOpen('launcher')` |
| 4 | project editor | `!editorEl.classList…` | `isModalOpen('project-editor')` |
| 5 | command palette | `getElementById('command-palette')…` | `isModalOpen('command-palette')` |
| 6 | settings | `getElementById('settings')…` | `isModalOpen('settings')` |
| 7 | worktrees | `getElementById('worktrees')…` | `isModalOpen('worktrees')` |
| 8 | help overlay | `getElementById('help-overlay')…` | `isModalOpen('help')` |
| 9 | dead-session overlay | `state.terms.get(activeId).deadOverlayShown` | unchanged |
| 10 | app bindings | — | unchanged |

`trapFocus` still needs an element, so each layer that traps passes its root via
`pageEl(<id>)`. The handler stays registered capture-phase, and every layer keeps
its own `return` — the ladder's shape is what the table-driven test pins.

**Ported behaviour that is easy to lose** (each has a test):

- The worktree rename is still `beginInlineRename` (so `inlineRenameActive()`
  keeps layer 1 true and Escape cancels the edit instead of closing the panel).
  It mounts into an *empty* React-rendered `.worktree-main` — React owns no
  children there while `renaming` is set, so a daemon repaint mid-edit can no
  longer clobber the input (the imperative version lost the edit).
- `closeWorktrees()` dismisses an open choice dialog; so does every repaint
  (`render()` did it because the row being asked about may not survive).
- Both destructive flows re-check `worktreesOpen() && projectId` after the await.
- The `(r)` refresh key ignores keystrokes typed into the rename input.
- The palette: `mouseenter` moves the selection, `ArrowDown`/`Tab` wrap,
  activation defers the command by `setTimeout(…, 0)` so the palette is fully
  closed before an action opens another modal, and the outside-click close.
- `closeCommandPalette()` blurs the input *before* hiding (`focusActiveTerm()`
  bails while `activeElement` is an INPUT).
- The project editor focuses its name field synchronously on open (a deferred
  focus raced ⌘N-then-Escape and typed into a `display:none` dialog), Enter
  saves from the name and cwd fields only, and an empty name is a no-op save.
- The help overlay renders its (static) groups once and `toggleHelpOverlay()`
  stays the single entry point the native ⌘/ menu item drives.
- The choice dialog's FIRST choice is the safe one: it takes focus, and Escape
  and a backdrop click resolve to it. Focus returns to the opener only if it is
  still connected (deleting a worktree takes its row's button with it).

### Phase 5 — grid shell (written 2026-09-03 against `main` @ e6757d0)

**Verbatim-move list.** Everything below moves from `src/app/view.ts` to the
new `src/app/grid-layout.ts` unchanged except for the two mechanical edits
named in "lines that change". Nothing else in these bodies is touched.

| From `view.ts` | To `grid-layout.ts` | Lines that change |
|---|---|---|
| `showSingle()` :66 | `applySingle(id)` | rename; `state.terms` → `termsMap()` / `getTerm()` |
| `_ric` :230 | `_ric` | none |
| `attachDeferred()` :234 | `attachDeferred` | none |
| `renderGrid()` :249 | `applyGridLayout()` | rename; `state.terms` → registry. **As shipped:** `deps.ensureTerm` / `deps.scrollTrace` stay on a deps seam (`initGridLayout()`, forwarded from `initView()`) rather than becoming direct imports — importing `ensureTerm` here would close a `session-term` ↔ `grid-layout` cycle. Phase 6 deletes both seams. |
| `gridLayout` cache :210 | module-local + `currentGridLayout()` | export accessor for `gridSpatialMove` (plus `spatialTarget()`, kept next to the cache so the two cannot drift) |
| `rebaselineGridReplayCols()` :592 | exported | none |
| `gridScopeFor()` :467, `gridScopeSessions()` :486 | same | none |
| `#terms` ResizeObserver :665-679 | same | `renderGrid()` → `applyGridLayout()` |

Stays in `view.ts` (commands, not rendering): `isSessionHidden` — it reads
nothing but the store, and moving it would pull `grid-layout.ts`'s module-scope
ResizeObserver into `keyboard.ts`'s import graph, which four dom tests mock
`view.js` specifically to avoid — plus `switchTo`, `switchToProject`,
`firstVisible`, `fallBackToSingleIfActiveHidden`, `gridSpatialMove`,
`shiftActiveProject`, `minimizeSession`, `restoreSession`, `minimizeProject`,
`restoreProject`, `enforceViewFloor`, `setView`, `updateAppTitle`. Each loses
its `renderGrid()` / `showSingle()` call; the store write that used to precede
it is what repaints now. `view.ts` is deleted in Phase 6, not here — Phase 6's
scope line is the authority; this plan's Tests line ("`src/app/view.js`
(deleted this phase)") is corrected to mean `initView` and the render exports.

**Trigger model.** `GridView` subscribes to a *derived layout signature*, not
to `sessions`:

```
`${view}|${activeId}|${gridProjectId}|${gridScopeSessions().map(s => s.id).join(' ')}`
```

and its single `useLayoutEffect` depends on that string alone. Two findings
force this shape, both verified in the current code:

- **`attention` must NOT be a dependency**, though this plan's Scope line
  listed it. Today the attention class is patched straight onto the host by
  `events.ts:174/177/184/226` and `focus.ts:54` — a bell never calls
  `renderGrid()`. Subscribing to it would repaint on every bell, and
  `attachDeferred` calls `ensureAttached()` on every in-grid tile, which
  re-latches follow-bottom (invariant 2). `applyGridLayout` keeps setting the
  class during a pass, reading `attention` non-reactively from
  `store.getState()` — exactly what `renderGrid` does today.
- **Raw `sessions` must NOT be a dependency.** `session:event(updated)` is the
  high-frequency kind (one per phase step, one per surviving session after a
  kill recompacts order, one per agent-id capture poll) and replaces the array
  reference every time. Today only two of those branches repaint
  (`events.ts:460` removal, `:469` order change) — both of which change the
  signature. A rename changes the array but not the signature, and today it
  does not repaint either.

**Ordering.** Store writes made by a command that has post-layout work
(`focusActiveTerm`, `snapVisibleTermsToBottom`, `rebaselineGridReplayCols`) are
wrapped in `flushSync` so the layout effect has already run when that work
starts — the same pattern the six modals adopted in Phases 3-4 for plain
listeners. Without it the effect would land after `focusActiveTerm`, inverting
invariants 3 and 4.

**Call sites that lose their explicit repaint** (the store write now carries
it): `events.ts` `deps.renderGrid` (seam field deleted, both calls with it);
`session-term.ts:620-628` mousedown (`setActive` alone — the `activeId` change
repaints); `main.ts`'s `renderGrid` import and its `wireDaemonEvents` argument.

**Known behaviour delta, accepted:** `switchTo(id)` where `id` is already
active in a grid view used to run a full `renderGrid()` and thereby re-anchor
every background tile to the bottom. It now repaints nothing (no signature
change); the active tile still gets its explicit
`snapVisibleTermsToBottom([st])`. This is strictly fewer `ensureAttached()`
calls, which is the direction the invariant allows.

**Mount point.** `GridView` renders `null`, so it needs a container of its own
rather than `#terms` (whose children are the terminal hosts React must never
own): `index.html` gains `<div id="grid-root" hidden></div>`. `hidden` is
`display: none`, so it is not a grid item and the `#app` row placement — every
region of which is placed explicitly — is unchanged. Phase 6 removes the
element with the island array.

## PR convergence ledger

This feature ships as seven PRs against one spec, so convergence is recorded
per phase, in each phase plan's own ledger. This section is the index the gate
reads: one line per phase, copied from that phase's final entry, because
`/hs-merge-gate` resolves the plan through the spec's `Exec plan:` link — which
points at this master plan, not at the phase plan whose PR is actually under
test.

- **Phase 0** — PR #311 — verdict: APPROVE; threads_open: 0; action: stop; head_sha: cd66176. Gate: NEEDS_FOLLOWUP (2026-09-01), accepted; plan in `completed/`.
- **Phase 1** — PR #317 — verdict: COMMENT; threads_open: 0; action: stop; head_sha: 9b68a26. Merged 2026-09-02 (`950dfaf`). Gate: not run.
- **Phase 2** — PR #318 — verdict: APPROVE; threads_open: 0; action: stop; head_sha: 26697ec. Merged 2026-09-02 (`fff838f`). Gate: not run.
- **Phase 3** — PR #319 — verdict: APPROVE; threads_open: 0; action: stop; head_sha: a65813f. Merged 2026-09-03 (`7af0f7c`). Gate: not run.
- **Phase 4** — PR #320 — verdict: APPROVE; threads_open: 0; action: stop; head_sha: 8439446. Merged 2026-09-03 (`d794caa`). Five review iterations; see [phase4](../completed/react-ui-rewrite-phase4.md#pr-convergence-ledger). Gate PASS 2026-09-03 (doc accuracy failed twice on stale counts in the plan's own bookkeeping, fixed both times on the branch).
- **Phase 5** — PR #321 — verdict: COMMENT; threads_open: 0; action: stop; head_sha: ac1158f. Merged 2026-09-03 (`b9ca655`). Two review iterations; see [phase5](../completed/react-ui-rewrite-phase5.md#pr-convergence-ledger). Gate ran post-merge: FAIL on two stale doc claims, then PASS on the re-run once they were fixed.

## Gate verdict

Per the [Gating convention](#gating-convention), a phase PR is gated against
**its own** plan's success criteria; the spec's criteria are the gate for
Phase 6 only, where they must all pass. Phase verdicts therefore live in the
phase plans. This section records only the spec-level gate, and stays empty
until Phase 6.

### Phase 6 — single root, legacy deletion, docs (written 2026-09-03 against `main` @ b9ca655)

Three lines of the Phase 6 scope were written in Phase 0 and have gone stale;
this brief supersedes them.

| Stale line | Reality at `b9ca655` | Brief |
|---|---|---|
| "Delete `src/app/view.ts`" | Phase 5 moved the *rendering* out (`grid-layout.ts`); what is left is the 442-line **view-command** module — `switchTo`, `setView`, `minimizeProject`, `shiftActiveProject`, `enforceViewFloor`. Deleting it deletes behaviour. | **Keep.** Nothing to delete. |
| "Delete `src/app/el.ts`" | Still imported by `app/dom.ts` (`termsHost`), `keyboard.ts` (`trapFocus` roots, `#app`), `session-term.ts` (`#terms`) and five modals (`releaseFocus` roots). | **Keep both `mustEl` and `pageEl`.** Only `main.ts`'s 20 `pageEl`/`mustEl` island calls go. |
| "`ui/button.ts` has zero production importers" | True. The two apparent importers (`components/Button.tsx`, `components/modals/Worktrees.tsx`) name it in comments only. | **Delete** `ui/button.ts`, `ui/field.ts`, `ui/kbd.ts` and their dom tests. `ui/icon.ts` and `ui/icon-button.ts` stay (live importers). |

**The `state` facade (the bulk of the phase).** 247 `state.*` references across 13
production files, and — the number the original scope did not have — **zero
write sites** in `src/`: every mutation was converted to a store action in
Phases 0–5. So the facade is a read-only surface, and its deletion is a
mechanical read conversion, not a semantic one.

Per-module idiom, chosen over a new shared export so no second state API
appears:

```ts
import { appStore } from '../store/store.js';
import { termsMap } from '../store/terms.js';
// Live read. A function, not a destructured snapshot: these modules run
// inside event handlers and must never cache a slice across a store write.
const s = () => appStore.getState();
```

`state.<field>` → `s().<field>` for every `AppData` field; `state.terms` →
`termsMap()` (the registry is deliberately outside the store — `store/terms.ts`).

**Where the facade goes.** The object moves verbatim into `src/store/store.ts`
as `hiveStateView`, together with the `VITE_WAILS_MOCK`/`VITE_WAILS_REAL`-gated
`window.__hive_state` assignment. Shape unchanged (Invariant 7). This is not a
rename of the compat layer: what made it a compat layer was that thirteen
production modules imported it, and after this phase **none do**. It survives as
exactly one thing — the Playwright API the specs read
`.terms.get(id).term.buffer.active` and `.sessions` off.

`src/app/state.ts` is left as a types-only module (`SessionInfo`, `ProjectInfo`,
`TermTile`, `AppState`) — the file is not renamed, because ~30 files import
those types and a rename is a diff through all of them for no gain. Its five
dead re-exports (`loadSavedView`, `loadSavedCollapsed`,
`loadSavedMinimizedProjects`, `saveCollapsed`, `saveMinimizedProjects` — zero
importers) go with the facade. Side effect worth noting: `state.ts` stops
importing `store.ts`, so the `app/state ↔ store/store` import cycle disappears.

**Tests keep writing through the facade, by design.** The dom suite seeds state
with `state.sessions = […]` at 192 sites across 14 files. Those sites are NOT
converted: rewriting the seeding of the tests that are the safety proof for this
refactor, in the same commit as the refactor, is how a false green happens. The
14 files change one import line each — `src/app/state.js` → `src/store/store.js`,
`state` → `hiveStateView` — and nothing else. `test/unit/store.test.ts`'s
`window.__hive_state` shape assertion re-points at the new home.

**Deps seams — audit, don't sweep.** Nine `init*`/`wireDaemonEvents` seams exist.
They are the acyclic-module design (modals ⇢ focus, view ⇢ session-term), not
render scaffolding, and the dom tests inject stubs through them. Each is checked
individually: a seam is removed only if its injected members become reachable by
a direct import with no cycle *and* no test stub depends on it. Whatever remains
is recorded here with the cycle it breaks.

**Single root.** `src/components/App.tsx` composes, in `index.html`'s document
order, the regions the fourteen islands own today. Two of them are not plain
children and constrain the design:

- `Sidebar` renders into `#projects` (a `<ul>` inside `<aside id="sidebar">`) and
  portals its minimized tray into `#minimized-projects`, a sibling. `VersionFooter`
  owns `#sidebar-hints`, another sibling.
- `GridView` renders no DOM and must not own `#terms`, whose children are
  SessionTerm hosts (Invariant 5).

So App.tsx mounts on `#app` and renders the *whole* `#app` subtree except
`#terms` and `#sidebar-resizer`, both of which stay static in `index.html` and
are reached imperatively as they are today. Mount order effects are preserved by
component order: `VersionFooter`'s `daemon:stale` subscription must still be
live before the modals mount (the reason the island array ordered it there).

`src/main.tsx` replaces `src/main.ts`, bootstrap order **theme → hydrate store
from localStorage → wire daemon events → mount root → freeze heartbeat**. The
`index.html` `<script src>` and `vite.config.js` entry follow. `index.html`
keeps: the theme-stamp inline script (Invariant 9), the stylesheet links, the
static boot overlay markup, `#app`, `#terms`, `#sidebar-resizer`. `#grid-root`
is removed with the island array.

**Verification** additionally runs the e2e suite 3× and a `wails build` smoke
(never `-s`), per the master Verification block.

## Decision log

- **2026-09-03 (Phase 6) — three scope lines were stale; the brief supersedes
  them.** "Delete `src/app/view.ts`" and "delete `src/app/el.ts`" were written
  in Phase 0, before Phase 5 turned view.ts into the view-*command* module and
  while el.ts still had only main.ts as a consumer. Both files are live and
  stay; see the Phase 6 brief's table for what actually got deleted. Deleting
  them as written would have removed `switchTo`/`setView`/`minimizeProject` and
  the `mustEl` handles `dom.ts`, `keyboard.ts`, `session-term.ts` and five
  modals import.

- **2026-09-03 (Phase 6) — the facade conversion was read-only.** All 247
  `state.*` references in `src/` are reads; Phases 0-5 had already converted
  every write to a store action. So deleting the facade was a mechanical
  substitution (`appData()` / `termsMap()`), not a semantic change, which is why
  it could land in the same PR as the single root.

- **2026-09-03 (Phase 6) — the dom tests keep seeding through the facade
  object, deliberately.** 192 seeding sites across 14 files write
  `state.sessions = […]`. Converting them to store actions in the same commit
  as the refactor they are the safety proof for is how a false green happens, so
  they were left alone: each file changed one import line
  (`src/app/state.js` → `src/store/store.js`, `state` → `hiveStateView`) and
  nothing else. The object they seed through is the same one Playwright reads,
  which is the property that makes it honest rather than a test-only shim.

- **2026-09-03 (Phase 6) — deps-seam audit: all nine stay.** The user's call was
  "remove only the seams `main` no longer needs", audited individually. Result:
  **zero**. `initView` and `initGridLayout` break a real
  `session-term ↔ view/grid-layout` cycle (session-term.ts imports view.js).
  `wireDaemonEvents` registers listeners and is not a seam in the same sense.
  `initCommandPalette` and part of `initWorktrees` carry main-owned wiring (the
  palette command table, `openSessionIn`). The five modal seams
  (`initLauncher`, `initProjectEditor`, `initSettings`, `initHelpOverlay`,
  `initWorktrees`) inject only members of `app/focus.ts`, which imports nothing
  that would cycle — so those are removable on cycle grounds alone. They stayed
  anyway: **each is the injection point a dom test uses to spy on focus
  behaviour** (`launcher`, `project-editor`, `settings`, `settings-updates`,
  `worktrees`, `help-overlay`, `command-palette`, `nav-history`). Removing them
  means rewriting those seven suites to module mocks in the same PR as the root
  collapse — the same false-green risk as the seeding sites above, for no
  user-visible gain. Filed as a candidate for a standalone follow-up instead.

- **2026-09-03 (Phase 6) — the single root renders portals, not markup.** The
  tree cannot own `#app`: `#terms`' children are SessionTerm hosts (Invariant
  5), and `#boot-state`'s card is painted pre-script on purpose, so a tree that
  emitted `#app`'s children would blank and rebuild it at mount. The root mounts
  on an empty hidden `#react-root` (the former `#grid-root`) and every region is
  a portal into the element it already owned — which also makes Invariant 1
  free. **The trap this exposed:** an island root *clears* its container on
  first render, a portal *appends*. `#status`, `#boot-state` and
  `#sidebar-hints` are seeded with pre-paint markup, so the first run duplicated
  their ids and 12 e2e specs failed. `main.tsx` now empties exactly those three
  and flushes the first commit synchronously, so no frame lands between.

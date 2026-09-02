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


## Gating convention

This feature ships as 7 PRs against one spec, so the spec's `## Success criteria`
describe the **finished** migration and cannot pass until Phase 6. Gating every
phase against them would wave the same unmet criterion through six times and
make the gate meaningless.

So: **each phase plan carries its own `## Success criteria`, and
`/hs-merge-gate` validates a phase PR against that phase's plan.** The spec's
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
| 3 — modals A | [phase3](react-ui-rewrite-phase3.md) | #319 | implemented, in review |
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

## Known spec-edit exceptions

Exactly one is sanctioned, surfaced by the Phase 0 review and carried into the
phase plans that hit it: `test/e2e/nav-history.spec.ts:100` mutates the store's
`minimized` Set in place. It works until a component subscribes to `minimized`
(Phase 2's tray, Phase 5's grid), at which point that phase changes the line to
call an action. It is not a DOM-contract break. Every other spec edit still
means the contract broke.

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

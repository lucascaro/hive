# React UI rewrite — Phase 2: Chrome island: status bar, banners, boot/empty state, tray, footer

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **PR:** [#318](https://github.com/lucascaro/hive/pull/318)
- **Branch:** `feature/react-ui-rewrite-phase2`
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Scope

New store state + actions: `status {text, hint, modeHint, flash}`, `banners[]`, `bootState`; actions `setStatus/flashStatus/setModeHint/reportFailure/setBootState/showBanner/dismissBanner`. Reuse, don't re-derive: flash timing stays in `src/lib/status.ts`'s `createStatus` engine (store holds only its rendered output — do not reimplement FLASH_MIN_MS semantics in actions); empty-state content comes from `src/lib/empty-state.ts`'s pure `emptyStateModel()` called in a selector, not stored; the minimized tray is derived in a selector via `src/lib/minimized.ts` (`filterHidden`) from sets already in the store — no new stored state for either.

New files: `src/components/StatusBar.tsx`, `Banner.tsx`, `Banners.tsx`, `BootState.tsx`, `EmptyState.tsx`, `MinimizedTray.tsx`, `VersionFooter.tsx`.

Files to change / delete:
- `src/app/dom.ts` — shrinks to `termsHost` + status functions whose bodies forward to store actions (signatures kept — every module calls `setStatus`). Import-time DOM mutation moves to `main.ts`.
- `src/app/banners.ts` — DOM building deleted; show/dismiss policy + `src/lib/update-state.ts` integration become actions.
- `src/app/version-footer.ts`, empty-state DOM parts of `src/lib/empty-state.ts`, boot-state writes in `main.ts` — replaced by components. Static boot overlay markup stays in `index.html` for pre-JS paint; `BootState` takes over the same ids on mount. `reportFailure` + bounded 5-attempt `retryBoot` keep exact semantics.
- `src/ui/banner.ts` — deleted (its last consumer, `app/undo-close.ts`, is
  ported in this phase too). `src/ui/chip.ts` — deleted; Phase 1 left it alive
  for `view.ts`'s minimized-session tray, which this phase ports.
- `src/ui/button.ts`, `icon.ts`, `icon-button.ts`, `kbd.ts` — **NOT** deleted.
  See the Decision log; every one has a live consumer this phase does not
  touch, and two of them have one no phase of this migration removes.
- `src/app/view.ts` — `renderEmptyState()` and `renderMinimizedTray()` deleted
  along with every call site, and with them their entries in `EventsDeps`
  (`app/events.ts`) and the `view.ts` imports in `app/keyboard.ts` and
  `app/session-term.ts`. Both regions are pure projections of store state, so a
  surviving imperative renderer would fight React for the same container — the
  double-render the master plan forbids.
- `index.html` — one added element, `<div id="banners">`, with
  `#banners { display: contents; }` in `src/theme/layout.css`.

## Success criteria

What `/hs-merge-gate` validates for THIS phase.

- Status bar, banners, boot state, empty state, minimized tray and version
  footer are React-rendered into the same ids.
- Flash timing is still `src/lib/status.ts`'s `createStatus` engine — the store
  holds its rendered output, and `FLASH_MIN_MS` semantics are not reimplemented
  in an action.
- Empty-state content comes from `emptyStateModel()` in a selector, and the tray
  from `filterHidden()` — neither is stored as new state.
- The static boot overlay markup stays in `index.html` for pre-JS paint, and
  `BootState` takes over the same ids on mount.
- `reportFailure` and the bounded 5-attempt `retryBoot` keep their exact
  semantics.
- `src/ui/banner.ts` and `src/ui/chip.ts` are deleted with no remaining
  callers. (`button.ts`, `icon.ts`, `icon-button.ts` and `kbd.ts` are NOT —
  see the Decision log.)
- `renderEmptyState()` and `renderMinimizedTray()` are gone from `view.ts`, and
  no module still calls them: one renderer per region, never two.
- `setStatus`'s signature is unchanged — every module calls it.
- The three banners are still direct children of the `#app` grid, so
  `banner.css`'s `[data-slot='daemon'] { grid-row: 1 }` / `[data-slot='update']
  { grid-row: 2 }` still place them.
- All 30 Playwright e2e specs pass with exactly one edit, the pre-authorised
  `nav-history.spec.ts` line below.

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

## Known spec-edit exception (carried from Phase 0 review)

`test/e2e/nav-history.spec.ts:100` does
`window.__hive_state?.minimized.add(id)` — an **in-place** mutation of a store
Set, the one pattern the store's reference equality cannot see.

It is correct today and stays correct through Phase 1: the facade getter returns
the live Set, and with no component subscribed to `minimized` the following
render picks the change up. **It stops working in the first phase that
subscribes to `minimized`** — Phase 2's `MinimizedTray` selector, and again in
Phase 5's `GridView`.

Deliberately NOT fixed in Phase 0: the migration's safety proof is that the e2e
specs never change, and editing one to chase a latent issue would have spent
that proof on a non-issue. When the subscriber lands, this is the **one
sanctioned spec edit** — `window.__hive.store.minimizeSession(id)` (or the
equivalent action exposed on the test global) instead of the raw `.add`. It is
NOT a DOM-contract break, so the "a spec edit means the contract broke" rule
does not apply to this line. Note it in that phase's PR description as the
signed-off exception the master plan's Tests section requires.


## Decision log

**2026-09-02 — the `src/ui/*` deletion list in Scope was wrong; four of the five
files stay.** Checked against live imports before starting:

| File | Live consumers this phase does not port |
|---|---|
| `icon.ts` | `app/session-term.ts`, `app/modals/project-editor.ts`, and `components/Icon.tsx` imports `ensureSprite()` from it |
| `icon-button.ts` | `app/session-term.ts`, `app/modals/settings.ts` |
| `button.ts` | `app/modals/{choice-dialog,project-editor,settings}.ts` (Phases 3-4), `app/view.ts` (Phase 5) |
| `kbd.ts` | `app/modals/{command-palette,help-overlay,launcher,worktrees}.ts` (Phases 3-4) |

`icon.ts` and `icon-button.ts` cannot be deleted by ANY phase: `session-term.ts`
stays imperative for the whole migration (master plan › Non-goals), so both
outlive Phase 6. `button.ts` and `kbd.ts` go with the modals and `view.ts` in
Phases 3-5. This is the same shape as Phase 1's `chip.ts` deviation. The
success criteria above are corrected accordingly; nothing else in the phase
changes.

**2026-09-02 — `app/undo-close.ts` is ported in this phase, though Scope named
only `banners.ts`.** It raises a third banner (`data-slot="undo-close"`) through
`ui/banner.ts`, and Scope calls for `ui/banner.ts` to be deleted. Leaving it
imperative would mean two banner mechanisms prepending into `#app` at once — and
`ui/banner.ts` has no scheduled home in any later phase, so "later" would mean
never. Decided with the user before implementation.

**2026-09-02 — the banner store slice holds data only, not action
descriptors.** `banners: Record<BannerSlot, BannerData>` carries text,
visibility, the root's `data-*` and per-action label/hidden/disabled. The static
half — kind, element id, action ids and their click handlers — is declared in
`components/Banners.tsx`, which imports the handlers from `app/banners.ts` and
`app/undo-close.ts`. Threading callbacks through the store would have been a
second copy of `ui/banner.ts`'s API for no gain.

**2026-09-02 — `#empty-state`'s `data-sig` is gone.** It keyed the imperative
rebuild off a JSON signature of the model so the renderer could skip an
`innerHTML` wipe. React reconciles, so the signature went with the wipe that
needed it. Nothing outside `view.ts` read it (no e2e selector, no test).

**2026-09-02 (review round 2) — the `single` class DID move to `main.ts`, as
Scope said.** Round 2 caught `app/dom.ts` still doing
`termsHost.classList.add('single')` at import time while Scope claimed the
mutation had moved. Honoured the plan rather than amending it: it is initial
paint state, not a property of holding the handle; `showSingle()` re-adds it on
the first paint (`view.ts:67`); and no test asserts it. Removing it also takes
one import-time DOM write out of a module ~30 jsdom suites import.

**2026-09-02 (review round 3) — `sameBanner`'s field list is guarded at the
type level.** A hand-written equality silently swallows any field added to the
type after it was written, and the failure mode is a banner that stops updating
with no test to say why. `BANNER_FIELDS` / `BANNER_ACTION_FIELDS` are
`Record<keyof T, true>` literals, so adding a field to `BannerData` or
`BannerActionData` fails to compile until the comparison is extended. Verified
by adding a probe field and watching `tsc` reject it.

**2026-09-02 (review round 2) — `setBanner` no longer notifies on a no-op.** It
rebuilds the banners record, so a new reference always reached `set()` and the
module's own "an action that changes nothing never notifies" contract did not
hold for it. Not hypothetical: `wireDaemonBanner` writes the same `daemonBuild`
on every control connect, and `renderUpdateAction` re-derives the same button on
every `update:progress` step. `sameBanner()` is shallow all the way down, which
is exactly as deep as `BannerData` goes.

**2026-09-02 (review round 1) — `MinimizedTray`'s chips are memoized;
`EmptyState` deliberately is not.** Both subscribe to `sessions`, which
`updateSession()` replaces on every `title` event — a path the deleted
`renderMinimizedTray()` was never wired to, so the naive port would have ADDED
work on the highest-rate event in the app, on the exact axis this migration
exists to improve. `TrayChip` is `memo`'d with primitive props, the same shape
Phase 1 gave `SessionItem`, and `test/dom/tray-render-scope.test.tsx` pins it
(4 of its 6 cases fail with the memo removed). It also records something
stronger than "only the affected chip rebuilds": a chip shows a session's NAME,
never its title, so a retitle rebuilds *nothing*.

`EmptyState` is left alone on purpose: a run is one Set build plus
`emptyStateModel()`, and it returns null whenever anything is visible — strictly
less work than the `renderEmptyState()` it replaces, which did the same two
things AND a `JSON.stringify` of the result, from roughly ten call sites
including every `switchTo()`. Memoizing three static nodes behind a derived-key
subscription would cost more code than the render it saves.

**2026-09-02 — `#banners` is the phase's one markup addition.** The three
banners are direct children of the `#app` grid and `banner.css` places two of
them by row, so a React root on a wrapper would have collapsed all three into
one row. `#banners { display: contents; }` keeps every existing row rule
literal. Render order inside it is undo-close, daemon, update — the order the
old `initBanners()` + `initUndoClose()` prepends produced.

**2026-09-02 — `VersionFooter` keeps its own `EventsOn` subscription rather than
a store slice.** `app/version-footer.ts`'s header explains why the footer must
not share the banner's handler: that one early-returns on `severity === 'match'`,
the case the footer must render. Keeping the subscription in the component
preserves that and adds no store surface; unlike the imperative module, it now
also unsubscribes on unmount, which Phase 6 will use.

## Progress

**2026-09-02** — Implemented. Store, `app/dom.ts`, `app/banners.ts`,
`app/undo-close.ts`, `app/version-footer.ts`, `app/view.ts`, `app/events.ts`,
`src/main.ts`, `index.html`, `layout.css`; new components `Banner`, `Banners`,
`BootState`, `Button`, `EmptyState`, `MinimizedTray`, `StatusBar`,
`VersionFooter`. Deleted `src/ui/banner.ts` and `src/ui/chip.ts`.

Tests: `update-banner`, `restart-hive`, `undo-close` and `boot-state` rewritten
to RTL `.tsx` (every case ported, counts unchanged at 8 / 8 / 15 / 4-minus-1);
`ui-banner.test.ts` became `banner.test.tsx` against the component;
`test/unit/version-footer.test.ts` split, its render half becoming
`test/dom/version-footer.test.tsx`; new `status-bar.test.tsx` and
`empty-state.test.tsx`. One case retired rather than ported — boot-state's "is
inert when the markup is absent": `setBootState` is now a pure store write and
cannot throw whether or not `BootState` is mounted. The minimized tray keeps its
coverage in `test/e2e/minimize.spec.ts` (hidden-class toggle, `data-sid` chips,
restore-by-click), unchanged.

**2026-09-02 — Verification.** `npm run typecheck` clean; `npm run ci` (biome)
clean; `scripts/ui-lint.sh --strict` 0 violations; `vite build` succeeds;
vitest `77 files / 834 tests` green; `scripts/test.sh go` green; Playwright e2e
**258 passed, 0 failed, 0 flaky** on a second confirming run (the first run
retried two pre-existing flakes, `theme.spec.ts:492` and `worktrees.spec.ts:387`,
both unrelated to this phase and both green on retry); `npm run test:e2e:real`
**22 passed**. No flake-baseline comparison was needed: nothing failed. `.plans/react-rewrite-flake-baseline.md`
is gitignored per-checkout scratch and is absent from this worktree, same as in
Phase 1.

## PR convergence ledger

_(opened 2026-09-02 for PR #318; `/hs-review-loop` appends one entry per iteration)_

- **2026-09-02 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 51083c23; threads_open: 0; action: fixes applied + push (1 IMPORTANT stood, so not convergence under the loop's "COMMENT with only MINOR remaining" bar); head_sha: 67dcf0a.
- **2026-09-02 iter 2** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: converged; MINOR sweep applied + push; head_sha: 59b49d8.
- **2026-09-02 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; CI all green; action: converged (re-review of the post-approval delta); last MINOR closed at the type level + push; head_sha: 26697ec.

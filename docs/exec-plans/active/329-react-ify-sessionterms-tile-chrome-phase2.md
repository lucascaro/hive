# React-ify SessionTerm's tile chrome — Phase 2: the overlays and the last of `src/ui/`

- **Master plan:** [329-react-ify-sessionterms-tile-chrome.md](329-react-ify-sessionterms-tile-chrome.md)
- **Spec:** [docs/product-specs/329-react-ify-sessionterms-tile-chrome.md](../../product-specs/329-react-ify-sessionterms-tile-chrome.md)
- **Issue:** —
- **Branch:** `feature/329-tile-chrome-phase2`
- **PR:** [#334](https://github.com/lucascaro/hive/pull/334)
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Summary

Port the dead-session overlay and the phase loading panel into the
`div.tile-overlays` mount Phase 1 created, port the last imperative
`iconButton()` caller out of `app/banners.ts`, relocate the sprite loader and
delete `src/ui/`. The whole-port acceptance criteria in the spec are the gate at
this phase.

## Approach

Master plan's **Approach**. Both overlays are rendered entirely by React —
element, class, `hidden`, `role` and content — inside the `display: contents`
mount, so the existing CSS and e2e selectors match unchanged.

### Files to change

- `src/app/session-term.ts` — delete the dead-overlay block (650–687), the
  phase-overlay block (689–705) and `_showPhaseOverlay()`'s `<li>` builder
  (1555–1580), plus the `deadOverlay` / `deadCloseBtn` / `deadDismissBtn` /
  `phaseOverlay` / `phaseStatus` / `phaseSteps` fields. `setDead()`,
  `setPhase()`, `_showPhaseOverlay()`, `_hidePhaseOverlay()`,
  `_revealAfterPhase()` and `revealAfterReplay()` keep their names, call sites
  and timing; their bodies become `tileChrome` writes. The `PHASE_REVEAL_CAP_MS`
  timer stays imperative — it is a timer, not rendering.
- `src/ui/icon.ts` → `src/lib/icon-sprite.ts`, keeping only `ensureSprite`,
  `ICON_NAMES` and `IconName`. `icon()`, `stateIcon()` and `updateStateIcon()`
  are deleted with their last callers.
- `src/ui/icons.svg` → `src/lib/icons.svg`; the sprite fetch path updated.
- `src/components/Icon.tsx` — import from `../lib/icon-sprite.js`.
- `src/app/banners.ts` — delete `wireCheckUpdatesButton()` and its
  `iconButton` import; `initBanners()` stops calling it.
- `src/components/Sidebar.tsx` — render the check-for-updates control with
  `IconButton`, `id="check-updates-btn"` preserved, calling `manualUpdateCheck`.
- `src/theme/components/tile-header.css` — add
  `.term-host .tile-overlays { display: contents; }`.
- `docs/design-docs/ui/components.md` — the paragraph describing `src/ui/` as
  the surviving imperative primitives is now false; rewrite it.
- `FRONTEND.md`, `DESIGN.md` — update the frontend-structure paragraphs:
  `src/ui/` is gone and `session-term.ts` no longer renders.
- `.changesets/react-ify-sessionterm-chrome.md` — `type: changed`,
  `bump: patch`, the one changeset for the whole port.

### New files

- `src/components/TileOverlays.tsx` — `DeadOverlay` and `PhaseOverlay`.
  `PhaseOverlay` renders `lib/phase-steps.ts`'s `phasePanel()` result directly.

### Tests

- `test/dom/tile-dead-overlay.test.tsx`
  - `shows the failure reason` — `role="alertdialog"`, `.dead-subtitle` carries
    the reason text.
  - `close kills, dismiss records` — `KillSession(id, true)` /
    `addDismissedDead(id)` + `refocusActiveTerm()`.
  - `does not steal focus while a modal is open` — the documented guard.
  - `survives destroy while shown` — `destroy()` with the overlay up unmounts
    cleanly and logs no React warning.
- `test/dom/tile-phase-overlay.test.tsx`
  - `renders phasePanel steps` — `li.phase-step[data-state]`, a check icon for
    `done`, a starting state icon for `active`, no mark for `todo`.
  - `fades before hiding` — `revealAfterReplay()` puts `.fading` on the element
    for a frame before `hidden`.
- `test/e2e/tile-chrome-stability.spec.ts` (new)
  - `terminal hosts are never recreated` — across a view switch, a reorder, a
    minimize/restore and a theme switch,
    `window.__hive_state.terms.get(id).host` is the same node and
    `term.buffer.active` is unchanged.
- `test/dom/check-updates-button.test.ts` — rewritten against the React path,
  same `#check-updates-btn`.
- Deleted: `test/dom/ui-icon.test.ts`, `test/dom/ui-icon-button.test.ts` (their
  subjects are gone; `ui-state-icon.test.tsx` already covers React `StateIcon`).

## Invariants

Every phase honours the master plan's **Invariants** section.

## Verification

Per the master plan's **Verification** block. **The spec's own
`## Success criteria` are the gate at this phase** — this is where the
whole-port checklist must pass.

## Success criteria

- Both overlays render from `TileOverlays.tsx`; `session-term.ts` creates no
  DOM but `host`, `.tile-header`, `.term-body` and `.tile-overlays`.
- `src/ui/` no longer exists; `rg` finds no orphaned exports.
- `app/banners.ts` no longer imports `iconButton`.
- `e2e` and `e2e-real` pass unmodified, and the new
  `tile-chrome-stability.spec.ts` passes.
- `components.md`, `FRONTEND.md` and `DESIGN.md` describe the frontend as it
  now is.
- A changeset is added for the whole port.

## Decision log

- **2026-09-03** — `.fading` is NOT reproduced, and `phaseFading` was
  dropped from the `tileChrome` slice. Why: the master plan's open
  question turned out to be moot. `revealAfterReplay()` adds `.fading`
  and then calls `_hidePhaseOverlay()`, which sets `hidden` and removes
  the class again — both in the same tick, so the browser never painted a
  faded frame and the CSS transition never ran. Reproducing the class in
  React would either change nothing or, if split across two commits,
  introduce a fade that does not exist today. The port keeps the net
  observable state (hidden, no class) and the planned
  `fades before hiding` test was not written, because it would assert
  behaviour the imperative code never had.
- **2026-09-03** — `phasePanel: PhasePanel | null` added to the slice
  (the plan's field list did not have it). Why: the panel outlives the
  phase it describes — `_revealAfterPhase()` holds it past PhaseReady,
  where `phasePanel()` returns null — so deriving it in the component
  would blank the steps on the ready edge and leave a bare spinner.
  `_showPhaseOverlay()` already computed the model to decide whether to
  show at all, so storing it costs nothing.
- **2026-09-03** — The dead card's focus grab moved into
  `TileOverlays.tsx` as an effect instead of staying at the `setDead()`
  call site. Why: the button does not exist until React commits the
  store write, so the old `setTimeout(…, 0)` would race the commit. The
  effect keeps the same deferral and the same `anyModalOpen()` guard, and
  its cleanup covers the tile being destroyed while the overlay is up —
  which used to focus a detached button.
- **2026-09-03** — `src/app/modals/project-editor.ts` was a third
  imperative `icon()` caller the plan's file list missed
  (`newProjectBtn.replaceChildren(icon('plus'))`), and `src/ui/` could
  not be deleted without it. Handled with the check-for-updates control
  in one place: `SidebarHeaderControls` in `Sidebar.tsx` portals the plus
  icon into `#new-project-btn` and the new IconButton into the header.
  index.html keeps owning the button element itself, because
  `initProjectEditor()` wires its click, the launcher uses it as a focus
  fallback and the dom tests reach it by id.
- **2026-09-03** — `ui-icon`, `ui-icon-button` and `ui-state-icon` tests
  were REWRITTEN against the React primitives rather than deleted as the
  plan said. Why: the plan assumed `ui-state-icon.test.tsx` already
  covered React `StateIcon`, but no such file existed — the suite had
  only the imperative `.ts` versions, and nothing anywhere rendered
  `<Icon>`, `<IconButton>` or `<StateIcon>`. Deleting them would have
  dropped the "an icon-only button without a label throws" and "every
  declared name has a sprite symbol" contracts on the floor.
- **2026-09-03** — The two planned overlay test files are one,
  `test/dom/tile-overlays.test.tsx`: they share the scaffold entirely and
  neither is large enough to earn a second copy of it. The imperative
  half (which edge raises the panel, which drops it) stays in
  `session-phase.test.ts`, repointed from the deleted DOM fields to the
  store.
- **2026-09-03** — `scripts/ui-lint.sh` drops `$FE/src/ui` from its
  default glyph targets. `find` on the missing directory was being
  swallowed by `2>/dev/null`, so the rule would have gone on passing
  while silently scanning one directory fewer.
## Progress

- **2026-09-03** — Scaffolded from the approved plan-first plan.
- **2026-09-03** — Review loop converged in 2 iterations. Iteration 1's
  BLOCKING finding was in the bookkeeping, not the port: both spec-305
  files were advanced to `stage: DONE` without the `shipped:` date
  `scripts/regen-generated.py` requires, failing `verify-generated`.
  Autofix also caught two of this phase's own tests being unable to
  fail — `survives an unmount while shown` (React nulls the ref, so a
  leaked timer runs `undefined?.focus()` silently) and the new e2e
  spec's host tagging (racing a deferred attach, whose replay
  `term.reset()`s the buffer — which reads back as exactly the phantom
  remount the spec exists to detect). Iteration 2 found no blocking
  defect; its one IMPORTANT finding is closed above.
- **2026-09-03** — Implemented on `feature/329-tile-chrome-phase2`.
  Green on every layer: `npm run typecheck`, `biome ci .` (exit 0),
  `scripts/ui-lint.sh --strict` (0 violations), `scripts/test.sh unit dom
  e2e` (571 dom/unit, 262 e2e passed / 31 skipped), `npm run
  test:e2e:real` (22 passed / 2 skipped, all with `CI=1`), and `go test
  ./...` after a `npm run build` (the embedded-assets tests need a real
  `dist/`, not ci-bootstrap's placeholder). No e2e or e2e-real spec was
  edited to accommodate the port; the one change under `test/e2e/` is a
  comment in `sidebar-header-actions.spec.ts` that named
  `initBanners()`.

## Open questions

- The `.fading` → `hidden` transition timing (master plan's **Open questions**).
- `display: contents` fallback (master plan's **Open questions**).

- **2026-09-03 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 4606dfb140c957b8aac6422943cce123ee955b756c10b9979a0abba87dfb686d; threads_open: 0; action: autofix+push; head_sha: 159cd7e.
- **2026-09-03 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: abf26b9accb3c87992e6e2f78850b370cb3db26664763a86e332fe9e57ecb02b; threads_open: 0; action: stop (converged; the one IMPORTANT finding — `setDead()`'s reason-merge branch untested against a real `SessionTerm` — was closed by hand rather than deferred, and mutation-checked: dropping the `deadReason` write fails it); head_sha: 86e017a.

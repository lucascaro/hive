# React-ify SessionTerm's tile chrome — Phase 2: the overlays and the last of `src/ui/`

- **Master plan:** [329-react-ify-sessionterms-tile-chrome.md](329-react-ify-sessionterms-tile-chrome.md)
- **Spec:** [docs/product-specs/329-react-ify-sessionterms-tile-chrome.md](../../product-specs/329-react-ify-sessionterms-tile-chrome.md)
- **Issue:** —
- **Branch:** `feature/329-tile-chrome-phase2`
- **PR:** —
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

## Progress

- **2026-09-03** — Scaffolded from the approved plan-first plan.

## Open questions

- The `.fading` → `hidden` transition timing (master plan's **Open questions**).
- `display: contents` fallback (master plan's **Open questions**).

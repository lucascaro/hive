# Minimized project chips fill the tray with a right-aligned restore

- **Spec:** [docs/product-specs/255-minimized-project-chips-fill-the-tray.md](../../product-specs/255-minimized-project-chips-fill-the-tray.md)
- **Issue:** —
- **PR:** #300
- **Branch:** feature/255-minimized-project-chip-layout
- **Status:** active

## Summary

Two scoped CSS overrides in the `#minimized-projects` tray so project chips
span the sidebar, the restore `+` right-aligns, and the whole row is one
restore target. No component or TypeScript change.

## Research

- `src/theme/components/chip.css` — `.hv-chip` carries `max-width: 240px`,
  sized for the horizontal `#minimized-tray` session pills. `.hv-chip__open`
  is content-sized (`flex: 0 1 auto`), so the restore `+` sits right after
  the label.
- `src/style.css:146` — `#minimized-projects` is a `flex-direction: column`
  list; `align-items: stretch` already gives chips the full tray width up to
  the 240px cap.
- `src/app/sidebar.ts:104` `renderMinimizedProjects` / `renderProjectChip` —
  already passes `onClick: () => deps.restoreProject(p.id)`. The handler
  lives on `.hv-chip__open`, so "click anywhere restores" is a *layout* gap,
  not a missing listener: the open button just doesn't cover the row.

## Approach

Override the two chip properties inside the `#minimized-projects` scope
rather than changing `chip.css`. The chip component is shared with the
session tray, where the 240px cap is correct — an unscoped change would
turn that horizontal row of pills into full-width bars. The id scope also
wins on specificity regardless of stylesheet order.

Making `.hv-chip__open` `flex: 1` is what delivers the click-anywhere
requirement: the existing restore handler already covers every pixel the
button occupies, so growing the button to fill the row is a smaller and
safer fix than adding a second listener on the chip root (which would have
to reason about `stopPropagation` from both child buttons).

### Files to change

- `cmd/hivegui/frontend/src/style.css` — add `#minimized-projects .hv-chip { max-width: none; }`
  and `#minimized-projects .hv-chip__open { flex: 1; text-align: left; }`.
- `cmd/hivegui/frontend/test/e2e/minimize-project.spec.ts` — add the layout regression test.

### New files

None.

### Tests

- `test/e2e/minimize-project.spec.ts` › `a project chip spans the tray,
  right-aligns +, and restores on any click` — minimizes the first project,
  then asserts (a) chip width ≈ tray content width, (b) the restore button's
  right edge is within 12px of the chip's right edge, (c) a click 12px left
  of the `+` restores the project. Must be e2e: jsdom applies no CSS, so a
  unit test cannot see a `max-width` or a flex distribution.

## Decision log

- **2026-08-30** — Scope the overrides to `#minimized-projects` instead of
  editing `.hv-chip`. Why: the same chip is the minimized-*sessions* pill,
  where the 240px cap is the intended design.
- **2026-08-30** — Deliver click-anywhere via `flex: 1` on the existing open
  button rather than a new root listener. Why: no new event plumbing, and
  the child restore button already calls `stopPropagation`.

## Progress

- **2026-08-30** — Implemented; `npm test` (676), `npx playwright test` (206),
  `biome ci .` and `npm run typecheck` all green.

- **2026-08-30** — PR #300 opened; stage = REVIEW.

## PR convergence ledger

<Append-only. One line per /hs-review-loop iteration.>

## Open questions

None.

# Activity grid: show the plan's tasks, not just the tool feed

- **Spec:** [docs/product-specs/428-activity-grid-tasks-first.md](../../product-specs/428-activity-grid-tasks-first.md)
- **Issue:** #428
- **Status:** active

## Summary

The activity grid's tile renders the plan as bare pips and gives the entire
body to the tool feed. Make the tile's body the plan's task list — readable
step text, per-step status, the current step scrolled into view — and let the
feed take whatever vertical space is left over, down to none. Rendering only:
no wire, daemon or agent-tier change.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/components/activity/ActivityTile.tsx:38-49` — the
  pip `<ol>`; each `<li>` carries the step text in a `title` attribute only.
  `:52-71` — the feed `<ol class="hv-activity__timeline">`, capped at
  `FEED_MAX = 40` and reversed newest-first. This file is the whole change.
- `.../components/activity/ActivityPanel.tsx:79-155` — `PlanStep`: the step
  markup the tile will mirror (`li.hv-activity__step[data-status]` > mark +
  text). Its chevron, tool pill and nested `CallRow` list are panel-only.
- `.../components/activity/shared.tsx:33-56` — `useActivityView` gives
  `{info, data, load, now, empty, stale, staleText}`; `:80-118` `CallRow`.
  Both already used by the tile; no change needed.
- `.../lib/activity.ts:26-33` — `PlanItem {id?, text, status, tools?}`. The
  tile reads `data.plan` directly; `groupTimeline` (panel-only) is not needed.
- `.../src/theme/components/activity.css:75-116` — `.hv-activity__plan`,
  `__step`, `__step-row`, `__mark`, `__step-text` (ellipsis, nowrap) — reused
  as-is. `:119-128` pips. `:131` `.hv-activity__timeline { flex: 1 }` — the
  rule that currently gives the feed the whole body. `:153-163` the
  `[data-stale]` desaturation, which already covers `__mark` and the pips.
- `.../test/e2e/activity-grid.spec.ts:33-57` — `boot()` seeds every session
  with a 2-item plan and one event via `window.__hive.setActivity`; `tiles()`
  (`:62-80`) already counts `.hv-activity__pip` per tile with
  `elementFromPoint`. The layout assertions go here.
- `.../test/unit/activity.test.ts` — `lib/` unit layer (pure modules only).
- `.../test/e2e/wails-mock.ts:1339,1356` — `setSessionPlan`, `emitActivity`.

### Constraints / dependencies

- `scripts/ui-lint.sh --strict` in CI: no raw hex, no px `font-size` or
  `border-radius`, no icon-shaped Unicode under `src/app|components|theme`.
- The activity grid takes no keystrokes (spec 416 phase 4) and the renderer
  root cancels `mousedown` (`shared.tsx:60`) so the terminal keeps focus. The
  task list must scroll by wheel without becoming focusable or stealing focus.
- vitest is CSS-blind: every claim about which element gets the leftover
  height must be proven in Playwright with `elementFromPoint` / computed
  heights, not by reasoning (repo rule, and spec 416's own criteria).
- The tile has no `dom` test today; the `dom` layer covers sidebar/visibility.

### Prior lessons

`brain-search 'activity grid tile plan tasks playwright css'` — no prior
lessons matched.

### Conventions card

```
build:  ./build.sh                                  # macOS .app (GUI + daemon)
test:   scripts/test.sh [go unit dom e2e]
e2e:    cd cmd/hivegui/frontend && CI=1 npx playwright test test/e2e/<spec>
lint:   scripts/ui-lint.sh --strict ; npx biome ci .
types:  npm run typecheck   (fresh worktree: ./scripts/ci-bootstrap.sh first)
```

- Frontend tests live under `cmd/hivegui/frontend/test/{unit,dom,e2e}`; `unit`
  is for pure `lib/` modules, so a component-layout change is tested in `e2e`.
- "Boil the lake": ship the behaviour change with the test that would have
  caught its regression, in the same PR.
- UI changes follow `docs/design-docs/ui/` — existing tokens only, `Icon`
  sprite for glyphs, no raw hex.
- User-visible change ⇒ a `.changesets/` entry is required (CI gate).
## Approach

The tile's body becomes a two-part flex column: the plan's task list on top,
the tool feed below, with the feed taking only the space the task list leaves.

`ActivityTile.tsx` renders, in order: the existing head (pips + stale text),
then an `<ol class="hv-activity__plan">` of the plan's items, then the
existing feed `<ol class="hv-activity__timeline">`. Each task reuses the
panel's step markup and CSS — `li.hv-activity__step[data-status]` holding the
`__mark` (check for done, ring for active) and `__step-text` — minus the
panel's chevron, tool pill and nested calls, which are disclosure affordances
the tile cannot offer (the activity grid takes no keystrokes and the renderer
root cancels mousedown).

The split is CSS-only, no measurement and no breakpoint:

```css
.hv-activity--tile .hv-activity__plan     { flex: 0 1 auto; min-height: 0; max-height: 100%; overflow-y: auto; margin-top: 0; position: relative; }
.hv-activity--tile .hv-activity__timeline { flex: 1 1 0;    min-height: 0; overflow: hidden; }
.hv-activity--tile .hv-activity__pips     { max-height: calc(2 * 8px + 3px); overflow: hidden; }  /* two pip rows */
.hv-activity--tile .hv-activity__step-row { cursor: default; }                     /* inert here */
```

The feed's `flex-basis: 0` means it contributes nothing to the column's
content height: it only ever grows into free space. A short plan leaves free
space and the feed fills it; a plan taller than the tile leaves none and the
feed collapses to zero height while the task list scrolls. `overflow: hidden`
on the feed keeps a partially-fitting row from bleeding past the tile.
`min-height: 0` on both is what lets them actually shrink inside a flex
column. The pip strip needs the cap because `.hv-activity__head` is
`flex-shrink: 0` and `.hv-activity__pips` is `flex-wrap: wrap`
(`activity.css:44-50,120`): a 40-item plan would otherwise wrap to three or
four rows and eat a third of a small tile before the task list got anything.
No plan at all ⇒ no `__plan` element ⇒ the feed fills the tile, which
is today's behaviour unchanged.

Each task keeps the panel's two-level shape — `li.hv-activity__step[data-status]`
wrapping a `div.hv-activity__step-row` — because `activity.css:85-86` styles the
current and done steps through a **direct-child** `> .hv-activity__step-row`
selector, and `__step-row` is also what supplies the row's `display: flex` and
gap. Dropping the wrapper would silently lose the "current step emphasized"
part of the spec while every presence-based test still passed.

The current step is scrolled into view by a `useEffect` keyed on the **id** of
the active item (`item.id ?? index`), which sets `scrollTop` on the task list
directly:

```ts
const list = planRef.current, row = activeRef.current;
if (list && row) {
  const top = row.offsetTop, bottom = top + row.offsetHeight;
  if (top < list.scrollTop) list.scrollTop = top;
  else if (bottom > list.scrollTop + list.clientHeight)
    list.scrollTop = bottom - list.clientHeight;
}
```

`position: relative` on the list is load-bearing, not decoration: `offsetTop`
is measured from the row's **offsetParent**, and with a static `<ol>` that
would be `.activity-tile` (`activity.css:14-23`, absolutely positioned), not
the scroll container — every branch would then be off by the head's height and
park the active row just above the visible window. Making the list the
offsetParent is what makes the four lines above correct as written.

Not `scrollIntoView`: that walks **every** scrollable ancestor, and both
`#terms.grid` and `#terms.grid .term-host` are `overflow: hidden`
(`src/theme/layout.css:100-125`) — programmatically scrollable, with no
scrollbar and nothing to scroll them back, so one mistimed call could leave
the whole tile grid permanently offset. Setting `scrollTop` touches one
element, is a no-op under jsdom (`offsetTop` is 0 there, so the dom tests need
no stub), and reproduces `block: 'nearest'` semantics in four lines. Keying on
identity rather than on every render means a wheel-scroll the user performs is
not yanked back while they read; it re-scrolls only when the agent actually
moves to another step.

**Hook ordering.** `ActivityTile.tsx:19` early-returns `if (!view) return null`
before any hook runs today. The new `useRef`s and `useEffect` are hoisted
**above** that return, or React throws on the no-view path — a path the e2e
tests never take, since they always seed a session.

**Why not the alternatives.** A container query on tile height (`@container
(min-height: X)`) would need a magic threshold tuned per density and per grid
size, and would hide the feed on a tall tile with a 1-item plan. Measuring in
JS re-runs on every resize and re-introduces the layout thrash spec 416
avoided. The flex rule above expresses "tasks first, feed gets the remainder"
directly, in two declarations.

### Files to change

1. `cmd/hivegui/frontend/src/components/activity/ActivityTile.tsx` — render
   the plan as an `<ol class="hv-activity__plan">` of step rows between the
   head and the feed; keep the pip strip in the head as the whole-plan shape;
   add the active-step ref + `useEffect` auto-scroll. `FEED_MAX` and the
   empty/stale paths are untouched.
2. `cmd/hivegui/frontend/src/theme/components/activity.css` — add the four
   `.hv-activity--tile` scoped rules above (the panel keeps its own
   `flex-shrink: 0; max-height: 50%`), plus the task-row type size for the
   tile (`font-size: var(--text-xs)`) so a step reads at tile density.
3. `cmd/hivegui/frontend/test/e2e/activity-grid.spec.ts` — new layout
   assertions (below). `boot()`'s 2-item seed stays; a long-plan case seeds
   its own via `window.__hive.setActivity`.
4. `cmd/hivegui/frontend/test/dom/activity-panel.test.tsx` — the existing
   `ActivityTile` cases ('draws one pip per plan item and the feed', :163;
   the empty-state case, :272) are updated for the new markup, and one case
   is added asserting the tile renders `.hv-activity__step-text` per plan
   item. Markup only — the layout claims stay in Playwright.
5. `docs/design-docs/agent-activity.md:280` — the Rendering bullet "Grid tile
   — plan shape as pips, body given to the live tool feed" becomes the new
   split, with a dated note that it supersedes the phase-4 shape.
6. `README.md:41` — "plan pips and the live tool feed" becomes the task list
   plus the feed.
7. `docs/product-specs/416-agent-activity-view.md` — a dated superseded note
   on the "Activity grid" bullet under Desired behavior, pointing at 428
   (same convention as the two existing corrections in that spec).
8. `docs/product-specs/428-activity-grid-tasks-first.md` — fill `## Success
   criteria` from the criteria below, so `/hs-merge-gate` has something to
   validate against.

### New files

- `.changesets/428-activity-grid-tasks-first.md` — user-visible change; the CI
  changeset gate requires it.

### Tests

All in `cmd/hivegui/frontend/test/e2e/activity-grid.spec.ts` (Playwright vs
the Wails mock — the claims are about computed layout, which vitest cannot
answer):

1. `test('the tile shows the plan\'s task text, not just pips')` — after
   toggling into the activity grid, every in-grid tile contains
   `.hv-activity__step-text` with the seeded texts `one` / `two`, and the
   active step's row is the one with `[data-status="active"]`. Fails today:
   the tile renders no `__step-text` at all.
2. `test('a plan that overflows the tile leaves the feed no height')` — seed
   one session with a 40-item plan, re-enter the grid, assert that tile's
   `.hv-activity__timeline` has `clientHeight === 0` while
   `.hv-activity__plan` has `scrollHeight > clientHeight` (it scrolls) and
   `elementFromPoint` at the body's centre still resolves inside
   `.activity-tile`. The same case asserts `.hv-activity__head`'s
   `clientHeight <= 24`, so an uncapped wrapping pip strip fails it. Fails on
   a fixed 50/50 split or on today's `flex: 1` feed.
3. `test('a short plan still leaves room for the feed')` — with `boot()`'s
   2-item plan, the same tile's `.hv-activity__timeline` has
   `clientHeight > 0` and at least one `.hv-activity__call` is rendered.
   Guards the over-correction where tasks swallow the tile.
4. `test('the current step is scrolled into view in a long plan')` — with the
   40-item plan whose `active` item is #35, the active row's
   `getBoundingClientRect()` lies within the `.hv-activity__plan` element's
   rect **fully** (row top >= list top and row bottom <= list bottom, so a
   half-clipped row fails too), **and** `#terms.grid` and the owning `.term-host` both still have
   `scrollTop === 0 && scrollLeft === 0`. Fails without the effect, and fails
   again if the effect ever goes back to walking scrollable ancestors.
5. `test('the current step reads as current')` — the active step row's
   computed `color` differs from a done row's. Fails if the tile drops the
   `div.hv-activity__step-row` wrapper that `activity.css:85-86` selects on,
   which every presence-based assertion above would happily allow.
6. `test('wheel-scrolling the task list steals no focus and no keystroke')` —
   wheel over the task list, then type; `window.__hive.stdinText()` stays
   empty and `document.activeElement` is not inside the tile. Extends the existing
   no-keystroke claim over the newly scrollable region.

The existing `activity-grid.spec.ts` cases (tiles swap and swap back, the
terminal keeps its width underneath, no keystroke reaches a PTY) are kept
as-is and must still pass — they are the regression net for the swap itself.

Checked against the mock before writing these: `addSession`, `setActivity`,
`resetStdin` and `stdinText(id?)` all exist (`test/e2e/wails-mock.ts:1272`,
`:1353`, `:1368`; `test/e2e/hive-global.d.ts:32-60`) — the stdin accessor is
`stdinText()`, not `stdin`.

### Verification

```bash
cd cmd/hivegui/frontend
CI=1 npx playwright test test/e2e/activity-grid.spec.ts      # 6 new + existing
CI=1 npx playwright test test/e2e/activity-panel.spec.ts     # panel unchanged
npm run typecheck
npx biome ci .
cd ../../.. && scripts/ui-lint.sh --strict
scripts/test.sh unit dom          # dom: the updated ActivityTile cases
```

Each of the six new tests fails on `main` (1, 4, 5 have no such element;
2, 3 assert the opposite of today's `flex: 1` feed; 6 exercises a region that
does not scroll today), so none is vacuous.

### Open questions / risks

- **Row markup drift.** The tile now copies the panel's mark + text row
  (`ActivityPanel.tsx:94-110`) rather than sharing it. Extracting the row into
  `shared.tsx` and having the panel compose its chevron and pill around it
  would keep the two from drifting; it is a small refactor of a file this PR
  already touches, and it is **surfaced, not taken** — say the word and it
  goes in.
- **Duplicate plan announcement.** The pip `<ol>` already carries
  `aria-label="Plan: n of m done"` (`ActivityTile.tsx:38`). With a real task
  list beside it the pips become decorative, so they get `aria-hidden` in the
  tile and the list keeps the label.
- **A single very long task text** wraps or ellipsises: `__step-text` is
  `white-space: nowrap; text-overflow: ellipsis` today, so a long task shows
  one clipped line. Accepted — it matches the panel, and the full text stays
  in the `title`.
- **The feed reaching exactly zero.** If `min-height: 0` were missed on either
  item, flexbox would floor the feed at its min-content height and a long plan
  would lose rows to a feed nobody can read. Test 2 is exactly that guard.
- **Pips become redundant on a short plan** (the pip strip and the task list
  say the same thing). Kept deliberately per the clarifying round: the strip
  is the whole-plan shape when the list is scrolled away from its ends.

### Blast radius checked and deliberately not changed

- `site/features.json:4` — the shipped blurb says a grid tile becomes "its
  session's plan and tool feed", which stays true under the new split. No edit.
- `test/e2e/activity-panel.spec.ts`, `test/e2e/keymap-activity.spec.ts` — panel
  and keymap only; the tile-scoped CSS cannot reach them.
- The CSS override is specificity-safe: `.hv-activity--tile .hv-activity__plan`
  (0,2,0) beats `.hv-activity__plan { flex-shrink: 0; max-height: 50% }`
  (`activity.css:76`, 0,1,0), and likewise for `__timeline` (`:131`).

## Second opinion

Two rounds, one `general-purpose` reviewer. (A first dispatch died with its
session before returning a verdict; it was re-dispatched, so no metric row
exists for it.)

- **Round 1 — `revise`, confidence 8, 5 must-fix, 5 applied.** Conditional
  hook after `ActivityTile.tsx:19`'s early return; `scrollIntoView` walking
  `overflow: hidden` ancestors (`layout.css:100-125`) with no way to scroll
  them back; jsdom lacking `scrollIntoView` while a new dom case renders an
  active step; the uncapped wrapping pip strip eating a third of a small tile;
  and the `div.hv-activity__step-row` wrapper that `activity.css:85-86`
  selects on as a direct child — dropping it would have lost "current step
  emphasized" while every presence-based test still passed. The `scrollTop`
  rewrite resolved two of the five at once. Two nice-to-haves taken
  (tile-scoped `cursor: default`, `aria-hidden` on the tile's pips).
- **Round 2 — `revise`, confidence 8, 1 must-fix, 1 applied.** `row.offsetTop`
  is measured from the offsetParent, which for a static `<ol>` is
  `.activity-tile`, not the scroll container — every branch off by the head's
  height. Fixed with `position: relative` on the tile's plan list. Both
  nice-to-haves taken (`calc()` for the pip cap, full-containment in test 4).

The round budget is spent; the plan was presented after round 2 without a
third pass, per the loop's rule.

## Decision log

- **2026-09-17** — Feed shown by CSS leftover height, not a container query or
  JS measurement. Why: operator's call at clarifying round A; a threshold
  would need tuning per density and per grid size, and would hide the feed on
  a tall tile with a one-item plan.
- **2026-09-17** — Task list scrolls and auto-scrolls to the current step,
  rather than windowing around it. Why: operator's call at clarifying round A.
- **2026-09-17** — Pip strip kept as a header summary. Why: operator's call;
  it is the whole-plan shape when the list is scrolled away from its ends.
  Capped to two rows so it cannot crowd out the list it summarizes.
- **2026-09-17** — `scrollTop` set directly instead of `scrollIntoView`. Why:
  reviewer round 1; `scrollIntoView` walks every scrollable ancestor and the
  grid's are `overflow: hidden` with no scrollbar to undo an offset.
- **2026-09-17** — Tile duplicates the panel's row markup rather than sharing
  it. Why: operator chose "approve as drafted" at the plan stop; a
  cross-reference comment in both files is the anti-drift substitute.

## Progress

- **2026-09-17** — Spec written, triaged `enhancement / S / P2`, researched,
  planned, approved at the plan stop.
- **2026-09-17** — Implemented. Two test-seam corrections found while running:
  `__hive.setActivity` only seeds the snapshot fetched at mount, and a
  `full: true` frame is dropped unless the client requested it
  (`store/activity.ts:73-78`), so the long-plan cases seed with a **delta**
  carrying the whole plan — which is what the daemon sends on a plan update.
  The pre-existing 416 pip-count assertion was a latent race (the tile paints
  before its snapshot lands) that the new tests' timing exposed; it now polls.

- **2026-09-17** — `.hv-activity__call` rows stay in the DOM behind a
  zero-height `overflow: hidden` feed, so the collapse is asserted on
  `clientHeight`, not on row count.

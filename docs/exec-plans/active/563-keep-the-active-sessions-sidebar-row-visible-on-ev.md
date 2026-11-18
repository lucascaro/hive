# Keep the active session's sidebar row visible on every switch

- **Spec:** [docs/product-specs/563-keep-the-active-sessions-sidebar-row-visible-on-ev.md](../../product-specs/563-keep-the-active-sessions-sidebar-row-visible-on-ev.md)
- **Issue:** #563
- **Status:** active
- **PR:** #565
- **Branch:** feature/563-sidebar-follows-active-session

## Summary

Make the sidebar follow the active session: scroll its row into view (minimally, clearing the sticky headers) and, when it sits in a collapsed project card or worktree group, show that one row without expanding the container.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/components/Sidebar.tsx` — `Sidebar()` (~L557) subscribes `activeId`, `collapsed`, `minimized`; `ProjectItem()` (~L473) already filters rows with the #407 rule "minimized rows hidden except the active one" (L481-486) — the prior art for "active row survives a hide". `renderRows()` (~L330-470) builds plain rows and `WorktreeGroup` runs; `item()` sets `selected={s.id === o.activeId}` (L375).
- `cmd/hivegui/frontend/src/components/ProjectCard.tsx` — header + `.hv-project-card__body > ul.hv-project-card__rows` (L204-211). Collapse is **CSS-only**: rows stay mounted, body goes `grid-template-rows: 0fr; visibility: hidden` (`theme/components/project-card.css:155-163`). So an active row inside a collapsed card is in the DOM but invisible — any "show it" must move it outside the clipped body or change the CSS.
- `cmd/hivegui/frontend/src/components/WorktreeGroup.tsx` — collapse is **local React state** (L59-66, deliberately not the persisted `collapsed` set); same clipped-body pattern (`theme/components/sidebar.css:228-265`). Only the group knows whether it is collapsed.
- `cmd/hivegui/frontend/src/lib/use-collapse.ts` — the clip is applied only while collapsed/animating, because a permanent `overflow:hidden` breaks sticky headers.
- Scroll container: `ul#projects` (`index.html:100`, `theme/components/sidebar.css:59-69`). Project header is `position: sticky; top: 0` with height `--sidebar-project-header-h: 26px`; worktree group header sticks at `top: var(--sidebar-project-header-h)` (`sidebar.css:110-115`) with **no height token**. A plain `scrollIntoView({block:'nearest'})` would park the row under these sticky headers — "fully visible" must clear them.
- `cmd/hivegui/frontend/src/components/SessionRow.tsx:209-214` — row is `li.hv-session-row[data-sid][data-selected]`.
- `cmd/hivegui/frontend/src/app/selectors.ts:30` `orderedSessions()` — nav (⌘↑/↓, ⌘1-9, palette) walks every session, including those in collapsed projects/groups. That is exactly how the user lands on an invisible row today.
- `cmd/hivegui/frontend/src/app/view.ts:437-450` `restoreSession` — already un-minimizes the holding project (⌘B, nav history).
- No `scrollIntoView` anywhere in the sidebar today.
- Tests: DOM harness `test/dom/sidebar-harness.tsx` (+ `sidebar-group.test.tsx`, `sidebar-render-scope.test.tsx`); e2e `test/e2e/sidebar-sticky.spec.ts`, `sidebar-collapse-animation.spec.ts`, fixture `test/e2e/fixtures/seed-worktree-group.ts` (`seedScrollableGroup`).

### Constraints / edge cases

- A newly spawned session can become `activeId` before its row exists (session list arrives separately) — the reveal must wait for the row, not fire once on the id change and miss.
- The reveal must fire only on an active-session change; re-running on every store change would yank the list back after the user scrolls away (spec non-goal).
- Sidebar hidden (⌘S): no reveal (non-goal); the row has no layout box then.
- A collapsed container must not duplicate the active row in the DOM (a hidden copy in the clipped body would win `querySelector('[data-sid]')`).
- Grid view: clicking a tile changes `activeId` — same path.

### Prior lessons

- `hiding-rows-must-reach-keyboard-slot-math` (brain): when a list paints a subset, every keyboard gesture walking the same source (nav, reorder, numeric hints, drop targets) must agree on what is a slot. Here nav already visits collapsed sessions (that is the bug's trigger); reorder/drag next to a lone peeked row must be checked in the plan.

### Conventions card

- Test: `scripts/test.sh` (layers `go · unit · dom · e2e`); frontend `npm run typecheck` (needs `./scripts/ci-bootstrap.sh` in a fresh worktree), `npx biome ci .`, `scripts/ui-lint.sh`. Local Playwright with `CI=1`.
- TDD: every behaviour change ships with its test. vitest/jsdom is CSS-blind and has no layout — scroll/visibility claims need Playwright (`elementFromPoint`).
- UI tokens/rules: `docs/design-docs/ui/` + `scripts/ui-lint.sh`; no hard-coded colours/sizes outside tokens.
- User-visible change: add `.changesets/<slug>.md` (`type: added|changed`, `bump: minor|patch`); never edit `CHANGELOG.md`. A `bump: minor` feature also adds `site/features.json` entry with `since: "Unreleased"`.
- Frontend-only change: no `DaemonContract` bump, no wire change.

## Approach

Two independent mechanisms, both frontend-only (no wire/daemon change, no DaemonContract bump).

### A. Reveal-scroll on active change — `useRevealActiveRow`

A hook called from `Sidebar()` (the component already subscribes `activeId`).

- `pending = useRef<string|null>(null)`. A `useLayoutEffect([activeId])` sets `pending.current = activeId`.
- A second `useLayoutEffect` with **no deps** (runs after every Sidebar render, which happens whenever sessions / collapsed / minimized / activeId change) does: if `pending.current` is set, look up `#projects .hv-session-row[data-sid="<CSS.escape(id)>"]`. Not found → leave pending (the row has not rendered yet: a spawned session, a project being restored). Found → clear pending, then `revealRow(row)`.
- Effect order: the "set pending" effect is declared before the "consume" effect, so on the render that changes `activeId` both run in order and the reveal happens in the same commit.
- Why a pending ref rather than `deps: [activeId, sessions]`: re-running the scroll on every sessions/status update would pull the list back after the user scrolled away (spec non-goal). Pending is consumed exactly once per active change. Why not `[activeId]` alone: the row can arrive in a later render than the id (spawned session, restoring a minimized project), and the reveal would miss it.
- Sidebar hidden (⌘S): the row is found but has no layout box; `scrollIntoView` on a `display:none` subtree is a no-op in every engine, and pending is cleared, so re-showing the sidebar later does not yank the list (non-goal).

`revealRow(row)` lives in new `src/lib/reveal-row.ts`. It sets `#projects.scrollTop` by hand, which is how `ActivityTile.tsx:46` and `FindBox.tsx:178` already scroll. It does not call `scrollIntoView`, which walks every scrollable ancestor and is missing in jsdom: every existing dom suite that mounts `Sidebar` would throw in the layout effect.
- Pure core `revealDelta({ rowTop, rowBottom, viewTop, viewBottom, inset }): number`. It returns 0 when the row is fully inside `[viewTop + inset, viewBottom]`. It returns a negative delta that puts the row's top at `viewTop + inset` when the row is above that band, and a positive delta that puts its bottom at `viewBottom` when the row is below it. A row taller than the band aligns to its top.
- `inset` = the height of the sticky headers stacked over the row. That is the row's card header `offsetHeight`, plus the group header `offsetHeight` when `row.closest('.hv-worktree-group')` is non-null, which covers both the group body and the group peek. A peeked row in a collapsed card sits outside any group, so its inset is the card header alone. These are measured values, not a new CSS token: the group header's height depends on its content, so pinning it to a token would be a layout change.
- Geometry: `rowTop`/`rowBottom` come from `row.getBoundingClientRect()`, and `viewTop`/`viewBottom` from `#projects.getBoundingClientRect()`. Both are viewport coordinates, so nesting and sticky containers don't matter. `offsetTop` is not used. The card header is `row.closest('.hv-project-card')` → `.hv-project-card__header`. The group header is `row.closest('.hv-worktree-group')` → `.hv-worktree-group__header`.
- Guard: if the scroller's `clientHeight === 0` (sidebar hidden with ⌘S, or jsdom), do nothing. jsdom therefore stays a no-op and throws nothing.
- Minimal by construction, which gives criteria 2 and 3: a fully visible row moves the list 0px.
- `pending` is `{ id, seen }`. It is replaced on the next `activeId` change. It is also dropped when its id was **seen** in the store and then vanished, which covers a session killed before its row ever painted. An id that has not arrived yet stays pending, which is the spawn race behind criterion 7. On initial mount `pending` holds the startup active id, so the first render reveals it. That's intended: the app opens with the active row in view.
- Grid view: a tile click that doesn't change `activeId` fires nothing, which is consistent with criterion 3.

Alternatives rejected:
- `scrollIntoView({block:'nearest'})` with a measured `scroll-margin-top`. It scrolls ancestors, throws in jsdom, and the margin is a side channel.
- A CSS `scroll-margin-top` token. The group header has no fixed height.

### B. Peek the active row out of a collapsed container

Collapse is CSS-only (rows stay mounted inside a clipped, `visibility:hidden` body), so the active row must render **outside** the body while collapsed, and must not also be inside it (a hidden duplicate would win the `[data-sid]` lookup and break A).

- **Project card.** `ProjectItem`: when `o.collapsed` and the active session is in `rows`, split it out **at the render step only**. `omit` is passed **only when the project card is collapsed**. In that case the project-level peek owns the row and no `WorktreeGroup` peeks it, so two peeks can never claim the same row. For a `WorktreeGroup`, `omit` filters only the children array it receives. `count`, `attention` and `activeIndex` are all computed from the **unfiltered** run. When the card is expanded, `omit` is null, the group gets every child and `activeIndex`, and it peeks only if it is itself collapsed. `renderRows` still builds `worktreeGroups()` / `allGroups` from the full `rows`, so a 2-member group never shrinks to one member and dissolves. That would remount `<WorktreeGroup key="wt:…">`, lose its local collapse state, and miscount its count and attention. Concretely, `renderRows(o, rows, { omit: activeId })`: inside a group run the omitted member is skipped, and a plain row is skipped too. The body renders everything except the omitted row; `ProjectCard` gets a new `peek?: ReactNode` prop rendered as `<ul className="hv-project-card__peek">` between the header and `.hv-project-card__body`. The peek row is rendered flat (no worktree group wrapper, `titleOnly=false`) by the same per-row renderer `renderRows` uses (extract its inner `item()` into a module-level `renderRow(o, s, titleOnly)`).
- **Worktree group.** Group collapse is local state inside `WorktreeGroup`, so only the group can decide. `renderRows` passes the run's rendered children plus `activeIndex` (index of the active member in the run, or -1). `WorktreeGroup`: when `collapsed && activeIndex >= 0`, renders `children[activeIndex]` in a `<ul className="hv-worktree-group__peek">` after the header, and the body gets the other children; otherwise unchanged.
- Expand: the peek disappears and the row returns to its body position. Collapse/expand state is untouched (still `collapsed` set / local state), chevron and `aria-expanded` unchanged → criteria 4–6.
- CSS, with the selectors named explicitly:
  - The card peek is `<ul className="hv-project-card__rows hv-project-card__peek">`. It inherits every `__rows` rule. The hairline rules keyed on `.hv-project-card__rows > .hv-worktree-group` (sidebar.css:99-105) never match it, because it holds a single flat row.
  - The group peek is `<ul className="hv-worktree-group__rows hv-worktree-group__peek">`. It keeps the rail (margin + `border-left`, sidebar.css:241) and the member indent `.hv-worktree-group__rows > .hv-session-row` (sidebar.css:254), so the peeked member still reads as part of its group.
  - `__peek` gets no extra rules unless the browser check shows a gap. Tokens only, and ui-lint stays clean.
- Nav/reorder: `orderedSessions()` already walks sessions in collapsed containers (that is the trigger of the bug); no change. Keyboard reorder of a peeked row swaps with an invisible neighbour — the same as today, where the active row is entirely invisible; not a regression and out of scope.

### Files to change

1. `cmd/hivegui/frontend/src/components/Sidebar.tsx` — call `useRevealActiveRow(activeId)` in `Sidebar()`; extract `renderRow`; `ProjectItem` splits the active row into `peek` when collapsed; `renderRows` passes `activeIndex` to `WorktreeGroup`.
2. `cmd/hivegui/frontend/src/components/ProjectCard.tsx` — `peek?: ReactNode` prop rendered between header and body.
3. `cmd/hivegui/frontend/src/components/WorktreeGroup.tsx` — `activeIndex` prop; peek when collapsed.
4. `cmd/hivegui/frontend/src/theme/components/project-card.css`, `.../sidebar.css` — peek list styles.
5. `.changesets/sidebar-follows-active-session.md` — `type: changed`, `bump: minor`.
6. `site/features.json` — entry, `since: "Unreleased"`.
7. `docs/design-docs/ui/components.md` (~L57-72): the ProjectCard and WorktreeGroup sections, including the "Collapsed: body hidden…" line, describe the active-row peek.
8. `docs/design-docs/ui/patterns.md` (near L39, attention bubbling): add "the active row stays visible in a collapsed container", alongside #407's minimized rule.
9. `test/e2e/sidebar-group-flatten.spec.ts`: add the `__peek` direct-child selector to its UNGROUPED/GROUPED sets so a peeked row is counted correctly.
10. `README.md`: only if a sidebar section describes collapse. Check during implementation.

### New files

- `cmd/hivegui/frontend/src/lib/reveal-row.ts`: the pure `revealDelta()` plus `revealRow(row, scroller)`.
- The `useRevealActiveRow(activeId)` hook sits next to `Sidebar()` in `Sidebar.tsx`. It has one caller, so it gets no new module.
- `cmd/hivegui/frontend/test/unit/reveal-row.test.ts`: tests for the pure delta.

### Tests

Unit (`test/unit/reveal-row.test.ts`, pure `revealDelta`):
- `returns 0 for a row fully inside the band`: covers criteria 2 and 3.
- `scrolls up just enough to clear the sticky inset`: a row partly under the inset gives `rowTop - (viewTop + inset)`.
- `scrolls down just enough to show the bottom edge`.
- `aligns the top of a row taller than the band`.

DOM (vitest, `test/dom/sidebar-harness.tsx`; spy on the scroller's `scrollTop` setter; stub `getBoundingClientRect`/`clientHeight`/`offsetHeight` on `#projects`, the target row and the headers, because jsdom has no layout):
- `test/dom/sidebar-reveal.test.tsx`
  - `reveals the row once when activeId changes`: the row is stubbed below the view, and `scrollTop` is set once to the expected value.
  - `does not scroll on unrelated store updates`: after the reveal, a status or attention update to another session leaves `scrollTop` unwritten (non-goal).
  - `waits for a row that renders after the id`: set `activeId` to an id with no session yet and nothing is written; add the session and `scrollTop` is written once (criterion 7 race).
  - `uses the card + group header heights as inset for a grouped row, including a group-peek row`.
  - `does nothing while the sidebar has no height` (⌘S non-goal).
  - `drops a pending id whose session was seen and then removed`: no write afterwards, even when an unrelated session is added. `waits for a row that renders after the id` must still pass, so both behaviours are asserted.
  - The setup stubs `clientHeight` on `#projects` to a value above 0. Without it, the hidden-sidebar guard turns every test into a silent no-op.
- `test/dom/sidebar-peek.test.tsx`
  - `collapsed project shows only the active row, outside the body` — exactly one `.hv-session-row` in `.hv-project-card__peek` with the active sid; no row with that sid inside `.hv-project-card__body`; card `data-collapsed`; chevron `aria-expanded="false"` (criterion 4).
  - `peek clears when the active session leaves the project` (criterion 6) — no `__peek` rendered, `collapsed` set unchanged.
  - `collapsed worktree group peeks its active member` (criterion 5) — click group chevron, active member in `.hv-worktree-group__peek`, others only in body.
  - `expanded containers render no peek`.
  - `active minimized session in a collapsed project still peeks` (criterion 8 + interaction with #407).
  - `a 2-member group keeps its wrapper and collapse state while a member peeks from a collapsed project`: collapse the group, collapse the project, activate a member, expand the project. The group still renders as `.hv-worktree-group` with `data-collapsed` and its member count is 2.

E2E (Playwright vs mock bridge, `CI=1`), `test/e2e/sidebar-reveal.spec.ts` — real layout:
- `keyboard nav reveals an off-screen row below the sticky headers` — seed with `seedScrollableGroup`, scroll `#projects` to top, nav (`Mod+ArrowDown` / `Mod+ArrowUp` via the bindings) to rows far below; assert row rect inside `#projects` rect and `elementFromPoint` at the row's top+1px and bottom-1px returns a node inside the row (not a sticky header) (criterion 1).
- `upward nav clears both sticky headers for a grouped row` — scroll so a grouped row sits under the headers, nav to it, same `elementFromPoint` assertion.
- `palette, nav history, grid tile click reveal` — one row each (criterion 1 sources); ⌘B via `ringBell` on an off-screen session then `next-attention`.
- `clicking a fully visible row does not move the list` — record `scrollTop`, click, assert equal (criterion 2).
- `switching to an already visible row does not move the list` (criterion 3).
- `a spawned session is revealed` — `window.__hive.addSession` (session:event `added` → `switchTo`) with the list scrolled away (criterion 7).
- `collapsed project peeks the active row` / `collapsed worktree group peeks the active row` — real visibility (`toBeVisible`), other rows `toBeHidden` (criteria 4, 5).
- `every-shortcut.spec.ts` unaffected (no new binding/scope).

## Verification

```bash
cd cmd/hivegui/frontend
./../../../scripts/ci-bootstrap.sh   # if wailsjs bindings missing
npm run typecheck >/dev/null && echo TC-OK
npx biome ci . >/dev/null && echo BIOME-OK
npx vitest run test/unit/reveal-row.test.ts test/dom/sidebar-reveal.test.tsx test/dom/sidebar-peek.test.tsx
CI=1 npx playwright test test/e2e/sidebar-reveal.spec.ts test/e2e/sidebar-sticky.spec.ts test/e2e/sidebar-collapse-animation.spec.ts test/e2e/sidebar-group-flatten.spec.ts
cd ../../.. && scripts/ui-lint.sh && scripts/test.sh unit dom e2e   # FULL dom layer: every suite that mounts Sidebar must stay green
```

Mutation checks (each change must turn the named test red):
- Drop `inset`: `upward nav clears both sticky headers` (e2e, using `elementFromPoint`, which is the real gate) and `scrolls up just enough to clear the sticky inset` both fail.
- Make `revealDelta` center the row: `returns 0 for a row fully inside the band` and `clicking a fully visible row does not move the list` both fail.
- Build groups from the reduced rows: `a 2-member group keeps its wrapper…` fails. - Leave the active row in the body as well as the peek: `collapsed project shows only the active row, outside the body` fails.
- Replace the pending ref with `deps: [activeId]`: `waits for a row that renders after the id` fails.

## Open questions / risks

- `sidebar-render-scope.test.tsx` counts renders per component. Adding `peek`/`omit` props must not add renders for unrelated rows. If a count changes, the reason gets recorded in the Decision log; the expectation is not loosened silently.
- e2e `toBeHidden` right after a collapse waits out the 160ms `visibility` delay (`COLLAPSE_MS`). Playwright auto-waits, and the test says so in a comment.
- Collapse animation: on collapse, the active row jumps from body to peek while the body animates shut. Acceptable; verify by eye in `wails dev` / Playwright screenshot.
- Drag-reorder with a peeked row: the peek `<ul>` is not the body `<ul>`; drag placeholder insertion targets siblings. Dropping onto a peeked row must not corrupt order — check `dragRowProps` during implement; if it misbehaves, the peek row gets no drop-target role (still draggable).
- `CSS.escape` exists in WKWebView/WebView2/webkit2gtk and jsdom.

## Second opinion

Independent reviewer (hs-reviewer subagent), two rounds.
- **Round 1:** `revise`, confidence 7, 5 must-fix items, all applied.
  - Peeking must not dissolve a two-member group (groups are built from the full rows).
  - Guard against jsdom's missing `scrollIntoView`, by switching to a manual `scrollTop`.
  - Name the CSS for the peek lists.
  - Make the e2e `elementFromPoint` check the real gate for sticky-header clearance.
  - Add the design-doc updates.
- **Round 2:** `revise`, confidence 8, 2 must-fix items, both applied.
  - The pending-clear rule contradicted the spawn race; it now uses a `seen` flag.
  - Make the `omit`/`activeIndex`/count wiring explicit.
- The round-2 fixes were not re-reviewed (cap of two rounds). The operator approved via chat.

## Decision log

- **2026-11-17** — Collapsed container keeps its collapsed state and shows only the active row (operator choice in brainstorm). Why: respects the user's collapse; mirrors #407.
- **2026-11-17** — Scroll is minimal (`nearest`); a click on a fully visible row never moves the list. Why: operator choice; avoids the list jumping under the cursor.
- **2026-11-17** — Any active-session change reveals, including programmatic ones. Why: "by any means".
- **2026-11-17** — Dropped the plan's `seen` flag: pending is just the active id, replaced on the next active change. Why: a stale id can only fire if that same session's row appears, and it is still the active session then, so revealing it is correct. The flag guarded nothing.
- **2026-11-17** — `revealRow`'s inset includes the scroller's `padding-top`. Why: e2e showed a stuck sticky header pins to the scroller's content edge, 4px below its border edge (`#projects { padding: 4px 0 }`). The DOM stubs couldn't see this. It was caught by the `elementFromPoint` gate.
- **2026-11-17** — `sidebar-group-flatten.spec.ts` was left unchanged. Why: each peek `ul` also carries the matching `__rows` class, so its `__rows > li` selectors already count peek rows in the right set.
- **2026-11-17** — `sidebar-collapse-animation.spec.ts` "a project card animates shut" now seeds a second session. Why: the mock's only session is active, so under #563 it peeks out of the body and leaves nothing to animate. That is intended behaviour, and the test premise needed a non-active row.

## Progress

- **2026-11-17** — Spec triaged (enhancement · M · P2); research complete.
- **2026-11-17** — Plan approved (chat); stage IMPLEMENT.
- **2026-11-17** — Implemented. 6 DOM/unit mutation checks and 1 e2e mutation check each turn a named test red. typecheck, biome ci, ui-lint, and scripts/test.sh unit (867) / dom (1361) / e2e (586) are all green.

## PR convergence ledger

- **2026-11-17 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 1; action: escalated:thread-opened-after-autofix; head_sha: 96dbf746.
- **2026-11-17 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: c7c41eab; threads_open: 0; action: autofix+push; head_sha: b8ec265a.
- **2026-11-17 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: d038e127.

# Keep the new-session launcher inside the window when its project is near the bottom

- **Spec:** [docs/product-specs/580-keep-the-new-session-launcher-inside-the-window-wh.md](../../product-specs/580-keep-the-new-session-launcher-inside-the-window-wh.md)
- **Issue:** #580
- **Status:** active
- **PR:** #581
- **Branch:** feature/580-launcher-stays-in-window

## Summary

The launcher popup is positioned once, at `header.bottom + 4`, with no viewport check, so for a project near the bottom of the sidebar it runs off the window. This plan adds a viewport-aware placement (below, else above, else clamped and scrolling) that re-runs when the popup's own size changes.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/components/modals/Launcher.tsx:219-245` — mount-only `useLayoutEffect` that anchors `#launcher` under `.hv-project-card[data-pid] .hv-project-card__header` (fallback `#new-project-btn`, then a fixed 16/64 spot). Sets `root.style.top = r.bottom + 4` with no bound. This is the bug.
- `Launcher.tsx:264-290` — the agent list loads asynchronously after positioning (`ListAgents()`), so the popup's height at mount ("Loading agents…") is not its final height. A mount-time-only clamp would still overflow.
- `Launcher.tsx:607-685` — worktree/branch/orchestrator/kind rows can appear after open (git probe), also changing height.
- `cmd/hivegui/frontend/src/theme/components/launcher.css:7-31` — `#launcher` is `position: fixed`, has a max-width but no max-height and no overflow; `.launcher-list` has no rule at all, so nothing bounds the height.
- No existing viewport-clamp helper in `src/` (only `innerHeight` users are unrelated). `ResizeObserver` is used elsewhere in the app but is absent in jsdom.
- `cmd/hivegui/frontend/test/e2e/launcher-stacking.spec.ts` — existing Playwright (mock bridge) spec that opens the launcher with ⌘/Ctrl+T and measures its rect; the pattern to copy.
- `cmd/hivegui/frontend/test/dom/launcher.test.tsx` — jsdom tests for the launcher; must keep passing without `ResizeObserver`.

### Constraints / dependencies

- The comment at `Launcher.tsx:226-227` deliberately forbids re-anchoring mid-open (it would move the popup under the pointer). The re-check must only move the popup when it would overflow, and must not flip back and forth while the user filters (shrinking list).
- CSS changes must be validated in a real browser (vitest is CSS-blind).

### Prior lessons

- No prior lessons matched (`brain-search 'launcher popup viewport'`).
- Decision records: none apply (`hs-decision list` printed nothing).

### Conventions card

- Build: `./build.sh` · Tests: `scripts/test.sh [go|unit|dom|e2e]` · Lint: `npx biome ci .` and `npm run typecheck` in `cmd/hivegui/frontend` · `scripts/ui-lint.sh` for token rules.
- TDD: every behaviour change ships with the test that would have caught it.
- Pure logic goes in `src/lib/` with a unit test in `test/unit/`; layout behaviour is asserted in Playwright e2e (mock bridge), run with `CI=1`.
- User-visible fix: add `.changesets/<slug>.md` (`type: fixed`, `bump: patch`); never edit `CHANGELOG.md`.
- UI styling uses theme tokens; `scripts/ui-lint.sh` enforces.

## Approach

Pure placement function + a size-driven re-run, plus a CSS height bound.

1. New pure function `placePopup(anchor: {top, bottom} | null, height, viewportH, prev?: 'below'|'above') => {top, side}` in `src/lib/popup-place.ts`:
   - `gap = 4`, `margin = 8`.
   - below = `anchor.bottom + gap`. If `prev !== 'above'` and `below + height <= viewportH - margin` → `{below, 'below'}`.
   - above = `anchor.top - gap - height`. If `above >= margin` → `{above, 'above'}`.
   - Otherwise clamp: `top = max(margin, min(below, viewportH - margin - height))`, side = `prev ?? 'below'` (keeps stickiness).
   - `anchor === null` (no card, no button) → keep today's fixed 64px top, clamped the same way.
   - Side is sticky: once `'above'`, never return to `'below'` (filtering shrinks the list; flipping back per keystroke would jump the popup under the pointer).
2. `Launcher.tsx` layout effect: keep left as is; measure anchor rect once at mount (mount-only anchor, as today), then one idempotent `place()` closure that reads `root.offsetHeight` + `window.innerHeight`, calls `placePopup`, and writes `root.style.top` only when the value changed. At mount the layout effect runs child-first, while `#launcher` is still `.hidden` (display:none, `offsetHeight === 0`), so `place()` first writes today's provisional `anchor.bottom + 4` and, when height is 0, returns without choosing a side. The real placement happens in a `ResizeObserver` on `root` (fires on reveal 0 → real height and on every later size change, delivered after layout and before paint, so the reveal itself does not jump; the agent list loading later can still cause one flip, see risks) and on `window` `resize`. RO is guarded with `typeof ResizeObserver !== 'undefined'` (jsdom lacks it; jsdom keeps today's provisional top). Disconnect/remove on unmount. The anchor rect is NOT re-measured — only the popup's own size drives re-placement, so the "no re-anchor mid-open" rule holds. Cannot loop: `top` does not feed back into size; a scrollbar appearing may cause one extra tick, which converges.
3. `launcher.css` `#launcher`: `max-height: calc(100vh - 16px); overflow-y: auto; overflow-x: hidden;` so a popup taller than the window scrolls instead of being clipped (8px margin each side; `placePopup` uses `innerHeight`, CSS is the backstop). Eyeball the worktree-on and prompt states in a real browser for clipped focus rings.

Why not CSS-only: the anchor position is runtime; CSS cannot flip a fixed popup above its anchor. Why not clamp only at mount: the agent list loads after mount, so the mount-time height is the "Loading agents…" shell.

### Files to change

1. `cmd/hivegui/frontend/test/e2e/wails-mock.ts` — test-only `__hive.addProject` helper (plus its type in `hive-global.d.ts` if typed there).
2. `cmd/hivegui/frontend/src/components/modals/Launcher.tsx` — replace the `top = r.bottom + 4` write with `placePopup` + ResizeObserver/resize re-run; update the comment block.
3. `cmd/hivegui/frontend/src/theme/components/launcher.css` — `max-height` + `overflow-y: auto` + `overflow-x: hidden` on `#launcher`.

### New files

- `cmd/hivegui/frontend/src/lib/popup-place.ts` — the pure placement function.
- `cmd/hivegui/frontend/test/unit/popup-place.test.ts` — unit cases.
- `cmd/hivegui/frontend/test/e2e/launcher-viewport.spec.ts` — real-browser regression.
- `.changesets/launcher-stays-in-window.md` — `type: fixed`, `bump: patch`, `issue: 580`.

### Tests

- `test/unit/popup-place.test.ts`:
  - `places below the anchor when it fits` (unchanged behaviour).
  - `flips above the anchor when below would overflow`.
  - `clamps to the viewport when neither side fits` (top == margin, bottom within viewport for height ≤ vh-16).
  - `stays above once flipped, even when the popup shrinks` (stickiness).
  - `no anchor: fixed spot, clamped`.
  - `height 0 (hidden) returns the provisional top and no side`.
- `test/e2e/wails-mock.ts` — add `window.__hive.addProject(name)` (wraps the mock's existing `CreateProject`), test-only, so a spec can put a second project card below the seed project.
- `test/e2e/launcher-viewport.spec.ts` (Playwright, mock bridge). Each test opens the launcher for a specific project (hover its header, click its + button), waits for `.launcher-item`, then reads rects with `expect.poll` so the post-`ListAgents` size is what is asserted:
  - `opens below its project header when there is room` — default viewport, seed project: assert `popup.top >= header.bottom`.
  - `flips above the header when a project near the bottom would overflow` — add ~10 sessions to `p1` and a second project via `addProject`; size the viewport so the second project's header sits near the bottom edge. Preconditions asserted first: `header.bottom + 4 + popup.height > innerHeight` (the bug is real in this layout) and `header.top - 4 - popup.height >= 8` (above fits, so this is not the clamp branch). Then assert `popup.bottom <= header.top` and `popup.top >= 0`.
  - `clamps and scrolls when neither side fits` — viewport too short for either side (e.g. 1200×220), seed project; preconditions: header rendered, neither side fits; assert `popup.top >= 0`, `popup.bottom <= innerHeight`, `scrollHeight > clientHeight`; then `scrollIntoView` the last `.launcher-item` and assert `elementFromPoint` at its centre is inside it (row reachable).
- Mutation checks (recorded in Progress): with the `Launcher.tsx` placement reverted, the "flips above" and "clamps" tests fail; with only the CSS `max-height`/`overflow` reverted, the "clamps" test fails.
- `test/dom/launcher.test.tsx` — must keep passing (no ResizeObserver in jsdom).

### Verification

- `cd cmd/hivegui/frontend && npx vitest run test/unit/popup-place.test.ts test/dom/launcher.test.tsx`
- `cd cmd/hivegui/frontend && CI=1 npx playwright test test/e2e/launcher-viewport.spec.ts test/e2e/launcher-stacking.spec.ts test/e2e/launcher-search.spec.ts`
- Mutation checks: (a) revert `Launcher.tsx` to `top = r.bottom + 4` → above + clamp tests fail; (b) revert only the CSS → clamp test fails.
- `scripts/test.sh unit dom e2e`, `npx biome ci .`, `npm run typecheck`, `scripts/ui-lint.sh`.

### Open questions / risks

- Resizing the prompt textarea (`resize: vertical`) in a flipped-above popup moves the popup's top as it grows (bottom stays pinned to the header). It converges and is rare; documented, not special-cased.
- Flip happens after the agent list loads (popup first paints below with "Loading agents…", then jumps above). One jump, before the user can act on rows; acceptable. Alternative (hide until loaded) delays the popup and changes focus timing — rejected.
- Sidebar scroll while open is a non-goal (anchor is mount-only today).
- ResizeObserver callback writing `style.top` cannot loop: changing `top` does not change the popup's size.

## Second opinion

- Round 1 (independent `hs-reviewer`): **revise**, confidence 7. Must-fix: `#launcher` is `.hidden` when the layout effect runs (height 0), the e2e layout as drafted would pass vacuously, and the CSS needs its own mutation check. All 3 applied.
- Round 2: **approve**, confidence 8. Nice-to-haves folded in: branch preconditions in e2e, `overflow-x: hidden`, height-0 unit case.
- Disposition: approved by the operator (chat), 2026-11-25.


## Decision log

- **2026-11-25** — proposed records: none — no direction-setting decision (a per-feature placement choice).
- **2026-11-25** — Assumed placement order below → above → clamp-and-scroll, with no clarifying question. Why: keeps today's behaviour when it fits, and "above" keeps the project header (the reason the popup is anchored there) visible.
- **2026-11-25** — The placement side is sticky once flipped to above. Why: filtering shrinks the list; re-choosing "below" on every keystroke would make the popup jump under the pointer.
- **2026-11-25** — `#launcher` gets `box-sizing: border-box`. Why: the e2e clamp test caught `max-height` being content-box, so padding + border put the clamped popup 2–10px past the window edge.

## Progress

- **2026-11-25** — Issue #580 created, spec triaged (bug, S, P2), research done.
- **2026-11-25** — Implemented. Unit (7) + e2e (3 branches) green; `scripts/test.sh unit dom e2e`, `biome ci`, `typecheck`, `ui-lint` green.
- **2026-11-25** — Mutation checks: placement reverted (height forced to 0) → "flips above" + "clamps" fail; CSS bound removed → "clamps" fails.
- **2026-11-25** — Eyeballed flip state with worktree rows open in Chromium: no clipped focus rings.

## Open questions

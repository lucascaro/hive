# Fix huge-text flash on grid → zoom → session switch (regression)

- **Spec:** [docs/product-specs/176-huge-text-flash-zoom-session-switch.md](../../product-specs/176-huge-text-flash-zoom-session-switch.md)
- **Issue:** #176
- **Stage:** IMPLEMENT
- **Status:** active

## Summary

The huge-text flash that #168 (7f1f78a) fixed for the *first* grid → zoom transition still appears on the *next hop*: while in zoomed (single) view, switching to a different session shows one paint frame with stale, grid-cell-sized canvas pixels stretched to the full viewport before the WebGL renderer catches up.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/main.js:448-459` — `SessionTerm.show()`. Today it adds `.visible`, forces a layout read (`void this.body.clientWidth`), then calls `_onBodyResize()` synchronously. This is the prior fix from #168.
- `cmd/hivegui/frontend/src/main.js:461-463` — `SessionTerm.hide()`. Just removes `.visible` (→ `display:none`). No teardown of the WebGL canvas, so the canvas keeps its last-fit pixel dimensions.
- `cmd/hivegui/frontend/src/main.js:469-500` — `_onBodyResize()` runs `fit.fit()` synchronously. `fit.fit()` calls `term.resize(cols, rows)`, which the WebGL renderer (xterm `addon-webgl`) handles by scheduling its canvas resize on the next `requestAnimationFrame`.
- `cmd/hivegui/frontend/src/main.js:108-119` — WebGL addon load. The renderer is the source of the rAF-deferred canvas resize.
- `cmd/hivegui/frontend/src/main.js:1149-1160` — `showSingle(id)`: iterates all terms, calls `hide()` on inactive ones and `show()` on the target; `ensureAttached()` afterward. This is the path taken on session switch while in `state.view === 'single'`.
- `cmd/hivegui/frontend/src/main.js:1162-1189` — `switchTo(id)` calls `showSingle(id)` for single-mode switches.

### Reproduction trace

1. Grid view → every `SessionTerm` is fit to its grid cell. Canvas pixel size = small.
2. Zoom session A → `showSingle(A)`. A's `show()` runs sync fit → A's canvas grows to viewport. Other sessions (B, C, …) are `hide()`d while still holding grid-cell-sized canvases.
3. Switch to B → `showSingle(B)`. B's `show()` runs sync fit. **But:** xterm-webgl applies the new canvas dimensions on the next rAF tick. The browser composites at least one frame in between, where:
   - CSS layout already reports the new full-size body box (host became `display:block`).
   - The WebGL canvas element still has the old grid-cell pixel dimensions and is being scaled up by CSS.
   - Result: one frame of huge-text flash before the rAF fires and the canvas re-rasterizes at the right cell size.

### Why #168's fix doesn't cover this hop

#168 reasoned that the trailing `ResizeObserver` callback was racing the rAF; calling `_onBodyResize()` synchronously from `show()` was supposed to land the fit before the next paint. That holds for the *first* hidden→visible flip when xterm hadn't drawn a new frame yet. For session switches, the receiving terminal's canvas already has stale pixels backing it from grid mode (or from a prior zoom session), and even a synchronous `fit.fit()` only enqueues the canvas resize to rAF — it doesn't change the backing canvas dimensions in time for the upcoming compositor frame.

### Constraints & dependencies

- Cannot rely on rAF timing: the bug *is* the rAF-deferred canvas resize.
- Cannot dispose/recreate the WebGL renderer on every switch — that would lose scrollback rendering state and add visible latency.
- The flash is specifically a CSS-stretched stale-canvas frame, so any fix that prevents the host from being painted with the stale canvas one frame would also work (visibility gate, opacity gate, pre-sizing the canvas, or forcing a synchronous render).

### Candidate approaches (for the PLAN stage to choose between)

1. **Pre-fit hidden tiles when entering zoom.** When `showSingle` runs, fit *every* `SessionTerm` (or at least all in-state ones) to the new container size up front, not just the visible one. The hidden ones' canvases would then already be at full size when later switched to.
2. **Visibility gate in `show()`.** Keep the host at `visibility: hidden` (or `opacity: 0`) on `show()`, run sync fit, then on the next rAF (after xterm-webgl's canvas resize lands) flip to visible. Costs one frame of blank but no flash.
3. **Force WebGL canvas resize synchronously.** Reach into `webgl._renderer` (or call `term.refresh(0, term.rows-1)`) right after `fit.fit()` to drive the pixel resize before paint. More fragile (touches xterm internals).
4. **Re-fit on `hide()` to "neutral" small size, with CSS that paints solid background until ready.** Hide background overlay during the transition.

Approach (2) is the smallest, most robust change; (1) is also cheap and avoids the one-frame blank but means doing N fits on every zoom entry. PLAN stage should pick.

## Approach

**Visibility gate during the rAF-deferred WebGL canvas resize.** In `SessionTerm.show()`, set the body to `visibility: hidden` *before* flipping the host to `display:block`, run the synchronous fit as today, then reveal the body on the next `requestAnimationFrame` — by which time xterm-webgl has applied the new canvas dimensions and painted at least one frame at the correct size.

Why this beats the alternatives considered in research:

- **Pre-fit hidden tiles on zoom entry** would also work for the specific switch-after-zoom path but doesn't cover other rAF races (e.g., resizing the window while in single mode then switching). It also costs N fits per zoom for users with many sessions. The visibility gate is path-agnostic.
- **Forcing the WebGL renderer to resize synchronously** would touch xterm internals (`webgl._renderer`) and is fragile across xterm-addon-webgl upgrades.
- **Overlay/background-color masking** trades one paint artifact for another.

`visibility: hidden` keeps layout intact, so `body.clientWidth` (the layout read the prior fix relies on) and `fit.fit()` still measure correctly — the flag only suppresses pixel paint. One rAF (~16ms at 60Hz) of a hidden tile is well below the perceptibility threshold for a session switch and far less jarring than the current huge-text flash.

Edge cases handled:

- **`hide()` racing the rAF.** If the user switches away again before the rAF fires, the host already has `display:none`; the rAF restorer must no-op so the next `show()` starts from a known-good state. We track the in-flight reveal with a token and clear the inline style on `hide()`.
- **Font-size change path.** `_onBodyResize()` is also called directly from font-size handlers (main.js:103). Those paths don't change visibility and don't need the gate.
- **Initial attach (`_pendingAttach`).** `show()` runs before the first attach; the visibility gate covers this path too without changes — `_onBodyResize` will trigger `ensureAttached()` which itself does a `fit.fit()`, and the rAF reveal still applies.

### Files to change

1. `cmd/hivegui/frontend/src/main.js`
   - `SessionTerm.show()` (lines 448–459): set `this.body.style.visibility = 'hidden'` before adding the `.visible` class, keep the layout-flush (`void this.body.clientWidth`), keep the synchronous `_onBodyResize()`, then schedule a single `requestAnimationFrame` that clears the inline `visibility` style. Track the rAF id on `this._revealRaf` so it can be cancelled.
   - `SessionTerm.hide()` (lines 461–463): cancel any pending `this._revealRaf` and clear `this.body.style.visibility` so the next `show()` starts clean.
   - Update the comment in `show()` to record the new reasoning (the rAF-deferred WebGL canvas resize is the underlying cause across both grid→zoom *and* zoom→switch hops).

### New files

_none_

### Tests

The hivegui frontend has **no JS test runner** (`cmd/hivegui/frontend/package.json` ships only Vite). `AGENTS.md` §Testing Conventions covers Go packages (`internal/state/`, `internal/tui/`, etc.); there is no equivalent JS unit/functional infrastructure to extend. Per the spirit of the TDD rule we still need *verification*, so this PR uses an explicit manual repro checklist:

1. **Repro the regression on `main`** before applying the patch (capture a screen recording at 60fps if available).
2. **Apply the patch** and re-run each path:
   - Grid (project) → zoom session A → switch to session B (same project).
   - Grid (all) → zoom session A → switch to session B (different project).
   - Single → switch back and forth between two sessions that were both in the prior grid.
   - Window resize while in single mode, then switch to another session.
   - Font-size change (no host visibility change should occur).
3. **Verify** no huge-text flash on any path; confirm the new one-frame visibility gap is imperceptible.
4. Run `go test ./...` and `cd cmd/hivegui/frontend && npm run build` to confirm no Go or build-time regressions.

If a follow-up wants to add JS test scaffolding, that's an "ocean" change tracked separately; this PR stays in the lake.

## Open questions / risks

- **Risk:** a future upgrade of xterm-addon-webgl that resizes the canvas synchronously would make the rAF gate strictly unnecessary but harmless. Worth a comment pointing at this exec plan so a future reader knows why the gate exists.
- **Risk:** users on very low refresh rates (<30Hz, e.g. external display in some power-save modes) might perceive the one-frame blank. Acceptable trade vs. the current flash.
- **Decided not to do:** pre-fit hidden tiles on zoom entry. Adds work proportional to session count and doesn't cover the window-resize-then-switch path.

## Decision log

- **2026-05-09** — Created from #176 after #168 regression reported. Why: prior fix only covered the first hidden→visible flip; the inter-session switch path while zoomed exhibits the same stale-canvas-stretched-by-CSS symptom.
- **2026-05-09** — Picked the visibility-gate approach over pre-fitting hidden tiles. Why: path-agnostic (also covers window-resize-then-switch), O(1) per show vs. O(N sessions) per zoom entry, and doesn't depend on xterm-webgl internals.
- **2026-05-09** — Cancel the rAF in `hide()` *and* `destroy()`, plus clear the inline `visibility` style on hide. Why: a switch-away during the rAF window would otherwise leave an inline `visibility: hidden` on a hidden host and the next show would race.

## Progress

- **2026-05-09** — Research complete; advancing to PLAN.
- **2026-05-09** — Plan approved; advancing to IMPLEMENT.
- **2026-05-09** — Implemented the visibility gate in `cmd/hivegui/frontend/src/main.js` (`show`/`hide`/`destroy`, `_revealRaf` token initialised in the constructor). Updated CHANGELOG `[Unreleased]`. Verified `node --check` on main.js and `go test ./internal/... ./cmd/hived/...` (`./cmd/hivegui` setup-fails on missing `frontend/dist`/`wailsjs/`, a pre-existing worktree limitation; this change does not touch Go).

## Open questions

- Is a one-frame blank (visibility gate) acceptable, or should the implementation pre-fit on zoom entry to avoid any visible delay?

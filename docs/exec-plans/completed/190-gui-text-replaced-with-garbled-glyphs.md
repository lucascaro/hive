# GUI: session terminal text gets replaced by garbled glyphs over time

- **Spec:** [docs/product-specs/190-gui-text-replaced-with-garbled-glyphs.md](../../product-specs/190-gui-text-replaced-with-garbled-glyphs.md)
- **Issue:** #190
- **Stage:** REVIEW
- **Status:** active
- **PR:** #191
- **Branch:** feature/190-gui-glyph-corruption-recovery

## Summary

Long-lived GUI sessions render corrupted glyphs in the xterm.js WebGL renderer; a window resize triggers a renderer rebuild and fixes the display. Root cause is the WebGL texture-atlas getting into a stale/invalidated state — most commonly when the WebGL context is lost (browser caps on simultaneous contexts), when the device pixel ratio changes, or after long sessions accumulate atlas pressure. This plan teaches each `SessionTerm` to detect those triggers and recover without user action.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/main.js:80-120` — `SessionTerm` constructor. Each tile creates its own `Terminal` and loads `WebglAddon`. The only context-loss handling is `webgl.onContextLoss(() => webgl.dispose())` (line 115), which tears down the addon but leaves the terminal mounted with no working renderer. Nothing reloads the addon, nothing falls back to the DOM renderer, and nothing forces a `term.refresh()`. The stale pixels survive until the next layout-driven `fit.fit()`.
- `cmd/hivegui/frontend/src/main.js:484-515` — `_onBodyResize` is the single resize path; it calls `fit.fit()` and (when attached) `ResizeSession(...)`. This is also the implicit "fix" the user discovered: any geometry change causes the WebGL renderer to rebuild its atlas, which masks the bug. It is *not* called for DPR-only changes (e.g. moving the window to a different-DPI display, OS zoom).
- `cmd/hivegui/frontend/src/main.js:548-554` — `destroy()` disposes the terminal but does not explicitly dispose the WebGL addon, so on tab close the GL context lingers until GC. With many sessions this contributes to context-limit pressure.
- `cmd/hivegui/frontend/package.json` — pins `@xterm/xterm ^5.5.0` and `@xterm/addon-webgl ^0.18.0`. Both versions are current; this is not a stale-dep issue. `WebglAddon` exposes `clearTextureAtlas()` (public since 0.16) and emits `onContextLoss` / `onChangeTextureAtlas`.

### Constraints / dependencies

- **WebGL contexts are a finite resource.** Browsers cap simultaneous WebGL contexts (Chromium ≈ 16). The grid-mode GUI creates one context per tile, so a user with many sessions reliably blows past this limit; older contexts get killed by the browser and fire `webglcontextlost`. xterm.js surfaces this via `WebglAddon.onContextLoss`. Our handler disposes the addon but does not re-create it or fall back to the DOM renderer, leaving the term visually frozen with stale glyphs.
- **Atlas corruption without context loss.** Even within one context, the WebGL renderer caches glyph pages keyed by (char, fg, bg, bold, italic, underline). Long Claude sessions cycle through many color/attr combos (syntax-highlighted output, tool diffs), evicting pages. xterm.js's atlas LRU has had several bugs across the 0.16–0.18 line that manifest as "wrong char drawn"; `clearTextureAtlas()` is the documented escape hatch.
- **DPR changes.** Moving the Wails window between displays with different scale factors changes `devicePixelRatio`. xterm's WebGL renderer reads DPR at addon-load time and at resize. If the window isn't resized at the same instant DPR changes, the atlas is rendered at the old DPR and looks garbled on the new display — until the next resize.
- **Visibility & GPU sleep.** When the Wails window is occluded or the GPU sleeps (laptop lid close/open), some Chromium/CEF builds invalidate the GL backbuffer without firing context-loss. The canvas then displays whatever was last drawn until something triggers a `term.refresh()`.
- **Conventions.** Backend is Go (`go build ./...` / `go test ./...`); the GUI frontend has no JS test harness today — manual / functional verification in the running Wails app is the project's pattern for renderer changes (mirrors how #176 and #178 were validated).

### Hypotheses, ranked by likelihood

1. **WebGL context loss in many-tile setups (highest).** Our `onContextLoss` handler disposes but never recovers. Reproduces by opening enough sessions to exceed the per-process WebGL context cap.
2. **Atlas LRU eviction bug in `@xterm/addon-webgl@0.18`.** Long Claude sessions emit enough unique (glyph × attr) combos to thrash the LRU. Documented fix: call `clearTextureAtlas()` periodically or on `onChangeTextureAtlas`.
3. **DPR desync after display-switch or OS-zoom change.** No `window` listener for DPR change today.
4. **Stale backbuffer after window occlusion / GPU sleep.** No `visibilitychange` handler today.

## Approach

Two-layered fix in `SessionTerm`:

1. **Recover from context loss instead of swallowing it.** Replace the current `onContextLoss(() => webgl.dispose())` with a handler that disposes the addon *and* either re-creates the WebGL addon (preferred, fast path) or, if re-creation fails, leaves the terminal on the DOM renderer with an explicit `term.refresh(0, term.rows-1)`. Either branch must call `term.refresh` after re-creation/fallback so the stale backbuffer is overwritten without waiting for a resize.

   Chosen over the alternative of "fall back to DOM renderer unconditionally" because the WebGL renderer is performance-critical (see comment at main.js:109-111: "dramatically faster than the default DOM renderer on older machines, VS Code uses the same approach"); permanent DOM fallback would regress the use case the WebGL renderer was added for.

2. **Forced atlas-clear on the known silent triggers.** Add three lightweight listeners per `SessionTerm`:
   - `window.matchMedia('(resolution: ...)')` change → `webgl.clearTextureAtlas(); term.refresh(0, term.rows-1)`. Cheaper than the alternative of disposing+reloading the addon on every DPR change, and recommended in the xterm.js docs.
   - `document.addEventListener('visibilitychange', …)` → on becoming visible, `term.refresh(0, term.rows-1)`. Covers backbuffer-stale-after-occlusion.
   - A periodic `clearTextureAtlas()` is **deliberately not added** — it would mask underlying bugs and waste GPU on the fast path. We rely on the loss-recovery and DPR/visibility hooks; if reports persist we can add an idle-time atlas clear as a follow-up.

   Chosen over "dispose+reload the WebglAddon on every trigger" because `clearTextureAtlas()` is the documented minimal-cost reset; full addon re-creation is reserved for the context-loss path where it is actually required.

3. **Tidy disposal.** In `destroy()`, explicitly call `this.webgl?.dispose()` before `this.term.dispose()` so the GL context is released proactively. Reduces context-cap pressure when users close tiles in a many-tile session.

### Files to change

1. `cmd/hivegui/frontend/src/main.js`
   - **lines ~109-120** (WebGL addon init): factor addon construction into a small helper `_attachWebgl()` so the same code path is used at init and after context loss. The helper stores the addon on `this.webgl`, wires `onContextLoss` to (a) dispose the current addon, (b) attempt re-attach via `_attachWebgl`, (c) on re-attach failure null `this.webgl` and call `this.term.refresh(0, this.term.rows-1)`. After successful (re)attach, also call `term.refresh(...)` so the freshly-built atlas is painted immediately.
   - **constructor tail (~line 230)**: register a DPR `matchMedia` listener and a `document.visibilitychange` listener; in both, call `this.webgl?.clearTextureAtlas()` then `this.term.refresh(0, this.term.rows-1)`. Bind the listeners through `this._dprMql.addEventListener` / `document.addEventListener` and store the bound handlers on `this` so `destroy()` can remove them.
   - **`destroy()` (~lines 548-554)**: remove the listeners added above, then `this.webgl?.dispose()`, then existing `this.term.dispose()`.

### New files

None.

### Tests

No JS test harness exists in the GUI frontend (see AGENTS.md; renderer changes have historically been validated manually — e.g. PR #176, #178). Coverage plan:

- **Manual repro & verification matrix** captured in the PR description: (a) trigger context loss by opening >16 session tiles in grid mode; expected: no glyph corruption, smooth painting; (b) drag the window across two displays of different DPR; expected: atlas refreshes within one frame, no garbled text; (c) ⌘-tab away for >30s on a laptop with discrete-GPU sleep; expected: on return the terminal repaints cleanly. Each case checked before and after the fix; screenshots attached.
- **Add a Go unit test only if** we touch any Go-side resize/PTY code; this plan is JS-only so there is no Go test to add. `go test ./...` must still pass.
- **Regression checks:** confirm `internal/tui/...` flow tests still pass (`go test ./...`), and that the existing #176 "huge-text flash" path (main.js:449-467) still behaves correctly after the new listeners are attached.

### Open questions / risks

- **Risk: re-attaching `WebglAddon` after context loss can itself fail repeatedly in a context-exhausted process.** Mitigation: the recovery helper attempts re-attach at most once; on failure it leaves the terminal on the DOM renderer and logs nothing user-visible (matching today's silent fallback at main.js:118-120). A subsequent successful re-attach can happen the next time the user closes another tile (freeing a context) and the visibilitychange handler fires.
- **Risk: `matchMedia('(resolution: ...)')` syntax varies across Chromium versions / Wails-CEF builds.** Mitigation: feature-detect — if `matchMedia` is unavailable or throws, skip the DPR listener; the visibilitychange + context-loss paths still cover the dominant cases.
- **Open question: is grid mode the only repro vector, or do users hit this with a single tile after very long sessions?** If only grid mode reproduces, the context-loss fix alone is likely sufficient and the DPR/visibility hooks are belt-and-braces. If single-tile sessions also reproduce, hypothesis #2 (atlas LRU) is implicated and we may need a follow-up `clearTextureAtlas()` cadence.

## Decision log

- **2026-05-12** — Picked targeted listeners + context-loss recovery over a periodic atlas-clear timer. Why: a timer would mask whichever underlying trigger is real and waste GPU on the fast path; targeted listeners give us a clean signal for follow-up if reports persist.

## Progress

- **2026-05-12** — Spec drafted (#190), triage = bug/M/P1, research complete, plan drafted; awaiting Gate 4.
- **2026-05-12** — Implemented: `_attachWebgl()` / `_onWebglContextLoss()` recovery, DPR + `visibilitychange` listeners that call `clearTextureAtlas()` + `term.refresh()`, explicit `webgl.dispose()` in `destroy()`. `go build ./...` + `go test ./...` pass. JS syntax checked with `node --check` (full `vite build` requires Wails-generated bindings not in repo).
- **2026-05-13** — Rebased onto `origin/main` (was days stale; resolved CHANGELOG conflict, main.js auto-merged cleanly). Extracted pure helpers into `cmd/hivegui/frontend/src/lib/renderer-recovery.js` and added 8 vitest unit cases covering context-loss recovery + visibility predicate. Full `scripts/test.sh go unit dom` passes (e2e skipped — requires Wails-built `frontend/dist`).

## PR convergence ledger

- **2026-05-13 iter 1** — verdict: REQUEST_CHANGES; findings_hash: c4864cd91a268b7facb5a18f1fa5697c785db449fb0fa31d82a2827f230a55a9; threads_open: 0; action: autofix+push (DPR matchMedia rebind helper + tests); head_sha: ad1fe80. CI failure (focus.spec.js stdinText='llo' not 'hello') confirmed flake — manual rerun of Linux job passed, and full CI on fe9ca21 also green.
- **2026-05-13 iter 2** — verdict: APPROVE; findings_hash: empty; threads_open: 0; action: stop; head_sha: fe9ca21.

## Open questions

- See "Open questions / risks" in Approach.

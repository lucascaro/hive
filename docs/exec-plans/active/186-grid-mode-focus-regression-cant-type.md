# Regression: focus inconsistent — switching from single-session to grid mode disables typing

- **Spec:** [docs/product-specs/186-grid-mode-focus-regression-cant-type.md](../../product-specs/186-grid-mode-focus-regression-cant-type.md)
- **Issue:** #186
- **Stage:** QA
- **Status:** active
- **PR:** [#189](https://github.com/lucascaro/hive/pull/189) (merged 2026-05-12, squash → dff1656)
- **Branch:** feature/186-grid-focus-regression (deleted on merge)

## Summary

User report: after the #181 atomic-focus fix shipped (commit `8b5c702`, on `origin/main`), the single → grid transition still fails to deliver keystrokes to the visually focused session. We need to identify what `setFocusedTile`'s atomic reconcile missed and patch it without re-introducing the two-writer split that #181 eliminated.

## Research

### What #181 already changed

`cmd/hivegui/frontend/src/main.js` (post-merge of `origin/main`):

- The per-tile `focusin`/`focusout` listeners that used to write `.term-focused` were removed (constructor at lines 210–215 now carries only an explanatory comment).
- `setFocusedTile(id)` (lines 1407–1463) is the **sole** writer of `.term-focused`. Its body, in order:
  1. Modal-open or null-id gate → sweep `.term-focused` and return.
  2. Schedule one `requestAnimationFrame`.
  3. Inside the rAF: re-check the modal gate; bail if a real `<input>`/`<textarea>` (not the xterm helper) owns the keyboard.
  4. Sweep `.term-focused` off all other hosts; add it to `st.host`; call `.focus()` on `st.host.querySelector('.xterm-helper-textarea')` (or `st.term.focus()` as fallback).
- `focusActiveTerm()` and `refocusActiveTerm()` are now thin wrappers calling `setFocusedTile(state.activeId)` (lines 1469–1475).

This eliminates the documented #181 root cause: visual class drifting onto a different tile than the keyboard target during DOM churn.

### Single → grid call graph (post #181)

User presses `⌘G` (or `⌘⇧G` / `⌘Enter`) while in single mode. `cmd/hivegui/frontend/src/main.js`:

1. `window` `keydown` (line 2305) → `setView('grid-all' | 'grid-project')` (line 1523).
2. `setView`:
   - `state.view = view`.
   - `renderGrid()` (line 1295) — does **not** call `show()`/`hide()`. Adds `.in-grid` to each grid session host and calls `termsHost.appendChild(st.host)` to reorder them in DOM order.
   - `focusActiveTerm()` → `setFocusedTile(state.activeId)`.

Side-effects of `renderGrid` that touch the focused tile:
- `termsHost.classList.remove('single'); termsHost.classList.add('grid')` (lines 1296–1297). Between this line and the per-tile `add('in-grid')` below, the active tile **transiently matches `#terms.grid .term-host` (no `.in-grid` yet) → `display: none`**. Going `display: none` on an element that contains the focused `xterm-helper-textarea` fires synchronous `focusout`; `document.activeElement` becomes `<body>`.
- `termsHost.appendChild(st.host)` (line 1312) reorders the active tile under the same parent. Reparenting/move also drops focus to `<body>`.

By the time `setFocusedTile`'s rAF fires, `document.activeElement` is `<body>` (gate passes), the active tile has `.in-grid` (display restored), and `ta.focus()` should land.

### Candidate remaining failure modes #181 did not address

Listed in roughly decreasing likelihood. Each is testable cheaply with a one-line probe.

1. **rAF runs while `body.style.visibility === 'hidden'`.** `show()` (line 438) sets `this.body.style.visibility = 'hidden'` and schedules `_revealRaf` to clear it. `setFocusedTile`'s rAF is scheduled by callers *after* `show()`'s rAF in same-microtask cases (e.g. `setActive` → `showSingle` → `show` then `setFocusedTile`), so they fire in FIFO and visibility is cleared first. **But the single → grid path does *not* call `show()` for the active tile** — `renderGrid` skips it. So `_revealRaf` from a *prior* `showSingle` (entering single mode) should have long since fired. Edge case: rapid grid → single → grid before the first rAF settles leaves `_revealRaf` pending; same-batch FIFO still saves us. Low probability.

2. **The `appendChild` reorder triggers an xterm internal mutation that orphans the helper-textarea.** xterm v5 keeps `.xterm-helper-textarea` as a fixed child of `body` once `term.open()` runs. `appendChild` move preserves the subtree. **But** the active tile's helper-textarea node was created when the tile was first opened; if `renderGrid` ever re-runs `term.open()` (it does not; `ensureTerm` is idempotent and constructors only run once), the node would be replaced. Verified: `ensureTerm` (line 1147+) returns existing entries.

3. **`appendChild` to the *same parent in a new position* does not reliably keep focus AND xterm's internal `_focused` flag drifts true.** This is the documented #159 / #181 trap. The current fix focuses the **DOM helper-textarea** instead of `term.focus()`, which forces a real browser focus event xterm's own listener consumes. Probability that this still fails: low if the textarea selector resolves and the body is visible at rAF time.

4. **`document.activeElement` at rAF time is *not* `<body>` but a stale `<input>` (e.g. the session-name `<span>` in the tile header).** The header has a `tile-name-input` only during inline rename; absent that, the gate passes. Probability: low.

5. **`renderGrid` adds `.in-grid` *after* the parent class flip, leaving an intra-script frame where the active tile is `display:none` long enough for xterm to mark itself blurred AND skip re-focusing on the new ta.focus().** No paint happens between the two synchronous statements, so xterm's `_focused` flag *does* go false (focusout fires synchronously on display:none), but a fresh `ta.focus()` should re-enter. Medium probability: this matches the user's description of "fixed it but didn't work" — #181's atomic reconcile correctly *targets* the right tile, but the reconcile happens after the same hard blur the prior fix was already racing.

6. **`setFocusedTile`'s rAF is captured by a *prior* rAF callback that re-blurs.** Concretely: `_revealRaf` runs first and *clears* `body.style.visibility = ''` (does not blur). Then the focus rAF runs. Should be safe.

7. **Other modes than `⌘G` skip `setFocusedTile` entirely.** Non-keyboard transitions: tile click in grid → `switchTo` (line 1130) → `renderGrid` → `focusActiveTerm`. Sidebar click → same. Restart Session → `refocusActiveTerm`. Window focus → `refocusActiveTerm` (line 642). All routes through `setFocusedTile`. No gap found.

8. **`switchTo(id)` in single mode early-returns to `focusActiveTerm()` only when `id === state.activeId` (line 1167–1170)**, but if `id` differs and view is single, it calls `setActive(id)` → `focusActiveTerm()` (inside setActive at 1383), then `showSingle(id)`, then `focusActiveTerm()` again at line 1193. Two rAFs scheduled; first one runs against pre-show DOM (target tile not yet `.visible`, so the `st.host.querySelector('.xterm-helper-textarea')` exists but body is `display:none`, so `ta.focus()` is a no-op). Second rAF runs after show() and succeeds. **But for single → grid this isn't relevant — `renderGrid` doesn't go through `setActive`.**

### The most plausible remaining cause

Combining items 5 + 3: when `setView('grid-…')` runs, the active tile undergoes a synchronous `display:none → display:flex` flip (CSS class change leaving the host without `.in-grid` for one statement). The browser fires `focusout` on the helper-textarea synchronously. xterm's internal listener on the helper-textarea sets `_focused = false`. Then `appendChild` moves the host. Then `setFocusedTile` schedules rAF and the rAF calls `ta.focus()`.

If the helper-textarea's body chain is `display:none` *at the moment `ta.focus()` is called inside the rAF*, the focus call is a no-op. By that moment we expect `.in-grid` to be on the host (added synchronously inside `renderGrid` before `setFocusedTile` runs), so display should be `flex`. **However**, if the tile body is gated by a `_revealRaf` that hasn't yet cleared `body.style.visibility`, focus on its descendant *also* fails silently. `_revealRaf` is set by `show()`. `renderGrid` does not call `show()`, so this only bites when `show()` was called *just before* the user pressed `⌘G` — e.g. the user clicked a sidebar item in single mode (which routes through `switchTo` → `showSingle` → `show()`) and *immediately* hit `⌘G` in the same paint frame.

This is consistent with the user's report that #181 "didn't work" only on certain runs — single → grid breaks when preceded by a session switch in single mode that has not yet had its reveal-rAF fire. The cure: `setFocusedTile` should not assume body visibility is settled — either explicitly clear `body.style.visibility = ''` on the target before focusing (and cancel `_revealRaf`), or schedule the focus *after* the next `_revealRaf` resolves rather than the same animation frame.

### Constraints / dependencies

- xterm.js v5 has no `onFocus`/`onBlur` events; helper-textarea focus is the only safe handle.
- Must not re-introduce the two-writer split #181 removed.
- The `body.style.visibility` gate from #176 is needed to prevent canvas-flash; removing it is not an option.
- No JS test harness in `cmd/hivegui/frontend/` (`package.json` has no `test` script). Per `AGENTS.md`, GUI-side bugs verify manually against spec acceptance criteria. A regression guard (runtime assertion in dev builds) is the closest substitute.

### Files implicated

- `cmd/hivegui/frontend/src/main.js`
  - `SessionTerm.show` / `hide` (lines 438–478) — `body.style.visibility` and `_revealRaf`.
  - `renderGrid` (lines 1295–1374) — class flip, appendChild reorder, no `show()` call for active.
  - `setFocusedTile` (lines 1407–1463) — sole focus writer; its rAF is the place to harden against the visibility-pending case.
  - `setView` (lines 1523–1540) — calls `focusActiveTerm()` after `renderGrid`.
- `cmd/hivegui/frontend/src/style.css:445–499` — `#terms.single .term-host` requires `.visible` for `display`; `#terms.grid .term-host` requires `.in-grid` for `display`. The mismatch is the source of the transient `display:none`.

## Approach

User-confirmed repro narrows the cause: **single → grid (either grid-project or grid-all) reliably loses keyboard input; grid → single and grid → grid both work.** The `_revealRaf` race hypothesis is therefore wrong (going *to* grid doesn't call `show()` and grid → single uses the same `_revealRaf` path successfully). The remaining cause is in `renderGrid`'s synchronous DOM churn that no other transition triggers:

1. `termsHost.classList.remove('single'); add('grid')` (line 1296–1297) — the active tile, which has `.visible` (irrelevant in grid CSS) but not yet `.in-grid`, goes `display:none` for the duration of the rest of the script. Synchronous `focusout` fires on the helper-textarea. `document.activeElement` becomes `<body>`. xterm's internal listener on the helper-textarea catches `blur` and sets its `_focused` flag to `false`.
2. The per-tile loop adds `.in-grid` to each grid host (display restored) and `appendChild`s each (more DOM churn; doesn't re-focus anything).
3. `focusActiveTerm()` → `setFocusedTile(state.activeId)` → schedules one rAF.
4. Inside the rAF, `ta.focus()` is called on the active tile's helper-textarea. The DOM focus call appears to succeed in the spec but, on this transition specifically, xterm's input pipeline doesn't recover — the user's reliable-failure report confirms this.

The `setFocusedTile` rAF currently does *one* attempt and assumes success. Hardening it without re-introducing the two-writer split #181 removed:

**Change 1 — verify-and-retry.** After `ta.focus()`, check `document.activeElement === ta`. If not (browser silently dropped the focus call — possible if the helper-textarea's ancestor chain wasn't yet "settled" enough by the browser's internal hit-testable state), schedule one more rAF that repeats the sweep + add + focus sequence. One retry is enough; if that still fails the cause is outside the rAF's reach and we should not loop indefinitely.

**Change 2 — also call `st.term.focus()` after `ta.focus()`.** The #181 fix moved away from `term.focus()` because it early-returns on a stale-true `_focused` flag, but the situation here is the opposite: after the synchronous `display:none` blur, xterm's `_focused` is now stale-*false*. Calling `term.focus()` is a safe second write that pokes xterm's internal state machine. Order matters: `ta.focus()` first (real DOM focus event drives xterm's listener-based update), then `term.focus()` as a belt-and-braces sync.

**Change 3 (regression guard).** Add a dev-mode assertion (gated on `localStorage.getItem('hive.debug') === '1'`) that 2 rAF ticks after `setFocusedTile` runs, checks `.term-focused` matches `document.activeElement`'s nearest `.term-host` ancestor. Console-warn on mismatch with enough context (view, activeId, ae.tagName, ae.className) to diagnose future variants. Cheap, off by default, on for QA.

**Why this approach over alternatives.**
- *"Cancel `_revealRaf` and clear `body.style.visibility` before `ta.focus()`"* — speculative; the user repro shows the bug is not gated on visibility-pending.
- *"Skip the `display:none` flip by adding `.in-grid` BEFORE the `single→grid` class flip on termsHost"* — would solve the synchronous blur, but reordering the class flip is fragile (every `#terms.single .term-host` rule needs to be re-evaluated for grid context with `.visible` lingering) and risks a worse layout artifact than the current transient `display:none`.
- *"Defer `setFocusedTile` to a `setTimeout(0)` instead of rAF"* — moves the bug from one tick to another without addressing what actually broke.
- *"Fire a synthetic `focus` event on the helper-textarea instead of `.focus()`"* — bypasses the browser's internal focus tracking; would set xterm's `_focused` flag but `document.activeElement` would not be the textarea, so real keystrokes still go to body. Worse than today.

The verify-and-retry + term.focus() pair targets the actual stuck state (xterm `_focused` desync after a synchronous display blur) without touching renderGrid's DOM order.

### Files to change

1. `cmd/hivegui/frontend/src/lib/focus.js` (new) — extract the pure gate-decision into a testable function `decideFocusAction({ id, modalOpen, activeTag, activeClasses, knownTermId })` returning one of `{ kind: 'clear' }`, `{ kind: 'preserve' }` (real input owns keyboard, leave alone), or `{ kind: 'focus', id }`. This isolates the only logic in `setFocusedTile` that has interesting branches; the rest is DOM side-effects.

2. `cmd/hivegui/frontend/src/main.js` — harden `setFocusedTile`:
   - Replace inline gate with a call to `decideFocusAction(...)` from `lib/focus.js`.
   - Inside the rAF, after `ta.focus()`, call `st.term.focus()` to belt-and-braces xterm's internal `_focused` flag (the #181 fix worried about stale-true; here we have stale-false after a synchronous `display:none` blur).
   - Verify `document.activeElement === ta` after the focus calls; if not, schedule one retry rAF that repeats the sweep + add + focus. Cap at one retry — do not loop.
   - Add `_assertFocusConsistent(id)` helper gated on `localStorage.getItem('hive.debug') === '1'`, scheduled two rAFs later, console-warning with `{ view, activeId, ae.tagName, ae.className }` on mismatch. Off by default; on for QA.

3. `cmd/hivegui/frontend/test/unit/focus.test.js` (new) — unit tests for `decideFocusAction` covering the gate matrix.

4. `cmd/hivegui/frontend/test/e2e/focus.spec.js` (new) — Playwright E2E driving the actual bug scenario via the Wails-mock bridge.

5. `CHANGELOG.md` — `[Unreleased] / Fixed` entry: "GUI: switching from single-focus to grid mode no longer leaves the active session visually focused but unable to receive keystrokes (#186, follow-up to #181)."

### New files

- `cmd/hivegui/frontend/src/lib/focus.js`
- `cmd/hivegui/frontend/test/unit/focus.test.js`
- `cmd/hivegui/frontend/test/e2e/focus.spec.js`

### Tests

The four-layer harness from PR #188 (`scripts/test.sh`) is now the contract. Per `AGENTS.md`: "All changes require both unit tests and functional tests." This change ships both, plus updated manual QA for the cases the harness can't reach (multi-window / native dialogs).

**Unit (`test/unit/focus.test.js`):**
- `decideFocusAction` returns `clear` when `id == null`.
- `decideFocusAction` returns `clear` when `modalOpen === true` (launcher / project editor / palette).
- `decideFocusAction` returns `clear` when `id` is not in `knownTermIds` (defensive: tile was destroyed).
- `decideFocusAction` returns `preserve` when `activeTag === 'INPUT'` / `'TEXTAREA'` and the active element is NOT the xterm helper (rename, inline editor).
- `decideFocusAction` returns `focus` when the active element is the xterm helper-textarea (same tile, already correct).
- `decideFocusAction` returns `focus` when the active element is `<body>` (post-blur state — the single → grid case).
- `decideFocusAction` returns `focus` when `contentEditable` is true on a non-terminal element only if `preserveContentEditable` flag is false (matches current code's lack of contentEditable carve-out — pinning behavior).

**E2E (`test/e2e/focus.spec.js`):** driven by the Wails-mock bridge that already powers `smoke.spec.js`. xterm is real in this harness, so helper-textarea behaves authentically. Each test waits for the initial sidebar to render, then exercises the transition and asserts focus state via `page.evaluate`.

- `single → grid-all preserves keyboard focus on the active session` — press `Mod+Shift+G`; assert `#terms` has `.grid` class; assert exactly one `.term-host` has `.term-focused`; assert `document.activeElement.classList.contains('xterm-helper-textarea')`; assert that helper-textarea is a descendant of the focused host.
- `single → grid-project preserves keyboard focus` — same with `Mod+Enter`.
- `keystrokes reach the active session after single → grid-all` — after the transition, `page.keyboard.type('hello')`; assert the mock backend received the text via `window.__hive.lastStdin` (extend the mock to capture WriteStdin payloads; the mock already exposes `window.__hive.state` so a parallel `window.__hive.stdinLog` is trivial).
- `grid → single still receives keystrokes` (#159 regression).
- `sidebar-click session switch in grid` (#181 regression) — click another session in the sidebar; assert `.term-focused` and helper-textarea both move to the new tile.
- `cold-start in grid mode (#187 path)` — set `localStorage.hive.view = 'grid-all'` before `page.goto('/')`; assert the first keystroke reaches the active session without any click or Mod+G press. This is the test that confirms whether the bug is purely transition-based or has a cold-start variant too.

Mock extension needed: add `stdinLog` (array of `{ id, b64 }` entries) and a `lastStdin(id)` helper in `test/e2e/wails-mock.js`'s `WriteStdin` shim. Trivial — one mutation; tracked as a sub-task of this plan.

**Manual QA (still required for native paths the E2E mock can't fake):**
- macOS native menu interaction with `⌘G` (the E2E mock does not exercise the AppKit menu path).
- Multi-window: two GUI windows open, switching modes in one must not steal focus from the other.

**Dev-mode assertion** stays as Change 3 above — covers the "future-variant" case unit + E2E miss.

## Decision log

- **2026-05-11** — Created spec/plan via /hs-feature-loop. Identified that #181 (commit 8b5c702) is the prior fix attempt the user is referring to.
- **2026-05-11** — Pulled `origin/main` into the worktree before research; the worktree was at `e3e3d2c` (release v2.2.1) and missed the #181 fix on which the user's regression report is grounded.

## Progress

- **2026-05-11** — Spec drafted, triage approved (bug / S / P1).
- **2026-05-11** — Research complete; no smoking-gun confirmed without a live repro, but most plausible remaining cause identified (visibility-pending interaction between #176's `_revealRaf` and #181's `setFocusedTile` rAF).
- **2026-05-11** — User repro detail: single → grid (either flavor) reliably fails; grid → grid and grid → single both pass. Narrowed cause to the synchronous `display:none` flip on the active tile during `renderGrid`'s parent class swap.
- **2026-05-12** — Plan revised to leverage the new four-layer test harness (PR #188): added unit + E2E tests for the fix and a sub-task to extend the Wails mock with a `stdinLog` capture.
- **2026-05-12** — Implemented on `feature/186-grid-focus-regression`. Initial fix (single retry rAF) was insufficient — E2E with focus tracing showed the disturbance fires ~10ms after the verify rAF, AFTER the in-rAF check passed. Generalised to bounded polling (8 frames), idempotent re-focus. All 50 vitest + 7 Playwright tests pass, including the user-visible bug repro (`single → grid then type 'hello'`).
- **2026-05-12** — `decideFocusAction` extracted to `lib/focus.js` with 10 unit tests; Wails mock extended with `stdinLog` / `stdinText()` / `resetStdin()` so E2E can assert keystrokes reach the backend.

## PR convergence ledger

<!-- Append-only. One line per /hs-review-loop iteration. -->

- **2026-05-12 iter 1** — verdict: COMMENT (coerced to REQUEST_CHANGES); findings_hash: ef8c1edc; threads_open: 5; action: continue (autofix next iter); head_sha: fda78e2.
- **2026-05-12 iter 2** — verdict: REQUEST_CHANGES (autofix ran); findings_hash: empty; threads_open: 0; action: autofix+push (5 fixes, CI passed); head_sha: fff3b54.
- **2026-05-12 iter 3** — verdict: APPROVE; findings_hash: empty; threads_open: 0; action: stop (converged); head_sha: fff3b54.

## Open questions

- **Reproduction path**: does the user hit this on every single → grid press, or only after a sidebar-click session switch immediately preceding the `⌘G`? Knowing this disambiguates between candidate causes 5 and the visibility-pending hypothesis.
- **Cold-start grid mode**: if the GUI launches into grid mode (persisted view), does the first keystroke land? Different code path.

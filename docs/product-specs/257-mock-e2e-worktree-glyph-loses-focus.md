---
issue: null
title: "Sidebar and grid repaints silently drop keyboard focus"
type: bug
complexity: S
priority: P1
stage: REVIEW
---

# Sidebar and grid repaints silently drop keyboard focus

- **Issue:** —
- **Type:** bug
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** —

## Problem

`cmd/hivegui/frontend/test/e2e/worktrees.spec.ts:247` — *"the worktree glyph on
a session opens the browser"* — intermittently fails at

```
await glyph.focus();
await expect(glyph).toBeFocused();   // Expected: focused / Received: inactive
```

The mock suite runs `failOnFlakyTests` (`playwright.config.js`), so one
first-attempt failure is a red gate. It failed the `Build, Vet & Test (Linux)`
leg on `main` at merge commit `ae88431` (run 33455952043), which is the *mock*
suite — not the `e2e-real` suite spec 245 fixed.

## What is already known

Measured 2026-08-31 on macOS, mock suite, `CI=1`:

- **Reproducible: 2 of 12** full-file runs under 8 `yes > /dev/null` CPU hogs.
- **The test alone: 10/10 green** (`-g "worktree glyph on a session"`, same
  load). So it is cross-test interference inside the file.
- **Inserting one `page.evaluate` between `focus()` and the assertion made it
  8/8 green** under the same load.
- At the moment of the added probe the element was always still connected and
  still `document.activeElement`.

## Root cause (confirmed 2026-08-31)

**Not a test-harness race. The app drops the focus, and it does it in
production too.**

`renderSidebar` (`cmd/hivegui/frontend/src/app/sidebar.ts`) rebuilds the whole
list:

```js
projectsUL.innerHTML = '';
```

and re-creates every `<li>` through `sessionRow()`. Nothing saved or restored
`document.activeElement` anywhere in `sidebar.ts` or `ui/session-row.ts`, so
any focused element inside `#projects` was destroyed and the browser dropped
focus to `<body>`.

That rebuild ran on **every** `session:event`, from the tail of the handler in
`app/events.ts` — including kind `updated`, which is the high-frequency kind.
The daemon emits `updated`:

- on every phase step (`starting` → `worktree` → `ready`),
- once per surviving session when a kill recompacts the order
  (`internal/registry/registry.go`, `projects.go`),
- and when the agent-session-id capture goroutine finally succeeds — it polls
  every 200ms for up to **30 seconds** after a spawn
  (`internal/registry/create.go`, `internal/agent/codex.go`).

The test's own create path is the same shape: `wails-mock.ts`'s
`CreateSession` emits `added`, then two chained `setTimeout(0)` `updated`
events. Under CPU load a `setTimeout(0)` slips far enough to land *after*
Playwright's `glyph.focus()` round trip — so the rebuild happens between
`focus()` and `toBeFocused()`, which is exactly the observed millisecond-scale
window, and exactly why one extra `page.evaluate` (which drains the pending
timers first) papered over it.

Note the comment above the assertion in `worktrees.spec.ts` proposed a
different mechanism — `:focus-within` applying `display:none` to the focused
button. That was wrong: `theme/components/session-row.css` does the hover swap
with `opacity` + `pointer-events`, deliberately *not* `display`, and the
worktree button sits outside both swapped containers. CSS was never involved.

### The same bug in the grid — the Windows leg

`focus-invariants.spec.ts:88` (F2, *"killing a non-active session preserves
focus on the active tile"*) failed the Windows leg of the same CI run with
`assertAlignedFocus` timing out. Same class, different code path:

```js
// app/view.ts, once per tile, every renderGrid
termsHost.appendChild(st.host); // re-order to keep DOM == nav order
```

`appendChild` on an already-attached node is a remove + insert, and the browser
blurs whatever is focused inside it. `app/focus.ts` already carries a 500ms
`_focusGuard` for precisely this blur — but it is armed only by
`setFocusedTile`, and the kill path only calls that when the session that died
was the *active* one. In F2 it is not, so nothing re-armed the guard and
nothing restored focus. Most `renderGrid` passes reorder nothing at all, so
the re-parent was pure cost.

## Fix

1. **Session updates stop rebuilding the sidebar.** `events.ts` routes kind
   `updated` to a new `updateSidebarRows()`, which patches the existing nodes
   via the `patchRows` path that `title` events already used, and falls back to
   a full `renderSidebar()` only when the sidebar's *shape* — project and
   session ids in render order — actually moved. `applyState` was extended to
   reconcile the worktree glyph and the agent code, which previously only
   existed on the build path; a session that gains its worktree branch on a
   later `updated` now grows the control in place.
2. **The grid reorders only when the order changed.** `renderGrid` compares the
   current DOM order of its tiles against nav order and re-parents nothing when
   they already agree — which is the common case, including F2's.
3. **Both paths restore focus when a real rebuild or move happens**, via
   `src/lib/preserve-focus.ts`. It only ever reclaims focus that was
   *dropped* to `<body>`; if the render deliberately moved focus (a modal, a
   terminal, an inline rename), the new owner keeps it — the same rule
   `focus.ts`'s blur guard follows.

### A trap this fix walked into

Rows were being rebuilt on every `updated`, and their event handlers closed
over the `SessionInfo` the row was drawn from. The rebuild refreshed those
closures as a side effect. Stopping the rebuild froze them, so a handler kept
answering from the session as it looked at first paint — phase `starting`, not
yet alive, no worktree.

`killSession` branches on exactly that: `alive === false` sends
`KillSession(id, force=true)`, which bypasses the daemon's `worktree_dirty`
refusal — so closing a session with uncommitted changes would have thrown them
away silently instead of asking. `worktrees.spec.ts:845` caught it. Row
handlers now capture only the id and look the session up at call time.

Worth remembering for any future move away from rebuild-everything rendering:
the rebuild is load-bearing in more places than the DOM.

## Desired behavior

The test passes deterministically on the first attempt, without weakening what
it checks: the worktree glyph must be visible at rest, be the element under the
cursor when the row is hovered, and hold focus. Those three assertions exist
because the meta column is `display:none` on `:hover`/`:focus-within`, so a
button parked there passes every jsdom assertion while being impossible to
click or tab to — only a real layout engine catches it.

## Success criteria

- The full `worktrees.spec.ts` file is green on the first attempt across 20
  consecutive runs under CPU load, with `CI=1`. **Met: 20/20 green on the
  first attempt**, against 2/12 red on the identical harness before the fix.
- `focus-invariants.spec.ts` green on the same terms. **20/20 after the fix —
  but it was also green before it.** F2 never reproduced locally; its failure
  was seen only on the CI Windows leg. So the grid half of this fix rests on
  the mechanism read out of `view.ts` and `focus.ts` plus
  `test/dom/grid-reorder-focus.test.ts`, NOT on a local red-to-green swing.
  Per 245's lesson, CI is the arbiter for that half.
- The fix addresses why focus is lost, rather than inserting a sleep or an
  arbitrary round trip before the assertion. **Met:** the cause was a real
  focus bug in the app; both E2E tests are unchanged.
- Regression cover that fails without the fix: `test/dom/sidebar-focus.test.ts`
  (3 of its 6 cases fail on the old always-rebuild behaviour, a 4th on the
  stale-closure bug below) and `test/dom/grid-reorder-focus.test.ts` (1 of 3
  on the unconditional re-parent, another with focus restoration removed).

## Non-goals

- The `e2e-real` suite, which spec 245 owns.
- Relaxing `failOnFlakyTests`. First-attempt green is the standard.
- Rebuilding the sidebar's render layer around a diffing scheme. The shape
  check plus the existing in-place patch path is enough to stop the churn;
  a general reconciler is not needed for a list this size.

## Notes

Found on 2026-08-31 while verifying spec 245's merge (PR #307). Unrelated to
that change: it is a different suite, and `git diff` for #307 does not touch
`test/e2e/` or any sidebar source file.

Reproducing this locally has a trap worth knowing about: `playwright.config.js`
hardcodes port 5174, and `reuseExistingServer` is false under `CI=1`. Two
worktrees running the mock suite at the same time silently fight over the port
and every run in the loser fails with `http://localhost:5174 is already used`
— which looks nothing like a flake and wastes a lot of time. Give a repro loop
its own port.

A caution carried over from 245: this is a CI-observed failure that also
reproduces locally. Confirm any fix against repeated first-attempt-green CI
runs, not only local ones — 245 lifted a CI-only quarantine on local evidence
and had to re-instate it the same day.

That caution is why this spec stays in REVIEW rather than DONE. The local
evidence here is much stronger than 245's was — a mechanism confirmed by
reading the code, a 2/12 → 20/20 swing on the same harness, and unit tests
that fail on the old behaviour — but the failure was still observed on CI, so
CI is where it has to be signed off.

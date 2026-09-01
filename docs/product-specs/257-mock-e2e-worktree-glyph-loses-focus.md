---
issue: null
title: "The mock e2e worktree-glyph test loses focus under load and reds main"
type: bug
complexity: S
priority: P2
stage: TRIAGE
---

# The mock e2e worktree-glyph test loses focus under load and reds main

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
  load). So it is cross-test interference inside the file, not the test's own
  setup — the same class of shared-state problem spec 245 found in `e2e-real`.
- **Inserting one `page.evaluate` between `focus()` and the assertion made it
  8/8 green** under the same load. The added round trip is a few milliseconds,
  which points at a focus steal or a pending re-render landing right after
  `focus()` rather than at anything the assertion itself does.
- At the moment of the added probe the element was always still connected and
  still `document.activeElement`, so the node is not being replaced *before*
  focus lands.

The obvious suspect is a sidebar re-render (`renderSidebar` / the hover-action
meta-column swap this test exists to guard) replacing or blurring the row
shortly after focus is applied, triggered by an event from an earlier test in
the file. That is a hypothesis, not a diagnosis — it has not been confirmed.

## Desired behavior

The test passes deterministically on the first attempt, without weakening what
it checks: the worktree glyph must be visible at rest, be the element under the
cursor when the row is hovered, and hold focus. Those three assertions exist
because the meta column is `display:none` on `:hover`/`:focus-within`, so a
button parked there passes every jsdom assertion while being impossible to
click or tab to — only a real layout engine catches it.

## Success criteria

- The full `worktrees.spec.ts` file is green on the first attempt across 20
  consecutive runs under CPU load, with `CI=1`.
- The fix addresses why focus is lost, rather than inserting a sleep or an
  arbitrary round trip before the assertion.
- If the cause turns out to be a real focus bug in the sidebar rather than a
  test-harness race, it is fixed in the app and the test is left alone.

## Non-goals

- The `e2e-real` suite, which spec 245 owns.
- Relaxing `failOnFlakyTests`. First-attempt green is the standard.

## Notes

Found on 2026-08-31 while verifying spec 245's merge (PR #307). Unrelated to
that change: it is a different suite, and `git diff` for #307 does not touch
`test/e2e/` or any sidebar source file.

A caution carried over from 245: this is a CI-observed failure that also
reproduces locally. Confirm any fix against repeated first-attempt-green CI
runs, not only local ones — 245 lifted a CI-only quarantine on local evidence
and had to re-instate it the same day.

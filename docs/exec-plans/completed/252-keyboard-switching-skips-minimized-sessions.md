# Keyboard switching skips minimized sessions and projects

- **Spec:** [docs/product-specs/252-keyboard-switching-skips-minimized-sessions.md](../../product-specs/252-keyboard-switching-skips-minimized-sessions.md)
- **Issue:** —
- **PR:** #293
- **Branch:** feature/252-keyboard-switching-skips-minimized-sessions
- **Status:** completed

## Summary

`⌘↑` / `⌘↓` and `⌘[` / `⌘]` walk unfiltered lists, so they step onto sessions and
projects the user minimized. Both navigation walks learn to skip hidden entries,
using the `isSessionHidden()` predicate that already exists for exactly this
question. The lists themselves stay unfiltered — sidebar, tray, `⌘K` and `⌘1-9`
all need the full set.

## Research

Authored via plan-first mode; code references identified during plan-mode
iteration:

- `cmd/hivegui/frontend/src/app/keyboard.ts:736` — `moveActiveSession(delta,
  reorder)`. The `reorder === false` branch does `const next = (idx + delta + n)
  % n; switchTo(ord[next].id)` over `orderedSessions()`, which is the raw list.
  `isSessionHidden` is already imported at line 60 for the `⌘B` and nav-history
  paths.
- `cmd/hivegui/frontend/src/app/view.ts:466` — `isSessionHidden(id)` returns true
  when the session is in `state.minimized` **or** its project is in
  `state.minimizedProjects`. Its own comment says keyboard.ts should branch on
  it rather than on `state.minimized` directly, "so the two mechanisms can never
  drift apart".
- `cmd/hivegui/frontend/src/app/view.ts:398` — `shiftActiveProject(delta)` walks
  `state.projects` modulo length with no minimized filter, then relies on
  `fallBackToSingleIfActiveHidden()` to paper over landing on a hidden project.
- `cmd/hivegui/frontend/src/app/selectors.ts:8` — `orderedSessions()`, shared by
  the sidebar, tray, palette and `⌘1-9`; must stay unfiltered.
- `cmd/hivegui/frontend/test/dom/minimize-project.test.ts` exercises the real
  `view.ts`; `test/dom/keyboard-arrows.test.ts` mocks it wholesale, so a mocked
  `isSessionHidden` returns falsy there and these assertions must not live in
  that file.

## Approach

Filter at the walk, not at the list. Narrowing `orderedSessions()` globally would
break the sidebar, tray, palette and `⌘1-9`, and would also break the reorder
path, which sends an index into the daemon's **global** order space. Stepping
over hidden entries inside each walk keeps that index space intact.

### Files to change

- `cmd/hivegui/frontend/src/app/keyboard.ts` — `moveActiveSession`, navigation
  branch only: cyclic walk from `idx` in direction `delta` to the first session
  where `!isSessionHidden(s.id)`; return without switching if the full circle
  finds none. Reorder branch untouched.
- `cmd/hivegui/frontend/src/app/view.ts` — `shiftActiveProject`: resolve `next`
  by walking `state.projects` cyclically from `i` to the first id not in
  `state.minimizedProjects`; return if none. Rest of the function, including the
  `fallBackToSingleIfActiveHidden()` guard, unchanged — that guard still covers a
  visible project whose sessions are all individually minimized.
- `docs/product-specs/250-minimize-projects-from-sidebar.md` — drop `⌘[ / ⌘]`
  from the "remain alive and reachable via" success criterion and note the
  supersession by #252.
- `.changesets/` — one entry (user-visible behavior change).

### New files

None.

### Tests

In `cmd/hivegui/frontend/test/dom/minimize-project.test.ts`, new
`describe('keyboard navigation skips minimized things')`:

- `⌘↓ skips a session inside a minimized project`
- `⌘↑ skips an individually-minimized session`
- `stays put when every other session is hidden`
- `⇧⌘↓ still reorders across a minimized sibling` (regression guard on the
  unfiltered index space)
- `⌘] skips a minimized project`
- `stays on the current project when every other project is minimized`

Replaces the now-unreachable `falls back to single when ⌘[ / ⌘] lands on a
minimized project` case.

## Decision log

- **2026-08-30** — Skip both hiding mechanisms, not just minimized projects.
  Why: `isSessionHidden()` already unifies them, and one rule cannot drift.
- **2026-08-30** — Also filter `⌘[ / ⌘]`, amending spec #250. Why: user decision
  — a project you set aside should be out of the keyboard rotation entirely.
- **2026-08-30** — Leave `⌘1-9` unfiltered. Why: positional index; filtering
  would renumber sessions on every minimize.

## Progress

- **2026-08-30** — Plan-first scaffold; stage = IMPLEMENT (set in the spec).
- **2026-08-30** — Implemented both walks, added 7 dom tests, amended spec
  #250, CHANGELOG entry under `[Unreleased] / Fixed` (repo has no
  `.changesets/`). Checks green: vitest 608 passed, `tsc --noEmit` clean,
  `biome ci .` clean. Go untouched, so the `go` layer was not run.

## Open questions

None.

## PR convergence ledger

- **2026-08-30 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 2749afd6…; threads_open: 0; action: stop; head_sha: bf0d8a8.
- **2026-08-30 iter 1 follow-up** — applied both IMPORTANT findings and the MINOR one by hand rather than exiting on COMMENT: the no-active-session seed in `moveActiveSession` was the same bug in the sibling exit, three source comments still claimed ⌘[ / ⌘] reaches minimized projects, and `delta * i` assumed |delta| === 1.
- **2026-08-30 iter 2** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 913d158. All three iteration-1 findings verified cleared. Two MINORs applied afterwards (dead initializer in `shiftActiveProject`, an inaccurate test comment about `keyboard-arrows.test.ts`'s mock). The third MINOR — no Active row in `docs/product-specs/index.md` — is not actionable: that file is generated on push to `main`.

## QA verdict

- **2026-08-30** — verdict: PASS; checks: 3 dimensions passed / 0 failed / 0 followups; followups: none; one-line: all 6 success criteria and 3 non-goals verified by execution against the merged tree, no regressions in the adjacent ⌘B / nav-history / reorder / ⌘1-9 paths.
  - 2026-08-30 dimensions:
    - build/lint/test — PASS — `scripts/test.sh go` all packages ok; `vitest run` 626 passed; `tsc --noEmit` clean; `biome ci .` clean (6 `noExplicitAny` warnings are pre-existing in `test/dom/theme-apply.test.ts` from #292, not this feature).
    - acceptance — PASS — every success criterion exercised; the grid-view leg is structural (`handleArrow` routes non-single views to `gridSpatialMove` over the already-filtered `gridLayout.sessions`, so `moveActiveSession` is never reached there).
    - non-goals — PASS — ⌘1-9 still indexes the unfiltered list; sidebar / tray / ⌘K untouched by the diff; no new keybinding.
    - regression — PASS — reorder branch still sends a global index (pinned by test); `jumpToAttention` / `jumpBack` / `navGo` / `switchToProject` / `firstVisible` / `fallBackToSingleIfActiveHidden` unchanged; 74/74 targeted dom tests pass.
    - doc accuracy — PASS — CHANGELOG `[Unreleased] / Fixed` accurate; README + `shortcuts.ts` labels still correct; spec #250 amendment coherent; repo-wide grep finds no remaining claim that the arrows or ⌘[ / ⌘] reach minimized things. (The shipped `[2.4.0]` CHANGELOG entry still describes the old ⌘[ / ⌘] behavior — historical, deliberately not rewritten.)
  - Coverage gap closed during QA: added `never lands in a minimized project across a full ⌘↓ cycle` and `puts a restored project back in both rotations`, the full-cycle and round-trip forms the spec words but the merged suite only proved per-step.
  - Manual validation: confirmed by the operator in an isolated dev build (`scripts/dev-iso.sh`).

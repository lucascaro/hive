# Hide minimized sessions from the sidebar

- **Spec:** [docs/product-specs/407-hide-minimized-sessions-from-the-sidebar.md](../../product-specs/407-hide-minimized-sessions-from-the-sidebar.md)
- **Issue:** #407
- **Status:** active

## Summary

A minimized session keeps a dimmed row in the sidebar while ⌘↑/⌘↓ step over it (#252), so the sidebar shows rows the keyboard cannot reach. This plan removes minimized sessions from the sidebar list, leaving the minimized-session tray and ⌘K as the restore paths — the same model minimized projects already follow.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/components/Sidebar.tsx:462` `ProjectItem` — computes `attentionCount` and `worktreeGroups()` from `o.sessions` and hands the same list to `renderRows` (`:356`), which renders every session with `minimized={o.minimizedSessions.has(s.id)}` (`:366`). This is the only place minimized rows are painted.
- `Sidebar.tsx:585` `Sidebar` — already drops minimized *projects* from the list (`visible`, `:602`); minimized sessions are passed down untouched.
- `Sidebar.tsx:102` `keyHints()` — ⌘1–9 hints read `orderedSessions()` positionally over the full list; sessions of minimized projects already consume a digit with no row painted.
- `src/app/keyboard.ts:971` `moveActiveSession` — ⌘↑/⌘↓ walk skips `isSessionHidden()`; its comment (`:1005`) says the sidebar "still lists everything".
- `src/app/view.ts:257` `isSessionHidden`, `:332` `minimizeSession` (comment says a minimized session stays reachable "via the sidebar / palette"), `:360` `restoreSession`.
- `src/components/MinimizedTray.tsx` — tray above the status bar, visible in every view whenever `minimized` is non-empty; chip click → `restoreSession`.
- `src/components/SessionRow.tsx:151,263` + `src/theme/components/session-row.css:222` — `data-minimized` styling and the `＋` restore toggle. Still needed for the one row that stays (the active minimized session).
- `test/dom/minimize-project.test.tsx:396` `describe('session rows')` — asserts a minimized row stays in the list with a restore toggle; must change.
- `test/e2e/nav-history.spec.ts:87` — minimizes the *active* session, then clicks other rows; unaffected (never clicks the minimized row after it goes inactive).

### Constraints / dependencies

- A minimized session can be active (⌘K / `switchTo` does not un-minimize; single view). Its row must stay or the selection is invisible.
- Card header count and attention must keep counting minimized sessions (patterns.md › Attention bubbling).
- Specs #250 (`:34`, row stays dimmed) and #252 (Non-goals: sidebar keeps listing minimized things) are amended by this change.

### Prior lessons

- No prior lessons matched (`brain-search "sidebar minimized session row"`).

### Conventions card

- Tests: `scripts/test.sh [go|unit|dom|e2e]`; frontend `npm test` (vitest), `npm run typecheck` (needs `./scripts/ci-bootstrap.sh` in a fresh worktree), `npx biome ci .`, `CI=1 npm run test:e2e`.
- TDD: every behaviour change ships with the test that would have caught it; DOM behaviour in `test/dom`, real layout in `test/e2e` (Wails mock).
- User-visible change → `.changesets/<slug>.md` (`type: fixed`, `bump: patch`); never edit `CHANGELOG.md` or `docs/product-specs/index.md`.
- Stale docs are a bug: amend superseded spec text in the same PR.

## Approach

Filter at the one place rows are painted: `ProjectItem` in `cmd/hivegui/frontend/src/components/Sidebar.tsx`. It paints `rows = sessions.filter((s) => !minimized.has(s.id) || s.id === activeId)`. `renderRows()` walks `rows`, and panel wrapping (`WorktreeGroup`, `titleOnly`) is decided from `worktreeGroups(rows)` — a panel wraps the rows it paints. The per-row `worktreeShared` count comes from `worktreeGroups()` over the full list, because it states a fact about the worktree: a minimized session still holds it, which matters right before a kill of a dirty worktree. The card header's `sessionCount` and `attentionSummary()` keep reading the full list. Minimized projects are already dropped one level up (`Sidebar.tsx:602`), so `minimized.has` is the complete test.

Rejected: filtering in `Sidebar` before `ProjectItem` (card count/attention would drop minimized sessions). Rejected: filtering `orderedSessions()` (shared with ⌘1–9, tray, palette — #252 non-goal).

### Files to change

1. `cmd/hivegui/frontend/src/components/Sidebar.tsx` — `ProjectItem`/`renderRows`: painted rows; panel groups from rows; `worktreeShared` from full-list groups.
2. `cmd/hivegui/frontend/src/app/keyboard.ts:1005-1008` — comment claiming the sidebar still lists everything.
3. `cmd/hivegui/frontend/src/app/view.ts:328-331` — `minimizeSession` comment: reachable via tray / palette.
4. `docs/product-specs/250-minimize-projects-from-sidebar.md`, `252-keyboard-switching-skips-minimized-sessions.md`, `202-minimize-session-from-grid.md` — Notes amended by #407.
5. `docs/design-docs/ui/components.md` — sessionRow bullet on minimized rows.
6. `cmd/hivegui/frontend/test/dom/minimize-project.test.tsx` — rework `describe('session rows')`, add a nav reachability test.
7. `cmd/hivegui/frontend/test/e2e/minimize.spec.ts` — one real-browser test.
8. `cmd/hivegui/frontend/test/e2e/theme.spec.ts` — scene comment; regenerate the six `sidebar-*-chromium-darwin.png` baselines (`HIVE_SNAPSHOT=1`).

### New files

- `.changesets/hide-minimized-session-rows.md` — `type: fixed`, `bump: patch`, `issue: 407`.

### Tests

`test/dom/minimize-project.test.tsx › session rows` (every "absent" assertion preceded by a "present" one):
- `minimizing an inactive session removes its row`
- `keeps the active session row, marked, while it is minimized`
- `drops the minimized row once another session becomes active`
- `restoring puts the row back in place and it stays after switching away` — p1 = s1, s4, s5; asserts `['s1','s4','s5']`. ⌘B and back/forward share `restoreSession`.
- `card count and attention still include minimized sessions` — `2 sessions · 1 waiting on you`.
- `a worktree group with one painted member renders as a plain row, still counting the hidden sharer`

`… › keyboard navigation skips minimized things`:
- `every non-active sidebar row is reachable with navSession`

`test/e2e/minimize.spec.ts`:
- `minimized session leaves the sidebar and the tray restores it`

### Verification

From `cmd/hivegui/frontend`:
- `npx vitest run test/dom/minimize-project.test.tsx` (new tests fail before the change)
- `npm test >/dev/null && echo OK`; `npm run typecheck >/dev/null && echo OK`; `npx biome ci . >/dev/null && echo OK`
- `CI=1 npx playwright test test/e2e/minimize.spec.ts test/e2e/nav-history.spec.ts test/e2e/minimize-project.spec.ts`
- `HIVE_SNAPSHOT=1 CI=1 npx playwright test test/e2e/theme.spec.ts`

## Second opinion

- **Round 1** — verdict `revise`, confidence 8. Restore test vacuous (`restoreSession` activates the session); e2e used `s2`/`s3` though the mock names added sessions `mock-N`; restore-path coverage unproven; "absent" assertions lacked a "present" precondition. All 4 applied.
- **Round 2** — verdict `revise`, confidence 8. Nav test placed where `initView` never runs (`navSession` no-op); restore-order assertion couldn't fail with two sessions; `theme.spec.ts` pixel baselines missing from blast radius. All 3 applied, plus nice-to-haves: shared count from the full list, exact card text, `__hive_state.activeId` in e2e, spec #202 note. No third round (loop rule); presented and approved by the operator.

## Decision log

- **2026-09-13** — Remove minimized session rows entirely (no per-card "N minimized" disclosure). Why: operator choice; mirrors minimized projects, tray + ⌘K remain restore paths.
- **2026-09-13** — Keep the active session's row even when minimized. Why: operator choice; otherwise the selection has no row.
- **2026-09-13** — Card header count/attention include minimized sessions. Why: operator choice; a bell on a hidden session still bubbles to its card.
- **2026-09-13** — Assumption: ⌘1–9 hints stay positional over the full list. Why: #252 non-goal; minimized-project sessions already skip digits.
- **2026-09-13** — Assumption: worktree group panels are built from painted rows only; a panel with <2 painted members renders as plain rows. Why: the panel wraps rows, so its count should match what it wraps.

- **2026-09-13** — A worktree group panel's attention badge counts painted members only; a hidden member's bell still reaches the card header and the tray chip. Why: the panel summarises the rows it wraps.
- **2026-09-13** — An expanded card whose sessions are all minimized and inactive shows its header count over an empty list. Why: follows the spec; the tray lists them.
- **2026-09-13** — Dropping a dragged row next to a hidden session places it relative to the target row only. Why: drop indexes are computed over the full list (`clusterDropOps`), so daemon order stays consistent; the hidden session keeps its slot.
- **2026-09-13** — Regenerated only the baselines that render the seeded sidebar (`sidebar-*` ×6, `chrome-classic`). Why: the committed ones already predated the #385 redesign, so they failed on `main` before this change; `settings-classic`, `launcher-*` and `worktrees-*` also fail on `main` for reasons unrelated to #407 and are left untouched.
- **2026-09-13** — `renderRows` takes the painted rows and computes both groupings itself (painted for panels, full for `worktreeShared`). Why: `ProjectItem` had no other use for the grouping.

## Progress

- **2026-09-13** — Issue #407 created; triaged bug/S/P2; research complete.
- **2026-09-13** — Plan approved (chat, after two second-opinion rounds).
- **2026-09-13** — Tests written first; 6 of the new DOM tests failed on the old code. `Sidebar.tsx` filter landed; vitest, typecheck, `biome ci`, targeted e2e green; sidebar pixel baselines regenerated.

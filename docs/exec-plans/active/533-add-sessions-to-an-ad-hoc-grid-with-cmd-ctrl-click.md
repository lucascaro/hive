# Add sessions to an ad-hoc grid with ⌘/Ctrl-click

- **Spec:** [docs/product-specs/533-add-sessions-to-an-ad-hoc-grid-with-cmd-ctrl-click.md](../../product-specs/533-add-sessions-to-an-ad-hoc-grid-with-cmd-ctrl-click.md)
- **Issue:** #533
- **PR:** #534
- **Branch:** feature/533-ad-hoc-grid-select
- **Status:** active

## Summary

Add a third grid scope, `grid-select`, whose tiles are a throwaway set of session ids picked with ⌘/Ctrl-click in the sidebar. GUI-only: no wire change, no `DaemonContract` bump. The *why* lives in the spec.

## Research

### Relevant code (all under `cmd/hivegui/frontend/src`)

- `lib/view.ts:11-47` — `ViewMode` = `single | grid-project | grid-all`, `normalizeView` (localStorage restore), `resolveView` (<2 tiles → single). A third mode must be added to `VALID_VIEWS` / the type.
- `app/grid-layout.ts:277-325` — `gridScopeFor(view, projectId)` is the one place a mode maps to tiles; `gridScopeSessions()` reads `appData().view`. `grid-all` goes through `orderedSessions()` + `filterHidden`; `grid-project` filters by project and `clusterSessions`. The new mode filters `orderedSessions()` by the id set (keeps sidebar order, "One order" rule) and `filterHidden` (minimized sessions drop out → criterion 9).
- `components/GridView.tsx:46-60` — `gridSignature()` already includes `view`, `activeId` and the ordered tile ids, so a selection change repaints with no new subscription.
- `app/view.ts:99-155` `switchTo`, `:355-362` `enforceViewFloor`, `:428-443` `setView` — `setView` runs `resolveView` against `gridScopeFor`, persists the mode to `hive.view`, and `switchTo` retargets `gridProjectId`. `minimizeSession` / `minimizeProject` already call `enforceViewFloor`; kill paths go via `store.removeSession` (`store/store.ts:714`).
- `store/store.ts:104-105, 523-524, 1052-1058` — `view` and `gridProjectId` live in the store; `loadSavedView()` restores `view` from `hive.view`. Persisting `grid-select` would restore a view whose id set is gone, so restore must map it to `single` (spec: session-only).
- `components/Sidebar.tsx:288` — `onSelect={() => p.sidebar.switchTo(id)}`; `SidebarProps` (`:89`) is a referentially stable bag built once in `main.tsx`, `SessionItem` is `memo`'d on primitives (`selected` is a prop → add `inSelection` the same way).
- `components/SessionRow.tsx:205-225` — row `onClick(e)` calls `p.onSelect()`; `data-selected` marks the active row, styled in `theme/components/session-row.css:214,504`. The event is available here, so the modifier check belongs here and `onSelect` gets a `{ toggle }` flag.
- `components/SessionRow.tsx` / `lib/drag-row.ts` — drag uses `dragRowProps`; a ⌘-click does not start a drag, so criterion 8 is a regression test, not new code.
- Settings: `components/modals/Settings.tsx` renders checkboxes (`settings-check`, e.g. `:924`); GUI-local prefs use `writeStorage` keys in `store/store.ts:69-71` (`hive.fontSize`, `hive.activityPanel`). A local-only pref (`hive.gridSelectMode`) fits that pattern and needs no daemon round-trip.
- Keybindings: ⌘G / ⇧⌘G are bindings in `lib/bindings.ts`; the select-then-⌘G option needs ⌘G to open `grid-select` when a selection ≥2 exists. That is a change to the command behind the binding, not a new chord, so the keybindings policy checklist (binding / menu / help / README) is not triggered unless a label changes.
- `lib/status.ts` `modeHints(view, …)` — status-bar mode hints take a `ViewMode`; the new mode needs a case or the hint lies (AGENTS.md).

### Constraints / risks

- Dropping `grid-select` when the set falls below 2 must reuse `enforceViewFloor`, which currently calls `setView('single')` and so would be correct, but it must also clear stale ids.
- Kill/close removes a session from `appData().sessions`; the selection set must be pruned (derive: `gridScopeFor` ignores ids with no session, and the set is pruned on `removeSession`).
- Persisted `hive.view === 'grid-select'` must not restore.
- `rebaselineGridReplayCols` must run on add/remove while in grid, as minimize does.

### Prior lessons

No brain entries matched.

### Conventions card

- Build: `./build.sh`. Tests: `scripts/test.sh` (layers `go`, `unit`, `dom`, `e2e`); run local Playwright with `CI=1`; `npm run typecheck` needs `./scripts/ci-bootstrap.sh` first in a fresh worktree; lint via `biome ci .`.
- Verify by exit code, not grep. Run under the CI Go toolchain only if Go is touched (it is not).
- TDD: every behaviour change ships its test. Pure logic in `lib/` gets a unit test; sidebar/row behaviour gets a dom test; the flow gets an e2e spec against the Wails mock bridge.
- No hard-coded keys in any label; ⌘/Ctrl branching uses `isMac` from `lib/platform.ts`.
- User-visible change → `.changesets/<slug>.md` (`bump: minor`) plus a `site/features.json` entry with `since: "Unreleased"`; README only if defaults/keybinds change. Never edit `CHANGELOG.md` or `docs/product-specs/index.md`.
- Never write a literal skip-CI marker in a commit or PR body.

## Approach

Add a third `ViewMode`, `grid-select`, plus a store field `gridSelection: string[]` (insertion-ordered session ids, never persisted). `gridScopeFor('grid-select')` returns `filterHidden(orderedSessions().filter(s => selection.has(s.id)), ...)` — sidebar order, minimized sessions drop out. Everything downstream (GridView signature, layout, `resolveView`, `enforceViewFloor`) already keys off `gridScopeFor`, so it works unchanged.

Rather than the obvious alternative of keeping a bare boolean "multi-select on" flag next to `grid-project`/`grid-all`, a first-class mode means `resolveView`, status hints, and the view-floor logic stay single-sourced.

Behaviour (pure reducer in `lib/grid-select.ts`, so it is unit-testable without a DOM):
- `toggleSelection(sel, id, activeId)`: seeds with the active session on first toggle (so ⌘-click B while on A → {A,B}); toggling an id removes it; returns the new list.
- `modeAfterToggle(sel, mode, view, autoEnter)`: auto mode (default) → `grid-select` when |sel|≥2, else `single`; select-then-⌘G mode → view unchanged (marks only).
- A command `toggleGridSelection(id)` in `app/view.ts` applies it: writes `gridSelection`, then `setView('grid-select', {persist:false})` or `setView('single')`, plus `rebaselineGridReplayCols()` when already in a grid. In single view with the select-then-⌘G setting, only the marks change.
- Plain click: `switchTo(id)` clears `gridSelection` and, if in `grid-select`, falls to `single` on that session (spec criterion 4; assumption: plain click leaves the grid).
- ⌘G (`toggleGridProject`, `actions.ts:138`): in select-then-⌘G mode with |sel|≥2 and not already in `grid-select`, enter `grid-select`; in `grid-select`, leave to `single`. Otherwise unchanged (criterion 5). ⇧⌘G (`actions.ts:183`) unchanged.
- Prune: `store.removeSession` also filters the id out of `gridSelection`; `minimizeSession` removes it from the selection too; both then run `enforceViewFloor`, which already drops to `single` below two tiles and now also clears a stale selection.
- `loadSavedView` / `normalizeView`: `grid-select` is not in the restorable set (→ `single`); `setView('grid-select')` is called with `persist:false`.
- Setting: `hive.gridSelectMode` (`'auto' | 'manual'`, default `auto`) in localStorage via the store's `writeStorage` pattern, a checkbox/radio in Settings → General with a hint line.
- Sidebar: `SessionRow` onClick passes `{toggle: e.metaKey (mac) || e.ctrlKey (other)}` (via `isMac`) to `onSelect`; `SessionItem` receives `inSelection: boolean`; row gets `data-in-selection` + a CSS rule using existing tokens (`ui-lint` clean). `e.preventDefault()` on toggle clicks (a ⌘-click must not start a drag or focus-select text).
- Status hint: `modeHints('grid-select', …)` shows the leave-grid shortcut derived from the keymap, never a hard-coded key.

### Files to change

1. `src/lib/view.ts` — add `VIEW_GRID_SELECT`, extend `ViewMode`; `normalizeView` does not restore it.
2. `src/lib/grid-select.ts` (new) — pure reducer above.
3. `src/store/store.ts` — `gridSelection` field + `setGridSelection`; prune in `removeSession`; `gridSelectMode` pref + loader/setter.
4. `src/app/grid-layout.ts` — `gridScopeFor` third branch.
5. `src/app/view.ts` — `toggleGridSelection`; `switchTo` clears selection; `minimizeSession`/`minimizeProject` prune; `enforceViewFloor` clears.
6. `src/app/actions.ts` — ⌘G handling for select-then-⌘G.
7. `src/components/SessionRow.tsx`, `Sidebar.tsx`, `main.tsx` (SidebarProps wiring) — modifier detection, `inSelection`.
8. `src/theme/components/session-row.css` — selection mark.
9. `src/components/modals/Settings.tsx` — the setting.
10. `src/lib/status.ts` / `shortcuts.ts` as needed for the new mode's hint.
11. `.changesets/ad-hoc-grid-select.md`, `site/features.json` entry (`since: "Unreleased"`), `README.md` (a Mouse/selection line; no new chord).

### Tests

- unit `test/unit/grid-select.test.ts`: seeds with active; toggle off; <2 → single; manual mode leaves view; order.
- unit `test/unit/view.test.ts`: `normalizeView('grid-select') === 'single'`; `resolveView` floor.
- unit `test/unit/store.test.ts`: `removeSession` prunes selection; `gridSelectMode` default + round trip.
- dom `test/dom/grid-select.test.tsx`: ⌘-click (Ctrl-click with `isMac=false`) on rows A→B opens 2 tiles across projects; C adds; B removes; <2 → single; plain click clears; marks render; drag-reorder dragstart leaves selection unchanged; kill/minimize prune.
- dom `test/dom/settings-grid-select.test.tsx`: option shows default and persists.
- e2e `test/e2e/grid-select.spec.ts` vs the Wails mock bridge (run with `CI=1`): select across two projects, ⌘G in manual mode, ⇧⌘G/⌘G unchanged. The every-shortcut sweep needs no change (no new chord/scope).

### Verification

```
cd cmd/hivegui/frontend && ../../../scripts/ci-bootstrap.sh && npm run typecheck >/dev/null && echo TYPECHECK_OK
CI=1 scripts/test.sh unit dom e2e
npx biome ci . >/dev/null && echo BIOME_OK
scripts/ui-lint.sh >/dev/null && echo UILINT_OK
```
Each new test is written first and shown failing (red) before the change.

### Manual smoke rows

Per `docs/verifying-the-gui-by-hand.md`, driven without a human by `test/e2e/grid-select.spec.ts` against the Wails mock bridge (the real frontend, mock daemon). Not run against a real `hived` (no daemon change).

- Modifier-click sessions to open a grid of exactly those, then add and remove tiles — `modifier-click tiles exactly the selected sessions`. Cross-project selection is covered in `test/dom/grid-select.test.tsx` (the mock boots a single project).
- Remove the active tile and confirm focus lands on a remaining tile — dom tests in `focus never rests on a session with no tile` and `closing a tile through the daemon event`.
- Drag a row after selecting — `a modifier-click starts no drag; a drag leaves the selection alone`.

### Open questions / risks

- `modeHints` and `shortcuts.ts` exhaustiveness: a new `ViewMode` may fail typecheck in switch sites — the typecheck run finds them all.
- Sidebar row click is also used by drag; ⌘-click must not alter `dragstart`.
- Selection ids belonging to a *minimized project* drop out of tiles but stay in the set; they reappear if restored (acceptable; selection is still throwaway).

## Revision 1 (applies round-1 must_fix; supersedes conflicting text above)

- **Clear point:** `switchTo` no longer clears the selection (kill/new-session/⌘1-9/palette/nav all call it). The clear lives in the sidebar plain-click path only (`onSelect({toggle:false})` → `clearGridSelection()` then `switchTo`).
- **Off-grid guard in `switchTo`:** in `grid-select`, if the target id is not a current tile, it is explicit navigation elsewhere → clear the selection and `setView('single')`, then proceed. Kill/close neighbour choice (`app/events.ts:1072,1106`) in `grid-select` picks the next *tile* from `gridScopeSessions()` (as `minimizeSession` does) so killing a tile removes one tile instead of exiting. Tests: dom kill-active-tile-in-3-grid (→ 2 tiles, grid stays); dom ⌘1 to an unselected session (→ single, selection cleared).
- **Selection lifetime:** auto mode — `setView` to any mode other than `grid-select` clears the selection. Manual mode — marks persist across views until a plain click, kill/minimize prune, or an explicit clear. Manual-mode ⌘-click while already in `grid-project`/`grid-all` only marks rows.
- **⌘G / ⇧⌘G while in `grid-select`:** ⌘G → `single` (leaves the ad-hoc grid; in manual mode with ≥2 marks from `single`, ⌘G enters `grid-select`); ⇧⌘G → `grid-all` as today. Recorded in the decision log. Setting hint text states that with select-then-⌘G and ≥2 marks, ⌘G opens the selection until a plain click clears it.
- **Seed edge cases:** ⌘-click on the active session with an empty selection is a no-op (seed {A} then toggle A would empty it; reducer returns `[]`). Seeding skips a null/minimized active session. Unit cases for both.
- **Status hint:** add a `modeHints('grid-select', …)` case in `lib/status.ts` and an assertion in `test/unit/status-hints.test.ts` that the hint is keymap-derived. `lib/empty-state.ts:77-90` gets an explicit `grid-select` branch. Help overlay (`lib/shortcuts.ts`) gets a ⌘/Ctrl-click line.
- **Drag (criterion 8):** asserted in the e2e spec (real ⌘-mousedown starts no dragstart; a drag leaves the selection unchanged), not only jsdom.
- **Verification (runnable from repo root):**
```
./scripts/ci-bootstrap.sh
(cd cmd/hivegui/frontend && npm run typecheck >/dev/null && echo TYPECHECK_OK)
(cd cmd/hivegui/frontend && npx biome ci . >/dev/null && echo BIOME_OK)
CI=1 scripts/test.sh unit dom e2e
scripts/ui-lint.sh >/dev/null && echo UILINT_OK
```

## Revision 2 (applies round-2 must_fix)

- `toggleGridSelection` reassigns the active id when the toggled-off session was active: `setActive` to the next tile from `gridScopeSessions()` (as `minimizeSession`, `view.ts:346-348`); if none remain, the surviving selected session. Dom tests: ⌘-click active tile in a 2-grid → single on B; in a 3-grid → focus on a remaining tile.
- `toggleGridSelection` calls `enforceViewFloor()` after every toggle in both modes, so manual mode cannot strand a one-tile `grid-select`. Test added.
- Exits from `grid-select` use `setView('single', {persist:false})` so the saved view is not overwritten. The plain-click path calls `setView('single')` itself before `switchTo`.
- No e2e-real spec: frontend-only change, no daemon/wire change; mock-bridge e2e only.

## Second opinion

Independent reviewer (Opus subagent), two rounds. Round 1: revise, confidence 8, 5 must-fix, all applied (Revision 1). Round 2: revise, confidence 7, 2 must-fix, both applied (Revision 2); not re-reviewed (skill allows no third round). Revisions below supersede conflicting text above them.

## Decision log

- **2026-10-26** — Setting persists in localStorage (`hive.gridSelectMode`), shown in Settings. Why: GUI-only pref like `fontSize`/`activityPanel`; avoids a wire or `DaemonContract` change. Resolves spec open question 2.
- **2026-10-26** — Exit gestures: ⌘G (as today) and a plain sidebar click. Why: matches the spec's stated assumption and `resolveView`. Resolves open question 1.
- **2026-10-26** — ⌘G from `grid-select` goes to single; ⇧⌘G goes to grid-all. Why: spec assumption was ambiguous; "leave the ad-hoc grid" is least surprising.
- **2026-10-26** — Clear selection in the sidebar plain-click path, not `switchTo`. Why: kill/⌘1-9/palette all call `switchTo`; clearing there would exit the grid when a tile is killed.
- **2026-10-26** — Selection is a store field of session ids, not persisted; `grid-select` is never restored from `hive.view`. Why: spec says throwaway.

- **2026-10-26** — Moved the ad-hoc tile selector to `app/selectors.ts` (`adHocGridSessions`). Why: `app/events.ts` needs it for the kill neighbour, and `app/grid-layout.ts` constructs a `ResizeObserver` at import time, which broke 16 dom suites that import `events.ts`.
- **2026-10-26** — No separate Settings dom test: the option's default, toggle and effect are covered by `grid-select.spec.ts` (e2e) and the store unit test; the checkbox is a plain controlled input.
- **2026-10-26** — Added `isSelectToggle(e, mac)` to `lib/grid-select.ts`. Why: jsdom is non-mac, so the ⌘ half of the rule needs a pure, platform-parameterised seam to be tested.

## Progress

- **2026-10-26** — Research complete.
- **2026-10-26** — Plan approved by operator (chat).
- **2026-10-26** — Implemented on `feature/533-ad-hoc-grid-select`: unit (`grid-select`, `view`, `store`, `status-hints`), dom (`grid-select.test.tsx`, 24 cases), e2e (`grid-select.spec.ts`, 6 cases). Typecheck, `biome ci`, `ui-lint`, unit/dom and e2e green; one unrelated flaky `every-shortcut` plugin-view case passed on retry.

## Open questions

None.

## PR convergence ledger

- **2026-10-26 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 7da2080447a1e9b3b5ebb1dbadc2da7c2782e53ae6575ecf00b1f8526ce60ba4; threads_open: 0; action: autofix+push; head_sha: 34787ed9.
- **2026-10-26 iter 2** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 5 (Greptile/CodeRabbit); action: autofix+push; head_sha: 6c60fa7b.
- **2026-10-26 iter 3** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 7d1c0a5e3b2f4a96; threads_open: 0; action: autofix+push; head_sha: a7fd56c4.
- **2026-10-26 iter 4** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: a7fd56c4.

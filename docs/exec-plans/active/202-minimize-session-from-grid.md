# GUI: minimize sessions to remove from grid view; restore from a tray

- **Spec:** [docs/product-specs/202-minimize-session-from-grid.md](../../product-specs/202-minimize-session-from-grid.md)
- **Issue:** #202
- **Stage:** IMPLEMENT
- **Status:** active

## Summary

Add a frontend-only "minimize" state to sessions so users can hide a tile from the grid without killing the session. Minimized sessions still appear in the sidebar and are reachable in single-session mode. They surface in a small tray strip at the bottom of the terminals area, with click-to-restore. State is held in memory in the Wails renderer; cross-launch persistence is out of scope for v1.

## Research

### Frontend code surface (Wails renderer)

All changes land in `cmd/hivegui/frontend/`. There is no backend / wire change — minimize is a renderer-local UI concept.

- **Grid filter** — `cmd/hivegui/frontend/src/main.js:1600` `gridScopeSessions()` returns the list of sessions to tile. Two view modes:
  - `grid-all` → `orderedSessions()` (every session, line 1601)
  - `grid-project` → `state.sessions.filter(projectId === pid)` (lines 1602–1607)
  - Adding a `!minimized` predicate to both branches removes minimized tiles from the grid.
- **Grid render** — `renderGrid()` at `main.js:1325`. Iterates `gridScopeSessions()`, adds `.in-grid` class to in-scope hosts, removes it from everything else. Already handles "session present but not in grid" correctly (line 1346), so the filter change suffices for hiding.
- **Tile DOM** — built once in `SessionTerm` constructor at `main.js:42–78`. Header (`.tile-header`) currently holds: color dot, name span, worktree glyph, term-title, project. Will add a minimize button at the right end of the header.
- **Tile click → switch** — `main.js:277–293`. `mousedown` on a tile calls `setActive(id)` then re-renders. The minimize button needs `stopPropagation` so clicking it doesn't switch-then-minimize.
- **Single-mode switching** — `main.js:1240` `switchTo(id)` works for any session id; it calls `ensureTerm()` which lazily creates terms. **Minimized sessions are still reachable** via the sidebar, the command palette (`main.js:2024` `switchTo`), and `⌘[/]` cycling (`main.js:1559–1567`) without any special-casing. No change needed there.
- **View state** — `state.view` in `main.js:666` is `'single' | 'grid-project' | 'grid-all'`. We don't add a new view; minimize is orthogonal.
- **Persistence** — `loadSavedView()` (`main.js:671`) shows the pattern for `localStorage`-backed state. v1 spec says minimize state does **not** persist across app launches, so we keep it in `state` only.

### Where the tray goes

- `index.html:64–65` —
  ```
  <main id="terms"></main>
  <div id="status">connecting…</div>
  ```
- Add a `<div id="minimized-tray" class="hidden">` between `#terms` and `#status`. It only renders when `state.minimized.size > 0`.
- Tray rows display: color dot, name, project label (mirrors tile header), restore button. Clicking a tray row restores AND switches to that session.

### Session model

- Frontend session shape (`main.js:654–669`): `state.sessions` is an array of `SessionInfo` wire objects (from `internal/wire/control.go:78`). We do **not** add a field to the wire struct. Instead, track minimized IDs in a renderer-local `Set`:
  ```js
  state.minimized = new Set();  // session IDs hidden from grid
  ```
- All hide/show logic reads `state.minimized.has(id)`. New session events default to "not minimized" (the Set never auto-populates). Session deletion (`session:remove` event) must `state.minimized.delete(id)` to avoid leaks.

### Existing event hooks

- `EventsOn('session:remove', ...)` and friends live around `main.js:1700–1770`. Look for the existing session-removed handler and add the `state.minimized.delete(id)` cleanup there.
- `gridSpatialMove` (`main.js:2534–2538`) navigates the spatial grid; since it iterates `gridLayout.sessions` which comes from `gridScopeSessions()`, minimized tiles are naturally skipped.

### Sidebar

- The sidebar (`#sidebar`, drawn by code starting around `main.js:893`) lists every session. Spec says minimized sessions remain reachable, so the sidebar lists them unchanged. A small "minimized" indicator next to the name (e.g. dimmed style) is a nice-to-have but not required for v1.

### Tests

- `cmd/hivegui/frontend/test/` — Vitest unit tests + Playwright. `lib/grid.js` already has unit tests (`buildGridLayout`). The minimize filter on `gridScopeSessions` is pure → add a unit test there. A Playwright flow test covers click-to-minimize / click-to-restore golden path.

### Constraints / Dependencies

- No backend changes — no `internal/wire/`, `internal/worktree/`, or `cmd/hived/` work.
- xterm terminals for minimized sessions stay attached in the DOM (`state.terms` keeps them) but are hidden because `.in-grid` is absent and `#terms` is in `grid` mode. Memory cost is identical to single-mode behaviour today.
- ResizeObserver fires per-tile when grid layout changes (`main.js:107`). Minimizing a tile triggers a re-layout; remaining tiles' `fit.fit()` runs automatically — no extra refit needed.

## Approach

Renderer-local "minimize" — a per-session boolean held in `state.minimized: Set<string>`. The grid filter excludes minimized sessions, a small button on each tile header sets the flag, and a chip-row tray under the terminals lists minimized sessions with a click-to-restore action. No wire change, no `localStorage` persistence (v1).

Chosen over the obvious alternative of adding a `minimized` field on `SessionInfo` (and a wire roundtrip): the spec explicitly scopes persistence as out-of-scope for v1, and minimize is a viewing concern not a session-lifecycle concern. Keeping it in the renderer means zero protocol churn and no migration cost if we later decide minimize should not persist at all.

**Tray behaviour (resolving open questions):**
- **Chip row** (horizontal), not a collapsible bar. Compact, one-row, matches the visual weight of the status bar above which it sits.
- **Horizontal scroll** when overflow occurs (`overflow-x: auto`), no wrap. Wrapping would push the terminals area up unpredictably; scrolling keeps layout stable.
- **`⌘[/]` cycles all sessions** (current behaviour), not just visible ones. Minimize is a "get this out of my grid for now" affordance; users still expect keyboard cycling to reach everything. This matches how single-mode already works.

### Files to change

1. `cmd/hivegui/frontend/src/main.js`
   - **`state` init (~line 666)** — add `minimized: new Set()` field.
   - **`SessionTerm` constructor (~line 42–78)** — append a `.tile-minimize` button to `.tile-header`. Click handler calls a new `minimizeSession(id)` and `stopPropagation()`s so the surrounding tile-mousedown handler at line 277 doesn't run.
   - **`gridScopeSessions()` (line 1600)** — add `.filter((s) => !state.minimized.has(s.id))` to both `grid-all` and `grid-project` return paths.
   - **New `minimizeSession(id)`** — `state.minimized.add(id)`; if `state.activeId === id`, pick the next visible session in `orderedSessions()` and `setActive()` it (so the focus indicator doesn't vanish); call `renderGrid()` and `renderMinimizedTray()`. If in single mode, do nothing visual — the tile isn't rendered as a grid tile anyway, but we still flip the flag so it disappears from the grid on next switch.
   - **New `restoreSession(id)`** — `state.minimized.delete(id)`; `switchTo(id)` (works in both views); `renderMinimizedTray()`.
   - **New `renderMinimizedTray()`** — toggle `.hidden` on `#minimized-tray`; rebuild its children as chip elements. Each chip = `<button class="min-chip">` with color dot, name, project label, dataset.sid. Single click = `restoreSession(sid)`.
   - **Session removed handler (around `main.js:1700–1770`)** — `state.minimized.delete(ev.session.id)` and call `renderMinimizedTray()` on the session-removed event so leaks don't accumulate.
   - **`setView()` (line 1611)** — call `renderMinimizedTray()` so the tray shows/hides correctly when toggling between grid and single. (Tray should still show in single mode — sessions are minimized from the grid, not from the app.)
   - Initial call: `renderMinimizedTray()` after the first session list load (alongside the existing initial `renderGrid()` / `showSingle()`).

2. `cmd/hivegui/frontend/index.html`
   - Insert `<div id="minimized-tray" class="hidden" aria-label="Minimized sessions"></div>` between `<main id="terms">` (line 64) and `<div id="status">` (line 65).

3. `cmd/hivegui/frontend/src/style.css`
   - `.tile-minimize` — small icon button at right end of tile-header (visually like the existing dead-session close button — see `main.js:325` `.deadCloseBtn` for the pattern). Show a minus / em-dash glyph. Hover affordance only; no need for default visibility tweaks since `.tile-header` is already grid-mode-only.
   - `#minimized-tray` — flex row, `overflow-x: auto`, fixed height (~28px), background matches `#status`, gap between chips. `.hidden { display: none; }` is presumably already defined globally.
   - `.min-chip` — small pill button: color dot, name, project label, close-ish layout consistent with existing tile-header chrome.

4. `CHANGELOG.md`
   - Add to `## [Unreleased] / ### Added`: "Minimize sessions to hide them from the grid view; restore from a tray below the terminals." (Run `/hs-changelog-update` during IMPLEMENT.)

5. `docs/features.md` (if minimize meets the "user-visible feature" bar per AGENTS.md doc maintenance — it does)
   - Add a short paragraph describing the minimize/restore behaviour.

### New files

None. Tray DOM and styles fold into existing files. No new module in `lib/`.

### Tests

Per AGENTS.md: both unit and functional coverage.

**Unit (Vitest, `cmd/hivegui/frontend/test/`)** — extract the filter as a tiny pure helper so it can be tested without the DOM:
- New `cmd/hivegui/frontend/src/lib/minimized.js` exporting `filterMinimized(sessions, minimizedSet)`. (Tiny exception to "no new files" — pure logic belongs in `lib/` per the pattern of `lib/grid.js`.) `gridScopeSessions()` calls it.
- New `cmd/hivegui/frontend/test/minimized.test.js`:
  - `filterMinimized — returns input unchanged when set is empty`
  - `filterMinimized — removes sessions whose id is in the set`
  - `filterMinimized — empty set keeps order stable`
  - `filterMinimized — minimizing all sessions returns empty array`

**Functional (Playwright, `cmd/hivegui/frontend/test/`)** — extend the existing Playwright suite (look for a grid-view spec; if none, create `minimize.spec.js`):
- `minimize hides tile from grid-all view`
- `minimize hides tile from grid-project view`
- `restore from tray returns session to grid and switches to it`
- `minimized session is still focusable in single mode via sidebar`
- `tray is empty / hidden when no sessions are minimized`
- `removing a minimized session also clears it from the tray`

### Risks / edge cases

- **Active session being minimized.** If the active session gets minimized in grid mode, focus moves to the next visible session via `setActive()`. If every session is minimized, the grid renders empty — handled because `renderGrid()` already tolerates `n === 0`. Status bar should still update.
- **`⌘[/]` cycling in grid mode.** Cycling lands on a minimized session → `setActive` highlights it but no tile shows. Mitigation: leave cycling all-inclusive (matches single mode), since cycling-to-minimized is no worse than cycling-to-different-project in `grid-project` mode today.
- **Sidebar click on minimized session.** Already covered by `switchTo` — in grid mode, the session won't appear in the grid; in single mode it shows normally. No special-case needed but document in CHANGELOG so the behaviour isn't surprising.
- **Session removal race.** If a session is removed while minimized, the `session:remove` cleanup must run before `renderMinimizedTray` reads stale entries — order matters in that handler.

## Decision log

- **2026-05-15** — Minimize is renderer-local (no wire change). Why: cross-launch persistence is out of scope for v1 per spec; backend doesn't need to know about UI visibility state.

## Progress

- **2026-05-15** — Spec, exec plan, and research drafted. Stage: RESEARCH.
- **2026-05-15** — Plan approved. Stage: PLAN → IMPLEMENT.
- **2026-05-15** — Implementation complete. New `lib/minimized.js` + 5 unit tests; tile minimize button; `#minimized-tray` chip row; `state.minimized` Set with cleanup on session removal and on `session:list` rebroadcast; CHANGELOG entry. All 71 Vitest tests + 12 Playwright tests pass.

## Open questions

- Should the tray be horizontally scrollable when there are many minimized sessions, or wrap to a second row? Defer to PLAN.
- Visual treatment of the tray: chip row vs. a single collapsible bar? Lean toward chip row (compact, one row, scrolls). Defer to PLAN.
- Should `⌘[/]` cycling include or skip minimized sessions in grid mode? Current `cycleSession` uses `orderedSessions()` (all sessions); in grid mode users may want it to follow the visible set. Defer to PLAN.

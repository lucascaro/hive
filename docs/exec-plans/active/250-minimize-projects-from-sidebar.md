# Minimize whole projects from the sidebar

- **Spec:** [docs/product-specs/250-minimize-projects-from-sidebar.md](../../product-specs/250-minimize-projects-from-sidebar.md)
- **Issue:** —
- **PR:** #290
- **Branch:** feature/250-minimize-projects-from-sidebar
- **Status:** active

## Summary

Add a per-project `–` minimize button to the sidebar. Minimized projects leave the main list, render as name-only chips in a tray pinned to the bottom of the sidebar, and have their sessions excluded from grid views. A `＋` on each chip restores the project to its original slot. State is per-GUI, persisted in `localStorage`.

## Research

- `src/lib/collapsed.ts` — already a key-agnostic string-set localStorage helper (`loadCollapsed` / `serializeCollapsed` / `pruneCollapsed`). Reused under a second key rather than duplicated.
- `src/app/state.ts` — `state.collapsed` shows the load-on-boot / save-on-toggle pattern to mirror; `state.minimized` is the session-level hidden set.
- `src/app/sidebar.ts` — `renderSidebar` / `renderProject`; `.project-actions` holds `+ ⎇ ✎ ✕`. `reorderDroppedProject` computes the new order index over the **full** ordered project list, anchored to a target project — so it stays correct with minimized projects filtered out of the render.
- `src/app/view.ts` — `gridScopeFor` filters `state.minimized` via `lib/minimized.ts:filterMinimized`; `minimizeSession` / `restoreSession` / `enforceViewFloor` own the grid repaint + focus handoff. `view.ts` imports `sidebar.ts` (`updateSidebarSelection`), so `sidebar.ts` must reach the reverse direction through `SidebarDeps`, not a direct import.
- `src/app/keyboard.ts` — `jumpToAttention` / `endRound` / `navGo` branch on `state.minimized.has(id)` and call `restoreSession`.
- `src/app/session-term.ts:281` — the session minimize glyph is `–`; the project button uses the same.
- `src/style.css:718` — `#minimized-tray` chip styling to mirror for project chips.

## Approach

Project minimization is a second hidden-set, kept separate from `state.minimized` (session ids) so restoring a project cannot lose which individual sessions the user had minimized inside it. Everything that asks "is this session visible?" goes through one predicate instead of two ad-hoc checks:

`lib/minimized.ts` gains `filterHidden(sessions, minimizedSessions, minimizedProjects)` — `filterMinimized` plus a project-id check — and `view.ts` gains `isSessionHidden(s)`. `gridScopeFor` uses `filterHidden`; `restoreSession` un-minimizes the session **and** its project, so `keyboard.ts` only has to swap `state.minimized.has(id)` for `isSessionHidden(id)` and its two round-trip paths keep working.

Chosen over the obvious alternative — adding every session of a minimized project to `state.minimized` — because that alternative destroys information (which sessions were individually minimized before) and would need a bookkeeping set to undo, which is strictly more state than one predicate.

The tray is a sibling `<ul>` between `#projects` and the version footer, not a trailing `<li>` inside `#projects`: `#projects` is a plain block scroll container, and turning it into a flex column just to get `margin-top: auto` would change every project row's layout for one alignment trick.

### Files to change

- `src/lib/collapsed.ts` — add `MINIMIZED_PROJECTS_STORAGE_KEY = 'hive.minimizedProjects'`; note in the header that the helpers are key-agnostic.
- `src/lib/minimized.ts` — add `filterHidden(sessions, minimizedSessions, minimizedProjects)`.
- `src/lib/empty-state.ts` — `all-minimized` hint becomes "Restore one from the tray or the sidebar." (a project-minimized scope has no chip in the session tray).
- `src/app/state.ts` — `minimizedProjects: Set<string>` + `attentionRestoredProjects: Set<string>` on `AppState`; load from the new key; `saveMinimizedProjects()`.
- `src/app/sidebar.ts` — split projects into visible/minimized in `renderSidebar`; `–` button in `.project-actions`; `renderMinimizedProjects()` + `renderProjectChip()`; prune dead ids; two new `SidebarDeps` (`minimizeProject`, `restoreProject`).
- `src/app/view.ts` — `isSessionHidden`; `gridScopeFor` uses `filterHidden`; `minimizeProject` / `restoreProject` (focus handoff + `renderGrid` + `enforceViewFloor`, mirroring `minimizeSession`); `restoreSession` also clears the session's project; empty-state input passes the union hidden set.
- `src/app/keyboard.ts` — `jumpToAttention` / `navGo` branch on `isSessionHidden`; `jumpToAttention` records a revealed project in `attentionRestoredProjects`; `endRound` re-minimizes those projects.
- `src/app/main.ts` — inject the two new sidebar deps.
- `index.html` — `<ul id="minimized-projects" class="hidden">` between `#projects` and `<footer id="sidebar-hints">`.
- `src/style.css` — `#minimized-projects` + `.min-project-chip` rules.
- `CHANGELOG.md` / `.changesets/` — user-visible change entry.

### New files

- `test/unit/minimized-projects.test.ts` — storage + `filterHidden`.
- `test/dom/minimize-project.test.ts` — sidebar behavior.

### Tests

- unit: `filterHidden` drops sessions minimized directly, sessions in a minimized project, and neither when both sets are empty; tolerates `snake_case` / `camelCase` project id spellings and null sets.
- unit: new-key round-trip through `loadCollapsed`/`serializeCollapsed`; `pruneCollapsed` drops deleted project ids; malformed JSON degrades to empty.
- e2e `test/e2e/minimize-project.spec.ts`: the tray's box really sits between the project list and the version footer — a jsdom test cannot see layout, and the bottom-pinning depends on `#projects` taking the free space.
- dom `minimize-project.test.ts`: minimize moves the row into the tray; restore returns it to its original index; state persists to `localStorage`; tray hidden when empty; chip gets `.active` for the current project; drag-drop reorder past a minimized project calls `UpdateProject` with the same index it would have with nothing minimized.

## Decision log

- **2026-08-29** — Added one e2e spec after all, against the plan's "none". Why: the bottom-pinning is pure CSS and jsdom reports zero-sized boxes, so nothing else could have caught a tray that floats under the last row.
- **2026-08-29** — `attention-jump.test.ts` and `nav-history.test.ts` mock `view.js`, so both needed a real-shaped `isSessionHidden` in their factories rather than a `vi.fn()` stub; a hard-coded false would have silently stopped testing the branch keyboard.ts takes.

- **2026-08-29** — Project-minimize is a separate set from `state.minimized`, unified behind `isSessionHidden` / `filterHidden`. Why: folding sessions into `state.minimized` would lose per-session minimize state on restore.
- **2026-08-29** — Tray is a sibling `<ul>`, read with the non-throwing `pageEl`. Why: existing jsdom tests mount partial sidebar markup and must not fail on an element they never exercise.

## Review findings addressed (iter 1)

- **BLOCKING** — `switchToProject` / `shiftActiveProject` could activate a session inside a still-minimized project while in a grid view, leaving the selection on a tile the filter removes and handing keystrokes to an invisible terminal. Fixed with one shared guard, `fallBackToSingleIfActiveHidden()`, called from both: single mode ignores the hidden filter, so dropping to it preserves "selecting a minimized project does not restore it" while keeping the view coherent.
- **IMPORTANT** — the chip body was a bare `<li>` with a click handler, so switch-without-restore was mouse-only. It is now a real `<button type="button">`, matching the session tray's chip.
- **IMPORTANT** — the prune moved out of `renderSidebar` (which runs before the first project list arrives and would have pruned against an empty `state.projects`) into the `project:list` / `project:event` handlers, beside the existing `collapsed` prune.
- **IMPORTANT** — `test/dom/keyboard-arrows.test.ts`'s `view.js` mock gained `minimizeProject` and a behavior-mirroring `isSessionHidden`; its own comment makes listing every imported export the file's invariant.
- **IMPORTANT** — added four grid-mode dom tests (focus handoff on minimize, both fall-back paths, and the no-op case), the branch every earlier test skipped by running in single mode.
- **MINOR (declined)** — `hiddenSessionIds()` → `renderEmptyState` has no test because the `all-minimized` state is unreachable through project minimize: `minimizeProject` ends in `enforceViewFloor()`, which drops to single before the model could return that kind.

## Progress

- **2026-08-29** — Plan-first scaffold; stage = IMPLEMENT.
- **2026-08-29** — Review iter 1: REQUEST_CHANGES (1 BLOCKING, 5 IMPORTANT). All addressed except one declined MINOR; unit/dom/e2e green (229 dom tests).
- **2026-08-29** — PR #290 opened; stage = REVIEW.
- **2026-08-29** — Implemented. go + unit + dom + e2e layers green (17 new unit/dom tests, 2 new ⌘B round-trip tests, 1 e2e layout test); `biome ci .` and `tsc --noEmit` clean. Layout verified in Chromium.

## PR convergence ledger

<Append-only. One line per review-loop iteration.>

- **2026-08-29 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; threads_open: 1; action: fixes applied + push; head_sha: pending.

## Open questions

Empty.

# GUI: Cmd+Enter in grid mode focuses the active session

- **Spec:** [docs/product-specs/327-cmd-enter-grid-focus-session.md](../../product-specs/327-cmd-enter-grid-focus-session.md)
- **Issue:** #327
- **PR:** #328
- **Branch:** feature/327-cmd-enter-grid-focus-session
- **Status:** completed

## Summary

Bind ⌘⏎ / Ctrl+Enter, only while a grid view is active, to switch to single view on the active tile. In single view the key is deliberately left unclaimed so it reaches the terminal for agent CLIs. Small additive change to the existing ⌘/Ctrl shortcut chain plus the shortcut documentation surfaces.

## Research

Authored via plan-first mode. Findings from the pre-plan pass:

- `cmd/hivegui/frontend/src/app/keyboard.ts` — single capture-phase `window` keydown listener. `const meta = cmdOrCtrl(e); if (!meta) return;` (~line 301) gates the ⌘/Ctrl chord chain; every ⌘ binding lives below it as an `if`/`else if` on `e.key`. `swallow()` is the local preventDefault+stopPropagation helper. **⌘⏎ is currently unbound** — `grep -n "Enter" app/keyboard.ts` finds only the dead-session overlay handler at :253.
- `cmd/hivegui/frontend/src/app/view.ts:410` — `setView(view, opts)` resolves below-floor grids back to `single`, writes the store inside `withLayout`, then calls `deps.focusActiveTerm()` and a deferred scroll-to-bottom. So "focus the session" needs no extra focus work here.
- `cmd/hivegui/frontend/src/lib/shortcuts.ts` — single source of truth for shortcut *text* (help overlay + command palette). Its header documents the five-file drift surface for a binding change: handler, this file, palette table in `main.tsx`, `cmd/hivegui/menu_darwin.go`, `README.md`. `KEYS.enter` already renders `↩` / `Enter`, so `m('enter')` works with no helper change.
- `cmd/hivegui/frontend/test/dom/keyboard-arrows.test.ts` — existing jsdom harness that mocks `bridge.js` and `view.js` (including `setView`) and dispatches synthetic ⌘-chord keydowns via a `press()` helper with a platform-correct `primary` modifier. Directly extensible for this binding.
- Spec #217 (`docs/product-specs/217-cmd-enter-newline-not-submit.md`) is the "previous decision" being partly reverted: it kept ⌘⏎ out of the terminal because the key was then the grid-project toggle. That toggle has since moved to ⌘G/⇧⌘G.

### Constraints

- Must not swallow ⌘⏎ in `single` view — agent CLIs (Claude, Codex) bind it, and #217's Shift+Enter work exists precisely because app-level interception of Enter chords breaks agent input.
- The binding sits below the modal/rename guards at the top of the listener, so an open Launcher/Settings/rename keeps its own Enter handling for free.

## Approach

Add one early-return branch to the ⌘/Ctrl chain in `keyboard.ts`, placed with the other `return`-style bindings (after `isHelpOverlayKey`), keyed on `e.key === 'Enter'`:

- If `e.shiftKey` → return without swallowing (⇧⌘⏎ stays unclaimed).
- If `appData().view === 'single'` → return without swallowing, so the key reaches xterm.
- Otherwise `swallow()` + `setView('single')`.

Chosen over the obvious alternative — reusing `toggleProjectGrid()` / a symmetric toggle — because a toggle necessarily claims ⌘⏎ in single view too, which is the exact conflict #217 documented. A one-way branch keeps the terminal's half of the key free, and reads as "zoom in", not "toggle".

`setView('single')` already restores terminal focus and snaps to bottom, so no focus code is added here.

### Files to change

1. `cmd/hivegui/frontend/src/app/keyboard.ts` — new ⌘⏎ branch in the ⌘/Ctrl chord chain, with a comment naming the one-way rationale and #217.
2. `cmd/hivegui/frontend/src/lib/shortcuts.ts` — add `{ keys: m('enter'), label: 'Grid: focus the active session (single view)' }` to the `View` group in `shortcutGroups()`.
3. `README.md` — add a row to the shortcut table (~line 132) next to the ⌘G row.
4. `cmd/hivegui/frontend/test/dom/keyboard-arrows.test.ts` — new describe block (see Tests).
5. `.changesets/` — a changeset entry; the binding is user-visible.

### New files

- `.changesets/<pr>-cmd-enter-grid-focus.md` — changelog entry for the new shortcut.

### Tests

In `cmd/hivegui/frontend/test/dom/keyboard-arrows.test.ts`, a new `describe('cmd+Enter focuses the active session from grid', …)` using the existing `press()` helper and the `setView` mock (added to the destructured mocks at the top of the file):

- `it('cmd+Enter in grid-all switches to single view')` — `state.view = 'grid-all'`; asserts `e.defaultPrevented === true` and `setView` called with `'single'`.
- `it('cmd+Enter in grid-project switches to single view')` — same for `grid-project`.
- `it('cmd+Enter in single view is left to the terminal')` — `state.view = 'single'`; asserts `defaultPrevented === false` and `setView` not called. This is the regression guard for #217's constraint.
- `it('shift+cmd+Enter is not claimed in grid')` — asserts `defaultPrevented === false` and `setView` not called.

## Open questions / risks

- The `single` case returns without `swallow()`, so the event continues to the terminal — verified against the pattern already used for horizontal ⌘-arrows (`handleArrow` returning `false`).
- No native menu item is added. `menu_darwin.go` accelerators intercept ⌘ chords before the webview on macOS, so *not* registering ⌘⏎ there is required for the key to reach this handler at all; it also keeps the terminal's single-view half working.

## Gate verdict

- **2026-09-03** — verdict: FAIL; checks: 2 dimensions passed / 1 failed / 0 followups; followups: none; one-line: an existing e2e spec still asserted the pre-#327 "⌘Enter is unbound" behavior and would have gone red in CI.
  - 2026-09-03 dimensions:
    - acceptance — PASS — every success criterion traced in code and demonstrated by `test/dom/keyboard-arrows.test.ts` (20 passed), including the single-view carve-out and the ⇧⌘⏎ exclusion.
    - non-goals — PASS — `menu_darwin.go` and `docs/product-specs/index.md` untouched; `keymap.ts` / `session-term.ts` diffs are comment-only, `isShiftEnter` / `NEWLINE_SEQ` byte-identical to main; no session-id mutation, view-only.
    - doc accuracy — FAIL — `test/e2e/cmd-enter-unbound.spec.ts` was left asserting the old behavior (its grid-mode test required NO view change). Also reported a missing `CHANGELOG.md` entry; that part is a false positive — AGENTS.md's own Documentation section says `CHANGELOG.md` is generated and `block-generated-edits` fails any PR touching it, so `.changesets/` is the correct surface and it was present.

- **2026-09-03** — verdict: PASS; checks: 3 dimensions passed / 0 failed / 0 followups; followups: none; one-line: re-run after the e2e spec was rewritten; all success criteria traced, no non-goal bleed, docs consistent.
  - 2026-09-03 dimensions:
    - acceptance — PASS — carried over from the first run; the fix touched only test and doc surfaces, no production code path.
    - non-goals — PASS — carried over from the first run for the same reason.
    - doc accuracy — PASS — `cmd-enter-unbound.spec.ts` gone, `cmd-enter-grid-focus.spec.ts` passes (4 tests), AGENTS.md item 4 now self-consistent with its Documentation section, repo-wide sweep finds no live prose still calling ⌘⏎ unbound.

## Decision log

- **2026-09-03** — One-way (grid → single) instead of a symmetric toggle. Why: a toggle would have to claim ⌘⏎ in single view, re-creating the agent-input conflict documented in spec #217. Confirmed with the operator at plan time.
- **2026-09-03** — No native macOS menu item. Why: a menu accelerator would swallow ⌘⏎ before the webview in *every* view, breaking the single-view carve-out.
- **2026-09-03** — Command-palette entry added after review (`focus-active-session`). Why: the original plan lumped the palette in with the native menu, but only the menu carries an accelerator that would swallow the key; AGENTS.md's Keybindings Policy step 2 requires both the overlay and the palette. The action was extracted to an exported `focusActiveSession()` so the key path and the palette path cannot drift — same reason `toggleProjectGrid` exists.

## Progress

- **2026-09-03** — Plan-first scaffold; stage = IMPLEMENT (set in spec frontmatter).
- **2026-09-03** — Gate FAIL; `test/e2e/cmd-enter-unbound.spec.ts` still asserted #249's "unbound" behavior. Rewritten and renamed to `cmd-enter-grid-focus.spec.ts` (4 tests, all passing), and AGENTS.md's Keybindings Policy item 4 corrected — it told contributors to edit the generated `CHANGELOG.md`, contradicting the same file's Documentation section and misleading the gate.

## PR convergence ledger

- **2026-09-03 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 23c5cf74; threads_open: 0; action: stop; head_sha: 6c7124e. Two IMPORTANT drift findings (stale `keymap.ts` / `session-term.ts` comments asserting ⌘⏎ is inert; missing command-palette entry) applied by hand rather than by autofix, since the loop stopped on COMMENT with strict off.
- **2026-09-03** — Gate PASS on re-run; all three dimensions clean. Stage = DONE; plan moved to completed/.

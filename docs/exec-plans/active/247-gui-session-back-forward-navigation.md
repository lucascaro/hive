# GUI: back / forward navigation between sessions (Ctrl+- / Ctrl+Shift+-)

- **Spec:** [docs/product-specs/247-gui-session-back-forward-navigation.md](../../product-specs/247-gui-session-back-forward-navigation.md)
- **Issue:** —
- **PR:** #248
- **Branch:** feature/247-gui-session-back-forward-navigation
- **Stage:** REVIEW
- **Status:** active

## Summary

Adds a VS Code-style back/forward stack over visited sessions to the GUI, bound to Ctrl+- / Ctrl+Shift+- on macOS and Ctrl+Alt+- / Ctrl+Alt+Shift+- on Windows and Linux. The whole feature hangs off one hook in `setActive` — the sole writer of `state.activeId` — which is what makes it cheap and, more importantly, complete: paths that bypass `switchTo` (tile clicks, grid spatial move) are recorded for free.

## Research

Authored via plan-first mode. Code references gathered during plan iteration:

- `src/app/focus.js:25` — `setActive(id)`, the **sole writer** of `state.activeId`. Its own comment (`:21-24`) states it centralizes "the focused session changed" for every code path.
- `src/app/view.js:46` — `switchTo(id)`, the full orchestrator (`setActive` → `ensureTerm` → grid retarget → `showSingle`/`renderGrid` → sidebar → status → title → focus). Not a valid hook point: four paths reach `setActive` without it — tile mousedown (`src/app/session-term.js:386`), `gridSpatialMove` (`src/app/view.js:262,269`), `shiftActiveProject` (`src/app/view.js:288`), `minimizeSession` (`src/app/view.js:329`).
- Three direct `state.activeId = null` writes bypass `setActive` entirely: `src/app/view.js:114`, `src/app/view.js:293`, `src/app/events.js:255`. All three mean "no active session" (empty project / just-killed session), which is deliberately not a history entry.
- `src/app/keyboard.js:47` — the single global `keydown` listener, **capture phase**, so `preventDefault` + `stopPropagation` beats xterm's `attachCustomKeyEventHandler` (`src/app/session-term.js:282`) to the event.
- `src/app/keyboard.js:153` — `const meta = cmdOrCtrl(e); if (!meta) return;`. `cmdOrCtrl` (`src/lib/platform.js:18`) is `⌘ && !ctrl` on mac, `ctrl && !⌘` elsewhere — so on macOS **plain Ctrl never passes this gate**. Any Ctrl-only binding must be dispatched before it, like the Ctrl+\` block at `:147`.
- `src/app/keyboard.js:156-170` — zoom on `=`/`+`, `-`/`_`, `0`. On Windows/Linux these *are* `Ctrl+-`/`Ctrl+=`, which is why this feature takes Ctrl+Alt there.
- `src/app/keyboard.js:376` — `jumpBack()`, the closest existing analogue; its `state.sessions.some(...)` still-exists guard at `:378` is reused here.
- `src/lib/shortcuts.js:1-6` — the drift contract: handler, this file, `src/main.js` palette table, `cmd/hivegui/menu_darwin.go`. A fifth surface not named there: the GUI shortcut table in `README.md:98-115`.
- `src/app/events.js:237-257` (`removed`) and `:213-217` (`session:list` reconcile) — the two prune points. Note `splice` happens at `:248`, *after* the sibling `delete` calls at `:238-240`.
- AGENTS.md "Keybindings Policy" (`:172`) is TUI-scoped — its four surfaces are `internal/config/`, `internal/tui/components/settings.go`, and `docs/keybindings.md`, none of which apply (the last does not exist in this tree). Its changelog clause does apply.

## Approach

Hook `setActive` rather than `switchTo`. `setActive` records a **departure**: before overwriting `state.activeId`, push the outgoing id onto the back stack. A module-level suppress flag (`withoutNavHistory`) lets the back/forward handlers replay history without re-recording it.

Chosen over the obvious alternative of wrapping `switchTo`, which reads cleaner but silently misses tile clicks and grid arrow-nav — the exact cases the user asked to cover ("no matter how").

### Files to change

- `cmd/hivegui/frontend/src/app/state.js` — add `nav: { back: [], fwd: [] }`; in-memory only, unlike `collapsed`.
- `cmd/hivegui/frontend/src/app/focus.js` — `pushNav` call in `setActive` before the assignment; export `withoutNavHistory(fn)`.
- `cmd/hivegui/frontend/src/lib/keymap.js` — `navHistoryKey(e, isMac)` → `'back' | 'forward' | null`.
- `cmd/hivegui/frontend/src/app/keyboard.js` — dispatch before the `cmdOrCtrl` gate at `:153`; `navBack()` / `navForward()` handlers near `jumpBack`.
- `cmd/hivegui/frontend/src/app/events.js` — `pruneNav` after the `splice` in the `removed` branch, and in the `session:list` reconcile.
- `cmd/hivegui/frontend/src/lib/shortcuts.js` — Sessions group + `paletteShortcuts`; needs a local render helper (Ctrl on mac, Ctrl+Alt elsewhere) since neither `mod()` nor `ctrl()` covers it.
- `cmd/hivegui/frontend/src/main.js` — `nav-back` / `nav-forward` palette commands.
- `README.md` — GUI shortcut table at `:98-115`; both chords spelled out, since the table's "Ctrl replaces ⌘" footnote does not describe this binding.
- `CHANGELOG.md` — `[Unreleased]` → `### Added`.

Not touched: `cmd/hivegui/menu_darwin.go`. Ctrl-only chords are deliberately JS-only here (Ctrl+\` precedent, `src/app/keyboard.js:143-146`), and no menu accelerator uses `-` without ⌘.

### New files

- `cmd/hivegui/frontend/src/lib/nav-history.js` — pure, no DOM, matching the `lib/collapsed.js` / `lib/minimized.js` idiom. `NAV_CAP = 50`, `pushNav`, `goBack`, `goForward`, `pruneNav`.

### Tests

- `test/unit/nav-history.test.js` — push ignores null and consecutive dupes; a push after a back truncates `fwd`; cap drops oldest; `goBack`/`goForward` skip ids failing `exists` and return `null` when empty; back→forward round-trips; `pruneNav` clears both arrays.
- `test/unit/keymap.test.js` (extend) — mac Ctrl+- → back, Ctrl+Shift+- → forward, ⌘- → null; **linux plain Ctrl+- → null (zoom regression guard)**, Ctrl+Alt+- → back, Ctrl+Alt+Shift+- → forward; `_` and `e.code === 'Minus'` both recognized.
- `test/dom/nav-history.test.js` — A→B→C then two backs and a forward; the tile-mousedown path (`setActive` without `switchTo`) is recorded; history navigation does not itself push; a killed top-of-stack session is skipped.
- `test/unit/shortcuts.test.js` already asserts no duplicate combos per group; new entries must pass unchanged.

## Decision log

- **2026-07-25** — Hook `setActive`, not `switchTo`. Why: four selection paths bypass `switchTo`, including tile clicks.
- **2026-07-25** — Per-platform chord (Ctrl on mac, Ctrl+Alt on Win/Linux) rather than one chord everywhere. Why: `Ctrl+-` is already zoom-out on Win/Linux via `cmdOrCtrl`; taking it would be a regression. VS Code splits the same way.
- **2026-07-25** — In-memory only, no localStorage. Why: the terminals are gone after a restart anyway, and persisting adds a startup prune against the live session list for no user benefit.
- **2026-07-25** — Grid ⌘-arrow walking pushes one entry per cell. Why: it is the literal reading of "no matter how"; the 50-entry cap bounds the cost.

## Progress

- **2026-07-25** — Plan-first scaffold; stage = IMPLEMENT.
- **2026-07-25** — Review fix: `withoutNavHistory` is injected via `initKeyboard` deps rather than statically imported, restoring the acyclic-modules invariant stated at main.js's wiring block (keyboard.js must not import the focus pipeline). Added `test/e2e/nav-history.spec.js` so the macOS chord (plain Ctrl+-) runs through the real listener in a real browser — jsdom reports a non-mac navigator and could only ever exercise the Ctrl+Alt+- branch.
- **2026-07-25** — Implemented on `feature/247-gui-session-back-forward-navigation`. All nine files changed as planned plus `src/lib/nav-history.js`. Checks: `npx vitest run` 32 files / 309 tests pass (28 in `keymap.test.js`, 16 in `unit/nav-history.test.js`, 12 in `dom/nav-history.test.js`); full `wails build` succeeds (frontend + Go); `go build ./...` and `go test ./cmd/hivegui/...` pass; full `wails build` produces a working `.app`.

## PR convergence ledger

<Append-only. One line per review iteration.>

## Open questions

- **Not verified: WKWebView delivery of plain `Ctrl+-` in the packaged `.app`.** The logic and the dispatch placement are covered by `test/e2e/nav-history.spec.js` running in real Chromium on darwin (where `isMac` is true, so the actual shipping mac chord is pressed), but the shipped runtime is WKWebView, not Chromium. Driving the packaged app was blocked: `osascript`/System Events is not authorized in this environment (`-1743`), and the GUI has no CLI for creating sessions headlessly. Strong precedent that it works: `Ctrl+\`` is an existing shipped plain-Ctrl binding on the same capture-phase listener, dispatched in the same pre-gate position (`src/app/keyboard.js:143-151`). A 30-second manual check on a machine with Accessibility granted would close this.

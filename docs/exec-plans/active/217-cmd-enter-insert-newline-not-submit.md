# GUI: Shift+Enter inserts a newline (not submit) in agent sessions

- **Spec:** [docs/product-specs/217-cmd-enter-insert-newline-not-submit.md](../../product-specs/217-cmd-enter-insert-newline-not-submit.md)
- **Issue:** #217
- **Stage:** REVIEW
- **Status:** active
- **PR:** #218
- **Branch:** feature/217-cmd-enter-insert-newline-not-submit

## Summary

Shift+Enter inside an agent session submits the prompt instead of inserting a newline, because xterm sends a bare `\r` and the Shift modifier is dropped. The fix intercepts Shift+Enter in the GUI's existing custom key handler and writes the byte that both Claude Code and Codex universally accept as "insert newline": Ctrl+J (`\x0a`). The original report named Cmd+Enter, but that key is already the grid-project toggle (see Decision log); Shift+Enter is the conflict-free, cross-platform newline key.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/main.js:243` — `this.term.attachCustomKeyEventHandler((e) => …)` (the handler block spans ~`main.js:239`–`290`). xterm.js keeps exactly **one** custom key handler, so every app-level key binding must live in this single closure. It already intercepts macOS Cmd+Backspace → `\x15` (`main.js:249`), Ctrl+` (`main.js:270`), and Ctrl+Shift+C/V/A (`main.js:280`). Each interception calls `e.preventDefault()` (where needed) and `return false` to suppress xterm's default, then writes a custom sequence via `this._writePty(...)`. This is the exact extension point for the new Shift+Enter branch. (Line numbers reflect the post-change file.) Note: Cmd/Ctrl+Enter never reaches this handler — the capture-phase window handler at `main.js:2768` consumes it first; Shift+Enter does reach it.
- `cmd/hivegui/frontend/src/main.js:331` — `this._writePty(data)` TextEncoder-encodes `data`, base64s it, and calls Wails `WriteStdin(id, …)`. `this.term.onData(...)` (`main.js:337`) is what normally forwards Enter as `\r`.
- `cmd/hivegui/frontend/src/lib/platform.js` — `isMac` (computed once) and `detectMac(nav)` / `cmdOrCtrl(e, mac)` pure helpers, unit-tested in `test/unit/platform.test.js`. Establishes the repo idiom: **pure key-decision logic lives in `lib/` and is unit-tested with fake event objects**, while `main.js` holds the imperative handler.

### What byte to send (the central question)

Authoritative sources for both agents converge on **Ctrl+J = `\x0a` (LF)** as the newline-insert byte that works in every terminal with no per-terminal configuration:

- Claude Code docs: to add a line break without submitting, press **Ctrl+J** (or type `\` then Enter) — "works in every terminal with no setup." Shift+Enter requires CSI-u extended-keys (`\x1b[13;2u`); Option+Enter requires mapping Option→`Esc+` (`\x1b\r`). Both are terminal-config-dependent and brittle.
- Codex CLI: "sends the prompt on ENTER and inserts a newline on **Ctrl+J**." Shift+Enter / Alt+Enter newline support is regression-prone across versions (codex issues #20580, #21562, #3024).

Sending `\x0a` for Shift+Enter is therefore agent-agnostic and depends on no terminal feature flags — strictly more robust than emulating Option+Enter (`\x1b\r`) or the CSI-u Shift+Enter encoding. `\x0a` is exactly the byte a real Ctrl+J keypress produces.

### Constraints / dependencies

- Must remain the **single** `attachCustomKeyEventHandler` (a second registration silently replaces the first — see the comment at `main.js:239`).
- Gate to macOS + Cmd only (`isMac && e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey`), per the spec's non-goals (no remap of Shift/Option+Enter, no change on non-mac platforms). Plain Enter (no modifier) must keep falling through to xterm's default `\r` submit.
- Numpad Enter reports `e.key === 'Enter'` with `e.code === 'NumpadEnter'`; keying off `e.key === 'Enter'` covers both.

### Testing approach (per repo conventions)

- **Unit (vitest):** extract a pure `isCmdEnter(e, mac)` predicate + the `NEWLINE_SEQ` constant into a `lib/` module, tested with fake event objects exactly like `test/unit/platform.test.js`.
- The inline `main.js` handler stays a thin imperative wrapper that calls the pure predicate, mirroring how `platform.js` logic is consumed by `main.js`.

## Approach

Add one branch to the existing single `attachCustomKeyEventHandler` closure (`main.js:243`) that intercepts **Shift+Enter** and writes `\x0a` (Ctrl+J) to the PTY instead of letting xterm submit `\r`. The decision logic (predicate + byte constant) is extracted into a new pure, unit-tested `lib/keymap.js` module, matching the `platform.js` idiom; the inline handler stays a thin wrapper.

Shift+Enter (not Cmd/Ctrl+Enter) is the chosen key for two reasons: (1) it is the cross-platform "newline in a chat input" convention, and (2) Cmd/Ctrl+Enter is already the grid-project toggle, handled by a **capture-phase** window keydown handler (`main.js:2768`, closes `}, true);` at `main.js:2825`) that `stopPropagation()`s the event before it reaches the terminal — so an xterm-layer Cmd+Enter intercept is structurally dead. Shift+Enter carries no Cmd/Ctrl modifier, so that window handler's `if (!cmdOrCtrl(e)) return` guard ignores it and the key reaches xterm. The byte `\x0a` (Ctrl+J) is chosen over Option+Enter (`\x1b\r`) or the CSI-u Shift+Enter encoding because Ctrl+J is the only newline byte both Claude Code and Codex accept with zero terminal configuration (see Decision log).

### Files to change

- `cmd/hivegui/frontend/src/main.js` — import `{ isShiftEnter, NEWLINE_SEQ }` from `./lib/keymap.js`; add a branch in the custom key handler (after the Cmd+Backspace branch) that does `e.preventDefault(); this._writePty(NEWLINE_SEQ); return false;` when `isShiftEnter(e)`.

### New files

- `cmd/hivegui/frontend/src/lib/keymap.js` — `export const NEWLINE_SEQ = '\x0a'` and `export function isShiftEnter(e)` returning `e.shiftKey && !e.metaKey && !e.ctrlKey && !e.altKey && e.key === 'Enter'`. Platform-independent (no `isMac` gate).
- `cmd/hivegui/frontend/test/unit/keymap.test.js` — vitest unit tests using fake event objects, like `test/unit/platform.test.js`.
- `cmd/hivegui/frontend/test/e2e/shift-enter-newline.spec.js` — mock-Wails E2E: Shift+Enter writes `0x0a` and does not change view; plain Enter writes `0x0d`.

### Tests

- `keymap.test.js`:
  - `isShiftEnter` true for `{shiftKey:true, key:'Enter'}`.
  - false for plain Enter (no modifier) — preserves submit.
  - false when meta / ctrl / alt is also held — non-goal guard.
  - false for Shift + a non-Enter key.
  - true for NumpadEnter (`key:'Enter'`, `code:'NumpadEnter'`).
  - platform-independent (no `isMac` gate).
  - `NEWLINE_SEQ === '\x0a'` — locks the exact byte sent to the agent.
- `shift-enter-newline.spec.js` (E2E): Shift+Enter → stdin `[0x0a]`, view unchanged; plain Enter → stdin `[0x0d]`.

## Decision log

- **2026-06-09** — Chose `\x0a` (Ctrl+J) over `\x1b\r` (Option+Enter) and `\x1b[13;2u` (Shift+Enter CSI-u). Why: Ctrl+J is the only newline byte both Claude Code and Codex accept with zero terminal-feature configuration; the alternatives depend on Option-as-Meta or extended-keys being enabled and are documented as regression-prone.
- **2026-06-09** — Switched the trigger from Cmd+Enter to **Shift+Enter**. Why: Cmd/Ctrl+Enter is already the grid-project toggle, consumed by a capture-phase window handler (`main.js:2768`) that `stopPropagation()`s the event before xterm sees it — so the original Cmd+Enter intercept was provably dead (verified via E2E: ⌘Enter toggled the view and wrote zero bytes to the PTY, the handler never ran). Shift+Enter has no Cmd/Ctrl modifier, reaches xterm, is the cross-platform chat-newline convention, and does not disturb the existing grid-project shortcut. User-directed decision.

## Progress

- **2026-06-09** — Spec scaffolded (#217), triaged bug/S/P2, research complete. Stage = RESEARCH.
- **2026-06-09** — Plan approved. Stage = IMPLEMENT.
- **2026-06-09** — Implemented: new `lib/keymap.js` (`isCmdEnter`, `NEWLINE_SEQ`), handler branch in `main.js`, `test/unit/keymap.test.js` (7 tests). Vitest 110/110 pass. CHANGELOG `[Unreleased]` updated. Go embed/vite build require Wails codegen (absent in plain checkout) — vitest is the authoritative gate for this frontend-only change.
- **2026-06-09** — Pushed `feature/217-cmd-enter-insert-newline-not-submit`; opened PR #218. Stage = REVIEW.
- **2026-06-09** — Review-loop iter 1 = APPROVE. Resolved 4 Copilot threads (stale `main.js:NNN` refs my diff shifted). CI investigation: Linux/macOS "Build, Vet & Test" was red, but pre-existing on `main` (proven: main@56252de fails identically; reverting this change locally still fails). Root cause: #212 added `ClipboardGetText`/`SetClipboardText` imports to `main.js` without updating `test/e2e/wails-mock.js` **and** `test/e2e-real/wails-bridge.js`; the missing ESM exports threw at module load, breaking all mock-Wails + ws-bridge E2E tests. Per user, folded both one-file harness fixes into this PR (mock-Wails 33 failed → 32 passed; ws-bridge 2 passed).
- **2026-06-09** — While stabilizing a "flaky" macOS focus test, discovered the original Cmd+Enter intercept was structurally dead (capture-phase window handler at `main.js:2768` owns Cmd/Ctrl+Enter for the grid-project toggle and `stopPropagation()`s it before xterm). Per user, reworked the trigger to **Shift+Enter** (`isShiftEnter`, platform-independent). Verified end-to-end: Shift+Enter → PTY `0x0a`, no view change; plain Enter → PTY `0x0d`. Added `test/e2e/shift-enter-newline.spec.js`. Vitest 110/110.

## Open questions

<Empty — newline byte and extension point are settled.>

## PR convergence ledger

<Append-only. One line per review-loop iteration.>

- **2026-06-09 iter 1** — verdict: APPROVE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 00cba6d.
- **2026-06-09** — CI note: required check CodeQL = pass. Non-required "Build, Vet & Test (Linux/macOS)" Playwright E2E jobs fail, but they fail identically on `main`@56252de (pre-existing, every failing test is a focus/scrollback/minimize/smoke boot-timeout — none touch key handling). PR is MERGEABLE (state UNSTABLE due to the pre-existing reds only). Frontend vitest 110/110 green.

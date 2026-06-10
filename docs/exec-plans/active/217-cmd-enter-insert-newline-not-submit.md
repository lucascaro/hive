# GUI: Cmd+Enter inserts a newline (not submit) in agent sessions on macOS

- **Spec:** [docs/product-specs/217-cmd-enter-insert-newline-not-submit.md](../../product-specs/217-cmd-enter-insert-newline-not-submit.md)
- **Issue:** #217
- **Stage:** REVIEW
- **Status:** active
- **PR:** #218
- **Branch:** feature/217-cmd-enter-insert-newline-not-submit

## Summary

On macOS, Cmd+Enter inside an agent session submits the prompt instead of inserting a newline, because the terminal sends a bare `\r` and the Cmd modifier is not encoded. The fix intercepts Cmd+Enter in the GUI's existing custom key handler and writes the byte that both Claude Code and Codex universally accept as "insert newline": Ctrl+J (`\x0a`).

## Research

### Relevant code

- `cmd/hivegui/frontend/src/main.js:243` — `this.term.attachCustomKeyEventHandler((e) => …)` (the handler block spans ~`main.js:239`–`290`). xterm.js keeps exactly **one** custom key handler, so every app-level key binding must live in this single closure. It already intercepts macOS Cmd+Backspace → `\x15` (`main.js:249`), Ctrl+` (`main.js:270`), and Ctrl+Shift+C/V/A (`main.js:280`). Each interception calls `e.preventDefault()` (where needed) and `return false` to suppress xterm's default, then writes a custom sequence via `this._writePty(...)`. This is the exact extension point for Cmd+Enter — the new Cmd+Enter branch is at `main.js:259`. (Line numbers reflect the post-change file.)
- `cmd/hivegui/frontend/src/main.js:331` — `this._writePty(data)` TextEncoder-encodes `data`, base64s it, and calls Wails `WriteStdin(id, …)`. `this.term.onData(...)` (`main.js:337`) is what normally forwards Enter as `\r`.
- `cmd/hivegui/frontend/src/lib/platform.js` — `isMac` (computed once) and `detectMac(nav)` / `cmdOrCtrl(e, mac)` pure helpers, unit-tested in `test/unit/platform.test.js`. Establishes the repo idiom: **pure key-decision logic lives in `lib/` and is unit-tested with fake event objects**, while `main.js` holds the imperative handler.

### What byte to send (the central question)

Authoritative sources for both agents converge on **Ctrl+J = `\x0a` (LF)** as the newline-insert byte that works in every terminal with no per-terminal configuration:

- Claude Code docs: to add a line break without submitting, press **Ctrl+J** (or type `\` then Enter) — "works in every terminal with no setup." Shift+Enter requires CSI-u extended-keys (`\x1b[13;2u`); Option+Enter requires mapping Option→`Esc+` (`\x1b\r`). Both are terminal-config-dependent and brittle.
- Codex CLI: "sends the prompt on ENTER and inserts a newline on **Ctrl+J**." Shift+Enter / Alt+Enter newline support is regression-prone across versions (codex issues #20580, #21562, #3024).

Sending `\x0a` for Cmd+Enter is therefore agent-agnostic and depends on no terminal feature flags — strictly more robust than emulating Option+Enter (`\x1b\r`) or Shift+Enter (CSI-u). `\x0a` is exactly the byte a real Ctrl+J keypress produces.

### Constraints / dependencies

- Must remain the **single** `attachCustomKeyEventHandler` (a second registration silently replaces the first — see the comment at `main.js:239`).
- Gate to macOS + Cmd only (`isMac && e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey`), per the spec's non-goals (no remap of Shift/Option+Enter, no change on non-mac platforms). Plain Enter (no modifier) must keep falling through to xterm's default `\r` submit.
- Numpad Enter reports `e.key === 'Enter'` with `e.code === 'NumpadEnter'`; keying off `e.key === 'Enter'` covers both.

### Testing approach (per repo conventions)

- **Unit (vitest):** extract a pure `isCmdEnter(e, mac)` predicate + the `NEWLINE_SEQ` constant into a `lib/` module, tested with fake event objects exactly like `test/unit/platform.test.js`.
- The inline `main.js` handler stays a thin imperative wrapper that calls the pure predicate, mirroring how `platform.js` logic is consumed by `main.js`.

## Approach

Add one branch to the existing single `attachCustomKeyEventHandler` closure (`main.js:243`) that intercepts macOS Cmd+Enter and writes `\x0a` (Ctrl+J) to the PTY instead of letting xterm submit `\r`. The decision logic (predicate + byte constant) is extracted into a new pure, unit-tested `lib/keymap.js` module, matching the `platform.js` idiom; the inline handler stays a thin wrapper. Chosen over emulating Option+Enter (`\x1b\r`) or Shift+Enter (CSI-u) because Ctrl+J is the only newline byte both Claude Code and Codex accept with zero terminal configuration (see Decision log).

### Files to change

- `cmd/hivegui/frontend/src/main.js` — import `{ isCmdEnter, NEWLINE_SEQ }` from `./lib/keymap.js`; add a branch in the custom key handler (after the Cmd+Backspace branch at `main.js:249`, landing at `main.js:259`) that does `e.preventDefault(); this._writePty(NEWLINE_SEQ); return false;` when `isCmdEnter(e)`.

### New files

- `cmd/hivegui/frontend/src/lib/keymap.js` — `export const NEWLINE_SEQ = '\x0a'` and `export function isCmdEnter(e, mac = isMac)` returning `mac && e.metaKey && !e.ctrlKey && !e.altKey && !e.shiftKey && e.key === 'Enter'`. Imports `isMac` from `./platform.js`.
- `cmd/hivegui/frontend/test/unit/keymap.test.js` — vitest unit tests using fake event objects, like `test/unit/platform.test.js`.

### Tests

- `keymap.test.js`:
  - `isCmdEnter` true for `{metaKey:true, key:'Enter'}` on mac.
  - false for plain Enter (no modifier) — preserves submit.
  - false when ctrl / alt / shift is also held — non-goal guard.
  - false on non-mac (`mac=false`) even with `metaKey` — platform gate.
  - true for NumpadEnter (`key:'Enter'`, `code:'NumpadEnter'`).
  - `NEWLINE_SEQ === '\x0a'` — locks the exact byte sent to the agent.

## Decision log

- **2026-06-09** — Chose `\x0a` (Ctrl+J) over `\x1b\r` (Option+Enter) and `\x1b[13;2u` (Shift+Enter CSI-u). Why: Ctrl+J is the only newline byte both Claude Code and Codex accept with zero terminal-feature configuration; the alternatives depend on Option-as-Meta or extended-keys being enabled and are documented as regression-prone.

## Progress

- **2026-06-09** — Spec scaffolded (#217), triaged bug/S/P2, research complete. Stage = RESEARCH.
- **2026-06-09** — Plan approved. Stage = IMPLEMENT.
- **2026-06-09** — Implemented: new `lib/keymap.js` (`isCmdEnter`, `NEWLINE_SEQ`), handler branch in `main.js`, `test/unit/keymap.test.js` (7 tests). Vitest 110/110 pass. CHANGELOG `[Unreleased]` updated. Go embed/vite build require Wails codegen (absent in plain checkout) — vitest is the authoritative gate for this frontend-only change.
- **2026-06-09** — Pushed `feature/217-cmd-enter-insert-newline-not-submit`; opened PR #218. Stage = REVIEW.
- **2026-06-09** — Review-loop iter 1 = APPROVE. Resolved 4 Copilot threads (stale `main.js:NNN` refs my diff shifted). CI investigation: Linux/macOS "Build, Vet & Test" was red, but pre-existing on `main` (proven: main@56252de fails identically; reverting this change locally still fails). Root cause: #212 added `ClipboardGetText`/`SetClipboardText` imports to `main.js` without updating `test/e2e/wails-mock.js`; the missing ESM exports threw at module load, breaking all 33 mock-Wails E2E tests. Per user, folded the one-file mock fix into this PR (33 failed → 32 passed locally).

## Open questions

<Empty — newline byte and extension point are settled.>

## PR convergence ledger

<Append-only. One line per review-loop iteration.>

- **2026-06-09 iter 1** — verdict: APPROVE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 00cba6d.
- **2026-06-09** — CI note: required check CodeQL = pass. Non-required "Build, Vet & Test (Linux/macOS)" Playwright E2E jobs fail, but they fail identically on `main`@56252de (pre-existing, every failing test is a focus/scrollback/minimize/smoke boot-timeout — none touch key handling). PR is MERGEABLE (state UNSTABLE due to the pre-existing reds only). Frontend vitest 110/110 green.

# GUI: unbind ⌘/Ctrl+Enter from the grid toggle

- **Spec:** [docs/product-specs/249-unbind-cmd-enter-grid-toggle.md](../../product-specs/249-unbind-cmd-enter-grid-toggle.md)
- **Issue:** —
- **Status:** active

## Summary

Delete the ⌘/Ctrl+Enter → grid-project toggle from the renderer, the macOS View menu, the shortcuts panel, and the README. No replacement behavior: the chord becomes app-inert and whatever xterm does with a meta-modified Enter is what happens (in practice, nothing).

## Research

Verified against the Playwright Wails-mock harness (`cmd/hivegui/frontend/test/e2e/`) on 2026-08-28: ⌘Enter toggles single ⇄ grid-project in both directions and writes zero bytes to the PTY, so today's behavior is exactly what the code says.

Relevant code:

- `cmd/hivegui/frontend/src/app/keyboard.ts:334-340` — the `else if (e.key === 'Enter')` branch inside the capture-phase window keydown handler, behind the `cmdOrCtrl(e)` gate at line 264. `swallow()` calls `preventDefault()` + `stopPropagation()`, which is why the key never reaches xterm.
- `cmd/hivegui/frontend/src/lib/shortcuts.ts:150` — `{ keys: m('enter'), label: 'Toggle grid ⇄ single' }` in the View group of the help panel. `enter` is also a key label used by unrelated rows (line 182 `⇧↩`, line 196 `Confirm`), so only the View row goes; the `enter` entry in the label map at line 37 stays.
- `cmd/hivegui/menu_darwin.go:91-92` — `view.AddText("Toggle Grid (⌘↩ alternate)", keys.CmdOrCtrl("enter"), emit("menu:toggle-project-grid"))`. This is a duplicate emitter for the same event as the ⌘G item at line 87, so removing it needs no handler change in `app/events.ts`. On macOS an AppKit accelerator is matched before the webview sees the keydown, so this line must go or ⌘Enter keeps toggling regardless of the renderer edit.
- `README.md:97` — `| ⌘Enter | Toggle grid / single |` in the shortcut table.

Constraints / non-issues:

- `cmd/hivegui/menu_darwin_test.go` has no assertion naming the alternate item (`TestSessionMenuAttentionItems`, `TestMenuHasNoArrowLeftRightAccelerators` only), so no Go test needs editing — only re-running.
- `test/e2e/focus.spec.ts:75` (`single → grid-project preserves keyboard focus`) drives `${MOD}+Enter` to reach grid mode. It must be re-pointed at ⌘G, otherwise it fails; its subject is the focus pipeline, not the binding.
- Shift+Enter (#217, `lib/keymap.ts` `isShiftEnter` / `NEWLINE_SEQ`) is orthogonal — it carries no Cmd/Ctrl, so it never entered this branch. `test/e2e/shift-enter-newline.spec.ts` stays green untouched.
- `docs/product-specs/217-*.md` documents ⌘Enter as the grid toggle in prose. Historical record of a shipped decision; left as-is, with the reversal recorded here and in spec 249's Notes.

## Approach

Straight deletion at all four sites rather than remapping the chord to a PTY byte. The user asked for unbind-only, and any pass-through would need an explicit `_writePty` in the xterm custom key handler (xterm emits nothing for meta+Enter on its own) — code with no requested behavior behind it.

The macOS menu line is the load-bearing removal: with the AppKit accelerator still registered, deleting the renderer branch alone would leave ⌘Enter toggling grid on the platform where it was reported.

### Files to change

1. `cmd/hivegui/frontend/src/app/keyboard.ts` — delete the `else if (e.key === 'Enter')` branch (lines 334-340) and its comment.
2. `cmd/hivegui/menu_darwin.go` — delete the `view.AddText("Toggle Grid (⌘↩ alternate)", ...)` call (lines 91-92).
3. `cmd/hivegui/frontend/src/lib/shortcuts.ts` — delete the `Toggle grid ⇄ single` row (line 150).
4. `README.md` — delete the `⌘Enter` row from the shortcut table (line 97).
5. `cmd/hivegui/frontend/test/e2e/focus.spec.ts` — re-point the `single → grid-project preserves keyboard focus` test from `${MOD}+Enter` to `${MOD}+g`; rename it accordingly.
6. `.changesets/` — one changeset for the user-visible shortcut removal.

### New files

- `cmd/hivegui/frontend/test/e2e/cmd-enter-unbound.spec.ts` — regression coverage for the removal.

### Tests

- `cmd-enter-unbound.spec.ts` → `⌘Enter in single mode does not enter grid` — boot 2 sessions, press `${MOD}+Enter`, assert `#terms` class is unchanged and still not `/grid/`.
- `cmd-enter-unbound.spec.ts` → `⌘Enter in grid mode does not maximize` — enter grid via `${MOD}+g`, press `${MOD}+Enter`, assert still `/grid/`.
- `cmd-enter-unbound.spec.ts` → `⌘G still toggles grid` — the control that proves the deletion did not take the working binding with it.
- `focus.spec.ts` → existing focus test re-pointed at ⌘G (edit, not new).
- `shift-enter-newline.spec.ts` → unchanged; must stay green (Shift+Enter still 0x0a, Enter still 0x0d).
- `go test ./cmd/hivegui/...` → existing menu tests must stay green after the menu line is removed.

## Decision log

- **2026-08-28** — Unbind only; no pass-through byte for ⌘Enter. Why: explicitly chosen by the user at the loop's behavior gate; a pass-through would require new `_writePty` code in the xterm handler with no requested behavior behind it.
- **2026-08-28** — Numbered 249, not 248. Why: no GitHub issue was created for this feature, and 248 is already a PR number in this repo.

## Progress

- **2026-08-28** — Spec written, triaged bug / S / P2, research verified against the e2e harness.
- **2026-08-28** — Implemented on `feature/249-unbind-cmd-enter-grid-toggle`: all four bindings removed, `focus.spec.ts` re-pointed at ⌘G, `cmd-enter-unbound.spec.ts` added, CHANGELOG entry under Unreleased → Changed. New tests confirmed red against the pre-fix branch before being green after it. Checks: `go build ./...`, `go test ./cmd/hivegui/...`, `biome ci .`, `npm run typecheck`, `scripts/test.sh unit dom e2e` (185 passed, 1 skipped) — all pass.

## Open questions

None.

# Natural text editing keybindings in the terminal

- **Spec:** [docs/product-specs/362-natural-text-editing-keybindings.md](../../product-specs/362-natural-text-editing-keybindings.md)
- **Issue:** #362
- **PR:** #363
- **Branch:** `feature/362-natural-text-editing-keybindings`
- **Status:** active

## Summary

Close the two remaining gaps in hive's macOS terminal text-editing keymap —
⌥⌦ (kill word forward) and ⌘⌦ (kill to end of line) — and document the
terminal-level editing keys that already work but appear on no keybinding
surface.

## Research

### What already works (verified against `@xterm/xterm@5.5.0`'s
`evaluateKeyboardEvent`, decompiled from `node_modules/@xterm/xterm/lib/xterm.js`)

| Chord | Source | Bytes today |
|---|---|---|
| ⌥⌫ | xterm `case 8`: `key = DEL; if (altKey) key = ESC + key` | `\x1b\x7f` ✅ |
| ⌘⌫ | `src/app/session-term.ts:442-453` | `\x15` ✅ |
| ⌥← / ⌥→ | xterm `case 37/39` special-cases `ESC[1;3D` → `ESC + (isMac ? 'b' : '[1;5D')` | `\x1bb` / `\x1bf` ✅ |
| ⌘← / ⌘→ | `macLineEditSeq`, `src/lib/keymap.ts:67-72` | `\x01` / `\x05` ✅ |
| ⌦ | xterm `case 46`, no modifiers | `\x1b[3~` ✅ |
| ⌘A | xterm default branch, `keyCode 65 && isMac && metaKey` → `type = 1` (selectAll) | ✅ |
| ⌘C / ⌘V | native AppKit Edit menu, `cmd/hivegui/menu_darwin.go:92` | ✅ |
| ⇧⏎ | `isShiftEnter`, `src/lib/keymap.ts:74-78` | `\x0a` ✅ |

Seven of the nine chords in the iTerm2 "Natural Text Editing" preset are
therefore already covered — most of them by xterm.js itself, not by hive.

### The gaps

1. **⌥⌦** — xterm `case 46` computes `ESC[3;<mods+1>~`, so this sends
   `\x1b[3;3~`. No shell, readline configuration or agent CLI binds it. iTerm
   sends `\x1bd` (meta-d, `kill-word` forward).
2. **⌘⌦** — same branch, sends `\x1b[3;9~`. Should be `\x0b` (Ctrl+K,
   `kill-line`), the exact mirror of the ⌘⌫ → `\x15` binding hive already has.
3. **Documentation** — `⌥⌫`, `⌥←/→` and `⌦` appear on no keybinding surface.
   `shortcuts.ts`'s "Inside a terminal" group (lines 176-190) lists only
   ⌃⇧C/V/A, ⇧⏎ and ⌘⌫.

### Relevant code

- `cmd/hivegui/frontend/src/lib/keymap.ts` — pure predicates, unit-testable
  with fake events. `KeyEventLike` at :29-36; `LINE_START_SEQ`/`LINE_END_SEQ`
  at :45-46; `macLineEditSeq` at :48-72 — the exact shape to mirror.
- `cmd/hivegui/frontend/src/app/session-term.ts:435-500` — the single
  `attachCustomKeyEventHandler`. xterm keeps only ONE, so every app-level
  binding lives here. `macLineEditSeq` is dispatched at :459-464.
- `cmd/hivegui/frontend/src/lib/shortcuts.ts` — single source of truth for the
  help overlay (`shortcutGroups`, :94) and command palette (`paletteShortcuts`,
  :213). "Inside a terminal" group at :176-190.
- `cmd/hivegui/frontend/test/unit/keymap.test.ts` — `ev({...})` fake-event
  helper at :13-22; `describe('macLineEditSeq')` at :192-222 is the pattern to
  copy.
- `README.md:214-240` — Keybinds table.

### Constraints

- Forward delete is `e.key === 'Delete'` (keyCode 46), *not* `'Backspace'`.
  Getting this wrong silently rebinds ⌫.
- `session-term.ts`'s custom key handler **is** covered end to end:
  `test/e2e/line-edit-keys.spec.ts` and `test/e2e/shift-enter-newline.spec.ts`
  boot a focused terminal and assert the bytes on the wire via
  `window.__hive.stdinText()`. That is the only layer where "not intercepted"
  and "actually works" are distinguishable, so the new chords need a spec
  there too, not just a predicate unit test.
- `test/unit/shortcuts.test.ts:47,50` pins the literal help-overlay label
  `'Clear input line'`, so relabelling that row is a two-file change.
- Per AGENTS.md › Keybindings Policy a binding change must update keymap, help
  overlay + command palette, README and a changeset. The palette holds only
  *dispatchable app commands* — raw terminal-editing keys have no command id
  and none are listed there today, so the palette is correctly untouched.

### Prior lessons

`brain-search` returned no hits for keybinding / keymap / terminal / xterm.
No prior lessons matched.
## Approach

One new pure predicate in `lib/keymap.ts`, dispatched from the existing single
`attachCustomKeyEventHandler` in `session-term.ts` immediately after
`macLineEditSeq`. This is the same shape as every terminal binding hive already
owns, which keeps the decision logic unit-testable with fake events and keeps
the imperative wiring to three lines.

The obvious alternative — a second `attachCustomKeyEventHandler`, or handling
these in the window-level `app/keyboard.ts` handler — is wrong on both counts:
xterm keeps only ONE custom handler (a second registration silently replaces the
first, already noted at `session-term.ts:432`), and `app/keyboard.ts` gates on
Cmd/Ctrl in a way that would swallow ⌥⌦ entirely.

`macForwardKillSeq` deliberately handles both chords rather than adding two
predicates: they share the `e.key === 'Delete'` guard, and splitting them would
duplicate the "is this the forward-delete key on a Mac" test that is the part
easiest to get wrong.

Shift is allowed through, matching `macLineEditSeq`'s existing reasoning: ⇧⌘⌦
selects-to-end-of-line in a text field, a PTY has no selection, and the kill is
the closest honest thing. Ctrl is not allowed — ⌃⌦ belongs to the shell.

### Files to change

1. `cmd/hivegui/frontend/src/lib/keymap.ts` — add after `macLineEditSeq` (~:73):

   ```ts
   // Bytes for the two forward-delete kills. \x1bd is meta-d (readline
   // kill-word, forward); \x0b is Ctrl+K (readline kill-line, to end).
   export const KILL_WORD_FORWARD_SEQ = '\x1bd';
   export const KILL_LINE_FORWARD_SEQ = '\x0b';

   export function macForwardKillSeq(
     e: KeyEventLike,
     isMac: boolean,
   ): string | null {
     if (!isMac || e.ctrlKey || e.key !== 'Delete') return null;
     if (e.metaKey && !e.altKey) return KILL_LINE_FORWARD_SEQ;
     if (e.altKey && !e.metaKey) return KILL_WORD_FORWARD_SEQ;
     return null;
   }
   ```

   With a doc comment in the house style covering: why xterm's own
   `\x1b[3;3~` / `\x1b[3;9~` are useless, why `'Delete'` and not `'Backspace'`,
   why mac-only, and why shift passes but ctrl does not.

2. `cmd/hivegui/frontend/src/app/session-term.ts` — import `macForwardKillSeq`
   and dispatch it directly after the `macLineEditSeq` block (~:464):

   ```ts
   const forwardKill = macForwardKillSeq(e, isMac);
   if (forwardKill) {
     e.preventDefault();
     this._writePty(forwardKill);
     return false;
   }
   ```

3. `cmd/hivegui/frontend/src/lib/shortcuts.ts` — two edits.

   **a.** Add a `delete` entry to `KEYS` (:36-43), which today has no such key,
   so `keyLabel('delete', …)` falls through its `k ? … : key` default and
   renders the literal string `delete` on both platforms:

   ```ts
   delete: { mac: '⌦', other: 'Del' },
   ```

   **b.** Extend the "Inside a terminal" group (:176-190) so it documents the
   whole terminal editing keymap, not just the two new chords. Mac-only rows go
   through the existing `...(isMac ? [...] : [])` spread already used for ⌘⌫;
   cross-platform rows use `keyLabel`/`arrowSeq` so non-mac renders words, not
   glyphs.

   Cross-platform:
   - `⌦` / `Del` — Delete the character after the cursor
   - `⌃←/→` (`Ctrl+Left/Right`) — Move by word

   Mac-only:
   - `⌥⌫` — Delete the word before the cursor
   - `⌥⌦` — Delete the word after the cursor  *(new)*
   - `⌘⌦` — Delete to end of line  *(new)*
   - `⌥←/→` — Move by word

   Plus relabel the existing `⌘⌫` row from "Clear input line" to "Delete to
   start of line", which is what `\x15` actually does.

4. `cmd/hivegui/frontend/test/unit/shortcuts.test.ts:43-52` — the relabel in (3)
   breaks the `mac-only clear-line entry appears only on mac` test, which pins
   the literal `'Clear input line'` on both branches. Update both assertions to
   the new label, and extend the same test to assert the ⌦ row renders `⌦` on
   mac and `Del` off it — otherwise the `KEYS` addition has no guard and a
   regression to the literal `delete` ships silently.

5. `README.md` — Keybinds table (:214-240): explicit rows, one per chord, not a
   pointer at the overlay (the spec's success criterion asks for them *listed*):
   `⌥⌫` delete word before cursor · `⌥⌦` delete word after cursor · `⌘⌫`
   delete to start of line · `⌘⌦` delete to end of line · `⌥←/⌥→` move by word
   · `⌦` forward delete. Marked macOS in the Action column the way the
   existing ⌘←/⌘→ row already is; that row stays as is.

6. `.changesets/362-natural-text-editing.md` — new. Frontmatter `type: added`,
   `bump: minor`, `issue: 362`, `pr: null` (backfilled at PR time). `type` and
   `bump` are the only required keys and both values are in the allowed sets
   (`scripts/regen-generated.py:42,153-159`).

### New files

- `.changesets/362-natural-text-editing.md` — changeset entry.

### Tests

`cmd/hivegui/frontend/test/unit/keymap.test.ts`, a new
`describe('macForwardKillSeq')` block placed after the `macLineEditSeq` one
(:192-222), reusing that file's `ev({...})` helper:

- `it('maps opt+forward-delete to kill-word and cmd+forward-delete to kill-line on mac')`
  — `macForwardKillSeq(ev({altKey:true, key:'Delete'}), true)` is
  `KILL_WORD_FORWARD_SEQ`; the `metaKey` variant is `KILL_LINE_FORWARD_SEQ`.
- `it('sends the readline bytes, not a CSI sequence')` — asserts
  `KILL_WORD_FORWARD_SEQ === '\x1bd'` and `KILL_LINE_FORWARD_SEQ === '\x0b'`.
  This is the assertion that fails if someone "simplifies" to `\x1b[3;3~`.
- `it('does not fire off mac')` — both chords with `isMac === false` are null.
- `it('ignores Backspace — it must keep xterm’s own \\x1b\\x7f / hive’s \\x15')`
  — `ev({altKey:true, key:'Backspace'})` and `ev({metaKey:true, key:'Backspace'})`
  are both null on mac. This is the regression guard for the one mistake that
  would silently rebind ⌫.
- `it('ignores a bare forward delete so xterm’s \\x1b[3~ still reaches the PTY')`
  — `ev({key:'Delete'})` is null.
- `it('does not fire when ctrl is held')` — `ev({ctrlKey:true, altKey:true, key:'Delete'})`
  is null.
- `it('passes shift through, matching macLineEditSeq')` —
  `ev({shiftKey:true, metaKey:true, key:'Delete'})` is `KILL_LINE_FORWARD_SEQ`.
- `it('ignores opt+cmd together — an ambiguous chord must not guess')` —
  `ev({altKey:true, metaKey:true, key:'Delete'})` is null.

`cmd/hivegui/frontend/test/e2e/forward-kill-keys.spec.ts` — **new**, modelled
line for line on `test/e2e/line-edit-keys.spec.ts`: same `isMac` guard +
`test.skip`, same `bootFocusedTerminal(page)` helper, same
`expect.poll(() => page.evaluate(() => window.__hive.stdinText()))` assertion.

- `test('⌥⌦ sends kill-word-forward and ⌘⌦ sends kill-to-end-of-line')` — type
  `some text`, press `Alt+Delete`, poll for `some text\x1bd`; then press
  `Meta+Delete`, poll for `some text\x1bd\x0b`.
- `test('the keys still do not change the active session')` — the same guard
  `line-edit-keys.spec.ts` carries, since ⌘-modified keys are exactly the ones
  the window handler could swallow.

This spec is the one that actually fails if the dispatch is mis-wired — placed
after an earlier `return true`, or importing the wrong predicate. The unit
tests pin the *decision*; only this pins the *bytes on the wire*. (The
`sends the readline bytes` unit test asserts a constant against itself: it is a
drift guard against someone "simplifying" the constant, and is deliberately not
the coverage that closes the wiring hole.)

No dom test: the predicate layer and the e2e wire layer between them leave
nothing for jsdom to add.

### Verification

```
cd cmd/hivegui/frontend && npm run typecheck && npm run ci
scripts/test.sh unit dom e2e
scripts/ui-lint.sh --strict
scripts/check-changeset.sh
```

`--strict` matters: `ui-lint.sh` exits 0 in its default warn mode, so the bare
invocation would pass on anything. `check-changeset.sh` is the local mirror of
the changesets CI gate. `e2e` is in the test layers because the new spec lives
there; it runs against the Wails **mock** bridge, and per the repo's Playwright
note it is invoked with `CI=1` so a stale vite dev server can't be reused.

Manual, on macOS, in a running session: type a line, press ⌥⌦ mid-line (the word
after the cursor disappears), press ⌘⌦ (the rest of the line disappears), then
confirm ⌫ and ⌥⌫ still delete backwards.

## Open questions / risks

- **⌘⌦ ergonomics.** On a laptop keyboard forward delete is `fn+⌫`, so this
  chord is physically `⌘fn⌫` — awkward. It ships because it is the mirror of the
  existing ⌘⌫ and the iTerm preset the request cites, not because it will see
  heavy use.
- **`\x0b` under an inner TUI.** In vim or a full-screen program Ctrl+K is
  digraph-insert, not kill-line. The same is already true of the shipped
  ⌘⌫ → `\x15`, so this adds no new class of problem.
- **Alternative ruled out:** `\x1b[3;3~`-style CSI with a shell-side keybinding
  (`bindkey` in zshrc). Rejected — it needs per-user shell config, which is
  precisely what the "no per-terminal configuration" rule in `keymap.ts`'s
  existing comments exists to avoid.
- **Declined refactor, surfaced not silent:** the inline ⌘⌫ → `\x15` block
  (`session-term.ts:439-453`) predates `keymap.ts` and could move next to the
  new predicate so all four mac line-edit chords live at one testable
  boundary. Not done here — it is a behaviour-neutral move of shipped code and
  belongs in its own diff, where a regression is attributable. Say so if you
  want it folded in.
- `.changesets/README.md` is referenced by `CONTRIBUTING.md:66` but does not
  exist. Pre-existing, unrelated, not fixed here.

## Second opinion

Two reviewer rounds, both `revise`. Five must-fix items, all applied.

**Round 1** — verdict `revise`, confidence 8:

- The draft claimed no e2e coverage existed for `session-term.ts`'s custom key
  handler. False — `test/e2e/line-edit-keys.spec.ts` and
  `test/e2e/shift-enter-newline.spec.ts` already drive it end to end via
  `window.__hive.stdinText()`. Fixed in the draft and in Research › Constraints;
  a new e2e spec is now part of the plan.
- The ⌘⌫ help-overlay relabel breaks `test/unit/shortcuts.test.ts:47,50`, which
  pins the literal `'Clear input line'`. Now listed as its own file to change.
- README needed explicit per-chord rows, not a pointer at the overlay.
- `scripts/ui-lint.sh` exits 0 in its default warn mode, so the verification
  command was vacuous. Now `--strict`, plus `scripts/check-changeset.sh`.

**Round 2** — verdict `revise`, confidence 7. Confirmed all four landed, and
found one more: `KEYS` in `src/lib/shortcuts.ts:36-43` has no `delete` entry, so
`keyLabel('delete', …)` falls through to the literal string `delete`. Applied,
with a test guarding it.

Disposition: all five applied; presented after round 2 without a third round,
per the skill's one-retry rule. The reviewer independently confirmed that
`e.key === 'Delete'` is the correct discriminator, that nothing in
`app/keyboard.ts` or `menu_darwin.go` claims ⌘⌦ or ⌥⌦, and that
`paletteShortcuts` is correctly untouched.

## Decision log

- **2026-09-06** — Skip Option-as-Meta (`macOptionIsMeta`). Why: operator call
  at clarifying round A. Enabling it breaks `[ ] { } \ @ #` on non-US Mac
  layouts; a settings toggle is more surface than the two chords warrant.
- **2026-09-06** — Forward delete keeps xterm's `\x1b[3~`, not iTerm's `0x04`.
  Why: `0x04` is Ctrl+D, which is delete-char in readline but EOF on an empty
  line — it can close the user's shell. `\x1b[3~` is bound by readline, zsh,
  vim and the agent CLIs.
- **2026-09-06** — macOS only. Why: on Windows/Linux Ctrl+⌦ already emits
  `\x1b[3;5~`, which readline binds as kill-word; adding bindings there risks
  colliding with tmux.

- **2026-09-06** — Deviated from the plan on one help-overlay row: ⌃←/→ "Move
  by word" is listed for Windows/Linux **only**, not cross-platform as drafted.
  Why: on macOS ⌃← is Mission Control's space switcher and never reaches the
  app, so advertising it there would document a binding that does not work.
  ⌥←/→ is the mac row and was already planned.

## Progress

- **2026-09-06** — Spec filed as #362, triaged S / P2.
- **2026-09-06** — Plan approved by the operator after two reviewer rounds.
- **2026-09-06** — PR #363 opened.
- **2026-09-06** — Implemented; all checks green (typecheck, `biome ci`,
  `scripts/test.sh unit dom e2e` = 628 + 278 passed, `ui-lint.sh --strict`,
  `check-changeset.sh`).

## PR convergence ledger

_(append-only, one line per review-loop iteration)_

- **2026-09-06** — Review: `COMMENT`, 0 BLOCKING / 1 IMPORTANT / 0 MINOR, 0
  unresolved threads. The IMPORTANT: the Sessions group's ⌘/Ctrl+←/→ row claimed
  "in focused mode these reach the terminal (start / end of line)" on both
  platforms, which the new "Inside a terminal" row correctly contradicts off mac
  (`macLineEditSeq` is mac-gated, so Ctrl+←/→ falls through to xterm's
  `\x1b[1;5D/C` = word movement). Autofixed: label is now platform-conditional,
  with a unit test pinning both halves. Checks green (unit + dom 628, biome ci,
  typecheck).

## Open questions

_(none)_

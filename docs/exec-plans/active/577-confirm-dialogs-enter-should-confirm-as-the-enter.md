# Confirm dialogs: Enter should confirm, as the [enter] hint says

- **Spec:** [docs/product-specs/577-confirm-dialogs-enter-should-confirm-as-the-enter.md](../../product-specs/577-confirm-dialogs-enter-should-confirm-as-the-enter.md)
- **Issue:** #577
- **Status:** active

## Summary

The in-app choice dialog focused its first choice (Cancel) so a stray Enter was harmless, while its footer advertised `[enter] choose` and AGENTS.md said Enter confirms. This plan makes the shown keys literal: each dialog names a default action that Enter answers, hints name real actions, and the docs state one rule.

## Research

**Relevant code**

- `cmd/hivegui/frontend/src/app/modals/choice-dialog.ts` — `ChoiceSpec`, the module-scope promise, `dismissChoiceDialog` (Escape/scrim → `dismissValue ?? choices[0]`). No Enter handling: Enter was left to the focused `<button>`'s native activation.
- `cmd/hivegui/frontend/src/components/modals/ChoiceDialog.tsx` — passive effect focuses `button[data-choice]` (the first one); hints hard-coded to `[enter] choose` / `[esc] cancel`.
- `cmd/hivegui/frontend/src/app/key-scopes.ts:136` — `choiceDialog` scope, `exclusive`, binds only Escape → `choice-dialog.dismiss` (`commands.ts:364`). An exclusive scope swallows unbound keys only for Tab-trap purposes, so Enter on a focused button activated natively; with focus off the buttons Enter did nothing.
- Callers of `openChoiceDialog`: `components/activity/PinnedSection.tsx:37` (Unpin all), `app/modals/idea-inbox.ts:173` (Delete idea), `app/events.ts:~1420` (worktree_dirty close), `app/events.ts:~1470` (Delete project and ideas), `app/events.ts:488` (parked-worktree prompt — self-raised, `dismissValue: ''`, Retry first on purpose), `app/archive.ts:150,183` (archived delete / dirty force), `app/modals/worktrees.ts:194,268` (worktree delete / branch delete).
- `cmd/hivegui/app_calls.go:650` — native `Confirm()` already sets `DefaultButton: "OK"`: Enter confirms there.
- `ModalShell.tsx` — renders hints and actions in one footer; hint text width changes reflow the action buttons.

**Constraints**

- `test/dom/keymap-parity.test.ts` pins every key cell against the pre-478 matchers; a new binding needs an approved delta.
- `test/e2e/every-shortcut.spec.ts` sweeps every scope binding automatically (choice-dialog fixture exists).
- Several e2e specs (`worktrees.spec.ts`, `focus-traps.spec.ts`, `pinned.spec.ts`) asserted Cancel-holds-focus and `[enter] choose`.

**Prior lessons** — no prior lessons matched.

**Conventions card**

- Tests: `scripts/test.sh [go|unit|dom|e2e]`; locally `npx vitest run`, `CI=1 npx playwright test` (CI=1 or a stale dev server is reused), `npm run typecheck` (needs `scripts/ci-bootstrap.sh` in a fresh worktree), `npx biome ci .`, `scripts/ui-lint.sh`.
- TDD; every behaviour change carries its test. Validate UI behaviour in a real browser (Playwright vs the Wails mock), not jsdom alone.
- Dialog Enter/Esc are hard-coded overlay keys (AGENTS.md › Hard-coded exceptions), not keymap bindings; no help-overlay row / README keybind change needed.
- User-visible change → `.changesets/<slug>.md`; never edit `CHANGELOG.md`.
- Frontend-only change: no `DaemonContract` bump.

## Approach

Give `ChoiceSpec` an explicit `defaultValue`: the choice that takes focus on open and that Enter answers (fallback `choices[0]`, so a caller that forgets gets an honest `[enter] Cancel`). Bind Enter in the `choice-dialog` key scope to a new `choice-dialog.confirm` command → `confirmChoiceDialog()`, which answers the choice the `[enter]` hint names (see below). Hints are derived: `[enter] <focused choice label>` (follows keyboard focus via `focusin`, gated on `:focus-visible` so a mouse press doesn't relabel → reflow → lose the click), `[esc] <label of the dismiss answer>` or `Close` when `dismissValue` is not a choice.

Why not the obvious alternative (keep Cancel focused, relabel the hint `[enter] cancel`): honest but makes Enter useless, contradicts AGENTS.md and the native `Confirm()`; operator chose "Enter confirms". Why not "Enter always = last/danger choice": for 3-way dialogs the destructive one would be Enter's; least-destructive affirmative is safer.

Every user-raised caller sets a default: unpin (PinnedSection), delete (idea-inbox; events Delete project and ideas), keep-branch (worktree delete), local (branch delete), keep (archived delete), close (worktree_dirty close — keeps the worktree). Parked-worktree prompt (self-raised) unchanged → Retry. The archived dirty-worktree refusal (`archive.ts` `onArchivedWorktreeDirty`) sets no default → Cancel: it arrives after an async gap with Enter possibly still held, and its only yes destroys uncommitted changes. The Enter binding is `repeat: false`, so a held Enter never answers. Enter and the `[enter]` hint read one value: a module-scope `enterTarget` that ChoiceDialog sets from the same `:focus-visible`-gated `focusin` that relabels the hint (null → the default). Reading `document.activeElement` at keydown was tried and failed in a real browser: Chromium marks a pointer-focused button `:focus-visible` on the very keydown, so Enter answered the dragged-off button while the hint named the default. Labels appear in hints as written (they carry branch/ref names). IME: the dialog has no text input, so composition cannot reach it.

### Files to change

1. `cmd/hivegui/frontend/src/app/modals/choice-dialog.ts` — `defaultValue`, `confirmChoiceDialog()`, `defaultChoice()`.
2. `cmd/hivegui/frontend/src/components/modals/ChoiceDialog.tsx` — focus default; derived hints; focus-visible gate.
3. `cmd/hivegui/frontend/src/app/key-scopes.ts` — Enter → `choice-dialog.confirm`.
4. `cmd/hivegui/frontend/src/app/commands.ts` — register the command.
5. Callers: `components/activity/PinnedSection.tsx`, `app/modals/idea-inbox.ts`, `app/events.ts` (2 sites + comment on parked prompt), `app/archive.ts` (2), `app/modals/worktrees.ts` (2).
6. `AGENTS.md` — Feedback on Action, Hard-coded exceptions, Design rule: one rule, drop `y/enter`/`esc/n`.
7. `docs/design-docs/ui/components.md` — choice-dialog bullet under ModalShell.
8. `.changesets/choice-dialog-enter-confirms.md` — fixed/patch.

### Tests

- `test/dom/choice-dialog.test.tsx`: `names what enter and esc answer`, `moves the enter hint with focus`, `says close, not cancel, when Escape leaves the question unanswered`, `focuses the first choice when the spec names no default`, `focuses the default choice`, describe `enter`: `answers the default choice`, `answers the focused choice once Tab moves focus`, `answers the default when focus has left the buttons`, `does nothing with no question open`.
- `test/dom/keyboard-precedence.test.tsx`: `the choice dialog owns Enter › runs choice-dialog.confirm and consumes the key`.
- `test/dom/keyboard-precedence.test.tsx`: `ignores an auto-repeated Enter` (fails without `repeat: false` — checked).
- `test/dom/archived-sessions.test.tsx`: `Enter deletes and keeps the worktree`, `Enter on the dirty-worktree refusal cancels`.
- `test/dom/keymap-parity.test.ts`: approved delta `C` (choice-dialog Enter null → choice-dialog.confirm) + occurs check.
- `test/dom/pinned-section.test.tsx`: asserts `defaultValue: 'unpin'`.
- `test/dom/worktrees.test.tsx`: `focuses "Delete, keep branch"`.
- e2e `focus-traps.spec.ts`: focus starts on keep-branch; `choice dialog: Enter does what its hint says` (row removed); `choice dialog: Esc cancels` (row kept).
- e2e `focus-traps.spec.ts` `choice dialog: Enter ignores pointer focus` — press Cancel, drag off, Enter → default deletes (failed on the activeElement version).
- e2e `pinned.spec.ts`: hints `[enter] unpin all`/`[esc] cancel`; Enter unpins all.
- e2e `worktrees.spec.ts`: focus assertions updated; `Enter activates the focused dialog button` now Shift+Tab to Cancel → hint `[enter] cancel` → Enter keeps row; then Enter on default deletes. Mouse-click tests (orphan branch cancel → delete) prove clicks still land.

### Verification

- `cd cmd/hivegui/frontend && npm run typecheck && npx biome ci . && npx vitest run`
- `CI=1 npx playwright test` (full e2e incl. every-shortcut sweep)
- `scripts/ui-lint.sh`
- Mutation: on the old code, `pinned.spec` Enter step leaves 3 pins and `focus-traps` "Enter does what its hint says" leaves the row → both fail.

### Open questions / risks

- Long labels (e.g. `Use cached origin/main (3h)`) make a long hint line; the footer wraps. Accepted.

## Second opinion

- **Round 1:** verdict `revise`, confidence 7 (independent reviewer, hs-reviewer agent). Must-fix: (1) the Enter binding lacked `repeat: false`, so a held Enter could auto-confirm a destructive default the moment the dialog opened; (2) the `force` default on the archived dirty-worktree refusal arrives after an async gap — give it the self-raised treatment. Both applied, with tests. Nice-to-haves applied: pointer focus must not steer Enter, labels as written, stale comments.
- **Round 2:** verdict `approve`, confidence 8. Its optional suggestion (a Playwright drag-off test) exposed a real bug in the round-1 fix — Chromium marks a pointer-focused button `:focus-visible` on the keydown itself — fixed by making Enter and the hint read one `enterTarget`.

## Decision log

- **2026-11-22** — Enter confirms; each dialog names a `defaultValue`; with several affirmatives the least destructive one; a self-raised dialog keeps a non-destructive default. Why: operator's choice when asked (option "Enter confirms (Recommended)").
- **2026-11-22** — Archived dirty-worktree refusal defaults to Cancel, not force. Why: second opinion round 1 — it arrives after an async gap with Enter possibly held, and its only yes destroys uncommitted changes.
- **2026-11-22** — Enter reads a module-scope `enterTarget` set by the hint's focusin, not `document.activeElement` at keydown. Why: Chromium marks a pointer-focused button `:focus-visible` on the keydown, so the two disagreed (caught by e2e).
- **2026-11-22** — Hint labels shown as written, not lowercased. Why: labels carry branch/ref names.
- **2026-11-22** — Clarifying round A skipped: the one material decision was already answered by the operator.

## Progress

- **2026-11-22** — Spec + plan created; research done.
- **2026-11-22** — Plan approved by operator (chat).

# React UI rewrite — Phase 4: Modals B + keyboard reads the store

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **PR:** [#320](https://github.com/lucascaro/hive/pull/320)
- **Branch:** `feature/react-phase4-modals-b`
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Scope

New files: `src/components/modals/Worktrees.tsx` (port of the 581-line `src/app/modals/worktrees.ts`, reusing the pure logic in `src/lib/worktrees.ts` — two distinct files), `ProjectEditor.tsx`, `CommandPalette.tsx`, `HelpOverlay.tsx`, `ChoiceDialog.tsx` (rendered from store state `choiceDialog: {question, options} | null` — fixes the forgot-to-unregister keyboard-strand hazard by construction).

Files to change / delete: `src/app/modals/worktrees.ts`, `project-editor.ts`, `command-palette.ts`, `help-overlay.ts`, `choice-dialog.ts`, `registry.ts` — deleted (open/close wrappers over actions kept where callers need them). `src/app/keyboard.ts` — per-modal `.hidden` DOM queries become store reads; **precedence ladder copied verbatim**: inline-rename → choice dialog → launcher → project editor → command palette → settings → worktrees → help → dead-session → app bindings. `src/app/modals/focus-trap.ts` moves to `src/lib/focus-trap.ts`.

## Success criteria

What `/hs-merge-gate` validates for THIS phase.

- Worktrees, project editor, command palette, help overlay and choice dialog
  render from React into the same ids; `modals/registry.ts` is deleted.
- `keyboard.ts` reads modal state from the store instead of querying `.hidden`
  in the DOM, and its precedence ladder is **verbatim** the legacy order:
  inline-rename → choice dialog → launcher → project editor → command palette →
  settings → worktrees → help → dead-session → app bindings, pinned by a
  table-driven test over all 9 layers.
- The keyboard handler is still registered capture-phase.
- The choice dialog is rendered from store state, so a forgotten unregister
  cannot strand the keyboard — proven by an open → answer → cleanup test.
- `modals/focus-trap.ts` has moved to `src/lib/focus-trap.ts`.
- The port reuses `src/lib/worktrees.ts` rather than re-deriving its logic.

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

## Decision log

**2026-09-02 — `anyModalOpen()` moves into the store, and `modals/registry.ts`
goes.** Phase 3 deliberately kept the DOM `.hidden` class as the single source
of truth for "a modal owns the keyboard", because the four unported modals had
no store entry to ask about. That reason expires here: after this phase every
modal is a store entry, so the render signal and the keyboard-ownership signal
are the same fact. `anyModalOpen()` is now `modals.length > 0 || choiceDialog
!== null` in `store.ts`; `app/focus.ts` and `app/session-term.ts` just change
their import. Keeping the registry would have meant maintaining two sources
that must agree, for nothing.

**2026-09-02 — `src/ui/dialog.ts` is deleted too.** Not named in the phase
scope, but its last four callers are exactly the four modals ported here, and
`ModalShell` is its React replacement with the same markup contract. Leaving a
dead primitive behind (plus its `test/dom/ui-dialog.test.ts`, whose coverage
`modal-shell.test.tsx` already carries) would be a second dialog implementation
that nothing renders. `docs/design-docs/ui/components.md` now documents
`ModalShell` in its place.

**2026-09-02 — the choice dialog is a store field, not a modal-stack entry.**
It is mounted over any modal (it can ask about a row in the worktree browser),
and its answer travels back through a promise `openChoiceDialog()` still owns,
so it does not behave like the stack's other members. `choiceDialog:
{ spec, seq } | null` plus a module-scope resolver in
`app/modals/choice-dialog.ts` keeps every existing `await openChoiceDialog(...)`
call site untouched — including `events.ts`'s worktree-dirty kill, which is not
React code at all.

**2026-09-02 — the worktree rename stays imperative.** `beginInlineRename` is
what makes `inlineRenameActive()` true, and that predicate is layer 1 of the
keyboard ladder: a React-owned input would make Escape close the whole panel
instead of cancelling the edit, which is the exact bug the ladder's first gate
was added for. It now mounts into an EMPTY React-rendered `.worktree-main`
(React owns no children there while the rename is up), which as a side effect
fixes a real bug: a daemon repaint mid-edit used to rebuild the row and lose
the edit.

**2026-09-02 — the confirmations moved to the non-React half.** `askDelete`,
`askDeleteBranch`, `noteFor` and the two `confirmAnd…` flows are daemon
round-trips and copy, not rendering, so they live in `app/modals/worktrees.ts`
with `ListWorktrees` and friends. `Worktrees.tsx` calls them and renders; it
holds no mutation logic.

**2026-09-02 — the palette's selection is clamped, not reset.** The ported
version briefly reset the selection to 0 on every keystroke. The imperative
`renderPalette()` only did that when the narrowed list no longer reached the
selected index, so the clamp is derived at render instead — a row the user has
already moved to survives a keystroke that still matches it.

## Progress

**2026-09-02** — Implemented. Store: `ModalId` grew to six ids with payloads for
`project-editor` and `worktrees`, plus `worktreesPayload`, `choiceDialog`,
`modalEntry()`, `setWorktreesPayload()`, `setChoiceDialog()` and
`anyModalOpen()`. New `components/modals/{Worktrees,ProjectEditor,CommandPalette,
HelpOverlay,ChoiceDialog}.tsx`; `ModalShell` gained `titleSuffix` (the worktree
browser's `· <project>`) and `showCloseButton` (the choice dialog has none).
`app/modals/{worktrees,project-editor,command-palette,help-overlay,
choice-dialog}.ts` gutted to store-backed open/close pairs plus what is not
rendering; `modals/registry.ts`, `src/ui/dialog.ts` and `test/dom/ui-dialog.test.ts`
deleted; `modals/focus-trap.ts` moved to `src/lib/focus-trap.ts`; `index.html`
gained the `#worktrees`, `#project-editor`, `#help-overlay` and `#choice-dialog`
roots and gave up the command palette's two static children; `keyboard.ts`'s
ladder reads the store; `main.ts` mounts the five islands.

Tests: `worktrees.test.ts` rewritten to RTL `.tsx` with all 45 cases ported;
`focus-trap.test.ts` repointed at `src/lib/`; new `keyboard-precedence.test.tsx`
(table-driven over all 9 layers, plus a capture-phase proof) and
`choice-dialog.test.tsx`; new dom coverage for the three modals that had none
(`command-palette`, `project-editor`, `help-overlay`). The e2e specs are
unmodified — they are the proof the DOM contract survived.

**2026-09-02 — two contract breaks the e2e specs caught, both fixed in the
component (never in the spec).**

1. *Focus landed nowhere.* `ChoiceDialog` and `ProjectEditor` focused from a
   layout effect. Layout effects run child-first, and the root's `hidden` class
   comes off in the PARENT island's layout effect — so both were calling
   `focus()` on an element still inside a `display:none` subtree, which the
   browser ignores outright. jsdom has no such rule, so every dom test passed;
   `focus-traps.spec.ts` failed on the real thing, with Tab walking the modal
   underneath. Both now focus from a passive effect, like Settings and the help
   overlay — still the same commit, nothing like the `setTimeout` the imperative
   project editor used.
2. *`#command-palette-input` vanished from a cold boot.* The palette's input and
   list were static children of `#command-palette` in `index.html`, and
   `ux-polish.spec.ts` reads the input's `aria-label` at boot. The island now
   renders them whether or not the palette is open — which is exactly what the
   imperative version did; opening only drops the root's `hidden` class.

   A third, `.choice-dialog` itself: the specs count that selector to assert no
   question is on screen, because the element used to be built per question.
   The root is static now, so the island adds and removes the class with the
   opening — see the Decision log.

**2026-09-02 — Verification.** `npm run typecheck` clean; `npx biome ci .` clean
(8 warnings, the same set as `main`); `scripts/ui-lint.sh --strict` 0 violations;
`vite build` succeeds; vitest **82 files / 923 tests** green;
`go build ./...` + `go test ./internal/... ./cmd/hivegui/...` green under the
`go.mod` toolchain; Playwright e2e **258 passed / 0 failed / 31 skipped** with
every spec unmodified; `npm run test:e2e:real` **24 passed**.

## PR convergence ledger

_(opened 2026-09-02 for PR #320; `/hs-review-loop` appends one entry per iteration)_

**2026-09-02 — review iteration 1 (three IMPORTANTs, all stood).**

- *The dialog footer lost its hint separator.* `ModalShell` renders one
  `<span class="hv-dialog__hint">` per hint and `.hv-dialog__hints` had no
  `gap` and no `.hv-dialog__hint` rule, so `[esc] close · (r) refresh` came out
  as `[esc] close(r) refresh` — the imperative `dialog()` took a literal
  `' · '` text node between the two, which the props-based shell has nowhere to
  put. Fixed in `dialog.css` with `.hv-dialog__hint + .hv-dialog__hint::before
  { content: ' · '; }`, which is the same three characters in the same place.
  Phase 3's Settings footer had the same shape, so this was already wrong on
  `main` — it is only *visible* here because Worktrees is the first two-hint
  footer whose separator used to be spelled out.
- *The rename could strand the keyboard.* `beginInlineRename` registers the edit
  module-side and only Enter/Escape/blur clear it; React removing a focused
  input fires no blur, so a row that unmounted mid-edit (the daemon dropped the
  worktree) left `inlineRenameActive()` true — layer 1 of the ladder, which
  then swallows every keystroke in the app. The effect now returns a cleanup,
  and `inline-rename.ts` gained `cancelInlineRenameFor(input)` so a React
  cleanup can only ever cancel its own editor, never one that started after it.
  Two tests; the row-removal one fails against the un-cleaned-up version.
- *The palette's Escape branch was covered by nothing.* The ladder deliberately
  bails for the palette, so that branch is the only keyboard close path, and
  deleting it left the suite green. Now tested.

Plus three MINORs: the empty-footer `hidden` the primitive used to set,
`spec.choices[0]` guarded in the component the way the module half guards it,
and the passive ladder layers now assert `defaultPrevented === false` rather
than only the silence below them.

**2026-09-02 — the snapshot baselines, which are the real proof.**
`theme.spec.ts`'s `maxDiffPixels: 0` baselines only run under `HIVE_SNAPSHOT=1`
(never in CI), and they were last regenerated at `7de6cae` — before any React
phase. Run against this branch they are the strongest available check that the
ports are pixel-identical, so they were run:

- **worktrees, sidebar, launcher, chrome: all green against pre-React
  baselines** — including all six preset variants of the worktree browser,
  which is the panel this phase rewrote. That is the phase's central claim,
  independently verified. They failed *before* the separator fix and pass
  after, which is also how that regression was confirmed.
- **The seven settings-dialog baselines were stale from Phase 3** (#319 added
  the `[esc] cancel · [enter] save` footer hints; the baselines predate them),
  so they failed on `main` too. Regenerated here — the only image files this PR
  touches — leaving the full suite 59/59 green for the first time since #319.

- **2026-09-02 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 911f9cd4; threads_open: 0; action: fixes applied + push (3 IMPORTANT stood, so not convergence under the loop's "COMMENT with only MINOR remaining" bar); head_sha: b5e3def.

**2026-09-02 — review iteration 2: APPROVE.** Zero BLOCKING, zero IMPORTANT,
zero review threads; the three iteration-1 fixes verified at source rather than
taken on trust; the reviewer independently re-ran the snapshot suite
(59/59 at `maxDiffPixels: 0`) and confirmed the six non-regenerated
`worktrees-*` baselines are still exact — i.e. no baseline was quietly masked.

The five surviving MINORs were applied post-loop rather than deferred:

- `components.md`'s `ModalShell` signature was the pre-React `dialog()` one —
  it now documents `children`, `hints`, `titleSuffix` and `showCloseButton`.
- `choice-dialog.css`'s comment still said the dialog is mounted on `<body>`.
- The master plan still listed `focus-trap.ts` at its pre-move path.
- `trapFocus`/`releaseFocus` take `HTMLElement | null` now. Every caller reaches
  them through `pageEl()`, which casts rather than throws (`app/el.ts`), so a
  jsdom mount of part of `index.html` hands them a null — and `keyboard.ts`
  calls `trapFocus` on *every* key while a modal is open, so a throw there takes
  the whole keyboard with it. Unreachable through the real `index.html`; fixed
  because the cost is a guard and the failure mode is total.
- `cancelInlineRenameFor`'s identity guard — the entire reason it exists rather
  than a bare `cancelInlineRename()` — had no test for its false branch. New
  `test/dom/inline-rename.test.ts` covers it; deleting the guard fails two of
  its three cases.
- **2026-09-02 iter 2** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: converged (iteration-1 fixes verified at source; 5 MINORs applied post-loop); head_sha: adcdc15.

**2026-09-02 — CI caught a real one after the loop converged: the command
palette could become uncloseable.**

The macOS and Windows legs failed on `focus-traps.spec.ts:533`
("command palette: open-then-immediately-close leaves focus sane") — the palette
was still visible 5s after Escape. Not a flake, and not a timing coincidence
either: the palette's key handling lives on a listener attached to
`#command-palette`, so it only ever sees keys typed *inside* the palette, and
`keyboard.ts`'s ladder deliberately bails for the palette on the assumption that
listener will handle them. Move focus out of the search box — which the focus
pipeline's 8-frame `focusActiveTerm` retry does on a slow enough machine — and
Escape reaches the ladder, hits a gate that returns, and nothing closes the
palette. Every subsequent key dies the same way.

Reproduced deterministically rather than guessed at: open the palette, blur the
search box, press Escape. It stayed open on this machine every time.

Fixed by giving the palette the same shape settings and the worktree browser
already have — the modal's own listener owns its keys, and the ladder owns
Escape as the fallback for when focus is elsewhere. `keyboard-precedence.test.tsx`
now expects the palette layer to close rather than to be passive, so reverting
the ladder branch fails it. The legacy palette had the same hazard (same
listener, same bail); it survived because focus happened to stay put.

## Gate verdict

_(awaiting `/hs-merge-gate`; it appends here)_

**2026-09-03 — review iteration 3 (the ladder change, re-reviewed).** One
IMPORTANT, and a good one: the ladder fix made `CommandPalette.tsx`'s own
Escape branch dead code. `keyboard.ts` registers capture-phase on `window` and
calls `stopPropagation()`, so a bubble-phase listener on `#command-palette` can
never see Escape — and the test covering that branch carried a comment
asserting the ladder still bails for the palette, which is exactly the belief
that produced the CI failure the ladder fix repaired. Removed the branch and
inverted the test: it now asserts the palette does NOT handle Escape, with the
close itself covered where it actually happens, in `keyboard-precedence.test.tsx`.
The MINOR — a choice-dialog test leaving its second question unanswered, so the
module-scope resolver `resetStore()` cannot clear leaked into the next test —
is fixed by answering it.

The reviewer also verified the ladder change independently: strictly additive
(it intercepts a key that previously fell into a bare `return`, leaving all nine
layers' order untouched), no double-close (capture-phase `stopPropagation`
prevents the palette's own listener from firing), `flushSync` safe from a plain
DOM handler for the same reason `closeSettings` already does it, and the hazard
pre-existed in the imperative palette — the React focus pipeline's 8-frame retry
is what made it reachable.
- **2026-09-03 iter 3** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 762045d1; threads_open: 0; action: fixes applied + push (1 IMPORTANT stood — the ladder fix had left `CommandPalette.tsx`'s Escape branch dead and its test comment inverted); head_sha: cd359cc.

**2026-09-03 — review iteration 4: four IMPORTANTs, three of them right.**

- *The macOS red leg had a cause.* `ux-polish.spec.ts:232` (the sidebar version
  footer) failed on macOS and passed on retry, which `failOnFlakyTests` turns
  into a failed leg. Mechanism: the five Phase 4 islands were mounted **ahead**
  of `VersionFooter`, which subscribes to `daemon:stale` in an effect, while the
  e2e `boot()` gate waits on `#projects li` — the *first* island. The handshake
  could land in the gap with nothing to replay it. `VersionFooter` now mounts
  before the modals: no modal can be opened before boot finishes, so they are
  the ones that can afford to wait.
- *The two answers that reach off this machine had no coverage.* No fixture set
  `upstream`, so "Delete + branch everywhere" and "Delete local + remote" never
  rendered and every assertion passed `false` for the remote flag. A swapped
  boolean would have pushed a branch deletion to a remote with the suite green.
  Both are tested in both directions now, and both mutation-checked.
- *A test that asserted nothing.* "does nothing if the browser closed while the
  dialog was open" clicked a button `closeWorktrees()` had already dismissed, so
  `?.click()` was a no-op that passed against any implementation. Split into the
  two guards that actually exist — the dismiss-on-close, and the post-await
  `projectId` check reached by answering and closing in the same tick — both
  mutation-checked.

**Rejected: the claim that the `worktrees-*.png` baselines were left stale.**
They are untouched since `7de6cae` (pre-React) and the snapshot suite runs
**59/59 green**, re-verified after these fixes. The finding inferred staleness
from "the dialog baselines were regenerated and these were not" without running
them. That they pass *unregenerated* is the evidence this phase is pixel-identical;
regenerating them would have destroyed the proof rather than confirmed it.

Four MINORs also applied: a keydown listener the precedence test left on
`#terms`, teardown for the choice dialog's module-scope resolver, a fixture that
hardcoded the `choice-dialog` class the island is supposed to toggle (so it
could not have caught the island failing to), and two ladder comments still
describing the deleted `ui/dialog.ts`.

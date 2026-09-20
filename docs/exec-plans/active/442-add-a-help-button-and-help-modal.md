# Add a Help button and Help modal to the sidebar header

- **Spec:** [docs/product-specs/442-add-a-help-button-and-help-modal.md](../../product-specs/442-add-a-help-button-and-help-modal.md)
- **Issue:** #442
- **PR:** #443
- **Branch:** `feature/442-help-modal`
- **Status:** active

## Summary

Add a fourth icon control to the sidebar header — a `help` button, rightmost, after the
What's new gift — opening a new `help-modal`. The modal carries a Concepts section, a
handoff row to the existing keyboard-shortcuts overlay (with its ⌘/ binding shown), and
external links opened through the Wails `OpenURL` bridge. The modal is new plumbing, not a
retitle of the shortcuts overlay, so the ⌘/ surface keeps its single job.

## Research

Investigated in the main thread this session (the relevant call sites were traced end to
end before the plan was drafted); no Explore fan-out was needed.

### Relevant code

- `cmd/hivegui/frontend/src/components/Sidebar.tsx:708` — `SidebarHeaderControls`. Renders
  the header's icon controls as `createPortal` calls into the `index.html`-owned `<header>`.
  Portal order is DOM order, and the comment at `:763` states the constraint: the gift must
  stay last today, so a new rightmost button goes after it. Null-guarded (`:722`) because
  dom-test scaffolds mount without a sidebar header.
- `cmd/hivegui/frontend/src/lib/icon-sprite.ts:30` — `'help'` is already a registered
  `IconName`; `lib/icons.svg` already carries `id="hv-help"`. Unused today. No new asset.
- `cmd/hivegui/frontend/src/app/modals/whats-new.ts` — the closest modal shape to copy:
  open/close/init, `releaseFocus` + `flushSync(closeModal(...))` on close, `setFocusedTile(null)`
  on open. Its read-receipt logic (`markSeen`) is the only part that does not apply.
- `cmd/hivegui/frontend/src/app/modals/help-overlay.ts:25,49` — `openHelpOverlay` /
  `toggleHelpOverlay`, modal id `'help'`. The new modal must therefore use a *different*
  id; `'help-modal'` is taken as the id and the `index.html` root.
- `cmd/hivegui/frontend/src/store/store.ts:148-181` — `ModalId` union and `ModalEntry`
  union; both need the new id. `seq` is the per-open generation used as a React `key`.
- `cmd/hivegui/frontend/src/components/App.tsx:62,165,170` — modal components are
  portalled into their `index.html` roots here.
- `cmd/hivegui/frontend/src/app/keyboard.ts:311,1021` — two `isModalOpen(...)` guards: the
  Escape/focus-trap branch and the global "a modal is up" check.
- `cmd/hivegui/frontend/src/main.tsx:176` — command palette command list
  (`{ id: 'whats-new', name: "What's New…", run: () => openWhatsNew() }`) and the `init*`
  wiring for the imperative modal halves.
- `cmd/hivegui/frontend/src/app/banners.ts:328` — the external-link pattern:
  `OpenURL(url).catch(reportFailure('open link'))`. A real `<a href>` would navigate the
  Wails webview, so links are buttons.
- `cmd/hivegui/frontend/src/components/modals/ModalShell.tsx` — the dialog chrome
  (`title`, `size`, `onClose`, `hints`). `HelpOverlay.tsx:44` shows the
  `hints={[{ keys: '[esc]', label: 'close' }]}` convention.
- `cmd/hivegui/frontend/index.html:139-149` — the modal roots. Each is a
  `div.hv-dialog.hidden[role=dialog][aria-modal][aria-labelledby]`.

### Constraints / dependencies

- Modal id `'help'` is **taken** by the keyboard-shortcuts overlay. Reusing it would make
  the two modals mutually exclusive through `isModalOpen` and break the handoff row.
- Four icons now share the sidebar header. Minimum-width layout is unverified by the unit
  layers (vitest is CSS-blind), so it needs a real-browser check before the PR is called
  done.
- `scripts/ui-lint.sh` enforces design tokens and the icon primitive: no raw SVG, no
  hard-coded colours.
- Frontend-only. No `internal/{wire,daemon,session,registry}` or `cmd/hived` change, so
  `buildinfo.DaemonContract` is **not** bumped and `scripts/check-daemon-contract.sh` has
  nothing to say.

### Prior lessons

`brain-search` returned no hits for this feature's terms — no prior lessons matched.

Two standing entries from project memory do apply and are treated as constraints below:
CSS/layout claims are not trustworthy from reasoning alone (verify in a real browser), and
local Playwright runs need `CI=1` or they reuse a stale vite dev server.

### Conventions card

- **Build:** `./build.sh` (macOS .app). For frontend-only verification,
  `cd cmd/hivegui/frontend && npm run build`.
- **Lint:** `cd cmd/hivegui/frontend && npx biome ci .` (`ci`, not `lint` — only `ci`
  checks formatting) plus `scripts/ui-lint.sh` and `npm run typecheck`.
- **Tests:** `scripts/test.sh` (layers: `go unit dom e2e`). This change touches only the
  frontend, so `scripts/test.sh unit dom e2e` is the relevant subset; the full script still
  runs before the commit.
- **Everything:** `scripts/test.sh` after `scripts/ui-lint.sh` and `npm run typecheck`.
- TDD: every behavioural change ships with the test that would have caught its regression.
  Frontend tests live under `cmd/hivegui/frontend/test/{unit,dom,e2e}`.
- User-visible change ⇒ a `.changesets/<slug>.md` entry (`type: added`, `bump: minor`) and
  a `site/features.json` entry with `since: "Unreleased"`. Never edit `CHANGELOG.md`.
- Key hints render as `[key]` for number/symbol keys and `(key)` for letter keys; a
  binding is shown next to the action it triggers.
- `npm run typecheck` needs the generated wailsjs bindings — run `./scripts/ci-bootstrap.sh`
  in a fresh worktree first.

## Approach

A **new** modal (`help-modal`), not a retitle of the ⌘/ keyboard-shortcuts overlay.

The obvious alternative — rename the existing overlay to "Help" and add an intro block —
is cheaper by a few lines but collapses two surfaces into one. The overlay's modal id is
literally `'help'` and is referenced from two `keyboard.ts` guards plus the macOS menu
action `menu:keyboard-shortcuts`; ⌘/ must keep landing straight on the shortcut list.
So the new modal takes the id `'help-modal'` and the shortcuts overlay is untouched.
`store.ts`'s `modals` is an array keyed by id, so the two ids coexist without interfering.

The modal is a copy of the `whats-new` shape minus its read receipt: an imperative half
(`app/modals/help.ts`) owning open/close/focus ordering, a React half
(`components/modals/Help.tsx`) rendering `ModalShell` into the `index.html` root.

Content is a static data array inside `Help.tsx` — three sections:

1. **Concepts** — a `<dl>` with four one-sentence definitions: project, session, worktree,
   agent.
2. **Keyboard shortcuts** — one row: a `<Kbd>` showing the platform-correct ⌘/ (via
   `isMac` from `lib/platform.js`, as `HelpOverlay.tsx` does) next to a button that calls
   `closeHelp()` then `openHelpOverlay()`. Order matters: closing first releases the focus
   trap before the overlay acquires its own.
3. **Links** — README, docs, report an issue, releases. Each is a `<button>` calling
   `OpenURL(url).catch(reportFailure('open link'))`, the pattern at `app/banners.ts:328`.
   A real `<a href>` would navigate the Wails webview out of the app. The four URLs are
   built from ONE module-level `REPO` const in `Help.tsx`, not four independent literals.

**⌘/ while Help is open** routes through the same handoff rather than being swallowed.
The `whats-new` ladder branch closes only on Escape; the Help branch also accepts
`isHelpOverlayKey(e)` and performs the close-then-open handoff, because ⌘/ is the one
binding this modal advertises and having it do nothing there is the worse answer.

No keybinding is added for Help itself: the button and a command-palette entry are the
only openers, so `lib/keymap.ts`, `lib/shortcuts.ts` and the README Keybinds table are
untouched.

### Files to change

1. `cmd/hivegui/frontend/index.html` — add
   `<div id="help-modal" class="hv-dialog hidden" role="dialog" aria-modal="true" aria-labelledby="help-modal-title">`
   after `#whats-new`, matching the sibling roots at :139-152.
2. `cmd/hivegui/frontend/src/store/store.ts` — add `'help-modal'` to the `ModalId` union
   (:148) and `| { id: 'help-modal'; seq: number }` to `ModalEntry` (:179).
3. `cmd/hivegui/frontend/src/components/Sidebar.tsx` — import `openHelp`; add a fourth
   `createPortal(<IconButton id="help-btn" icon="help" label="Help" size={22}
   onClick={openHelp} />, header)` as the **last** entry in the returned fragment, and
   update the `:763` comment that currently claims the gift is "third and rightmost".
4. `cmd/hivegui/frontend/src/components/App.tsx` — import `Help`, add a `mustEl('help-modal')`
   to the flat root list at :103-113, and `{createPortal(<Help root={helpModal} />, helpModal)}`
   alongside the sibling portals. (`mustEl` throws on a missing root and `test/dom/app-root.test.tsx`
   mounts the real `index.html`, so a forgotten root fails loudly.)
5. `cmd/hivegui/frontend/src/app/keyboard.ts` — add an `isModalOpen('help-modal')` branch
   next to the `whats-new` one (:324): Escape closes; `isHelpOverlayKey(e)` performs the
   shortcuts handoff; otherwise `trapFocus(pageEl('help-modal'), e)`. Add
   `isModalOpen('help-modal')` to `ideaKeysBlocked()` (:1021).
6. `cmd/hivegui/frontend/src/main.tsx` — import `openHelp, initHelp`; add
   `{ id: 'help', name: 'Help…', run: () => openHelp() }` to the palette command list next
   to the What's New entry (:176); call `initHelp({ setFocusedTile, focusActiveTerm })`
   next to `initWhatsNew` (:330).
7. `cmd/hivegui/frontend/src/theme/components/index.css` — `@import` the new `help.css`.
8. `.changesets/442-help-modal.md` — `type: added`, `bump: minor`. (Issue-backed entries
   are named `<issue>-<slug>.md`, per every existing entry in that directory.)
9. `site/features.json` — a `status: shipped`, `since: "Unreleased"` entry with a
   one-sentence blurb.
10. `cmd/hivegui/frontend/test/dom/whats-new.test.tsx` — **breaking**: `:64-73` asserts the
    header button ids `toEqual(['new-project-btn','check-updates-btn','whats-new-btn'])`
    under the title "renders third in the header". A fourth button fails it. Append
    `'help-btn'` to the array and retitle to reflect the new order.
11. `cmd/hivegui/frontend/test/dom/keyboard-precedence.test.tsx` — **breaking-adjacent**:
    add a `help-modal` row to the Escape-ladder table (`:278-290`, next to `help overlay`
    and `what's new`) and to the `menu:quick-idea` no-op table (`:496-497`). Two lines each;
    without them the `ideaKeysBlocked()` addition ships with zero coverage.
12. `cmd/hivegui/frontend/test/e2e/sidebar-header-actions.spec.ts` — `help-btn` joins **both**
    id arrays: `BUTTONS` at `:11` (the layout/adjacency test) and the separate
    `['check-updates-btn','whats-new-btn']` loop at `:100` (the reachable/hit-testable test).
    Update the test title at `:18` ("all three header buttons"). Plus the new min-width case
    below.

### New files

- `cmd/hivegui/frontend/src/app/modals/help.ts` — `openHelp` / `closeHelp` / `initHelp`,
  modelled on `app/modals/whats-new.ts` minus `markSeen`: `openModal({ id: 'help-modal' })`
  + `setFocusedTile(null)` on open; `releaseFocus(pageEl('help-modal'))` then
  `flushSync(() => closeModal('help-modal'))` then `focusActiveTerm()` on close.
- `cmd/hivegui/frontend/src/components/modals/Help.tsx` — the modal body. `ModalShell`
  with `id="help-modal"`, `title="Help"`, `size="lg"`,
  `hints={[{ keys: '[esc]', label: 'close' }]}`; remounted per opening via `key={entry.seq}`;
  focuses its close button on mount; toggles the root's `hidden` class in a
  `useLayoutEffect`, exactly as `HelpOverlay.tsx:22` does.
- `cmd/hivegui/frontend/src/theme/components/help.css` — token-only rules for the concepts
  list and the link rows.
- `cmd/hivegui/frontend/test/dom/help-modal.test.tsx` — see Tests.
- `cmd/hivegui/frontend/test/e2e/help-modal.spec.ts` — see Tests.

### Tests

`cmd/hivegui/frontend/test/dom/help-modal.test.tsx` (new, modelled on
`test/dom/whats-new.test.tsx`'s harness):

- `renders a help button in the sidebar header after the gift` — mounts
  `SidebarHeaderControls`, asserts `#help-btn` exists with `aria-label="Help"` and that the
  header's button-id list ends with it.
- `opens the modal on click, and Escape closes it AND returns focus to the terminal` —
  the test calls `initHelp({ setFocusedTile, focusActiveTerm: spy })` with a vi.fn spy.
  Asserting only "the dialog closed" would pass on a broken implementation, because the
  module's deps default to no-ops (`whats-new.ts:27-30` shape). The assertion is that the
  spy fired *and* that the dialog root carried `hidden` by then.
- `explains the core concepts` — asserts the four concept terms (project, session,
  worktree, agent) are present in the dialog.
- `hands off to the keyboard-shortcuts overlay` — clicking the shortcuts row closes
  `#help-modal` and opens `#help-overlay`; asserts the row shows the ⌘/ binding. A second
  case drives ⌘/ through the keyboard pipeline while Help is open and asserts the same
  handoff (not a swallowed key).
- `opens external links through the bridge, not the webview` — mocks the `OpenURL` bridge
  export, clicks a link row, asserts `OpenURL` was called with the expected URL and that
  no `<a href>` exists inside the dialog.

`cmd/hivegui/frontend/test/dom/keyboard-precedence.test.tsx` (changed): `help-modal` rows
in the Escape-ladder table and the `menu:quick-idea` no-op table, covering the
`ideaKeysBlocked()` addition.

`cmd/hivegui/frontend/test/dom/whats-new.test.tsx` (changed): the header-order assertion
grows to four ids.

`cmd/hivegui/frontend/test/e2e/sidebar-header-actions.spec.ts` (changed): `help-btn` in
both arrays, plus a **new min-width case** — success criterion 7 must be verified by a
command, not by eye (AGENTS.md's manual-smoke rule rejects "build it and look"). The case
sets `--sidebar-width` on `#app` to `store.SIDEBAR_MIN_WIDTH` (220, `store.ts:57`;
`layout.css:8` reads the var) via `page.evaluate`, then re-runs the existing one-row /
adjacency / no-overflow assertions against all four buttons.

`cmd/hivegui/frontend/test/e2e/help-modal.spec.ts` (new): opens the modal from the button
and from the command palette (⌘K → "Help"), asserts the dialog is visible and Escape
closes it.

### Verification

Run from `cmd/hivegui/frontend` unless noted:

```
./scripts/ci-bootstrap.sh            # repo root; generates the wailsjs bindings typecheck needs
npm run typecheck
npx biome ci .
./scripts/ui-lint.sh --strict        # repo root; WITHOUT --strict it exits 0 in warn mode
./scripts/ui-lint.sh --contrast      # repo root; only if help.css adds a fg/bg pair
scripts/test.sh unit dom             # repo root
CI=1 npm run test:e2e -- sidebar-header-actions help-modal
CI=1 scripts/test.sh                 # repo root; full harness before the commit
```

`--strict` is load-bearing: `ui-lint.sh:32` documents "Exit 0 in warn mode", and CI runs
the strict form (`.github/workflows/ci.yml:154`), so the bare command cannot fail.
`CI=1` is load-bearing on **both** e2e lines: `playwright.config.js` sets
`reuseExistingServer: !process.env.CI`, and `scripts/test.sh`'s `run_e2e` (:52-58) does not
set `CI` itself, so an unguarded full run reuses a stale vite dev server and proves nothing.

## Second opinion

Two reviewer rounds (`general-purpose` subagent, both `revise`, confidence 8 each).

**Round 1** — six must-fix items, all verified against the files and all applied: the
`test/dom/whats-new.test.tsx:69` header-id array breaks on a fourth button; a *second*
id array at `test/e2e/sidebar-header-actions.spec.ts:100` also needs `help-btn`; success
criterion 7 had no runnable check; the `ideaKeysBlocked()` addition had zero coverage;
the Escape test would pass on broken focus-return because the modal's deps default to
no-ops; `scripts/ui-lint.sh` exits 0 without `--strict`. Three nice-to-haves were taken
as well (`CI=1` on the full `test.sh`, the `<issue>-<slug>` changeset name, one `REPO`
const instead of four literals).

**Round 2** — one must-fix, applied: the min-width e2e case as first drafted was a
tautology. `SIDEBAR_MIN_WIDTH` is 220 (`store/store.ts:57`) and that is already the boot
width (`store.ts:352` -> `main.tsx:405`), and the spec file has no containment assertion
to re-run. The case now names its assertions explicitly and runs them twice, including at
a forced 180px below the resizer's clamp. The reviewer also flagged the ⌘/ handoff as
unlisted scope; it is kept deliberately (a modal that displays a binding and then eats the
key is the worse outcome) and recorded in the Decision log. Per the pipeline's one-revise
rule, no third round was run.

Both rounds reported no injection attempts in the spec or plan text.

## Decision log

- **2026-09-20** — New `help-modal` rather than retitling the ⌘/ overlay. Why: the overlay's
  modal id `'help'` and its `Keyboard shortcuts` title are referenced from `keyboard.ts`
  guards and the menu action `menu:keyboard-shortcuts`; repurposing it would fold two
  distinct surfaces into one and lose the direct ⌘/ path the operator asked to keep.
- **2026-09-20** — No keybinding for the Help modal (operator decision). Why: avoids the
  keymap/README/shortcut-list ceremony in `AGENTS.md` and avoids a second binding competing
  with ⌘/.
- **2026-09-20** — Rightmost placement, after the gift (operator decision). Why: the
  least-frequently-used control sits furthest out; the gift keeps its unread-dot prominence.
- **2026-09-20** — Content scope: link hub + concepts + shortcuts handoff (operator
  decision). Why: useful to a cold user without committing to a tutorial that goes stale.
- **2026-09-20** — Keep the ⌘/ handoff while Help is open, rather than letting Help
  swallow the key like every other modal. Why: the modal advertises ⌘/ next to its
  shortcuts row, and a displayed binding that does nothing is worse than either
  alternative. It adds no new binding and does not change ⌘/ outside the modal, so the
  spec's non-goal holds.
- **2026-09-20** — Verify the four-icon header layout with an e2e assertion rather than by
  eye. Why: AGENTS.md's manual-smoke rule rejects "build it and look", and the first draft
  of that check was a tautology at the default width.
- **2026-09-20** — Assert the header's INTRINSIC width against SIDEBAR_MIN_WIDTH rather
  than forcing a narrow `--sidebar-width`. Why: measured during implementation — #sidebar
  is a grid item with the default `min-width: auto`, so the column floors at min-content
  and a 180px request renders identically to 220px (both 219px). The planned "narrow"
  case was therefore a second no-op. The real failure mode is that floor rising past 220,
  which would stop the sidebar being resized to its documented minimum. Today the header
  needs ~173px, and the assertion was confirmed to fail when the threshold is lowered.
- **2026-09-20** — `handOffToShortcuts()` lives in `app/modals/help.ts`, not in
  `components/modals/Help.tsx`. Why: `app/keyboard.ts` needs it for the ⌘/ branch, and
  app/ importing a component would invert the existing layering.
- **2026-09-20** — Split the Help e2e cases into their own `help-modal.spec.ts`, leaving
  `sidebar-header-actions.spec.ts` to layout only. Why: the plan put both in the layout
  file; behaviour and layout are different jobs and the layout file says so in its header.

## Progress

- **2026-09-20** — Spec created from issue #442; triaged enhancement / M / P2; research
  recorded.
- **2026-09-20** — Plan approved after two second-opinion rounds; stage PLAN -> IMPLEMENT.
- **2026-09-20** — Implemented; all checks green (typecheck, biome ci, ui-lint --strict,
  ui-lint --contrast, CI=1 scripts/test.sh: 397 passed / 31 skipped, one unrelated
  worktrees.spec.ts flake that passed on retry). PR #443 opened; stage IMPLEMENT -> REVIEW.

## Open questions

- **Content staleness.** The concepts text is prose in a component and will drift if the
  model changes. Kept to four sentences to bound the cost.
- **Modal id collision.** `'help'` is the shortcuts overlay. Every new reference must say
  `'help-modal'`; a slip would make the two modals mutually exclusive.
- **Four icons in a narrow header.** Now covered by the min-width e2e case rather than left
  to inspection. If it fails, the fix is a header `gap` / `min-width` adjustment in
  `sidebar.css`, not dropping a button.

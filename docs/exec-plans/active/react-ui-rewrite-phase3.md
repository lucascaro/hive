# React UI rewrite — Phase 3: Modals A: launcher + settings

- **Master plan:** [react-ui-rewrite.md](react-ui-rewrite.md)
- **Spec:** [docs/product-specs/react-ui-rewrite.md](../../product-specs/react-ui-rewrite.md)
- **Issue:** —
- **Status:** active

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Scope

New store state: `modals: ModalId[]` stack + `openModal/closeModal/anyModalOpen` actions. Legacy `modals/registry.ts` keeps working for still-legacy modals; `anyModalOpen()` ORs both sources until Phase 4.

New files: `src/components/modals/ModalShell.tsx` (root-id passthrough, `.hidden` toggling from store, focus trap via existing `focus-trap.ts` helpers, Enter/Esc per AGENTS.md, visible confirm/cancel key hints), `Launcher.tsx` (faithful port of the 680-line `launcher.ts` incl. search via `src/lib/shortcuts.ts`, stacking, open-generation token semantics — do not "improve" flows mid-port), `Settings.tsx` (473-line port: theme picker via `src/theme/theme.ts`, font size, custom agents CRUD, update settings via `src/lib/update-state.ts`).

Files to change: `src/app/modals/launcher.ts`, `settings.ts` deleted; their `openX/closeX` exports become thin wrappers over store actions (callers in `keyboard.ts`/`events.ts`/`main.ts` keep compiling). `index.html` — launcher/settings markup reduced to empty root divs with the same ids.

## Success criteria

What `/hs-merge-gate` validates for THIS phase.

- Launcher and settings render from React into the same root ids, and the
  `.hidden` visibility contract is unchanged.
- The launcher's search (`src/lib/shortcuts.ts`), stacking, and open-generation
  token semantics are ported faithfully — no flow is "improved" mid-port.
- Settings keeps theme picking via `src/theme/theme.ts`, font size, custom-agent
  CRUD, and update settings via `src/lib/update-state.ts`.
- `ModalShell` acquires and releases the existing focus trap, handles Enter/Esc
  per AGENTS.md, and shows visible confirm/cancel key hints.
- `openLauncher` / `openSettings` / `closeSettings` still exist as callable
  exports, so `keyboard.ts`, `events.ts` and `main.ts` compile unchanged.
- `anyModalOpen()` reports correctly while the legacy registry still owns the
  not-yet-migrated modals.

## Invariants

Every phase honours the Invariants section of the [master plan](react-ui-rewrite.md#invariants-every-phase--violating-any-reintroduces-a-shipped-bug).
Violating any one reintroduces a shipped bug.

## Verification

Per the master plan's Verification block, compared against
`.plans/react-rewrite-flake-baseline.md`.

## Decision log

**2026-09-02 — `anyModalOpen()` does NOT OR two sources.** Scope called for the
store to be a second source of "is a modal open" alongside `modals/registry.ts`.
It isn't needed: the registry already answers off the `.hidden` class of every
registered root, and both React modals keep their root registered and keep
toggling that class (from the island's layout effect). So the DOM class stays
the single source of truth for "a modal owns the keyboard", and `app/focus.ts`,
`app/session-term.ts` and every `getElementById(...).classList` gate in
`keyboard.ts` are untouched — the smaller diff *and* the one with no
two-sources-of-truth to keep in agreement.

**2026-09-02 — modal entries, not bare ids.** `modals: ModalEntry[]` where an
entry is `{ id: 'launcher'; seq; req }` or `{ id: 'settings'; seq }`, rather than
the planned `ModalId[]`. Every launcher opening is parameterised (which project,
duplicate-from, resume-in-worktree), and `seq` — minted by `openModal` — is what
the component keys its per-open state off (`key={entry.seq}`). That key IS the
port of the imperative `openGeneration` / `openToken` counters: a re-open
remounts the body, so a stale response lands on an unmounted component and the
"did it get reopened under me?" check is just the unmount guard.

**2026-09-02 — `flushSync` on both close paths.** `closeLauncher` and
`closeSettings` run from plain window/DOM listeners, so an ordinary store write
is flushed a microtask later — the modal would still be visible when
`refocusActiveTerm()` ran, and `app/focus.ts` refuses to touch the terminal
while a modal is open. Four e2e specs caught exactly that ("closing returns the
keyboard to the terminal"). Wrapping the `closeModal` call in `flushSync` keeps
the blur → hide → refocus order the imperative version had.

**2026-09-02 — focus-on-open is a passive effect, not a layout one.** Layout
effects run child-first, so when the body's ran, the island had not yet removed
`.hidden` — and `focus()` on a `display: none` element is a silent no-op. jsdom
has no CSS, so the DOM tests stayed green while the browser lost the filter box's
focus entirely. Positioning stays in the layout effect (no flash); focus moved to
a passive one, which runs after the island's layout effect has revealed the
popup.

**2026-09-02 — `settings.ts` and `launcher.ts` are gutted, not deleted.**
Scope said "deleted". Both files still export the open/close pair every caller
imports from that path, plus what is not rendering: the launch-count table, the
three session actions (duplicate / restart / duplicate-choose-tool), the argv
splitter Go's validator mirrors, and `initThemeWatch`, which is not part of the
modal at all. Deleting the modules would have meant rewriting every import in
`keyboard.ts`, `events.ts`, `main.ts`, `Sidebar.tsx` and `EmptyState.tsx` for no
gain — the same shape as Phase 2's gutted `banners.ts`.

**2026-09-02 — the dialog root moved into `index.html`.** `#settings` is now a
static empty `div.hv-dialog.hidden` with the `role`/`aria-modal`/
`aria-labelledby` `ui/dialog.ts` used to stamp, because a React root needs a
mount node that exists before the modal is first opened. `#settings-scroll` and
`#settings-updates` stay direct children of `.hv-dialog__body` — `settings.css`
pins Updates below the scrolling region — so the Enter-to-save listener hangs off
the root element rather than a wrapper div.

## Progress

**2026-09-02** — Implemented. Store (`modals` slice + `openModal`/`closeModal`/
`isModalOpen`/`modalEntry`); new `components/modals/{ModalShell,Launcher,Settings}.tsx`;
`app/modals/{launcher,settings}.ts` gutted to the store-backed open/close pairs;
`index.html` gained the `#settings` root; `main.ts` mounts the two islands;
`IconButton` gained an `id` prop (the dialog close button needs `#settings-close`).

Tests: `launcher.test.ts`, `settings.test.ts` and `settings-updates.test.ts`
rewritten to RTL `.tsx` with every case ported and counts unchanged
(36 / 19 / 11); new `modal-shell.test.tsx` (7). The e2e specs are unmodified —
they are the proof the DOM contract survived.

**2026-09-02 — Verification.** `npm run typecheck` clean; `npx biome ci .` clean
(8 pre-existing warnings, same set as `main`); `scripts/ui-lint.sh --strict`
0 violations; `vite build` succeeds; vitest **78 files / 848 tests** green;
`go build ./...` + `go test ./internal/... ./cmd/hivegui/...` green; Playwright
e2e **258 passed / 0 failed / 31 skipped**; `npm run test:e2e:real` **24 passed**.
No flake-baseline comparison was needed: nothing failed.

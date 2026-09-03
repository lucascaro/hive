# Add a "check for updates" button to the sidebar

- **Spec:** [docs/product-specs/323-add-check-for-updates-button-to-sidebar.md](../../product-specs/323-add-check-for-updates-button-to-sidebar.md)
- **Issue:** #323
- **Status:** active

## Summary

Put a "Check for updates" icon button in the sidebar header, next to the
"New project" (+) button, wired to the update check that already exists. Nothing
new is built on the Go side: `manualUpdateCheck()` is already exported and
already drives the macOS menu item. This is a discoverability fix plus a small
CSS de-duplication that moves both header buttons onto the documented
`iconButton()` primitive.

## Research

Plan authored via plan-first mode (`/hs-feature-loop --full-auto plan`).

**Relevant code**

- `cmd/hivegui/frontend/src/app/banners.ts:366` — `manualUpdateCheck()`, already
  exported. Shows "Checking for updates…", calls `CheckForUpdate()`, routes
  every outcome through `applyUpdateInfo(info, { manual: true })`, and is
  guarded by the module-level `updateCheckInFlight` flag against double-fires.
  On rejection it shows `Update check failed: …`.
- `cmd/hivegui/frontend/src/app/keyboard.ts:710` — `'menu:check-for-updates'`
  is the only current caller, dispatched from the macOS app menu.
- `cmd/hivegui/frontend/src/app/banners.ts:406` — `initBanners()`, called once
  from `src/main.ts:373`. The wiring home, since this module owns
  `manualUpdateCheck`.
- `cmd/hivegui/frontend/index.html:86` — `#new-project-btn`, static markup in
  `<aside id="sidebar"><header>`, wired imperatively by `initProjectEditor()`
  (`src/app/modals/project-editor.ts:56`), which injects `icon('plus')`.
- `cmd/hivegui/frontend/src/ui/icon-button.ts` — `iconButton()`, the shared
  primitive. `docs/design-docs/ui/components.md` names 22×22 as the
  sidebar-header size and says never to hand-roll an icon-only `<button>`.
- `cmd/hivegui/frontend/src/theme/components/icon-button.css` — supplies base,
  `[data-size="22"]`, `:hover`, `:disabled` and `:focus-visible` for
  `.hv-icon-btn`.
- `cmd/hivegui/frontend/src/theme/components/sidebar.css:32,43,74` — the three
  `#new-project-btn` rules, fully subsumed by the primitive's CSS.
- `cmd/hivegui/frontend/src/ui/icons.svg:36` — the `hv-download` sprite symbol.
- `cmd/hivegui/frontend/src/ui/icon.ts` — `ICON_NAMES` includes `download`.

**Constraints**

- `test/dom/update-banner.test.tsx:77` and `test/dom/restart-hive.test.tsx:68`
  both call `initBanners()` on a partial scaffold with no sidebar header, so any
  DOM lookup added there must be null-guarded.
- `wireUpdateBanner()` fires a boot-time `CheckForUpdate()` inside
  `initBanners()`, so click tests must `mockClear()` after init or the
  "called once" assertions are off by one.
- `scripts/ui-lint.sh` enforces the token/icon rules in CI.
- `CHANGELOG.md` is generated; user-visible changes add `.changesets/<slug>.md`.

## Approach

Build the new button with `iconButton()` from `src/ui/icon-button.ts` at
`size: 22`, and append it to the sidebar header from `initBanners()` with
`onClick: () => void manualUpdateCheck()`. Because it uses the primitive it
needs **no new CSS**.

The primitive is `background: none; color: var(--fg-subtle)` while the legacy
`#new-project-btn` is `background: var(--btn); color: var(--fg)`, so the two
adjacent buttons would not match. Resolution: adopt `.hv-icon-btn` on
`#new-project-btn` too and delete its three id-selector rules from
`sidebar.css`. Net CSS deletion, consistent pair, and the existing button moves
onto the documented primitive. It keeps its `id` — `initProjectEditor`, the
Launcher's focus fallback (`Launcher.tsx:161`) and
`test/dom/project-editor.test.tsx` all look it up by id — and keeps its
imperative `replaceChildren(icon('plus'))`; only its class and CSS change.

**Accepted visible side effect:** the "+" button loses its filled `var(--btn)`
background and its `var(--fg)` foreground, becoming flat and `--fg-subtle` like
every other icon button in the app. Disclosed in the changeset.

Chosen over a React header component: the sidebar header is still static markup
wired imperatively, and porting it belongs to the React rewrite's own phase.

Chosen over wiring in `project-editor.ts`: that module only wires
`#new-project-btn` because it owns that modal; the update button belongs to the
module that owns `manualUpdateCheck`.

### Files to change

- `cmd/hivegui/frontend/index.html` — add `class="hv-icon-btn"`,
  `data-size="22"` and `type="button"` to the existing `#new-project-btn`. No
  new markup for the check-updates button; the primitive creates it at runtime.
- `cmd/hivegui/frontend/src/app/banners.ts` — import `iconButton` from
  `../ui/icon-button.js`. In `initBanners()`, look up `#new-project-btn`
  (null-guarded), then append
  `iconButton({ icon: 'download', label: 'Check for updates', size: 22,
  onClick: () => void manualUpdateCheck() })` to its parent, setting
  `id = 'check-updates-btn'` on the returned element. Guard against a double
  append if `initBanners()` runs twice.
- `cmd/hivegui/frontend/src/theme/components/sidebar.css` — delete the
  `#new-project-btn` base, `:hover` and `:focus-visible` rules. Add `gap: 6px`
  to `#sidebar header` and `margin-right: auto` to `#sidebar header .brand`, so
  the two buttons sit adjacent on the right rather than being spread apart by
  the header's `justify-content: space-between`, while `.brand`'s existing
  `min-width: 0 / overflow: hidden / text-overflow: ellipsis` keeps truncating.

### New files

- `cmd/hivegui/frontend/test/dom/check-updates-button.test.ts` — DOM-layer
  tests. Plain `.ts`: no JSX.
- `.changesets/323-check-updates-button.md` — `type: added`, `bump: minor`.
  Frontmatter copied from an existing entry
  (`.changesets/305-ide-theme-presets.md`), since the `.changesets/README.md`
  schema AGENTS.md cites does not exist in the repo.

### Tests

`cmd/hivegui/frontend/test/dom/check-updates-button.test.ts` (vitest + jsdom,
`dom` layer), mocking `../../src/bridge.js`:

- `initBanners adds a check-updates icon button to the sidebar header` — mount
  the header scaffold plus the `#terms` / `#projects` / `#status` elements
  `banners.ts`'s `dom.js` import needs, call `initBanners()`, assert
  `#check-updates-btn` exists, is `.hv-icon-btn` with `data-size="22"`, has
  `aria-label="Check for updates"`, is the next sibling of `#new-project-btn`,
  and contains an `svg.hv-icon` whose `<use>` `href` is `#hv-download`.
- `initBanners is a no-op on markup with no sidebar header` — mount the scaffold
  without `#new-project-btn`, call `initBanners()`, assert it does not throw and
  no `#check-updates-btn` appears. Covers the null guard the two existing
  `initBanners()` callers depend on.
- `clicking the button runs a manual update check` — `mockClear()` the boot
  poll, click, assert `CheckForUpdate` was called once and the store's `update`
  banner text is "Checking for updates…".
- `two rapid clicks fire only one check` — two synchronous clicks before the
  mocked promise resolves; assert `CheckForUpdate` called exactly once.
- `a failed check surfaces the failure in the banner` — mocked `CheckForUpdate`
  rejects; await, assert the banner text starts with `Update check failed:`.

## Decision log

- **2026-09-03** — Use `iconButton()` rather than hand-rolled markup + copied
  id-selector CSS. Why: `components.md` forbids hand-rolled icon-only buttons
  and the primitive's CSS already covers every state, so this adds zero CSS.
- **2026-09-03** — Migrate `#new-project-btn` onto `.hv-icon-btn` in the same
  PR. Why: otherwise the two adjacent header buttons render differently
  (filled vs transparent). Net CSS deletion; the alternative was shipping a
  visible inconsistency.

## Progress

- **2026-09-03** — Plan-first scaffold; stage = IMPLEMENT (set in spec
  frontmatter). Gate 4 reviewer approved at confidence 8 after one revision.

## Open questions

- Deleting the `#new-project-btn` rules changes an existing control's look.
  Grep `test/e2e` and `test/dom` for `new-project-btn` before landing in case an
  assertion pins its background.

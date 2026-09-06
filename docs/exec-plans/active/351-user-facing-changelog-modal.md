# Show a user-facing changelog behind a gift icon in the sidebar

- **Spec:** [docs/product-specs/351-user-facing-changelog-modal.md](../../product-specs/351-user-facing-changelog-modal.md)
- **Issue:** #351
- **PR:** #357
- **Branch:** `feature/351-user-facing-changelog-modal`
- **Status:** active

## Summary

Add a gift icon button to the sidebar header that opens a modal rendering the
project's `CHANGELOG.md` as a user-facing "what's new" list, newest version
first. The changelog is bundled at build time, so the modal always matches the
running build and works offline. The gift carries an unread dot until the user
opens it on a build newer than the one they last read.

## Research

### The modal system (`cmd/hivegui/frontend`)

`ModalShell` (`src/components/modals/ModalShell.tsx:39-63`) owns header/title/close,
Escape (`:85-90`), backdrop-click and Tab focus trapping (`:83-115`). It does **not**
own the dialog root element or its `hidden` class. Every modal is two files:

- `src/app/modals/<name>.ts` — non-React half: `open*`/`close*`, `openModal`/`closeModal`/
  `isModalOpen`, `releaseFocus`, `flushSync`. Template: `help-overlay.ts` (no daemon
  round-trip, closest shape to this one).
- `src/components/modals/<Name>.tsx` — reads `useAppStore(s => s.modals.find(...))`,
  toggles `root.classList.toggle('hidden', !entry)` in a `useLayoutEffect`, remounts the
  body with `key={entry.seq}`, renders `<ModalShell>`.

There is no dynamic registry: a new modal must also touch `store.ts:115-133`
(`ModalId` + `ModalEntry` unions), `index.html` (dialog root div, alongside the ones at
`index.html:130-145`) and `App.tsx:98-149` (`mustEl` + `createPortal`). Palette entry is
optional, `main.tsx:130-186`.

### The sidebar header

`SidebarHeaderControls` (`src/components/Sidebar.tsx:542-561`) portals an `Icon` into the
`#new-project-btn` that `index.html:92-93` owns, then portals a whole `IconButton`
(`#check-updates-btn`, `size={22}`) into the `<header>` itself. A third button is the same
pattern, listed after the second so it lands third in DOM order — i.e. rightmost.
`.brand { margin-right: auto }` (`src/theme/components/sidebar.css:25-35`) already
right-aligns the button cluster. `test/e2e/sidebar-header-actions.spec.ts` records that the
header layout previously had to be re-tuned when it went from one button to two, so the
3-button case needs a real browser check, not reasoning.

### Icons

`ICON_NAMES` (`src/lib/icon-sprite.ts:17-42`) is a hand-maintained `as const` array that
must match the `<symbol id="hv-*">` set in `src/lib/icons.svg`. Symbols are `viewBox="0 0 24 24"`
and inherit `fill="none" stroke="currentColor" stroke-width="1.75"` from the sprite root.
`test/dom/ui-icon.test.tsx:52-57` asserts every declared name resolves **and** hardcodes
`expect(ICON_NAMES).toHaveLength(24)` — that number has to move with the new icon.

### The user-facing feature list

`site/features.yml` is the single curated user-facing list ("Living feature list rendered on
the website"): 13 entries, `title` / `blurb` / `status: shipped|planned` / optional `highlight`.
`site/build.mjs` parses it with the `yaml` package and renders shipped entries as cards and
planned entries as an `<li>` list into `site/src/index.html`.

`site/build.mjs` separately parses the repo-root `CHANGELOG.md` with `marked` into
`site/dist/changelog.html`. That page stays as it is; it is the full engineering changelog and
is not what this modal shows.

`CHANGELOG.md` itself is **generated** — `scripts/regen-generated.py` rewrites its
`[Unreleased]` body from `.changesets/*.md`, and CI's `block-generated-edits` job fails any
PR that edits it directly.

### Persistence

No app-wide storage helper. `localStorage` is called per feature at the call site
(`app/banners.ts:315`, `app/keyboard.ts:681`, `modals/Settings.tsx:310`,
`modals/Launcher.tsx:432`). `src/lib/collapsed.ts:1-59` is the model to copy: a pure,
storage-agnostic module with the `getItem`/`setItem` done by the caller. It also has
`namespacedKey(base, ns)` because one webview process shares `localStorage` across windows —
not needed here, since "what have I read" is genuinely per user, not per daemon.

### Prior lessons

No prior lessons matched (`brain-search` returned no ranked hits for this feature's terms).

## Approach

**One list, two renderers.** `site/features.yml` becomes the single user-facing "what shipped"
list, gains a `since:` version stamp on every shipped entry, and is read by both the website
build and the in-app modal. The engineering `CHANGELOG.md` is untouched and keeps feeding
`site/dist/changelog.html`; it is not what a user sees behind the gift.

**Resolving the cross-boundary import.** `site/features.json` lives outside
`cmd/hivegui/frontend`, which two things care about. `tsconfig.json` sets
`"module": "ESNext"` and no `resolveJsonModule`, so the default is `false` and `npm run
typecheck` would reject the import — that flag has to be added. And `vite.config.js` sets no
`server.fs.allow` (`vite.config.js:44-47`), so the dev server's default workspace root may
deny `/@fs/…/site/features.json`, which would break **every** Playwright spec, not just the new
ones. Fix: `server.fs.allow` naming the repo root. `vite build` inlines the JSON and is
unaffected either way, so this is a dev-server-only concern — but it is the one e2e runs
through, so it gets checked empirically against a cold server **before** the shape is committed
to. Fallback if it misbehaves: move the file to
`cmd/hivegui/frontend/src/data/features.json` and have `site/build.mjs` read it by path —
Node has no such restriction, so the constrained consumer wins the location.

**The list moves from YAML to JSON** (`site/features.yml` → `site/features.json`). This is the
one non-obvious call, and it makes the file *cheaper*, not more expensive: Vite and TypeScript
read JSON natively, so the GUI gets a typed, build-time import with **zero new dependencies and
no Vite plugin**, and `site/build.mjs` swaps `YAML.parse` for `JSON.parse` and drops the `yaml`
devDependency entirely. The alternative — keep YAML, add `yaml` plus a transform plugin to both
`vite.config.js` and `vitest.config.js` — adds a dependency and two config edits to preserve
comment support in a 13-entry file. Net: one dependency deleted rather than one added.

**The unread dot reads the bundled list, not the running binary.** The app's version arrives at
runtime over the Wails `daemon:stale` event (`components/VersionFooter.tsx:29-31`), which is the
wrong clock for this: the list is bundled into the build, so the highest `since` in the bundled
list *is* this build's frontier. `hasUnread()` compares that against
`localStorage['hive.whatsNewSeen']`. **A missing key shows the dot**, deliberately — the users who
most need to discover this modal are the ones updating into the release that adds it, and they
have no stored value.

**Rendering is a `.map()`, not a parser.** Because the source is structured data rather than
markdown, there is no markdown to parse in the app at all: group by `since`, sort versions
descending by a small semver comparator, render `<h3>version</h3>` + title/blurb per entry,
then a "Coming soon" section for `status: planned`. No `marked`, no `innerHTML`, no XSS surface.
The hand-rolled parsing decided at clarifying round B shrinks to that comparator plus the grouping.

### Files to change

1. `site/features.yml` — **deleted**, replaced by (see New files). Backfilled `since` values,
   derived by matching each feature's terms against `CHANGELOG.md` version sections:
   `2.0.0` for the six launch features (sessions outlive the GUI, every agent, grid view,
   git worktrees, know who needs you, multi-window), `2.1.0` in-app updates,
   `2.5.0` undo a close, `2.6.0` 17 themes.
2. `site/build.mjs` — `JSON.parse(readFileSync(features.json))` in place of `YAML.parse`;
   drop the `import YAML from "yaml"`; fix the stale `features.yml` in its own header comment
   (`build.mjs:2`). Add an assertion that every `status: shipped` entry carries a `since`, so
   the site build fails loudly rather than the modal silently dropping a feature into an
   `undefined` bucket.
3. `site/package.json` — remove the `yaml` devDependency (`package-lock.json` regenerated).
4. `site/src/index.html:37` — the "edit this list" link points at
   `blob/main/site/features.yml` and 404s after the rename.
5. `cmd/hivegui/frontend/tsconfig.json` — add `"resolveJsonModule": true`.
6. `cmd/hivegui/frontend/vite.config.js` — add `server.fs.allow` for the repo root.
7. `cmd/hivegui/frontend/vitest.config.js` — a standalone config that does **not** extend
   `vite.config.js`, and the one that runs the `features.json` import in the unit test the plan
   designates as the PR gate. Same treatment as item 6, confirmed empirically rather than
   assumed; it fails loudly under `scripts/test.sh` if it is needed and missing.
8. `cmd/hivegui/frontend/index.html` — add the dialog root
   `<div id="whats-new" class="hv-dialog hidden" role="dialog" aria-modal="true" aria-labelledby="whats-new-title">`
   beside the existing roots.
9. `cmd/hivegui/frontend/src/store/store.ts` — `'whats-new'` added to the `ModalId` union
   (`:115-121`) and `{ id: 'whats-new'; seq: number }` to `ModalEntry` (`:127-133`).
10. `cmd/hivegui/frontend/src/components/App.tsx` — `mustEl('whats-new')` +
   `createPortal(<WhatsNew root={whatsNew} />, whatsNew)`.
11. `cmd/hivegui/frontend/src/components/Sidebar.tsx` — third portal in
   `SidebarHeaderControls`: `<IconButton id="whats-new-btn" icon="gift" label="What's new"
   size={22} className={unread ? 'hv-unread' : undefined} onClick={...} />`. `IconButton`
   (`src/components/IconButton.tsx:7-25`) has no `data-*` passthrough but does take
   `className`, so the dot hangs off a class and the shared primitive needs no new prop.
   `unread` is a `useState` in `SidebarHeaderControls`, seeded from `hasUnread()` on mount and
   set `false` by the click handler before it calls `openWhatsNew()` — a bare `localStorage`
   read at render would never re-render, and the dot would sit there until a reload.
12. `cmd/hivegui/frontend/src/lib/icons.svg` — new `<symbol id="hv-gift" viewBox="0 0 24 24">`
   (stroke-only, matching the sheet's inherited stroke attrs).
13. `cmd/hivegui/frontend/src/lib/icon-sprite.ts` — `'gift'` added to `ICON_NAMES`.
14. `cmd/hivegui/frontend/test/dom/ui-icon.test.tsx` — sprite length assertion 24 → 25.
15. `docs/design-docs/ui/icons.md` — add `gift` to the action-icon list and move
    "That's 24 symbols" (`icons.md:62`) to 25. The repo's own icon convention requires the
    row; the sprite, `ICON_NAMES`, the test count and this doc move together or not at all.
16. `cmd/hivegui/frontend/src/theme/components/sidebar.css` — unread-dot rule on
    `.hv-icon-btn.hv-unread` (the class item 11 sets — **not** a `data-` attribute, which
    `IconButton` cannot pass through), plus whatever the 3-button header needs after a real
    browser check. Dot colour and size come from theme tokens, since `scripts/ui-lint.sh
    --strict` rejects raw hex and px radius under `src/theme/**`.
17. `cmd/hivegui/frontend/src/main.tsx` — palette command
    `{ id: 'whats-new', name: "What's New…", run: openWhatsNew }`.
18. `README.md` — one line under the feature list; the gift button is user-visible chrome.
19. `AGENTS.md` — documentation-maintenance rule: `site/features.json` is the single
    user-facing feature list, it feeds both the website and the in-app What's New modal, and a
    `bump: minor` change that ships a user-visible feature adds an entry with a `since`.
20. `.changesets/351-whats-new-modal.md` — `type: added`, `bump: minor`.

### New files

- `site/features.json` — the single user-facing list. Same fields as the YAML plus
  `since` on shipped entries.
- `cmd/hivegui/frontend/src/lib/whats-new.ts` — pure module, no DOM: `compareVersions(a, b)`,
  `groupByVersion(features)` → `[{ version, entries }]` newest first, `plannedOf(features)`,
  `latestVersion(features)`, `hasUnread(latest, seen)`, and the `SEEN_KEY` constant.
- `cmd/hivegui/frontend/src/app/modals/whats-new.ts` — `openWhatsNew` / `closeWhatsNew`,
  modelled on `help-overlay.ts`. **`openWhatsNew` writes the seen version; `closeWhatsNew`
  writes nothing.** Opening is the act of reading, and a user who opens the modal and then
  kills the app has still read it.
- `cmd/hivegui/frontend/src/components/modals/WhatsNew.tsx` — the `ModalShell` body.

### Tests

- `test/unit/whats-new.test.ts` (new)
  - `compareVersions` orders `2.10.0 > 2.9.0` and `2.0.0 > 2.0.0-alpha.2` — the naive
    string sort this replaces gets both backwards, so the test fails on a broken comparator.
  - `groupByVersion` returns versions newest-first, buckets every shipped entry exactly once,
    and excludes `planned` entries.
  - `plannedOf` returns only `status: planned`, in file order.
  - `hasUnread(latest, null)` is `true` (the update-into-this-release case);
    `hasUnread('2.6.0', '2.6.0')` is `false`; `hasUnread('2.7.0', '2.6.0')` is `true`;
    `hasUnread('2.6.0', '2.7.0')` is `false` (a downgrade must not nag);
    `hasUnread('2.6.0', 'garbage')` is `true` **once** — an unparseable stored value is
    treated as unseen, not as a permanent nag, because opening rewrites it.
  - Every `status: shipped` entry in the imported `features.json` carries a parseable `since`.
    This is the same assertion `site/build.mjs` makes, duplicated here on purpose: nothing in
    `.github/workflows/ci.yml` builds `site/` (only `pages.yml`, and only on `main`), so the
    site-build assertion gates no PR. `scripts/test.sh` runs this one.
- `test/dom/whats-new.test.tsx` (new)
  - The gift button renders in the sidebar header with an accessible name, and carries the
    unread marker when nothing is stored.
  - Clicking it opens `#whats-new`, and the body renders version headings in descending order
    with each feature's title and blurb, plus the "Coming soon" section.
  - Clicking the gift clears the unread marker in the same render pass, without a reload,
    and the seen version is on disk before the modal body has mounted.
  - The button renders nothing and throws nothing on a scaffold with no sidebar header
    (the existing `SidebarHeaderControls` null-guard).
- `test/e2e/sidebar-header-actions.spec.ts` (**edited**, not only extended) — its existing
  adjacency assertion (`checkBox.x - (addBox.x + addBox.width) <= 8`) and brand-slack check
  were written for a 2-button row and have to move to the 3-button one, or they keep asserting
  the old layout. Then the third button: visible, 22×22,
  `aria-label`, positioned right of `#check-updates-btn` on the same row via `boundingBox()`,
  `elementFromPoint` hit-test (nothing eats the click), `tabIndex >= 0`, and click →
  `#whats-new` loses `.hidden`, Escape closes it.
- `site/build.mjs` — the shipped-needs-`since` assertion fails the site build, but only on
  `main` via `pages.yml`; the unit test above is what actually gates a PR.

### Verification

```
./scripts/ci-bootstrap.sh                       # wailsjs bindings, fresh worktree
cd cmd/hivegui/frontend && npx biome ci .       # ci, not lint — lint does not check formatting
npm run typecheck
cd $(git rev-parse --show-toplevel) && scripts/test.sh    # go · unit · dom · e2e
cd site && npm run build                        # site still builds off features.json
CI=1 npx playwright test --project=chromium      # ALL specs: an fs.allow regression breaks every one
scripts/ui-lint.sh --strict                     # rejects raw hex / px radius in src/theme/**
```
The 3-button header row and the unread dot get looked at in a real browser before this is
called done — vitest is CSS-blind and this change adds a positioned element to a row whose
layout has needed re-tuning before.

## Open questions / risks

- **The backfilled `since` values are inferred, not recorded.** They come from matching each
  feature's keywords against `CHANGELOG.md` sections, so "17 themes → 2.6.0" means "the theme
  count last changed in 2.6.0", not "themes shipped in 2.6.0". Worth an operator eyeball; they
  are user-visible.
- **`features.json` is coarse.** Nine shipped entries collapse into just four version buckets
  — `2.0.0` holds six of them, and `2.2.x`/`2.3.x`/`2.4.x` hold none at all. It reads as a feature tour with version
  stamps rather than a per-release "what's new", and it stays thin unless someone remembers to
  add an entry per release. The AGENTS.md rule above is the only thing enforcing that; a CI check
  that warns when a `bump: minor` changeset lands without a `features.json` touch is the obvious
  follow-up, deliberately not in this PR.
- **YAML → JSON loses the file's comment.** The one comment at the top of `features.yml`
  ("Living feature list rendered on the website…") moves into the AGENTS.md rule.
- **The site build is not on the PR path.** `.github/workflows/ci.yml` never builds `site/`;
  only `pages.yml` does, on `main`. A `features.json` mistake only the site notices lands green
  and breaks the deploy. Mitigated by duplicating the one assertion that matters into the unit
  suite; a site-build step in CI is the real fix, and is follow-up.
- **`server.fs.allow` is dev-server-only.** If it is wrong, `vite build` and `wails build`
  still succeed and only Playwright fails — a failure that looks like flake rather than a
  config bug. Hence the full-suite run in Verification.
- **Header layout at three buttons.** Flagged by `sidebar-header-actions.spec.ts`'s own history.
  Covered by the browser check above rather than assumed away.

## Decision log

- **2026-09-05** — Content source: bundle `CHANGELOG.md` at build time rather
  than fetching GitHub releases at runtime. Why: zero deps, works offline, and
  the text always describes the build actually running. Operator decision at
  clarifying round A.
- **2026-09-05** — Unread affordance: persist a last-seen version locally and
  show an attention dot on the gift when the running build is newer. Why: that
  signal is the reason the gift-icon pattern exists; without it nobody opens
  the modal. Operator decision at clarifying round A.
- **2026-09-05** — **Reversal of the round-A content-source decision.** Round A settled on
  bundling `CHANGELOG.md`; round B's answer moved the source to the curated user-facing list
  the website already renders, so the modal reads `site/features.json` and `CHANGELOG.md` is
  not bundled into the app at all. The round-A entry above is kept rather than edited because
  this log is append-only.
- **2026-09-05** — The unread dot is component state seeded from `localStorage`, not a
  `localStorage` read at render. Why: a read at render never re-renders, so the dot would
  outlive the click that cleared it.
- **2026-09-05** — Show the full history, scrollable, newest first, rather than
  collapsing older versions. Why: no truncation logic to get wrong, and the
  file is small enough to scroll. Operator decision at clarifying round A.

## Progress

- **2026-09-05** — Spec created, triaged M / P2, research started.
- **2026-09-05** — Plan approved by the operator. Implemented on
  `feature/351-user-facing-changelog-modal`.

## Implementation notes

- **`vitest.config.js` needed no change** (plan item 7). `server.fs.allow` is a
  dev-server *serving* restriction; vitest transforms the import through its own
  module pipeline and never consults it. Verified by running the unit suite
  against the real `site/features.json` before touching any other file, rather
  than assuming either way. `vite.config.js` still needs it — the e2e suite runs
  against `vite dev`.
- The `docs/design-docs/ui/icons.md` count and `ui-icon.test.tsx`'s
  `toHaveLength(24)` both moved to 25, as did `ICON_NAMES` and the sprite. Four
  places, one icon; the test is what stops three of them drifting.
- The unread dot is `::after` on the button itself rather than a sibling
  element, so `elementFromPoint` at the button's centre still resolves to the
  button. The e2e hit-test asserts exactly that.
- Verified in a real browser (not just jsdom): the three-button header row lays
  out right-aligned and evenly spaced, and the dot paints in
  `--state-attention`. The modal renders releases newest-first with `2.6.0` at
  the top and scrolls inside its own 60vh body.

## Second opinion

Two rounds with a `general-purpose` reviewer, both against the plan on disk.

- **Round 1 — `verdict: revise`, `confidence: 8`.** Caught that the central technical bet was
  not resolvable as configured: `tsconfig.json` has no `resolveJsonModule` (module is `ESNext`,
  so the default is `false`) and `vite.config.js` sets no `server.fs.allow`, which would fail
  typecheck and 403 the dev server respectively — the latter breaking *every* Playwright spec,
  not only the new ones. Also found three missed consumers (`site/src/index.html:37`'s
  "edit this list" link, `docs/design-docs/ui/icons.md:62`'s "That's 24 symbols", and the fact
  that nothing in `.github/workflows/ci.yml` builds `site/`, so the plan's `since` assertion
  gated no PR), an internal contradiction on whether the seen-version is written on open or on
  close with no reactive source for the dot, and that `IconButton` has no `data-*` passthrough
  for the planned selector. **All eight applied.**
- **Round 2 — `verdict: revise`, `confidence: 8`.** Confirmed eight of nine fixes landed
  concretely and that scope still maps to the spec, but caught two self-inconsistencies the
  first revision introduced: the CSS rule still named `[data-unread]` while the component now
  sets a `className`, and `vitest.config.js` is a separate config from `vite.config.js` and
  runs the very import the PR gate depends on. **Both applied**, along with its three
  nice-to-haves (stale `build.mjs:2` comment, duplicated list numbering, and a risk bullet that
  claimed seven version buckets where the backfill produces four).

Disposition: the pipeline allows one revise cycle, so the plan comes to the operator here. The
round-2 items were plan-text contradictions rather than design objections, and both are now
fixed; no reviewer objection to the approach itself is outstanding.

## PR convergence ledger

Append-only. One line per `/hs-review-loop` iteration.
- **2026-09-06 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 2354a4c8; threads_open: 0; action: fixes-applied+push (3 IMPORTANT + 2 MINOR fixed by hand rather than stopping on COMMENT); head_sha: 1048470.

# Show an update pip on the Check for updates button instead of auto-showing the update bar

- **Spec:** [docs/product-specs/436-update-pip-on-check-for-updates-button.md](../../product-specs/436-update-pip-on-check-for-updates-button.md)
- **Issue:** #436
- **Status:** active
- **PR:** #437
- **Branch:** feature/436-update-pip-on-check-for-updates-button

## Summary

A background update check (boot poll, 6h `update:available` event) currently pops the update banner. Replace that with a pip on the sidebar's ⤓ "Check for updates" button, reusing the What's New gift's `.hv-unread` dot. The banner still appears for everything the user initiated: a manual check (button / menu / palette / `check_update` command) and staging progress, ready, and errors.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/app/banners.ts:325-400` — `applyUpdateInfo(info, {manual})`. The `info.available` branch auto-shows the banner on non-manual calls unless `localStorage['hive.updateDismissedFor'] === info.latest`. Called from `wireUpdateBanner` (`update:available`, `update:progress`, boot `CheckForUpdate()`) and `manualUpdateCheck` (`manual: true`).
- `banners.ts:236-320` — `UPDATE_DISMISS_KEY`, `showUpdateBanner` (writes `data.version` solely as the dismissal key), `dismissUpdateBanner` (writes the key).
- `cmd/hivegui/frontend/src/components/Sidebar.tsx:707-770` — `SidebarHeaderControls` renders `#check-updates-btn` (`IconButton`, `onClick=manualUpdateCheck`) and `#whats-new-btn` with `className={unread ? 'hv-unread' : undefined}` and a label that carries the unread bit in words.
- `cmd/hivegui/frontend/src/theme/components/icon-button.css:23-39` — `.hv-icon-btn.hv-unread::after` dot in `--state-attention`. Generic to `.hv-icon-btn`, so reusable as-is.
- `cmd/hivegui/frontend/src/store/store.ts` — app store (`whatsNewSeen` is the precedent for a header-control flag read via `useAppStore`).
- `cmd/hivegui/update_action.go:66-75` — Go keeps `Available` true through staging (`rememberCheck` while busy), so `info.available` is a stable "update pending" signal until install.
- Tests pinning current auto-show: `test/dom/update-banner.test.tsx` (tests at :95, :195, :211 emit `update:available` and expect a visible banner; dismissal describe at :248-296), `test/e2e/banner-visibility.spec.ts:17-42` (emits `update:available` to show the banner), `test/dom/check-updates-button.test.tsx` (button markup).
- `README.md:180-186` — "shows an 'Update available' banner".

### Constraints / dependencies

- None blocking. Pure frontend; no wire/daemon-contract change.

### Prior lessons

- No prior lessons matched (`brain-search 'update banner pip sidebar'`).

### Conventions card

- Test: `scripts/test.sh [go|unit|dom|e2e]` (all layers by default). Frontend lint: `npx biome ci .` in `cmd/hivegui/frontend`; typecheck `npm run typecheck` (needs `./scripts/ci-bootstrap.sh` in a fresh worktree). Local Playwright with `CI=1`.
- TDD: every behaviour change ships with the test that would have caught it; frontend tests under `cmd/hivegui/frontend/test/{unit,dom,e2e}`.
- User-visible change adds `.changesets/<slug>.md` (`type`, `bump`, `issue`, `pr`). `CHANGELOG.md` / `docs/product-specs/index.md` are generated — never edit.
- CSS/layout changes are validated in a real browser (Playwright mock), not jsdom.
- Update README/docs when user-visible behaviour changes.

## Approach

Add one boolean to the app store, `updatePending`, owned by `applyUpdateInfo` in `app/banners.ts`: every UpdateInfo that reaches it sets `updatePending = !!info.available` (Go keeps `Available` true through staging: `setStage` emits a snapshot of `last`, and `StartUpdate` refuses unless `Available` — update_action.go:124-153). The `available` branch then returns early unless `manual`, so background results only move the pip. `SidebarHeaderControls` reads the flag and puts the existing `hv-unread` class (and a worded accessible name) on `#check-updates-btn`. Clicking keeps calling `manualUpdateCheck`, whose `manual: true` path shows the banner.

Why a store flag over reading `banners.update` state: the pip must survive banner dismissal and must not depend on whether a banner happens to be up. Why not a new CSS rule: `.hv-icon-btn.hv-unread::after` is already generic to icon buttons.

The per-version dismissal (`hive.updateDismissedFor`, `showUpdateBanner`'s `version` option, the key write in `dismissUpdateBanner`) only ever suppressed the automatic banner. With no automatic banner it is dead, and is deleted rather than kept.

### Files to change

1. `cmd/hivegui/frontend/src/store/store.ts` — add `updatePending: boolean` (initial `false`, included in `resetStore`) and an exported `setUpdatePending(v)` that no-ops when unchanged. Fix the `setBanner` doc comment (store.ts:1023-1026) that justifies wholesale `data` replacement by the deleted dismissal key.
2. `cmd/hivegui/frontend/src/app/banners.ts` — `applyUpdateInfo`: set the pip right after the existing `if (!info) return` guard (a null boot-poll result must not clear a pip an event just set); in the `available` branch replace the dismissal read with `if (!manual) return;`; delete `UPDATE_DISMISS_KEY`, the `version` option/data of `showUpdateBanner`, and the localStorage write in `dismissUpdateBanner` (which becomes a plain hide). Update the comments that describe the auto banner / dismiss key: the header block (banners.ts:236-244), the `renderUpdateAction` note (:266-269) the "stays sticky" note (:326-328), the `data` comment inside `showUpdateBanner` (:280-285) and the boot-poll comment (:411-413).
3. `cmd/hivegui/frontend/src/components/Sidebar.tsx` — `SidebarHeaderControls` reads `updatePending`; `#check-updates-btn` gets `className={pending ? 'hv-unread' : undefined}` and label `pending ? 'Check for updates — update available' : 'Check for updates'`.
4. `cmd/hivegui/frontend/src/theme/components/icon-button.css` — comment only: the unread dot now serves two buttons.
5. `README.md` "Updating" — background check puts a dot on ⤓ instead of showing a banner; clicking shows it.
6. `.changesets/update-pip-on-check-for-updates-button.md` — `type: changed`, `bump: patch`, `issue: 436`.

### New files

- The changeset above.

### Tests

- `test/dom/update-banner.test.tsx`:
  - new `it('a background update:available sets the pip and leaves the banner down')` — emit `update:available` (available, 2.5.0); assert `#update-banner` hidden and `store.updatePending === true`.
  - `it('offers Update when a release is available…')`, `it('points at the releases page…')`, `it('does not point at the releases page on the latest channel')` — drive through `manualUpdateCheck()` with `CheckForUpdate` resolving the same payload instead of emitting `update:available`. Each additionally asserts `#update-banner` is visible and `bannerText()` contains `2.5.0` and not `Checking` — the action button alone is filled before any early return, so it cannot prove the manual branch shows the banner.
  - replace the "update banner dismissal" describe with `it('dismissing the banner keeps the pip, and a later poll does not reopen it')` — manual check shows banner, dismiss, emit `update:available` again; banner hidden, pip still true, `localStorage.getItem('hive.updateDismissedFor')` null.
  - new `it('a check reporting no update clears the pip')` — pip set by a background event, then `manualUpdateCheck()` resolving `{available:false,current:'2.5.0'}`; pip false.
  - new `it('a background result with no update clears the pip')` — pip set by `update:available`, then `update:progress` with `available:false` (reachable after `forgetUpdateState` + `setStage(StageIdle)`, noted in the test); pip false. Plus the boot-poll path: `CheckForUpdate` resolving `{available:false}` through `initBanners()` also clears it.
- `test/dom/check-updates-button.test.tsx`:
  - new `it('shows the unread dot and says so when an update is pending')` — set `updatePending` true in the store; button has `hv-unread` and accessible name "Check for updates — update available"; false → neither.
  - new `it('a background update:available puts the dot on the button')` — header mounted plus `initBanners()`, emit `update:available`; button gets `hv-unread` (event → store → class, end to end in jsdom).
- `test/e2e/banner-visibility.spec.ts` — the hidden-download case now emits `update:progress` `{stage:'staging', url:''}` (still a visible banner with Download hidden), since `update:available` no longer shows one.
- `test/e2e/sidebar-header-actions.spec.ts` — new `test('a background update shows a dot on the check-for-updates button, not a banner')` — assert baseline first (no `hv-unread`, `::after` content `none`), then emit `update:available`; `#update-banner` toBeHidden; `toHaveClass(/hv-unread/)`; `getComputedStyle(#check-updates-btn, '::after').content !== 'none'` and non-zero width (real layout, since jsdom is CSS-blind).

## Verification

- `cd cmd/hivegui/frontend && npx vitest run test/dom/update-banner.test.tsx test/dom/check-updates-button.test.tsx` — the new background-event test fails on today's code (banner visible).
- `CI=1 npx playwright test test/e2e/sidebar-header-actions.spec.ts test/e2e/banner-visibility.spec.ts`
- `scripts/test.sh unit dom e2e`, `npx biome ci .`, `npm run typecheck`.
- `grep -rn updateDismissedFor cmd/hivegui/frontend/src` and for `UPDATE_DISMISS_KEY` / `dismiss key` / `dismissal key` returns nothing.

## Open questions / risks

- Users who dismissed a version keep a stale `hive.updateDismissedFor` in localStorage; inert, not cleaned up.
- ~~Changing the channel in Settings clears Go's `last` without an event, so a pip can outlive it.~~ Fixed in review (see Decision log).
- The macOS menu/palette/`check_update` all route through `manualUpdateCheck`, so they keep the banner.

## Second opinion

- Round 1: **revise** (confidence 8). Must-fix: manual-path test could not fail (action button filled before the early return); stale dismissal-key comments at store.ts:1023-1026 and banners.ts:266-269; no background pip-clear test and an untested `stage==='ready'` branch. All 3 applied (branch removed — Go keeps `Available` through staging).
- Round 2: **approve** (confidence 8). Nice-to-haves applied: set the pip after the null guard, two more stale comments, boot-poll clear test.

## Decision log

- **2026-09-18** — Pip stays until the update is installed (not cleared on click/seen). Why: operator choice; it mirrors a real pending state, and makes the per-version dismissal key dead code, which is deleted rather than preserved.
- **2026-09-18** — Assumption: manual checks (button, macOS menu, palette, `check_update` command) keep showing the banner, and staging/ready/error keep auto-showing it. Why: the user initiated those; only unsolicited background results move to the pip.
- **2026-09-18** — Assumption: clicking the pipped button runs the existing `manualUpdateCheck` (fresh check, then banner with Update action). Why: no new path; the result is always current.
- **2026-09-18** — Review iter 1 escalated the stale-dot-after-settings-change gap (RISKY: Go change). Operator chose to fix: `SaveUpdateSettings` now calls `setStage(StageIdle, "")` after `forgetUpdateState()`, and `setStage` emits through the existing `emitFn` seam so it is testable. Why: the dot, unlike the old banner, cannot be dismissed.

## Progress

- **2026-09-18** — Spec, issue #436, research written.

- **2026-09-18** — Plan approved (chat, fast lane).
- **2026-09-18** — Implemented; 8 new dom tests fail on the old source and pass now; `scripts/test.sh unit dom e2e`, `biome ci`, `typecheck` green.

- **2026-09-18** — PR #437 opened.

## Open questions

## PR convergence ledger

- **2026-09-18 iter 1** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 889a451ee8fc115c28f4bb0d2053ea54d495c12d39614826a160bd287cd609cf; threads_open: 0; action: escalated:risky-fix-needs-human-decision; head_sha: 27d1c78.

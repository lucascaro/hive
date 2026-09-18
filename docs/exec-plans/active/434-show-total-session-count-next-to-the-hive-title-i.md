# Show total session count next to the Hive title in the sidebar

- **Spec:** [docs/product-specs/434-show-total-session-count-next-to-the-hive-title-i.md](../../product-specs/434-show-total-session-count-next-to-the-hive-title-i.md)
- **Issue:** #434
- **Status:** active

## Summary

Add a live total-session count next to the "Hive" brand in the sidebar header, so the operator can see how many sessions exist without summing project cards or expanding minimized projects.

## Research

### Relevant code

- `cmd/hivegui/frontend/index.html:89-98` — the sidebar `<header>` is static markup: `<span class="brand">Hive</span>` then `#new-project-btn`. React fills the header through portals.
- `cmd/hivegui/frontend/src/components/Sidebar.tsx:708` — `SidebarHeaderControls` portals the header's icon buttons into `#new-project-btn` / its parent header, reading the store via `useAppStore`. Mounted once in `src/components/App.tsx:124`. The natural home for the count.
- `cmd/hivegui/frontend/src/theme/components/sidebar.css:17-35` — `#sidebar header` is a flex row; `.brand` has `margin-right: auto` and eats the slack (truncates). A sibling span after `.brand` would be pushed right with the buttons, so the count must live *inside* `.brand` (or the auto-margin must move).
- `cmd/hivegui/frontend/src/store/store.ts:65,596-635` — `sessions: SessionInfo[]` is every session the daemon reports, including minimized sessions and sessions of minimized projects; `setSessions` / add / update / remove keep it live.
- `cmd/hivegui/frontend/src/components/Sidebar.tsx:510` + `ProjectCard.tsx:53-61,134` — the per-project count already counts every session of the project (minimized included) and renders as `.hv-project-card__count`: `--fg-subtle`, `--font-mono`, `--text-xs`, tabular-nums (`project-card.css:81-87`). Same visual vocabulary for the header total.
- `cmd/hivegui/frontend/test/dom/check-updates-button.test.tsx` — the pattern for testing `SidebarHeaderControls`: fixture header markup, fresh module registry, `render(<SidebarHeaderControls />)`, store seeded through `appStore`.

### Constraints / dependencies

- Header width is tight: brand + three 22px icon buttons. The count must truncate with the brand, not push buttons off.
- `.brand` has `letter-spacing: 0.04em` and `color: var(--accent)`; the count needs its own color/spacing overrides.

### Prior lessons

No prior lessons matched (`brain-search 'sidebar header session count brand'` returned no hits).

### Conventions card

- Frontend commands (from `cmd/hivegui/frontend`): `npm run typecheck` (needs `./scripts/ci-bootstrap.sh` in a fresh worktree), `npx biome ci .`, `npm test` (vitest), `npm run test:e2e` (Playwright vs Wails mock, run with `CI=1`). Layer runner: `scripts/test.sh [go|unit|dom|e2e]`.
- TDD: every behavior change ships with its test; frontend tests live under `cmd/hivegui/frontend/test/`.
- User-visible change → add `.changesets/<slug>.md` (frontmatter `type`, `bump`, `issue`, `pr`); never edit `CHANGELOG.md`.
- CSS/layout fixes are validated in a real browser (Playwright Wails mock + `elementFromPoint`), not by vitest.

## Approach

`SidebarHeaderControls` (`src/components/Sidebar.tsx:708`) already owns the header's React content and reads the store. Add one more portal there: `const total = useAppStore((s) => s.sessions.length)`, and when `total > 0` portal `[" ", <span className="brand-count" title="N session(s)">N</span>]` into the header's `.brand` span, after the "Hive" text node. The leading text space is deliberate: CSS margin does not enter the accessible name, so without it a screen reader hears "Hive3".

Inside `.brand`, not a header sibling: `.brand` carries `margin-right: auto`, so a sibling after it would be pushed right into the button cluster; moving the auto-margin to the count breaks the layout whenever the count is hidden (zero). Inside `.brand`, the count truncates together with the title and the button cluster keeps its position — the existing `sidebar-header-actions.spec.ts` layout invariants still hold unchanged.

Reading `sessions.length` (not a filtered list) counts every session, minimized ones and those of minimized projects included — same semantics as the per-project card count (`Sidebar.tsx:510`). The selector returns a number, so the header re-renders only when the total changes.

Screen readers read "Hive 12" (thanks to the text space) — same as the expanded project card's bare count; the `title` carries "12 sessions" for hover. No live region (would announce every create/close).

### Files to change

1. `cmd/hivegui/frontend/src/components/Sidebar.tsx` — in `SidebarHeaderControls`, select `sessions.length`, find `header.querySelector('.brand')`, portal the count span into it when `> 0`. Pluralized title ("1 session" / "N sessions").
2. `cmd/hivegui/frontend/src/theme/components/sidebar.css` — `#sidebar header .brand-count`: `margin-left: 2px` (plus the text space), `color: var(--fg-subtle)`, `font-family: var(--font-mono)`, `font-size: var(--text-xs)`, `font-weight: 400`, `letter-spacing: 0`, `font-variant-numeric: tabular-nums` (mirrors `.hv-project-card__count`, overriding the brand's accent colour / weight / letter-spacing).
3. `cmd/hivegui/frontend/test/e2e/chrome.spec.ts-snapshots/*.png` and `test/e2e/theme.spec.ts-snapshots/*.png` — regenerate darwin baselines that include the sidebar header (`HIVE_SNAPSHOT=1 CI=1 npx playwright test chrome theme --update-snapshots`); inspect that diffs are confined to the header.

4. `cmd/hivegui/frontend/src/theme/components/sidebar.css:13-16` — refresh the stale header comment (brand + three icon buttons; the brand now also holds the session total).
5. `docs/design-docs/ui/components.md` — one line under the sidebar-header / icon-button entry noting the brand's muted session total.

### New files

- `cmd/hivegui/frontend/test/dom/sidebar-session-total.test.tsx` — DOM tests (below).
- `.changesets/sidebar-session-total.md` — `type: added`, `bump: minor`, `issue: 434`, `pr:` backfilled once the PR exists.

### Tests

`test/dom/sidebar-session-total.test.tsx` (same fixture pattern as `check-updates-button.test.tsx`: header markup, fresh module registry, `render(<SidebarHeaderControls />)`):
- `shows the total number of sessions next to the brand` — seed 3 sessions across 2 projects; `.brand .brand-count` text is `3`, title `3 sessions`, and it is a child of `.brand` (not a header sibling).
- `counts minimized sessions and sessions of minimized projects` — seed 4 sessions over 2 projects, then minimize a session in the NON-minimized project and minimize the other project through the real store API (`minimizeSession(id)` and `minimizeProject(pid)`, `store.ts:929-950` — minimized state is a pair of Sets, not a SessionInfo field); count is still `4`; `localStorage.clear()` in `beforeEach` (`minimizeProject` persists).
- `updates live as sessions are added and removed` — `addSession` then `removeSession` via the store; count goes 2 → 3 → 2.
- `is hidden at zero sessions` — empty `sessions`: no `.brand-count` element; brand text is exactly `Hive`. Then add one: `1`, title `1 session` (singular).

`test/e2e/sidebar-header-actions.spec.ts`:
- New test `the session total sits inside the brand, right after Hive` — boot the mock (seeds 1 session), add a second via `window.__hive.addSession('s2')`, wait for `sessions.length >= 2`; assert `#sidebar header .brand > .brand-count` exists (child of `.brand`, not a header sibling), text `2`, title `2 sessions`; its box lies within the `.brand` box; `count.x - brand.x < 60` (right after the Hive glyphs, not pushed right); its right edge ≤ `#new-project-btn`'s left edge. Existing tests stay unchanged and must still pass (they are the regression guard for the cluster position).

### Verification

From `cmd/hivegui/frontend`:
- `npm run typecheck >/dev/null && echo OK`
- `npx biome ci . >/dev/null && echo OK`
- `npx vitest run test/dom/sidebar-session-total.test.tsx test/dom/check-updates-button.test.tsx test/dom/whats-new.test.tsx test/dom/whats-new-empty.test.tsx`
- `npm test` (full vitest)
- `CI=1 npx playwright test sidebar-header-actions chrome theme minimize-project`
- Baselines: regenerate with `HIVE_SNAPSHOT=1 CI=1 npx playwright test chrome theme --update-snapshots`, then inspect the PNG diffs by eye (`git diff --stat` + open changed PNGs) and confirm the only change is the header count. A post-update snapshot run is only a smoke check — it cannot fail.

### Open questions / risks

- Very narrow sidebar: count truncates with the brand via the existing ellipsis. Acceptable.
- Snapshot churn: every darwin baseline showing the sidebar changes; reviewer should eyeball the regenerated PNGs.
- Portal into `.brand` relies on the static markup in `index.html`; if `.brand` is missing the count simply does not render (same degrade as the other header portals).

## Second opinion

- **Round 1:** revise, confidence 8. Must-fix (all 3 applied): e2e must prove the count is inside `.brand` right after "Hive" (box containment + x offset), not a header sibling; minimized DOM test must use the real `minimizeSession`/`minimizeProject` Sets; drop the vacuous post-`--update-snapshots` run in favour of eyeballing diffs.
- **Round 2:** approve, confidence 8. Nits applied: minimize a session in the non-minimized project (so filtering on either Set fails the test), clear `localStorage` between tests, fix store cite. Noted: `title` is a hover tooltip only; screen readers hear "Hive 2".

## Decision log

- **2026-09-18** — Count every session in the store (minimized sessions and minimized projects included). Why: operator choice at clarifying round A; matches the per-project count's semantics.
- **2026-09-18** — Render as a muted number badge after "Hive", with `N sessions` as the hover tooltip (screen readers hear "Hive N" via a text space). Why: operator choice; reuses the project card count's look and costs little header width.
- **2026-09-18** — Hide the count at zero sessions. Why: operator choice; the empty state already explains itself.
- **2026-09-18** — Assumption: no alive/exited or attention breakdown; count only. Why: not asked for; the project cards already bubble attention.
- **2026-09-18** — Dropped the planned `docs/design-docs/ui/components.md` line. Why: no doc section describes the sidebar header; the only nearby entry is iconButton's, where a count note would be off-topic.
- **2026-09-18** — Committed only the 7 darwin baselines the feature changes (`theme` sidebar-* and chrome-classic). Why: regenerating with the feature disabled still rewrites 23 other baselines (dialog/launcher/worktrees/settings and chrome grid/launcher) — pre-existing drift unrelated to this change; absorbing it here would hide it.

## Progress

- **2026-09-18** — Spec and plan created; research done (fast lane).
- **2026-09-18** — Plan approved (chat, first pass).
- **2026-09-18** — Implemented: count portal in `SidebarHeaderControls`, `.brand-count` CSS, DOM + e2e tests, changeset.

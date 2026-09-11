# Sidebar: flatten worktree groups and reclaim side space

- **Spec:** [docs/product-specs/392-flatten-worktree-groups-and-reclaim-side-space.md](../../product-specs/392-flatten-worktree-groups-and-reclaim-side-space.md)
- **Issue:** #392
- **Status:** completed
- **PR:** #393
- **Branch:** `feature/392-flatten-worktree-groups`
- **Worktree:** `.worktrees/flat-group` on `feature/392-flatten-worktree-groups`
- **Design review:** [docs/design-docs/ui/mocks/sidebar-group-flatten-options.html](../../design-docs/ui/mocks/sidebar-group-flatten-options.html) — three group cues and two trim levels, measured from the live DOM.

## Summary

#390 made the project a flat, square, full-bleed label but left the worktree group as a rounded, bordered, 8px-inset card. The two treatments now sit directly on top of each other in the same tree, and the group's inset costs a grouped row 9px of left margin and 18px of window-title width against an ungrouped row. This plan squares the group off and runs it edge to edge. Nothing outside the group's own CSS block moves.

## Research

### Relevant code

- `cmd/hivegui/frontend/src/theme/components/sidebar.css:80-96` — `.hv-worktree-group` and `.hv-worktree-group__header`. The whole change lives here: `margin: var(--space-1) var(--space-2)`, `border: 1px solid var(--border)`, `border-radius: var(--radius-md)` on the panel; a matching `border-radius: var(--radius-md) var(--radius-md) 0 0` and `padding: var(--space-1) var(--space-2)` on the header.
- `cmd/hivegui/frontend/src/theme/components/sidebar.css:~150` — `.hv-worktree-group::after`, the shared colour bar, rounded `0 var(--radius-md) var(--radius-md) 0`.
- `cmd/hivegui/frontend/src/theme/components/project-card.css:6-35` — the flat label this has to stop clashing with: no border, no fill, no side margins, `padding: 0 var(--space-3)`, hairline rule beneath.
- `cmd/hivegui/frontend/src/theme/components/session-row.css:3-23` — the row's own `padding: 0 var(--space-3)`; this is the 12px the group header should align to once its side margin is gone.
- `cmd/hivegui/frontend/src/theme/components/sidebar.css:44-53` — `#sessions, #projects { padding: 4px var(--space-2) 4px 0 }`. The 8px right gutter clears `#sidebar-resizer`, and the group lives **inside** that padding box, so a full-bleed group's `right: 0` lands exactly where an ungrouped row's colour bar lands. The resizer stays cleared for free.
- `cmd/hivegui/frontend/src/components/WorktreeGroup.tsx` — markup, untouched. No class names change.

### Constraints / dependencies

- **`overflow: hidden` stays banned on the panel** (comment at `sidebar.css:82`): clipping makes the panel the nearest scroll container and its sticky header silently stops working. The current file works around the clip by rounding the header's own corners; with the radius gone that workaround becomes dead weight, not a thing to preserve.
- **The sticky header must keep an opaque background of its own.** Rows scroll *underneath* it inside the panel; a transparent header would let them show through. This is why the header keeps an explicit `background`, even though the panel now has the same ground.
- `--sidebar-project-header-h` (26px, declared on `#projects`) is the header's sticky offset. Header padding changes horizontally only — its height must not move, or the two headers overlap.
- Three density presets (`normal` / `tight` / `compact`, `theme/density.ts`) re-value row metrics. The trim decision (see Decision log) deliberately keeps this change out of row padding, so none of the three can be affected.

### Prior lessons

`brain-search` returned no hits for these terms. From the repo's own record, two rules bind here:

- CSS/layout changes are not verifiable by reading vitest output — `test/dom` has no CSS. The proof is a Playwright assertion against computed geometry.
- `scripts/ui-lint.sh` enforces the token rules; literal radii need a trailing `/* ui-lint: allow */` and a stated reason. This change removes radii rather than adding any.

### Conventions card

- **Build:** `./build.sh` (do not rebuild by hand as part of ordinary work); GUI dev server `wails dev` → `http://localhost:34115`.
- **Lint:** `scripts/ui-lint.sh` (token/icon rules, CI-enforced); `biome ci .` from `cmd/hivegui/frontend` — `ci`, not `lint`, because only `ci` checks formatting.
- **Test:** `scripts/test.sh [go|unit|dom|e2e]`. Playwright e2e runs against the Wails **mock** bridge; run it locally with `CI=1` or it reuses a stale vite dev server and a green run means nothing.
- **TDD is mandatory** — no behaviour change ships without the test that would have caught the regression.
- Frontend tests live under `cmd/hivegui/frontend/test/`; visual rules live in `docs/design-docs/ui/` and must be updated in the same PR as the CSS they describe.
- User-visible change ⇒ a `.changesets/*.md` entry (`/hs-changelog-update`).

## Approach

**Square the panel and let it run edge to edge; keep every other axis exactly as it is.**

The group stops being a card and becomes a *band*: two hairline rules top and bottom, a full-width header with its own ground, and the body on the panel's raised ground. That is the same vocabulary the project label already uses (a rule plus a header band), so the tree reads as one system, and it keeps a containment cue stronger than a bare header — which is what ruled out the "header only" variant.

Rejected, with the mock as the record:

- **Left rail + flat header.** Reads as a tree and reclaims the right edge, but keeps an 8px indent — so it only half-solves the complaint — and it moves the group's colour from the right bar to the rail, which contradicts patterns.md › *the right edge is the shared-worktree position*.
- **Header only, no indent.** Reclaims the most width, but where the group *ends* rests on a single hairline, and it drops the "one box in the tree" rule entirely rather than restating it.
- **Also tighten every row** (row padding 12→8px, gutter 8→4px). Measured at +12px more title per row, but it moves every ungrouped row and the project label, needs a pass against all three density presets, and narrows the gutter that keeps the colour-bar hit target clear of the resizer. Out of scope by the operator's decision; recorded in the spec's Non-goals.

The exact edit, all inside `sidebar.css`:

| Property | Now | After |
|---|---|---|
| `.hv-worktree-group` `margin` | `var(--space-1) var(--space-2)` | `var(--space-1) 0` |
| `.hv-worktree-group` `border` | `1px solid var(--border)` (all four) | `border-top` + `border-bottom` only |
| `.hv-worktree-group` `border-radius` | `var(--radius-md)` | *removed* |
| `.hv-worktree-group` `background` | `var(--surface)` | `var(--surface-raised)` |
| `__header` `padding` | `var(--space-1) var(--space-2)` | `var(--space-1) var(--space-3)` |
| `__header` `border-radius` | `var(--radius-md) var(--radius-md) 0 0` | *removed* |
| `__header` `background` | `var(--surface-raised)` | unchanged (stays opaque — sticky) |
| `::after` `border-radius` | `0 var(--radius-md) var(--radius-md) 0` | *removed* |

The header's 8px → 12px padding is what puts the branch name in the same column as the row names below it, now that the 8px margin no longer supplied that offset. Header height is unchanged (vertical padding untouched).

Measured on the mock at the real 260px width: grouped content x `21px → 12px` (identical to an ungrouped row), window title `188px → 206px`.

### Files to change

1. `cmd/hivegui/frontend/src/theme/components/sidebar.css` — the table above, plus the comments. The "NOT `overflow: hidden` … the header rounds its own corners instead" note loses its second half. The block comment at `sidebar.css:71-73` ("The one box in the sidebar tree, so boxing means one thing") is falsified by this change and is restated: the group is the one *container* in the tree, now a full-bleed band rather than a card. The `.hv-worktree-group` block gains a line on why it is full-bleed (it aligns with the rows and with the flat project label) and why the header keeps its own opaque background (it is sticky, rows scroll under it).
2. `cmd/hivegui/frontend/src/theme/components/project-card.css:3-5` — the same "the worktree group panel (sidebar.css) is the only box in the tree" claim, restated the same way. Comment only; no declarations change.
3. `docs/design-docs/ui/components.md:55,57` — `worktreeGroup`: "The only BOX in the sidebar tree: 1px `--border`, `--radius-md`, `--surface`" becomes the band description (full-bleed, top/bottom hairlines, `--surface-raised`, no radius); the `overflow: hidden` bullet drops "The header rounds its own corners instead"; the header bullet gains the `--space-3` alignment with the row column.
4. `docs/design-docs/ui/components.md:63` — `projectCard`'s "The worktree group panel is the only box in the tree", the fourth copy of the invalidated rule.
5. `docs/design-docs/ui/patterns.md` — check only. The shared-worktree bullet says "the group's panel" and the right-edge rule still holds; edit only if the wording implies rounding.
6. `cmd/hivegui/frontend/test/e2e/sidebar-sticky.spec.ts` — `seedScrollableGroup()` (line 22) moves out to the shared fixture below and is imported here. Assertions unchanged; this is the anti-drift half of reusing it.
7. `.changesets/sidebar-flat-worktree-group.md` — new, user-visible.

### New files

- `cmd/hivegui/frontend/test/e2e/fixtures/seed-worktree-group.ts` — `seedScrollableGroup()` extracted verbatim from `sidebar-sticky.spec.ts:22`. It lands next to `fixtures/xterm-reflow.ts`, which is a *browser-side* module the page loads — a different genre in the same directory — so it opens with a one-line header saying it is a Playwright-side seeder. `boot()` stays behind in `sidebar-sticky.spec.ts`, which still uses it. Extracted rather than copied: two copies of a 25-line seeding helper drift, and the drift is silent — a copy that stops producing a scrollable list turns every sticky assertion vacuous.
- `cmd/hivegui/frontend/test/e2e/sidebar-group-flatten.spec.ts` — the geometry proof. jsdom has no CSS, so this is the only place the change can be verified.
- `docs/design-docs/ui/mocks/sidebar-group-flatten-options.html` — already written; kept in the repo as the record of the decision, rejected options included, matching how #390 kept its mocks.

### Tests

`cmd/hivegui/frontend/test/e2e/sidebar-group-flatten.spec.ts`, on the default preset (`terminal` zeroes `--radius-md`, which would make the radius assertions pass for the wrong reason), seeded from the shared `seedScrollableGroup()`:

Every measurement goes through one helper that **throws on a null bounding box or a missing element**, so a selector that matches nothing fails loudly instead of reading as a pass. The ungrouped-row selector is `.hv-project-card__rows > li.hv-session-row` — the real DOM is `li.hv-project-card > div.hv-project-card__body > ul.hv-project-card__rows` (`ProjectCard.tsx:211-212`), so the child chain has to start at `__rows`; grouped rows are `.hv-worktree-group__rows > li.hv-session-row`.

- `test('a grouped row starts at the same x as an ungrouped one')` — bounding boxes of `.hv-session-row__state` in each; asserts `|Δleft| <= 1`. **Fails today at Δ = 9.**
- `test('a grouped row gives up no width on the right')` — right edges of a grouped and an ungrouped `.hv-session-row__sub`; asserts `|Δright| <= 1`. **Fails today at Δ = 9.**
- `test('the panel has no rounded corners')` — `getComputedStyle` `borderRadius` is `0px` on `.hv-worktree-group`, on `.hv-worktree-group__header`, and on the `::after` colour bar (`getComputedStyle(panel, '::after')`, which the table also changes). **Fails today at 6px** on all three.
- `test('the sticky group header paints its own ground')` — parses `getComputedStyle(header).backgroundColor` and asserts **alpha === 1**. Two weaker drafts were rejected: `elementFromPoint` is vacuous (it returns the sticky header whether or not it has a background, so it passes on exactly the regression it claims to catch), and `!== 'rgba(0, 0, 0, 0)'` still lets `rgba(17, 17, 17, 0.6)` through — a header rows visibly show through. `background-color` does not inherit, so a dropped declaration computes to alpha 0 and this goes red. Comparing against the panel's own background would be a tautology (the table puts both on `--surface-raised`) and would break on a future ground change that harms nothing.

Untouched and must stay green (the regression surface for everything the redesign already settled): `test/e2e/sidebar-sticky.spec.ts` (import changed, assertions not), `test/e2e/sidebar-collapse-animation.spec.ts`, `test/e2e/shared-worktree-cue.spec.ts`, `test/e2e/sidebar-density.spec.ts`, `test/dom/sidebar-group.test.tsx`, `test/dom/sidebar-reorder.test.tsx`.

## Verification

Run from `.worktrees/flat-group`:

```sh
./scripts/ci-bootstrap.sh                       # fresh worktree: generates the wailsjs bindings
scripts/ui-lint.sh && echo UI-LINT-OK           # token rules; exit code, not grep
(cd cmd/hivegui/frontend && npx biome ci . >/dev/null && echo BIOME-OK)
CI=1 scripts/test.sh dom e2e                    # CI=1 or a stale vite dev server makes green meaningless
```

The e2e run is the real gate: every new assertion except the background one fails against the current CSS and passes after it, so a green run cannot be vacuous.

**Real-browser visual pass (spec criterion 4).** The geometry tests prove the numbers; they cannot say whether the grouping still *reads*. A throwaway Playwright script screenshots the seeded sidebar — group expanded, group collapsed, two adjacent groups — and those go next to `docs/design-docs/ui/mocks/sidebar-group-flatten-options.html` option C for comparison. It runs against the **mock harness** (`VITE_WAILS_MOCK=1`, `localhost:5174`), not `wails dev`: the seeding hooks the script needs (`window.__hive.addSession`, `createSessionWithWorktree`) exist only there. Per AGENTS.md this needs no human.

`terminal` is the pass/fail case, not one of three equals: it zeroes `--radius-md` (so the old design already looked flat there) and its ground shift is `--surface: #0a0a0a` against `--surface-raised: #0f0f0f` (`themes.css:140`) — five values. Two hairlines plus a five-value shift is the weakest the band cue ever gets. If the grouping reads in `terminal` it reads everywhere; `hive-dark` and `hive-light` are the confirmations. If it does not, the change does not ship as drawn.

`scripts/test.sh go` is unaffected (no Go touched) but runs in CI regardless.

## Second opinion

**Round 1 — `revise`, confidence 8.** Approach and scope approved; four must-fix items, all applied:

1. *The sticky-header test was vacuous.* `elementFromPoint` returns the sticky header whether or not it has a background, so the test passed on exactly the regression it claimed to guard. Replaced with a computed `backgroundColor` assertion.
2. *The ungrouped-row selector matched nothing.* `#projects > li > ul > .hv-session-row` skips `div.hv-project-card__body` (`ProjectCard.tsx:211-212`); a null measurement would have read as a pass. Replaced with `.hv-project-card__rows > li.hv-session-row`, and every measurement now throws on a missing element.
3. *Blast radius missed two more copies of the "only box in the tree" rule* this change falsifies — `project-card.css:3-5` and `components.md:63` — plus the block comment at `sidebar.css:71-73`. All three added to Files to change.
4. *Spec criterion 4 (real-browser check) had no step.* Added as an explicit verification pass across `hive-dark`, `hive-light` and `terminal`.

Nice-to-haves also applied: the `::after` radius assertion, the default-preset note, the header/body-ground decision, and extracting `seedScrollableGroup()` to a shared fixture rather than copying it.

**Round 2 — `approve`, confidence 8.** No must-fix items. The reviewer verified each fix against the tree rather than the plan's prose: both selectors match the real DOM and the drop placeholder (which carries neither class by design, `drag-placeholder.ts:8-12`) cannot be miscounted; the extraction closes over nothing but its `page` argument and the ambient `window.__hive` typing, and Playwright's `testMatch` will not pick the fixture up as a spec; exactly four live sites assert the invalidated rule and all four are listed (the fifth hit, in the options mock, is the historical record of a rejected option and is correctly left alone).

All four nice-to-haves applied: the background assertion now checks **alpha === 1** rather than the tautological comparison, the visual pass is pinned to the mock harness rather than `wails dev`, `terminal` is named as the pass/fail case rather than one of three equals, and the new fixture gets a header comment marking it Playwright-side.

## Verification results (2026-09-11)

- `scripts/ui-lint.sh` — OK. `biome ci .` — OK. `CI=1 scripts/test.sh dom` — 734 passed. `CI=1 scripts/test.sh e2e` — 321 passed, 31 skipped.
- **Non-vacuity proven, not asserted.** With `sidebar.css` reverted to `HEAD`, three of the four new tests fail (left Δ, right Δ, radius) and the background one passes. With the new CSS but the header's `background` declaration deleted, only the background test fails. Each assertion fails for its own reason and nothing else's.
- **Visual pass — all three presets read as a group.** Screenshots of the seeded sidebar (group expanded, group collapsed, a second group directly below) in `terminal`, `hive-dark` and `hive-light`. The band is carried by the header's own ground plus the closing hairline; the body's ground shift against `--surface` is the weakest of the three channels in every preset and effectively invisible in `terminal`, as predicted. It still reads, because the header band and the closing rule do not depend on it. If the sidebar ever grows a second full-width header band that is not a group, this is the cue to revisit.

## Gate verdict

- **2026-09-11** — verdict: NEEDS_FOLLOWUP; phase: —; checks: 2 dimensions passed / 0 failed / 1 followup; followups: pending operator decision; one-line: everything #392 owns passes; the one finding is #385's unfinished pipeline bookkeeping, inherited not introduced.
  - 2026-09-11 dimensions:
    - acceptance — PASS — all five success criteria independently re-verified, not taken from the plan's prose: ui-lint exit 0, 734 dom, 23 sidebar e2e, 16 ordering e2e, 19 sidebar dom; the validator drove its own Playwright script against the mock harness and took its own screenshots rather than trusting the implementation's visual pass. Noted, not a regression: no spec covers dragging *within* a group, and this diff does not touch the grouping logic that would make one necessary.
    - non-goals — PASS — zero diff in `theme/density.ts`, `session-row.css`'s density block, `lib/worktree-groups.ts` and `internal/registry`; `project-card.css` is comment-only.
    - doc accuracy — NEEDS_FOLLOWUP — changeset, `components.md` and `patterns.md` all match what shipped, and the options mock is correctly excluded as a historical record. The repo-wide sweep found one stale doc, **pre-existing**: `docs/exec-plans/active/385-sidebar-redesign.md:200-201` still documents `.hv-worktree-group { border-radius: var(--radius-md) }` as target CSS, and that plan is still `Status: active` / spec stage `REVIEW` despite having shipped as #390. #392 did not introduce it; #390's own gate bookkeeping was never completed.

- **2026-09-11 (re-run)** — verdict: PASS; phase: —; checks: 3 dimensions passed / 0 failed / 0 followups; followups: none; one-line: the doc-accuracy followup is closed — #385's bookkeeping was completed in this PR at the operator's direction, and its stale CSS snippet is now marked superseded.
  - 2026-09-11 dimensions:
    - acceptance — PASS — unchanged from the first run; no code moved between the two.
    - non-goals — PASS — unchanged.
    - doc accuracy — PASS — `385-sidebar-redesign.md` moved to `completed/` with `Status: completed`, `PR: #390`, `Shipped: 2026-09-11`; its spec advanced to `stage: DONE` with `pr`/`shipped` set and its `Exec plan:` link repointed; the stale `border-radius` snippet is kept as the historical record of what #390 built and explicitly marked superseded by #392, pointing at the live CSS. Its `## Gate verdict` records honestly that it shipped without a gate rather than back-dating a PASS that never ran.

## PR convergence ledger

- **2026-09-11 iter 1** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 780270d.

One MINOR, not actioned and recorded here instead: grouped-row body text moves from `--surface` to `--surface-raised`, and `--fg-subtle` on that ground falls below 3:1 in `dracula` (2.73) and `catppuccin-mocha` (2.94). Both presets are `--contrast-exempt`, so `ui-lint.sh --contrast` is green and no enforced preset regresses. The transferable part is the gap, not this PR: the contrast table pairs `--fg*` against `--surface` and `--state-*` against `--surface-raised`, so body text on `--surface-raised` is a combination it never checks. Worth a table entry the next time that script is touched.

## Decision log

- **2026-09-11** — Group cue: square full-bleed box, over "left rail + flat header" and "header only". Why: operator's pick from the mock; it keeps containment as strong as today, drops the rounding that clashes with the flat project label, and reclaims both edges.
- **2026-09-11** — Side trim stops at the group's own inset; row padding and the list gutter are not touched. Why: operator's decision. The extra 12px is not worth moving every row, re-checking all three density presets, and narrowing the resizer clearance on the colour-bar hit target.
- **2026-09-11** — Panel ground moves to `--surface-raised` and the header keeps its own opaque background rather than inheriting. Why: the header is sticky and rows scroll beneath it — a transparent sticky header shows the rows through. The mock did not expose this because its frames do not scroll; a test now covers it.
- **2026-09-11** — Panel and header both sit on `--surface-raised`, so the header/body split rests on the header's `border-bottom` alone. Why: deliberate — the band's containment comes from the two outer hairlines and the ground shift against `--surface`, not from a third internal contrast step. Two grounds inside a 3-row panel is noise at this size.
- **2026-09-11** — Two adjacent groups will show two hairlines `2 * var(--space-1)` apart (8px). Accepted as-is: that gap is the same rhythm ungrouped rows get, and collapsing it would need a `+` sibling rule for a case (two worktrees, one project, adjacent) that is not the common one. Revisit if it reads as a double rule in the visual pass.
- **2026-09-11** — All work happens in the `.worktrees/flat-group` worktree, not the main checkout. Why: operator's instruction.

## Progress

- **2026-09-11** — Spec + mock written, issue #392 filed, worktree created, plan drafted.
- **2026-09-11** — Gate PASS on re-run after closing the followup in-PR (operator's call). Spec advanced to DONE.
- **2026-09-11** — Gate NEEDS_FOLLOWUP; sole item is #385's unfinished bookkeeping (plan still in `active/`, spec still `REVIEW`, plan text still documents the pre-#392 CSS). Nothing #392 owns is outstanding. CI green on all 14 checks.
- **2026-09-11** — Plan approved after two second-opinion rounds. Implemented, all checks green, visual pass done. PR #393 opened; stage REVIEW.

## Open questions

None.

# Reorganize Settings into tabbed sections

- **Spec:** [docs/product-specs/335-reorganize-settings-into-tabbed-sections.md](../../product-specs/335-reorganize-settings-into-tabbed-sections.md)
- **Issue:** —
- **Branch:** `feature/335-reorganize-settings-into-tabbed-sections`
- **PR:** [#335](https://github.com/lucascaro/hive/pull/335)
- **Status:** completed

All paths relative to `cmd/hivegui/frontend/` unless rooted.

## Summary

Split the Settings modal body into three tabs — Agents (default), Appearance,
Updates — rendered by a new `Tabs` primitive. All three panels stay mounted and
the inactive ones are hidden with `display: none`. The shared error slot moves
out of the agents section so errors raised from Updates are still visible, and
the `#settings-scroll` / `#settings-updates` pinning hack is deleted because the
tab makes the invariant it protected unreachable.

## Research

Authored via plan-first mode (`/hs-feature-loop plan`). Code read during
plan-mode iteration:

- `src/components/modals/Settings.tsx` (725 lines) — the whole dialog. Body is
  `#settings-scroll` (agents + appearance, scrolling) followed by
  `#settings-updates` (pinned). Per-open state resets by construction: the body
  mounts only while open, keyed on `entry.seq`.
- `src/theme/components/settings.css:24-40` — the pinning rules
  (`#settings-panel .hv-dialog__body` flex column, `#settings-scroll` flex
  `1 1 auto` + `overflow-y:auto`, `#settings-updates` flex `0 0 auto`). Their
  comment names the e2e test they exist for.
- `src/components/modals/ModalShell.tsx` — owns Escape, the backdrop
  mousedown/click pair and `trapFocus`. Body children are rendered verbatim, so
  the tab strip is Settings' own markup, not the shell's.
- `src/components/modals/Settings.tsx:435-450` — the root `keydown` listener
  that makes Enter save. It already excludes `HTMLButtonElement`, so tab buttons
  keep their own activation with no change.
- `src/app/keyboard.ts:201-215` — the `isModalOpen('settings')` branch handles
  only Escape, ⌘,, and `trapFocus`; arrow keys fall through to the focused
  element, so the strip's Left/Right needs no keymap work.
- `src/theme/components/dialog.css:13-26` — `.hv-dialog__panel` is
  `max-height: 80vh`, `data-size` caps width (`md` = 560px). The panel grows to
  content below that cap, which is why a short tab needs a `min-height` floor.
- `src/components/Button.tsx` — the primitive shape the new `Tabs` follows
  (`hv-*` classes, `data-*` variants, explicit `id`).
- `test/dom/settings.test.tsx` and `test/dom/settings-updates.test.tsx` — both
  resolve controls with `document.getElementById`, so keeping panels mounted
  leaves them passing unedited.
- `test/dom/settings-updates.test.tsx:183` — asserts a source-repo failure lands
  in `#settings-error`. That slot currently lives inside the agents section; with
  tabs it would render inside a hidden panel. This is the constraint that forces
  the slot to move.
- `test/e2e/settings.spec.ts` — pins `#settings` `hidden` as the open signal, the
  `.settings-agent-*` selectors, Tab containment, and two layout invariants:
  "the Updates section stays reachable under a long agent list" (tests the pin,
  must be rewritten) and "the agent list is on screen the moment Settings opens"
  (Agents is the default tab, so it survives unchanged).
- `docs/design-docs/ui/README.md` — rule 1 (add a primitive, never hand-roll
  markup in a feature component), rule 2 (tokens only), rule 5 (record the
  decision), rule 6 (`hv-*` names are the Playwright contract).

## Approach

Tabs — **Agents** (default) / **Appearance** / **Menu bar**, macOS-only /
**Updates** — with the strip rendered by a new `Tabs` primitive inside the
dialog body, above the panels.

**Menu bar is its own tab.** `main` gained a macOS-only Menu bar section (PR
#333) inside the `#settings-scroll` div this branch deletes, so the rebase had
to place it somewhere. It is neither appearance nor updates — it is whether
launchd starts a background agent at login — so it gets a fourth tab, built
into the strip behind the same `isMac && status !== 'unsupported'` guard the
section carried. The tab is absent, not disabled, everywhere that guard fails,
so no platform sees a tab that opens onto an explanation of why it is empty.

**A primitive, not inline markup.** `docs/design-docs/ui/README.md` rule 1
forbids hand-rolled markup in a feature component. The strip is ~50 lines and
carries the ARIA pattern (roving tabindex, Left/Right/Home/End), which is
exactly what should not be re-rolled per surface.

**Panels stay mounted, inactive ones hidden.** The obvious alternative —
unmounting inactive panels — was rejected: the `update:progress` subscription
and the `SourceRepoStatusFor` debounce live in effects that would re-fire on
every tab switch, and `test/dom/settings-updates.test.tsx` would break. Keeping
them mounted also preserves the agent draft, the theme state and the overrides
debounce for free, and `display: none` removes hidden fields from the Tab order,
so `lib/focus-trap.ts` needs no change.

**The error slot moves.** `#settings-error` is the slot for *every* error in the
dialog, including `SaveUpdateSettings` rejections and `PickDirectory` failures
raised from the Updates section. Inside the agents panel it would render
invisibly whenever another tab is active, so it moves out of the panels into a
shared slot between the panels and the footer. Its id, classes and `role="alert"`
are unchanged, so the e2e colour assertion still holds. The `scrollIntoView`
`useLayoutEffect` goes away with it — the slot is no longer inside a scrolling
region.

**The pin is deleted.** `#settings-scroll` / `#settings-updates` existed so a
long agent list could not push the channel picker off screen. Updates is now its
own tab, so no quantity of agents can reach it. The e2e test for that invariant
is rewritten to assert it through the tab rather than deleted.

### Files to change

- `src/components/modals/Settings.tsx` — add `tab` state
  (`'agents' | 'appearance' | 'updates'`, default `'agents'`); render `<Tabs>` at
  the top of the body; wrap each section in
  `<section className="settings-panel[ hidden]" role="tabpanel" id="settings-panel-<tab>">`;
  move `#settings-error` out of the agents section into a shared slot after the
  panels; drop the `scrollIntoView` `useLayoutEffect` and the stale comment about
  `#settings-scroll` / `#settings-updates` having to be direct children of the
  body.
- `src/theme/components/settings.css` — delete the `#settings-scroll` /
  `#settings-updates` pinning rules; add `.settings-panel`
  (`flex: 1 1 auto; min-height: 0; overflow-y: auto`),
  `.settings-panel.hidden { display: none; }`, and a `min-height` floor on the
  panel region so switching to a short tab does not collapse the dialog.
- `src/theme/components/index.css` — import `tabs.css`.
- `test/e2e/settings.spec.ts` — rewrite "the Updates section stays reachable
  under a long agent list"; add the tab specs below.
- `test/dom/settings.test.tsx` — add the tab-switching cases below.
- `test/dom/settings-updates.test.tsx` — no source change expected; verify the
  `#settings-error` case at line 183 still passes with the slot outside the
  panels.
- `docs/design-docs/ui/components.md` — document the `Tabs` primitive (anatomy,
  states, tokens, ARIA contract, the `<id>-tab-<tabId>` selector contract).
- `docs/design-docs/ui/README.md` — decision row for the tabbed Settings layout.
  No `mocks/` page (operator's call at plan time).
- `.changesets/<slug>.md` — user-visible change; never edit `CHANGELOG.md`.

### New files

- `src/components/Tabs.tsx` — the primitive.
  `{ id, tabs: {id,label}[], active, onChange }`. Renders `role="tablist"` with
  one `role="tab"` button per entry, `aria-selected`, `aria-controls`, roving
  `tabindex` (`0` on active, `-1` elsewhere) and Left/Right/Home/End key handling
  that moves focus and selection together. Button ids are `<id>-tab-<tabId>` —
  the selector contract for the Playwright specs.
- `src/theme/components/tabs.css` — `.hv-tabs` strip and `.hv-tab` button.
  Tokens only (`--border`, `--accent`, `--fg-muted`, `--fg`, `--space-*`,
  `--text-*`), bottom border on the strip, accent underline on the selected tab,
  `:focus-visible` ring matching the existing rules. No hex, no px font sizes —
  `scripts/ui-lint.sh --strict` gates it.

### Tests

`test/dom/settings.test.tsx`:

- `opens on the Agents tab` — `#settings-tab-agents` has `aria-selected="true"`;
  the appearance and updates panels carry `hidden`.
- `clicking a tab swaps the visible panel` — click `#settings-tab-updates`; the
  updates panel loses `hidden`, agents gains it, `aria-selected` follows.
- `arrow keys move between tabs and wrap` — Left from Agents lands on Updates,
  Right wraps back; the focused element is the newly selected tab.
- `an in-progress agent draft survives a tab round-trip` — type a row, switch to
  Appearance and back, name and cmd are intact.
- `Enter still saves from a field on any tab` — the root Enter handler excludes
  buttons, so activating a tab does not save.

`test/dom/settings-updates.test.tsx`:

- `a source-repo error is visible from the Updates tab` — the existing line-183
  case re-asserted with Updates active: `#settings-error` is not inside a hidden
  panel. This is the regression the slot move exists for.

`test/e2e/settings.spec.ts`:

- `the agent list is on screen the moment Settings opens` — kept as is; Agents is
  the default tab so the existing `elementFromPoint` assertion still means what
  it meant.
- `the Updates tab shows the channel picker without scrolling` — replaces "stays
  reachable under a long agent list". Add 12 agents, click the Updates tab,
  `elementFromPoint` at the channel select's centre hits the select, and
  `#settings-panel` does not exceed the window.
- `Tab containment still holds with a tab strip` — extends the existing
  focus-trap case: 12 `Tab` presses from the strip stay inside `#settings` and
  never reach a control in a hidden panel.
- `switching tabs does not lose an unsaved draft across save` — type an agent,
  visit Appearance and Updates, return, Save, reopen: the agent is there. Covers
  the real risk of the mounted-panels choice.

Commands (from `AGENTS.md`):

```bash
scripts/test.sh unit dom e2e
scripts/ui-lint.sh --strict
scripts/ui-lint.sh --contrast
npx biome ci .            # ci, not lint — lint does not check formatting
npm run typecheck         # needs ./scripts/ci-bootstrap.sh in a fresh worktree
```

## Decision log

- **2026-09-03** — `Tabs` is a new primitive under `src/components/`, not inline
  markup in `Settings.tsx`. Why: `docs/design-docs/ui/README.md` rule 1, and the
  ARIA roving-tabindex logic should exist once.
- **2026-09-03** — Inactive panels stay mounted and are hidden with
  `display: none`. Why: unmounting re-fires the `update:progress` and
  source-repo effects on every switch, discards nothing useful, and breaks
  `test/dom/settings-updates.test.tsx`; `display: none` also keeps hidden fields
  out of the Tab order for free.
- **2026-09-03** — `#settings-error` moves out of the agents section into a
  shared slot below the panels. Why: it is the slot for Updates-section errors
  too (`test/dom/settings-updates.test.tsx:183`), which would otherwise render
  inside a hidden panel.
- **2026-09-03** — No `mocks/` page despite `docs/design-docs/ui/README.md`
  rule 5. Why: operator's call — there is one sane tab-strip layout here and the
  component is the better artifact. The decision row in the README is still
  added.
- **2026-09-03** — Local spec, no GitHub issue (operator chose "Skip GitHub" at
  Gate 1P). Number 335 allocated above the highest number GitHub has issued
  (PR #334) rather than max-file-prefix+1 (330), because 330–334 are live
  GitHub issue/PR numbers and reusing one would be ambiguous.
- **2026-09-03** — Dropped the per-section `<h4>` headings. Why: the selected
  tab is the section heading, and each panel is `aria-labelledby` its tab, so
  "Updates" was rendering twice, six pixels apart. The e2e open-signal
  assertion moved from the "Custom agents" heading to the tab strip.
- **2026-09-03** — No `min-height` floor on the panel region. Why: it was in
  the plan to stop the dialog resizing between a long Agents list and a short
  Appearance tab, but the dialog already varies with content, a fixed floor can
  clip inside `max-height: 80vh` on a short window, and the browser screenshots
  showed the switch reads fine without it.

- **2026-09-03** — Menu bar gets its own macOS-only tab rather than riding on
  Appearance. Why: review iteration 1 surfaced the conflict with `main`'s new
  `#settings-menubar` section; a launchd login item is not appearance, and
  Appearance would become a junk drawer. Absent rather than disabled off macOS,
  matching the guard the section already had.
- **2026-09-03** — `Tabs` is generic over the caller's id union. Why: the review
  flagged the `as TabId` cast at the call site; a generic removes it, and an
  unchecked cast on a value coming back from a child is exactly what breaks
  silently when a tab id is added.
- **2026-09-03** — Restored the `activeTab` fallback that re-selects Agents when
  `tab` names a tab missing from the strip. It was dropped earlier on the wrong
  premise ("the status is read once and never re-read"); review iteration 2
  caught that `toggleMenuBarLoginItem` re-reads it after every toggle, and that
  `"unsupported"` is Go's `default:` branch in `loginitem_darwin.go`, not just
  macOS ≤ 12. So the tab can leave the strip while selected, and the failure is
  a strip with nothing selected, no visible panel and dead arrow keys. `Tabs`
  also no longer goes inert on an `active` outside its list.

## Progress

- **2026-09-03** — Plan-first scaffold; stage = IMPLEMENT (set in spec
  frontmatter).
- **2026-09-03** — Implemented on `feature/335-reorganize-settings-into-tabbed-sections`.
  `Tabs` primitive + `tabs.css`, Settings split into three panels, error slot
  moved out, pinning rules deleted, docs and changeset written. dom (567) and
  e2e (265) green, `ui-lint --strict` / `--contrast` clean, `biome ci` and
  `tsc --noEmit` clean, `npm run build` succeeds. No Go files touched, so the
  `go` layer was not re-run.
- **2026-09-03** — Review-loop iteration 1: REQUEST_CHANGES, escalated on a
  merge conflict needing a human IA call. Rebased onto `origin/main` (PR #333
  landed mid-flight), resolved by giving Menu bar its own macOS-only tab, fixed
  the stale comment citing the deleted pin, typed `Tabs` generically, added four
  dom cases and one e2e spec for the new tab, and regenerated the seven
  invalidated screenshot baselines on macOS (eyeballed per themes.md step 4).
  All checks re-run green: dom 566 / e2e 266, ui-lint, contrast, biome, tsc,
  build.

## Gate verdict

- **2026-09-03** — verdict: PASS; checks: 9 acceptance / 6 non-goals / 8 doc — 0 failed, 0 followups; followups: none; one-line: tabbed Settings delivers every success criterion, stays inside its non-goals, and the docs match what shipped.
  - 2026-09-03 dimensions:
    - acceptance — PASS — all nine criteria verified against running code. First pass returned NEEDS_FOLLOWUP: the criterion "editing state survives tab switches" had a shipped test for the agent draft only, leaving the theme select and token textarea uncovered. Closed on this branch in d1ca455 and re-checked; the new test is non-vacuous (unmounting inactive panels turns 9 of 38 cases red).
    - non-goals — PASS — no tab persistence, `keymap.ts` and README untouched (0-line diff), dialog still `md`, no palette deep link, and main's menu-bar logic relocated byte-identical apart from the JSX wrapper.
    - doc accuracy — PASS — changeset valid and accurate, `Tabs` contract in components.md matches Tabs.tsx verbatim, decision row present, and a repo-wide sweep found no current-tense doc still describing the pre-tabs layout.
  - Note: this worktree's local `main` ref is stale (a6dec3e, missing PR #333) because `main` is checked out in the primary clone; every dimension used `origin/main...HEAD` as the range.

## PR convergence ledger

Append-only. One line per `/hs-review-loop` iteration.

- **2026-09-03 iter 1** — verdict: REQUEST_CHANGES; mergeable: CONFLICTING; findings_hash: 511f85f4; threads_open: 0; action: escalated:risky fix needs human decision (main gained a #settings-menubar section inside the deleted #settings-scroll; which tab owns it is an IA call); head_sha: 9d64f72.
- **2026-09-03 iter 1 (resolution)** — rebased onto origin/main; Menu bar placed in its own macOS-only tab; findings 2 and 3 fixed; baselines regenerated and reviewed.
- **2026-09-03 iter 2** — verdict: COMMENT; mergeable: MERGEABLE; findings_hash: 21471a1d; threads_open: 0; action: stop (strict off, no threads); head_sha: 7c64152.
- **2026-09-03 iter 2 (follow-up)** — the one IMPORTANT was a false invariant in a comment this branch added; corrected, the clamp restored, and covered by a dom test verified to fail without it.
- **2026-09-03 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 7df8e8f. Run because iter 2's convergence predated the follow-up commit; re-verified the clamp, the Tabs index fallback and the corrected comment. One MINOR (an inaccurate comment in Tabs.tsx) fixed after.

## Open questions

None blocking. Two settled at plan time: the tab strip lives inside the dialog
body above the panels (not in the 44px header), and the dialog stays `md`
(560px) — widening to `lg` is a separate call.

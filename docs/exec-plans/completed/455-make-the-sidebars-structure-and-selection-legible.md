# Make the sidebar's structure and selection legible

- **Spec:** [docs/product-specs/455-make-the-sidebars-structure-and-selection-legible.md](../../product-specs/455-make-the-sidebars-structure-and-selection-legible.md)
- **Issue:** #455
- **Status:** completed
- **PR:** #456
- **Branch:** feature/455-sidebar-legibility

## Summary

A CSS-only restyle of the GUI sidebar that makes four things easier to see. Worktree groups become an indented rail. The selected row gets an accent fill that still shows attention. The session list loses its side gutters, and the resize handle moves so the colour picker stays clickable. Project headers get a top rule in the project colour. The design was chosen in a two-round interactive tuner; the chosen values are in the spec's Notes. The tuner page itself was dropped from the PR after CodeQL flagged it (decision log).

## Research

All `src/` and `test/` paths are under `cmd/hivegui/frontend/`.

### Relevant code

- `src/theme/components/sidebar.css`
  - `:1-10`: `#sidebar` has `position: relative; overflow-x: hidden`. The overflow clip hides any part of the resizer that extends past the sidebar's edge.
  - `:52`: `--sidebar-project-header-h: 26px` sets the group header's sticky offset.
  - `:54-64`: `#sessions, #projects { padding: 4px var(--space-2) 4px 0 }`. The 8px right gutter exists only to clear the resizer (the comment at `:57-61`).
  - `:85-260`: the worktree group:
    - the band: a `--surface-raised` ground with top and bottom hairlines;
    - the sticky header, which must stay opaque (`:115`);
    - the 3px right-edge `::after` bar at `:246-253`;
    - per-row bars that are transparent inside a group (`:255-260`).
- `src/theme/components/session-row.css`
  - `:19`: row padding is `0 var(--space-3)`.
  - `:138`: `:hover` has the same specificity as `[data-selected]`; selection wins only by coming later in the file.
  - `:138-172`: the attention overlay, `::after` with `hv-attn-tint`.
  - `:181-194`: selection is the `--sel` ground plus a 2px `::before` accent bar.
  - `:217-228`: attention and exited name colours.
  - `:326-341`: the colour bar, 3px at `right: 0`, widening to 12px on hover.
  - `:386-475`: densities. In compact, `__sub` takes `color: inherit` (`:446`), and the attention/exited overrides at `:458-475` repeat the name colours.
- `src/theme/components/project-card.css:13-40`: the sticky 26px header with a bottom border; the active project's label is `--accent` at `:40`; the card margin `var(--space-2) 0 var(--space-3)` is at `:11`.
- `src/theme/layout.css:51-68`: `#sidebar-resizer` is absolute, `right: 0`, 5px wide, z-index 20. It lives *inside* `<aside id="sidebar">` (`index.html:114-119`), so the grid placement at `layout.css:23` does nothing. `src/main.tsx:390-470` never reads the handle's own geometry: it uses `e.clientX` only.
- `src/theme/components/launcher.css:14-23`: `#sidebar` must never get a z-index, or the resizer is trapped in its stacking context.
- Where the colour variables come from:
  - `--session-color` is set inline on the row (`SessionRow.tsx:184`) and on the group `<li>` (`WorktreeGroup.tsx:94`), and inherits into the header. The fallback `var(--fg-subtle)` is needed when it is unset.
  - `--project-color` is set on the card (`ProjectCard.tsx:60`).
- `.hv-session-row` is rendered only in the sidebar, so a selection restyle cannot leak elsewhere. The launcher and command palette copy the old look in their own CSS (`launcher-item.css:3,21-27`).

### Constraints / dependencies

- **Contrast of `--on-accent` on the fill** (color-mix of accent 88% into surface; see the table below):
  - Fails on native-light (4.06, and this preset is **gated**), github-light (4.27), solarized-dark (3.56) and solarized-light (3.00).
  - Borderline on catppuccin-latte (4.53).
  - On light presets, mixing toward the surface always lowers the contrast. Plain `--accent` is already gated by `ui-contrast.mjs:44` (`--on-accent` on `--accent`, 4.5).
- **Accent and attention collide:**
  - Δhue ≤ 15° on hive-dark, hive-light, native-dark, alucard and gruvbox-dark.
  - Exactly equal on classic, native-light and terminal. Only terminal is monochrome, so `tokens.md:36` is already violated by the other two.
  - Any attention cue drawn in `--state-attention` on an accent fill disappears on those presets.
- **`scripts/ui-contrast.mjs`:**
  - It cannot resolve `color-mix()` (`hex()` at `:90-101` returns null, and the pair is skipped silently).
  - A new token in `PAIRS` must be declared in **every** preset, including those with `--contrast-exempt` (`:186-211`).
- **Straddling resizer:** it needs `overflow-x: hidden` dropped from `#sidebar`, or the handle moved out of it. `#projects` keeps its own `overflow-x: hidden`.
- **Any part of the resizer left inside the sidebar covers the right-edge colour bar once the gutter is 0.** Three `elementFromPoint` tests encode this:
  - `sidebar-window-title.spec.ts:83-107` (the unhovered 3px bar);
  - `sidebar-colour-picker.spec.ts:~52-70` (the hovered 12px bar at its centre);
  - `shared-worktree-cue.spec.ts:57-94` (`r.right - 1`).

### Tests that change

- **Breaks by design:**
  - `test/e2e/sidebar-group-flatten.spec.ts:41-49`: a grouped row's state icon must sit at the same x as an ungrouped one. Rewrite it to assert the indent. The full-bleed header comment at `:1-11` also changes.
  - `test/e2e/sidebar-sticky.spec.ts:106-137`: `tops[0] > 4` relied on 4px list padding plus an 8px card margin.
  - The three `elementFromPoint` tests above, depending on where the resizer ends up.
  - `test/e2e/shared-worktree-cue.spec.ts:84-91`: asserts the panel's 3px `::after` bar.
- **Must keep passing:**
  - `sidebar-group-flatten.spec.ts:83-102`: the header's alpha is 1.
  - `:60-81`: radius is 0.
  - `:109-218`: the branch stays legible at 220px.
  - `theme.spec.ts:945-1037`: the attention tint stays quieter than the selected ground, and the pulse stays on `::after`.
  - `ux-polish.spec.ts:417-432`: dragging the resizer's box still resizes.
  - `launcher-stacking.spec.ts:18-110`: samples the resizer strip's centre pixel.
- **Visual baselines:** `theme.spec.ts:134-143,208-217` and `chrome.spec.ts:160`. These run only with `HIVE_SNAPSHOT=1` on darwin.

### Prior lessons

- Darwin pixel baselines are not checked in CI and many are stale. After `--update-snapshots`, commit only the PNGs that render the sidebar, and list the rest as drift in the PR.
- Selectors with equal specificity tie on source order: `.hv-session-row:hover` versus `[data-selected]`. Hovering a selected row must keep the fill and the `--on-accent` text.
- Assert CSS behaviour on the real element with `getComputedStyle` in Playwright, not on a probe element or a restated constant.

### Conventions card

```
build:  ./build.sh
test:   scripts/test.sh [go|unit|dom|e2e]    # frontend: unit dom e2e
e2e:    CI=1 npx playwright test <spec>       # CI=1 or a stale dev server is reused
lint:   scripts/ui-lint.sh --strict ; scripts/ui-lint.sh --contrast --verbose ; npx biome ci . ; npm run typecheck (needs ./scripts/ci-bootstrap.sh first)
```

- TDD: every behaviour change ships with the test that would catch its regression. Boil the lake: fix every high-confidence nit in the same PR.
- Every UI change follows `docs/design-docs/ui/`. Update `components.md`, `patterns.md` and `tokens.md` in the same commit. Components use tokens only (enforced by `ui-lint`).
- Changelog: add `.changesets/<slug>.md` (type `changed`, bump `patch`, because this is a restyle, not a feature). Never edit `CHANGELOG.md`.
- CSS fixes are validated in a real browser (Playwright plus `elementFromPoint` / `getComputedStyle`); vitest cannot see CSS.
- No daemon change, so no `DaemonContract` bump.

## Approach

It is almost all CSS. There is one markup move (the resizer, in `index.html`), no JS change and no daemon change. Solarized-dark and solarized-light get a new `--on-accent` value, which the spec's non-goals allow as a token value the new style needs. Each of the four changes lives in the component CSS that owns it, and the design docs are updated in the same commit.

1. **Worktree group becomes a rail** (`sidebar.css`)
   - `.hv-worktree-group` drops its `--surface-raised` ground and keeps its top and bottom hairlines and its `--space-1` margin.
   - The sticky header keeps an opaque ground, now `var(--surface)`, because the alpha-1 test and the sticky scroll-under both still need it. It drops its `border-bottom`, as the tuner's rail mode did; the rail marks where the body starts.
   - `.hv-worktree-group__rows` gets `margin-left: var(--space-3)` and `border-left: 1px solid var(--session-color, var(--fg-subtle))`.
   - Member rows get `padding-left: var(--space-2)`, so their text sits about 9px right of an ungrouped row's.
   - The panel's right-edge `::after` bar is removed, and the per-row colour bar inside a group returns to its normal painted state. Spec: "per-row colour bars stay". The rail now carries the group colour, so one panel bar plus transparent row bars is no longer needed.
   - Header `__label` and `__branch` take `color-mix(in srgb, var(--session-color, var(--fg-muted)) 70%, var(--fg))`. The 30% `--fg` share keeps them readable, even on light presets with a pale user colour.
   - The rail indent is the same at every density. The tuner chose 13px; this uses `--space-3` (12px), so no literal is needed.
2. **Selected row becomes an accent fill** (`session-row.css`)
   - Colours: `.hv-session-row[data-selected]` gets `background: var(--accent)` and `color: var(--on-accent)`. That is **100%, not 88%**: the operator decided this after research, because 88% fails AA on native-light (gated), github-light and both solarized presets. `--on-accent` on `--accent` is already gated by `ui-contrast.mjs:44`.
   - The `::before` accent bar is removed; the fill is the selection signal.
   - Inside a selected row, `--on-accent` also applies to:
     - `__name`, `__sub`, `__meta`, `__worktree-count`, `__idea`, the agent chip text, `.hv-icon-btn`, and the focus-visible outlines;
     - the attention and exited name colours, which are overridden while the strikethrough stays;
     - the state icon (`.hv-state-icon`, including its pulse ring) and the plan pie (`--hv-plan-color`): the icon's *shape* carries the state. The icon's pulse ring is handled by redefining `--state-attention: var(--on-accent)` on `.hv-session-row[data-selected] .hv-state-icon`; a `var()` inside the keyframes (`icon.css:52-55`) resolves on the animated element. A stale plan pie needs `!important` at higher specificity (`.hv-session-row[data-selected] .hv-session-row__plan--stale`), where it takes `--on-accent` at 55% via color-mix. Without that, `session-row.css:~121` keeps it `--fg-subtle` (about 1.3:1). Its subagent badge follows it. Without this the icons measure 1.0–2.1:1 against the fill and the status dot vanishes, which AGENTS.md forbids.
   - **Specificity.** The non-compact overrides are declared *after* `session-row.css:217-244`, where they tie at (0,3,0). The compact ones use `:root[data-density='compact'] .hv-session-row[data-selected][data-state='…'] .hv-session-row__sub`, at (0,6,0), to beat `:458-475`, which sit at (0,5,0).
   - `.hv-session-row[data-selected]:hover` keeps the accent ground. It is stated explicitly so source order no longer decides it.
   - **The attention cue on a selected row:** for `data-state` `attention` and `failed`, the `::after` pulse background becomes `color-mix(in srgb, var(--on-accent) 18%, transparent)`, using the same `hv-attn-tint` animation. With reduced motion it is a static `--on-accent` tint at 12%. The diamond state icon stays too, now in `--on-accent` and so visible, and under reduced motion it is the main cue.
   - **Solarized.** Even plain `--accent` fails AA there: `--on-accent` measures 4.08:1 on solarized-dark and 3.41:1 on solarized-light. Both presets get `--on-accent: #00161c`, a darkened solarized base03, which gives 5.04:1 on `#268bd2`. This also fixes their primary buttons.
3. **The list loses its gutters and the resizer moves outside** (`sidebar.css`, `layout.css`)
   - `#sessions, #projects` padding becomes `4px 0` (the vertical padding stays).
   - **`#sidebar-resizer` moves out of `<aside>` and becomes a sibling in `#app`** (`index.html:114-119`). `layout.css:23` already places it in the grid (row 3 / span 2, column 1), so that rule stops being dead. It becomes an in-flow grid item: `justify-self: end; width: 5px; margin-right: -5px; position: relative; z-index: 20`. It now sits just past column 1, over the terminal's (and the minimized tray's) first 5px, and nothing inside the sidebar. `#sidebar` keeps `overflow-x: hidden`, so nothing spills while the column animates. `main.tsx:390-470` finds the handle by id and reads only `clientX`, so the JS does not change.
   - `#app.sidebar-hidden #sidebar-resizer { display:none }` already exists at `layout.css:68`, so a hidden sidebar leaves no strip over the terminal.
   - `#sidebar` still gets no z-index. The comment at launcher.css:14-23 about the resizer being trapped in the sidebar's stacking context is rewritten, since the handle no longer lives there.
4. **Project header gets a colour top rule** (`project-card.css`)
   - It is drawn as `box-shadow: inset 0 2px 0 var(--project-color, var(--fg-subtle))`, not as a border, so the 26px header height, and with it the group header's sticky offset `--sidebar-project-header-h`, does not change.
   - The card margin becomes `var(--space-1) 0 var(--space-3)`: 4px above and 12px below, as the tuner rendered it. The visible gap between projects is about 16px (the next card's 4px top margin plus this card's 12px bottom margin). The spec's "about 4px between projects" describes the tuner slider, not the result on screen; I'll correct the spec's wording in the same PR.
5. **Out of scope** (operator decision): the launcher and palette items keep the old `--sel` + bar look. Only the stale comment in `launcher-item.css:3` that says they match the session row gets fixed.

### Why this beats the obvious alternative

- **The fill:** a new `--sel-fill` token per preset would let the fill differ from `--accent`. It would cost 20 preset edits plus a contrast pair for a difference nobody can see. Plain `--accent` is already gated.
- **The attention cue:** an attention-coloured ring or bar vanishes on the 8 presets where accent ≈ attention. Motion is independent of hue, and it is attention's existing channel.
- **The resizer:** keeping it inside would cover the zero-gutter colour bar, so either the picker would need hover to open, or three hit-test tests would have to be weakened.

### Files to change

0. `cmd/hivegui/frontend/index.html`: move `#sidebar-resizer` out of `<aside id="sidebar">`, directly after it.
0b. `src/theme/themes.css`: `--on-accent` becomes `#00161c` in solarized-dark (:471) and solarized-light (:500).
0c. `src/theme/components/launcher.css:14-23`: comment only.
0d. `docs/design-docs/ui/themes.md:28,40-41`: a note that solarized `--on-accent` is darkened to `#00161c` so the selected-row fill reads at AA, rather than keeping upstream's value.
1. `src/theme/components/sidebar.css`: list padding and its comment; group ground, rail, member padding and header colour; remove the `::after` bar and the transparent-row-bar rules; rewrite the comments.
2. `src/theme/components/session-row.css`: the selection rule, the selected-row text/icon/focus overrides, the explicit selected:hover rule, the selected+attention pulse (motion and reduced-motion), and the compact-density selected override; rewrite the comments at :138-194.
3. `src/theme/layout.css`: `#sidebar-resizer` becomes a grid item that sits outside the sidebar (see Approach 3), replacing the absolute `right: 0`, plus a comment explaining why.
4. `src/theme/components/project-card.css`: the header's top-rule box-shadow and the card margin.
5. `src/theme/components/launcher-item.css:3`: comment only.
6. `docs/design-docs/ui/components.md`:
   - sessionRow: `:36`, `:41` (agent chip on the fill), `:42` (gutter/resizer), `:43`, `:45`; the launcher item's "same as session row" at `:111`;
   - worktreeGroup: `:55-61`;
   - projectCard: `:67-69`.
7. `docs/design-docs/ui/patterns.md`:
   - Selection vs attention `:9-14`: the fill, and the `--on-accent` pulse as attention's channel on a selected row; drop "never share a colour", which classic and native-light already break.
   - The group colour bar `:18-20`: the rail is on the left, and the left edge is no longer selection's.
8. `docs/design-docs/ui/tokens.md:11` (`--surface-raised` no longer grounds the group) and `:16-18`: `--accent` becomes the selected-row fill; `--on-accent` is the text on that fill; `--sel` stays for the launcher, palette, tabs and find.
9. `docs/product-specs/455-…md`: the Desired behavior wording on project spacing; "about 88%" becomes "accent"; "straddles the border" becomes "sits just outside the sidebar's edge" (in Desired behavior and SC5).
10. Tests: see below.

### New files

- `.changesets/sidebar-legibility.md`: `type: changed`, `bump: patch`, `issue: 455`. It is a visual restyle, not a feature, so there is no `site/features.json` entry.
- `test/e2e/sidebar-legibility.spec.ts`: new geometry and colour assertions (below).

### Tests

All tests use `getComputedStyle` on real elements in Playwright, following the prior lesson. They run with `CI=1`.

The new file, `test/e2e/sidebar-legibility.spec.ts`:
- **`selected row fills with --accent and reads in --on-accent, on every first-party preset`:** loops over `PRESETS` (excluding `system`), as `theme.spec.ts:323` does. For each preset it checks that the row's `backgroundColor` equals the computed `--accent` and its `color` equals `--on-accent`. It fails against today's `--sel` ground.
- **`hovering the selected row keeps the fill`:** hovers the selected row and asserts the background is still `--accent`. It also hovers an unselected row and asserts its background is not `--accent`.
- **`a selected attention row pulses in --on-accent`:** selects the seeded attention row. It asserts that `::after` has `animationName === 'hv-attn-tint'` and that its `backgroundColor` is rgba with the `--on-accent` rgb channels. Under `emulateMedia({reducedMotion:'reduce'})` it asserts no animation and opacity 1.
- **`selected row's name, sub and agent chip text are --on-accent`, at normal, tight and compact density,** including an attention row and an exited row. The exited row keeps `line-through`. This catches the (0,5,0) compact overrides silently winning.
- **`the selected row's state icon and plan pie stay visible on the fill, on every preset (including a stale plan)`:** the state icon's computed colour is `--on-accent`, and a real WCAG contrast calculation shows it at least 3:1 against the computed `--accent`.
- **`--on-accent on --accent passes WCAG AA on every preset`:** a real contrast calculation (not colour equality) over all `PRESETS`, *including* contrast-exempt ones, because the selected row reads in `--on-accent` everywhere. This is what proves SC2; the equality test only proves the wiring.
- **`worktree members sit behind a rail in the group colour`:**
  - `.hv-worktree-group__rows` has a 1px `borderLeftWidth`, and its `borderLeftColor` equals the group `<li>`'s `--session-color`;
  - a grouped state icon's x is at least 8px right of an ungrouped one;
  - ungrouped rows' parent list has no left border;
  - the group panel's `backgroundColor` is transparent or `--surface`, not `--surface-raised`.
- **`group header text takes the group colour`:** `__branch`'s color differs from the computed `--fg-muted`, and its rgb lies between the session colour and `--fg`.
- **`the session list has no side gutters`:** `#projects` `paddingLeft` and `paddingRight` are both `0px`.
- **`the resizer sits outside the sidebar and the colour bar is reachable at rest`:**
  - `#sidebar-resizer` is not a descendant of `#sidebar`, and its left edge is at or past `#sidebar`'s right edge (`getBoundingClientRect().right`, border included);
  - `elementFromPoint` at the centre of an **unhovered** row's colour bar lands on `.hv-session-row__colour`.
- **`the hidden sidebar leaves no resizer strip`:** toggles the sidebar off and asserts the resizer's `display` is `none`.
- **`project header carries a 2px top rule in the project colour`:** the header `boxShadow` contains the computed `--project-color`, and the header height is unchanged at 26px.

Changed specs:
- `test/e2e/sidebar-group-flatten.spec.ts:41-49`: "a grouped row starts at the same x" becomes "a grouped row is indented behind the rail" (grouped x − ungrouped x ≥ 8), and the header comment at `:1-11` is updated. The alpha-1 header and the 220px legibility tests stay. In the radius-0 test (`:60-81`), the `::after` `bar` key is dropped (`:77`), because it would read a pseudo-element that no longer exists; it is replaced by the rail list's radius.
- `test/e2e/shared-worktree-cue.spec.ts:55-120`: `barOf` stops reading the panel's `::after` and reads each row's `.hv-session-row__colour` background instead. Otherwise the "same colour" test (~:115-120) compares two transparent values and passes vacuously. The 3px `::after` assertions (:84-91) become rail assertions (`__rows` border-left 1px in `--session-color`). `hitInsidePanel` at `r.right - 1` now lands inside the panel, because the resizer is outside.
- `test/e2e/theme.spec.ts:945-1015`: only the comment changes. It now says this covers attention on an *unselected* row, and that the selected-row cue is tested in `sidebar-legibility.spec.ts`.
- `test/e2e/ux-polish.spec.ts:417-432` and `launcher-stacking.spec.ts`: no edits planned. They locate `#sidebar-resizer` by id, so moving it in the DOM should not affect them, and running them verifies that.
- `test/e2e/sidebar-sticky.spec.ts:106-137`: re-check `tops[0] > 4`. With 4px padding plus a 4px margin the value is 8, so it should pass. Adjust only if the measurement proves otherwise.
- Visual baselines (darwin, `HIVE_SNAPSHOT=1`): regenerate and commit only the PNGs that render the sidebar, and list the drift in the PR.

### Verification

```
cd cmd/hivegui/frontend
../../../scripts/ci-bootstrap.sh            # bindings for typecheck, if missing
CI=1 npx playwright test sidebar-legibility sidebar-group-flatten shared-worktree-cue sidebar-sticky sidebar-colour-picker sidebar-window-title theme ux-polish launcher-stacking sidebar-density
cd ../../.. && scripts/test.sh unit dom e2e
scripts/ui-lint.sh --strict && scripts/ui-lint.sh --contrast --verbose
(cd cmd/hivegui/frontend && npx biome ci . && npm run typecheck)
```

Plus a manual visual check against the tuner in `wails dev` (http://localhost:34115) on hive-dark, hive-light and dracula, at normal and compact density.

#
- **2026-09-24** — Implemented. PR #456 opened. All frontend layers and lint gates are green.

## PR convergence ledger

- **2026-09-24 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 3f20e7737587c7fb3e4dd52835687ef7d57b770b4ebe4f3ab9306c65c352c492; threads_open: 1; action: escalated:risky-fix-needs-human-decision; head_sha: 7e7d1d8.
- **2026-09-24 iter 2** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 544a58fb5894c83ace6523f5d23f480bc5c40b17593157dd2ab6a5437e13c4c8; threads_open: 1; action: escalated:risky-fix-needs-human-decision; head_sha: f00d160.
- **2026-09-24 iter 3** — verdict: APPROVE; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: de63ca8.

## Gate verdict

- **2026-09-24** — verdict: PASS; phase: —; checks: 13 passed / 0 failed / 0 followups; followups: none; one-line: all 7 success criteria met with per-preset e2e evidence, no non-goal bleed, and the docs match the shipped CSS.
  - 2026-09-24 dimensions:
    - acceptance — PASS — SC1–SC7 verified. 68 specs green (sidebar-legibility, sidebar-group-flatten, shared-worktree-cue); ui-lint 0 violations; ui-contrast 0 failures. The resizer drag test (ux-polish) was covered by the full local e2e run (460) and CI.
    - non-goals — PASS — only CSS, markup, docs and tests changed; the solarized `--on-accent` change is justified; launcher and palette changes are comment-only.
    - doc accuracy — PASS — changeset valid (`type: changed`, so `regression_of` does not apply). components, patterns, tokens and themes docs match the CSS. No dangling tuner reference. CHANGELOG and index untouched.

## Open questions / risks

- **Hue collision.** On hive-dark and similar presets, a selected attention row's orange name gives way to `--on-accent`, so attention there is carried by the pulse plus the diamond icon. That is the operator's choice.
- **Moving the resizer out of `<aside>`.** If React ever re-renders `#sidebar` from a template, the handle could be lost. It is static markup in `index.html`, and `main.tsx` binds it once at boot. The e2e drag test catches a regression.
- **The resizer now covers the first 5px of the terminal pane and of the minimized tray.** Clicks there resize instead of reaching xterm or a tray chip. That is acceptable: xterm has inner padding, and the tray's first chip is inset. The pending-prompt card (z-index 15) and the find box (z-index 5) at the terminal's left edge also sit under the 5px strip.
- **Changing solarized `--on-accent` also changes `.dead-btn.primary`** (`phase-overlay.css:70`), which fills with the user's `--session-color`: solarized-light's light ink becomes dark ink there. Accepted.
- **The group header's `color-mix` with a user colour** can't be contrast-gated, since the colour is data. The 30% `--fg` share is the mitigation. A contrast check with the default palette colours on hive-light is a nice-to-have.
- **Compact density.** The 12px rail indent plus 8px member padding takes 9px from the title at compact density. That is accepted (spec: "all densities").

## Second opinion

- **Round 1: revise (confidence 8).** Five must-fix items, all applied:
  - the solarized presets fail AA even at 100% accent, so their `--on-accent` is fixed and a real contrast test is added;
  - state icons and the plan pie vanish on the fill, so they take `--on-accent`;
  - `shared-worktree-cue` "same colour" would pass vacuously once the panel `::after` is gone;
  - the `sidebar-group-flatten.spec.ts:77` radius check on the missing `::after` would pass vacuously;
  - CSS specificity ties would let the attention, exited and compact rules beat the selected overrides.

  I also adopted five of its suggestions: the resizer moves out of `<aside>` instead of dropping `#sidebar`'s overflow clip, the group header hairline is dropped, the missing doc lines are added, the changeset bump is settled as patch, and the `theme.spec` comment is updated.
- **Round 2: approve (confidence 8).** Two small must-fix items, both applied: the stale plan pie's `!important` needs a selected override, and `themes.md` needs the solarized note. Four nice-to-haves were also applied: the icon pulse-ring mechanism, the dead-session button and overlay-overlap risks, and the spec's "straddles" wording.

## Decision log

- **2026-09-24** — The design comes from the tuner's raw state (in the spec's Notes). Why: the operator chose it interactively over two rounds.
- **2026-09-24** — The resize handle straddles the border, and a selected row that needs attention keeps an attention cue. Why: the operator decided both at the brainstorm gate.

- **2026-09-24** — The selected fill is plain `--accent` at 100%, not 88%. Why: 88% fails AA on native-light (gated), github-light and solarized, and `--on-accent` on `--accent` is already gated in CI.
- **2026-09-24** — A selected row that needs attention signals it with the `::after` pulse tinted in `--on-accent`. Why: accent is close to or equal to attention on 8 presets, so an attention-coloured cue vanishes. Motion does not depend on hue.
- **2026-09-24** — The resizer sits fully outside the sidebar (over the border and the terminal's first pixels); `#sidebar` drops `overflow-x: hidden`. Why: anything left inside covers the colour bar once the gutter is 0.
- **2026-09-24** — The launcher and palette items keep the old selection look (comment fix only). Why: out of scope; they are not the sidebar.

- **2026-09-24** — On the selected row the state icon keeps its state colours on a `--surface` disc, and does not switch to `--on-accent` ink. Why: `state-glyphs.spec.ts` enforces that "needs you" never shares a colour with "fine", and inking the icon made them identical. The operator chose the disc when this came up during implementation.

- **2026-09-24** — The tuner mock page was dropped from the PR. Why: CodeQL flagged its localStorage→innerHTML path. The operator chose to remove it rather than sanitize it or dismiss the alert.

- **2026-09-24** — A selected row that needs attention now pulses a 2px inset `--on-accent` border instead of an ink tint. Why: review iteration 2 and CodeRabbit found the 18%/12% tint dropped text under AA (Solarized 3.79:1 / 4.18:1). The operator chose "pulse a border only".

## Progress

- **2026-09-24** — Spec written, triaged (enhancement, M, P2), research done.
- **2026-09-24** — Plan approved (HTML review, first round). Second opinion: revise, then approve (8/10).

## Open questions


# UI design system — master plan

- **Spec:** [docs/product-specs/ui-design-system.md](../../product-specs/ui-design-system.md)
- **Issue:** —
- **Stage:** IMPLEMENT (phases 1–5 shipped; phase 6 not started)
- **Status:** active

## Summary

Replace the piece-by-piece GUI styling with a token-driven, themeable design system: one token layer, five presets (light and dark at launch), an SVG icon family, a small primitive component layer in `src/ui/`, and a CI lint that keeps literals out. Six phases, each its own PR and its own detailed plan; each phase leaves the app shippable.

## Research

- `cmd/hivegui/frontend/src/style.css` — 2159 lines, 51 hex colours, 12 font sizes, 1 custom property. Loaded from `index.html` (`<link>`), not imported from TS.
- `src/app/sidebar.ts` (666 lines), `view.ts` (795), `modals/*.ts` — hand-built DOM with string classes; targets for the primitive layer.
- `src/app/session-term.ts:314` — xterm theme hard-coded `{ background: '#000000' }`.
- `src/app/state.ts:192` — `localStorage['hive.fontSize']`; the view key sits beside it. Theme prefs go in the same store.
- `biome.json` excludes `**/*.css` and `**/*.html` — Biome cannot be the CSS gate; `scripts/ui-lint.sh` is.
- CI: `.github/workflows/ci.yml`, frontend steps run on the `matrix.biome` (Linux) leg; new lint step goes there.
- Tests: `test/unit` (vitest), `test/dom` (jsdom), `test/e2e` (Playwright + `wails-mock.ts`), `test/e2e-real` (flaky, see memory). Screenshot baselines belong in `test/e2e`.
- `AGENTS.md` › UX Best Practices — inline key hints are mandatory; the system keeps them (`kbd` primitive).

## Approach

Bottom-up, visually no-op first. Phase 1 introduces tokens with a `classic` preset that reproduces today's pixels exactly, so the largest mechanical diff carries zero visual risk and can be verified by screenshot equality. Every later phase is a visible, reviewable change against a stable foundation. Chosen over "restyle the sidebar first" because restyling on top of 51 literals means doing every colour decision twice.

### Phases

| # | Deliverable | Detailed plan | Visible change |
|---|---|---|---|
| 1 | `src/theme/{tokens,themes}.css`, `theme.ts` (preset apply from localStorage), `style.css` literals → tokens, xterm theme from tokens, `scripts/ui-lint.sh` (warn), CI step, screenshot baseline | [ui-design-system-phase1.md](ui-design-system-phase1.md) | none (`classic` default) |
| 2 | `src/ui/icons.svg` + `icon()`/`stateIcon()`/`iconButton()`/`kbd()`; replace every Unicode glyph; lint → error | [ui-design-system-phase2.md](ui-design-system-phase2.md) | icons only |
| 3 | `sessionRow`, `projectCard`, `chip` primitives; sidebar + trays rebuilt on them; sidebar min-width 220 | [ui-design-system-phase3.md](ui-design-system-phase3.md) | sidebar |
| 4 | `banner`, `statusBar` skin, grid tile header, toolbar, launcher rows, empty/phase states | [ui-design-system-phase4.md](ui-design-system-phase4.md) | chrome |
| 5 | `dialog`, form fields; Settings/Worktrees/Project editor/Help on them; **Settings › Appearance** (preset picker + custom tokens) | [ui-design-system-phase5.md](ui-design-system-phase5.md) | dialogs, theming UI |
| 6 | Default → `hive-dark`; ship `hive-light`, `native-*`, `terminal`; contrast check in lint; per-preset screenshot baselines; `style.css` split into `src/theme/{base,layout}.css` + `components/*.css` | [ui-design-system-phase6.md](ui-design-system-phase6.md) | everything |

### Files to change (across phases)

- `cmd/hivegui/frontend/index.html` — link theme CSS, inline sprite
- `cmd/hivegui/frontend/src/style.css` — tokenised (1), then dissolved (6)
- `cmd/hivegui/frontend/src/app/{sidebar,view,banners,session-term}.ts`, `src/app/modals/*.ts` — migrate to primitives (2–5)
- `.github/workflows/ci.yml` — `ui-lint` step (1)
- `AGENTS.md` — already points at the spec

### New files

- `cmd/hivegui/frontend/src/theme/` — `tokens.css`, `themes.css`, `theme.ts`, `fonts/*.woff2` (phase 6), later `base.css`, `layout.css`, `components/`
- `cmd/hivegui/frontend/src/ui/` — one file per primitive + `icons.svg`
- `scripts/ui-lint.sh`
- `cmd/hivegui/frontend/test/e2e/theme.spec.ts` — screenshot baselines

### Tests

- `test/unit/theme.test.ts` — preset resolution, override sanitiser
- `test/dom/ui-*.test.ts` — one per primitive (renders, aria, data-state)
- `test/e2e/theme.spec.ts` — screenshot equality (phase 1), per-preset baselines (6)
- `scripts/ui-lint.sh` self-test fixtures under `scripts/testdata/ui-lint/`

## Decision log

- **2026-08-29** — Default preset is option B (`hive-dark`); A/C/current ship as presets. Why: user choice after mock comparison; all share one token layer so the cost is values only.
- **2026-08-29** — Two-line rows inside project cards; subtitle = window title. Why: window titles are the most-used feature; state moves to the icon channel.
- **2026-08-29** — SVG line icons for all glyphs, geometric shapes for states. Why: consolidating iconography only works if actions and states come from one family; Unicode rendering varies per platform.
- **2026-08-29** — Enforcement = docs + lint + primitive layer. Why: docs-only is the regime that produced 51 colours.
- **2026-08-29** — Phase 1 is visually no-op via `classic`. Why: separates mechanical risk from design risk; screenshot-verifiable.
- **2026-08-29** — `exit_code` is not on the wire; exited-vs-error resolves from `last_error`. Why: found while planning phase 2/3.
- **2026-08-29** — `[n]` hints go on session rows, not project cards. Why: ⌘1–9 bind to `orderedSessions()`; no project chord exists.
- **2026-08-29** — `hive-light --on-accent` → `#1a1b22`, `terminal --fg-subtle` → `#6f6f6f`. Why: computed WCAG ratios in phase 6 planning failed the spec's own rule.
- **2026-08-29** — Phases 2–6 planned up front at task depth; implementers re-verify `file:line` refs and may amend. Why: user request.
- **2026-08-29** — Inline key hints stay (AGENTS.md rule). Why: existing project policy; only rendering is standardised via `kbd()`.
- **2026-08-29** — Pixel baselines in `test/e2e/theme.spec.ts` are gated behind `HIVE_SNAPSHOT` and default-skipped. Why: CI runs e2e on ubuntu + macos + windows, but Playwright snapshots carry a per-platform suffix; committed darwin baselines would fail the other legs. The cross-platform guard is the computed-style preset test instead.
- **2026-08-30** — Phase 1's "pixel-identical to v2.4.0" guarantee is retired on purpose. Why: `classic` is a preset of token *values*, not of markup, and phase 4 rebuilds the chrome markup (status bar moved into the grid, banners built in TS, tile header rebuilt). The `theme.spec.ts` baselines were regenerated and now guard preset switching instead.
- **2026-08-30** — The grid tile header drops the project→session colour gradient. Why: README principle 2, one channel per fact — the sidebar card owns project identity and the tile already encodes the session twice (colour + name). Reversing it is one `background:` rule in `tile-header.css`.
- **2026-08-30** — The launcher's per-agent colour swatch stands in for components.md's "leading icon for agent kind". Why: `icons.md` forbids a sprite symbol per agent, and the colour is the one thing that tells two agents apart at a glance.
- **2026-08-30** — The umbrella spec is NOT gated per phase. Its `## Success criteria` span all six phases (`dialog` + form fields and Settings › Appearance are phase 5; the ui-lint contrast check is phase 6), so `/hs-merge-gate` against it mid-programme fails on criteria the phase PR was never meant to deliver. Phase PRs merge on green CI + a converged `/hs-review-loop`; the spec stays at `IMPLEMENT` and is gated once, after phase 6. Why: the spec's own last criterion says each phase ships as its own PR, and the one-spec-one-PR gate does not model a six-PR programme.
- **2026-08-30** — Status-bar mode hints are `⌘G`, `⇧⌘K` and `⌘`+arrows, not the chords the phase-4 plan sketched. Why: verified against `lib/shortcuts.ts` — the palette is `⇧⌘K` and tile movement is `⌘`+arrows; a hint naming an unbound chord is worse than no hint.
- **2026-08-29** — `data-theme` is stamped by an inline blocking `<script>` in `<head>`, not by the module import. Why: `type="module"` is deferred and would let the first paint land on the wrong preset.
- **2026-08-29** — Terminal cursor and selection colours changed from white to amber (under `classic` preset). Why: v2.4.0 xterm theme was hard-coded `{ background: '#000000' }` with no cursor/selection values, so xterm supplied its own white defaults; they now derive from `--accent` per the spec's xterm mapping table. Terminal text stays white and background is unchanged.
- **2026-08-29** — `classic`'s `--term-fg` is `#ffffff` not `#ddd`. Why: terminal text must keep reproducing v2.4.0 exactly.
- **2026-08-30** — Sprite is inlined via Vite's `?raw` + one-time DOM injection, not a build plugin. Why: no plugin code, resolves identically under vitest, and every icon is created from TS anyway.
- **2026-08-30** — `last_error` replaces `exit_code` in the state resolution. Why: no exit code exists on the wire (`internal/wire` has no exit-code field).
- **2026-08-30** — Edit-project uses `settings` (gear); the 22-icon inventory has no pencil.
- **2026-08-30** — `ui-lint`'s glyph rule is a denylist of icon-shaped characters, not `[^\x00-\x7F]`. Why: prose comments and mandated `⌘` key hints are legitimate non-ASCII, so the old rule could never go strict.
- **2026-08-30** — `.hv-icon` restates the sprite root's `fill`/`stroke`/`stroke-width`/`linecap`/`linejoin`. Why: `<use>` clones a `<symbol>` without its defining tree's ancestors, so the sprite root's presentation attributes never reach the clone — without this every stroke-based icon renders invisible. This was a real bug caught only by a screenshot check; it is worth writing down so nobody "simplifies" it away.
- **2026-08-30** — Two of the three state-icon sites — the sidebar row (`updateSidebarSelection`) and the grid tile header (`session-term.ts`'s `refreshStateIcon`) — are patched in place via `updateStateIcon()`; the minimized tray gets a correct icon for free because `renderMinimizedTray()` clears and rebuilds every chip from scratch on each render, calling `stateIcon()` fresh rather than patching. Why patch at all: a bell only toggles a CSS class, so without an explicit refresh on the two sites that don't already rebuild wholesale, the shape and its `<title>` would keep saying "Running" while the session waits.
- **2026-08-30** — The project-header action buttons keep an 18px box via a scoped `style.css` override rather than the primitive's 22/24 sizes. Why: five 24px buttons plus the caret squeezed the project name out of the sidebar entirely.
- **2026-08-30** — `[n]` key hints render on session rows, not project cards. Why: ⌘1–9 selects the nth session in global order (`keyboard.ts`); there is no project-number binding, and a hint for a key that does nothing is worse than none. Revisit if a project-number chord is added.
- **2026-08-30** — Sidebar rows gained restart and kill. Why: patterns.md requires `rotate`/`x` on an exited row, and there was no way to restart a session from the sidebar at all (`RestartSession` was dead code in the frontend). Kill on a live session goes through the native confirm. Second deviation, from patterns.md › Hover-revealed actions ("every hover action has a keyboard equivalent listed in the help overlay"): kill has one (⌘W, `close-session`), restart does not — `restart-session` ships with an empty chord in `lib/shortcuts.ts`, reachable via the command palette but with nothing to list in the overlay. Bind a chord when a key is free; until then the hover button is restart's only direct path.
- **2026-08-30** — Both minimized trays (`#minimized-tray`, `#minimized-projects`) are `<div role="toolbar">`, not `<ul>`. Why: `chip()` returns a `<span>` so it can also sit inline elsewhere, and a `<span>` is not a valid child of `<ul>`; `role="toolbar"` with an `aria-label` is the accurate role for a strip of restore controls anyway, where a list role would announce them as content.
- **2026-08-30** — State resolution reads `last_error`, not `exit_code`. Why: the daemon never sends an exit code; `SessionInfo` has `alive` and `last_error` only. Line 2 reads "Exited" or "Exited — <error>".

- **2026-08-31** — Appearance applies on change, not on Save. Why: a theme with no round-trip and no validation has nothing to be transactional about, and a preview you cannot see is not a picker. Cancel therefore leaves it applied, and the section says so.
- **2026-08-31** — Overrides are sanitised on write and stored as finished CSS. Why: the pre-paint boot script would otherwise need a second copy of the sanitiser; `theme.ts` re-sanitises on read so a hand-edited store is still safe.
- **2026-08-31** — The override block is `:root:root`, not `:root`. Why: themes.css's preset blocks are `:root[data-theme="…"]` (0,2,0) and outrank a plain `:root` (0,1,0) whatever the cascade order — as planned, every user override was silently ignored. Caught in a real browser, pinned by `theme.spec.ts`.
- **2026-08-31** — Settings keeps its Updates section outside the scrolling part of the dialog body. Why: `dialog()` scrolls the whole body, so a dozen custom agents pushed the channel picker below the fold; `test/e2e/settings.spec.ts` already guarded that and caught it.
- **2026-08-31** — Phase 5 also rebuilt the Updates section (channel, source repo, update action), which the phase-5 plan predates. Why: it lives inside the Settings dialog, so it moved with the markup.

- **2026-08-31** — Override values are allow-listed by function, not deny-listed by `url()`. Why: review found `image-set("https://…")` passed the denylist and reached `background: var(--bg)` — egress. Unbalanced parens are now rejected too: `--accent: rgb(` swallowed the appended `;` and the rest of the block, killing every override with `rejected.length === 0`, so nothing was reported. Both reproduced in Chromium before fixing.
- **2026-08-31** — The pre-paint boot script shape-checks the store before injecting it. Why: re-sanitising a paint later closes the visual window, not the request; the store is hand-editable and that script writes straight into a `<style>`.
- **2026-08-31** — Custom agents sits above Appearance in the Settings scroll region. Why: the agent list is what people open Settings to edit and the only section that grows; Appearance above it pushed the list off-screen on open.
- **2026-08-31** — The custom-token box debounces at 150ms. Why: every keystroke otherwise ran a style invalidation plus a `getComputedStyle` and palette rebuild on every live terminal plus a synchronous `localStorage` write — exactly the per-frame case `applyXtermTheme`'s comment rules out.
- **2026-08-31** — `.hv-dialog__actions` wraps. Why: a four-answer question overflows the `sm` panel, and without wrapping the labels broke to three lines inside a 28px button and rendered outside it — on the dialog that deletes branches.

- **2026-08-31** — ANSI 0–15 shipped in phase 5 after all, rather than being deferred to phase 6. Why: `themes.md` had documented `--ansi-*` as existing when no such token ever did, so xterm kept its Tango defaults under every preset — measured, seven of the sixteen fail WCAG AA on `hive-light`'s white ground and `brightWhite` sits at 1.16:1, invisible. Shipping a preset the picker offers but whose terminal output cannot be read is worse than the extra scope. `classic` and `hive-dark` restate the Tango values so nothing moves; `hive-light` gets a palette whose worst slot is 5.93:1.
- **2026-08-31** — The project editor gets a real Tab trap in `keyboard.ts`. Why: `dialog()` sets `aria-modal="true"`, which the pre-migration bare `role="dialog"` never claimed; without a trap the attribute was a false promise and Tab walked out into the sidebar. It was also the one migrated dialog with no containment test.

## Progress

- [x] Phase 1
- [x] Phase 2
- [x] Phase 3
- [x] Phase 4
- [x] Phase 5
- [ ] Phase 6

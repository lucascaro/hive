# UI design system — master plan

- **Spec:** [docs/design-docs/ui/README.md](../../design-docs/ui/README.md) (+ tokens/themes/icons/components/patterns)
- **Issue:** none yet — open one per phase when it starts
- **Stage:** PLAN
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
| 2 | `src/ui/icons.svg` + `icon()`/`stateIcon()`/`iconButton()`/`kbd()`; replace every Unicode glyph; lint → error | phase2 (write when 1 lands) | icons only |
| 3 | `sessionRow`, `projectCard`, `chip` primitives; sidebar + trays rebuilt on them; sidebar min-width 220 | phase3 | sidebar |
| 4 | `banner`, `statusBar` skin, grid tile header, toolbar, launcher rows, empty/phase states | phase4 | chrome |
| 5 | `dialog`, form fields; Settings/Worktrees/Project editor/Help on them; **Settings › Appearance** (preset picker + custom tokens) | phase5 | dialogs, theming UI |
| 6 | Default → `hive-dark`; ship `hive-light`, `native-*`, `terminal`; contrast check in lint; per-preset screenshot baselines; `style.css` split into `src/theme/{base,layout}.css` + `components/*.css` | phase6 | everything |

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
- **2026-08-29** — Inline key hints stay (AGENTS.md rule). Why: existing project policy; only rendering is standardised via `kbd()`.

## Progress

- [ ] Phase 1
- [ ] Phase 2
- [ ] Phase 3
- [ ] Phase 4
- [ ] Phase 5
- [ ] Phase 6

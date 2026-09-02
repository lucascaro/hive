# Add 12 IDE-inspired theme presets

- **Spec:** [docs/product-specs/305-add-ide-inspired-theme-presets.md](../../product-specs/305-add-ide-inspired-theme-presets.md)
- **Issue:** —
- **Branch:** `feature/305-add-ide-inspired-theme-presets`
- **Status:** active

## Summary

Add twelve presets drawn from the most-installed IDE themes — nine dark, three light — to
`themes.css`, register them in `PRESETS`, and group the now-19-entry picker into
`<optgroup>` buckets. Every source palette fails the repo's WCAG gate as published upstream,
so each block declares `--contrast-exempt: 1` and the gate reports it as skipped instead of
checking it. The gate's rules are untouched and the six default presets stay under them.

## Research

Authored via plan-first mode; the code references below were established during plan-mode
iteration.

**Relevant code:**

- `cmd/hivegui/frontend/src/theme/theme.ts` — `ThemeName` union, `PRESETS` (the picker
  renders from this list), `STAMPABLE` (derived from `PRESETS`, needs no edit), `resolveTheme`,
  `xtermTheme` (reads `--ansi-0…15` positionally, omits unset slots).
- `cmd/hivegui/frontend/src/theme/themes.css` — one `:root[data-theme="…"]` block per preset.
  File header rule: **every block re-values every token**; a partial preset falls through to
  `hive-dark` and looks broken on a light ground. `hive-light`'s block documents the one
  exception — `var()`-derived tokens (`--hover`, `--state-starting`, `--state-exited`) and
  the shared type/space/motion scales are correctly left to fall through.
- `cmd/hivegui/frontend/src/theme/tokens.css` — the base `:root` block; defaults are
  `hive-dark`.
- `cmd/hivegui/frontend/index.html:37–53` — pre-paint boot script with a duplicated `KNOWN`
  preset list. Cannot import the module (a deferred module script would paint the wrong
  preset first). Guarded by `test/e2e/theme.spec.ts:517`.
- `cmd/hivegui/frontend/src/ui/field.ts:62` — `selectInput`, flat `<option>` rendering only.
- `cmd/hivegui/frontend/src/app/modals/settings.ts:97` — the theme picker, already
  data-driven from `PRESETS`.
- `scripts/ui-contrast.mjs` — the WCAG gate. Parses both token files, merges the base `:root`
  into every preset (so an omitted token is checked at its *inherited* value, which is what
  the browser paints). 13 pairs per preset, plus all sixteen ANSI when `--term-bg` has
  relative luminance > 0.5. Run via `scripts/ui-lint.sh --contrast`.
- `docs/design-docs/ui/themes.md` — § "Adding a preset" is the 5-step checklist.

**Constraints:**

- The gate's pair list includes `--state-error` on `--sel` (the destructive row action's
  fill) and the whole state family on both `--surface` and `--surface-raised`. These are the
  pairs canonical palettes fail hardest — they were tuned for syntax highlighting, not for
  UI text on panel grounds.
- Light presets must re-value all sixteen ANSI slots, and the gate additionally holds them
  to 4.5:1 on their own `--term-bg` — which is the rule the three community light presets
  opt out of.
- Two places pin the preset id list literally and must be updated by hand:
  `test/unit/theme.test.ts:38` and `test/e2e/theme.spec.ts:542`. Everything else in the test
  suite is data-driven off the rendered picker and covers new presets for free.

**Measured baseline** (canon hexes against the gate's own rules) — the reason an exemption is
needed at all, and the record of exactly what is being waived:

| Palette | Failing pairs |
|---|---|
| Dracula | `--fg-subtle` 2.51, error/sel 2.91, error/raised 3.75 |
| Nord | subtle 2.42, error/surface 2.46, error/sel 1.80 |
| Gruvbox Dark | subtle 2.40, error/surface 3.37, error/sel 2.56 |
| Tokyo Night | subtle 1.99, error/sel 3.23 |
| Catppuccin Mocha | subtle 1.87, error/raised 3.94, error/sel 2.88 |
| One Dark | subtle 2.16, error/surface 4.38, error/sel 3.06 |
| Monokai (→ `neon`) | subtle 2.57, error/surface 3.93, error/sel 2.43 |
| Solarized Light | 13 pairs; green/cyan/orange ~2.6 on `--surface` |

## Approach

Follow `themes.md` § "Adding a preset" for all ten, plus one piece of UI work the checklist
does not cover (the picker outgrows a flat `<select>` at 17 entries).

**Presets.** Dark: `dracula`, `nord`, `gruvbox-dark`, `tokyo-night`, `catppuccin-mocha`,
`one-dark`, `neon`, `solarized-dark`, `github-dark`. Light: `solarized-light`,
`catppuccin-latte`, `github-light`. A palette shipping in both moods sits adjacent in the
picker rather than being sorted dark-then-light — the pair is what the user looks for.

**Token mapping** from each source palette:

| Hive token | Source-palette role |
|---|---|
| `--bg` / `--surface` / `--surface-raised` | base / mantle-or-panel / lifted panel |
| `--border`, `--btn`, `--btn-border`, `--sel` | the palette's own chrome greys |
| `--fg` / `--fg-muted` / `--fg-subtle` | text / subtext / comment, at upstream values |
| `--accent` / `--on-accent` | the palette's signature hue, avoiding one already used by the state family; `--on-accent` is the preset's own darkest/lightest ground |
| `--state-running/-attention/-error/-info` | green / yellow-orange / red / blue, at upstream values |
| `--ansi-0…15` | the palette's own 16, verbatim |
| `--font-ui`, `--font-mono`, `--radius-*`, `--shadow-popover` | system stack + 4/6px radii, matching `native-*` |

Do **not** restate `--hover`, `--state-starting` or `--state-exited`: `tokens.css` declares
them as `var()` references that re-resolve against each preset's own `--sel` / `--fg-subtle`.
Do **not** invent per-preset type, spacing or motion scales — one scale for the whole app.

Each block opens with `--contrast-exempt: 1` and carries a comment naming its source and
licence. The section header comment states the trade-off once, for all twelve.

**Why exempt rather than correct.** The first draft corrected every palette until it passed,
the way `native-light` corrects VS Code Light+. Reversed on the owner's call: these presets
are opt-in, and someone who picks "Dracula" wants Dracula, not a re-fitted approximation of
it. The accessibility cost is real, so the exemption is contained rather than blanket — it
is declared per-preset in the CSS (`--contrast-exempt: 1`), printed on every gate run with a
count, and **refused** on the six presets a user can reach without opting in (`NEVER_EXEMPT`
in `ui-contrast.mjs`). The alternative shapes were both worse: a hardcoded id list in the
gate duplicates `PRESETS` across two languages with nothing keeping them in sync, and
dropping the pairs from `PAIRS` would have disabled them for the default presets too.

**Why `neon`.** "Monokai" is an active Monokai Pro trademark and "Sublime" is Sublime HQ's.
The palette (Wimer Hazenberg's 2006 TextMate theme) is reproduced everywhere; only the name
carries risk.

**Picker grouping.** `<optgroup>` is native, keyboard- and AT-friendly, and about eight
lines in `selectInput`. No custom dropdown, no new component.

### Files to change

- `cmd/hivegui/frontend/src/theme/themes.css` — twelve new `:root[data-theme="…"]` blocks, each
  re-valuing every token including ANSI 16, under a `Community presets` header comment. The
  bulk of the diff.
- `scripts/ui-contrast.mjs` — honour `--contrast-exempt` (read from the preset's own
  declarations, not the base-merged view), refuse it on `NEVER_EXEMPT`, and report the
  opt-out count in the summary line.
- `cmd/hivegui/frontend/src/theme/theme.ts` — extend `ThemeName`; add twelve `PRESETS` entries;
  add a `group` field to `Preset` and `export const GROUPS = ['Hive','Native','Community']`;
  tag existing presets (`system`/`hive-*` → Hive, `native-*`/`terminal`/`classic` → Native,
  the twelve new → Community).
- `cmd/hivegui/frontend/index.html` — add the twelve ids to the `KNOWN` array in the pre-paint
  script.
- `cmd/hivegui/frontend/src/ui/field.ts` — optional `group?: string` on `selectInput`'s
  option type; when any option carries one, render `<optgroup label>` buckets in first-seen
  group order. Ungrouped callers keep flat rendering.
- `cmd/hivegui/frontend/src/app/modals/settings.ts` — pass `group: p.group` through.
- `cmd/hivegui/frontend/test/unit/theme.test.ts` — update the pinned id list; add the group
  invariant test.
- `cmd/hivegui/frontend/test/e2e/theme.spec.ts` — update the pinned option-value list (~:542).
- `docs/design-docs/ui/themes.md` — ten table rows, a new `## Attribution` section, and a
  note in "Adding a preset" about the `group` field.
- `.changesets/<pr>.md` — new, via `/hs-changelog-update`.

### New files

None (beyond the changeset).

### Tests

Most coverage is already data-driven off the rendered picker and comes free.

- `test/unit/theme.test.ts` › `PRESETS › lists every selectable theme exactly once, System
  first` — **update**: the 17-entry id list in picker order, no duplicates.
- `test/unit/theme.test.ts` › `PRESETS › every preset declares a group listed in GROUPS` —
  **new**: no preset silently falls out of the picker from a typo'd group.
- `test/unit/theme.test.ts` › `PRESETS › every non-system preset resolves to itself` —
  existing, covers the twelve new ids for free.
- `test/unit/field.test.ts` (or wherever `selectInput` is covered) › `selectInput › groups
  options into optgroups` and `› renders flat when no option has a group` — **new**.
- `test/e2e/theme.spec.ts` › `the preset list is exactly what theme.ts exports` — **update**:
  option values across optgroups.
- `test/e2e/theme.spec.ts` › `the boot script knows every preset theme.ts stamps` — existing,
  proves `KNOWN` covers all ten.
- `test/e2e/theme.spec.ts` › `every preset paints its own tokens and its own ANSI 16` —
  existing, proves each block is reached, declares all sixteen, paints a distinct ground, and
  reaches xterm's cached palette.
- `scripts/ui-lint.sh --contrast` — existing gate; must report 0 failures, 12 opted out.
- `test/e2e/theme.spec.ts` › `<preset> gives terminals a palette readable on its own ground`
  — loops the FIRST-PARTY light presets only; comment updated to say so and why, since it
  previously claimed to cover every light preset.

## Decision log

- **2026-09-01** — ~~Correct the palettes to pass the WCAG gate rather than allowlist them
  out of it.~~ **Reversed the same day** by the owner, after reviewing the measured failures:
  ship the palettes at their upstream values and let them opt out of the gate. Why: they are
  opt-in, and fidelity to the palette the user came for is the reason to ship it at all.
- **2026-09-01** — Express the opt-out as a `--contrast-exempt: 1` declaration in each CSS
  block rather than an id list inside `ui-contrast.mjs`. Why: the gate is Node and the preset
  registry is TypeScript, so an id list would be a third copy of the preset names with
  nothing keeping it in sync (the repo already pays for the two copies it has). The CSS is
  the thing being checked, so it is also the right place to say "do not check this". Read
  from the preset's own declarations, never the base-merged view — otherwise one line in
  tokens.css would switch the gate off for everything at once.
- **2026-09-01** — Pin `NEVER_EXEMPT` (the six default presets) rather than pinning the
  exempt list. Why: pinning the *protected* set fails closed — a new community preset needs
  no edit there, and a default preset that tries to opt out fails the gate loudly.
- **2026-09-01** — Ship the Monokai palette as `neon`. Why: "Monokai" is an active Monokai
  Pro trademark and "Sublime" is Sublime HQ's; the palette itself is not the risk, the name is.
- **2026-09-01** — Add `<optgroup>` support to `selectInput` rather than a custom dropdown.
  Why: 17 flat entries is a scanning problem, and `<optgroup>` is native, accessible, and
  ~8 lines against a component that has no other callers needing it.
- **2026-09-01** — Ship the three light presets without their dark counterparts
  (`solarized-dark`, `github-dark`). Why: keeps the diff to the agreed scope; each is one
  block plus one line to add later.
- **2026-09-01** — Skip regenerating the `HIVE_SNAPSHOT` screenshot baselines. Why: 10
  presets × 4 scenes = 40 darwin-local PNGs that CI skips by default and that nothing would
  then guard; the manual walkthrough in Verification is the real check.

## Progress

- **2026-09-01** — Plan-first scaffold; stage = IMPLEMENT (set in spec frontmatter).
- **2026-09-01** — Contrast strategy reversed mid-implementation (see Decision log); the
  fitted-palette work was discarded before it reached the tree.
- **2026-09-01** — All ten presets, the picker grouping, the gate opt-out, tests and docs
  implemented. `ui-lint.sh --contrast` 0 failures / 10 opted out; typecheck, `biome ci`,
  unit + dom (380 tests) and the theme e2e suite (31 passed) all green.

## Open questions

- None.

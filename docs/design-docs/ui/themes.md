# Themes

A theme is a full set of values for every token in [tokens.md](tokens.md). Nothing else changes between themes — no per-theme selectors in component CSS.

## Presets

Shipped in `src/theme/themes.css` as `:root[data-theme="<name>"] { … }` blocks. Mocked in [mocks/identity-options.html](mocks/identity-options.html).

| Name | Basis | Notes |
|---|---|---|
| `hive-dark` | Option B | Cool-biased near-black, Plex Sans + JetBrains Mono, amber accent. What `system` resolves to in dark mode |
| `hive-light` | Option B inverted by hand | Same hue bias, same fonts. Ground `#f4f4f7`, surface `#ffffff`, fg `#1a1b22`, accent `#c47a12` (darkened for contrast on white), `--on-accent #1a1b22` (white on that accent is only 3.42:1), attention `#a35f0d`, running `#177a53`, error `#bd3030`, info `#1163a8` (all four measured >= 4.5:1 on `--surface`, `--surface-raised` and, for error, `--sel` — the mock's lighter `#d9731a`/`#1f9d6a` were 3.27:1 and 3.45:1, fine for icon fills and not for the words they now colour). Not a naive inversion: borders lighten, shadows soften, accent darkens |
| `native-dark` | Option A | Lifted greys, system font, filled selection, 6px radii. Ground `#1e1e1f`, surface `#252527`, accent `#e6a23c`. ANSI is the VS Code Dark+ set |
| `native-light` | Option A inverted | Ground `#ffffff`, surface `#f3f3f3`, fg `#1c1c1e`, accent `#a35f0d` (the mock's `#e6a23c` gives white-on-accent 1.9:1). ANSI is VS Code Light+ **with every hue darkened until it clears 4.5:1 on white** — Light+ itself puts seven of sixteen under AA there |
| `terminal` | Option C | Monochrome, all `--font-mono`, radius 0, `--accent == --state-attention`; `--fg-subtle` raised to `#6f6f6f` (mock's `#5c5c5c` is 2.96:1). ANSI is near-monochrome but keeps a desaturated red at 1/9: program output that says "error" in colour must still read as error |
| `classic` | v2.4.0 values | Pure black, amber everywhere, system font. Exists so migration step 1 is visually a no-op and so users who liked it keep it |

### Community presets

Ports of the palettes people arrive with from other editors, added in spec 305. They live in the same file under a `Community` heading and follow every structural rule above — every token re-valued, all sixteen ANSI slots, no per-preset type/space/motion scales.

What they do **not** do is meet the contrast bar. They are ported at their published values, so hues, ANSI 16 and chrome greys are upstream's, not re-derived; several fall below WCAG AA on grounds this app paints text on. Each block therefore declares `--contrast-exempt: 1` and `ui-contrast.mjs` reports it as skipped rather than pretending it passed. This is a deliberate trade — a user who picks "Dracula" wants Dracula, and these are opt-in — but it is a real accessibility cost, and the six presets above (the only ones a user can land on *without* opting in) stay strictly gated. `ui-contrast.mjs` refuses the marker on those six by name.

| Name | Basis | Notes |
|---|---|---|
| `dracula` | Dracula | `#21222c`/`#282a36` grounds, purple accent (pink is spoken for by `--state-error`). Dracula's own published ANSI |
| `nord` | Nord | Polar Night grounds, Snow Storm text, Frost accent, Aurora states. Nord's ANSI maps the bright row to the same eight hues as the normal row — upstream's choice, kept |
| `gruvbox-dark` | Gruvbox dark | `bg0`/`bg0_s`/`bg1` grounds, the bright Aurora-equivalents for the states |
| `tokyo-night` | Tokyo Night | `#1a1b26`/`#24283b`, the theme's blue selection, and its distinct bright ANSI row |
| `catppuccin-mocha` | Catppuccin Mocha | `base`/`surface0` grounds, `text`/`subtext1`/`overlay1` type ramp, mauve accent |
| `one-dark` | Atom One Dark | `--fg` is Atom's UI text `#d7dae0`, not the syntax foreground `#abb2bf` — that is the muted step here, the same chrome/code split Atom makes |
| `neon` | Monokai (classic) | Wimer Hazenberg's 2006 TextMate palette. **Named `neon` on purpose**: "Monokai" is an active Monokai Pro trademark and "Sublime" is Sublime HQ's. Purple accent; green/orange/pink/cyan are all taken by the state family |
| `solarized-dark` | Solarized Dark | `base03`/`base02` grounds, `base1`/`base0`/`base01` type, the same accent eight as its light sibling |
| `solarized-light` | Solarized Light | `base3`/`base2` grounds, `base01`/`base00` type. Its bright ANSI row is Solarized's greys by design, which makes it the preset furthest from the AA bar on its own `#fdf6e3` ground |
| `catppuccin-latte` | Catppuccin Latte | `mantle`/`base`/white grounds so a row card genuinely lifts off the panel |
| `github-dark` | GitHub Primer (dark) | Primer's dark canvas grounds and `fg` ramp, `accent.emphasis` blue, Primer's terminal ANSI |
| `github-light` | GitHub Primer | Primer canvas grounds, `fg.default`/`fg.muted`/`fg.subtle` ramp, `accent.emphasis` blue, Primer's terminal ANSI |

Presets that come in both moods (`solarized-*`, `catppuccin-*`, `github-*`) sit next to each other in the picker rather than being sorted dark-then-light — the pair is what the user is looking for.

Selection: Settings → Appearance → Theme (dropdown, grouped into `Hive` / `Native` / `Community` via `<optgroup>`) with "System" mapping to `hive-dark`/`hive-light` via `prefers-color-scheme`. **`system` is the default** since v2.5.0 — an absent or unrecognised `hive.theme` resolves the same way an explicit `system` does. Anyone who already picked a preset keeps it.

### Attribution

Every community palette is MIT-licensed and used as a set of colour values; none of the upstream code ships here.

| Preset | Source | Author |
|---|---|---|
| `dracula` | [Dracula](https://draculatheme.com) | Zeno Rocha and contributors |
| `nord` | [Nord](https://www.nordtheme.com) | Arctic Ice Studio / Sven Greb |
| `gruvbox-dark` | [gruvbox](https://github.com/morhetz/gruvbox) | Pavel Pertsev |
| `tokyo-night` | [Tokyo Night](https://github.com/enkia/tokyo-night-vscode-theme) | Enkia |
| `catppuccin-mocha`, `catppuccin-latte` | [Catppuccin](https://catppuccin.com) | Catppuccin org |
| `one-dark` | Atom One Dark | GitHub / Atom contributors |
| `solarized-dark`, `solarized-light` | [Solarized](https://ethanschoonover.com/solarized/) | Ethan Schoonover |
| `github-dark`, `github-light` | [Primer](https://primer.style) | GitHub |
| `neon` | Monokai (2006 TextMate theme) | Wimer Hazenberg. Shipped under a different name — see the table above |

Contrast: every preset in the first table must pass WCAG AA for `--fg` on `--surface` (≥4.5:1) and `--fg-muted` on `--surface` (≥4.5:1); `--fg-subtle` is decorative and only needs ≥3:1; `--fg` on `--bg` and `--term-fg` on `--term-bg` are ≥4.5:1; and `--on-accent` on `--accent` must be ≥4.5:1 — a primary button must read its own label. The four state hues (`--state-running`, `--state-attention`, `--state-error`, `--state-info`) are checked at ≥4.5:1 on `--surface`: they are not icon-only fills, they colour real text in the worktree browser, the merged badge, the destructive action and the version-mismatch hint.

On a preset whose `--term-bg` is a **light** ground, all sixteen `--ansi-*` are checked at ≥4.5:1 against it too. Dark grounds are exempt because ANSI 0 is meant to disappear into the background there, which is what every terminal does; on a light ground that same slot is the darkest colour and the rule bites where it should.

`scripts/ui-lint.sh --contrast` is the gate (add `--verbose` for every ratio); it runs on the Linux CI leg, since token values are platform-independent.

A preset can opt out by declaring `--contrast-exempt: 1`, which is how the community ports above ship at their upstream values. The gate prints a `skip` line and a count for each, so an exemption is visible in every run rather than silent. It is not a general escape hatch:

- The six default presets are named in `NEVER_EXEMPT` and the marker is a *failure* on them — a preset a user can land on without opting in must clear the bar.
- **Exempt from the ratios is not exempt from being a complete preset.** An exempt block must still declare every token the pair list names, in its own declarations. Without that rule the exemption would delete the only check that catches a partial preset: the base block is merged in, so an omitted token reads back as `hive-dark`'s value through the cascade, and every other check — including the e2e "paints its own tokens" test — sees a plausible colour.
- The marker is read from the preset's own declarations, never the base-merged view, so one line in `tokens.css` cannot switch the gate off for everything at once.

`NEVER_EXEMPT` names today's defaults, so it does not protect a *future* first-party preset from exempting itself. That is accepted rather than solved — see the comment on `NEVER_EXEMPT` for why, and **if you are adding a first-party preset, it must pass the gate; do not reach for the marker.**

Fixtures: `scripts/testdata/ui-contrast/exempt-default.css` (refusal branch) and `exempt-incomplete.css` (completeness branch) must each fail; `good.css` carries the positive case, an exempt preset whose every pair is far below threshold and which must still pass. They are separate files on purpose — folded into `bad.css` they would prove nothing, since it already exits 1 for dozens of other reasons.

## User overrides

Settings → Appearance → "Custom tokens" textarea. Content is CSS declarations only (e.g. `--accent: #7aa2f7; --font-mono: "Berkeley Mono";`). Persisted in `localStorage['hive.themeOverrides']`; preset name in `localStorage['hive.theme']` (same store as `hive.fontSize` and the view key today).

Applied at boot and on every keystroke as a `<style id="theme-overrides">:root:root { … }</style>` declared in `index.html` after `themes.css`. Order is what makes overrides last, but order alone is not enough: every preset block is `:root[data-theme="…"]` (0,2,0), which outranks a plain `:root` (0,1,0) — `:root:root` ties the specificity and wins on order. Input is sanitised to `--[a-z0-9-]+: <value>;` lines (`sanitizeOverrides` in `theme.ts`); anything else is dropped and reported in the Settings error slot.

Overrides are sanitised **on write** and stored as finished CSS, so `index.html`'s pre-paint boot script does not carry a second copy of the sanitiser. It is not blind, though: the store is hand-editable and the script writes straight into a `<style>`, so it shape-checks what it reads (declarations only; no block, tag or at-rule characters; no network-reaching function) and injects nothing that fails. `theme.ts` then re-runs the real sanitiser on import.

Values are filtered by an **allowlist of CSS functions** (`var`, `calc`, `min`/`max`/`clamp`, the colour functions, `color-mix`), not a denylist of `url()`: `image-set()` reaches the network just as `url()` does, and so would the next function anyone adds. Unbalanced parentheses are rejected rather than emitted — an open `(` swallows the terminating `;`, every later declaration and the closing brace, so one half-typed value would silently wipe the whole block while reporting nothing.

Appearance applies as you change it and is remembered; Cancel does not revert it. The agent list in the same dialog is a transactional draft, but a theme has no round-trip and nothing to validate — and a preview you cannot see is not a picker.

Fonts named in overrides must already be installed on the OS — Hive doesn't fetch fonts.

## Terminal (xterm.js) mapping

The xterm `theme` object is rebuilt from tokens whenever the theme changes:

| xterm key | Token |
|---|---|
| `background` | `--term-bg` |
| `foreground` | `--term-fg` |
| `cursor`, `cursorAccent` | `--accent`, `--on-accent` |
| `selectionBackground` | `--accent` at 30% alpha, written as an 8-digit hex (`#rrggbb` + `4d`). Not `color-mix()`: `getPropertyValue` returns that function unresolved and xterm cannot parse it, but it does accept hex-alpha. A non-7-char `--accent` is passed through unchanged. |
| ANSI 0–15 | `--ansi-0 … --ansi-15`, positionally (SGR 30–37 then 90–97). A slot the preset leaves unset is omitted from the theme object rather than sent as `''`, so xterm keeps its own default. |
| `fontFamily` | `--font-mono` |

Read via `getComputedStyle(document.documentElement).getPropertyValue(...)` once per theme change, then `term.options.theme = …` and `term.options.fontFamily = …` on every open terminal. Not per frame — `getComputedStyle` is a layout read.

## Adding a preset

1. Copy the `hive-dark` block in `themes.css`, rename, re-value every token (no partial presets — missing tokens fall through to `hive-dark` silently and look broken in light).
2. Add ANSI 16 (`--ansi-0 … --ansi-15`). A preset on a light ground **must** re-value all sixteen: the defaults are xterm's Tango palette, and seven of those fail WCAG AA on white — `brightWhite` lands at 1.16:1, i.e. invisible. `test/e2e/theme.spec.ts` computes the contrast rather than pinning hexes, so a new light preset is checked by the same rule.
2b. Add the preset to `PRESETS` in `src/theme/theme.ts` **and** to the duplicated list in `index.html`'s pre-paint boot script. The boot script cannot import the module (a deferred module script would paint the wrong preset first), so the two lists are kept honest by `test/e2e/theme.spec.ts`. The `PRESETS` entry needs a `group` from `GROUPS` — the picker buckets consecutive runs of the same group into an `<optgroup>`, so keep entries for one group contiguous.
3. Run `scripts/ui-lint.sh --contrast`. A first-party preset must pass it. A community port keeping its upstream values declares `--contrast-exempt: 1` instead — and then belongs in the Community table, not the one above it.
4. Nothing to do for screenshot baselines except generate them: `test/e2e/theme.spec.ts` loops over `PRESETS`, so the new preset already has sidebar, dialog, worktrees and launcher tests. Run `HIVE_SNAPSHOT=1 npx playwright test test/e2e/theme.spec.ts --update-snapshots` on macOS and read the four new PNGs — they are darwin-local and default-skipped, which is why they must be eyeballed rather than trusted.
5. Add a row to the table above.

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

Selection: Settings → Appearance → Theme (dropdown) with "System" mapping to `hive-dark`/`hive-light` via `prefers-color-scheme`. **`system` is the default** since v2.5.0 — an absent or unrecognised `hive.theme` resolves the same way an explicit `system` does. Anyone who already picked a preset keeps it.

Contrast: every preset must pass WCAG AA for `--fg` on `--surface` (≥4.5:1) and `--fg-muted` on `--surface` (≥4.5:1); `--fg-subtle` is decorative and only needs ≥3:1; `--fg` on `--bg` and `--term-fg` on `--term-bg` are ≥4.5:1; and `--on-accent` on `--accent` must be ≥4.5:1 — a primary button must read its own label. The four state hues (`--state-running`, `--state-attention`, `--state-error`, `--state-info`) are checked at ≥4.5:1 on `--surface`: they are not icon-only fills, they colour real text in the worktree browser, the merged badge, the destructive action and the version-mismatch hint.

On a preset whose `--term-bg` is a **light** ground, all sixteen `--ansi-*` are checked at ≥4.5:1 against it too. Dark grounds are exempt because ANSI 0 is meant to disappear into the background there, which is what every terminal does; on a light ground that same slot is the darkest colour and the rule bites where it should.

`scripts/ui-lint.sh --contrast` is the gate (add `--verbose` for every ratio); it runs on the Linux CI leg, since token values are platform-independent.

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
2b. Add the preset to `PRESETS` in `src/theme/theme.ts` **and** to the duplicated list in `index.html`'s pre-paint boot script. The boot script cannot import the module (a deferred module script would paint the wrong preset first), so the two lists are kept honest by `test/e2e/theme.spec.ts`.
3. Run `scripts/ui-lint.sh --contrast`.
4. Nothing to do for screenshot baselines except generate them: `test/e2e/theme.spec.ts` loops over `PRESETS`, so the new preset already has sidebar, dialog, worktrees and launcher tests. Run `HIVE_SNAPSHOT=1 npx playwright test test/e2e/theme.spec.ts --update-snapshots` on macOS and read the four new PNGs — they are darwin-local and default-skipped, which is why they must be eyeballed rather than trusted.
5. Add a row to the table above.

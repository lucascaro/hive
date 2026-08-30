# Themes

A theme is a full set of values for every token in [tokens.md](tokens.md). Nothing else changes between themes — no per-theme selectors in component CSS.

## Presets

Shipped in `src/theme/themes.css` as `:root[data-theme="<name>"] { … }` blocks. Mocked in [mocks/identity-options.html](mocks/identity-options.html).

| Name | Basis | Notes |
|---|---|---|
| `hive-dark` | Option B | **Default.** Cool-biased near-black, Plex Sans + JetBrains Mono, amber accent |
| `hive-light` | Option B inverted by hand | Same hue bias, same fonts. Ground `#f4f4f7`, surface `#ffffff`, fg `#1a1b22`, accent `#c47a12` (darkened for contrast on white), attention `#d9731a`, running `#1f9d6a`, error `#d64545`. Not a naive inversion: borders lighten, shadows soften, accent darkens |
| `native` | Option A | Lifted greys, system font, filled selection. Ships in dark and light variants (`native-dark`, `native-light`) |
| `terminal` | Option C | Monochrome, all `--font-mono`, radius 0, `--accent == --state-attention` |
| `classic` | v2.4.0 values | Pure black, amber everywhere, system font. Exists so migration step 1 is visually a no-op and so users who liked it keep it |

Selection: Settings → Appearance → Theme (dropdown) with "System" mapping to `hive-dark`/`hive-light` via `prefers-color-scheme`.

Contrast: every preset must pass WCAG AA for `--fg` on `--surface` (≥4.5:1) and `--fg-muted` on `--surface` (≥4.5:1); `--fg-subtle` is decorative and only needs ≥3:1. Check with the contrast script in `scripts/ui-lint.sh --contrast`.

## User overrides

Settings → Appearance → "Custom tokens" textarea. Content is CSS declarations only (e.g. `--accent: #7aa2f7; --font-mono: "Berkeley Mono";`). Persisted in `localStorage['hive.themeOverrides']`; preset name in `localStorage['hive.theme']` (same store as `hive.fontSize` and the view key today).

Applied at boot and on save as a `<style id="theme-overrides">:root { … }</style>` appended after `themes.css`, so overrides beat presets by cascade order, not specificity. Input is sanitised to `--[a-z0-9-]+:\s*[^;{}]+;` lines; anything else is dropped and reported in the Settings error slot.

Fonts named in overrides must already be installed on the OS — Hive doesn't fetch fonts.

## Terminal (xterm.js) mapping

The xterm `theme` object is rebuilt from tokens whenever the theme changes:

| xterm key | Token |
|---|---|
| `background` | `--term-bg` |
| `foreground` | `--term-fg` |
| `cursor`, `cursorAccent` | `--accent`, `--on-accent` |
| `selectionBackground` | `color-mix(in srgb, var(--accent) 30%, transparent)` |
| ANSI 0–15 | per-preset table in `themes.css` as `--ansi-0 … --ansi-15` |

Read via `getComputedStyle(document.documentElement).getPropertyValue(...)` once per theme change, then `term.options.theme = …` on every open terminal. Not per frame.

## Adding a preset

1. Copy the `hive-dark` block in `themes.css`, rename, re-value every token (no partial presets — missing tokens fall through to `hive-dark` silently and look broken in light).
2. Add ANSI 16.
3. Run `scripts/ui-lint.sh --contrast`.
4. Add a Playwright screenshot baseline (`e2e/theme.spec.ts`) for sidebar + dialog under the new preset.
5. Add a row to the table above.

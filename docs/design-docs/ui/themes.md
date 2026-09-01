# Themes

A theme is a full set of values for every token in [tokens.md](tokens.md). Nothing else changes between themes — no per-theme selectors in component CSS.

## Presets

Shipped in `src/theme/themes.css` as `:root[data-theme="<name>"] { … }` blocks. Mocked in [mocks/identity-options.html](mocks/identity-options.html).

| Name | Basis | Notes |
|---|---|---|
| `hive-dark` | Option B | **Default.** Cool-biased near-black, Plex Sans + JetBrains Mono, amber accent |
| `hive-light` | Option B inverted by hand | Same hue bias, same fonts. Ground `#f4f4f7`, surface `#ffffff`, fg `#1a1b22`, accent `#c47a12` (darkened for contrast on white), `--on-accent #1a1b22` (white on that accent is only 3.42:1), attention `#d9731a`, running `#1f9d6a`, error `#d64545`. Not a naive inversion: borders lighten, shadows soften, accent darkens |
| `native` | Option A | Lifted greys, system font, filled selection. Ships in dark and light variants (`native-dark`, `native-light`) |
| `terminal` | Option C | Monochrome, all `--font-mono`, radius 0, `--accent == --state-attention`; `--fg-subtle` raised to `#6f6f6f` (mock's `#5c5c5c` is 2.96:1) |
| `classic` | v2.4.0 values | Pure black, amber everywhere, system font. Exists so migration step 1 is visually a no-op and so users who liked it keep it |

Selection: Settings → Appearance → Theme (dropdown) with "System" mapping to `hive-dark`/`hive-light` via `prefers-color-scheme`.

Contrast: every preset must pass WCAG AA for `--fg` on `--surface` (≥4.5:1) and `--fg-muted` on `--surface` (≥4.5:1); `--fg-subtle` is decorative and only needs ≥3:1; `--on-accent` on `--accent` must be ≥4.5:1 (primary buttons). Check with the contrast script in `scripts/ui-lint.sh --contrast`.

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
| `selectionBackground` | `color-mix(in srgb, var(--accent) 30%, transparent)` |
| ANSI 0–15 | **not implemented.** Phase 6 — no `--ansi-*` token exists yet, so xterm keeps its own dark-tuned defaults under every preset. Under `hive-light` that leaves a program's explicit `white` near-invisible on a white ground; the light presets are not shippable as the default until this lands. |

Read via `getComputedStyle(document.documentElement).getPropertyValue(...)` once per theme change, then `term.options.theme = …` on every open terminal. Not per frame.

## Adding a preset

1. Copy the `hive-dark` block in `themes.css`, rename, re-value every token (no partial presets — missing tokens fall through to `hive-dark` silently and look broken in light).
2. Add ANSI 16 (`--ansi-0 … --ansi-15`). A preset on a light ground **must** re-value all sixteen: the defaults are xterm's Tango palette, and seven of those fail WCAG AA on white — `brightWhite` lands at 1.16:1, i.e. invisible. `test/e2e/theme.spec.ts` computes the contrast rather than pinning hexes, so a new light preset is checked by the same rule.
3. Run `scripts/ui-lint.sh --contrast`.
4. Add a Playwright screenshot baseline (`e2e/theme.spec.ts`) for sidebar + dialog under the new preset.
5. Add a row to the table above.

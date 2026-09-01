# Tokens

Semantic roles, defined once in `src/theme/tokens.css` (defaults = `hive-dark`) and re-valued per preset in `src/theme/themes.css`. Components reference roles only. Names describe *what the value is for*, never what it looks like (`--fg-muted`, not `--grey-400`).

## Colour

| Token | Role | hive-dark |
|---|---|---|
| `--bg` | App ground, terminal area chrome | `#0f1014` |
| `--surface` | Sidebar, bars, tray, dialog bodies | `#13141a` |
| `--surface-raised` | Project card body, launcher, popovers | `#0f1015` |
| `--border` | Every 1px rule | `#22242e` |
| `--fg` | Primary text | `#e6e7ee` |
| `--fg-muted` | Secondary text (subtitles, labels) | `#a5a8b8` |
| `--fg-subtle` | Tertiary (hints, version, disabled) | `#5f6273` |
| `--accent` | Brand mark, selection bar, primary button, focus ring | `#ffb454` |
| `--on-accent` | Text on `--accent` | `#15120a` |
| `--sel` | Selected row background | `#1c1e28` |
| `--hover` | Hover background for rows/buttons | `color-mix(in srgb, var(--sel) 60%, transparent)` |
| `--btn` | Default button/chip fill | `#1a1c25` |
| `--btn-border` | Default button/chip border | `#2b2e3b` |
| `--state-running` | Session alive & ready | `#5fd7a5` |
| `--state-attention` | Session waiting on the user | `#ff9f43` |
| `--state-starting` | Daemon phase ≠ ready | `--fg-subtle` |
| `--state-exited` | Exit 0 | `--fg-subtle` |
| `--state-error` | Exit ≠ 0, banners, destructive actions | `#ff6b6b` |
| `--term-bg` | xterm background | `#0b0c10` |
| `--term-fg` | xterm foreground | `#dfe1ea` |

Rules:
- `--accent` and `--state-attention` are **different hues on purpose** so "selected" never reads as "needs you". Presets may set them equal only if the preset is monochrome (`terminal`).
- Session/project colours (user-chosen swatches) are data, not tokens. They render via `--session-color` on the element, as today.
- Derived shades use `color-mix()` in components, not extra tokens. Cap: one derivation per use site.

## Typography

| Token | Value (hive-dark) |
|---|---|
| `--font-ui` | `"IBM Plex Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif` |
| `--font-mono` | `"JetBrains Mono", "SF Mono", Menlo, Consolas, monospace` |

Scale (5 steps, replaces 12 sizes):

| Token | px | Used for |
|---|---|---|
| `--text-xs` | 11 | Hints, version, keyboard hints, pills, project-card counts |
| `--text-sm` | 12 | Subtitles, chips, launcher items, uppercase labels |
| `--text-md` | 13 | Body: row names, buttons, dialog fields, status bar |
| `--text-lg` | 14 | Dialog section headings, banner text |
| `--text-xl` | 16 | Dialog titles, empty-state title |

Line-height: `1.4` UI, `1.5` terminal. Weights: 400 body, 500 emphasis/selected, 600 brand & dialog titles only. Uppercase labels use `letter-spacing: .08em` and `--text-xs`/`--text-sm`. Tabular numerals (`font-variant-numeric: tabular-nums`) on anything that counts.

Fonts are bundled as woff2 under `src/theme/fonts/` (Wails cannot hit a CDN): IBM Plex Sans 400/500/600 and JetBrains Mono 400/500/700, both SIL OFL 1.1, unmodified upstream releases. Provenance — exact release URLs and sha256 — is in `src/theme/fonts/README.md`, and the licence texts sit beside the files. `base.css` declares the `@font-face` rules; Vite hashes them into `dist/assets/` and `//go:embed all:frontend/dist` carries them into the binary on every platform (`cmd/hivegui/assets_test.go` asserts that).

Presets `native-dark`, `native-light` and `classic` set `--font-ui` to the system stack and load nothing. `terminal` sets both `--font-ui` and `--font-mono` to the mono stack.

`html, body` is the app's only `font-family` declaration and it reads `var(--font-ui)`; everything else inherits. A literal stack there would silently exclude every surface that sets no font of its own.

## Spacing

4px base. `--space-1: 4px` … `--space-6: 24px` (4, 8, 12, 16, 20, 24). Row heights: single-line 28px, two-line 40px, bars 36px. Sidebar min width 220px (was 200; project cards need the margin).

## Radius, elevation, motion

| Token | Value |
|---|---|
| `--radius-sm` | 4px (chips, pills, icon buttons) |
| `--radius-md` | 6px (project cards, dialogs, launcher) |
| `--shadow-popover` | `0 8px 24px rgba(0,0,0,.45)` (launcher, dialogs only) |
| `--motion-fast` | 120ms ease (layout, hover) |
| `--motion-pulse` | 1.6s ease-in-out infinite (attention only) |

`@media (prefers-reduced-motion: reduce)` sets both motion tokens to `0s`.

## Focus

`:focus-visible` → `outline: 2px solid var(--accent); outline-offset: 1px`. Nothing removes it without replacing it.

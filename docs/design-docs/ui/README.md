# Hive UI design system

The visual and interaction system for the Hive GUI (`cmd/hivegui/frontend/`). This directory is the source of truth for *how the UI looks and behaves*; `DESIGN.md` at the repo root covers architecture.

Status: **complete** — all six phases implemented, the last in PR #312 (v2.5.0). Changes to the UI now follow "How to change the UI from now on" below. The six phases: tokens and presets; the SVG icon sprite and primitives, with every Unicode UI glyph removed; the sidebar rebuilt on `sessionRow`/`projectCard`/`chip`; the chrome — banners, status bar, tile headers, launcher and palette rows, empty/boot/phase states — on `button`/`banner` and tokens; every modal on `dialog()` plus the form-field primitives, with Settings › Appearance shipping the preset picker and user token overrides; and finally the bundled webfonts, the remaining presets, the WCAG contrast gate, the flip to the OS colour scheme and the dissolution of `style.css` into `base.css` + `layout.css` + `components/*.css`. `ui-lint --strict` and `ui-lint --contrast` gate CI. Implementation is tracked in [docs/exec-plans/active/ui-design-system.md](../../exec-plans/active/ui-design-system.md).

## Why this exists

As of v2.4.0 `style.css` was 2159 lines with one custom property, 51 distinct hex colours, 12 font sizes (8–18px, including 10.5/11.5/12.5), no theming, and state glyphs pulled from five different Unicode blocks. Every screen was built piece by piece. This system replaces that with a small set of named decisions that every future change reads from.

## Principles

1. **The terminal is the product; chrome recedes.** Sidebar, bars and dialogs exist to route attention to the right terminal. They are quiet by default and loud only for *attention*.
2. **One channel per fact.** Session name, window title, and state each get exactly one place to live (name line, subtitle line, state icon). No fact is encoded twice, none is dropped.
3. **Values come from tokens, never from literals.** No hex, no px font-size, no Unicode glyph outside `tokens.css`, `themes.css`, and the icon sprite. Enforced by `scripts/ui-lint.sh` in CI.
4. **Theming is data.** A theme is a block of token values. Users can pick a preset or override any token. Light and dark are presets, not code paths.
5. **State is shape + colour + words.** Colour alone is never the only signal (colour-blind users, monochrome presets).
6. **Motion only where it carries meaning.** Attention pulses. Nothing else animates except layout transitions ≤120ms. `prefers-reduced-motion` disables all of it.

## Documents

| File | Contents |
|---|---|
| [tokens.md](tokens.md) | Semantic token roles: colour, type scale, spacing, radius, elevation, motion |
| [themes.md](themes.md) | Presets (`system` default, resolving to `hive-dark`/`hive-light`; plus `native-dark`, `native-light`, `terminal`, `classic`), user overrides, xterm mapping |
| [icons.md](icons.md) | SVG sprite inventory, state shapes, rules |
| [components.md](components.md) | Primitives in `src/ui/`: anatomy, states, tokens used |
| [patterns.md](patterns.md) | Cross-component behaviour: attention bubbling, selection vs attention, empty/error states, keyboard hints |
| [mocks/](mocks/) | The option comparisons the decisions were made from. Open in a browser. |

## Decisions made (2026-08-29)

| Question | Options considered | Decision | Mock |
|---|---|---|---|
| Visual identity | native-adjacent / own-brand dev-tool / terminal-first | **Own-brand palette (B)** as default; all three plus current ship as presets | [identity-options.html](mocks/identity-options.html) |
| Light theme | tokens-only / dark-only / both at launch | **Both at launch** | — |
| Sidebar row | compact / two-line / grouped cards | **Two-line rows inside project cards**; subtitle = window title | [sidebar-structure.html](mocks/sidebar-structure.html) |
| Iconography | geometric dots / SVG line set / mono glyphs | **SVG line set** for everything; **geometric shapes** for states | [state-icons.html](mocks/state-icons.html) |
| Enforcement | docs / docs+lint / docs+lint+components | **Docs + lint + component layer** | — |

## How to change the UI from now on

1. Read `components.md` for the primitive you're touching. If none fits, add one — don't hand-roll markup in a feature module.
2. Use tokens. If a token is missing, add it to `tokens.md` *and* `tokens.css` in the same PR, with a role name (not a colour name).
3. Need an icon? Add it to the sprite per `icons.md`. Never paste a Unicode symbol.
4. Run `scripts/ui-lint.sh` locally; CI runs it too.
5. For anything with a visual choice, put a mock in `mocks/` and record the decision in this README's table.

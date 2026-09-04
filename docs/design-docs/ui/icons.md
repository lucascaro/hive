# Icons

One inline SVG sprite, `src/lib/icons.svg` (injected into the document on first use by `ensureSprite()` in `src/lib/icon-sprite.ts`), rendered through `<Icon name>` from `src/components/Icon.tsx`. Mocked in [mocks/state-icons.html](mocks/state-icons.html).

Geometry: 24×24 viewBox, `stroke: currentColor`, `stroke-width: 1.75`, round caps/joins, `fill: none` unless noted. Rendered at 14px (rows, chips, tile headers) or 12px (inline in text). Colour always comes from the parent's `color`, set by a token.

## State icons (geometric, filled)

Shapes come from option I3; drawn as SVG so they align and render identically on every platform.

| Name | Shape | Fill | Meaning | Animates |
|---|---|---|---|---|
| `state-running` | ▶ triangle | `--state-running` | alive, phase ready, no attention | no |
| `state-attention` | ◆ diamond | `--state-attention` | agent waiting on the user | yes — `--motion-pulse` on a `box-shadow` ring |
| `state-starting` | ◌ dotted ring | stroke `--state-starting` | daemon phase ≠ ready (starting / fetching / worktree) | yes — rotate 1s linear |
| `state-exited` | ■ square | stroke `--state-exited` | process ended, no `last_error` | no |
| `state-error` | ✗ cross | stroke `--state-error` | process ended with `last_error` set | no |

Resolution from `SessionInfo`:

```
!isReady(phase)            → starting
!alive && !last_error       → exited
!alive                      → error   (no exit_code on the wire; last_error is the signal)
attention.has(id)           → attention
else                        → running
```

Implemented in `src/lib/session-state.ts`; the resolver accepts both the `last_error` and `lastError` spellings, since wire payloads are snake_case but the frontend sees both.

## Action / navigation icons (line)

`plus`, `minus` (minimize), `x` (close/kill), `rotate` (restart), `grid`, `single` (one pane), `branch` (worktree), `chevron-down`, `chevron-right`, `settings`, `search`, `help`, `arrow-left`, `arrow-right` (nav history), `external` (open in OS terminal), `download`, `check`.

That's 22 symbols. Adding one: draw it on the 24 grid at 1.75 stroke, add `<symbol id="…">` to the sprite, add a row here, never inline SVG in a feature module.

`settings` (gear) also doubles as the sidebar's edit-project control (the project card's `data-action="edit"` control) — the 22-icon inventory has no pencil.

## Rules

- **No Unicode glyphs as UI.** `×`, `＋`, `●`, `◐`, `✓`, `⎇`, `—` are all replaced. Text content that *is* text (`…`, `·` separators, keyboard hint characters like `⌘`) is fine. `scripts/ui-lint.sh` flags anything outside the allow-list in `src/app/**` and `index.html`.
- Icons are never the only label on a control: `IconButton` requires `aria-label`, and `title` is set from it.
- State icons appear in exactly three places: sidebar row, minimized chip, grid tile header. Same size, same resolution function.
- No emoji anywhere in the GUI.

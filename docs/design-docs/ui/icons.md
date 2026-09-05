# Icons

One inline SVG sprite, `src/lib/icons.svg` (injected into the document on first use by `ensureSprite()` in `src/lib/icon-sprite.ts`), rendered through `<Icon name>` from `src/components/Icon.tsx`. Mocked in [mocks/state-icons.html](mocks/state-icons.html).

Geometry: 24×24 viewBox, `stroke: currentColor`, `stroke-width: 1.75`, round caps/joins, `fill: none` unless noted. Rendered at 14px (rows, chips, tile headers) or 12px (inline in text). Colour always comes from the parent's `color`, set by a token.

## State icons (geometric, filled)

Shapes come from option I3; drawn as SVG so they align and render identically on every platform.

| Name | Shape | Fill | Meaning | Animates |
|---|---|---|---|---|
| `state-running` | ▶ triangle | `--state-running` | alive, phase ready, idle — nothing changing on screen | no |
| `state-working` | ⋯ three dots | `--state-running` | the session is producing output / mid-turn | yes — `--motion-pulse` on opacity |
| `state-attention` | ◆ diamond | `--state-attention` | agent waiting on the user (a bell, or a reported `waiting_input`) | yes — `--motion-pulse` on a `box-shadow` ring |
| `state-waiting-permission` | ? question | `--state-attention` | agent blocked on an explicit yes/no | yes — `--motion-pulse` on a `box-shadow` ring |
| `state-starting` | ◌ dotted ring | stroke `--state-starting` | daemon phase ≠ ready (starting / fetching / worktree) | yes — rotate 1s linear |
| `state-exited` | ■ square | stroke `--state-exited` | process ended, no `last_error` | no |
| `state-error` | ✗ cross | stroke `--state-error` | process ended with `last_error` set | no |

Resolution from `SessionInfo`:

```
!isReady(phase)               → starting
!alive && !last_error          → exited
!alive                         → error   (no exit_code on the wire; last_error is the signal)
state === waiting_permission   → waiting-permission
state === waiting_input        → attention
state === error                → error
needs_attention                → attention
state === working              → working
else                           → running
```

`state` is `SessionInfo.state` (`internal/wire/control.go` `State*`), owned by the daemon. Empty means idle — both the `omitempty` case and what a daemon predating spec 336 sends, so no branch here needs an "unknown".

The daemon derives it by sampling what the session's screen *renders*, not by watching bytes arrive: a measured idle Claude Code session emits an `ESC[?6n` cursor-position query every 200 ms, which changes no cell. Keying off byte arrival pinned every agent to `working` forever and let a redraw bury a bell within one tick. `waiting-permission` is what the hook and extension tiers report (spec 336 phases 2–3); `cmd/hived/hook.go` produces it from Claude's `PermissionRequest`, and `internal/agent/pi/hive.ts` from Pi's blocking `confirm`/`select` prompts.

Two rules in that order are deliberate. An agent-reported permission prompt outranks `needs_attention`, because "blocked on a yes/no" versus "rang the bell" is the distinction the state model exists to draw. An unacknowledged bell outranks `working`, because the one that wants a human is the one worth showing.

### The icon's words

The glyph carries the state; its `<title>` carries everything the glyph cannot. `stateTooltip()` (same module, so the two can never disagree) stacks, dropping any line whose field is empty:

```
Waiting for permission          ← STATE_WORDS[state]
“refactor the parser”           ← last_prompt, quoted: the user's own words read back
Needs to run rm -rf build/      ← last_summary
reported by the agent           ← state_source; heuristic reads "guessed from terminal output"
```

The tier line is always present, because it is what explains the uncertain ring to someone who hovered to ask about it. `hook` and `extension` deliberately collapse to one phrase: which transport the agent used to speak is not the user's business, only that it spoke rather than we inferred.

`state-working` and `state-waiting-permission` reuse `--state-running` and `--state-attention` rather than claiming tokens of their own: those two colours are already picked for all 18 themes, and shape plus motion carries the busy/idle and bell/permission difference without asking anyone to pick 18 more values.

Implemented in `src/lib/session-state.ts`; the resolver accepts both the `last_error` and `lastError` spellings, since wire payloads are snake_case but the frontend sees both.

## Action / navigation icons (line)

`plus`, `minus` (minimize), `x` (close/kill), `rotate` (restart), `grid`, `single` (one pane), `branch` (worktree), `chevron-down`, `chevron-right`, `settings`, `search`, `help`, `arrow-left`, `arrow-right` (nav history), `external` (open in OS terminal), `download`, `check`.

That's 24 symbols. Adding one: draw it on the 24 grid at 1.75 stroke, add `<symbol id="…">` to the sprite, add a row here, never inline SVG in a feature module.

`settings` (gear) also doubles as the sidebar's edit-project control (the project card's `data-action="edit"` control) — the 24-icon inventory has no pencil.

## Rules

- **No Unicode glyphs as UI.** `×`, `＋`, `●`, `◐`, `✓`, `⎇`, `—` are all replaced. Text content that *is* text (`…`, `·` separators, keyboard hint characters like `⌘`) is fine. `scripts/ui-lint.sh` flags anything outside the allow-list in `src/app/**` and `index.html`.
- Icons are never the only label on a control: `IconButton` requires `aria-label`, and `title` is set from it.
- State icons appear in exactly three places: sidebar row, minimized chip, grid tile header. Same size, same resolution function.
- No emoji anywhere in the GUI.

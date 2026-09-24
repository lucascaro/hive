# Patterns

Behaviour that spans components. When a component doc and this file disagree, this file wins.

## Selection vs attention

Two independent facts, two independent channels:

- **Selected** = the session you are looking at. An `--accent` fill with `--on-accent` ink for everything on the row (spec 455; it was `--sel` + a 2px accent bar, which sat too close to the ground and to hover to find at a glance). Exactly one session row is selected in single view; in grid view the focused tile's row is selected.
- **Attention** = a session wants you. `state-attention` icon (diamond, pulsing) + name in `--state-attention` + a background tint that **pulses**. Many rows may have it.

They do not rely on colour to stay apart: `--accent` and `--state-attention` are the same hue or close to it on several presets (`hive-dark`, `classic`, `native-light`, `alucard`, …). Selection is a static FILL; attention is MOTION (the pulse) plus the diamond icon's shape.

Selection owns the row's *static* background. Attention never paints a static background — but it may pulse one, which is a channel selection does not use. On an unselected row the pulse is `--state-attention` at 12%. On the SELECTED row an attention tint would vanish into the fill wherever the two hues meet, so the pulse there is `--on-accent` at 18% — the fill rhythmically shifts toward the ink — and the state icon keeps its diamond and its attention colour on a `--surface` disc. Under `prefers-reduced-motion` the pulses become static tints (9% attention, 12% ink); an unselected attention row's tint must stay quieter than the selection, which the theme tests assert, and the selected row's cue is asserted in `sidebar-legibility.spec.ts`.

A third fact, added by spec 384:

- **Shared worktree** = this session and at least one other are editing the same files. The members indent behind a 1px `--session-color` rail on the **left** (spec 455; it was a bar on the panel's right edge), under a header that names the branch and a count in the group's colour. Many rows may have it, in several distinct groups.

The left edge became free when selection moved from a left bar to a fill. Colour alone never carries the group either — the indent is positional, and the count and the branch name say it in words, which is what makes the cue survive a monochrome preset and a colourblind reader.

## One order

Shared-worktree sessions paint as a block, anchored at the group's lowest-order member. That makes the painted order differ from the daemon's flat `r.order`, and **the painted order is the only one any feature may use**: `app/selectors.ts` `orderedSessions()` clusters, and the sidebar, ⌘1-9, ⌘↑/⌘↓, the tray and the command palette all read it. Resolving anything against raw `.order` puts navigation in a different order from the rows on screen, which is the bug this rule exists to prevent (spec 384). Reorder targets are computed as a painted ARRAY and turned into daemon moves by `lib/worktree-groups.ts`; nothing does index arithmetic in two spaces.

Reordering a grouped session has two gestures, because there are two things to move:

- **Within the group** — drop a member on another member, or press ⇧⌘↑/⇧⌘↓ while it still has room among its own members.
- **The whole group** — drop a member anywhere outside its group, or press ⇧⌘↑/⇧⌘↓ once the member is at the group's edge.

A member never leaves its group: membership is which worktree it runs in, not where it sits.

## Drag to reorder

Every list that reorders by drag — sidebar sessions, project cards, Settings' pinned agents — goes through one implementation. A row carries `data-drag-row` and spreads `dragRowProps({ mime, id, onCommit, cancelFrom?, aboveOf? })` from `src/lib/drag-row.ts`; the dashed drop placeholder and the dragged row's `display: none` come from `src/lib/drag-placeholder.ts` and `theme/components/drag.css`. The list must be a `<ul>`/`<ol>` (the placeholder is an `<li>`), and only rows that may be dropped among carry `data-drag-row` — a drop resolves against its nearest `data-drag-row` neighbours. A drag bubbling up from a nested drag row (a session inside its project card) is ignored by the outer row, never `preventDefault`-ed, which would cancel the inner drag.

## Attention bubbling

Attention on a session propagates *up* to every container that can hide it: project card header (swatch ring), collapsed project ("k need you" count), minimized session chip, minimized project chip (state icon + "k" alert count, and the label colour / dot pulse). The collapsed card and the minimized chip both derive their number from one helper, `attentionSummary()` in `lib/session-state.ts`, so the two cannot disagree; it resolves through `sessionState()`, which means a session that is still starting or already gone stops bubbling even if its last-known `needs_attention` flag was set. It propagates *nowhere else* — no window-level flashing, no dock badge beyond what `internal/notify` already does. Clearing: attention clears when the session receives input or is selected, as today; every bubbled indicator clears with it in the same render.

## Exited sessions

Stay in the list, dimmed with strike-through, until the user kills them. Rationale: an agent that exited holds scrollback the user may want; hiding it loses the "why did it stop" trail. Row hover shows `rotate` (restart) first, `x` second. Error exits use `--state-error` on the icon only — the name stays `--fg-subtle`, so a column of failures doesn't turn the sidebar red.

## Hover-revealed actions

Actions on rows, card headers and tile headers are hidden until hover or keyboard focus within the row. They replace the meta column (worktree/agent) rather than pushing text. Every hover action has a keyboard equivalent listed in the help overlay; hover is a shortcut, not the only path.

## Empty and loading states

- **No projects:** centred empty state in the terminal area — title `--text-xl`, one-line hint, one primary `button` ("New project ⌘N"). No illustration.
- **No sessions in a project:** card body shows one ghost row "New session…" (`--fg-subtle`, `plus` icon) that opens the launcher.
- **Session starting:** terminal area shows the existing phase checklist, restyled: steps use `icon(check)` / `state-starting` / `--fg-subtle` dot; no Unicode.
- **Daemon unreachable:** `banner` kind `error` with "Restart Hive" primary action; terminal area keeps last content dimmed to 50%.
- **Daemon build differs:** raise the `banner` only when the daemon *contracts* differ — "Restart Hive", behind the confirm overlay, because every session ends. Matching contracts mean the two are compatible, and a differing build is then unactionable (reloading cannot change which build the daemon is), so it belongs in the sidebar footer's two-line readout, not in a banner. General rule: a banner is for a state the user must act on; a fact they cannot act on goes in the footer.

## Errors

Errors are sentences: what failed, and what to do. `flash()` errors go to the status bar for 6s (`--state-error`); errors that block a dialog go in the dialog's error slot under the field that caused them. Never both. No toasts.

## Keyboard hints

`AGENTS.md` › UX Best Practices requires the key shown next to the action it triggers. That rule stands; this system only fixes *how* hints render:

- One primitive, `kbd(text)` → `<kbd class="hv-kbd">`, `--font-mono --text-xs --fg-subtle`, no border, no fill. Feature modules never format hints by hand.
- Format is uniform: `[1]` for digits/symbols, `(n)` for letters — exactly as `AGENTS.md` says. Modifier symbols on macOS (`⌘⇧`), words elsewhere (`Ctrl+Shift`), from `lib/platform.ts`.
- Placement: session rows show `[n]` (⌘1–9 bind to sessions in global order; there is no project chord); overlay footers keep their `[esc] close · (r) refresh` line via `kbd`; the status bar right slot shows the current mode's top 1–2 shortcuts; the help overlay lists all.
- Hints are never the only label and never carry colour.

## Density

One density. Two-line rows cost ~6 rows per screen versus today; accepted because the subtitle is the feature. If a "compact" mode is requested later it is a preset that sets `--row-h: 28px` and hides line 2 — a token, not a code path.

## Motion

Allowed: sidebar width transition (120ms), attention pulse, starting spinner, hover background (120ms). Disallowed: dialog slide/scale, list item enter/exit animation, anything on the terminal host. All motion reads `--motion-*` tokens and is off under `prefers-reduced-motion`.

## Platform

Window chrome is the OS's (Wails default title bar; no custom chrome). Nothing in the layout assumes an inset for traffic lights.

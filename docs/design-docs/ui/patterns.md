# Patterns

Behaviour that spans components. When a component doc and this file disagree, this file wins.

## Selection vs attention

Two independent facts, two independent channels:

- **Selected** = the session you are looking at. `--sel` background + 2px `--accent` bar. Exactly one session row is selected in single view; in grid view the focused tile's row is selected.
- **Attention** = a session wants you. `state-attention` icon (diamond, pulsing) + name in `--state-attention`. Many rows may have it.

They never share a colour (`--accent` ≠ `--state-attention` in every non-monochrome preset) and never share a position (bar at left edge vs icon in the state column).

## Attention bubbling

Attention on a session propagates *up* to every container that can hide it: project card header (swatch ring), collapsed project ("k need you" count), minimized session chip, minimized project chip. It propagates *nowhere else* — no window-level flashing, no dock badge beyond what `internal/notify` already does. Clearing: attention clears when the session receives input or is selected, as today; every bubbled indicator clears with it in the same render.

## Exited sessions

Stay in the list, dimmed with strike-through, until the user kills them. Rationale: an agent that exited holds scrollback the user may want; hiding it loses the "why did it stop" trail. Row hover shows `rotate` (restart) first, `x` second. Error exits use `--state-error` on the icon only — the name stays `--fg-subtle`, so a column of failures doesn't turn the sidebar red.

## Hover-revealed actions

Actions on rows, card headers and tile headers are hidden until hover or keyboard focus within the row. They replace the meta column (worktree/agent) rather than pushing text. Every hover action has a keyboard equivalent listed in the help overlay; hover is a shortcut, not the only path.

## Empty and loading states

- **No projects:** centred empty state in the terminal area — title `--text-xl`, one-line hint, one primary `button` ("New project ⌘N"). No illustration.
- **No sessions in a project:** card body shows one ghost row "New session…" (`--fg-subtle`, `plus` icon) that opens the launcher.
- **Session starting:** terminal area shows the existing phase checklist, restyled: steps use `icon(check)` / `state-starting` / `--fg-subtle` dot; no Unicode.
- **Daemon unreachable:** `banner` kind `error` with "Restart Hive" primary action; terminal area keeps last content dimmed to 50%.

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

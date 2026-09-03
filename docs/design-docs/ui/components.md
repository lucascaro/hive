# Components

Primitives live in two places while the [React rewrite](../../exec-plans/active/react-ui-rewrite.md) is in flight. `src/components/` holds the React ones (`.tsx`, one component per file) and is where new work goes; `src/ui/` holds the original plain-TypeScript DOM builders — a function returning an element plus, where the element has state, a small `updateX()` patch twin — and shrinks one phase at a time as each region is ported. A primitive that exists in both renders byte-identical markup; the React one has no patch twin, because props replace it. Feature modules (`src/app/*`, `src/components/*`) compose primitives; they do not create `button`/`li`/`div` with hand-written classes for anything listed here.

The signatures below are written in the imperative form, and each heading names the file(s) that implement it. Read a React port's props as the same fields: `sessionRow({ session, selected, … })` is `<SessionRow session={…} selected={…} … />`. A heading with no file marked is imperative-only and has not been ported yet.

Each primitive owns its CSS in `src/theme/components/<name>.css`. Class names are `hv-<name>` and `hv-<name>__<part>`; modifiers are data attributes (`data-state="attention"`, `data-selected`), not extra classes.

## `button({ label, kind?, icon?, onClick })` — both: `src/components/Button.tsx`, `src/ui/button.ts`

- `kind`: `default` | `primary` | `danger` | `ghost`.
- Anatomy: optional leading icon (14px) + label. Height 28px, padding `0 var(--space-3)`, `--text-md`, `--radius-sm`.
- Tokens: default = `--btn`/`--btn-border`/`--fg-muted`; primary = `--accent`/`--on-accent`; danger = transparent with `--state-error` text and border; ghost = no fill, no border.
- States: hover `--hover`, active darken 8%, disabled `opacity .5; pointer-events none`, focus-visible ring.

## `iconButton({ icon, label, onClick })` — both: `src/components/IconButton.tsx`, `src/ui/icon-button.ts`

- 24×24 (rows/bars) or 22×22 (sidebar header), icon 14px centred, `aria-label` required, `title` mirrored. Same fills as `button` kind `ghost` at rest, `default` on hover.

## `kbd(text)` — both: `src/components/Kbd.tsx`, `src/ui/kbd.ts`

- `<kbd class="hv-kbd">`, `--font-mono --text-xs --fg-subtle`. The only way to render a key hint. See patterns.md › Keyboard hints.

## `icon(name, { size? })` — both: `src/components/Icon.tsx`, `src/ui/icon.ts`

- Returns `<svg class="hv-icon"><use href="#hv-<name>"/></svg>`. Size 14 default, 12 inline.

## `stateIcon(state)` / `updateStateIcon(el, state)` — both: `StateIcon` in `src/components/Icon.tsx`, `src/ui/icon.ts`

- Wraps `icon()` for the five states, sets `data-state`, applies animation classes. Used by session row, chip, tile header only. The React port has no `updateStateIcon` twin — a new `state` prop is the update.

## `sessionRow({ session, selected, onSelect, onMinimize, onRestart, onKill })` — React, `src/components/SessionRow.tsx`

Decided in [mocks/sidebar-structure.html](mocks/sidebar-structure.html) (S2 inside S3).

- Height 40px. Grid: `[state 14px] [text 1fr] [meta auto]`.
- Line 1: name, `--text-md`, `--fg` when selected else `--fg-muted`; attention → `--state-attention` + weight 500; exited/error → `--fg-subtle` + `text-decoration: line-through` (see patterns.md for why not hidden).
- Line 2: window title (`session-title` today), `--text-sm`, `--fg-subtle`. Falls back to state words when no title: "Starting…", "Exited", "Exited — <last_error>". Never both.
- `kbd("[n]")` before the name when the session is one of the first nine in `orderedSessions()` (⌘1–9 bind to sessions, not projects).
- Meta column: worktree `branch` icon (12px, `--fg-subtle`) if session has a worktree; agent short code in `--font-mono --text-xs` (`cl`, `cx`, `gm`, `sh`, custom = first two letters).
- Selected: `--sel` background + 2px `--accent` bar at left edge (`::before`). Hover: `--hover` and reveals actions replacing the meta column — `minus` (minimize), `rotate` (restart, exited/error rows only), `x` (kill, via the native `Confirm()` bridge; `force: false` on a live session so the daemon's dirty-worktree refusal still runs, `force: true` once the session is already dead).
- Inline rename (existing feature) swaps line 1 for an input with the same metrics.
- Drag-reorder handle: whole row, as today.
- The row is composed by `src/components/Sidebar.tsx`, which owns the behaviour around it: drag-reorder, double-click-to-rename, and reading live session state at call time rather than closing over the `SessionInfo` the row was drawn from.

## `projectCard({ project, sessions, collapsed, ... })` — React, `src/components/ProjectCard.tsx`

- `--surface-raised` body, 1px `--border`, `--radius-md`, margin `var(--space-1) var(--space-2) var(--space-2)`.
- Header 30px: chevron (collapsed state), 8px colour swatch (`--session-color` data), name `--text-md` 500, session count `--font-mono --text-xs --fg-subtle` right-aligned, then hover actions (`plus` new session, `branch` worktrees, `settings` edit project, `minus` minimize project, `x` delete project). The five buttons take an 18px box, not the primitive's 24px — at the 220px sidebar floor the default size squeezes the name to ~3px.
- The card ROOT gets `data-state="attention"` when any child session has attention (that is what the CSS selects): the header's swatch gains the pulse ring. Nothing else on the header changes.
- Collapsed: body hidden, header shows "n sessions · k need you" in the count slot.

## `chip({ label, color?, state?, onClick, onRestore? })` — `src/components/Chip.tsx`

- Draws both trays: the minimized-projects footer in the sidebar and the minimized-sessions tray above the status bar. 24px tall, `--radius-sm`, `--btn` fill, `--text-sm`.
- Anatomy: state icon or colour swatch (7px) + label + optional `plus` restore icon button.
- `data-state="attention"` → state icon pulses; label `--state-attention`.

## `<ModalShell {...{ id, root, title, body, actions, size? }} />` — `src/components/modals/ModalShell.tsx`

- Backdrop `rgba(0,0,0,.5)`, panel `--surface`, `--radius-md`, `--shadow-popover`, max-width `sm` 420 / `md` 560 / `lg` 720px.
- Header 44px: title `--text-xl` 600 + `x` icon button. Body padding `var(--space-4)`. Footer: right-aligned `button`s, primary last.
- Uses `src/lib/focus-trap.ts`; Escape closes; `role="dialog" aria-modal="true" aria-labelledby`.
- The root element is NOT created by the shell — it is declared in `index.html`, and the island that renders the shell toggles its `hidden` class from a layout effect. That class is the open/closed signal every keyboard gate and e2e assertion reads.
- The imperative `ui/dialog.ts` this replaced was deleted with the Phase 4 modal ports.
- Section heading inside bodies: `--text-lg` 500, `--fg`, margin-top `--space-5`.
- Hint paragraph: `--text-sm --fg-muted`, max-width 60ch.

## `banner({ text, kind, actions })` — `src/components/Banner.tsx`

- Full-width row above the app grid (rows 1–2 today). `kind`: `error` (`--state-error` left border 3px, `--surface`) | `info` (`--accent` border).
- 36px, `--text-md`, actions as `button` kind `ghost`, dismiss as `iconButton(x)`.

## `statusBar` (existing `status.ts` controller, new skin)

- 24px, `--surface`, top border, `--text-xs --fg-muted`; error flash → `--state-error`. Left: persistent slot. Right: keyboard hint for the current mode.

## `launcherItem` / command palette rows

- 32px, `--text-md`, leading `icon` (12px) for agent kind, trailing shortcut in `--font-mono --text-xs --fg-subtle`. Selected → `--sel` + accent bar, same as session row.

## Grid tile header

- 28px, `--surface`, bottom border. `stateIcon` + name `--text-sm` 500 + `·` + window title `--fg-subtle`, hover actions right.

## Form fields (project editor, settings)

- Label `--text-sm --fg-muted` above input. Input 28px, `--surface-raised`, `--border`, `--radius-sm`, `--text-md`; focus → border `--accent`. Colour input keeps native picker, wrapped in a 28px swatch button.

## What is *not* a primitive

Empty state, boot/phase panel, help overlay, and the terminal host keep bespoke markup but must use tokens and `icon()`; they're one-off surfaces.

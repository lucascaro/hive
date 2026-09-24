# Components

Primitives are React components in `src/components/` (`.tsx`, one per file). `src/ui/` — the original plain-TypeScript DOM builders, a function returning an element plus, where the element has state, a small `updateX()` patch twin — is gone: the [React rewrite](../../exec-plans/completed/react-ui-rewrite.md) took all but `icon.ts` and `icon-button.ts`, and the [tile-chrome port](../../product-specs/329-react-ify-sessionterms-tile-chrome.md) took those two with the terminal tile's last imperative markup. The sprite outlived them: `ensureSprite()`, `ICON_NAMES` and `icons.svg` moved to `src/lib/icon-sprite.ts`, which `components/Icon.tsx` calls on every render. There is no imperative path to markup left — a feature module that needs a control renders a component. Feature modules (`src/app/*`, `src/components/*`) compose primitives; they do not create `button`/`li`/`div` with hand-written classes for anything listed here.

The signatures below are written in the imperative form, and each heading names the file that implements it. Read a React component's props as the same fields: `sessionRow({ session, selected, … })` is `<SessionRow session={…} selected={…} … />`.

Each primitive owns its CSS in `src/theme/components/<name>.css`. Class names are `hv-<name>` and `hv-<name>__<part>`; modifiers are data attributes (`data-state="attention"`, `data-selected`), not extra classes.

## `button({ label, kind?, icon?, onClick })` — `src/components/Button.tsx`

- `kind`: `default` | `primary` | `danger` | `ghost`.
- Anatomy: optional leading icon (14px) + label. Height 28px, padding `0 var(--space-3)`, `--text-md`, `--radius-sm`.
- Tokens: default = `--btn`/`--btn-border`/`--fg-muted`; primary = `--accent`/`--on-accent`; danger = transparent with `--state-error` text and border; ghost = no fill, no border.
- States: hover `--hover`, active darken 8%, disabled `opacity .5; pointer-events none`, focus-visible ring.

## `iconButton({ icon, label, onClick })` — `src/components/IconButton.tsx`

- 24×24 (rows/bars) or 22×22 (sidebar header), icon 14px centred, `aria-label` required, `title` mirrored. Same fills as `button` kind `ghost` at rest, `default` on hover.

## `kbd(text)` — `src/components/Kbd.tsx`

- `<kbd class="hv-kbd">`, `--font-mono --text-xs --fg-subtle`. The only way to render a key hint. See patterns.md › Keyboard hints.

## `icon(name, { size? })` — `src/components/Icon.tsx` (sprite: `src/lib/icon-sprite.ts`)

- Returns `<svg class="hv-icon"><use href="#hv-<name>"/></svg>`. Size 14 default, 12 inline.

## `stateIcon(state)` — `StateIcon` in `src/components/Icon.tsx`

- Wraps `icon()` for the five states, sets `data-state`, applies animation classes. Used by session row, chip, tile header and the loading panel's active step. There is no `updateStateIcon` twin any more — a new `state` prop is the update.

## `sessionRow({ session, selected, onSelect, onMinimize, onRestart, onKill })` — React, `src/components/SessionRow.tsx`

Decided in [mocks/sidebar-structure.html](mocks/sidebar-structure.html) (S2 inside S3); reworked in [mocks/sidebar-redesign.html](mocks/sidebar-redesign.html).

- Height 40px at the default density. Grid: `[state 14px] [name 1fr] [idea] [worktree] [meta+actions]` on row 1, the window title on row 2 spanning `2 / -1`. Every child is placed explicitly — an unplaced grid child auto-flows to a new line and wraps the row — and the action buttons span both rows, since at 24px they would otherwise set row 1's height and push the row past 40px.
- Line 1: name, `--text-md`, `--on-accent` when selected (on the accent fill) else `--fg-muted`; attention → `--state-attention` + weight 500; exited/error → `--fg-subtle` + `text-decoration: line-through` (see patterns.md for why not hidden). The name is rendered verbatim. Generated names no longer carry the agent id at all (`internal/agent/names.go`, `internal/registry/create.go`), which is where that duplication was fixed — never at display time: nothing on the wire distinguishes a generated name from one the user typed, so trimming a trailing agent id would silently rewrite a session deliberately named "weekly claude".
- Line 2: window title, `--text-sm`, `--fg-subtle`, full row width. Falls back to state words when no title: "Starting…", "Exited", "Exited — <last_error>". Never both. With `titleOnly` (a row inside a worktree group whose name is the branch-derived default) the title becomes line 1 and there is no line 2.
- Density (`src/theme/density.ts`, `Settings › Appearance › Sidebar density`, stamped as `data-density` on `<html>`): `normal` 40px two lines (default), `tight` ~34px two lines, `compact` 28px one line — measured at ≥40% more rows per screen than normal. The line compact keeps is the window TITLE, moved up into line 1's cell with the name hidden; a row with no non-empty subtitle (no title published, or a `titleOnly` row inside a group) shows the name instead, so no row is ever blank.
- `kbd("[n]")` before the name when the session is one of the first nine in `orderedSessions()` (⌘1–9 bind to sessions, not projects).
- Meta column: worktree `branch` icon (12px, `--fg-subtle`) if session has a worktree; agent short code in `--font-mono --text-xs` (`cl`, `co`, `ge`, `sh`, custom = first two letters) on an 18% tint of `--agent-color` — the agent's own colour from `internal/agent/agent.go`, carried to the store at boot by `ListAgents()`. An agent that declares no colour renders `hv-session-row__agent--plain`: the code, no tint. A tint rather than a solid fill because Claude's `#f59e0b` would otherwise compete with `--accent` and `--state-attention`. On a selected row the code reads in `--on-accent`; the tint stays.
- Session colour: a 3px `--session-color` bar on the row's right edge (`hv-session-row__colour`), absolutely positioned in a right gutter the row reserves permanently, widening to 12px on hover or keyboard focus. The bar IS the colour control — it holds the uncontrolled `<input type="color">` — so widening costs no reflow and never moves the revealed actions. The bar runs to the list's edge — `#projects` has no side padding — and nothing covers it: `#sidebar-resizer` sits just outside the sidebar (spec 455), not over its right edge.
- Shared worktree: two or more sessions on one worktree path are wrapped in a `worktreeGroup` panel (below); the rows still carry `data-wt-shared` and the branch icon's count, and each keeps its own colour bar; the group's colour is its rail. Grouping, the drag, and the keyboard reorder are pure functions in `src/lib/worktree-groups.ts`. See patterns.md › One order for the ordering rule and the two reorder gestures.
- Attention: an `::after` overlay pulsing `--state-attention` at 12% on `--motion-pulse`, flat 9% under `prefers-reduced-motion`. An overlay rather than the row's own background, because an animation's value beats a normal declaration and would override selection's fill (patterns.md › Selection vs attention). On a selected row the overlay is `--on-accent` at 18% (12% static) instead — see Selected.
- Selected (spec 455): an `--accent` fill with `--on-accent` ink for everything the row draws — name, title, key hint, agent code, meta, action icons, focus rings, and the plan pie, stale or live. The STATE icon is the exception: it keeps its state colour — "needs you" never shares a colour with "fine" — and sits on a `--surface` disc (background + a 2px `outline`, since the attention pulse animates `box-shadow`), the ground every state colour is gated on. Plain `--accent`, not a mix toward `--surface`: `--on-accent` on `--accent` is the pair `ui-contrast.mjs` gates, and a mix fails AA on the light presets. The overrides live at the end of `session-row.css`, after the state and density rules they must beat. Hover keeps the fill. Hover on any other row: `--hover` and reveals actions replacing the meta column — `minus` (minimize), `rotate` (restart, exited/error rows only), `x` (kill, via the native `Confirm()` bridge; `force: false` on a live session so the daemon's dirty-worktree refusal still runs, `force: true` once the session is already dead).
- Minimized: a minimized session is painted only while it is the active session — `data-minimized`, and the `minus` action flips to `plus` (restore). Otherwise it has no row; it lives in the minimized-sessions tray, so every row besides the active one is a ⌘↑/⌘↓ stop (#407). The project card's count and attention still include it, and so does the row's shared-worktree count.
- Inline rename (existing feature) swaps line 1 for an input with the same metrics.
- Drag-reorder handle: whole row, as today. The row carries `data-drag-row` and spreads `dragRowProps()` from `src/lib/drag-row.ts` — the one drag implementation, shared with project cards and Settings' pinned agents (see patterns.md › Drag to reorder).
- The row is composed by `src/components/Sidebar.tsx`, which owns the behaviour around it: drag-reorder, double-click-to-rename, and reading live session state at call time rather than closing over the `SessionInfo` the row was drawn from.

## `worktreeGroup({ branch, count, color, label, onRenameTitle })` — React, `src/components/WorktreeGroup.tsx`

Decided in [mocks/sidebar-redesign.html](mocks/sidebar-redesign.html) (G3c).

- An `<li>` in the project card's `<ul>`, holding a header and its own `<ul>` of rows, so document order — which IS the painted order — is unchanged by the wrapping.
- The only CONTAINER in the sidebar tree: a RAIL (spec 455). `border-top` + `border-bottom` 1px `--border`, no fill, square; the member list (`__rows`) is indented `--space-3` behind a 1px `border-left` in `--session-color`, and member rows step in `--space-2` from it (9px of title, not 13). Containment means exactly one thing — these sessions share a working directory. History: a rounded inset card until #392, then a full-bleed band on `--surface-raised` — about 1% luminance off `--surface`, so only the hairlines separated a group from loose rows, and they read like the project label's rule. Picked in an interactive tuner during spec 455; the chosen values are recorded in the spec's Notes.
- Header: `--space-1 --space-3` padding, no bottom hairline (the rail marks where the body starts). Its `background` (`--surface`) is stated explicitly and fully opaque even though the panel has no ground: it is sticky and member rows scroll under it. Its text is `--hv-group-ink` — `color-mix(--session-color 70%, --fg)`, the group's colour kept legible whatever the user picked (the colour is data, so ui-contrast cannot gate it). Chevron (collapse, local state — the store's `collapsed` set is keyed by project id and pruned against the project list, which would drop a worktree key), `branch` icon + branch name (`--text-sm`, "detached HEAD" when there is none), member count. Sticky at `--sidebar-project-header-h`, nested under the project label.
- **Never `overflow: hidden`.** Clipping makes the panel the nearest scroll container, so its own sticky header sticks to a box that never scrolls and silently does nothing.
- The rail is the group's colour mark, since members inherit the colour of the worktree they adopt. The panel has no bar of its own; each member keeps its own right-edge bar, which is also its picker.
- A member renders `titleOnly` when its name is the branch-derived default the header already states; a session the user renamed keeps its name. Naming the GROUP (below) does not change this: the label is the group's, the names are the members'.
- **The group can be named** (#395). Double-clicking the title opens the shared inline editor (`app/inline-rename.ts`, class `group-name-input`), seeded with the current name or the branch. The name is real state — persisted daemon-side on the owning project as `worktree_labels[worktreePath]`, set with `SET_WORKTREE_LABEL` and broadcast as a `PROJECT_EVENT`, so every open window repaints rather than only the one that typed it. It is NOT derived from member names, and committing renames no session: an earlier design fanned the name out over the members and was withdrawn because it worked exactly once and could clobber a hand-named session. Emptying the field clears the name (`allowEmpty`, opt-in precisely because an empty session or project name is a mistake while an unnamed group is just a group).
- Title cell: one `__title` span holding the optional `__label` (`--text-sm`) and then the `__branch`, both in `--hv-group-ink`. The branch is never dropped when a name is present — stating it is the panel's whole reason to exist — it only demotes to `--text-xs` and gives up its width first. The editor mounts over the whole `__title` cell so there is one mount target whether or not a name exists.
- The double-click guard is `closest('.hv-worktree-group__title')`, not target identity: the branch carries an `<Icon>` svg, and a click on the icon is a click on the title. The chevron, count and collapsed-state alert sit outside `__title`, so no deny list is needed.

## `projectCard({ project, sessions, collapsed, ... })` — React, `src/components/ProjectCard.tsx`

- A flat LABEL, not a card: no border, no fill, no side margins. The worktree group rail is the only container in the tree.
- Header `--sidebar-project-header-h` (26px), sticky at the top of `#projects`: chevron (collapsed state), 8px colour swatch (`--project-color` data), name uppercase `--text-xs` 600 `--fg-muted` with `0.04em` tracking, a 2px `--project-color` rule across the top (an inset `box-shadow`, so the header's height — which the group header's sticky offset reads — does not change; spec 455), hairline `--border` rule beneath, session count `--font-mono --text-xs --fg-subtle` right-aligned, then hover actions (`plus` new session, `branch` worktrees, `settings` edit project, `minus` minimize project, `x` delete project). The five buttons take an 18px box, not the primitive's 24px — at the 220px sidebar floor the default size squeezes the name to ~3px.
- The active project is the label in `--accent` (it was the card's border-color, which went with the card).
- Card margin `--space-1` above, `--space-3` below.
- The card ROOT gets `data-state="attention"` when any child session has attention (that is what the CSS selects): the header's swatch gains the pulse ring. Nothing else on the header changes.
- Collapsed: body hidden, header shows "n sessions · k waiting on you" in the count slot — the same wording the menu bar summary uses, off the same predicate.

## `chip({ label, color?, state?, count?, attention?, onClick, onRestore? })` — `src/components/Chip.tsx`

- Draws both trays: the minimized-projects footer in the sidebar and the minimized-sessions tray above the status bar. 24px tall, `--radius-sm`, `--btn` fill, `--text-sm`.
- Anatomy: state icon or colour swatch (7px) + label + optional count + optional alert (state icon + number) + optional `plus` restore icon button.
- A **session** chip passes `state` and draws the state icon in the leading slot. A **project** chip keeps its identity colour dot there and instead passes `count` (its sessions) and `attention` (`{ count, state }` from `attentionSummary()`), which render as the two trailing slots — so a minimized project reads like the collapsed card it replaces. Both trailing slots sit *inside* `.hv-chip__open`, left-packed after the label: the slack before the `+` is a restore target and must stay empty.
- `data-state="attention"` → state icon pulses; label `--state-attention`. A project chip can also carry `data-state="waiting-permission"`; `minimized.css` gives it the same label colour and dot pulse, scoped to `#minimized-projects` so session chips are unaffected.

## `<ModalShell {...{ id, root, title, size?, hints?, titleSuffix?, showCloseButton?, actions? }}>{children}</ModalShell>` — `src/components/modals/ModalShell.tsx`

- Backdrop `rgba(0,0,0,.5)`, panel `--surface`, `--radius-md`, `--shadow-popover`, max-width `sm` 420 / `md` 560 / `lg` 720px.
- Header 44px: title `--text-xl` 600 + `x` icon button. Body padding `var(--space-4)`. Footer: right-aligned `button`s, primary last.
- Uses `src/lib/focus-trap.ts`; Escape closes; `role="dialog" aria-modal="true" aria-labelledby`.
- The root element is NOT created by the shell — it is declared in `index.html`, and the island that renders the shell toggles its `hidden` class from a layout effect. That class is the open/closed signal every keyboard gate and e2e assertion reads.
- The body is `children`, not a prop. `hints` are `{ keys, label }` pairs rendered through `Kbd` and separated by ` · ` in CSS; `titleSuffix` rides inside the `<h3>` so the accessible name stays one string; `showCloseButton={false}` is for a dialog whose own actions already cover backing out (the choice dialog). A footer with neither hints nor actions is `hidden`.
- The imperative `ui/dialog.ts` this replaced was deleted with the Phase 4 modal ports.
- Section heading inside bodies: `--text-lg` 500, `--fg`, margin-top `--space-5`.
- Hint paragraph: `--text-sm --fg-muted`, max-width 60ch.

## `<Tabs {...{ id, tabs, active, onChange, label }} />` — `src/components/Tabs.tsx`

- Splits a surface into sections. `tabs` is `{ id, label }[]`; `active` is a tab id; `onChange` is called with the newly selected id. The panels are **not** owned here — the caller renders them and hides the inactive ones, which is what keeps their state alive across a switch.
- Strip 1px `--border` bottom, `--space-1` gap. Tab: `--text-md`, `--fg-muted` at rest, `--fg` on hover and when selected, `--space-2`/`--space-3` padding, 2px bottom border (`transparent` → `--accent` when selected) so the selection is shape as well as colour.
- `role="tablist"` (`aria-label` from `label`) over `role="tab"` buttons with `aria-selected` and `aria-controls`. Roving `tabindex`: the selected tab is `0`, the rest `-1`, so the strip is one Tab stop. Left/Right wrap, Home/End jump; selection follows focus, and focus is moved with it.
- Ids are `<id>-tabs` for the strip and `<id>-tab-<tabId>` for each tab — a selector contract for the e2e specs, like the `hv-*` names (FRONTEND.md).
- Callers pair each tab with a `role="tabpanel"` element at `<id>-panel-<tabId>`, `aria-labelledby` its tab, hidden with `display: none` when inactive — which is also what takes its controls out of the tab order.
- Generic over the caller's id union (`Tabs<TabId>`), so `onChange` hands back that union rather than a bare `string` the call site has to cast. A conditional tab is built into the `tabs` array (Settings' macOS-only Menu bar tab), not rendered disabled.

## `banner({ text, kind, actions })` — `src/components/Banner.tsx`

- Full-width row above the app grid (rows 1–2 today). `kind`: `error` (`--state-error` left border 3px, `--surface`) | `info` (`--accent` border).
- 36px, `--text-md`, actions as `button` kind `ghost`, dismiss as `iconButton(x)`.

## `statusBar` (existing `status.ts` controller, new skin)

- 24px, `--surface`, top border, `--text-xs --fg-muted`; error flash → `--state-error`. Left: persistent slot. Right: keyboard hint for the current mode.

## `launcherItem` / command palette rows

- 32px, `--text-md`, leading `icon` (12px) for agent kind, trailing shortcut in `--font-mono --text-xs --fg-subtle`. Selected → `--sel` + accent bar. This is the session row's OLD look; since spec 455 the session row is an accent fill and these rows deliberately kept theirs.
- The launcher's agent list is `orderAgents()` (`src/lib/agent-order.ts`): hidden agents dropped, pinned agents first in the user's order, the rest by launch count. Settings → Agents edits both (`LauncherAgents.tsx`, stored as `hive.agentPrefs`); pinned rows there are drag-reorderable, with ⌥↑/⌥↓ as the keyboard path.
- Settings → Appearance → Editor (`EditorSettings.tsx`) picks what ⇧⌘-click on a file path opens: a preset (VS Code, Cursor, Zed, Sublime), a custom argv template with `{file}`/`{line}`/`{col}`, or a macOS application. It is stored Go-side in `editor.json`, not localStorage, because it is the command Hive executes — the bridge call carries only a path and a position. A file that will not parse disables the section instead of being overwritten on save, as with `agent-settings.json`.

## Grid tile header

- 28px, `--surface`, bottom border. `stateIcon` + name `--text-sm` 500 + `·` + window title `--fg-subtle`, hover actions right.

## Agent activity (panel and tile)

- One component family in `src/components/activity/`, looked up through `registry.ts` by placement (`panel`, `tile`). A second visualization is a new file plus one registry entry.
- **Panel:** `#activity-panel`, #app's third column (`--activity-panel-width`) in single view. `--surface`, left border. Plan steps with a `chevron` disclosure, a done `check`, and a tool-count pill (`--sel`, `--text-xs`); the timeline below in `--font-mono --text-xs`, tool name `--accent`, target `--fg-muted`, outcome `--fg-subtle` (`--state-running` while running, `--state-error` on failure).
- **Tile:** absolutely positioned under the 28px tile header over a `visibility: hidden` terminal body. The plan's task rows are the body — the same mark + text row as the panel, without its chevron or pill, the current step emphasized and scrolled into view. Above them a capped two-row strip of 8px pips (outline pending, `--state-running` ring active, filled done) carries the whole plan's shape, decorative (`aria-hidden`) beside the list. The same call rows follow, taking whatever height the tasks leave — `flex: 1 1 0` with `min-height: 0`, so they collapse to nothing on a plan taller than the tile.
- **Stale:** `data-stale` recolours tool names, marks and pips to `--fg-subtle`; the head says how long the tier has been quiet.
- Read-only and never focusable: no buttons, no `tabIndex`, and the root cancels `mousedown` so the terminal keeps the keyboard.

## Form fields (project editor, settings)

- Label `--text-sm --fg-muted` above input. Input 28px, `--surface-raised`, `--border`, `--radius-sm`, `--text-md`; focus → border `--accent`. Colour input keeps native picker, wrapped in a 28px swatch button.

## What is *not* a primitive

Empty state, boot/phase panel, help overlay, and the terminal host keep bespoke markup but must use tokens and `icon()`; they're one-off surfaces.

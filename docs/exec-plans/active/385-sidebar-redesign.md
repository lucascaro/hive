# Sidebar redesign: readable rows, real worktree groups, density setting

- **Spec:** [docs/product-specs/385-sidebar-redesign.md](../../product-specs/385-sidebar-redesign.md)
- **Issue:** —
- **Stage:** RESEARCH
- **Status:** active

## Summary

Rework the sidebar so each row states each fact once, worktree groups read as groups, and the window title gets the horizontal space it needs. The design was settled over three rounds of mockups against real tokens and real data — see `docs/design-docs/ui/mocks/sidebar-redesign.html`, which stays in the repo as the visual record. This plan covers the *how*; the decisions themselves are in the spec and are not open.

## Research

**The row.** `cmd/hivegui/frontend/src/components/SessionRow.tsx` builds the whole row; `theme/components/session-row.css` places six explicit grid children (`state | text | idea | worktree | meta+actions | swatch`). Two details constrain any change: `meta` and `actions` deliberately share grid cell 5 so the hover swap can be `opacity` rather than `display` (a `display: none` would drop the buttons out of the tab order), and the worktree control sits *outside* that swap because a control that vanishes on hover can never be clicked. `subtitleFor()` and `agentCode()` in the same file own the second line and the two-letter code.

**Where the duplication comes from.** `internal/agent/names.go:49` — `RandomName` returns `adjective-noun <agentID>`. `internal/registry/create.go:475` — a session with a worktree is instead named `branch.replace('/','-') + ' ' + agentID`, with no uniquifying pass. Both are Go-side and stay as they are; the dedupe is display-only, in the frontend.

**Agent colour already exists.** `internal/agent/agent.go:120` defines `Def.Color` for all seven built-ins, and `agent/custom.go:165` carries one for user-defined agents. Nothing new crosses the wire. Note the collision flagged in review: Claude's `#f59e0b` sits between `--accent` `#ffb454` and `--state-attention` `#ff9f43`, which is why the glyph is an 18% tint rather than a solid fill.

**Grouping.** `Sidebar.tsx` already computes `worktreeGroups(o.sessions)` and `clusterSessions()` sorts members adjacently; today the only output is `data-wt-shared` plus a count. The panel is new markup around an existing partition, not a new grouping algorithm. Collapse state can follow the existing `toggleCollapsed(pid)` / `collapsed` store pattern.

**Session colour is the tile border.** `theme/layout.css:72,104,126` paint `--session-color` onto the grid tile border and focus glow, so the sidebar's job is row↔tile matching. `session-row.css:70,172` are the swatch and the shared-worktree bar. The swatch is also the colour *control* (an uncontrolled `<input type="color">`, deliberately uncontrolled so the picker doesn't snap back mid-drag) — removing it means rehoming that control.

**Two implementation traps found while prototyping.** Both were caught in a real browser, not by reading:

1. *The group panel cannot use `overflow: hidden`.* It makes the panel the nearest scroll container, so a `position: sticky` group header sticks to the panel — which never scrolls — and silently does nothing. The panel must round its header's own corners instead of clipping. The failure is invisible until you actually scroll the list.
2. *The colour bar must not be laid out in the row's flow.* Absolutely positioned inside a `position: relative` row, with a permanently reserved right gutter, it can widen on hover without reflowing the row. Verified with `elementFromPoint` that the widened bar is the top element at its own centre and is not covered by the hover-revealed action buttons, which end 10px clear of it.

**Rules that constrain this.** `docs/design-docs/ui/patterns.md › Selection vs attention` reserves the row background for selection; the attention pulse needs that amended (wording proposed in the spec). `README` principle 2 — one channel per fact — is why the agent colour does not also go on the state dot.

**Tests that will move.** `test/dom/sidebar-*.test.tsx` (render scope, reorder, dblclick rename, title, focus) and `test/e2e/sidebar-*.spec.ts`, plus the theme snapshots under `test/e2e/theme.spec.ts-snapshots/` — the sidebar PNGs will all need regenerating.

## Approach

<Populated at PLAN stage. The design is fixed by the spec; what remains is decomposition — most likely: (1) display-name dedupe + agent glyph, (2) subtitle spans the row, (3) group panel, (4) flat sticky project label, (5) colour edge bar + picker rehome, (6) attention pulse + patterns.md amendment, (7) density setting. Each is independently shippable and independently revertable.>

### Files to change

<Populated at PLAN stage.>

### Tests

<Populated at PLAN stage.>

## Slices

Each slice is one PR. Run `./scripts/ci-bootstrap.sh` once in a fresh worktree first — `npm run typecheck` needs the generated wailsjs bindings and will otherwise report ~20 errors in files you never touched. Verify by exit code, not by reading output: `npx biome ci . >/dev/null && echo OK`.

### Slice 1 — Display name and agent glyph

**Files:** create `src/lib/session-name.ts`, `test/dom/session-name.test.ts`, `test/dom/sidebar-agent-glyph.test.tsx`; modify `src/components/SessionRow.tsx`, `src/store/store.ts`, `src/main.tsx`, `src/theme/components/session-row.css`.

**Produces:** `displayName(s: { name?: string; agent?: string }): string`, and `useAppStore(s => s.agentColors): ReadonlyMap<string, string>`.

- [ ] Write `test/dom/session-name.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import { displayName } from '../../src/lib/session-name.js';

describe('displayName', () => {
  it('strips the trailing agent id the row also shows', () => {
    expect(displayName({ name: 'rising-shore claude', agent: 'claude' })).toBe('rising-shore');
    expect(displayName({ name: 'feat-sidebar codex', agent: 'codex' })).toBe('feat-sidebar');
  });
  it('leaves a name whose tail is a different agent', () => {
    expect(displayName({ name: 'rising-shore claude', agent: 'codex' })).toBe('rising-shore claude');
  });
  it('leaves a name that is only the agent id', () => {
    expect(displayName({ name: 'claude', agent: 'claude' })).toBe('claude');
  });
  it('leaves names with no agent, no tail, or trailing space noise', () => {
    expect(displayName({ name: 'my session', agent: '' })).toBe('my session');
    expect(displayName({ name: 'shipit', agent: 'claude' })).toBe('shipit');
    expect(displayName({ name: '', agent: 'claude' })).toBe('');
  });
});
```

- [ ] Run `npx vitest run test/dom/session-name.test.ts` — expect FAIL, module not found.
- [ ] Implement `src/lib/session-name.ts`:

```ts
// Display-time only. The stored name stays exactly as the daemon wrote it
// (internal/agent/names.go builds "adjective-noun <agentID>", and
// internal/registry/create.go:475 builds "<branch> <agentID>"), because
// rename, search and the `hive` CLI all key on it. The row already renders
// the agent as its own glyph, so the tail is the one thing we drop.
export function displayName(s: { name?: string; agent?: string }): string {
  const name = (s.name ?? '').trim();
  const agent = (s.agent ?? '').trim();
  if (!name || !agent) return name;
  const tail = ` ${agent}`;
  // `endsWith` alone would turn a session literally named "claude" into ''.
  if (!name.endsWith(tail) || name.length === tail.length) return name;
  return name.slice(0, -tail.length);
}
```

- [ ] Run the test — expect PASS.
- [ ] Add the `agentColors` store slice and populate it at boot in `main.tsx` from `ListAgents()` (the same call `components/modals/Launcher.tsx:229` makes), keyed `id -> color`, skipping empty colours.
- [ ] Write `test/dom/sidebar-agent-glyph.test.tsx` covering: a claude row's glyph has `--agent-color: #f59e0b`; an agent absent from the map renders `.hv-session-row__agent--plain` with no custom property; the row's name element renders `displayName`, not `s.name`.
- [ ] Point `SessionRow.tsx` at `displayName(s)` and render the glyph with `style={{ '--agent-color': color }}`; in `session-row.css` give `.hv-session-row__agent` the tint:

```css
.hv-session-row__agent {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--agent-color, var(--fg-subtle));
  background: color-mix(in srgb, var(--agent-color, transparent) 18%, transparent);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-1);
}
```

- [ ] Run `npx vitest run test/dom` and `npx biome ci . >/dev/null && echo OK`.
- [ ] Regenerate the sidebar theme snapshots: `CI=1 npx playwright test theme.spec.ts --update-snapshots` (local Playwright must run with `CI=1` or it reuses a stale vite dev server and a green run means nothing).
- [ ] Commit: `feat(gui): show each session's agent once, as a coloured glyph`
- [ ] Add a changeset under `.changesets/` — never write the literal skip-ci marker in the body.

### Slice 2 — The window title spans the row

**Files:** modify `src/theme/components/session-row.css`; update `test/dom/sidebar-title.test.tsx`.

- [ ] Add to `sidebar-title.test.tsx` an assertion that the sub element's computed `grid-column-end` is `-1` (jsdom returns the specified value, which is what we are pinning — the visual check is the snapshot).
- [ ] Run it — expect FAIL.
- [ ] In `session-row.css`, place the subtitle on its own grid row spanning to the end, keeping row height at 40px:

```css
.hv-session-row__sub { grid-column: 2 / -1; grid-row: 2; }
```

- [ ] Run the test — expect PASS. Confirm in a browser that row height is unchanged at 40px; a mock that grew to 44px was the exact error caught in design review.
- [ ] Update snapshots, `biome ci`, commit: `feat(gui): give the sidebar window title the full row width`

### Slice 3 — Session colour as an edge bar that is also the picker

**Files:** modify `src/components/SessionRow.tsx`, `src/theme/components/session-row.css`; create `test/dom/sidebar-colour-bar.test.tsx`, `test/e2e/sidebar-colour-picker.spec.ts`.

- [ ] Write the DOM test: the bar is a `<button>` with an accessible name containing the session name; it carries `--session-color`; clicking it forwards to a visually hidden `<input type="color">`; the input stays **uncontrolled** (assert no `value` prop — a controlled value snaps the swatch back mid-drag, which is why `SessionRow.tsx:118` writes it through a ref).
- [ ] Run — expect FAIL.
- [ ] Replace the `__swatch` cell with the bar. Reserve the gutter permanently so the hover expansion costs no reflow:

```css
.hv-session-row { padding-right: var(--space-3); }
.hv-session-row__colour {
  position: absolute; right: 0; top: 1px; bottom: 1px;
  width: 3px; border-radius: var(--radius-sm);
  background: var(--session-color, var(--fg-subtle));
  border: 0; padding: 0; cursor: pointer;
  transition: width var(--motion-fast) ease;
}
.hv-session-row:hover .hv-session-row__colour,
.hv-session-row__colour:focus-visible { width: 12px; }
```

- [ ] Run the DOM test — expect PASS.
- [ ] Write `test/e2e/sidebar-colour-picker.spec.ts`: hover a row, assert `document.elementFromPoint(barCentre)` is the bar (not an action button), and assert the actions' bounding box is identical at rest and on hover. vitest is CSS-blind — this assertion only means something in a real browser.
- [ ] Run `CI=1 npx playwright test sidebar-colour-picker.spec.ts`.
- [ ] Update snapshots, `biome ci`, commit: `feat(gui): move session colour to an edge bar that opens the picker`

### Slice 4 — Worktree group panel

**Files:** create `src/components/WorktreeGroup.tsx`, `test/dom/sidebar-group.test.tsx`; modify `src/components/Sidebar.tsx`, `src/components/SessionRow.tsx`, `src/theme/components/sidebar.css`.

**Consumes:** `displayName` (slice 1). **Produces:** `<WorktreeGroup branch={string} count={number} color={string} collapsed={boolean} onToggle={() => void}>`, and `SessionRow`'s new `titleOnly: boolean` prop.

- [ ] Write `test/dom/sidebar-group.test.tsx` using `sidebar-harness`: two sessions sharing `worktree_path` render exactly one `.hv-worktree-group` whose header text contains the branch and the count `2`; one session alone renders none; a member whose name differs from the branch-derived default still renders its name while default-named members render only the title; and — the half of slice 3 that could not be tested until now — members render **no** per-row colour bar, the panel carries one for the group.
- [ ] Run — expect FAIL.
- [ ] Suppress the per-row bar inside a panel and give the panel its own, since members share a colour (a session adopting a sibling's worktree inherits it — `internal/registry/create.go`), so one bar per row would be three marks for one fact:

```css
.hv-worktree-group { position: relative; }
.hv-worktree-group .hv-session-row__colour { display: none; }
.hv-worktree-group::after {
  content: ''; position: absolute; right: 0; top: 0; bottom: 0;
  width: 3px; border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
  background: var(--session-color, var(--fg-subtle));
}
```

  Note the consequence for slice 3's control: inside a panel the picker's hover target must still be per-row, so the row's button stays in the DOM and becomes visible on hover over its own row rather than being removed.
- [ ] Implement. Derive `titleOnly` as `displayName(s) === branch.replaceAll('/', '-')`. Wrap the members `clusterSessions()` already places adjacently — do **not** introduce a second ordering; `lib/worktree-groups.ts`'s header comment explains why (drag slots resolved in two spaces was a real bug).
- [ ] Critical CSS constraint — the panel must **not** use `overflow: hidden`. It makes the panel the nearest scroll container, so slice 5's sticky group header sticks to a box that never scrolls and silently does nothing. Round the header's own corners instead:

```css
.hv-worktree-group { border: 1px solid var(--border); border-radius: var(--radius-md); }
.hv-worktree-group__header { border-radius: var(--radius-md) var(--radius-md) 0 0; }
```

- [ ] Run the DOM tests; run `test/dom/sidebar-reorder.test.tsx` unchanged — drag-reorder across a group boundary must still pass.
- [ ] Update snapshots, `biome ci`, commit: `feat(gui): render worktree groups as a panel with a branch header`

### Slice 5 — Flat sticky project label

**Files:** modify `src/components/ProjectCard.tsx`, `src/theme/components/project-card.css`, `src/theme/components/sidebar.css`; create `test/e2e/sidebar-sticky.spec.ts`.

- [ ] Write the e2e test: seed a project with enough sessions to overflow, scroll into the middle of a worktree group, assert both the project label and the group's branch header have `top >= scroller.top` and are within the viewport.
- [ ] Run — expect FAIL.
- [ ] Replace the card chrome with the label; move the active cue from `border-color` to the label's colour (`[data-active]` currently marks the card border — that cue must not simply disappear). Pin both headers:

```css
.hv-project-card__header { position: sticky; top: 0; z-index: 3; background: var(--surface); }
.hv-worktree-group__header { position: sticky; top: 26px; z-index: 2; }
```

- [ ] Run the e2e test — expect PASS. Also run `test/e2e/sidebar-focus-regression.spec.ts` and `sidebar-controls-reachable.spec.ts`: repaints dropping keyboard focus is a known failure mode in this area.
- [ ] Update snapshots, `biome ci`, commit: `feat(gui): flatten the project header and pin it while scrolling`

### Slice 6 — Attention pulse, and the rule it changes

**Files:** modify `src/theme/components/session-row.css`, `docs/design-docs/ui/patterns.md`; modify `test/e2e/theme.spec.ts`.

- [ ] Amend `patterns.md › Selection vs attention` first, with the wording in the spec's Notes. The doc changes in the same commit as the code it licenses.
- [ ] Add the theme-test assertion that the ordering survives reduced motion: with animation disabled the attention tint's alpha must stay below `--sel`, or a selected row stops reading as selected. Assert it rather than commenting it.
- [ ] Run — expect FAIL.
- [ ] Implement:

```css
@keyframes hv-attn-tint {
  0%, 100% { background-color: transparent; }
  50% { background-color: color-mix(in srgb, var(--state-attention) 12%, transparent); }
}
.hv-session-row[data-state='attention'] { animation: hv-attn-tint var(--motion-pulse) ease-in-out infinite; }
@media (prefers-reduced-motion: reduce) {
  .hv-session-row[data-state='attention'] {
    animation: none;
    background-color: color-mix(in srgb, var(--state-attention) 9%, transparent);
  }
}
```

- [ ] Run the theme tests — expect PASS. Check a selected + attention row in a browser under both motion settings.
- [ ] Update snapshots, `biome ci`, commit: `feat(gui): pulse the row background for sessions needing attention`

### Slice 7 — Sidebar density setting

**Files:** create `src/theme/density.ts`, `test/dom/sidebar-density.test.ts`; modify `src/components/modals/Settings.tsx`, `src/main.tsx`, `src/theme/components/session-row.css`.

**Produces:** `type Density = 'normal' | 'tight' | 'compact'`, `DENSITY_KEY`, `readDensity(storage?: Storage): Density`, `applyDensity(d: Density, doc?: Document): void`.

- [ ] Write `test/dom/sidebar-density.test.ts`: `readDensity` returns `'normal'` for absent, empty and garbage values and for a storage that throws; round-trips each valid value; `applyDensity` stamps `documentElement.dataset.density`.
- [ ] Run — expect FAIL.
- [ ] Implement `density.ts` mirroring `theme/theme.ts`'s shape (same try/catch-around-`localStorage` treatment — a throwing storage must not take the sidebar down). Do **not** add a pre-paint `<script>` to `index.html`: that duplication is a known sync hazard the theme module already warns about, and the worst case here is one frame of normal-height rows, not a full-window flash.
- [ ] Add the `<select>` to `Settings.tsx` beside the theme picker, following `settings-theme`'s markup and label wiring.
- [ ] Add the density blocks to `session-row.css` under `[data-density='tight']` and `[data-density='compact']`.
- [ ] Verify the measured claim in the spec: compact must fit at least 40% more rows in the same height than normal. Count in a browser, do not estimate — the round-2 mock claimed 60% and measured 40%.
- [ ] Update snapshots, `biome ci`, commit: `feat(gui): add a sidebar density setting`
- [ ] Update `docs/design-docs/ui/components.md` for `sessionRow` and the new group panel; flip the spec and this plan to `stage: REVIEW`.

## Decision log

- **2026-09-10** — Design settled over three mockup rounds before writing any code. Why: the ask was explicitly "all design changes go through a visual review before implementing".
- **2026-09-10** — Session colour moves to a right-edge bar rather than being hidden for auto-picked sessions. Why: nothing records whether a colour was auto-picked (`create.go:321`), so hiding "defaults" would need a new `colorExplicit` bit on the entry; the bar keeps every colour visible and needs no wire change.
- **2026-09-10** — Agent glyph is an 18% tint, not a solid fill. Why: Claude's `#f59e0b` would otherwise compete with `--state-attention` on most rows.
- **2026-09-10** — Inside a group, a row shows its name only when it differs from the branch-derived default. Why: dropping the name unconditionally would hide a name the user deliberately chose.
- **2026-09-10** — Group headers pin under the project label rather than scrolling away. Why: rows that scroll away from their branch header lose the thing the group exists to say.
- **2026-09-10** — The colour bar is the picker, widening on hover/focus, rather than moving the picker to a context menu. Why: it keeps the control where the colour is, keeps the existing keyboard path (today's swatch is focusable), and the row already reserves this space — today's swatch is at `grid-column: 6`, right of the actions.

## Progress

- **2026-09-10** — Spec written; exec plan opened at RESEARCH.

## Open questions

None. Both were resolved in review — headers nest (project at `top: 0`, group at `top: 26px`), and the colour bar is itself the picker.

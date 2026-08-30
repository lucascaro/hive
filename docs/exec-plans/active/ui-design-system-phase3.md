# UI design system — Phase 3: sessionRow, projectCard, chip (the sidebar)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hand-built sidebar markup with three primitives — `sessionRow`, `projectCard`, `chip` — and rebuild `sidebar.ts` and both minimized trays on them, landing the two-line 40px row, the project card, and the state-icon channel, with every existing sidebar behaviour intact.

**Architecture:** `src/ui/{session-row,project-card,chip}.ts` are pure DOM builders: they take data + callbacks, return an element, and own no application state. Each ships its CSS as `src/theme/components/<name>.css`, linked from `index.html` after `style.css` so the component layer wins over the legacy rules it replaces (the legacy rules are then deleted). `sidebar.ts` keeps every responsibility that is *not* markup — drag-reorder, inline rename, collapse persistence, minimize/restore delegation, the `updateSidebarSelection` / `updateSidebarTitles` in-place patch paths — and calls `updateSessionRow` instead of toggling classes by hand. Both trays (`#minimized-projects` in `sidebar.ts`, `#minimized-tray` in `view.ts`) render `chip()`. State comes from Phase 2's `sessionState(session, hasAttention)` (`src/lib/session-state.ts`); icons from `stateIcon`/`icon`/`iconButton`; key hints from `kbd`. Modifiers are data attributes (`data-state`, `data-selected`, `data-minimized`, `data-collapsed`, `data-active`), never ad-hoc classes.

**Tech Stack:** Vanilla TS, Vite 8, vitest 4 (`test/dom`, jsdom project), Playwright 1.62 (`test/e2e` + `wails-mock.ts`), `scripts/ui-lint.sh`. No new dependencies.

**Spec:** `docs/design-docs/ui/components.md` (`sessionRow`, `projectCard`, `chip`), `patterns.md` (selection vs attention, attention bubbling, exited sessions, hover-revealed actions, keyboard hints, density, motion), `tokens.md` (type scale, spacing, row heights, sidebar min width 220), `icons.md` (state icons, no Unicode), `AGENTS.md` › UX Best Practices (inline key hints are mandatory).

## Global Constraints

- **Re-verify every `file:line` reference in this plan before using it.** It was written against `echo-mesa` at commit `37e32f6` with Phase 1 landed and Phase 2 *not* landed. Phase 2 rewrites every Unicode glyph in `src/app/**`, which shifts line numbers in `sidebar.ts`, `view.ts`, `index.html` and `style.css`. Grep for the quoted code, do not trust the number.
- **Phase 2 is a hard prerequisite.** This plan is written against the API in [ui-design-system-phase2.md](ui-design-system-phase2.md):
  ```ts
  // src/ui/icon.ts
  icon(name: IconName, opts?: { size?: 12 | 14 }): SVGSVGElement;
  stateIcon(state: SessionState): SVGSVGElement;
  updateStateIcon(el: SVGSVGElement, state: SessionState): void;
  // src/ui/icon-button.ts, src/ui/kbd.ts
  iconButton(o: IconButtonOpts): HTMLButtonElement;   // { icon, label, onClick }
  kbd(text: string): HTMLElement;
  // src/lib/session-state.ts  (pure; NOT in src/ui/)
  type SessionState = 'starting' | 'attention' | 'running' | 'exited' | 'error';
  sessionState(s: StateCarrier, hasAttention: boolean): SessionState;
  const STATE_WORDS: Record<SessionState, string>;
  ```
  Note the state resolver takes a **boolean**, not the attention `Set` — every call site here passes `state.attention.has(s.id)`. If Phase 2 landed with different names, adapt the call sites — do not re-implement.
- **This is the first visible change.** `classic` stays the default preset until Phase 6; the new sidebar therefore has to be legible under `classic` too. It uses tokens only, so it is — but eyeball it once under all three presets (Task 8).
- **Every existing behaviour survives:** drag-reorder (sessions and projects, both drop indicators, the "bubbled dragstart from a session" guard), inline rename (session and project, `beginInlineRename`), collapse + `saveCollapsed`, minimize/restore for sessions and projects, attention bubbling to the project header and to both chip trays, keyboard nav (⌘1–9, ⌘[ / ⌘], arrows), the focus invariants covered by `sidebar-focus-regression.spec.ts` and `focus-invariants.spec.ts`, worktree glyph → `openWorktrees`, colour swatch → `UpdateSession`.
- **No literals.** No hex, no `font-size: <n>px`, no Unicode glyph. `./scripts/ui-lint.sh --strict` must exit 0 for the whole tree at the end of every task.
- Row height 40px, project-card header 30px, chip 24px, sidebar min width **220** (was 200/140 — see Task 6).
- Class names: `hv-<name>` root, `hv-<name>__<part>` parts. `data-sid` / `data-pid` stay exactly as they are — they are the stable test and query hook.
- No new npm dependencies. `npx biome ci .` (not `biome lint`), `npm run typecheck`, `npx vitest run`, `npx playwright test` all green per task.
- Run every frontend command from `cmd/hivegui/frontend/`. Fresh worktree → `./scripts/ci-bootstrap.sh` first or `npm run typecheck` fails on missing `wailsjs/`.
- Commits: conventional, one per task.

### Deliberate spec deviations (decided here, recorded in Task 9)

1. **`[n]` project number hints go on session rows, not project cards.** `components.md` › `projectCard` calls for `kbd("[n]")` with a project number 1–9, and `AGENTS.md` says numbered project shortcuts must show their number. **There is no project-number binding in the GUI.** `keyboard.ts:~358` binds ⌘1–9 to `orderedSessions()[n-1]` — sessions, globally ordered. A hint for a key that does nothing is worse than no hint, so the `[n]` renders in the session row's meta column for the first nine sessions in global order, which is exactly what ⌘1–9 does. `projectCard` takes no `index` parameter (an always-null one is dead code). Task 9 corrects `components.md`.
2. **`sessionRow` gains restart and kill.** `patterns.md` › Exited sessions requires `rotate` then `x` on hover. Neither exists on a sidebar row today, and `RestartSession` is currently unused in the frontend. They are wired: `rotate` is rendered only for `exited`/`error` rows; `x` kills, via the native `Confirm()` bridge when the session is still alive (AGENTS.md: destructive actions go through a confirm) and directly when it is not — the same rule `session-term.ts` `_closeDead` already follows.
3. **Both trays become `<div role="toolbar">`.** `#minimized-tray` already is one; `#minimized-projects` is a `<ul>`. `chip()` returns a `<span>` (it contains two buttons, so it can be neither a `<button>` nor a bare `<li>` child of a div), so the project tray changes element to match. Geometry assertions in `minimize-project.spec.ts` are unaffected.
4. **Exit codes do not exist on the wire.** `icons.md`'s state resolution reads `exit_code`, but `SessionInfo` (`src/app/state.ts:23-50`) and `internal/wire` have no such field — only `alive` and `last_error`/`lastError`. So `error` is "not alive and `last_error` is set", and line 2 reads `Exited` or `Exited — <last_error>`, never `Exited (1)`. This matches Phase 2's `sessionState()`, which resolves the same way for the same reason; `icons.md` is corrected in Task 9.

---

### Task 1: `chip` primitive

The smallest of the three and the one both trays share. Built first so Task 5 has something to render into.

**Files:**
- Create: `cmd/hivegui/frontend/src/ui/chip.ts`
- Create: `cmd/hivegui/frontend/src/theme/components/chip.css`
- Create: `cmd/hivegui/frontend/test/dom/ui-chip.test.ts`
- Modify: `cmd/hivegui/frontend/index.html` (the `<link>` block, currently lines 18-21)

**Interfaces:**
- Produces:
  ```ts
  export interface ChipOpts {
    label: string;
    sublabel?: string;          // project name under a session chip
    color?: string;             // user swatch colour; omitted → --fg-subtle
    state?: SessionState;       // omitted → colour dot instead of a state icon
    active?: boolean;
    title?: string;
    ariaLabel: string;
    onClick: () => void;
    onRestore?: () => void;     // renders the trailing `plus` icon button
    restoreLabel?: string;      // aria-label for it; required when onRestore is set
  }
  export function chip(o: ChipOpts): HTMLSpanElement;
  ```

- [ ] **Step 1: Write the failing DOM test**

`test/dom/ui-chip.test.ts`:

```ts
// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { chip } from '../../src/ui/chip';

describe('chip', () => {
  it('renders label, aria-label and fires onClick from the body button', () => {
    const onClick = vi.fn();
    const el = chip({ label: 'api', ariaLabel: 'Restore api', onClick });
    expect(el.className).toBe('hv-chip');
    const open = el.querySelector<HTMLButtonElement>('.hv-chip__open');
    expect(open?.getAttribute('aria-label')).toBe('Restore api');
    expect(el.querySelector('.hv-chip__label')?.textContent).toBe('api');
    open?.click();
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('carries state as a data attribute and renders a state icon', () => {
    const el = chip({
      label: 'api',
      state: 'attention',
      ariaLabel: 'Restore api',
      onClick: () => {},
    });
    expect(el.dataset.state).toBe('attention');
    expect(el.querySelector('.hv-state-icon')).not.toBeNull();
  });

  it('falls back to a colour dot when no state is given', () => {
    const el = chip({
      label: 'web',
      color: '#0af',
      ariaLabel: 'Restore web',
      onClick: () => {},
    });
    expect(el.querySelector('.hv-chip__swatch')).not.toBeNull();
    expect(el.style.getPropertyValue('--chip-color')).toBe('#0af');
  });

  it('renders the restore button only when onRestore is given, and it does not also fire onClick', () => {
    const onClick = vi.fn();
    const onRestore = vi.fn();
    expect(
      chip({ label: 'a', ariaLabel: 'a', onClick }).querySelector(
        '.hv-chip__restore',
      ),
    ).toBeNull();
    const el = chip({
      label: 'a',
      ariaLabel: 'Restore a',
      onClick,
      onRestore,
      restoreLabel: 'Restore a',
    });
    el.querySelector<HTMLButtonElement>('.hv-chip__restore')?.click();
    expect(onRestore).toHaveBeenCalledTimes(1);
    expect(onClick).not.toHaveBeenCalled();
  });

  it('renders the sublabel when given and omits the node when not', () => {
    const withSub = chip({
      label: 'api',
      sublabel: 'hive',
      ariaLabel: 'a',
      onClick: () => {},
    });
    expect(withSub.querySelector('.hv-chip__sub')?.textContent).toBe('hive');
    expect(
      chip({ label: 'api', ariaLabel: 'a', onClick: () => {} }).querySelector(
        '.hv-chip__sub',
      ),
    ).toBeNull();
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run test/dom/ui-chip.test.ts`
Expected: FAIL — cannot resolve `../../src/ui/chip`.

- [ ] **Step 3: Implement `src/ui/chip.ts`**

```ts
// Chip — minimized-session tray and minimized-project tray.
// docs/design-docs/ui/components.md › chip.
//
// A <span>, not a <button>: the chip body is one action and the restore
// control is another, and a button cannot contain a button. The trays are
// role="toolbar" divs, so a span is also the only valid child of both.
import { iconButton } from './icon-button.js';
import { stateIcon } from './icon.js';
import type { SessionState } from '../lib/session-state.js';

export interface ChipOpts {
  label: string;
  sublabel?: string;
  color?: string;
  state?: SessionState;
  active?: boolean;
  title?: string;
  ariaLabel: string;
  onClick: () => void;
  onRestore?: () => void;
  restoreLabel?: string;
}

export function chip(o: ChipOpts): HTMLSpanElement {
  const root = document.createElement('span');
  root.className = 'hv-chip';
  if (o.state) root.dataset.state = o.state;
  if (o.active) root.dataset.active = '';
  // Only set when the user actually picked a colour: the CSS falls back to
  // --fg-subtle, so an unset property is a themed default, while a literal
  // '#888' here would be an untokenised colour smuggled in from TS.
  if (o.color) root.style.setProperty('--chip-color', o.color);

  const open = document.createElement('button');
  open.type = 'button';
  open.className = 'hv-chip__open';
  open.setAttribute('aria-label', o.ariaLabel);
  open.title = o.title ?? o.ariaLabel;
  open.addEventListener('click', (e) => {
    e.stopPropagation();
    o.onClick();
  });

  // State icon when the chip stands for a session (it carries the bell for
  // a session that has no row on screen); a plain colour dot when it stands
  // for a project, whose state is the union of its sessions' and is carried
  // by the pulse on the dot instead.
  if (o.state) open.append(stateIcon(o.state));
  else {
    const dot = document.createElement('span');
    dot.className = 'hv-chip__swatch';
    open.append(dot);
  }

  const label = document.createElement('span');
  label.className = 'hv-chip__label';
  label.textContent = o.label;
  open.append(label);

  if (o.sublabel) {
    const sub = document.createElement('span');
    sub.className = 'hv-chip__sub';
    sub.textContent = o.sublabel;
    open.append(sub);
  }
  root.append(open);

  if (o.onRestore) {
    const restore = iconButton({
      icon: 'plus',
      label: o.restoreLabel ?? o.ariaLabel,
      onClick: (e) => {
        e.stopPropagation();
        o.onRestore?.();
      },
    });
    restore.classList.add('hv-chip__restore');
    root.append(restore);
  }
  return root;
}
```

- [ ] **Step 4: Write `src/theme/components/chip.css`**

```css
/* Chip — docs/design-docs/ui/components.md › chip. 24px, --btn fill.
   --chip-color is data (a user-picked session/project colour), not a token. */
.hv-chip {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
  height: 24px;
  max-width: 240px;
  min-width: 0;
  background: var(--btn);
  border: 1px solid var(--btn-border);
  border-radius: var(--radius-sm);
  color: var(--fg-muted);
  font-size: var(--text-sm);
}
.hv-chip:hover { background: var(--hover); color: var(--fg); }
.hv-chip[data-active] { border-color: var(--accent); color: var(--fg); }

.hv-chip__open {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  height: 100%;
  padding: 0 var(--space-2);
  background: none;
  border: 0;
  color: inherit;
  font: inherit;
  cursor: pointer;
}

.hv-chip__swatch {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
  background: var(--chip-color, var(--fg-subtle));
}

.hv-chip__label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.hv-chip__sub {
  flex-shrink: 0;
  color: var(--fg-subtle);
  font-size: var(--text-xs);
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

/* Attention bubbles up to the chip: the label takes the attention colour
   and the state icon pulses on its own (see icons.md). A colour-dot chip
   (project) pulses the dot, since it has no state icon. */
.hv-chip[data-state='attention'] .hv-chip__label { color: var(--state-attention); }
.hv-chip[data-state='attention'] .hv-chip__swatch {
  animation: hv-chip-pulse var(--motion-pulse) ease-in-out infinite;
}
@keyframes hv-chip-pulse {
  0%, 100% { box-shadow: 0 0 0 0 transparent; }
  50% { box-shadow: 0 0 0 3px color-mix(in srgb, var(--state-attention) 45%, transparent); }
}

.hv-chip__restore { flex-shrink: 0; margin-right: 2px; }

.hv-chip__open:focus-visible,
.hv-chip__restore:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
```

- [ ] **Step 5: Link the stylesheet**

In `index.html`, after the `./src/style.css` link (currently line 20), add:

```html
<link rel="stylesheet" href="./src/theme/components/chip.css"/>
```

After `style.css` on purpose: the component layer must win over the legacy rules until Task 5 deletes them.

- [ ] **Step 6: Run**

Run: `npx vitest run test/dom/ui-chip.test.ts && npm run typecheck && npx biome ci . && ../../../scripts/ui-lint.sh --strict`
Expected: 5 passed, typecheck clean, biome clean, `ui-lint: 0 violation(s)`.

- [ ] **Step 7: Commit**

```bash
git add cmd/hivegui/frontend/src/ui/chip.ts \
        cmd/hivegui/frontend/src/theme/components/chip.css \
        cmd/hivegui/frontend/test/dom/ui-chip.test.ts \
        cmd/hivegui/frontend/index.html
git commit -m "feat(ui): add chip primitive"
```

---

### Task 2: `sessionRow` primitive

**Files:**
- Create: `cmd/hivegui/frontend/src/ui/session-row.ts`
- Create: `cmd/hivegui/frontend/src/theme/components/session-row.css`
- Create: `cmd/hivegui/frontend/test/dom/ui-session-row.test.ts`
- Modify: `cmd/hivegui/frontend/index.html` (link block)

**Interfaces:**
- Produces:
  ```ts
  export interface SessionRowState {
    state: SessionState;
    selected: boolean;
    minimized: boolean;
    index: number | null;   // 1..9 → kbd("[n]"); null → no hint
  }
  export interface SessionRowOpts extends SessionRowState {
    session: SessionInfo;
    onSelect: () => void;
    onMinimize: () => void;
    onRestore: () => void;
    onRestart: () => void;
    onKill: () => void;
    onWorktrees: () => void;
    onColor: (hex: string) => void;
  }
  export function sessionRow(o: SessionRowOpts): HTMLLIElement;
  export function updateSessionRow(
    el: HTMLLIElement,
    s: SessionInfo,
    next: SessionRowState,
  ): void;
  ```
- Consumed by: Task 4 (`sidebar.ts` render + both in-place patch paths).
- Part classes other modules may query: `.hv-session-row__name`, `.hv-session-row__sub`.

- [ ] **Step 1: Write the failing DOM test**

`test/dom/ui-session-row.test.ts`:

```ts
// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { sessionRow, updateSessionRow } from '../../src/ui/session-row';
import type { SessionInfo } from '../../src/app/state';

const noop = () => {};
const base = {
  onSelect: noop,
  onMinimize: noop,
  onRestore: noop,
  onRestart: noop,
  onKill: noop,
  onWorktrees: noop,
  onColor: noop,
};
const row = (s: Partial<SessionInfo>, over: Partial<Parameters<typeof sessionRow>[0]> = {}) =>
  sessionRow({
    session: { id: 's1', name: 'api', ...s } as SessionInfo,
    state: 'running',
    selected: false,
    minimized: false,
    index: null,
    ...base,
    ...over,
  });

describe('sessionRow', () => {
  it('renders name on line 1 and the window title on line 2', () => {
    const el = row({ title: 'npm run build' });
    expect(el.dataset.sid).toBe('s1');
    expect(el.querySelector('.hv-session-row__name')?.textContent).toBe('api');
    expect(el.querySelector('.hv-session-row__sub')?.textContent).toBe(
      'npm run build',
    );
  });

  it('falls back to state words when there is no window title', () => {
    expect(
      row({}, { state: 'starting' }).querySelector('.hv-session-row__sub')
        ?.textContent,
    ).toBe('Starting…');
    expect(
      row({ alive: false }, { state: 'exited' }).querySelector(
        '.hv-session-row__sub',
      )?.textContent,
    ).toBe('Exited');
    expect(
      row({ alive: false, last_error: 'boom' }, { state: 'error' }).querySelector(
        '.hv-session-row__sub',
      )?.textContent,
    ).toBe('Exited — boom');
  });

  it('suppresses a title equal to the name (displayTitle rule)', () => {
    expect(
      row({ title: 'api' }, { state: 'running' }).querySelector(
        '.hv-session-row__sub',
      )?.textContent,
    ).toBe('');
  });

  it('exposes selection and state as data attributes, never as ad-hoc classes', () => {
    const el = row({}, { selected: true, state: 'attention' });
    expect(el.dataset.selected).toBe('');
    expect(el.dataset.state).toBe('attention');
    expect(el.className).toBe('hv-session-row');
  });

  it('renders the key hint for the first nine rows only', () => {
    expect(row({}, { index: 3 }).querySelector('.hv-kbd')?.textContent).toBe(
      '[3]',
    );
    expect(row({}, { index: null }).querySelector('.hv-kbd')).toBeNull();
  });

  it('renders the worktree icon and agent code in the meta column', () => {
    const el = row({ worktree_branch: 'feat/x', agent: 'codex' });
    expect(el.querySelector('.hv-session-row__worktree')).not.toBeNull();
    expect(el.querySelector('.hv-session-row__agent')?.textContent).toBe('co');
    expect(
      row({ agent: 'claude' }).querySelector('.hv-session-row__agent')
        ?.textContent,
    ).toBe('cl');
    expect(row({}).querySelector('.hv-session-row__worktree')).toBeNull();
  });

  it('shows restart only for exited/error rows and always shows kill + minimize', () => {
    const live = row({});
    expect(live.querySelector('[data-action="restart"]')).toBeNull();
    expect(live.querySelector('[data-action="kill"]')).not.toBeNull();
    expect(live.querySelector('[data-action="minimize"]')).not.toBeNull();
    expect(
      row({ alive: false }, { state: 'exited' }).querySelector(
        '[data-action="restart"]',
      ),
    ).not.toBeNull();
  });

  it('wires the actions, and none of them also selects the row', () => {
    const onSelect = vi.fn();
    const onMinimize = vi.fn();
    const onKill = vi.fn();
    const el = row({}, { onSelect, onMinimize, onKill });
    document.body.append(el);
    el.querySelector<HTMLButtonElement>('[data-action="minimize"]')?.click();
    el.querySelector<HTMLButtonElement>('[data-action="kill"]')?.click();
    expect(onMinimize).toHaveBeenCalledTimes(1);
    expect(onKill).toHaveBeenCalledTimes(1);
    expect(onSelect).not.toHaveBeenCalled();
    el.click();
    expect(onSelect).toHaveBeenCalledTimes(1);
    el.remove();
  });

  it('minimize flips to restore when the row is minimized', () => {
    const onRestore = vi.fn();
    const el = row({}, { minimized: true, onRestore });
    expect(el.dataset.minimized).toBe('');
    el.querySelector<HTMLButtonElement>('[data-action="minimize"]')?.click();
    expect(onRestore).toHaveBeenCalledTimes(1);
  });

  it('updateSessionRow patches state, title and hint in place without replacing nodes', () => {
    const el = row({ title: 'a' });
    const name = el.querySelector('.hv-session-row__name');
    updateSessionRow(
      el,
      { id: 's1', name: 'api', title: 'b' } as SessionInfo,
      { state: 'attention', selected: true, minimized: false, index: 1 },
    );
    expect(el.querySelector('.hv-session-row__name')).toBe(name);
    expect(el.querySelector('.hv-session-row__sub')?.textContent).toBe('b');
    expect(el.dataset.state).toBe('attention');
    expect(el.dataset.selected).toBe('');
    expect(el.querySelector('.hv-kbd')?.textContent).toBe('[1]');
    updateSessionRow(
      el,
      { id: 's1', name: 'api', title: 'b' } as SessionInfo,
      { state: 'running', selected: false, minimized: false, index: null },
    );
    expect(el.dataset.selected).toBeUndefined();
    expect(el.querySelector('.hv-kbd')).toBeNull();
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run test/dom/ui-session-row.test.ts`
Expected: FAIL — cannot resolve `../../src/ui/session-row`.

- [ ] **Step 3: Implement `src/ui/session-row.ts`**

```ts
// Session row — docs/design-docs/ui/components.md › sessionRow.
//
// 40px, two lines: name over window title. Grid is
// [state 14px] [text 1fr] [meta auto]; the meta column (key hint,
// worktree, agent code) is swapped for the action buttons on hover or
// keyboard focus — see patterns.md › Hover-revealed actions.
//
// This module owns markup and per-part callbacks only. Drag-reorder,
// inline rename and double-click live on the returned <li> and are wired
// by app/sidebar.ts, which owns that behaviour.
import { icon, stateIcon, updateStateIcon } from './icon.js';
import { iconButton } from './icon-button.js';
import { kbd } from './kbd.js';
import type { SessionState } from '../lib/session-state.js';
import { displayTitle } from '../lib/term-title.js';
import type { SessionInfo } from '../app/state.js';

export interface SessionRowState {
  state: SessionState;
  selected: boolean;
  minimized: boolean;
  index: number | null;
}

export interface SessionRowOpts extends SessionRowState {
  session: SessionInfo;
  onSelect: () => void;
  onMinimize: () => void;
  onRestore: () => void;
  onRestart: () => void;
  onKill: () => void;
  onWorktrees: () => void;
  onColor: (hex: string) => void;
}

// Line 2 when the program has published no window title. One channel per
// fact (README principle 2): the row says what the session is doing, and
// when it is doing nothing it says why. Never both title and state words.
function subtitleFor(s: SessionInfo, state: SessionState): string {
  const t = displayTitle(s.title, s.name);
  if (t) return t;
  if (state === 'starting') return 'Starting…';
  if (state === 'exited') return 'Exited';
  if (state === 'error') {
    const err = (s.last_error ?? s.lastError ?? '').trim();
    return err ? `Exited — ${err}` : 'Exited';
  }
  return '';
}

// Agent short code: two letters, mono, in the meta column. `cl`, `co`,
// `ge`, `sh` fall out of "first two letters" for the built-ins, so there
// is no table to keep in sync with settings' user-defined agents.
function agentCode(agent?: string): string {
  return (agent ?? '').trim().slice(0, 2).toLowerCase();
}

export function sessionRow(o: SessionRowOpts): HTMLLIElement {
  const s = o.session;
  const li = document.createElement('li');
  li.className = 'hv-session-row';
  li.dataset.sid = s.id;
  li.dataset.pid = s.projectId ?? s.project_id ?? '';
  li.draggable = true;
  if (s.color) li.style.setProperty('--session-color', s.color);

  // classList (an Element API, so it works on SVG); never `.className`,
  // which is a read-only SVGAnimatedString on an SVG element.
  const st = stateIcon(o.state);
  st.classList.add('hv-session-row__state');

  const text = document.createElement('span');
  text.className = 'hv-session-row__text';
  const name = document.createElement('span');
  name.className = 'hv-session-row__name';
  const sub = document.createElement('span');
  sub.className = 'hv-session-row__sub';
  text.append(name, sub);

  const meta = document.createElement('span');
  meta.className = 'hv-session-row__meta';

  const wtBranch = s.worktreeBranch ?? s.worktree_branch;
  if (wtBranch) {
    const wt = iconButton({
      icon: 'branch',
      label: `Worktree: ${wtBranch} — manage worktrees`,
      onClick: (e) => {
        e.stopPropagation();
        o.onWorktrees();
      },
    });
    wt.classList.add('hv-session-row__worktree');
    meta.append(wt);
  }

  const code = agentCode(s.agent);
  if (code) {
    const agent = document.createElement('span');
    agent.className = 'hv-session-row__agent';
    agent.textContent = code;
    meta.append(agent);
  }

  const actions = document.createElement('span');
  actions.className = 'hv-session-row__actions';

  const minBtn = iconButton({
    icon: o.minimized ? 'plus' : 'minus',
    label: `${o.minimized ? 'Restore' : 'Minimize'} ${s.name ?? 'session'}`,
    onClick: (e) => {
      e.stopPropagation();
      if (li.dataset.minimized === undefined) o.onMinimize();
      else o.onRestore();
    },
  });
  minBtn.dataset.action = 'minimize';

  const restartBtn = iconButton({
    icon: 'rotate',
    label: `Restart ${s.name ?? 'session'}`,
    onClick: (e) => {
      e.stopPropagation();
      o.onRestart();
    },
  });
  restartBtn.dataset.action = 'restart';

  const killBtn = iconButton({
    icon: 'x',
    label: `Kill ${s.name ?? 'session'}`,
    onClick: (e) => {
      e.stopPropagation();
      o.onKill();
    },
  });
  killBtn.dataset.action = 'kill';

  // rotate first, x second — patterns.md › Exited sessions. Restart is
  // only offered where it means something; a running session's restart is
  // the tile's job, not a one-click sidebar action.
  actions.append(minBtn);
  if (o.state === 'exited' || o.state === 'error') actions.append(restartBtn);
  actions.append(killBtn);

  // The colour picker keeps its native input (components.md › Form fields)
  // and sits outside the hover swap: it is data, not an action.
  const swatch = document.createElement('span');
  swatch.className = 'hv-session-row__swatch';
  const colorInput = document.createElement('input');
  colorInput.type = 'color';
  colorInput.value = s.color || '#888888';
  colorInput.setAttribute('aria-label', `Colour for ${s.name ?? 'session'}`);
  colorInput.addEventListener('input', () => o.onColor(colorInput.value));
  swatch.append(colorInput);

  li.append(st, text, meta, actions, swatch);

  li.addEventListener('click', (e) => {
    // The swatch opens the native picker; it must not also switch sessions.
    if (e.target === colorInput || e.target === swatch) return;
    o.onSelect();
  });

  applyState(li, s, o);
  return li;
}

// updateSessionRow patches an existing row. Exists for the same reason
// app/sidebar.ts's updateSidebarSelection does: a full rebuild replaces the
// <li> between two clicks and eats the dblclick pair that starts a rename.
export function updateSessionRow(
  el: HTMLLIElement,
  s: SessionInfo,
  next: SessionRowState,
): void {
  applyState(el, s, next);
}

function applyState(
  el: HTMLLIElement,
  s: SessionInfo,
  next: SessionRowState,
): void {
  el.dataset.state = next.state;
  if (next.selected) el.dataset.selected = '';
  else delete el.dataset.selected;
  if (next.minimized) el.dataset.minimized = '';
  else delete el.dataset.minimized;

  const name = el.querySelector<HTMLElement>('.hv-session-row__name');
  // Absent while an inline rename has swapped the label for its <input>.
  if (name) name.textContent = s.name ?? '';

  const sub = el.querySelector<HTMLElement>('.hv-session-row__sub');
  if (sub) {
    const t = subtitleFor(s, next.state);
    sub.textContent = t;
    sub.title = t;
  }

  const st = el.querySelector<SVGSVGElement>('.hv-session-row__state');
  if (st) updateStateIcon(st, next.state);

  const minBtn = el.querySelector<HTMLButtonElement>('[data-action="minimize"]');
  if (minBtn) {
    const label = `${next.minimized ? 'Restore' : 'Minimize'} ${s.name ?? 'session'}`;
    minBtn.setAttribute('aria-label', label);
    minBtn.title = label;
    minBtn.replaceChildren(icon(next.minimized ? 'plus' : 'minus'));
  }

  const meta = el.querySelector<HTMLElement>('.hv-session-row__meta');
  const existing = el.querySelector('.hv-kbd');
  if (existing) existing.remove();
  if (meta && next.index !== null) meta.prepend(kbd(`[${next.index}]`));
}
```

- [ ] **Step 4: Write `src/theme/components/session-row.css`**

```css
/* Session row — docs/design-docs/ui/components.md › sessionRow.
   --session-color is data (a user-picked colour), not a token. */
.hv-session-row {
  position: relative;
  display: grid;
  grid-template-columns: 14px minmax(0, 1fr) auto auto;
  align-items: center;
  gap: var(--space-2);
  min-height: 40px;
  padding: 0 var(--space-2) 0 var(--space-3);
  color: var(--fg-muted);
  font-size: var(--text-md);
  cursor: pointer;
}
.hv-session-row:hover { background: var(--hover); color: var(--fg); }

/* Selection: --sel ground + a 2px accent bar at the left edge. Attention
   never uses either channel (patterns.md › Selection vs attention). */
.hv-session-row[data-selected] {
  background: var(--sel);
  color: var(--fg);
  font-weight: 500;
}
.hv-session-row[data-selected]::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 2px;
  background: var(--accent);
}

.hv-session-row__text {
  display: flex;
  flex-direction: column;
  justify-content: center;
  min-width: 0;
  gap: 1px;
}
.hv-session-row__name,
.hv-session-row__sub {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.hv-session-row__sub {
  font-size: var(--text-sm);
  color: var(--fg-subtle);
  line-height: 1.4;
}

.hv-session-row[data-state='attention'] .hv-session-row__name {
  color: var(--state-attention);
  font-weight: 500;
}
/* Exited stays in the list, dimmed and struck through, until killed
   (patterns.md › Exited sessions). Error colours the icon only, so a
   column of failures does not turn the sidebar red. */
.hv-session-row[data-state='exited'] .hv-session-row__name,
.hv-session-row[data-state='error'] .hv-session-row__name {
  color: var(--fg-subtle);
  text-decoration: line-through;
}

.hv-session-row__meta,
.hv-session-row__actions {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  color: var(--fg-subtle);
}
.hv-session-row__actions { display: none; }
.hv-session-row__agent {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
}

/* Actions replace the meta column rather than pushing the text
   (patterns.md › Hover-revealed actions). Keyboard focus counts as hover
   so the buttons are reachable without a mouse. */
.hv-session-row:hover .hv-session-row__meta,
.hv-session-row:focus-within .hv-session-row__meta,
.hv-session-row[data-minimized] .hv-session-row__meta { display: none; }
.hv-session-row:hover .hv-session-row__actions,
.hv-session-row:focus-within .hv-session-row__actions,
.hv-session-row[data-minimized] .hv-session-row__actions { display: flex; }

.hv-session-row__swatch {
  position: relative;
  width: 12px;
  height: 12px;
  flex-shrink: 0;
  border-radius: var(--radius-sm);
  background: var(--session-color, var(--fg-subtle));
  cursor: pointer;
}
.hv-session-row__swatch input[type='color'] {
  position: absolute;
  inset: 0;
  opacity: 0;
  cursor: pointer;
}

/* Inline rename: same metrics as line 1, so the row does not jump. */
.hv-session-row .name-input {
  width: 100%;
  min-width: 0;
  padding: 2px var(--space-1);
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-family: inherit;
  font-size: var(--text-md);
  outline: none;
}

/* Drag-to-reorder. Classes, not data attributes: they are transient
   drag chrome, not row state. */
.hv-session-row.dragging { opacity: 0.45; }
.hv-session-row.drop-above::after,
.hv-session-row.drop-below::after {
  content: '';
  position: absolute;
  left: var(--space-1);
  right: var(--space-1);
  height: 2px;
  border-radius: 1px;
  background: var(--accent);
  pointer-events: none;
}
.hv-session-row.drop-above::after { top: -1px; }
.hv-session-row.drop-below::after { bottom: -1px; }

.hv-session-row button:focus-visible,
.hv-session-row input:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
```

- [ ] **Step 5: Link the stylesheet** — add `<link rel="stylesheet" href="./src/theme/components/session-row.css"/>` next to `chip.css` in `index.html`.

- [ ] **Step 6: Run**

Run: `npx vitest run test/dom/ui-session-row.test.ts && npm run typecheck && npx biome ci . && ../../../scripts/ui-lint.sh --strict`
Expected: 10 passed, everything else green.

- [ ] **Step 7: Commit**

```bash
git add cmd/hivegui/frontend/src/ui/session-row.ts \
        cmd/hivegui/frontend/src/theme/components/session-row.css \
        cmd/hivegui/frontend/test/dom/ui-session-row.test.ts \
        cmd/hivegui/frontend/index.html
git commit -m "feat(ui): add two-line sessionRow primitive"
```

---

### Task 3: `projectCard` primitive

**Files:**
- Create: `cmd/hivegui/frontend/src/ui/project-card.ts`
- Create: `cmd/hivegui/frontend/src/theme/components/project-card.css`
- Create: `cmd/hivegui/frontend/test/dom/ui-project-card.test.ts`
- Modify: `cmd/hivegui/frontend/index.html` (link block)

**Interfaces:**
- Produces:
  ```ts
  export interface ProjectCardState {
    collapsed: boolean;
    active: boolean;
    attention: boolean;
    sessionCount: number;
    attentionCount: number;
  }
  export interface ProjectCardOpts extends ProjectCardState {
    project: ProjectInfo;
    onSelect: () => void;
    onToggleCollapse: () => void;
    onNewSession: () => void;
    onMinimize: () => void;
    onWorktrees: () => void;
    onEdit: () => void;
    onDelete: () => void;
  }
  export function projectCard(o: ProjectCardOpts): {
    root: HTMLLIElement;
    header: HTMLElement;
    body: HTMLUListElement;
    name: HTMLElement;
  };
  ```
  The four returned nodes are exactly what `sidebar.ts` needs and nothing more: `body` to append rows into, `header` for the drag hit-test (its bounds, not the whole tall `li`), `name` for the rename mount and the dblclick target check.
- No `index` / `kbd` here — see Global Constraints › deviation 1.

- [ ] **Step 1: Write the failing DOM test**

`test/dom/ui-project-card.test.ts`:

```ts
// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { projectCard } from '../../src/ui/project-card';
import type { ProjectInfo } from '../../src/app/state';

const noop = () => {};
const make = (over: Partial<Parameters<typeof projectCard>[0]> = {}) =>
  projectCard({
    project: { id: 'p1', name: 'hive', color: '#0af' } as ProjectInfo,
    collapsed: false,
    active: false,
    attention: false,
    sessionCount: 2,
    attentionCount: 0,
    onSelect: noop,
    onToggleCollapse: noop,
    onNewSession: noop,
    onMinimize: noop,
    onWorktrees: noop,
    onEdit: noop,
    onDelete: noop,
    ...over,
  });

describe('projectCard', () => {
  it('renders name, swatch colour and data-pid', () => {
    const { root, name } = make();
    expect(root.className).toBe('hv-project-card');
    expect(root.dataset.pid).toBe('p1');
    expect(name.textContent).toBe('hive');
    expect(root.style.getPropertyValue('--project-color')).toBe('#0af');
  });

  it('shows the session count expanded and "n sessions · k need you" collapsed', () => {
    expect(
      make().root.querySelector('.hv-project-card__count')?.textContent,
    ).toBe('2');
    const collapsed = make({ collapsed: true, attentionCount: 1 });
    expect(collapsed.root.dataset.collapsed).toBe('');
    expect(
      collapsed.root.querySelector('.hv-project-card__count')?.textContent,
    ).toBe('2 sessions · 1 needs you');
    expect(
      make({ collapsed: true, sessionCount: 1, attentionCount: 0 }).root.querySelector(
        '.hv-project-card__count',
      )?.textContent,
    ).toBe('1 session');
  });

  it('carries attention as data-state and nothing else changes on the header', () => {
    const { root } = make({ attention: true });
    expect(root.dataset.state).toBe('attention');
    expect(root.dataset.selected).toBeUndefined();
  });

  it('marks the active project with data-active, a separate channel from attention', () => {
    expect(make({ active: true }).root.dataset.active).toBe('');
    expect(make().root.dataset.active).toBeUndefined();
  });

  it('gives the chevron aria-expanded and toggles through the callback', () => {
    const onToggleCollapse = vi.fn();
    const { root } = make({ onToggleCollapse });
    const chev = root.querySelector<HTMLButtonElement>(
      '.hv-project-card__chevron',
    );
    expect(chev?.getAttribute('aria-expanded')).toBe('true');
    chev?.click();
    expect(onToggleCollapse).toHaveBeenCalledTimes(1);
    expect(
      make({ collapsed: true }).root
        .querySelector('.hv-project-card__chevron')
        ?.getAttribute('aria-expanded'),
    ).toBe('false');
  });

  it('wires all five header actions and none of them selects the project', () => {
    const spies = {
      onSelect: vi.fn(),
      onNewSession: vi.fn(),
      onMinimize: vi.fn(),
      onWorktrees: vi.fn(),
      onEdit: vi.fn(),
      onDelete: vi.fn(),
    };
    const { root, header } = make(spies);
    document.body.append(root);
    for (const a of ['new', 'minimize', 'worktrees', 'edit', 'delete']) {
      root.querySelector<HTMLButtonElement>(`[data-action="${a}"]`)?.click();
    }
    expect(spies.onNewSession).toHaveBeenCalledTimes(1);
    expect(spies.onMinimize).toHaveBeenCalledTimes(1);
    expect(spies.onWorktrees).toHaveBeenCalledTimes(1);
    expect(spies.onEdit).toHaveBeenCalledTimes(1);
    expect(spies.onDelete).toHaveBeenCalledTimes(1);
    expect(spies.onSelect).not.toHaveBeenCalled();
    header.click();
    expect(spies.onSelect).toHaveBeenCalledTimes(1);
    root.remove();
  });

  it('returns a body list that is the append target for rows', () => {
    const { root, body } = make();
    expect(body.tagName).toBe('UL');
    expect(body.parentElement).toBe(root);
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run test/dom/ui-project-card.test.ts`
Expected: FAIL — cannot resolve `../../src/ui/project-card`.

- [ ] **Step 3: Implement `src/ui/project-card.ts`**

```ts
// Project card — docs/design-docs/ui/components.md › projectCard.
//
// A raised card per project: 30px header (chevron, colour swatch, name,
// count, hover actions) over a body that holds the session rows. Attention
// on any child session bubbles to the header swatch and, when collapsed,
// to the count (patterns.md › Attention bubbling).
//
// Returns the nodes app/sidebar.ts needs to do its own job: the body to
// fill, the header to anchor the drag hit-test on, the name to mount an
// inline rename into.
import { icon } from './icon.js';
import { iconButton } from './icon-button.js';
import type { ProjectInfo } from '../app/state.js';

export interface ProjectCardState {
  collapsed: boolean;
  active: boolean;
  attention: boolean;
  sessionCount: number;
  attentionCount: number;
}

export interface ProjectCardOpts extends ProjectCardState {
  project: ProjectInfo;
  onSelect: () => void;
  onToggleCollapse: () => void;
  onNewSession: () => void;
  onMinimize: () => void;
  onWorktrees: () => void;
  onEdit: () => void;
  onDelete: () => void;
}

function countText(o: ProjectCardState): string {
  if (!o.collapsed) return String(o.sessionCount);
  const n = `${o.sessionCount} session${o.sessionCount === 1 ? '' : 's'}`;
  if (o.attentionCount === 0) return n;
  return `${n} · ${o.attentionCount} need${o.attentionCount === 1 ? 's' : ''} you`;
}

export function projectCard(o: ProjectCardOpts): {
  root: HTMLLIElement;
  header: HTMLElement;
  body: HTMLUListElement;
  name: HTMLElement;
} {
  const p = o.project;
  const root = document.createElement('li');
  root.className = 'hv-project-card';
  root.dataset.pid = p.id;
  root.draggable = true;
  if (o.collapsed) root.dataset.collapsed = '';
  if (o.active) root.dataset.active = '';
  if (o.attention) root.dataset.state = 'attention';
  if (p.color) root.style.setProperty('--project-color', p.color);

  const header = document.createElement('div');
  header.className = 'hv-project-card__header';

  // A real <button> so the chevron is keyboard-operable and can carry
  // aria-expanded.
  const chevron = document.createElement('button');
  chevron.type = 'button';
  chevron.className = 'hv-project-card__chevron';
  chevron.setAttribute('aria-expanded', String(!o.collapsed));
  chevron.setAttribute(
    'aria-label',
    `${o.collapsed ? 'Expand' : 'Collapse'} ${p.name ?? 'project'}`,
  );
  chevron.append(icon(o.collapsed ? 'chevron-right' : 'chevron-down'));
  chevron.addEventListener('click', (e) => {
    e.stopPropagation();
    o.onToggleCollapse();
  });

  const swatch = document.createElement('span');
  swatch.className = 'hv-project-card__swatch';

  const name = document.createElement('span');
  name.className = 'hv-project-card__name';
  name.textContent = p.name ?? '';
  name.title = p.cwd ? `${p.name} — ${p.cwd}` : (p.name ?? '');

  const count = document.createElement('span');
  count.className = 'hv-project-card__count';
  count.textContent = countText(o);

  const actions = document.createElement('span');
  actions.className = 'hv-project-card__actions';
  const act = (
    name_: string,
    ic: string,
    label: string,
    fn: () => void,
  ): HTMLButtonElement => {
    const b = iconButton({
      icon: ic,
      label,
      onClick: (e) => {
        e.stopPropagation();
        fn();
      },
    });
    b.dataset.action = name_;
    return b;
  };
  actions.append(
    act('new', 'plus', `New session in ${p.name ?? 'project'}`, o.onNewSession),
    // The binding is shown inline, per AGENTS.md › Key Discoverability.
    act('worktrees', 'branch', `Worktrees in ${p.name ?? 'project'} (⌘E)`, o.onWorktrees),
    act('edit', 'settings', `Edit ${p.name ?? 'project'}`, o.onEdit),
    act('minimize', 'minus', `Minimize ${p.name ?? 'project'}`, o.onMinimize),
    act('delete', 'x', `Delete ${p.name ?? 'project'}`, o.onDelete),
  );

  header.append(chevron, swatch, name, count, actions);
  header.addEventListener('click', (e) => {
    // Only the row background, swatch or name selects. Every control stops
    // propagation in its own handler; this is the belt to that's braces.
    const t = e.target;
    if (!(t instanceof Element)) return;
    if (t.closest('.hv-project-card__actions') || t.closest('.hv-project-card__chevron')) {
      return;
    }
    o.onSelect();
  });

  const body = document.createElement('ul');
  body.className = 'hv-project-card__body';

  root.append(header, body);
  return { root, header, body, name };
}
```

- [ ] **Step 4: Write `src/theme/components/project-card.css`**

```css
/* Project card — docs/design-docs/ui/components.md › projectCard.
   --project-color is data (a user-picked colour), not a token. */
.hv-project-card {
  position: relative;
  list-style: none;
  margin: var(--space-1) var(--space-2) var(--space-2);
  background: var(--surface-raised);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
}
.hv-project-card[data-active] { border-color: color-mix(in srgb, var(--accent) 45%, var(--border)); }

.hv-project-card__header {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 30px;
  padding: 0 var(--space-2);
  color: var(--fg);
  font-size: var(--text-md);
  font-weight: 500;
  cursor: pointer;
  user-select: none;
  min-width: 0;
}
.hv-project-card__header:hover { background: var(--hover); }

.hv-project-card__chevron {
  display: flex;
  align-items: center;
  flex-shrink: 0;
  padding: 0;
  background: none;
  border: 0;
  color: var(--fg-subtle);
  cursor: pointer;
}

.hv-project-card__swatch {
  width: 8px;
  height: 8px;
  flex-shrink: 0;
  border-radius: 2px;
  background: var(--project-color, var(--fg-subtle));
}
/* Attention bubbles to the swatch and nothing else on the header
   (patterns.md › Attention bubbling). */
.hv-project-card[data-state='attention'] .hv-project-card__swatch {
  animation: hv-card-pulse var(--motion-pulse) ease-in-out infinite;
}
@keyframes hv-card-pulse {
  0%, 100% { box-shadow: 0 0 0 0 transparent; }
  50% { box-shadow: 0 0 0 3px color-mix(in srgb, var(--state-attention) 50%, transparent); }
}

.hv-project-card__name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.hv-project-card__count {
  flex-shrink: 0;
  color: var(--fg-subtle);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  font-variant-numeric: tabular-nums;
}
.hv-project-card[data-state='attention'][data-collapsed] .hv-project-card__count {
  color: var(--state-attention);
}

.hv-project-card__actions {
  display: none;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
}
.hv-project-card__header:hover .hv-project-card__count,
.hv-project-card__header:focus-within .hv-project-card__count { display: none; }
.hv-project-card__header:hover .hv-project-card__actions,
.hv-project-card__header:focus-within .hv-project-card__actions { display: flex; }

.hv-project-card__body {
  list-style: none;
  margin: 0;
  padding: 0 0 var(--space-1);
}
.hv-project-card[data-collapsed] .hv-project-card__body { display: none; }

.hv-project-card .project-name-input {
  flex: 1;
  min-width: 0;
  padding: 2px var(--space-1);
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-family: inherit;
  font-size: var(--text-md);
  outline: none;
}

.hv-project-card.dragging { opacity: 0.45; }
.hv-project-card.drop-above::after,
.hv-project-card.drop-below::after {
  content: '';
  position: absolute;
  left: var(--space-1);
  right: var(--space-1);
  height: 2px;
  border-radius: 1px;
  background: var(--accent);
  pointer-events: none;
  z-index: 2;
}
.hv-project-card.drop-above::after { top: -1px; }
.hv-project-card.drop-below::after { bottom: -1px; }

.hv-project-card button:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
```

- [ ] **Step 5: Link the stylesheet** — add `<link rel="stylesheet" href="./src/theme/components/project-card.css"/>` to `index.html`.

- [ ] **Step 6: Run**

Run: `npx vitest run test/dom/ui-project-card.test.ts && npm run typecheck && npx biome ci . && ../../../scripts/ui-lint.sh --strict`
Expected: 7 passed, everything else green.

- [ ] **Step 7: Commit**

```bash
git add cmd/hivegui/frontend/src/ui/project-card.ts \
        cmd/hivegui/frontend/src/theme/components/project-card.css \
        cmd/hivegui/frontend/test/dom/ui-project-card.test.ts \
        cmd/hivegui/frontend/index.html
git commit -m "feat(ui): add projectCard primitive"
```

---

### Task 4: Rebuild `sidebar.ts` on the primitives

The behaviour-preserving core of the phase. Every listener that exists today survives; only the markup construction moves.

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/sidebar.ts` — `renderProject` (line ~209), `renderSession` (line ~417), `updateSidebarSelection` (line ~150), `updateSidebarTitles` (line ~187), `applyTitle` (line ~199, deleted), `beginRenameSession` / `beginRenameProject` (lines ~638/~654)
- Modify: `cmd/hivegui/frontend/src/style.css` — delete the superseded blocks
- Modify: `cmd/hivegui/frontend/src/app/state.ts` — nothing (listed only to confirm no state shape changes)

**Interfaces:**
- `renderSidebar()`, `updateSidebarSelection()`, `updateSidebarTitles()`, `initSidebar(deps)`, `SidebarDeps` keep their exact signatures. `SidebarDeps` gains nothing: restart and kill go straight to the bridge, like `UpdateSession` already does.

- [ ] **Step 1: Replace `renderSession`**

```ts
function renderSession(s: SessionInfo, index: number | null): HTMLLIElement {
  const li = sessionRow({
    session: s,
    state: sessionState(s, state.attention.has(s.id)),
    selected: s.id === state.activeId,
    minimized: state.minimized.has(s.id),
    index,
    onSelect: () => deps.switchTo(s.id),
    onMinimize: () => deps.minimizeSession(s.id),
    onRestore: () => deps.restoreSession(s.id),
    onRestart: () =>
      RestartSession(s.id).catch(reportFailure('restart session')),
    onKill: () => killSession(s),
    onWorktrees: () => {
      const proj = state.projects.find((p) => p.id === readProjectId(s));
      if (proj) openWorktrees(proj);
    },
    onColor: (hex) =>
      UpdateSession(s.id, '', hex, -1).catch(reportFailure('color change')),
  });

  const name = li.querySelector<HTMLElement>('.hv-session-row__name');
  if (name) {
    li.addEventListener('dblclick', () => beginRenameSession(s, name));
  }
  wireSessionDrag(li, s);
  return li;
}

// killSession routes a live session through the native confirm (AGENTS.md:
// destructive actions never skip it) and a dead one straight through, which
// is the rule session-term.ts's _closeDead already follows — there is
// nothing left to lose once the process is gone.
function killSession(s: SessionInfo) {
  const alive = s.alive !== false;
  if (!alive) {
    KillSession(s.id, true).catch(reportFailure('kill session'));
    return;
  }
  Confirm(
    'Kill session',
    `Kill ${s.name ?? 'this session'}? Its scrollback is lost.`,
  )
    .then((ok) => {
      if (ok) KillSession(s.id, true).catch(reportFailure('kill session'));
    })
    .catch(reportFailure('kill session'));
}
```

Add `RestartSession`, `KillSession`, `Confirm` to the `../bridge.js` import at the top of the file (line 8) `sessionState` from `../lib/session-state.js`, and `sessionRow` + `updateSessionRow` from `../ui/session-row.js`. Check `Confirm`'s real signature in `src/bridge.ts` before writing this — if it takes a single message string, drop the title argument.

- [ ] **Step 2: Move the session drag handlers into `wireSessionDrag`**

Cut the four listeners from today's `renderSession` (`dragstart`/`dragend`/`dragover`/`dragleave`/`drop`, lines ~532–573) verbatim into a `function wireSessionDrag(li: HTMLLIElement, s: SessionInfo)`, changing only the two selector strings in `dragend`:

```ts
document
  .querySelectorAll('.hv-session-row.drop-above, .hv-session-row.drop-below')
  .forEach((el) => el.classList.remove('drop-above', 'drop-below'));
```

`reorderDroppedSession` is untouched.

- [ ] **Step 3: Replace `renderProject`**

```ts
function renderProject(
  p: ProjectInfo,
  activePID: string,
  indexOf: (id: string) => number | null,
): HTMLLIElement {
  const sessions = state.sessions
    .filter((s) => readProjectId(s) === p.id)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  const attentionCount = sessions.filter((s) =>
    state.attention.has(s.id),
  ).length;
  const collapsed = state.collapsed.has(p.id);

  const { root, header, body, name } = projectCard({
    project: p,
    collapsed,
    active: p.id === activePID,
    attention: attentionCount > 0,
    sessionCount: sessions.length,
    attentionCount,
    onSelect: () => deps.switchToProject(p.id),
    onToggleCollapse: () => {
      if (state.collapsed.has(p.id)) state.collapsed.delete(p.id);
      else state.collapsed.add(p.id);
      saveCollapsed();
      renderSidebar();
    },
    onNewSession: () => openLauncher(p.id),
    onMinimize: () => deps.minimizeProject(p.id),
    onWorktrees: () => openWorktrees(p),
    onEdit: () => openProjectEditor(p),
    onDelete: () => deps.confirmAndDeleteProject(p),
  });

  header.addEventListener('dblclick', (e) => {
    if (e.target === name || e.target === header) beginRenameProject(p, name);
  });

  for (const s of sessions) body.appendChild(renderSession(s, indexOf(s.id)));

  wireProjectDrag(root, header, p);
  return root;
}
```

`wireProjectDrag` is today's five project listeners (lines ~330–390) moved verbatim, with `header` passed in for the bounds hit-test and the `dragstart` guards retargeted:

```ts
if (t.closest('.hv-session-row')) return;               // bubbled inner drag
if (t.closest('.hv-project-card__actions') || t.closest('.project-name-input')) {
  e.preventDefault();
  return;
}
```
and `dragend`'s query changed to `.hv-project-card.drop-above, .hv-project-card.drop-below`.

- [ ] **Step 4: Feed the `[n]` hint from the real ⌘1–9 mapping**

`keyboard.ts` resolves ⌘n against `orderedSessions()[n-1]`, so the hint must come from the same list — not from a per-project counter, which would label rows with keys that jump elsewhere.

```ts
export function renderSidebar() {
  projectsUL.innerHTML = '';
  const activePID = activeProjectId();
  // ⌘1–9 selects orderedSessions()[n-1] (app/keyboard.ts). The hint has to
  // be read off the same list or it advertises the wrong key.
  const hints = new Map<string, number>();
  orderedSessions()
    .slice(0, 9)
    .forEach((s, i) => hints.set(s.id, i + 1));
  const indexOf = (id: string) => hints.get(id) ?? null;
  for (const p of state.projects) {
    if (state.minimizedProjects.has(p.id)) continue;
    projectsUL.appendChild(renderProject(p, activePID, indexOf));
  }
  renderMinimizedProjects(activePID);
  deps.renderEmptyState();
}
```

Import `orderedSessions` from `./selectors.js` (it already exports `activeProjectId` from there).

- [ ] **Step 5: Rewrite the two in-place patch paths**

Delete `applyTitle`; `updateSessionRow` owns the subtitle now. Both functions keep their names, exports and call sites.

```ts
export function updateSidebarSelection() {
  const activePID = activeProjectId();
  for (const el of projectsUL.querySelectorAll<HTMLElement>('.hv-project-card')) {
    const pid = el.dataset.pid ?? '';
    if (pid === activePID) el.dataset.active = '';
    else delete el.dataset.active;
    // Attention on a card is the union of its sessions': it has to be
    // recomputed here too, or a bell that arrives without a rebuild leaves
    // a collapsed card silent.
    if (projectHasAttention(pid)) el.dataset.state = 'attention';
    else delete el.dataset.state;
  }
  for (const el of minimizedProjectsTray?.querySelectorAll<HTMLElement>(
    '.hv-chip[data-pid]',
  ) ?? []) {
    const pid = el.dataset.pid ?? '';
    if (pid === activePID) el.dataset.active = '';
    else delete el.dataset.active;
    if (projectHasAttention(pid)) el.dataset.state = 'attention';
    else delete el.dataset.state;
  }
  patchRows();
  deps.renderEmptyState();
}

export function updateSidebarTitles() {
  patchRows();
}

// patchRows re-applies every row's state from `state` without rebuilding a
// node — the invariant updateSidebarSelection was created for: a rebuild
// between two clicks replaces the <li> and the dblclick pair that starts a
// rename never forms.
function patchRows() {
  const hints = new Map<string, number>();
  orderedSessions()
    .slice(0, 9)
    .forEach((s, i) => hints.set(s.id, i + 1));
  for (const el of projectsUL.querySelectorAll<HTMLLIElement>('.hv-session-row')) {
    const s = state.sessions.find((x) => x.id === el.dataset.sid);
    if (!s) continue;
    updateSessionRow(el, s, {
      state: sessionState(s, state.attention.has(s.id)),
      selected: s.id === state.activeId,
      minimized: state.minimized.has(s.id),
      index: hints.get(s.id) ?? null,
    });
  }
}
```

- [ ] **Step 6: Point the renames at the new parts**

`beginRenameSession(sess, nameEl)` loses its unused `_li` parameter; both functions keep `className: 'name-input'` / `'project-name-input'` — the two component stylesheets style those classes, so the mount/unmount dance in `inline-rename.ts` needs no change at all.

- [ ] **Step 7: Delete the superseded CSS**

From `src/style.css`, remove: `.project`, `.project::before`, `.project.active::before`, `.project-header` and every `.project-header *` rule, `.project-sessions`, `.project.collapsed *`, `.session-item` and every `.session-item *` rule, `.session-minimize`, `@keyframes hive-attention-pulse` **only if** `.min-project-chip.attention` no longer uses it after Task 5 (it does not — Task 5 replaces that chip), `.project.dragging`, `.project.drop-*`, `.worktree-glyph*` (lines ~1131–1145 and ~2073–2081), and the `.session-item.closing/.starting` rules near line ~1634. Remove the now-dangling selectors from the shared `:focus-visible` list at line ~1832 (`.session-minimize`, `.project-header .caret`, `.project-header .project-actions button`).

Grep before deleting each block: `grep -n "session-item\|project-header\|worktree-glyph" src/style.css` should return nothing when this step is done.

- [ ] **Step 8: Run the whole local gate**

Run: `npx vitest run && npm run typecheck && npx biome ci . && ../../../scripts/ui-lint.sh --strict`
Expected: typecheck/biome/ui-lint green. `test/dom/sidebar-title.test.ts` and `test/dom/minimize-project.test.ts` **fail** here — they query the old selectors. That is expected; Task 7 fixes them. Do not fix them by re-adding old classes.

- [ ] **Step 9: Commit**

```bash
git add cmd/hivegui/frontend/src/app/sidebar.ts cmd/hivegui/frontend/src/style.css
git commit -m "refactor(sidebar): rebuild project and session rows on the ui primitives"
```

---

### Task 5: Both minimized trays on `chip`

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/sidebar.ts` — `renderMinimizedProjects` (line ~76), `renderProjectChip` (line ~100)
- Modify: `cmd/hivegui/frontend/src/app/view.ts` — `renderMinimizedTray` (line ~597)
- Modify: `cmd/hivegui/frontend/index.html` — `#minimized-projects` element (line ~43)
- Modify: `cmd/hivegui/frontend/src/app/dom.ts:20` — the exported handle, if `pageEl` asserts a tag type
- Modify: `cmd/hivegui/frontend/src/style.css` — delete `#minimized-projects .min-project-*` (lines ~144–232) and `#minimized-tray .min-chip*` (lines ~859–895); keep the `#minimized-tray` / `#minimized-projects` container rules, retokenised

- [ ] **Step 1: Turn the project tray into a toolbar**

`index.html`:

```html
<!-- Projects the user minimized out of the list above. Pinned to the
     bottom of the sidebar, name only, one restore control each.
     Hidden (display: none) while the set is empty. -->
<div id="minimized-projects" class="hidden" role="toolbar" aria-label="Minimized projects"></div>
```

In `style.css`, drop `list-style` / `margin` from the `#minimized-projects` block; everything else about it (the `flex: 0 0 auto`, `max-height: 30vh`, `border-top`, column flex, gap) stays — the geometry assertions in `minimize-project.spec.ts` depend on it.

- [ ] **Step 2: Replace `renderProjectChip`**

```ts
function renderProjectChip(p: ProjectInfo, activePID: string): HTMLSpanElement {
  const el = chip({
    label: p.name ?? '',
    color: p.color,
    active: p.id === activePID,
    title: p.cwd ? `${p.name} — ${p.cwd}` : (p.name ?? ''),
    ariaLabel: `Restore ${p.name}`,
    // Clicking the chip body restores the project — the same thing the
    // restore control does. A minimized row is a thing you put away; the
    // only reason to click it is to get it back.
    onClick: () => deps.restoreProject(p.id),
    onRestore: () => deps.restoreProject(p.id),
    restoreLabel: `Restore ${p.name}`,
  });
  el.dataset.pid = p.id;
  // The chip is the only surface left carrying a bell for a project whose
  // rows are gone (patterns.md › Attention bubbling).
  if (projectHasAttention(p.id)) el.dataset.state = 'attention';
  return el;
}
```

`renderMinimizedProjects` keeps its body verbatim except `tray.appendChild(...)` now takes the span.

- [ ] **Step 3: Replace `renderMinimizedTray` in `view.ts`**

```ts
export function renderMinimizedTray() {
  const tray = document.getElementById('minimized-tray');
  if (!tray) return;
  tray.innerHTML = '';
  if (state.minimized.size === 0) {
    tray.classList.add('hidden');
    renderEmptyState();
    return;
  }
  tray.classList.remove('hidden');
  // Display order, so the chip row reads left-to-right like the sidebar
  // reads top-to-bottom.
  for (const info of orderedSessions().filter((s) =>
    state.minimized.has(s.id),
  )) {
    const proj = state.projects.find((p) => p.id === readProjectId(info));
    const el = chip({
      label: info.name ?? '',
      sublabel: proj?.name,
      color: info.color,
      state: sessionState(info, state.attention.has(info.id)),
      ariaLabel: `Restore ${info.name}`,
      onClick: () => restoreSession(info.id),
    });
    el.dataset.sid = info.id;
    tray.append(el);
  }
  // Minimize/restore changes which sessions are visible without a sidebar
  // render — re-evaluate the empty state here too.
  renderEmptyState();
}
```

Note the early-return now also calls `renderEmptyState()`; today's version returns without it, which is a latent bug on the "last chip restored" path (the tray hides, the empty state is stale until the next repaint). Keep the fix.

- [ ] **Step 4: Retokenise the two container rules**

`#minimized-tray` keeps `display: flex; gap: var(--space-2); padding: var(--space-1) var(--space-2)` and `border-top: 1px solid var(--border)` (it currently uses `--sel` for the border — a hover token doing a rule's job); `min-height: 28px` becomes `min-height: 32px` so a 24px chip plus padding fits without the row growing on hover.

- [ ] **Step 5: Run**

Run: `npx vitest run && npm run typecheck && npx biome ci . && ../../../scripts/ui-lint.sh --strict`
Expected: same as Task 4 — green except the two DOM tests Task 7 migrates.

- [ ] **Step 6: Commit**

```bash
git add cmd/hivegui/frontend/src/app/sidebar.ts cmd/hivegui/frontend/src/app/view.ts \
        cmd/hivegui/frontend/index.html cmd/hivegui/frontend/src/style.css \
        cmd/hivegui/frontend/src/app/dom.ts
git commit -m "refactor(sidebar): render both minimized trays with the chip primitive"
```

---

### Task 6: Sidebar min width 200 → 220

Project cards carry a horizontal margin the old flat rows did not; below 220 the two-line row's meta column starts clipping the name.

**Files:**
- Modify: `cmd/hivegui/frontend/src/style.css:46` — `grid-template-columns: var(--sidebar-width, 200px) 1fr;`
- Modify: `cmd/hivegui/frontend/src/main.ts:265` — `const MIN = 140, MAX = 480;`
- Modify: `cmd/hivegui/frontend/src/main.ts:321` — `const base = Number.isFinite(cur) ? cur : 200;`

- [ ] **Step 1: Raise the three numbers**

```css
grid-template-columns: var(--sidebar-width, 220px) 1fr;
```

```ts
  // 220 is the design system's sidebar floor (docs/design-docs/ui/tokens.md
  // › Spacing): below it a project card's margins eat the two-line row's
  // name column. A width stored below the floor is clamped up on load.
  const MIN = 220,
    MAX = 480;
```

and the keyboard `nudge` fallback `: 200` → `: 220`.

- [ ] **Step 2: Add an e2e guard**

Append to `test/e2e/ux-polish.spec.ts`:

```ts
test('the sidebar cannot be dragged below the 220px design floor', async ({
  page,
}) => {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  const handle = page.locator('#sidebar-resizer');
  const box = await handle.boundingBox();
  if (!box) throw new Error('resizer not laid out');
  await page.mouse.move(box.x + box.width / 2, box.y + 100);
  await page.mouse.down();
  await page.mouse.move(40, box.y + 100, { steps: 5 });
  await page.mouse.up();
  const w = await page
    .locator('#sidebar')
    .evaluate((el) => el.getBoundingClientRect().width);
  expect(w).toBeGreaterThanOrEqual(220);
});
```

- [ ] **Step 3: Run**

Run: `npx playwright test test/e2e/ux-polish.spec.ts && npm run typecheck && npx biome ci .`
Expected: green.

- [ ] **Step 4: Commit**

```bash
git add cmd/hivegui/frontend/src/main.ts cmd/hivegui/frontend/src/style.css \
        cmd/hivegui/frontend/test/e2e/ux-polish.spec.ts
git commit -m "feat(sidebar): raise the sidebar width floor to 220px for project cards"
```

---

### Task 7: Migrate the specs

Mechanical selector migration plus the new assertions the phase earns. No behaviour is being changed here — if a spec needs its *logic* rewritten (not just its selectors), that is a regression in Tasks 4–6, not a spec to loosen.

**Files:**
- Modify: `test/dom/sidebar-title.test.ts`, `test/dom/minimize-project.test.ts`
- Modify: `test/e2e/{minimize,minimize-project,ordering,sidebar-window-title,worktrees,payload-shapes,silent-failures,ux-polish,line-edit-keys,launcher-search,grid-scroll-regressions,focus-traps,theme}.spec.ts`

**Interfaces:**
- Selector mapping (apply everywhere, including inside `page.evaluate` bodies):

| Old | New |
|---|---|
| `.session-item` | `.hv-session-row` |
| `.session-item.selected` | `.hv-session-row[data-selected]` |
| `li[data-sid="x"]` + `toHaveClass(/attention/)` | `[data-sid="x"]` + `toHaveAttribute('data-state','attention')` |
| `.session-item .name` | `.hv-session-row__name` |
| `.session-item .session-title` | `.hv-session-row__sub` |
| `.session-item .swatch` | `.hv-session-row__swatch` |
| `.session-minimize` | `.hv-session-row [data-action="minimize"]` |
| `.worktree-glyph` | `.hv-session-row__worktree` |
| `.project` | `.hv-project-card` |
| `.project.collapsed` | `.hv-project-card[data-collapsed]` |
| `.project-header` | `.hv-project-card__header` |
| `.caret` | `.hv-project-card__chevron` |
| `.project-actions button[aria-label^="Minimize"]` | `.hv-project-card [data-action="minimize"]` |
| `.project-actions button[title*="Worktrees"]` | `.hv-project-card [data-action="worktrees"]` |
| `.min-chip` | `#minimized-tray .hv-chip` |
| `.min-chip-name` | `.hv-chip__label` |
| `.min-chip-project` | `.hv-chip__sub` |
| `.min-project-chip` | `#minimized-projects .hv-chip` |
| `.min-project-open` | `.hv-chip__open` |
| `.min-project-restore` | `.hv-chip__restore` |
| `.min-project-name` | `.hv-chip__label` |

`.name-input` / `.project-name-input` are **unchanged** — the rename inputs keep their class names.

- [ ] **Step 1: Apply the mapping**

Work file by file (a blind repo-wide sed will corrupt `.name` in `${p.name}` template strings — `silent-failures.spec.ts` and `focus-traps.spec.ts` both contain those). After each file:

Run: `npx playwright test test/e2e/<file>.spec.ts`
Expected: same pass count as before the phase.

- [ ] **Step 2: Add the new sidebar assertions to `sidebar-window-title.spec.ts`**

The existing three tests already assert "title below the name, left-aligned, quieter" — they survive the mapping unchanged. Add the fallback-words case, which is new in this phase:

```ts
test('a titleless row shows state words on line 2, not an empty line', async ({
  page,
}) => {
  await boot(page);
  const row = rows(page).first();
  // A freshly booted mock session is ready and titleless.
  await expect(row.locator('.hv-session-row__sub')).toHaveText('');

  // Kill it: the subtitle becomes the state word, and the name is struck
  // through rather than removed (patterns.md › Exited sessions).
  const sid = await row.getAttribute('data-sid');
  await page.evaluate((id) => {
    const s = window.__hive.state?.sessions.find((x) => x.id === id);
    if (!s) throw new Error('no mock session');
    s.alive = false;
    window.__hive.emit(
      'session:event',
      JSON.stringify({ kind: 'exited', session: s }),
    );
  }, sid);

  await expect(row).toHaveAttribute('data-state', 'exited');
  await expect(row.locator('.hv-session-row__sub')).toHaveText('Exited');
  const deco = await row
    .locator('.hv-session-row__name')
    .evaluate((el) => getComputedStyle(el).textDecorationLine);
  expect(deco).toContain('line-through');
});

test('the row is 40px and the key hint shows the number ⌘n actually selects', async ({
  page,
}) => {
  await boot(page, 3);
  const first = rows(page).first();
  const box = await first.boundingBox();
  expect(box?.height).toBeGreaterThanOrEqual(40);
  await expect(first.locator('.hv-kbd')).toHaveText('[1]');

  const secondSid = await rows(page).nth(1).getAttribute('data-sid');
  await page.keyboard.press(`${MOD}+2`);
  await expect(
    page.locator('.hv-session-row[data-selected]'),
  ).toHaveAttribute('data-sid', secondSid ?? '');
});
```

(`MOD` is already defined in the sibling specs; add the same line here.)

- [ ] **Step 3: Add the attention-bubbling assertion to `minimize-project.spec.ts`**

```ts
test('a bell inside a minimized project lights its chip', async ({ page }) => {
  await boot(page);
  const first = page.locator('#projects > li.hv-project-card').first();
  const pid = await first.getAttribute('data-pid');
  const sid = await first
    .locator('.hv-session-row')
    .first()
    .getAttribute('data-sid');

  await first.locator('[data-action="minimize"]').click();
  const chip = page.locator(`#minimized-projects .hv-chip[data-pid="${pid}"]`);
  await expect(chip).toBeVisible();
  await expect(chip).not.toHaveAttribute('data-state', 'attention');

  await page.evaluate((id) => window.__hive.emit('pty:data', id, btoa('\x07')), sid);
  await expect(chip).toHaveAttribute('data-state', 'attention');
});
```

If the bell is ignored because that session is the active+focused one (see `theme.spec.ts`'s note on `onSessionBell`), boot with a second session and minimize the *non*-active project.

- [ ] **Step 4: Run everything**

Run: `npx vitest run && npx playwright test`
Expected: all green, first attempt. A failure in `e2e-real` is not this diff (see the flaky-suite note in the repo memory); a failure anywhere in `test/e2e` is.

- [ ] **Step 5: Commit**

```bash
git add cmd/hivegui/frontend/test
git commit -m "test(sidebar): migrate specs to the primitive selectors and cover two-line rows"
```

---

### Task 8: Screenshot baselines for the new sidebar

**Files:**
- Modify: `cmd/hivegui/frontend/test/e2e/theme.spec.ts`
- Create (generated): `test/e2e/theme.spec.ts-snapshots/sidebar-hive-dark-chromium-darwin.png`, `…/sidebar-hive-light-chromium-darwin.png`

**Interfaces:**
- Same `HIVE_SNAPSHOT=1` gate the existing pixel tests use: baselines are darwin-local, CI runs three platforms, and a darwin PNG fails the linux/windows legs outright.

- [ ] **Step 1: Add the two baselines**

Append to `theme.spec.ts`, reusing the seeding block already in the file (two projects, three sessions, one minimized, one with attention) — extract it into a `seedSidebar(page)` helper and call it from the existing `classic` test too, so the three baselines show the same scene:

```ts
// Phase-3 baselines: the sidebar's first visible redesign. Clipped to
// #sidebar — the terminal area is not what these guard, and masking it
// leaves the diff hostage to xterm's renderer.
for (const preset of ['hive-dark', 'hive-light'] as const) {
  test(`sidebar under ${preset}`, async ({ page }) => {
    test.skip(
      !process.env.HIVE_SNAPSHOT,
      'pixel baselines are darwin-local; run with HIVE_SNAPSHOT=1',
    );
    await page.addInitScript(
      (p) => localStorage.setItem('hive.theme', p),
      preset,
    );
    await page.setViewportSize({ width: 1100, height: 700 });
    await boot(page);
    await seedSidebar(page);
    await expect(page.locator('#sidebar')).toHaveScreenshot(
      `sidebar-${preset}.png`,
      { maxDiffPixels: 0, animations: 'disabled' },
    );
  });
}
```

- [ ] **Step 2: Generate and verify**

Run: `HIVE_SNAPSHOT=1 npx playwright test test/e2e/theme.spec.ts --update-snapshots`
Then: `HIVE_SNAPSHOT=1 npx playwright test test/e2e/theme.spec.ts`
Expected: all pass on the second run.

The `classic` baselines from Phase 1 **will** fail now — the sidebar deliberately changed. Regenerate `sidebar-classic.png` in the same `--update-snapshots` run and say so in the commit body; that snapshot's contract ("classic reproduces v2.4.0 pixel-for-pixel") ends here, and Phase 1's Task 5 note should no longer be read as a standing guard for the sidebar. The `settings-classic.png` baseline is untouched and must still pass unchanged — if it moved, a component stylesheet is leaking outside the sidebar.

- [ ] **Step 3: Look at it**

Run: `wails build && open build/bin/hive.app` (plain `wails build`, never `-s` — `-s` skips the frontend build and the app dies with "no index.html").
Check by eye under all three presets: rows are two lines and 40px, exited rows are struck through and still there, hovering a row swaps meta for actions without the text shifting, the `[n]` hints match what ⌘1–9 does, a collapsed card reads "n sessions · k need you", drag-reorder indicators land where the cursor is, dragging a session inside a card does not start a project drag.

- [ ] **Step 4: Commit**

```bash
git add cmd/hivegui/frontend/test/e2e/theme.spec.ts \
        cmd/hivegui/frontend/test/e2e/theme.spec.ts-snapshots
git commit -m "test(theme): baseline the rebuilt sidebar under hive-dark and hive-light"
```

---

### Task 9: Docs, changeset, plan bookkeeping

**Files:**
- Modify: `docs/design-docs/ui/README.md` (Status line → "Phases 1–3 implemented")
- Modify: `docs/design-docs/ui/components.md` — `projectCard`: replace the `kbd("[n]")` project-number clause with the session-row hint rule and the reason (no project-number binding exists); `sessionRow`: record that hover actions are `minus`, `rotate` (exited/error only), `x`, and that the meta column carries `[n]` for the first nine sessions in global order
- Modify: `docs/design-docs/ui/icons.md` — state resolution: replace `exit_code == 0` with `last_error` (the wire has no exit code)
- Modify: `docs/exec-plans/active/ui-design-system.md` — tick Phase 3 in Progress; add the three decision-log entries below
- Create: `.changesets/<pr>-ui-sidebar.md` via `/hs-changelog-update`

- [ ] **Step 1: Decision-log entries**

```markdown
- **<date>** — `[n]` key hints render on session rows, not project cards. Why: ⌘1–9 selects the nth session in global order (`keyboard.ts`); there is no project-number binding, and a hint for a key that does nothing is worse than none. Revisit if a project-number chord is added.
- **<date>** — Sidebar rows gained restart and kill. Why: patterns.md requires `rotate`/`x` on an exited row, and there was no way to restart a session from the sidebar at all (`RestartSession` was dead code in the frontend). Kill on a live session goes through the native confirm.
- **<date>** — State resolution reads `last_error`, not `exit_code`. Why: the daemon never sends an exit code; `SessionInfo` has `alive` and `last_error` only. Line 2 reads "Exited" or "Exited — <error>".
```

- [ ] **Step 2: Changeset text**

"The sidebar is rebuilt on the design system: two-line rows (name over the live window title, or its state when there is none), project cards, geometric state icons, and `[n]` hints showing which session ⌘1–9 selects. Exited sessions stay in the list, struck through, with restart and kill on hover. Minimum sidebar width is now 220px."

- [ ] **Step 3: Full local gate**

Run:
```bash
cd cmd/hivegui/frontend \
  && npx biome ci . && npm run typecheck && npx vitest run && npx playwright test \
  && cd ../../.. && ./scripts/ui-lint.sh --strict && go build ./...
```
Expected: all green, `ui-lint: 0 violation(s)`.

- [ ] **Step 4: Commit and open the PR**

```bash
git add docs .changesets
git commit -m "docs(ui): mark phase 3 of the design system implemented"
```

PR title: `feat(ui): design-system phase 3 — sessionRow, projectCard, chip (sidebar rebuild)`. Body: link the spec, list the three spec deviations, attach the before/after sidebar screenshots from Task 8, and state that `classic` remains the default preset.

---

## Self-review

- **Spec coverage:** components.md `sessionRow` → Task 2 (40px, `[state][text][meta]`, name/title lines, fallback words, meta column, selection bar, hover actions, inline rename, drag handle); `projectCard` → Task 3 (raised body, 30px header, chevron, swatch, count, hover actions, attention swatch, collapsed count); `chip` → Task 1 (24px, state icon or 7px swatch, label, restore button, attention). patterns.md: selection vs attention → separate channels in `session-row.css` (`--sel` + `--accent` bar vs `--state-attention` on the name and the pulsing icon); attention bubbling → card swatch (Task 3), collapsed count (Task 3), session chip (Task 5), project chip (Task 5), and `updateSidebarSelection` recomputes all four without a rebuild (Task 4 Step 5); exited sessions → struck-through, still listed, `rotate` then `x` (Tasks 2/4); hover actions → replace the meta column, `:focus-within` included; keyboard hints → `kbd` only, `[n]` format, Task 4 Step 4 sources it from the real binding. tokens.md sidebar min width 220 → Task 6. icons.md "no Unicode" → every glyph in the touched code is gone; `ui-lint --strict` is a per-task gate.
- **Deferred, not missed:** `dialog`, `banner`, `statusBar`, grid tile header, launcher rows, empty/phase states are Phase 4/5 per the master plan. The default preset flip is Phase 6. `style.css` is trimmed of what these primitives replace, not split — the split is Phase 6.
- **Placeholders:** none. Every task ships code, a run command and an expected result. Task 7 Step 1 is a per-file procedure with an exhaustive mapping table, not a TODO.
- **Type consistency:** `SessionState` and `sessionState` are imported from Phase 2 in Tasks 1, 2, 4, 5 with the same shape; `SessionRowState` is the single argument type shared by `sessionRow` and `updateSessionRow`; `ProjectCardState` likewise; `chip()` returns `HTMLSpanElement` in all three call sites; `SidebarDeps` is unchanged, so `main.ts`'s injection site needs no edit.
- **Known risk 1 — the dblclick-eating rebuild.** `renderSidebar()` wipes `projectsUL.innerHTML`; the whole reason `updateSidebarSelection` / `updateSidebarTitles` exist is that a rebuild between two clicks kills the rename gesture. Task 4 Step 5 keeps both paths patch-only (`updateSessionRow` never replaces a node). If a rename stops opening on double-click after this phase, something started calling `renderSidebar` on a selection or title event — fix the caller, not the primitive.
- **Known risk 2 — `:focus-within` and the hover swap.** The meta↔actions swap uses `display`, so a focused action button inside a row that loses focus-within disappears mid-interaction. `[data-minimized]` pins the actions open for the one case where that matters today (the restore control is the only way back). If keyboard users report vanishing buttons, the fix is `visibility` + fixed-width columns, not re-adding an always-visible button.
- **Known risk 3 — `classic` under the new sidebar.** `classic` has `--surface-raised: #111` against `--surface: #0a0a0a`, an 11-unit separation; project cards may read as flat there. Acceptable for one phase (the default flips in Phase 6), but if it looks broken in Task 8 Step 3, adjust `classic`'s `--surface-raised` in `themes.css` rather than adding a border-only fallback in `project-card.css`.
- **Known risk 4 — `Confirm()`'s signature.** Task 4 Step 1 assumes `Confirm(title, message): Promise<boolean>`. It is used elsewhere in the app; check `src/bridge.ts` and one existing call site before writing the kill path, and match whatever is there.

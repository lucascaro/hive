# UI design system — Phase 2: icon sprite, primitives, glyph removal, lint → strict

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace every Unicode glyph used as UI with one SVG sprite rendered through a tiny primitive layer (`icon`, `stateIcon`, `iconButton`, `kbd`), and turn `scripts/ui-lint.sh` from a warning into a CI gate so glyphs can never come back.

**Architecture:** `src/ui/icons.svg` holds 22 `<symbol>`s on a 24×24 grid. `src/ui/icon.ts` imports it with Vite's built-in `?raw` suffix and injects it once into `<body>` (`#hv-icon-sprite`, `display:none`); `icon(name)` returns `<svg class="hv-icon"><use href="#hv-<name>"/></svg>`. Session state is resolved by one pure function, `sessionState()` in `src/lib/session-state.ts` (same seam as `lib/phase-steps.ts`: pure, node-env unit tested, no `app/state.ts` import); `stateIcon()`/`updateStateIcon()` in `icon.ts` render its five outcomes. `iconButton()` and `kbd()` are one file each in `src/ui/`. Each primitive owns a stylesheet under `src/theme/components/` (the layout `components.md` specifies; the directory starts here rather than in Phase 6). Feature modules (`index.html`'s static buttons, `sidebar.ts`, `session-term.ts`, `view.ts`, `modals/settings.ts`) stop writing `textContent = '✕'` and call the primitives. Finally the lint's glyph rule is narrowed from "any non-ASCII" to a denylist of icon-shaped characters (so prose em-dashes and `⌘` hints stay legal) and CI runs it with `--strict`.

**Tech Stack:** Vanilla TS, Vite 8 (`?raw` — no plugin, no new dep), vitest 4 (`test/unit` node + `test/dom` jsdom), Playwright 1.62 (`test/e2e`), bash + grep for lint. No new dependencies.

**Spec:** `docs/design-docs/ui/icons.md` (sprite inventory, state shapes, resolution, rules), `components.md` (`icon`, `stateIcon`, `iconButton`, `kbd` anatomy), `patterns.md` (selection vs attention, exited sessions, keyboard hints), `tokens.md` (`--state-*`, `--motion-pulse`, `--radius-sm`).

## Global Constraints

- **Phase 1 must have landed.** This plan references `src/theme/tokens.css`, `src/theme/themes.css`, `src/theme/theme.ts` and `scripts/ui-lint.sh` as existing. At the time of writing, Phase 1 Tasks 1–6 are committed on this branch and **Task 7 (`scripts/ui-lint.sh` + the warn-mode CI step) is not yet written** — if it is still missing when this plan starts, do Phase 1 Task 7 first; Task 9 here only *edits* that script.
- **Every `file:line` below must be re-verified before editing.** The glyph inventory was taken at commit `4e1f632`; Phase 1's remaining commits shift `style.css` and `index.html` lines. Re-run the inventory command in Task 8 Step 0 and work from its output, not from this document's numbers.
- No new npm dependencies. Sprite inlining uses `?raw`, which Vite and vitest both resolve natively.
- No emoji anywhere. `aria-label` is **required** by `iconButton()` — it throws without one, and mirrors it into `title`.
- Tokens only: no hex, no `px` font-size, in any CSS added here. Colour comes from the parent's `color`; icons use `stroke: currentColor`.
- `npx biome ci .` (not `biome lint`) must pass; `npm run typecheck` needs `./scripts/ci-bootstrap.sh` in a fresh worktree.
- Commits: conventional (`feat(ui): …`, `refactor(ui): …`, `chore(ci): …`), one per task.
- Run every frontend command from `cmd/hivegui/frontend/`.

### Glyph inventory (commit `4e1f632`)

| File:line | Glyph | Replacement |
|---|---|---|
| `index.html:28` | `×` daemon banner dismiss | `iconButton x` filled from `banners.ts` |
| `index.html:33` | `×` update banner dismiss | `iconButton x` filled from `banners.ts` |
| `index.html:38` | `＋` new project | `icon('plus')` filled from `project-editor.ts` |
| `index.html:42` | `＋` inside an HTML comment | reword the comment |
| `index.html:88` | `×` worktrees close | `icon('x')` filled from `worktrees.ts` |
| `index.html:118` | `×` settings close | `icon('x')` filled from `settings.ts` |
| `index.html:158` | `×` help close | `icon('x')` filled from `help-overlay.ts` |
| `src/app/modals/settings.ts:135` | `×` delete agent row | `iconButton({ icon: 'x' })` |
| `src/app/sidebar.ts:115` | `＋` in a comment | reword |
| `src/app/sidebar.ts:132` | `＋` restore minimized project | `iconButton plus` |
| `src/app/sidebar.ts:226` | `▾` collapse caret | `icon('chevron-down'\|'chevron-right')` |
| `src/app/sidebar.ts:239` | `+` (ASCII) new session | `icon('plus')` |
| `src/app/sidebar.ts:261` | `⎇` project worktrees | `icon('branch')` |
| `src/app/sidebar.ts:270` | `✎` edit project | `icon('settings')` — see Ambiguities |
| `src/app/sidebar.ts:280` | `–` minimize project | `icon('minus')` |
| `src/app/sidebar.ts:289` | `✕` delete project | `icon('x')` |
| `src/app/sidebar.ts:437-441` | `.dot` span + `title` | `stateIcon(sessionState(s, …))` |
| `src/app/sidebar.ts:468` | `⎇` session worktree marker | `icon('branch', { size: 12 })` |
| `src/app/sidebar.ts:505` | `＋` / `–` minimize toggle | `icon('plus'\|'minus')` |
| `src/app/session-term.ts:248` | `⎇` tile worktree | `icon('branch', { size: 12 })` |
| `src/app/session-term.ts:282` | `–` tile minimize | `iconButton minus` |
| `src/app/session-term.ts:1523` | `.phase-step` `li` (glyph is in CSS) | `icon('check')` / `stateIcon('starting')` / no mark |
| `src/app/keyboard.ts:702` | `✕` in a comment | reword |
| `src/app/modals/launcher.ts:652` | `✎`, `✕` in a comment | reword |
| `src/style.css:484` | `content: '●'` sidebar attention dot | delete rule (state icon carries it) |
| `src/style.css:741` | `content: '●'` tile attention dot | delete rule |
| `src/style.css:802` | `content: "—"` tile title separator | keep — text separator, allow-listed |
| `src/style.css:1617` | `content: '·'` phase step bullet | delete rule |
| `src/style.css:1622` | `content: '✓'` phase step done | delete rule |
| `src/style.css:1626` | `content: '◐'` phase step active | delete rule |
| `src/app/view.ts:765` | `•` status separator | keep — text separator |

Non-ASCII **kept** everywhere (prose and key hints, per `icons.md` › Rules): `— … · • ▸ – “ ” ’ ≈ ⌘ ⇧ ⌥ ⌃ ⌫ ← → ↑ ↓ ↔`.

---

### Task 1: The sprite + `icon()`

**Files:**
- Create: `cmd/hivegui/frontend/src/ui/icons.svg`
- Create: `cmd/hivegui/frontend/src/ui/icon.ts`
- Create: `cmd/hivegui/frontend/src/theme/components/icon.css`
- Modify: `cmd/hivegui/frontend/index.html` (one `<link>`)
- Test: `cmd/hivegui/frontend/test/dom/ui-icon.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export type IconName =
    | 'plus' | 'minus' | 'x' | 'rotate' | 'grid' | 'single' | 'branch'
    | 'chevron-down' | 'chevron-right' | 'settings' | 'search' | 'help'
    | 'arrow-left' | 'arrow-right' | 'external' | 'download' | 'check'
    | 'state-running' | 'state-attention' | 'state-starting'
    | 'state-exited' | 'state-error';
  export function ensureSprite(doc?: Document): void;
  export function icon(name: IconName, opts?: { size?: 12 | 14 }): SVGSVGElement;
  ```

**Sprite inlining decision:** `import sprite from './icons.svg?raw'` + one-time DOM injection, **not** a Vite `transformIndexHtml` plugin. `?raw` is a built-in Vite feature (zero config, zero plugin code) that vitest resolves through the same transform pipeline, so jsdom tests get the real sprite for free; a plugin would need a matching stub for vitest and would keep `index.html` and the primitive in two places. The cost of injecting after first paint is nil because every icon in the app is created by TS anyway — the six static buttons in `index.html` are filled by their own modules (Task 5), not by `<use>` in markup.

- [ ] **Step 1: Write `src/ui/icons.svg`**

24×24 viewBox, `stroke-width: 1.75`, round caps/joins, `fill: none` on the outer `<svg>`; the three filled state shapes set `fill="currentColor" stroke="none"` on themselves.

```svg
<!-- Icon sprite. Inlined once by src/ui/icon.ts (`?raw` import).
     Geometry rules and the inventory: docs/design-docs/ui/icons.md.
     24x24 grid, stroke 1.75, round caps. Never inline an SVG in a
     feature module - add a <symbol> here and a row in icons.md. -->
<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0" style="position:absolute"
     fill="none" stroke="currentColor" stroke-width="1.75"
     stroke-linecap="round" stroke-linejoin="round">
  <symbol id="hv-plus" viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></symbol>
  <symbol id="hv-minus" viewBox="0 0 24 24"><path d="M5 12h14"/></symbol>
  <symbol id="hv-x" viewBox="0 0 24 24"><path d="M6 6l12 12M18 6L6 18"/></symbol>
  <symbol id="hv-rotate" viewBox="0 0 24 24"><path d="M20 12a8 8 0 1 1-2.34-5.66M20 4v4h-4"/></symbol>
  <symbol id="hv-grid" viewBox="0 0 24 24"><path d="M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z"/></symbol>
  <symbol id="hv-single" viewBox="0 0 24 24"><path d="M4 5h16v14H4z"/></symbol>
  <symbol id="hv-branch" viewBox="0 0 24 24">
    <circle cx="7" cy="6" r="2"/><circle cx="7" cy="18" r="2"/><circle cx="17" cy="7" r="2"/>
    <path d="M7 8v8M17 9v1a4 4 0 0 1-4 4H9"/>
  </symbol>
  <symbol id="hv-chevron-down" viewBox="0 0 24 24"><path d="M6 9l6 6 6-6"/></symbol>
  <symbol id="hv-chevron-right" viewBox="0 0 24 24"><path d="M9 6l6 6-6 6"/></symbol>
  <symbol id="hv-settings" viewBox="0 0 24 24">
    <circle cx="12" cy="12" r="3.2"/>
    <path d="M12 2.5v3M12 18.5v3M2.5 12h3M18.5 12h3M5.3 5.3l2.1 2.1M16.6 16.6l2.1 2.1M18.7 5.3l-2.1 2.1M7.4 16.6l-2.1 2.1"/>
  </symbol>
  <symbol id="hv-search" viewBox="0 0 24 24"><circle cx="11" cy="11" r="6.5"/><path d="M20 20l-4.4-4.4"/></symbol>
  <symbol id="hv-help" viewBox="0 0 24 24">
    <circle cx="12" cy="12" r="8.5"/>
    <path d="M9.6 9.4a2.5 2.5 0 1 1 3.3 2.4c-.7.3-.9.8-.9 1.6"/>
    <path d="M12 17h.01"/>
  </symbol>
  <symbol id="hv-arrow-left" viewBox="0 0 24 24"><path d="M19 12H5M11 6l-6 6 6 6"/></symbol>
  <symbol id="hv-arrow-right" viewBox="0 0 24 24"><path d="M5 12h14M13 6l6 6-6 6"/></symbol>
  <symbol id="hv-external" viewBox="0 0 24 24">
    <path d="M14 4h6v6M20 4l-8 8"/>
    <path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>
  </symbol>
  <symbol id="hv-download" viewBox="0 0 24 24"><path d="M12 4v11M7 11l5 5 5-5M4 20h16"/></symbol>
  <symbol id="hv-check" viewBox="0 0 24 24"><path d="M5 12.5l5 5 9-11"/></symbol>

  <!-- State shapes (icons.md > State icons). Filled ones set fill on themselves. -->
  <symbol id="hv-state-running" viewBox="0 0 24 24"><path d="M9 6l9 6-9 6z" fill="currentColor" stroke="none"/></symbol>
  <symbol id="hv-state-attention" viewBox="0 0 24 24"><path d="M12 4l8 8-8 8-8-8z" fill="currentColor" stroke="none"/></symbol>
  <symbol id="hv-state-starting" viewBox="0 0 24 24"><circle cx="12" cy="12" r="7" stroke-dasharray="2 4"/></symbol>
  <symbol id="hv-state-exited" viewBox="0 0 24 24"><path d="M6 6h12v12H6z"/></symbol>
  <symbol id="hv-state-error" viewBox="0 0 24 24"><path d="M7 7l10 10M17 7L7 17"/></symbol>
</svg>
```

- [ ] **Step 2: Write the failing test** `test/dom/ui-icon.test.ts`

```ts
// @vitest-environment jsdom
//
// The icon primitive (src/ui/icon.ts) and the sprite it inlines.
// The sprite is imported with Vite's `?raw`, so this test also proves
// the build-time inlining path works under vitest, not just in Wails.
import { describe, it, expect, beforeEach } from 'vitest';
import { icon, ensureSprite, ICON_NAMES } from '../../src/ui/icon.js';

beforeEach(() => {
  document.body.innerHTML = '';
});

describe('icon()', () => {
  it('injects the sprite exactly once', () => {
    icon('plus');
    icon('x');
    ensureSprite();
    expect(document.querySelectorAll('#hv-icon-sprite')).toHaveLength(1);
  });

  it('renders a <use> pointing at the sprite symbol', () => {
    const el = icon('branch');
    expect(el.tagName.toLowerCase()).toBe('svg');
    expect(el.getAttribute('class')).toBe('hv-icon');
    expect(el.querySelector('use')?.getAttribute('href')).toBe('#hv-branch');
    expect(el.getAttribute('aria-hidden')).toBe('true');
  });

  it('defaults to 14px and honours the 12px inline size', () => {
    expect(icon('check').getAttribute('width')).toBe('14');
    const small = icon('check', { size: 12 });
    expect(small.getAttribute('width')).toBe('12');
    expect(small.dataset.size).toBe('12');
  });

  it('has a symbol in the sprite for every declared name', () => {
    ensureSprite();
    for (const name of ICON_NAMES) {
      expect(document.getElementById(`hv-${name}`), name).not.toBeNull();
    }
    expect(ICON_NAMES).toHaveLength(22);
  });
});
```

- [ ] **Step 3: Run to verify failure**

Run: `npx vitest run test/dom/ui-icon.test.ts`
Expected: FAIL — cannot resolve `../../src/ui/icon.js`.

- [ ] **Step 4: Implement `src/ui/icon.ts`**

```ts
// The icon primitive. One sprite, one <use>, colour from the parent's
// `color`. Feature modules never write SVG or a Unicode glyph.
// Spec: docs/design-docs/ui/icons.md, components.md > icon().
//
// The sprite is imported as a string (Vite's built-in `?raw`) and
// injected into <body> on first use rather than being pasted into
// index.html by a build plugin: `?raw` needs no plugin and resolves the
// same way under vitest, so DOM tests exercise the real sprite.
import sprite from './icons.svg?raw';
import type { SessionState } from '../lib/session-state.js';

const SVG_NS = 'http://www.w3.org/2000/svg';
const SPRITE_ID = 'hv-icon-sprite';

export const ICON_NAMES = [
  'plus', 'minus', 'x', 'rotate', 'grid', 'single', 'branch',
  'chevron-down', 'chevron-right', 'settings', 'search', 'help',
  'arrow-left', 'arrow-right', 'external', 'download', 'check',
  'state-running', 'state-attention', 'state-starting',
  'state-exited', 'state-error',
] as const;

export type IconName = (typeof ICON_NAMES)[number];

/** Inject the sprite once. Idempotent and safe to call per icon. */
export function ensureSprite(doc: Document = document): void {
  if (doc.getElementById(SPRITE_ID)) return;
  const host = doc.createElement('div');
  host.id = SPRITE_ID;
  host.setAttribute('aria-hidden', 'true');
  // The HTML parser puts <svg> in the SVG namespace here, so <use>
  // references resolve; the host is display:none via icon.css, which
  // does NOT break <use> (the symbol only has to be in the document).
  host.innerHTML = sprite;
  doc.body.prepend(host);
}

export function icon(
  name: IconName,
  { size = 14 }: { size?: 12 | 14 } = {},
): SVGSVGElement {
  ensureSprite();
  const svg = document.createElementNS(SVG_NS, 'svg');
  // SVGElement.className is a read-only SVGAnimatedString: setAttribute
  // is the only way to set a class on an SVG element.
  svg.setAttribute('class', 'hv-icon');
  svg.setAttribute('width', String(size));
  svg.setAttribute('height', String(size));
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  svg.setAttribute('focusable', 'false');
  if (size !== 14) svg.dataset.size = String(size);
  const use = document.createElementNS(SVG_NS, 'use');
  use.setAttribute('href', `#hv-${name}`);
  svg.appendChild(use);
  return svg;
}
```

`?raw` needs a type declaration. Append to `src/globals.d.ts`:

```ts
declare module '*.svg?raw' {
  const content: string;
  export default content;
}
```

- [ ] **Step 5: Write `src/theme/components/icon.css`** and link it

```css
/* icon primitive - docs/design-docs/ui/components.md > icon() */
#hv-icon-sprite { display: none; }

.hv-icon {
  width: 14px;
  height: 14px;
  flex: none;
  display: inline-block;
  vertical-align: -0.15em;
  color: inherit;
}
.hv-icon[data-size="12"] { width: 12px; height: 12px; }
```

In `index.html`, after the `style.css` link (component styles win over the legacy sheet):

```html
<link rel="stylesheet" href="./src/theme/components/icon.css"/>
```

- [ ] **Step 6: Run**

Run: `npx vitest run test/dom/ui-icon.test.ts && npm run build && npx biome ci .`
Expected: 4 passed; build succeeds.

- [ ] **Step 7: Commit**

```bash
git add src/ui/icons.svg src/ui/icon.ts src/globals.d.ts src/theme/components/icon.css index.html test/dom/ui-icon.test.ts
git commit -m "feat(ui): add the icon sprite and icon() primitive"
```

---

### Task 2: `iconButton()` and `kbd()`

**Files:**
- Create: `cmd/hivegui/frontend/src/ui/icon-button.ts`
- Create: `cmd/hivegui/frontend/src/ui/kbd.ts`
- Create: `cmd/hivegui/frontend/src/theme/components/icon-button.css`, `kbd.css`
- Modify: `cmd/hivegui/frontend/index.html` (two `<link>`s)
- Test: `cmd/hivegui/frontend/test/dom/ui-icon-button.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export interface IconButtonOpts {
    icon: IconName;
    label: string;               // required; becomes aria-label AND title
    onClick?: (e: MouseEvent) => void;
    size?: 22 | 24;              // 22 = sidebar header, 24 = rows/bars
    className?: string;          // extra class for legacy selectors
  }
  export function iconButton(opts: IconButtonOpts): HTMLButtonElement;
  export function kbd(text: string): HTMLElement;
  ```

- [ ] **Step 1: Failing test**

```ts
// @vitest-environment jsdom
//
// iconButton() and kbd() (src/ui/). The aria-label assertion is the
// point of the file: an icon-only control with no label is invisible to
// a screen reader, so the primitive refuses to build one.
import { describe, it, expect, vi } from 'vitest';
import { iconButton } from '../../src/ui/icon-button.js';
import { kbd } from '../../src/ui/kbd.js';

describe('iconButton()', () => {
  it('is a type=button with the icon inside and no text', () => {
    const b = iconButton({ icon: 'x', label: 'Close' });
    expect(b.tagName).toBe('BUTTON');
    expect(b.type).toBe('button');
    expect(b.className).toBe('hv-icon-btn');
    expect(b.querySelector('use')?.getAttribute('href')).toBe('#hv-x');
    expect(b.textContent).toBe('');
  });

  it('mirrors the label into aria-label and title', () => {
    const b = iconButton({ icon: 'plus', label: 'New project' });
    expect(b.getAttribute('aria-label')).toBe('New project');
    expect(b.title).toBe('New project');
  });

  it('throws on a missing label rather than shipping an unlabelled control', () => {
    expect(() => iconButton({ icon: 'plus', label: '  ' })).toThrow(/label/i);
  });

  it('wires onClick and appends extra classes', () => {
    const onClick = vi.fn();
    const b = iconButton({ icon: 'minus', label: 'Minimize', onClick, className: 'session-minimize' });
    expect(b.className).toBe('hv-icon-btn session-minimize');
    b.click();
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('carries the 22px size as a data attribute', () => {
    expect(iconButton({ icon: 'x', label: 'Close', size: 22 }).dataset.size).toBe('22');
  });
});

describe('kbd()', () => {
  it('renders a <kbd class="hv-kbd"> with the literal text', () => {
    const el = kbd('[esc]');
    expect(el.tagName).toBe('KBD');
    expect(el.className).toBe('hv-kbd');
    expect(el.textContent).toBe('[esc]');
  });
});
```

- [ ] **Step 2: Run → FAIL** (`npx vitest run test/dom/ui-icon-button.test.ts`).

- [ ] **Step 3: Implement `src/ui/icon-button.ts`**

```ts
// Icon-only button. components.md > iconButton(): 24x24 (rows/bars) or
// 22x22 (sidebar header), 14px icon, aria-label REQUIRED and mirrored
// into title. Never build an icon-only <button> by hand.
import { icon, type IconName } from './icon.js';

export interface IconButtonOpts {
  icon: IconName;
  label: string;
  onClick?: (e: MouseEvent) => void;
  size?: 22 | 24;
  className?: string;
}

export function iconButton({
  icon: name,
  label,
  onClick,
  size = 24,
  className,
}: IconButtonOpts): HTMLButtonElement {
  // Accessibility is not a soft requirement here: the icon carries the
  // whole meaning, so an empty label is a bug, not a default.
  if (!label.trim()) throw new Error(`iconButton(${name}): label is required`);
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = className ? `hv-icon-btn ${className}` : 'hv-icon-btn';
  btn.setAttribute('aria-label', label);
  btn.title = label;
  if (size !== 24) btn.dataset.size = String(size);
  btn.appendChild(icon(name));
  if (onClick) btn.addEventListener('click', onClick);
  return btn;
}
```

`src/ui/kbd.ts`:

```ts
// The only way to render a key hint. patterns.md > Keyboard hints:
// mono, --text-xs, --fg-subtle, no border, no fill; format ([1] for
// digits/symbols, (n) for letters) is the caller's, per AGENTS.md.
export function kbd(text: string): HTMLElement {
  const el = document.createElement('kbd');
  el.className = 'hv-kbd';
  el.textContent = text;
  return el;
}
```

- [ ] **Step 4: Styles**

`src/theme/components/icon-button.css`:

```css
/* iconButton - docs/design-docs/ui/components.md > iconButton() */
.hv-icon-btn {
  width: 24px;
  height: 24px;
  padding: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 0;
  background: none;
  color: var(--fg-subtle);
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: background var(--motion-fast), color var(--motion-fast);
}
.hv-icon-btn[data-size="22"] { width: 22px; height: 22px; }
.hv-icon-btn:hover { background: var(--hover); color: var(--fg); }
.hv-icon-btn:disabled { opacity: 0.5; pointer-events: none; }
.hv-icon-btn:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
```

`src/theme/components/kbd.css`:

```css
/* kbd - docs/design-docs/ui/patterns.md > Keyboard hints */
.hv-kbd {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--fg-subtle);
  background: none;
  border: 0;
  padding: 0;
}
```

Link both in `index.html` next to `icon.css`.

- [ ] **Step 5: Run**

Run: `npx vitest run test/dom/ui-icon-button.test.ts && npm run typecheck && npx biome ci .`
Expected: 6 passed, typecheck clean.

- [ ] **Step 6: Commit**

```bash
git add src/ui/icon-button.ts src/ui/kbd.ts src/theme/components/icon-button.css src/theme/components/kbd.css index.html test/dom/ui-icon-button.test.ts
git commit -m "feat(ui): add iconButton() and kbd() primitives"
```

---

### Task 3: `sessionState()` resolver

The five state icons need one resolution function used by all three sites (row, chip, tile header). It lives in `src/lib/` — pure, no `app/state.ts` import (that module touches `localStorage` at import time and can't be pulled into the node-env unit suite), exactly like `lib/phase-steps.ts`.

**The spec's `exit_code` field does not exist.** `wire.SessionInfo` carries no exit code (`grep -rn 'exit_code\|ExitCode' internal/` finds only an unrelated `worktree.go` hit) and neither does `SessionInfo` in `src/app/state.ts`. The available "it ended badly" signal is `last_error`/`lastError`, which `events.ts` and `session-term.ts` already use for the dead-session overlay. The resolver uses it and the docs get a note (Task 10).

**Files:**
- Create: `cmd/hivegui/frontend/src/lib/session-state.ts`
- Modify: `cmd/hivegui/frontend/src/ui/icon.ts` (add `stateIcon`, `updateStateIcon`)
- Modify: `cmd/hivegui/frontend/src/theme/components/icon.css` (state colours + motion)
- Test: `cmd/hivegui/frontend/test/unit/session-state.test.ts`, `cmd/hivegui/frontend/test/dom/ui-state-icon.test.ts`

**Interfaces:**
- Produces:
  ```ts
  // src/lib/session-state.ts
  export type SessionState = 'starting' | 'attention' | 'running' | 'exited' | 'error';
  export interface StateCarrier {
    alive?: boolean; phase?: string; last_error?: string; lastError?: string;
  }
  export function sessionState(s: StateCarrier, hasAttention: boolean): SessionState;
  export const STATE_WORDS: Record<SessionState, string>;

  // src/ui/icon.ts
  export function stateIcon(state: SessionState): SVGSVGElement;
  export function updateStateIcon(el: SVGSVGElement, state: SessionState): void;
  ```

- [ ] **Step 1: Failing unit test** `test/unit/session-state.test.ts`

```ts
import { describe, it, expect } from 'vitest';
import { sessionState } from '../../src/lib/session-state.js';

describe('sessionState', () => {
  it('is starting for any non-ready phase, alive or not', () => {
    expect(sessionState({ alive: false, phase: 'worktree' }, false)).toBe('starting');
    expect(sessionState({ alive: true, phase: 'closing' }, true)).toBe('starting');
  });
  it('is running when alive, ready and unflagged', () => {
    expect(sessionState({ alive: true }, false)).toBe('running');
    expect(sessionState({ alive: true, phase: '' }, false)).toBe('running');
  });
  it('is attention when alive and flagged', () => {
    expect(sessionState({ alive: true }, true)).toBe('attention');
  });
  it('is exited when dead with no recorded error', () => {
    expect(sessionState({ alive: false }, false)).toBe('exited');
    // A stale bell on a dead session must not outrank the exit.
    expect(sessionState({ alive: false }, true)).toBe('exited');
  });
  it('is error when dead with a last_error, either spelling', () => {
    expect(sessionState({ alive: false, last_error: 'boom' }, false)).toBe('error');
    expect(sessionState({ alive: false, lastError: 'boom' }, false)).toBe('error');
    expect(sessionState({ alive: false, last_error: '' }, false)).toBe('exited');
  });
});
```

- [ ] **Step 2: Run → FAIL** (`npx vitest run test/unit/session-state.test.ts`).

- [ ] **Step 3: Implement `src/lib/session-state.ts`**

```ts
// The one resolution from a SessionInfo to the five state shapes
// (docs/design-docs/ui/icons.md > State icons). Sidebar row, minimized
// chip and grid tile header all call this, so they can never disagree.
//
// Pure and structural for the same reason as lib/phase-steps.ts: it
// must be importable from the node-env unit suite, which app/state.ts
// (localStorage on import) is not. `hasAttention` is passed in rather
// than read from state.attention for the same reason.
//
// icons.md writes the exit branch as `exit_code == 0`, but no exit code
// exists on the wire (internal/wire has no ExitCode field and
// SessionInfo has no exit_code). last_error is the only "it ended
// badly" signal the daemon sends, and it is what the dead-session
// overlay already reads (app/events.ts, app/session-term.ts).
import { phaseOf, isReady } from './phase-steps.js';

export type SessionState =
  | 'starting'
  | 'attention'
  | 'running'
  | 'exited'
  | 'error';

export interface StateCarrier {
  alive?: boolean;
  phase?: string;
  last_error?: string;
  lastError?: string;
}

/** Words for the icon's <title>: state is shape + colour + words. */
export const STATE_WORDS: Record<SessionState, string> = {
  starting: 'Starting',
  attention: 'Waiting for you',
  running: 'Running',
  exited: 'Exited',
  error: 'Exited with an error',
};

export function sessionState(
  s: StateCarrier,
  hasAttention: boolean,
): SessionState {
  // A session mid-create has no PTY yet; `alive: false` there means
  // "not born", not "died" (same reasoning as sidebar.ts's dead class).
  if (!isReady(phaseOf(s))) return 'starting';
  if (!s.alive) return s.last_error || s.lastError ? 'error' : 'exited';
  return hasAttention ? 'attention' : 'running';
}
```

- [ ] **Step 4: Add `stateIcon`/`updateStateIcon` to `src/ui/icon.ts`**

```ts
// State icons (components.md > stateIcon). Used by the sidebar row, the
// minimized chip and the grid tile header - nowhere else. data-state
// drives colour and animation from icon.css; the <title> child is the
// "words" channel required by README principle 5.
export function stateIcon(state: SessionState): SVGSVGElement {
  const el = icon(`state-${state}` as IconName);
  el.setAttribute('class', 'hv-icon hv-state-icon');
  el.setAttribute('role', 'img');
  el.removeAttribute('aria-hidden');
  el.dataset.state = state;
  const title = document.createElementNS(SVG_NS, 'title');
  title.textContent = STATE_WORDS[state];
  el.prepend(title);
  return el;
}

export function updateStateIcon(el: SVGSVGElement, state: SessionState): void {
  if (el.dataset.state === state) return;
  el.dataset.state = state;
  el.querySelector('use')?.setAttribute('href', `#hv-state-${state}`);
  const title = el.querySelector('title');
  if (title) title.textContent = STATE_WORDS[state];
}
```

Add `import { STATE_WORDS, type SessionState } from '../lib/session-state.js';` to `icon.ts` (the `type SessionState` import stub added in Task 1 becomes this real one).

- [ ] **Step 5: State styles** — append to `src/theme/components/icon.css`

```css
/* State icons - icons.md > State icons. Colour and motion only; the
   shape comes from the symbol. */
.hv-state-icon[data-state="running"] { color: var(--state-running); }
.hv-state-icon[data-state="attention"] { color: var(--state-attention); }
.hv-state-icon[data-state="starting"] {
  color: var(--state-starting);
  animation: hv-state-spin 1s linear infinite;
}
.hv-state-icon[data-state="exited"] { color: var(--state-exited); }
.hv-state-icon[data-state="error"] { color: var(--state-error); }

.hv-state-icon[data-state="attention"] {
  border-radius: 50%;
  animation: hv-state-pulse var(--motion-pulse) ease-in-out infinite;
}

@keyframes hv-state-spin { to { transform: rotate(360deg); } }
@keyframes hv-state-pulse {
  0%, 100% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--state-attention) 55%, transparent); }
  50% { box-shadow: 0 0 0 3px color-mix(in srgb, var(--state-attention) 0%, transparent); }
}
@media (prefers-reduced-motion: reduce) {
  .hv-state-icon { animation: none !important; }
}
```

- [ ] **Step 6: DOM test** `test/dom/ui-state-icon.test.ts`

```ts
// @vitest-environment jsdom
import { describe, it, expect } from 'vitest';
import { stateIcon, updateStateIcon } from '../../src/ui/icon.js';

describe('stateIcon()', () => {
  it('renders the shape, the data-state hook and the words', () => {
    const el = stateIcon('attention');
    expect(el.dataset.state).toBe('attention');
    expect(el.getAttribute('role')).toBe('img');
    expect(el.querySelector('use')?.getAttribute('href')).toBe('#hv-state-attention');
    expect(el.querySelector('title')?.textContent).toBe('Waiting for you');
  });

  it('updates in place instead of being rebuilt', () => {
    const el = stateIcon('running');
    const use = el.querySelector('use');
    updateStateIcon(el, 'error');
    expect(el.dataset.state).toBe('error');
    expect(el.querySelector('use')).toBe(use); // same node, patched
    expect(use?.getAttribute('href')).toBe('#hv-state-error');
    expect(el.querySelector('title')?.textContent).toBe('Exited with an error');
  });
});
```

- [ ] **Step 7: Run**

Run: `npx vitest run test/unit/session-state.test.ts test/dom/ui-state-icon.test.ts && npm run typecheck && npx biome ci .`
Expected: green.

- [ ] **Step 8: Commit**

```bash
git add src/lib/session-state.ts src/ui/icon.ts src/theme/components/icon.css test/unit/session-state.test.ts test/dom/ui-state-icon.test.ts
git commit -m "feat(ui): resolve session state to the five state icons"
```

---

### Task 4: `index.html` static glyphs → icons

Six buttons carry glyphs in markup. Their modules already hold references (`pageEl(...)`), so each fills its own icon at init — no `<use>`-before-sprite ordering question, and the markup stops carrying content.

**Files:**
- Modify: `cmd/hivegui/frontend/index.html:28,33,38,42,88,118,158`
- Modify: `src/app/banners.ts` (:40, :140), `src/app/modals/project-editor.ts:111`, `src/app/modals/worktrees.ts:557`, `src/app/modals/settings.ts:246`, `src/app/modals/help-overlay.ts:24`

- [ ] **Step 1: Empty the buttons in `index.html`**

Replace each glyph with nothing, keeping ids, `type`, and `aria-label`:

```html
<button id="daemon-banner-dismiss" type="button" aria-label="Dismiss"></button>
<button id="update-banner-dismiss" type="button" aria-label="Dismiss"></button>
<button id="new-project-btn" title="New project (⌘N)" aria-label="New project"></button>
<button id="worktrees-close" type="button" aria-label="Close"></button>
<button id="settings-close" type="button" aria-label="Close"></button>
<button id="help-overlay-close" type="button" aria-label="Close"></button>
```

Reword the comment at `index.html:42` to drop the `＋`: "…name only, one restore button each."

- [ ] **Step 2: Fill them from their owning modules**

In each module's init/module scope, next to the existing `pageEl` lookup, add one line (import `icon` from `'../ui/icon.js'` / `'../../ui/icon.js'` for `modals/*`):

```ts
daemonBannerDismiss.replaceChildren(icon('x'));   // banners.ts
updateBannerDismiss.replaceChildren(icon('x'));   // banners.ts
pageEl('new-project-btn').replaceChildren(icon('plus'));      // project-editor.ts initProjectEditor
pageEl('worktrees-close').replaceChildren(icon('x'));         // worktrees.ts initWorktrees
pageEl('settings-close').replaceChildren(icon('x'));          // settings.ts initSettings
helpCloseBtn.replaceChildren(icon('x'));                      // help-overlay.ts
```

`banners.ts` and `help-overlay.ts` resolve their elements at module scope; put the `replaceChildren` right after the `pageEl` const so it runs on import. The other three run inside their existing `init*()`.

- [ ] **Step 3: `settings.ts:135` — the delete-agent row button**

```ts
const del = iconButton({
  icon: 'x',
  label: `Delete ${a.id || 'agent'}`,
  className: 'agent-del',
  onClick: () => { /* existing handler body, unchanged */ },
});
```

Keep whatever class the CSS already targets on that button and drop the `del.textContent = '×'` line plus any now-duplicated `title`/listener wiring.

- [ ] **Step 4: Verify by eye and by test**

Run: `npx vitest run test/dom/settings.test.ts test/dom/update-banner.test.ts && npm run build`
Expected: green. If a settings/banner DOM test asserts on `textContent`, update it to `getAttribute('aria-label')` or a `use[href]` check.

- [ ] **Step 5: Commit**

```bash
git add index.html src/app/banners.ts src/app/modals/{project-editor,worktrees,settings,help-overlay}.ts test/dom
git commit -m "refactor(ui): replace static index.html glyphs with sprite icons"
```

---

### Task 5: `sidebar.ts` — project header, chips, session rows

The biggest cluster. Nine glyph sites plus the `.dot` state span.

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/sidebar.ts` (:115, :132, :226, :239, :261, :270, :280, :289, :437-441, :468, :505)

- [ ] **Step 1: Minimized project chip (`:132`)**

```ts
const restore = iconButton({
  icon: 'plus',
  label: `Restore ${p.name}`,
  className: 'min-project-restore',
  size: 22,
  onClick: (e) => {
    e.stopPropagation();
    deps.restoreProject(p.id);
  },
});
```

Delete the old `restore.textContent`/`title`/`setAttribute('aria-label')`/`addEventListener` block. Reword the comment at `:115` ("the same thing the restore button does").

- [ ] **Step 2: Collapse caret (`:226`)**

The caret's direction is now the icon, not a CSS rotation — check `style.css` for a `.caret` `transform: rotate(...)` on the collapsed state and delete it (Task 8).

```ts
const caret = document.createElement('button');
caret.type = 'button';
caret.className = 'caret hv-icon-btn';
caret.dataset.size = '22';
const collapsedNow = state.collapsed.has(p.id);
caret.replaceChildren(icon(collapsedNow ? 'chevron-right' : 'chevron-down'));
caret.setAttribute('aria-expanded', String(!collapsedNow));
caret.setAttribute('aria-label', `${collapsedNow ? 'Expand' : 'Collapse'} ${p.name}`);
```

(Keep the existing click handler as-is.)

- [ ] **Step 3: Project header actions (`:239`–`:289`)**

Replace the five hand-built buttons with `iconButton` calls, preserving each handler body verbatim:

```ts
const newBtn = iconButton({
  icon: 'plus',
  label: 'New session in this project',
  onClick: (e) => { e.stopPropagation(); openLauncher(p.id); },
});
const wtBtn = iconButton({
  // The binding is shown inline, per the key-discoverability rule.
  icon: 'branch',
  label: 'Worktrees in this project (⌘E)',
  onClick: (e) => { e.stopPropagation(); openWorktrees(p); },
});
const editBtn = iconButton({
  icon: 'settings',
  label: 'Edit project',
  onClick: (e) => { e.stopPropagation(); openProjectEditor(p); },
});
const minBtn = iconButton({
  icon: 'minus',
  label: `Minimize ${p.name}`,
  onClick: (e) => { e.stopPropagation(); deps.minimizeProject(p.id); },
});
const delBtn = iconButton({
  icon: 'x',
  label: `Delete project ${p.name}`,
  onClick: (e) => { e.stopPropagation(); deps.confirmAndDeleteProject(p); },
});
```

`minBtn`'s old `title` was "Minimize project (hide from sidebar and grid)" — keep that as the label instead if the tooltip wording matters more than the aria brevity; label and title are the same string by design.

- [ ] **Step 4: Session row state icon (`:437`–`:441`)**

Replace the `.dot` span:

```ts
const dot = stateIcon(sessionState(s, state.attention.has(s.id)));
dot.classList.add('dot'); // keep the legacy hook until phase 3 rebuilds the row
```

Delete the `if (!isReady(phase)) { dot.title = … }` block — the `<title>` child now carries the words. Keep the existing `li.classList.add('dead'|'starting'|'closing'|'attention')` lines: `style.css` still uses them for row dimming and strike-through.

`renderSession` already computes `const phase = phaseOf(s)`; `sessionState` recomputes it — leave that, it is a string compare.

- [ ] **Step 5: Session worktree marker (`:468`) and minimize toggle (`:505`)**

```ts
glyph = document.createElement('span');
glyph.className = 'worktree-glyph clickable';
glyph.replaceChildren(icon('branch', { size: 12 }));
glyph.title = `Worktree: ${wtBranch} — click to manage worktrees`;
glyph.setAttribute('role', 'button');
// (existing click handler unchanged)
```

```ts
const minBtn = iconButton({
  icon: isMin ? 'plus' : 'minus',
  label: `${isMin ? 'Restore' : 'Minimize'} ${s.name ?? 'session'}`,
  className: 'session-minimize',
  onClick: (e) => {
    e.stopPropagation();
    if (isMin) deps.restoreSession(s.id);
    else deps.minimizeSession(s.id);
  },
});
```

The old `minBtn.title` ("Restore to the grid" / "Minimize (hide from grid)") is lost to the aria-derived title; that is the intended trade (one string, two channels). If the longer tooltip is wanted, pass it as `label`.

- [ ] **Step 6: Run the sidebar suites**

Run: `npx vitest run test/dom/sidebar-title.test.ts test/dom/minimize-project.test.ts test/dom/attention-jump.test.ts && npm run typecheck && npx biome ci .`
Expected: `minimize-project.test.ts` fails at `:160`/`:278` on `textContent === '＋'`. Fix those assertions to the new contract (Task 9 covers the full sweep, but fix these two now since this task breaks them):

```ts
expect(rowBtn('s2')?.getAttribute('aria-label')).toMatch(/^Restore /);
expect(rowBtn('s2')?.querySelector('use')?.getAttribute('href')).toBe('#hv-plus');
```

- [ ] **Step 7: Commit**

```bash
git add src/app/sidebar.ts test/dom/minimize-project.test.ts
git commit -m "refactor(ui): render sidebar controls and session state from the sprite"
```

---

### Task 6: `session-term.ts` tile header + phase checklist, `view.ts` chip

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/session-term.ts` (:248, :282, ~:685 phase list, :1523)
- Modify: `cmd/hivegui/frontend/src/app/view.ts` (`renderMinimizedTray`, ~:595–640)

- [ ] **Step 1: Tile worktree marker and minimize (`:248`, `:282`)**

```ts
this.tileWorktree = document.createElement('span');
this.tileWorktree.className = 'worktree-glyph clickable';
this.tileWorktree.replaceChildren(icon('branch', { size: 12 }));
this.tileWorktree.setAttribute('role', 'button');
```

```ts
this.tileMinimize = iconButton({
  icon: 'minus',
  label: 'Minimize session',
  className: 'tile-minimize',
  onClick: (e) => { e.stopPropagation(); minimizeSession(this.info.id); },
});
this.tileMinimize.addEventListener('mousedown', (e) => e.stopPropagation());
```

- [ ] **Step 2: Tile header state icon**

`icons.md` puts a state icon in the tile header. Add one next to `tileColor`, kept in sync wherever the tile's info is refreshed (the same method that updates `tileTermTitle`, around `:1056`):

```ts
// constructor, before this.header.append(...)
this.tileState = stateIcon(sessionState(info, state.attention.has(info.id)));
```

```ts
this.header.append(
  this.tileState,
  this.tileColor,
  this.tileName,
  this.tileWorktree,
  this.tileTermTitle,
  this.tileProject,
  this.tileMinimize,
);
```

and in the info-refresh method:

```ts
updateStateIcon(
  this.tileState,
  sessionState(this.info, state.attention.has(this.info.id)),
);
```

Declare `tileState: SVGSVGElement;` alongside the other tile fields. If the class's field list is typed in the `SessionTermLike` structural interface in `app/state.ts`, it does **not** need adding there — nothing outside the class reads it.

- [ ] **Step 3: Phase checklist marks (`:1523`)**

```ts
this.phaseSteps.replaceChildren(
  ...panel.steps.map((step) => {
    const li = document.createElement('li');
    li.className = `phase-step ${step.state}`;
    // The mark used to be a CSS ::before glyph ('·'/'✓'/'◐'); it is an
    // icon now so it matches the rest of the family. 'todo' gets no
    // mark - the indent in phase-step::before holds the column.
    if (step.state === 'done') li.append(icon('check', { size: 12 }));
    else if (step.state === 'active') li.append(stateIcon('starting'));
    const label = document.createElement('span');
    label.textContent = step.label;
    li.append(label);
    return li;
  }),
);
```

- [ ] **Step 4: Minimized session chip (`view.ts` `renderMinimizedTray`)**

`icons.md` lists the chip as the third state-icon site. Insert one before `dot`/`name`:

```ts
const st = stateIcon(sessionState(s, state.attention.has(s.id)));
chip.append(st, dot, name);
```

(the tray is rebuilt wholesale on every render, so no `updateStateIcon` is needed here).

- [ ] **Step 5: Run**

Run: `npx vitest run test/dom/session-phase.test.ts test/dom/view-floor.test.ts && npm run typecheck && npx biome ci . && npm run build`
Expected: green; `session-phase.test.ts:139` asserts `phaseStatus.textContent`, which is untouched. If any test reads a `phase-step` `li`'s `textContent`, it now still returns the label (the icon contributes no text) — no change needed.

- [ ] **Step 6: Commit**

```bash
git add src/app/session-term.ts src/app/view.ts
git commit -m "refactor(ui): icons and state shapes in the tile header, chip and phase checklist"
```

---

### Task 7: CSS cleanup

**Files:**
- Modify: `cmd/hivegui/frontend/src/style.css` (:484, :741, :1617, :1622, :1626, `.dot`/`.caret` rules)

- [ ] **Step 1: Delete the glyph rules**

Remove these four blocks entirely (the state icon and its `data-state` colouring replace them):

- `.session-item.attention .name::before { content: '●'; … }` (`:483-490`)
- `#terms.grid .term-host.in-grid.attention .tile-name::before { content: '●'; … }` (`:740-747`)
- `.phase-step::before { … content: '·'; }` (`:1614-1618`), `.phase-step.done::before` (`:1622`), `.phase-step.active::before` (`:1626`)

Keep `.term-host .tile-term-title::before { content: "—"; … }` (`:800-805`) — em dash as a text separator is explicitly allowed.

- [ ] **Step 2: Re-point the `.dot` rules**

`.session-item .dot` (`:560`) sizes and colours a `<span>` that is now an `<svg class="hv-icon hv-state-icon dot">`. Strip its `background`/`border-radius`/`width`/`height` (the icon carries those) and keep only layout (`margin`, `flex: none`). Delete `.session-item.dead .dot { background: #333; }` (`:568`) and `.session-item.starting .dot { … }` (`:1639`) — `data-state` owns both now, so those literals (and their `/* ui-lint: allow */`) go away.

- [ ] **Step 3: Phase-step layout**

Replace the deleted `::before` sizing with a flex row so the mark column survives:

```css
.phase-step {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 1px 0;
}
.phase-step > .hv-icon { flex: none; }
/* 'todo' has no mark; the gap keeps its label on the same column. */
.phase-step.todo::before { content: ''; width: 12px; flex: none; }
```

- [ ] **Step 4: `.caret`**

If `style.css` rotates `.caret` for the collapsed state, delete the `transform`/`transition` (Task 5 swaps the symbol instead). Keep any sizing that `hv-icon-btn` does not already give it.

- [ ] **Step 5: Verify visually**

Run: `npm run build && npx playwright test test/e2e/theme.spec.ts`
Expected: the Phase 1 pixel-equality test **will fail** — this phase changes the UI on purpose. Update those baselines: `npx playwright test test/e2e/theme.spec.ts --update-snapshots`, then eyeball the diff in `test-results/` before accepting, and check the attention row, a starting row, a dead row and the phase overlay each render a shape.

- [ ] **Step 6: Commit**

```bash
git add src/style.css test/e2e/theme.spec.ts-snapshots
git commit -m "refactor(ui): drop the CSS glyph pseudo-elements, restyle for state icons"
```

---

### Task 8: `ui-lint.sh` glyph rule → denylist, CI → `--strict`

Phase 1's glyph rule is "any non-ASCII outside a small allow-list", which fires on ~560 em dashes and 92 `⌘`s in prose comments. Flipping that to `--strict` would fail CI on comments, so the rule inverts: ban a fixed set of icon-shaped characters, allow everything else.

**Files:**
- Modify: `scripts/ui-lint.sh` (glyph rule)
- Modify: `scripts/testdata/ui-lint/bad.css`, `good.css` (glyph fixtures)
- Modify: `.github/workflows/ci.yml` (the "UI lint" step)
- Modify: comments at `src/app/keyboard.ts:702`, `src/app/modals/launcher.ts:652`

- [ ] **Step 0: Re-take the inventory** (line numbers in this plan are from `4e1f632`)

```bash
cd cmd/hivegui/frontend && python3 - <<'EOF'
import pathlib
deny='×✕✗＋✚⎇✎▾▴●○◐◆■▶⟳↻'
for f in sorted(pathlib.Path('src/app').rglob('*.ts')) + [pathlib.Path('index.html'), pathlib.Path('src/style.css')]:
    for i, l in enumerate(f.read_text().splitlines(), 1):
        if any(c in l for c in deny): print(f"{f}:{i}: {l.strip()[:100]}")
EOF
```

Expected after Tasks 4–7: only the two comment lines below.

- [ ] **Step 1: Reword the two remaining comments**

- `src/app/keyboard.ts:702`: "shared by the sidebar ✕ button and the ⇧⌘⌫ shortcut" → "shared by the sidebar's delete button and the ⇧⌘⌫ shortcut".
- `src/app/modals/launcher.ts:652`: "clicking ✎ or ✕ still moves focus out" → "clicking the edit or delete button still moves focus out".

- [ ] **Step 2: Replace the glyph rule in `scripts/ui-lint.sh`**

```bash
# glyph — Unicode used as UI. A denylist, not "all non-ASCII": prose in
# comments legitimately uses em dashes, curly quotes and arrows, and key
# hints (⌘⇧⌥⌃⌫) are required by AGENTS.md. What is banned is the set of
# characters that stand in for an icon — those come from src/ui/icons.svg
# via icon() now (docs/design-docs/ui/icons.md > Rules).
DENY='×✕✗＋✚⎇✎▾▴●○◐◆■▶⟳↻'
while IFS= read -r line; do report "$line"; done < <(
  grep -rnP --include='*.ts' --include='*.html' --include='*.css' "[$DENY]" \
    "$FE/src/app" "$FE/src/style.css" "$FE/index.html" 2>/dev/null \
    | grep -v -e 'ui-lint: allow' \
    | sed 's/^/glyph: /' || true)
```

Note the rule now also covers `src/style.css` (`content: '●'` was invisible to the Phase 1 version, which scanned only `src/app` and `index.html`) and no longer needs the `^\S*:\s*//` comment filter.

- [ ] **Step 3: Update the fixtures**

`scripts/testdata/ui-lint/bad.css`: keep `color: #fff; font-size: 12.5px;` and change the glyph line to `content: '●';` (a denied character). `good.css`: add `content: '—';` and `/* one ⌘N hint */` — both must pass.

- [ ] **Step 4: Self-test**

Run: `scripts/ui-lint.sh --strict scripts/testdata/ui-lint/bad.css; echo exit=$?`
Expected: 3 violations, `exit=1`.
Run: `scripts/ui-lint.sh --strict scripts/testdata/ui-lint/good.css; echo exit=$?`
Expected: `0 violation(s)`, `exit=0`.
Run: `scripts/ui-lint.sh --strict; echo exit=$?`
Expected: `0 violation(s)`, `exit=0` — this is the gate for the whole phase. Anything listed is an unconverted call site; go fix it, do not allow-list it.

- [ ] **Step 5: CI**

In `.github/workflows/ci.yml`, replace the Phase 1 step:

```yaml
      - name: UI lint (tokens / icons)
        if: matrix.biome
        run: ./scripts/ui-lint.sh --strict
```

- [ ] **Step 6: Commit**

```bash
git add scripts/ui-lint.sh scripts/testdata/ui-lint .github/workflows/ci.yml src/app/keyboard.ts src/app/modals/launcher.ts
git commit -m "chore(ci): gate on ui-lint --strict with an icon-glyph denylist"
```

---

### Task 9: Update the tests that assert on glyphs

**Files:**
- Modify: `cmd/hivegui/frontend/test/e2e/launcher-search.spec.ts:124`
- Modify: `cmd/hivegui/frontend/test/dom/minimize-project.test.ts:160,278` (if not already done in Task 5)
- Sweep: any other spec selecting a control by its glyph text

- [ ] **Step 1: Find them**

```bash
cd cmd/hivegui/frontend && grep -rnP "hasText:?\s*['\"][^'\"]*[×✕＋–⎇✎▾●◐✓]" test/ \
  ; grep -rnP "textContent\).toBe\(['\"][×✕＋–⎇✎▾●◐✓]" test/
```

- [ ] **Step 2: Fix `launcher-search.spec.ts:124`**

```ts
    .locator('#projects .project-actions button[aria-label="Edit project"]')
```

(replacing `{ hasText: '✎' }` — an icon button has no text to match).

- [ ] **Step 3: Add a regression guard** — append to `test/e2e/theme.spec.ts`:

```ts
test('no Unicode glyph is used as a control label', async ({ page }) => {
  await page.goto('/');
  // Same denylist as scripts/ui-lint.sh, asserted against the rendered
  // DOM: the lint reads source, this reads what the user actually sees.
  const found = await page.evaluate(() => {
    const deny = /[×✕✗＋✚⎇✎▾▴●○◐◆■▶⟳↻]/;
    return [...document.querySelectorAll('button, .caret, .worktree-glyph')]
      .filter((el) => deny.test(el.textContent ?? ''))
      .map((el) => `${el.tagName}#${el.id}.${el.className}`);
  });
  expect(found).toEqual([]);
});
```

- [ ] **Step 4: Run the full suite**

Run: `npx vitest run && npx playwright test && npm run typecheck && npx biome ci .`
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add test/
git commit -m "test(ui): select controls by aria-label instead of glyph text"
```

---

### Task 10: Docs, changeset, plan bookkeeping

**Files:**
- Modify: `docs/design-docs/ui/icons.md` (the `exit_code` resolution block)
- Modify: `docs/design-docs/ui/README.md` (Status line → "Phase 2 implemented")
- Modify: `docs/exec-plans/active/ui-design-system.md` (tick Phase 2; decision-log entries)
- Create: `.changesets/<pr>-ui-icons.md` via `/hs-changelog-update`

- [ ] **Step 1: Correct `icons.md`'s resolution block** to what shipped

```
!isReady(phase)             → starting
!alive && !last_error       → exited
!alive                      → error
attention.has(id)           → attention
else                        → running
```

with a line under it: "`exit_code` is not on the wire (`internal/wire` has no exit-code field); `last_error` is the signal the daemon actually sends, and it is what the dead-session overlay already reads. Implemented in `src/lib/session-state.ts`."

Add a row for the edit-project use of `settings` if the decision below is kept.

- [ ] **Step 2: Decision-log entries** in `ui-design-system.md`

- "Sprite is inlined via Vite's `?raw` + one-time DOM injection, not a build plugin. Why: no plugin code, resolves identically under vitest, and every icon is created from TS anyway."
- "`last_error` replaces `exit_code` in the state resolution. Why: no exit code exists on the wire."
- "Edit-project uses `settings` (gear); the 22-icon inventory has no pencil." (or, if a pencil was added, note the inventory is 23.)
- "`ui-lint`'s glyph rule is a denylist of icon-shaped characters, not `[^\x00-\x7F]`. Why: prose comments and mandated `⌘` key hints are legitimate non-ASCII; the old rule could not go strict."

- [ ] **Step 3: Changeset text**

"The GUI's controls and session-state indicators are now SVG icons instead of Unicode symbols, so they render identically on every platform. Session state reads as a shape (▶ running, ◆ needs you, ◌ starting, ■ exited, ✗ error) as well as a colour." — write the shapes as words in the changeset if the changelog is linted for glyphs too.

- [ ] **Step 4: Full local gate**

Run:
```bash
cd cmd/hivegui/frontend && npx biome ci . && npm run typecheck && npx vitest run && npx playwright test \
  && cd ../../.. && scripts/ui-lint.sh --strict && go build ./...
```

- [ ] **Step 5: Commit and open PR**

```bash
git add docs .changesets
git commit -m "docs(ui): mark phase 2 of the design system implemented"
```

PR title: `feat(ui): design-system phase 2 — icon sprite, primitives, ui-lint strict`. Body: link the spec, list the primitives, paste the `ui-lint --strict` output (`0 violation(s)`), and note the intentionally-updated screenshot baselines.

---

## Self-review

- **Spec coverage:** `icons.md` sprite inventory (22 symbols, 24×24/1.75/round) → Task 1; state shapes and their motion → Tasks 1+3; resolution from `SessionInfo` → Task 3 (with a documented substitution for the non-existent `exit_code`); "state icons appear in exactly three places" → sidebar row (Task 5), minimized chip and tile header (Task 6); "no Unicode glyphs as UI" → Tasks 4–7, enforced by Task 8; "icons are never the only label" → `iconButton` throws without one (Task 2). `components.md` `icon`/`stateIcon`/`updateStateIcon`/`iconButton`/`kbd` → Tasks 1–3. `kbd()` is built here but has **no call site yet** — `patterns.md` places hints on the project card header and overlay footers, which are Phase 3/4 markup; building it now is what the master plan's phase table asks for, and the DOM test covers it.
- **Deliberately out of scope:** `sessionRow`/`projectCard`/`chip`/`dialog`/`banner` primitives (Phase 3–5), the two-line row and 220px sidebar (Phase 3), `style.css` splitting into `src/theme/components/*` for anything but the four primitives added here (Phase 6). The row keeps its current markup; only its glyph children change.
- **Placeholders:** none. Every step names a real file, a real line, and the code that goes there.
- **Type consistency:** `IconName`, `SessionState`, `STATE_WORDS`, `sessionState`, `icon`, `stateIcon`, `updateStateIcon`, `iconButton`, `kbd` are used identically across Tasks 1–6 and 9. `SessionState` is declared once (`lib/session-state.ts`) and imported by `ui/icon.ts` — do not re-declare it in the UI layer.
- **Known risk 1:** the Phase 1 screenshot baselines are pixel-equality assertions and this phase moves pixels on purpose. Task 7 Step 5 updates them; do not let an agent update them earlier to make an intermediate task green.
- **Known risk 2:** `?raw` gives vitest the sprite through Vite's transform, but the `test/e2e-real` Playwright project builds with `VITE_WAILS_REAL=1` — same Vite pipeline, so the sprite is present there too. If any suite ever runs TS through plain `tsc`/node without Vite, the `?raw` import breaks; that suite does not exist today.
- **Known risk 3:** `.dot` and `.tile-minimize` are load-bearing class names in `style.css` and in DOM tests. Tasks 5 and 6 keep them as extra classes on the new elements rather than renaming; Phase 3 removes them with the row rebuild.

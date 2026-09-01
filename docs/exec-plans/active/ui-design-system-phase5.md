# UI design system — Phase 5: dialog + form fields, and Settings › Appearance

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**PR:** #310 · **Branch:** `feature/ui-design-system-phase5`

**Goal:** Replace four hand-rolled dialog implementations with one `dialog()` primitive and one set of form fields, and give the theme layer a user interface — a preset picker and a custom-token override box that apply live, including to every open terminal.

**Architecture:** `src/ui/dialog.ts` builds a backdrop + panel + header + body + footer, owning the four behaviours every modal in this app re-implements today: Escape, the backdrop mousedown/click pair, the close button, and registration with `modals/registry.ts`. Focus containment stays where it already works — `keyboard.ts` calls `trapFocus()` on the open modal's root — so `dialog()` only has to keep the contract that root depends on: a stable `id` and the `hidden` class as the open/closed signal. `src/ui/field.ts` supplies the label+control pairs (text, select, textarea, colour swatch) and the shared error slot. The four static dialog blocks leave `index.html` and are built in TypeScript at module scope, so the module-level `export const settingsEl` / `editorEl` / `worktreesEl` / `helpEl` refs that `keyboard.ts` and the tests import keep working unchanged.

`theme.ts` grows the data the picker renders from (`PRESETS`) and the override pipeline (`sanitizeOverrides` / `readOverrides` / `writeOverrides` / `applyOverrides`). Overrides are sanitised **on write**, stored as finished CSS declarations in `localStorage['hive.themeOverrides']`, and injected into a `<style id="theme-overrides">` that sits statically in `index.html` after `themes.css` — so the existing boot `<script>` can fill it before first paint with no second copy of the sanitiser and no flash. Re-theming an open terminal cannot live in `theme.ts` (`session-term.ts` imports it, not the other way round), so `session-term.ts` exports `applyXtermTheme()` alongside the `applyFontSize()` it already has, and Settings calls both halves.

**Tech Stack:** Vanilla TS, Vite 8, vitest 4 (`test/unit` node, `test/dom` jsdom), Playwright 1.62 (`test/e2e` + `wails-mock.ts`), Biome 2.5.5. No new dependencies.

**Spec:** `docs/design-docs/ui/components.md` (`dialog`, form fields, `button`), `themes.md` (presets, user overrides, xterm mapping), `patterns.md` (errors, keyboard hints), `tokens.md`.

## Global Constraints

- **Assumes Phases 1–4 have landed.** `src/theme/theme.ts` already exports `ThemeName`, `THEME_KEY`, `DEFAULT_THEME`, `resolveTheme`, `readTheme`, `applyTheme`, `xtermTheme` (Phase 1, in tree). `src/ui/` is expected to already contain `icon.ts`, `iconButton`, `kbd.ts` and `button.ts` from Phases 2–4; **`src/ui/` does not exist yet as of writing** — if it is still missing when this phase starts, Phases 2–4 are not done and this plan is not startable.
- **Every `file:line` reference below was read at plan time against a tree with Phase 1 landed and Phases 2–4 not landed. Re-verify each one before editing** — Phases 2–4 rewrite `style.css` and `src/app/*.ts` heavily and every line number here will have moved.
- Behaviour preservation is the bar, not "close enough". `test/e2e/settings.spec.ts`, `worktrees.spec.ts`, `focus-traps.spec.ts` and `focus-invariants.spec.ts` must pass **without being edited**, except where a step below names the edit and says why. They pin: the `hidden` class as the open signal, `#settings` / `#worktrees` / `#help-overlay` / `#project-editor` ids, `.settings-agent-row` / `.settings-agent-name` / `.settings-agent-cmd` / `#settings-agent-add` / `#settings-error`, Tab containment, the ⌘, and ⌘E toggle-to-close, and the "typing must not reach the terminal behind the backdrop" invariant.
- Tokens only. No hex, no `px` font-size, no Unicode glyph in the new files — `scripts/ui-lint.sh --strict` is the gate (Phase 2 flipped CI to `--strict`).
- No new npm dependencies.
- Commits: conventional, one per task. Run every frontend command from `cmd/hivegui/frontend/`. Fresh worktree → `./scripts/ci-bootstrap.sh` first or `npm run typecheck` fails on missing `wailsjs/`.
- Full local gate for every task that touches TS: `npx biome ci . && npm run typecheck && npx vitest run`.

---

### Task 1: `dialog()` primitive

**Files:**
- Create: `cmd/hivegui/frontend/src/ui/dialog.ts`
- Create: `cmd/hivegui/frontend/src/theme/components/dialog.css`
- Modify: `cmd/hivegui/frontend/index.html` (add the stylesheet link)
- Test: `cmd/hivegui/frontend/test/dom/ui-dialog.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export type DialogSize = 'sm' | 'md' | 'lg';
  export interface DialogSpec {
    id: string;                       // becomes the root element id (e2e + keyboard.ts depend on it)
    title: string;
    size?: DialogSize;                // default 'md'
    role?: 'dialog' | 'alertdialog';  // default 'dialog'
    body?: (Node | null)[];
    actions?: (Node | null)[];        // footer, right-aligned, primary last
    hints?: (Node | null)[];          // footer-left keyboard hints, built with kbd()
    onClose: () => void;              // the module's own close fn; dialog() never hides itself
    closeOnBackdrop?: boolean;        // default true
    showCloseButton?: boolean;        // default true
  }
  export interface DialogHandle {
    el: HTMLElement;      // backdrop root; carries the id and the `hidden` class
    panel: HTMLElement;
    body: HTMLElement;
    footer: HTMLElement;
    isOpen(): boolean;
    show(): void;         // removes `hidden`; does NOT move focus
    hide(): void;         // adds `hidden`
    setTitle(text: string): void;
    setTitleSuffix(node: Node | null): void;  // worktrees' "· project name"
  }
  export function dialog(spec: DialogSpec): DialogHandle;
  ```

The handle deliberately exposes `show`/`hide` and not `open`/`close`: focus discipline and the `deps.setFocusedTile(null)` / `deps.refocusActiveTerm()` calls differ per modal and stay in the modal modules, which is where the comments explaining them already live.

- [ ] **Step 1: Failing DOM test**

```ts
// @vitest-environment jsdom
//
// The dialog primitive. Four modals hand-rolled these behaviours with
// four slightly different rules; the risk in consolidating them is that
// one caller quietly loses one. Each is pinned here.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { dialog } from '../../src/ui/dialog';
import { anyModalOpen } from '../../src/app/modals/registry';

describe('dialog()', () => {
  beforeEach(() => {
    document.body.replaceChildren();
  });

  it('renders the id, aria contract and starts hidden', () => {
    const d = dialog({ id: 'demo', title: 'Demo', onClose: () => {} });
    document.body.append(d.el);
    expect(d.el.id).toBe('demo');
    expect(d.el.getAttribute('role')).toBe('dialog');
    expect(d.el.getAttribute('aria-modal')).toBe('true');
    const labelledBy = d.el.getAttribute('aria-labelledby');
    expect(document.getElementById(labelledBy ?? '')?.textContent).toBe('Demo');
    expect(d.el.classList.contains('hidden')).toBe(true);
    expect(d.isOpen()).toBe(false);
  });

  it('registers with the modal registry so the focus pipeline sees it', () => {
    const d = dialog({ id: 'demo2', title: 'Demo', onClose: () => {} });
    document.body.append(d.el);
    expect(anyModalOpen()).toBe(false);
    d.show();
    expect(anyModalOpen()).toBe(true);
    d.hide();
    expect(anyModalOpen()).toBe(false);
  });

  it('calls onClose for Escape, the close button and the backdrop', () => {
    const onClose = vi.fn();
    const d = dialog({ id: 'demo3', title: 'Demo', onClose });
    document.body.append(d.el);
    d.show();

    const esc = new KeyboardEvent('keydown', {
      key: 'Escape',
      bubbles: true,
      cancelable: true,
    });
    d.panel.dispatchEvent(esc);
    expect(onClose).toHaveBeenCalledTimes(1);
    // Consumed: keyboard.ts's window handler would otherwise see an
    // already-hidden dialog and spend the same Escape on what is behind it.
    expect(esc.defaultPrevented).toBe(true);

    d.el.querySelector<HTMLElement>('.hv-dialog__close')?.click();
    expect(onClose).toHaveBeenCalledTimes(2);

    d.el.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    d.el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(onClose).toHaveBeenCalledTimes(3);
  });

  it('ignores a click that only ENDS on the backdrop', () => {
    // A text-selection drag that starts in an input and releases outside
    // the panel dispatches its click on the backdrop. Closing on that
    // discards the user's draft mid-edit. Both ends must be the backdrop.
    const onClose = vi.fn();
    const d = dialog({ id: 'demo4', title: 'Demo', onClose });
    document.body.append(d.el);
    d.show();
    d.panel.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    d.el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(onClose).not.toHaveBeenCalled();
  });

  it('places body, actions and hints', () => {
    const b = document.createElement('p');
    const a = document.createElement('button');
    const k = document.createElement('kbd');
    const d = dialog({
      id: 'demo5',
      title: 'Demo',
      size: 'lg',
      body: [b, null],
      actions: [a],
      hints: [k],
      onClose: () => {},
    });
    expect(d.body.contains(b)).toBe(true);
    expect(d.footer.contains(a)).toBe(true);
    expect(d.footer.contains(k)).toBe(true);
    expect(d.panel.dataset.size).toBe('lg');
  });
});
```

- [ ] **Step 2: Run → FAIL**

Run: `npx vitest run test/dom/ui-dialog.test.ts`
Expected: FAIL — cannot resolve `../../src/ui/dialog`.

- [ ] **Step 3: Implement `src/ui/dialog.ts`**

```ts
// ---------- dialog ----------
//
// One implementation of the modal shell that Settings, the worktree
// browser, the project editor, the help overlay and the choice dialog
// each grew separately. Consolidating them is worth doing because the
// differences between the five were bugs, not choices: only Settings
// guarded the "click that merely ENDS on the backdrop" case, only three
// of them consumed Escape, and the ids the keyboard pipeline keys off
// were spelled out by hand in index.html.
//
// What this does NOT own:
//   - Focus containment. keyboard.ts calls trapFocus() on the open
//     modal's root, because a dialog opened over a terminal starts with
//     focus outside it and a listener on the dialog would never fire.
//   - Where focus goes on open/close. Every modal has its own answer and
//     its own reason; see the modules.
//   - Hiding itself. onClose is the module's close function, which has
//     bookkeeping (in-flight loads, drafts, dismissing sub-dialogs) that
//     must run whichever gesture closed the dialog.
//
// The `hidden` class is load-bearing: registry.ts's anyModalOpen(),
// keyboard.ts's per-modal gates and every e2e visibility assertion read
// it. It is the open/closed signal, not a styling detail.
//
// See docs/design-docs/ui/components.md › dialog.

import { iconButton } from './iconButton.js';
import { registerModal } from '../app/modals/registry.js';

export type DialogSize = 'sm' | 'md' | 'lg';

export interface DialogSpec {
  id: string;
  title: string;
  size?: DialogSize;
  role?: 'dialog' | 'alertdialog';
  body?: (Node | null)[];
  actions?: (Node | null)[];
  hints?: (Node | null)[];
  onClose: () => void;
  closeOnBackdrop?: boolean;
  showCloseButton?: boolean;
}

export interface DialogHandle {
  el: HTMLElement;
  panel: HTMLElement;
  body: HTMLElement;
  footer: HTMLElement;
  isOpen(): boolean;
  show(): void;
  hide(): void;
  setTitle(text: string): void;
  setTitleSuffix(node: Node | null): void;
}

function keep(nodes: (Node | null)[] | undefined): Node[] {
  return (nodes ?? []).filter((n): n is Node => n != null);
}

export function dialog(spec: DialogSpec): DialogHandle {
  const el = document.createElement('div');
  el.id = spec.id;
  el.className = 'hv-dialog hidden';
  el.setAttribute('role', spec.role ?? 'dialog');
  el.setAttribute('aria-modal', 'true');

  const panel = document.createElement('div');
  panel.className = 'hv-dialog__panel';
  panel.dataset.size = spec.size ?? 'md';

  const header = document.createElement('header');
  header.className = 'hv-dialog__header';

  const titleId = `${spec.id}-title`;
  const title = document.createElement('h3');
  title.className = 'hv-dialog__title';
  title.id = titleId;
  title.textContent = spec.title;
  el.setAttribute('aria-labelledby', titleId);

  // Suffix rides inside the <h3> so the accessible name stays one
  // string ("Worktrees · hive"), which is what aria-labelledby reads.
  const suffix = document.createElement('span');
  suffix.className = 'hv-dialog__title-suffix';
  title.append(suffix);
  header.append(title);

  if (spec.showCloseButton !== false) {
    const close = iconButton({
      icon: 'x',
      label: 'Close',
      onClick: spec.onClose,
    });
    close.classList.add('hv-dialog__close');
    header.append(close);
  }

  const body = document.createElement('div');
  body.className = 'hv-dialog__body';
  body.append(...keep(spec.body));

  const footer = document.createElement('footer');
  footer.className = 'hv-dialog__footer';
  const hints = keep(spec.hints);
  const actions = keep(spec.actions);
  if (hints.length) {
    const hintSlot = document.createElement('div');
    hintSlot.className = 'hv-dialog__hints';
    hintSlot.append(...hints);
    footer.append(hintSlot);
  }
  if (actions.length) {
    const actionSlot = document.createElement('div');
    actionSlot.className = 'hv-dialog__actions';
    actionSlot.append(...actions);
    footer.append(actionSlot);
  }
  footer.hidden = hints.length === 0 && actions.length === 0;

  panel.append(header, body, footer);
  el.append(panel);

  // Escape is consumed here as well as handled. keyboard.ts's window
  // listener runs after this one and would otherwise see an
  // already-hidden dialog, fall past its gate, and spend the same
  // Escape on whatever is behind the modal.
  el.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    e.stopPropagation();
    spec.onClose();
  });

  if (spec.closeOnBackdrop !== false) {
    // Both ends of the gesture must land on the backdrop. A
    // text-selection drag that starts inside an input and releases
    // outside the panel dispatches its click on the nearest common
    // ancestor — the backdrop — so testing the click alone discards the
    // whole draft mid-edit.
    let downOnBackdrop = false;
    el.addEventListener('mousedown', (e) => {
      downOnBackdrop = e.target === el;
    });
    el.addEventListener('click', (e) => {
      const fire = downOnBackdrop && e.target === el;
      downOnBackdrop = false;
      if (fire) spec.onClose();
    });
  }

  registerModal(el);

  return {
    el,
    panel,
    body,
    footer,
    isOpen: () => !el.classList.contains('hidden'),
    show: () => el.classList.remove('hidden'),
    hide: () => el.classList.add('hidden'),
    setTitle: (text) => {
      title.firstChild
        ? (title.firstChild.nodeValue = text)
        : title.prepend(document.createTextNode(text));
    },
    setTitleSuffix: (node) => suffix.replaceChildren(...(node ? [node] : [])),
  };
}
```

- [ ] **Step 4: Write `src/theme/components/dialog.css`**

Values from `components.md` › dialog. Nothing here may contain a literal — `ui-lint --strict` only exempts `tokens.css` and `themes.css`.

```css
/* dialog primitive — docs/design-docs/ui/components.md › dialog */
.hv-dialog {
  position: fixed;
  inset: 0;
  z-index: 40;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, black 50%, transparent);
}
.hv-dialog.hidden { display: none; }

.hv-dialog__panel {
  display: flex;
  flex-direction: column;
  width: 100%;
  max-height: 80vh;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-popover);
  font-family: var(--font-ui);
}
.hv-dialog__panel[data-size='sm'] { max-width: 420px; }
.hv-dialog__panel[data-size='md'] { max-width: 560px; }
.hv-dialog__panel[data-size='lg'] { max-width: 720px; }

.hv-dialog__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  height: 44px;
  padding: 0 var(--space-3) 0 var(--space-4);
  border-bottom: 1px solid var(--border);
  flex: 0 0 auto;
}
.hv-dialog__title {
  margin: 0;
  font-size: var(--text-xl);
  font-weight: 600;
  color: var(--fg);
}
.hv-dialog__title-suffix {
  color: var(--fg-subtle);
  font-weight: 400;
}

.hv-dialog__body {
  padding: var(--space-4);
  overflow-y: auto;
  flex: 1 1 auto;
  color: var(--fg-muted);
  font-size: var(--text-md);
  line-height: 1.4;
}
/* Section heading inside a body. */
.hv-dialog__body h4 {
  margin: var(--space-5) 0 var(--space-2);
  font-size: var(--text-lg);
  font-weight: 500;
  color: var(--fg);
}
.hv-dialog__body h4:first-child { margin-top: 0; }
/* Hint paragraph. */
.hv-dialog__body p {
  margin: 0 0 var(--space-3);
  max-width: 60ch;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}

.hv-dialog__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border);
  flex: 0 0 auto;
}
.hv-dialog__footer[hidden] { display: none; }
.hv-dialog__hints { color: var(--fg-subtle); font-size: var(--text-xs); }
/* Pushes the actions right even when there are no hints. */
.hv-dialog__actions {
  display: flex;
  gap: var(--space-2);
  margin-left: auto;
}
```

- [ ] **Step 5: Link it**

In `index.html`, after the `themes.css` link and before `style.css` (verified at plan time: `index.html:21-23`; re-check, Phases 2–4 add links here):

```html
<link rel="stylesheet" href="./src/theme/components/dialog.css"/>
```

- [ ] **Step 6: Run**

Run: `npx vitest run test/dom/ui-dialog.test.ts && npm run typecheck && npx biome ci . && ../../../scripts/ui-lint.sh --strict`
Expected: green.

- [ ] **Step 7: Commit**

```bash
git add cmd/hivegui/frontend/src/ui/dialog.ts \
        cmd/hivegui/frontend/src/theme/components/dialog.css \
        cmd/hivegui/frontend/index.html \
        cmd/hivegui/frontend/test/dom/ui-dialog.test.ts
git commit -m "feat(ui): add dialog primitive"
```

---

### Task 2: form field primitives

**Files:**
- Create: `cmd/hivegui/frontend/src/ui/field.ts`
- Create: `cmd/hivegui/frontend/src/theme/components/field.css`
- Modify: `cmd/hivegui/frontend/index.html` (stylesheet link)
- Test: `cmd/hivegui/frontend/test/dom/ui-field.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export function field(label: string, control: HTMLElement, hint?: string): HTMLLabelElement;
  export function textInput(o: { value?: string; placeholder?: string; ariaLabel?: string; className?: string; onInput?: (v: string) => void }): HTMLInputElement;
  export function selectInput(o: { options: { value: string; label: string }[]; value?: string; ariaLabel?: string; id?: string; onChange?: (v: string) => void }): HTMLSelectElement;
  export function textareaInput(o: { value?: string; placeholder?: string; rows?: number; ariaLabel?: string; id?: string; onInput?: (v: string) => void }): HTMLTextAreaElement;
  export function colorInput(o: { value: string; ariaLabel: string; onInput?: (v: string) => void }): { el: HTMLElement; input: HTMLInputElement };
  export function errorSlot(id?: string): { el: HTMLElement; show(msg: string): void; clear(): void };
  ```

Exactly the five controls this app has. No `numberInput`, no `checkbox`, no validation framework — add one when a screen needs it.

- [ ] **Step 1: Failing DOM test**

```ts
// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import {
  field,
  textInput,
  selectInput,
  textareaInput,
  colorInput,
  errorSlot,
} from '../../src/ui/field';

describe('field primitives', () => {
  it('associates the label with the control it wraps', () => {
    const input = textInput({ value: 'x' });
    const l = field('Name', input);
    document.body.replaceChildren(l);
    expect(l.querySelector('.hv-field__label')?.textContent).toBe('Name');
    expect(l.contains(input)).toBe(true);
    // A wrapping <label> associates implicitly; no id/for bookkeeping.
    expect(input.closest('label')).toBe(l);
  });

  it('reports input through the callback and keeps the aria-label', () => {
    const onInput = vi.fn();
    const i = textInput({ ariaLabel: 'Agent name', onInput });
    i.value = 'typed';
    i.dispatchEvent(new Event('input'));
    expect(onInput).toHaveBeenCalledWith('typed');
    expect(i.getAttribute('aria-label')).toBe('Agent name');
  });

  it('builds a select from options and reports change', () => {
    const onChange = vi.fn();
    const s = selectInput({
      options: [
        { value: 'a', label: 'A' },
        { value: 'b', label: 'B' },
      ],
      value: 'b',
      onChange,
    });
    expect([...s.options].map((o) => o.value)).toEqual(['a', 'b']);
    expect(s.value).toBe('b');
    s.value = 'a';
    s.dispatchEvent(new Event('change'));
    expect(onChange).toHaveBeenCalledWith('a');
  });

  it('wraps the native colour picker in a swatch that mirrors the value', () => {
    const { el, input } = colorInput({ value: '#112233', ariaLabel: 'Colour' });
    expect(input.type).toBe('color');
    expect(el.style.getPropertyValue('--swatch')).toBe('#112233');
    input.value = '#445566';
    input.dispatchEvent(new Event('input'));
    expect(el.style.getPropertyValue('--swatch')).toBe('#445566');
  });

  it('error slot is hidden when empty and announces when not', () => {
    const e = errorSlot();
    expect(e.el.getAttribute('role')).toBe('alert');
    expect(e.el.classList.contains('hidden')).toBe(true);
    e.show('it broke');
    expect(e.el.textContent).toBe('it broke');
    expect(e.el.classList.contains('hidden')).toBe(false);
    e.clear();
    expect(e.el.classList.contains('hidden')).toBe(true);
  });

  it('textarea passes rows and id through', () => {
    const t = textareaInput({ rows: 4, id: 'ov', value: '--accent: red;' });
    expect(t.rows).toBe(4);
    expect(t.id).toBe('ov');
    expect(t.value).toBe('--accent: red;');
  });
});
```

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement `src/ui/field.ts`**

```ts
// ---------- form fields ----------
//
// Label-above-control pairs for the two forms this app has (project
// editor, settings) plus the error slot they share. See
// docs/design-docs/ui/components.md › Form fields.
//
// The label WRAPS its control rather than pointing at it with `for`.
// Both are correct HTML; wrapping means no id has to be minted for a
// control that is otherwise anonymous, and settings' agent rows are
// rebuilt on every render — ids there would either collide or need a
// counter.

export function field(
  label: string,
  control: HTMLElement,
  hint?: string,
): HTMLLabelElement {
  const l = document.createElement('label');
  l.className = 'hv-field';
  const span = document.createElement('span');
  span.className = 'hv-field__label';
  span.textContent = label;
  l.append(span, control);
  if (hint) {
    const h = document.createElement('span');
    h.className = 'hv-field__hint';
    h.textContent = hint;
    l.append(h);
  }
  return l;
}

function applyCommon(
  el: HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement,
  o: { ariaLabel?: string; className?: string; id?: string },
) {
  el.classList.add('hv-input');
  if (o.className) el.classList.add(...o.className.split(/\s+/).filter(Boolean));
  if (o.ariaLabel) el.setAttribute('aria-label', o.ariaLabel);
  if (o.id) el.id = o.id;
}

export function textInput(o: {
  value?: string;
  placeholder?: string;
  ariaLabel?: string;
  className?: string;
  id?: string;
  onInput?: (v: string) => void;
}): HTMLInputElement {
  const el = document.createElement('input');
  el.type = 'text';
  el.autocomplete = 'off';
  el.value = o.value ?? '';
  if (o.placeholder) el.placeholder = o.placeholder;
  applyCommon(el, o);
  if (o.onInput) el.addEventListener('input', () => o.onInput?.(el.value));
  return el;
}

export function selectInput(o: {
  options: { value: string; label: string }[];
  value?: string;
  ariaLabel?: string;
  className?: string;
  id?: string;
  onChange?: (v: string) => void;
}): HTMLSelectElement {
  const el = document.createElement('select');
  for (const opt of o.options) {
    const node = document.createElement('option');
    node.value = opt.value;
    node.textContent = opt.label;
    el.append(node);
  }
  if (o.value != null) el.value = o.value;
  applyCommon(el, o);
  if (o.onChange) el.addEventListener('change', () => o.onChange?.(el.value));
  return el;
}

export function textareaInput(o: {
  value?: string;
  placeholder?: string;
  rows?: number;
  ariaLabel?: string;
  className?: string;
  id?: string;
  onInput?: (v: string) => void;
}): HTMLTextAreaElement {
  const el = document.createElement('textarea');
  el.rows = o.rows ?? 4;
  el.spellcheck = false;
  el.value = o.value ?? '';
  if (o.placeholder) el.placeholder = o.placeholder;
  applyCommon(el, o);
  el.classList.add('hv-input--mono');
  if (o.onInput) el.addEventListener('input', () => o.onInput?.(el.value));
  return el;
}

// colorInput keeps the OS colour picker — there is no reason to build
// one — and hides its inconsistent native chrome behind a fixed-size
// swatch. The chosen colour is published as --swatch so the wrapper can
// paint itself from CSS instead of from a second style write per event.
export function colorInput(o: {
  value: string;
  ariaLabel: string;
  onInput?: (v: string) => void;
}): { el: HTMLElement; input: HTMLInputElement } {
  const el = document.createElement('span');
  el.className = 'hv-swatch';
  const input = document.createElement('input');
  input.type = 'color';
  input.value = o.value;
  input.setAttribute('aria-label', o.ariaLabel);
  el.style.setProperty('--swatch', o.value);
  input.addEventListener('input', () => {
    el.style.setProperty('--swatch', input.value);
    o.onInput?.(input.value);
  });
  el.append(input);
  return { el, input };
}

// errorSlot is the "errors that block a dialog go under the field that
// caused them" half of patterns.md › Errors. The other half is
// flashStatus(); a message must never go to both.
export function errorSlot(id?: string): {
  el: HTMLElement;
  show(msg: string): void;
  clear(): void;
} {
  const el = document.createElement('p');
  el.className = 'hv-field-error hidden';
  el.setAttribute('role', 'alert');
  if (id) el.id = id;
  return {
    el,
    show(msg: string) {
      el.textContent = msg;
      el.classList.toggle('hidden', !msg);
    },
    clear() {
      el.textContent = '';
      el.classList.add('hidden');
    },
  };
}
```

- [ ] **Step 4: Write `src/theme/components/field.css`**

```css
/* form fields — docs/design-docs/ui/components.md › Form fields */
.hv-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin-bottom: var(--space-3);
}
.hv-field__label {
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.hv-field__hint {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
  max-width: 60ch;
}

.hv-input {
  height: 28px;
  padding: 0 var(--space-2);
  background: var(--surface-raised);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  font-family: var(--font-ui);
  font-size: var(--text-md);
}
textarea.hv-input {
  height: auto;
  padding: var(--space-2);
  line-height: 1.4;
  resize: vertical;
}
.hv-input--mono { font-family: var(--font-mono); font-size: var(--text-sm); }
.hv-input:focus { outline: none; border-color: var(--accent); }
.hv-input:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
.hv-input:disabled { opacity: 0.5; }

.hv-swatch {
  display: inline-flex;
  width: 28px;
  height: 28px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--swatch, var(--surface-raised));
  overflow: hidden;
  flex: 0 0 auto;
}
/* The native control is the hit target; it is made invisible rather
   than display:none so it stays focusable and keyboard-operable. */
.hv-swatch input[type='color'] {
  width: 100%;
  height: 100%;
  padding: 0;
  border: 0;
  opacity: 0;
  cursor: pointer;
}
.hv-swatch:focus-within { outline: 2px solid var(--accent); outline-offset: 1px; }

.hv-field-error {
  margin: var(--space-2) 0 0;
  font-size: var(--text-sm);
  color: var(--state-error);
}
.hv-field-error.hidden { display: none; }
```

- [ ] **Step 5: Link it in `index.html`** next to `dialog.css`.

- [ ] **Step 6: Run**

Run: `npx vitest run test/dom/ui-field.test.ts && npm run typecheck && npx biome ci . && ../../../scripts/ui-lint.sh --strict`

- [ ] **Step 7: Commit**

```bash
git add cmd/hivegui/frontend/src/ui/field.ts \
        cmd/hivegui/frontend/src/theme/components/field.css \
        cmd/hivegui/frontend/index.html \
        cmd/hivegui/frontend/test/dom/ui-field.test.ts
git commit -m "feat(ui): add form field primitives"
```

---

### Task 3: theme presets as data + the override pipeline

**Files:**
- Modify: `cmd/hivegui/frontend/src/theme/theme.ts`
- Modify: `cmd/hivegui/frontend/index.html` (boot script + static `<style id="theme-overrides">`)
- Test: `cmd/hivegui/frontend/test/unit/theme.test.ts` (extend)

**Interfaces:**
- Produces:
  ```ts
  export interface Preset { id: ThemeName; label: string }
  export const PRESETS: readonly Preset[];       // drives the picker; phase 6 appends native-*/terminal
  export const OVERRIDES_KEY = 'hive.themeOverrides';
  export const OVERRIDES_STYLE_ID = 'theme-overrides';
  export interface Sanitized { css: string; rejected: string[] }
  export function sanitizeOverrides(input: string): Sanitized;
  export function readOverrides(storage?: Storage): string;
  export function writeOverrides(css: string, storage?: Storage): void;
  export function applyOverrides(css: string, doc?: Document): void;
  ```
- Consumes: the existing `ThemeName`, `resolveTheme`, `readTheme`, `applyTheme` (unchanged signatures).

**Design note — why sanitise on write.** Overrides must land before first paint or the app flashes preset colours for every overridden token. The only code that runs that early is the inline `<script>` in `index.html`, and a second copy of the sanitiser there is exactly the duplication the phase-1 boot script already apologises for. Storing the *sanitised* text instead means the boot script is a two-line assignment. `theme.ts` re-sanitises what it reads on module init anyway — the store is hand-editable, and CSS text going into a `<style>` is a trust boundary however unlikely the attacker.

- [ ] **Step 1: Failing unit tests** — append to `test/unit/theme.test.ts`

```ts
import {
  PRESETS,
  OVERRIDES_KEY,
  sanitizeOverrides,
  applyOverrides,
  readOverrides,
  writeOverrides,
} from '../../src/theme/theme';

describe('PRESETS', () => {
  it('lists every selectable theme exactly once, System first', () => {
    expect(PRESETS[0].id).toBe('system');
    const ids = PRESETS.map((p) => p.id);
    expect(ids).toEqual(['system', 'hive-dark', 'hive-light', 'classic']);
    expect(new Set(ids).size).toBe(ids.length);
    expect(PRESETS.every((p) => p.label.length > 0)).toBe(true);
  });

  it('every non-system preset resolves to itself', () => {
    for (const p of PRESETS) {
      if (p.id === 'system') continue;
      expect(resolveTheme(p.id, true)).toBe(p.id);
    }
  });
});

describe('sanitizeOverrides', () => {
  it('keeps well-formed custom property declarations', () => {
    const r = sanitizeOverrides('--accent: #7aa2f7; --text-md: 14px;');
    expect(r.css).toBe('--accent: #7aa2f7;\n  --text-md: 14px;');
    expect(r.rejected).toEqual([]);
  });

  it('accepts newline-separated input and normalises spacing', () => {
    const r = sanitizeOverrides('  --fg:#fff\n--bg:  #000  ');
    expect(r.css).toBe('--fg: #fff;\n  --bg: #000;');
    expect(r.rejected).toEqual([]);
  });

  it('rejects non-custom properties', () => {
    const r = sanitizeOverrides('color: red; --accent: blue;');
    expect(r.rejected).toEqual(['color: red']);
    expect(r.css).toBe('--accent: blue;');
  });

  it('rejects anything that could escape the :root block', () => {
    for (const bad of [
      '--x: red } body { display: none',
      '--x: url(http://evil/a.png)',
      '--x: expression(alert(1))',
      '@import "http://evil/x.css"',
      '--x: </style><script>alert(1)</script>',
    ]) {
      const r = sanitizeOverrides(bad);
      expect(r.css, bad).toBe('');
      expect(r.rejected.length, bad).toBeGreaterThan(0);
    }
  });

  it('rejects uppercase and empty property names, and empty values', () => {
    const r = sanitizeOverrides('--Accent: red; --: red; --ok:;');
    expect(r.css).toBe('');
    expect(r.rejected).toHaveLength(3);
  });

  it('is a no-op on empty input', () => {
    expect(sanitizeOverrides('')).toEqual({ css: '', rejected: [] });
    expect(sanitizeOverrides('   \n  ')).toEqual({ css: '', rejected: [] });
  });
});

describe('overrides storage', () => {
  it('round-trips through storage and re-sanitises on read', () => {
    const store = new Map<string, string>();
    const storage = {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
    } as unknown as Storage;
    writeOverrides('--accent: red; color: blue;', storage);
    expect(store.get(OVERRIDES_KEY)).toBe('--accent: red;');
    // Hand-edited store: read must not trust it either.
    store.set(OVERRIDES_KEY, '--a: 1; } body {');
    expect(readOverrides(storage)).toBe('--a: 1;');
  });

  it('survives a storage that throws', () => {
    const storage = {
      getItem() {
        throw new Error('denied');
      },
      setItem() {
        throw new Error('denied');
      },
    } as unknown as Storage;
    expect(readOverrides(storage)).toBe('');
    expect(() => writeOverrides('--a: 1;', storage)).not.toThrow();
  });
});
```

And a jsdom test for the injection, in `test/dom/theme-apply.test.ts` (which already exists — append):

```ts
it('writes overrides into the static style element, replacing not appending', () => {
  document.head.innerHTML = '<style id="theme-overrides"></style>';
  applyOverrides('--accent: red;');
  const el = document.getElementById('theme-overrides');
  expect(el?.textContent).toBe(':root {\n  --accent: red;\n}');
  applyOverrides('--accent: blue;');
  expect(el?.textContent).toBe(':root {\n  --accent: blue;\n}');
  applyOverrides('');
  expect(el?.textContent).toBe('');
});
```

- [ ] **Step 2: Run → FAIL.**

Run: `npx vitest run test/unit/theme.test.ts test/dom/theme-apply.test.ts`

- [ ] **Step 3: Implement in `theme.ts`**

Replace the private `const PRESETS = new Set([...])` (verified at plan time: `src/theme/theme.ts:8`) with the exported list plus a derived set, then append the override pipeline:

```ts
export interface Preset {
  id: ThemeName;
  label: string;
}

// The picker renders from this list, so adding a preset in phase 6
// (native-dark, native-light, terminal) is one line here plus its block
// in themes.css — no UI change. Order is the order shown.
export const PRESETS: readonly Preset[] = [
  { id: 'system', label: 'System' },
  { id: 'hive-dark', label: 'Hive Dark' },
  { id: 'hive-light', label: 'Hive Light' },
  { id: 'classic', label: 'Classic' },
];

// Everything resolveTheme can stamp on <html>. 'system' is a selection,
// not a value, so it is excluded.
const STAMPABLE = new Set(
  PRESETS.map((p) => p.id).filter((id) => id !== 'system'),
);
```

…and update the two uses of the old set (`resolveTheme`, `readTheme`) to `STAMPABLE`. Then:

```ts
export const OVERRIDES_KEY = 'hive.themeOverrides';
export const OVERRIDES_STYLE_ID = 'theme-overrides';

export interface Sanitized {
  css: string;
  rejected: string[];
}

// Custom property names only, lowercase — the whole token vocabulary is
// lowercase, and allowing case would let `--Accent` sit in the store
// looking like it should work.
const NAME = /^--[a-z0-9-]+$/;
// The value may not contain anything that could end the declaration,
// end the :root block, start a new rule, fetch a resource, or close the
// <style> element it is injected into.
const BAD_VALUE = /[{}<>;]|url\s*\(|expression\s*\(|@import/i;

// sanitizeOverrides turns whatever the user typed into declarations
// that are safe to drop inside `:root { … }`, plus the lines it refused
// so the dialog can say which ones and why (patterns.md › Errors:
// "errors that block a dialog go in the dialog's error slot").
//
// Splitting on both ';' and newline means a value can never contain a
// semicolon. That is the spec (themes.md), and no token value needs one.
export function sanitizeOverrides(input: string): Sanitized {
  const css: string[] = [];
  const rejected: string[] = [];
  for (const raw of String(input ?? '').split(/[;\n]/)) {
    const line = raw.trim();
    if (!line) continue;
    const i = line.indexOf(':');
    const name = i < 0 ? '' : line.slice(0, i).trim();
    const value = i < 0 ? '' : line.slice(i + 1).trim();
    if (!NAME.test(name) || !value || BAD_VALUE.test(value)) {
      rejected.push(line);
      continue;
    }
    css.push(`${name}: ${value};`);
  }
  return { css: css.join('\n  '), rejected };
}

export function readOverrides(storage?: Storage): string {
  try {
    const s = storage ?? localStorage;
    // Re-sanitised, not trusted: the store is hand-editable and this
    // text is injected into a <style>.
    return sanitizeOverrides(s.getItem(OVERRIDES_KEY) ?? '').css;
  } catch {
    return '';
  }
}

// writeOverrides stores the SANITISED text, so index.html's boot script
// can inject it before first paint without a second copy of the
// sanitiser (and without a flash of un-overridden colours).
export function writeOverrides(css: string, storage?: Storage): void {
  try {
    (storage ?? localStorage).setItem(
      OVERRIDES_KEY,
      sanitizeOverrides(css).css,
    );
  } catch {
    // Private mode / denied storage: the override still applies to this
    // session, it just will not survive a restart. Nothing to report.
  }
}

// applyOverrides rewrites the <style id="theme-overrides"> that
// index.html declares after themes.css — overrides beat presets by
// cascade ORDER, not by specificity, so the element's position matters
// and it is never moved or recreated.
export function applyOverrides(css: string, doc: Document = document): void {
  let el = doc.getElementById(OVERRIDES_STYLE_ID);
  if (!el) {
    el = doc.createElement('style');
    el.id = OVERRIDES_STYLE_ID;
    doc.head.appendChild(el);
  }
  el.textContent = css ? `:root {\n  ${css}\n}` : '';
}
```

Extend the module's boot side effect (currently `if (typeof document !== 'undefined') applyTheme(readTheme());`) to also apply overrides:

```ts
// Side effect on import: stamp before anything renders. index.html's
// inline script has already done both from raw localStorage; this run
// re-does them from the sanitising path, which is what makes a
// hand-edited store harmless.
if (typeof document !== 'undefined') {
  applyTheme(readTheme());
  applyOverrides(readOverrides());
}
```

- [ ] **Step 4: `index.html` — static style element + boot script**

The `<style>` must be declared **after** the three theme links so overrides win by order. Extend the existing inline script (verified at plan time: `index.html:7-19`); it stays a duplicate of the preset list, so update the sync comment:

```html
    <link rel="stylesheet" href="./src/theme/tokens.css"/>
    <link rel="stylesheet" href="./src/theme/themes.css"/>
    <link rel="stylesheet" href="./src/theme/components/dialog.css"/>
    <link rel="stylesheet" href="./src/theme/components/field.css"/>
    <link rel="stylesheet" href="./src/style.css"/>
    <!-- User token overrides. Declared here, after every preset, so they
         win by cascade ORDER. Filled synchronously by the script below
         and rewritten by src/theme/theme.ts. Never moved. -->
    <style id="theme-overrides"></style>
    <script>
      // Stamps data-theme and injects the user's token overrides before
      // first paint. A module script (src/theme/theme.ts) is deferred and
      // would let the first paint land on the wrong preset.
      //
      // The preset list is duplicated from PRESETS in src/theme/theme.ts —
      // keep them in sync. The overrides are NOT re-sanitised here: they
      // are stored already-sanitised (writeOverrides), and theme.ts
      // re-sanitises on import, which covers a hand-edited store one paint
      // later.
      try {
        var t = localStorage.getItem('hive.theme');
        if (t === 'system') t = matchMedia('(prefers-color-scheme: dark)').matches ? 'hive-dark' : 'hive-light';
        if (t !== 'classic' && t !== 'hive-dark' && t !== 'hive-light') t = 'classic';
        document.documentElement.dataset.theme = t;
        var o = localStorage.getItem('hive.themeOverrides');
        if (o) document.getElementById('theme-overrides').textContent = ':root {\n  ' + o + '\n}';
      } catch (e) { document.documentElement.dataset.theme = 'classic'; }
    </script>
```

Note the script now runs *after* the `<style>` element it fills — move it below the links if it is still above them.

- [ ] **Step 5: Run**

Run: `npx vitest run test/unit/theme.test.ts test/dom/theme-apply.test.ts && npm run typecheck && npx biome ci .`

- [ ] **Step 6: Commit**

```bash
git add cmd/hivegui/frontend/src/theme/theme.ts cmd/hivegui/frontend/index.html \
        cmd/hivegui/frontend/test/unit/theme.test.ts cmd/hivegui/frontend/test/dom/theme-apply.test.ts
git commit -m "feat(theme): expose presets as data and add the user override pipeline"
```

---

### Task 4: re-theme open terminals

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/session-term.ts` (next to `applyFontSize`, verified at plan time: `session-term.ts:1587`)
- Test: `cmd/hivegui/frontend/test/dom/xterm-theme.test.ts` (extend)

**Interfaces:**
- Produces: `export function applyXtermTheme(): void`

`theme.ts` cannot do this itself: `session-term.ts:11` imports `xtermTheme` from it, and the reverse import would be a cycle. The seam is the same one `applyFontSize()` already occupies — a module-level "push the current preference into every live terminal" function that the settings screen calls.

- [ ] **Step 1: Failing test** — append to `test/dom/xterm-theme.test.ts`

```ts
import { applyXtermTheme } from '../../src/app/session-term';
import { state } from '../../src/app/state';

it('pushes the current tokens into every open terminal', () => {
  document.documentElement.style.setProperty('--term-bg', '#101010');
  document.documentElement.style.setProperty('--term-fg', '#f0f0f0');
  const a = { term: { options: { theme: { background: '#000000' } } } };
  // Tiles whose term is absent (the DOM-test stubs, and a tile whose
  // terminal has been disposed) must not throw.
  const b = {};
  state.terms.set('a', a as never);
  state.terms.set('b', b as never);

  applyXtermTheme();

  expect(a.term.options.theme.background).toBe('#101010');
  expect(a.term.options.theme.foreground).toBe('#f0f0f0');
  state.terms.clear();
});
```

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement**, directly above `applyFontSize()`:

```ts
// applyXtermTheme pushes the current token values into every live
// terminal. Called when the theme changes (Settings › Appearance), not
// per frame — getComputedStyle is a layout read.
//
// Same shape and same guard as applyFontSize: TermTile's `term` is
// optional because the DOM-test stubs omit it, and a disposed tile can
// outlive its terminal for a tick.
export function applyXtermTheme() {
  const theme = xtermTheme();
  for (const st of state.terms.values()) {
    const opts = st.term?.options;
    if (opts) opts.theme = theme;
  }
}
```

- [ ] **Step 4: Run**

Run: `npx vitest run test/dom/xterm-theme.test.ts && npm run typecheck && npx biome ci .`

- [ ] **Step 5: Commit**

```bash
git add cmd/hivegui/frontend/src/app/session-term.ts cmd/hivegui/frontend/test/dom/xterm-theme.test.ts
git commit -m "feat(theme): re-theme open terminals on demand"
```

---

### Task 5: Settings on `dialog()` + `field()`, with an Appearance section

The big one. Settings is rebuilt in TypeScript and gains the theme UI in the same change, because the Appearance section has to be built with the new primitives anyway.

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/modals/settings.ts`
- Modify: `cmd/hivegui/frontend/index.html` (delete the `#settings` block)
- Modify: `cmd/hivegui/frontend/src/style.css` (delete `#settings*` / `.settings-*` rules, verified at plan time: lines 1236–1408 and 1843–1845)
- Modify: `cmd/hivegui/frontend/test/dom/settings.test.ts` (its `MARKUP` fixture no longer applies)

**Interfaces:**
- Consumes: `dialog`, `field`/`textInput`/`selectInput`/`textareaInput`/`colorInput`/`errorSlot`, `button`, `kbd`, `PRESETS`/`THEME_KEY`/`readTheme`/`applyTheme`/`readOverrides`/`writeOverrides`/`applyOverrides`/`sanitizeOverrides`, `applyXtermTheme`.
- Preserves exported API: `settingsEl`, `openSettings`, `closeSettings`, `initSettings`, `splitCommand`, `SettingsDeps`.
- Preserves selectors: `#settings`, `#settings-agents-list`, `#settings-error`, `#settings-agent-add`, `#settings-save`, `#settings-cancel`, `#settings-close`, `.settings-agent-row`, `.settings-agent-name`, `.settings-agent-cmd`, `.settings-agent-delete`.
- New selectors: `#settings-theme`, `#settings-overrides`, `#settings-overrides-error`.

**Decision — Appearance applies immediately, Cancel does not revert it.** The agent list is a transactional draft (Go owns validation, one `SaveCustomAgents` call). The theme is a local preference with no server round-trip and no validation to fail; making it wait for Save would mean either previewing without persisting (two sources of truth) or not previewing at all (you cannot pick a theme you cannot see). It therefore writes to `localStorage` and to the DOM on change, and Cancel/Escape leave it applied. The section carries a hint saying so.

- [ ] **Step 1: Delete the `#settings` block from `index.html`** (verified at plan time: `index.html:126-146`). Nothing else in the file references it.

- [ ] **Step 2: Rewrite `settings.ts`**

The module keeps its existing structure — module-scope refs, `draft`/`loading`/`loadFailed`/`openToken`, `render()`, the open/close pair — and swaps hand-built markup for primitives. Everything below the fold of `openSettings`/`closeSettings`/`saveSettings`/`setEditingEnabled` is unchanged from the current file; reproduce it verbatim rather than rewriting it, and keep every comment (the re-entry guard, `loadFailed`, `openToken`, the delete-button refocus) — each documents a real bug.

```ts
// ---------- settings (appearance + custom agents) ----------
//
// The panel is built here rather than declared in index.html: it is the
// only way the dialog and field primitives can own the markup, and the
// Appearance section's controls are data-driven off PRESETS anyway.
// The ids the keyboard pipeline and the e2e specs key off are set
// explicitly and are part of this module's contract.
//
// The agent list is edited as a local draft and written in one
// SaveCustomAgents call. Go owns validation and ID assignment — new
// rows are saved with an empty id and come back slugged. IDs are
// deliberately not editable: registry entries persist only the agent
// id, so changing one would break revive for every session already
// created with that agent.
//
// Appearance is NOT part of that draft. It is a local preference with
// no round-trip and nothing to validate, so it applies and persists on
// change — you cannot choose a theme you are not allowed to look at.
// Cancel therefore does not revert it, and the section says so.

import { ListCustomAgents, SaveCustomAgents } from '../../bridge.js';
import { dialog } from '../../ui/dialog.js';
import { button } from '../../ui/button.js';
import {
  field,
  textInput,
  selectInput,
  textareaInput,
  colorInput,
  errorSlot,
} from '../../ui/field.js';
import {
  PRESETS,
  THEME_KEY,
  applyTheme,
  readTheme,
  applyOverrides,
  readOverrides,
  writeOverrides,
  sanitizeOverrides,
  type ThemeName,
} from '../../theme/theme.js';
import { applyXtermTheme } from '../session-term.js';
import type { main } from '../../../wailsjs/go/models';

export interface SettingsDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
}

let deps: SettingsDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

const DEFAULT_COLOR = '#64748b'; // ui-lint: allow — agent data default, not a theme token

// ---- Appearance ----------------------------------------------------

const themeError = errorSlot('settings-overrides-error');

const themeSelect = selectInput({
  id: 'settings-theme',
  ariaLabel: 'Theme',
  options: PRESETS.map((p) => ({ value: p.id, label: p.label })),
  onChange: (value) => selectPreset(value as ThemeName),
});

const overridesInput = textareaInput({
  id: 'settings-overrides',
  ariaLabel: 'Custom tokens',
  rows: 4,
  placeholder: '--accent: #7aa2f7;',
  onInput: (value) => applyUserOverrides(value),
});

// One place stamps the theme, so the three things that must happen
// together cannot drift apart: the attribute, the terminals (xterm
// caches its palette; a CSS change alone leaves every open session on
// the old colours), and the store.
function selectPreset(name: ThemeName): void {
  applyTheme(name);
  applyXtermTheme();
  try {
    localStorage.setItem(THEME_KEY, name);
  } catch {
    // Denied storage: applied for this session, not remembered.
  }
}

// applyUserOverrides runs on every keystroke, which is what makes the
// box usable — but it must never leave a half-typed line showing as an
// error while the user is still typing it. Rejected lines are reported;
// accepted ones apply. Typing "--acc" reports one rejected line and
// changes nothing, which is the honest answer.
function applyUserOverrides(raw: string): void {
  const { css, rejected } = sanitizeOverrides(raw);
  applyOverrides(css);
  applyXtermTheme();
  writeOverrides(css);
  themeError.show(
    rejected.length === 0
      ? ''
      : `Ignored ${rejected.length} line(s) — only "--token: value;" declarations are allowed: ${rejected.join(' / ')}`,
  );
}

function appearanceSection(): Node[] {
  const h = document.createElement('h4');
  h.textContent = 'Appearance';
  const hint = document.createElement('p');
  hint.textContent =
    'Applies as you change it, and is remembered. Cancel does not undo it.';
  const tokensHint = document.createElement('p');
  tokensHint.textContent =
    'Override any design token, one declaration per line. Fonts must already be installed on this machine.';
  return [
    h,
    hint,
    field('Theme', themeSelect),
    tokensHint,
    field('Custom tokens', overridesInput),
    themeError.el,
  ];
}

// ---- Custom agents -------------------------------------------------

const listEl = document.createElement('div');
listEl.id = 'settings-agents-list';

const agentsError = errorSlot('settings-error');
// The e2e and DOM specs assert on `.settings-error`; the primitive's
// own class carries the styling.
agentsError.el.classList.add('settings-error');

const addBtn = button({
  label: 'Add agent',
  icon: 'plus',
  onClick: () => addAgentRow(),
});
addBtn.id = 'settings-agent-add';

const saveBtn = button({ label: 'Save', kind: 'primary', onClick: () => saveSettings() });
saveBtn.id = 'settings-save';
const cancelBtn = button({ label: 'Cancel', onClick: () => closeSettings() });
cancelBtn.id = 'settings-cancel';

function agentsSection(): Node[] {
  const h = document.createElement('h4');
  h.textContent = 'Custom agents';
  const hint = document.createElement('p');
  hint.textContent =
    'Define your own tools — a command and its arguments. They appear in the new-session menu alongside the built-ins.';
  return [h, hint, listEl, addBtn, agentsError.el];
}

// ---- The dialog ----------------------------------------------------

const dlg = dialog({
  id: 'settings',
  title: 'Settings',
  size: 'md',
  body: [...appearanceSection(), ...agentsSection()],
  actions: [cancelBtn, saveBtn],
  onClose: () => closeSettings(),
});
// keyboard.ts and the tests import this; it must stay the root element.
export const settingsEl = dlg.el;
// The primitive's close button needs the id the e2e specs focus.
dlg.el.querySelector('.hv-dialog__close')?.setAttribute('id', 'settings-close');
```

`render()` becomes the same function with primitive-built rows:

```ts
function render() {
  listEl.replaceChildren();
  if (loading) {
    listEl.append(hintPara('Loading…'));
    return;
  }
  if (draft.length === 0) {
    listEl.append(hintPara('No custom agents yet.'));
    return;
  }

  draft.forEach((a, i) => {
    const row = document.createElement('div');
    row.className = 'settings-agent-row';

    const color = colorInput({
      value: a.color || DEFAULT_COLOR,
      ariaLabel: 'Agent color',
      onInput: (v) => {
        draft[i].color = v;
      },
    });
    const name = textInput({
      className: 'settings-agent-name',
      placeholder: 'Name (e.g. Claude Lite)',
      ariaLabel: 'Agent name',
      value: a.name || '',
      onInput: (v) => {
        draft[i].name = v;
      },
    });
    const cmd = textInput({
      className: 'settings-agent-cmd',
      placeholder: 'Command (e.g. claude --model haiku)',
      ariaLabel: 'Agent command',
      value: (a.cmd || []).join(' '),
      onInput: (v) => {
        draft[i].cmd = splitCommand(v);
      },
    });
    const del = iconButton({
      icon: 'x',
      label: `Delete ${a.name || 'agent'}`,
      onClick: () => deleteAgentRow(i),
    });
    del.classList.add('settings-agent-delete');

    row.append(color.el, name, cmd, del);
    listEl.append(row);
  });
}
```

with `deleteAgentRow` carrying the existing refocus comment verbatim:

```ts
function deleteAgentRow(i: number) {
  draft.splice(i, 1);
  agentsError.clear();
  render();
  // render() destroyed the button that had focus, dropping it to
  // <body> — from there the Tab trap has no boundary to wrap and the
  // next Tab walks behind the backdrop. Put focus back on the row that
  // took this one's place, or on "Add agent".
  const dels = listEl.querySelectorAll<HTMLElement>('.settings-agent-delete');
  (dels[Math.min(i, dels.length - 1)] ?? addBtn).focus();
}
```

`openSettings` gains one line — seed the Appearance controls from the store, since the user may have changed the theme elsewhere (the Phase 6 `system` preset follows the OS) — and otherwise keeps its re-entry guard and load token exactly as they are:

```ts
export function openSettings() {
  // Re-entry must not discard an in-progress draft. (…keep the full
  // existing comment: the macOS File ▸ Settings… accelerator path.)
  if (dlg.isOpen()) return;
  themeSelect.value = readTheme();
  overridesInput.value = readOverrides().replace(/\n\s*/g, '\n');
  themeError.clear();
  agentsError.clear();
  draft = [];
  loading = true;
  loadFailed = false;
  const token = ++openToken;
  setEditingEnabled(false);
  render();
  dlg.show();
  // Drop the active tile's visual focus and pull focus into the
  // dialog — same discipline as the help overlay. Without this, focus
  // stays on the terminal and keystrokes leak behind the backdrop.
  deps.setFocusedTile(null);
  document.getElementById('settings-close')?.focus();
  // …ListCustomAgents() chain unchanged.
}
```

`closeSettings` swaps `settingsEl.classList.add('hidden')` for `dlg.hide()` and `showError('')` for `agentsError.clear()`; everything else is unchanged.

`initSettings` loses the four `addEventListener` wirings that moved into the primitives (close button, cancel, save, add) and the whole backdrop mousedown/click pair and the Escape branch — `dialog()` owns those now. What remains:

```ts
export function initSettings(injected: SettingsDeps) {
  deps = injected;
  document.getElementById('app')?.append(settingsEl);
  // Enter in a text field saves, as before. Escape and the backdrop are
  // the dialog primitive's. The Appearance controls are excluded: Enter
  // in a <textarea> is a newline, and this listener would otherwise turn
  // "next line of overrides" into "save and close".
  settingsEl.addEventListener('keydown', (e) => {
    if (
      e.key === 'Enter' &&
      e.target instanceof HTMLInputElement &&
      e.target.type === 'text'
    ) {
      e.preventDefault();
      saveSettings();
    }
  });
}
```

- [ ] **Step 3: Keep the layout rules that are not the primitives' job**

Delete `#settings*` and `.settings-*` from `style.css` except the agent-row layout, which is Settings' own composition, not a primitive. Keep (retokenised if Phases 2–4 have not already):

```css
/* Settings — agent rows. The dialog shell, fields and buttons come from
   the primitives; this is only how the four controls share one line. */
#settings-agents-list { display: flex; flex-direction: column; gap: var(--space-2); }
.settings-agent-row { display: flex; align-items: center; gap: var(--space-2); }
.settings-agent-row .settings-agent-name { flex: 1 1 34%; min-width: 0; }
.settings-agent-row .settings-agent-cmd { flex: 1 1 66%; min-width: 0; }
.settings-agent-delete:hover { color: var(--state-error); }
```

Phase 6 moves this into `src/theme/components/`. Leaving it in `style.css` now keeps this diff to the dialog migration.

- [ ] **Step 4: Update `test/dom/settings.test.ts`**

Its `MARKUP` constant mounts the `#settings` block that no longer exists in `index.html`. Replace the fixture with `document.body.innerHTML = '<div id="app"></div>'` plus `initSettings({ … })`, which now builds the DOM itself. The assertions (save payload, id survival across rename, error surfaced on rejection) are the point of the file and must not change. Two new mocks are needed since the module now imports them:

```ts
vi.mock('../../src/app/session-term.js', () => ({ applyXtermTheme: vi.fn() }));
```

- [ ] **Step 5: Run the whole gate, including the specs this must not break**

Run: `npx vitest run && npm run typecheck && npx biome ci . && npx playwright test test/e2e/settings.spec.ts test/e2e/focus-traps.spec.ts test/e2e/focus-invariants.spec.ts && ../../../scripts/ui-lint.sh --strict`
Expected: green with **no edits to the three e2e specs**. If `focus-traps.spec.ts` fails, the cause is almost certainly focusable order or a control that is now `opacity: 0` — the colour input. Fix `field.css` (it stays focusable by design), not the spec.

- [ ] **Step 6: Commit**

```bash
git add cmd/hivegui/frontend/src/app/modals/settings.ts cmd/hivegui/frontend/index.html \
        cmd/hivegui/frontend/src/style.css cmd/hivegui/frontend/test/dom/settings.test.ts
git commit -m "feat(settings): rebuild on the dialog primitive and add an Appearance section"
```

---

### Task 6: Worktrees and the project editor

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/modals/worktrees.ts`, `project-editor.ts`
- Modify: `cmd/hivegui/frontend/index.html` (delete `#worktrees` and `#project-editor` blocks)
- Modify: `cmd/hivegui/frontend/src/style.css` (delete the shell rules; keep row/list layout)
- Modify: `cmd/hivegui/frontend/test/dom/worktrees.test.ts` (fixture, as in Task 5)

- [ ] **Step 1: Project editor**

Smallest of the four. The whole `#project-editor` block becomes:

```ts
const nameInput = textInput({ id: 'project-editor-name', ariaLabel: 'Name' });
const cwdInput = textInput({ id: 'project-editor-cwd', ariaLabel: 'Working directory' });
const browseBtn = button({ label: 'Browse…', onClick: () => pickDirectory() });
browseBtn.id = 'project-editor-browse';
const color = colorInput({ value: DEFAULT_PROJECT_COLOR, ariaLabel: 'Color' });

const cwdRow = document.createElement('div');
cwdRow.className = 'cwd-row';
cwdRow.append(cwdInput, browseBtn);

const cancelBtn = button({ label: 'Cancel', onClick: () => closeProjectEditor() });
cancelBtn.id = 'project-editor-cancel';
const saveBtn = button({ label: 'Save', kind: 'primary', onClick: () => saveProjectEditor() });
saveBtn.id = 'project-editor-save';

const dlg = dialog({
  id: 'project-editor',
  title: 'New project',
  size: 'sm',
  body: [field('Name', nameInput), field('Working directory', cwdRow), field('Color', color.el)],
  actions: [cancelBtn, saveBtn],
  onClose: () => closeProjectEditor(),
});
export const editorEl = dlg.el;
```

`openProjectEditor` calls `dlg.setTitle(project ? 'Edit project' : 'New project')` and `dlg.show()`; it keeps the synchronous `nameInput.focus()` and its comment verbatim (the ⌘N-then-Escape race is real). `closeProjectEditor` keeps `releaseFocus(editorEl)` before `dlg.hide()` — the primitive does not do this, deliberately, because only the module knows where focus is going next. `initProjectEditor` keeps only the Enter-saves branch and the `new-project-btn` wiring; Escape and the backdrop move to the primitive.

Note: today's editor has no close button and no backdrop dismissal (`index.html:80-100` — it is a bare `role="dialog"` with no `aria-modal`). Adding both is the correct outcome of the migration, and `dialog()` gives it `aria-modal="true"` — which `focus-invariants.spec.ts` may assert against. Check that spec before assuming.

- [ ] **Step 2: Worktrees**

Same shape at `lg` size, with the footer hint that `patterns.md` names explicitly:

```ts
const projectSuffix = document.createElement('span');
projectSuffix.id = 'worktrees-project';
projectSuffix.className = 'worktrees-project';

const dlg = dialog({
  id: 'worktrees',
  title: 'Worktrees',
  size: 'lg',
  body: [emptyEl, bodyEl],
  hints: [kbd('esc'), text(' close · '), kbd('r'), text(' refresh')],
  onClose: () => closeWorktrees(),
});
dlg.setTitleSuffix(projectSuffix);
export const worktreesEl = dlg.el;
```

`hints` is where the `[esc] close · (r) refresh` line from `index.html:117` goes, now rendered through `kbd()` per `patterns.md` › Keyboard hints (`[…]` for symbols, `(…)` for letters — `kbd()` from Phase 2 owns the bracket style; pass the bare key). `emptyEl` and `bodyEl` are the existing `#worktrees-empty` / `#worktrees-body` subtrees, built in TS with the same ids and classes so `render()`, `showEmpty()` and every `worktrees.spec.ts` selector are untouched.

`initWorktrees` keeps its `r`/`R` refresh branch (including the `!(e.target instanceof HTMLInputElement)` guard for inline rename) and drops the Escape branch and the backdrop listener. `closeWorktrees` keeps `dismissChoiceDialog()` and `releaseFocus()` before `dlg.hide()`.

- [ ] **Step 3: `style.css`** — delete the `#worktrees` / `#worktrees-panel` / `#worktrees-close` / `#project-editor` shell rules (verified at plan time: 1149–1234 and 1851–1900ish; re-check the end of the worktrees block). Keep the row, list, section, badge and empty-card rules — the browser's contents are not a primitive.

- [ ] **Step 4: Run**

Run: `npx vitest run && npm run typecheck && npx biome ci . && npx playwright test test/e2e/worktrees.spec.ts test/e2e/focus-traps.spec.ts test/e2e/focus-invariants.spec.ts && ../../../scripts/ui-lint.sh --strict`
Expected: green, no spec edits.

- [ ] **Step 5: Commit**

```bash
git add cmd/hivegui/frontend/src/app/modals/worktrees.ts \
        cmd/hivegui/frontend/src/app/modals/project-editor.ts \
        cmd/hivegui/frontend/index.html cmd/hivegui/frontend/src/style.css \
        cmd/hivegui/frontend/test/dom/worktrees.test.ts
git commit -m "feat(ui): move the worktree browser and project editor onto the dialog primitive"
```

---

### Task 7: Help overlay and choice dialog

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/modals/help-overlay.ts`, `choice-dialog.ts`
- Modify: `cmd/hivegui/frontend/index.html` (delete the `#help-overlay` block)
- Modify: `cmd/hivegui/frontend/src/style.css` (delete the shell rules, keep `#help-overlay-groups` typography)

- [ ] **Step 1: Help overlay** — `dialog({ id: 'help-overlay', title: 'Keyboard shortcuts', size: 'lg', body: [helpGroupsEl], hints: [kbd('esc'), text(' close')], onClose: closeHelpOverlay })`. `helpGroupsEl` keeps its id and its render-once flag. `renderHelpOverlay()` swaps its hand-built `<kbd>` for `kbd(item.keys)` — that is the whole point of the primitive (`patterns.md`: "Feature modules never format hints by hand"). `toggleHelpOverlay` uses `dlg.isOpen()`.

- [ ] **Step 2: Choice dialog** — this one is built per question and unregistered on close, so it uses `dialog()` differently: build, `show()` immediately, `el.remove()` and `unregisterModal(el)` in `finish()`.

```ts
const dlg = dialog({
  id: 'choice-dialog',
  role: 'alertdialog',
  title: spec.title,
  size: 'sm',
  body: [detailPara, bulletList, notePara],
  actions: spec.choices.map((c) =>
    button({
      label: c.label,
      kind: c.danger ? 'danger' : 'default',
      onClick: () => finish(c.value),
    }),
  ),
  // The FIRST choice is the safe one: Escape and a scrim click resolve
  // to it, so a stray key can never destroy anything.
  onClose: () => finish(spec.choices[0].value),
  showCloseButton: false,
});
```

Three things must survive:
1. `dataset.choice = c.value` on each action button — `worktrees.spec.ts` selects on it. `button()` returns the element, so set it after construction.
2. `choiceDialogEl()` returns `dlg.el`, and `keyboard.ts:153` traps Tab from it. Unchanged.
3. `finish()` must call `unregisterModal(dlg.el)` before `dlg.el.remove()`. `dialog()` calls `registerModal` for every dialog it builds; the four static ones are never removed, so only this one needs the pair. **`registry.ts`'s existing comment already explains why**: a detached element has no `hidden` class, so leaving it registered makes `anyModalOpen()` answer true forever and permanently strands the keyboard. Getting this wrong is the single highest-risk line in this phase.
4. The opener-refocus (`if (opener?.isConnected) opener.focus()`) and its comment stay verbatim.

Rename `id: 'choice-dialog'`: today the element has only `class="choice-dialog"`. An id is new; check `worktrees.spec.ts` and `focus-traps.spec.ts` for `.choice-dialog` selectors and **keep the class as well** (`dlg.el.classList.add('choice-dialog')`) so nothing has to be edited.

- [ ] **Step 3: Run**

Run: `npx vitest run && npm run typecheck && npx biome ci . && npx playwright test && ../../../scripts/ui-lint.sh --strict`
Expected: the full e2e suite green. Per the project memory, a `test/e2e-real` scroll/wheel failure is the known flake — confirm against `main` before touching it; a `test/e2e` failure here is yours.

- [ ] **Step 4: Commit**

```bash
git add cmd/hivegui/frontend/src/app/modals/help-overlay.ts \
        cmd/hivegui/frontend/src/app/modals/choice-dialog.ts \
        cmd/hivegui/frontend/index.html cmd/hivegui/frontend/src/style.css
git commit -m "feat(ui): move the help overlay and choice dialog onto the dialog primitive"
```

---

### Task 8: e2e for the Appearance flow

**Files:**
- Modify: `cmd/hivegui/frontend/test/e2e/theme.spec.ts` (created in Phase 1)

- [ ] **Step 1: Write the spec**

```ts
// Settings › Appearance, driven through the real dialog. The unit tests
// prove the sanitiser; this proves the wiring — that picking a preset
// repaints the app, that a bad override line is reported instead of
// injected, and that a good one survives a reload.
test.describe('Settings › Appearance', () => {
  const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

  async function openAppearance(page: Page) {
    await page.goto('/');
    await page.waitForFunction(
      () => document.querySelectorAll('#projects li').length > 0,
    );
    await page.keyboard.press(`${mod}+,`);
    await expect(page.locator('#settings')).toBeVisible();
  }

  test('picking a preset repaints the sidebar and is remembered', async ({
    page,
  }) => {
    await openAppearance(page);
    const sidebarBg = () =>
      page
        .locator('#sidebar')
        .evaluate((el) => getComputedStyle(el).backgroundColor);
    const before = await sidebarBg();

    await page.locator('#settings-theme').selectOption('hive-light');
    await expect(page.locator('html')).toHaveAttribute(
      'data-theme',
      'hive-light',
    );
    expect(await sidebarBg()).not.toBe(before);
    // hive-light's --surface is #ffffff (themes.css).
    expect(await sidebarBg()).toBe('rgb(255, 255, 255)');

    // Cancel does not revert it — Appearance is a preference, not part
    // of the agent draft.
    await page.locator('#settings-cancel').click();
    await expect(page.locator('html')).toHaveAttribute(
      'data-theme',
      'hive-light',
    );
    expect(
      await page.evaluate(() => localStorage.getItem('hive.theme')),
    ).toBe('hive-light');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute(
      'data-theme',
      'hive-light',
    );
  });

  test('the preset list is exactly what theme.ts exports', async ({ page }) => {
    await openAppearance(page);
    // Guards the "data-driven from PRESETS" requirement: phase 6 adds
    // native-*/terminal by editing theme.ts alone.
    const values = await page
      .locator('#settings-theme option')
      .evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value));
    expect(values).toEqual(['system', 'hive-dark', 'hive-light', 'classic']);
  });

  test('a good override applies live; a bad line is reported, not injected', async ({
    page,
  }) => {
    await openAppearance(page);
    const accent = () =>
      page.evaluate(() =>
        getComputedStyle(document.documentElement)
          .getPropertyValue('--accent')
          .trim(),
      );

    await page.locator('#settings-overrides').fill('--accent: #7aa2f7;');
    expect(await accent()).toBe('#7aa2f7');
    await expect(page.locator('#settings-overrides-error')).toBeHidden();

    await page
      .locator('#settings-overrides')
      .fill('--accent: #7aa2f7;\nbody { display: none }');
    await expect(page.locator('#settings-overrides-error')).toBeVisible();
    await expect(page.locator('#settings-overrides-error')).toContainText(
      'Ignored',
    );
    // The good line still applies and the bad one did not escape the
    // :root block — the sidebar is still on screen.
    expect(await accent()).toBe('#7aa2f7');
    await expect(page.locator('#sidebar')).toBeVisible();

    // Only the sanitised text is persisted.
    expect(
      await page.evaluate(() => localStorage.getItem('hive.themeOverrides')),
    ).toBe('--accent: #7aa2f7;');
  });

  test('an override reaches every open terminal', async ({ page }) => {
    await openAppearance(page);
    await page.locator('#settings-overrides').fill('--term-bg: #123456;');
    await expect
      .poll(() =>
        page.evaluate(() => {
          const t = [...window.__hive.terms().values()][0];
          return t?.term?.options?.theme?.background ?? '';
        }),
      )
      .toBe('#123456');
  });
});
```

`window.__hive.terms()` may not exist on the mock — check `test/e2e/wails-mock.ts` and `test/e2e/hive-global.d.ts`. If it does not, either add it there (it is a test-only accessor over `state.terms`) or assert the rendered `.xterm-screen` background instead. Do not assert on a screenshot: the terminal is masked in the Phase 1 baselines for a reason.

- [ ] **Step 2: Guard the Phase 1 baselines**

The screenshot tests assert `classic` is pixel-identical. Dialogs are now primitive-built, so `settings-classic.png` **will** differ. That is an intended visual change (this is the "dialogs" phase per the master plan), so regenerate that one baseline and say so in the PR:

Run: `npx playwright test test/e2e/theme.spec.ts --update-snapshots --grep "settings dialog"`

`sidebar-classic.png` must **not** move. If it does, something in `style.css` was deleted that the sidebar was using.

- [ ] **Step 3: Run**

Run: `npx playwright test test/e2e/theme.spec.ts`
Expected: all green.

- [ ] **Step 4: Commit**

```bash
git add cmd/hivegui/frontend/test/e2e/theme.spec.ts \
        cmd/hivegui/frontend/test/e2e/theme.spec.ts-snapshots
git commit -m "test(theme): cover the Settings Appearance flow end to end"
```

---

### Task 9: Docs, changeset, bookkeeping

**Files:**
- Modify: `docs/design-docs/ui/README.md` (Status → "Phases 1–5 implemented")
- Modify: `docs/design-docs/ui/themes.md` (the "Settings → Appearance" line now describes shipped behaviour; add that overrides are stored sanitised and that Cancel does not revert)
- Modify: `docs/exec-plans/active/ui-design-system.md` (Progress: tick Phase 5; decision-log entries for the two decisions below)
- Create: `.changesets/<pr>-ui-dialogs-appearance.md` via `/hs-changelog-update`

Decision-log entries to add:
- **Appearance applies on change, not on Save.** Why: a theme with no round-trip and no validation has nothing to be transactional about, and a preview you cannot see is not a picker.
- **Overrides are sanitised on write and stored as finished CSS.** Why: the pre-paint boot script would otherwise need a second copy of the sanitiser; `theme.ts` re-sanitises on read so a hand-edited store is still safe.

- [ ] **Step 1: Edit the docs, then run the full gate**

Run:
```bash
cd cmd/hivegui/frontend && npx biome ci . && npm run typecheck && npx vitest run && npx playwright test \
  && cd ../../.. && scripts/ui-lint.sh --strict && go build ./...
```

- [ ] **Step 2: Commit and open the PR**

```bash
git add docs .changesets
git commit -m "docs(ui): mark phase 5 of the design system implemented"
```

PR title: `feat(ui): design-system phase 5 — dialog + fields, Settings › Appearance`. Body: link the spec, note the intentional `settings-classic.png` baseline change, and paste the `ui-lint --strict` output.

---

## Self-review

- **Spec coverage.** components.md › `dialog` (backdrop, panel, sizes, 44px header, focus-trap reuse, Escape, aria) → Task 1; › Form fields (label above, 28px input, colour swatch) → Task 2; themes.md › Presets selection (dropdown, System) → Tasks 3+5; › User overrides (sanitiser regex, both localStorage keys, `<style id="theme-overrides">` after `themes.css`, error slot) → Tasks 3+5+8; › xterm re-theme on change → Task 4; patterns.md › Errors (dialog error slot, never also `flashStatus`) → Task 2 `errorSlot` + Task 5; › Keyboard hints (`kbd()`, footer `[esc] close · (r) refresh`) → Tasks 6+7. **Not covered, by design:** `native-*`/`terminal` presets and the contrast check are Phase 6 — `PRESETS` is the seam that makes them a one-line addition, which Task 8's second test pins.
- **Placeholders.** None. Task 5's "reproduce the rest verbatim" is an instruction not to rewrite working code, not a TODO.
- **Type consistency.** `DialogHandle` is consumed identically in Tasks 5–7 (`el`, `show`, `hide`, `isOpen`, `setTitle`, `setTitleSuffix`); `Sanitized` flows from `sanitizeOverrides` → `applyUserOverrides` → the e2e assertion on the same rejected-line count; `applyXtermTheme()` has one producer (Task 4) and two callers (Task 5).
- **Highest risk: `unregisterModal` in the choice dialog** (Task 7, step 2, item 3). `dialog()` registers unconditionally; the choice dialog is the only one that is removed from the DOM. Miss the pair and `anyModalOpen()` is permanently true and the keyboard is permanently stranded — a bug the existing code has a comment about because it happened before. `test/e2e/worktrees.spec.ts` covers the delete flow and should catch it; verify it does before trusting it.
- **Second risk: focus order changed by the colour swatch.** `focusableWithin` filters on `disabled`/`hidden`/`.hidden`, not on visibility, so an `opacity: 0` colour input stays in the tab order — correct, and what `focus-traps.spec.ts` expects. But if Phases 2–4 changed the swatch to `display: none` for any reason, Tab silently skips it. Task 5 step 5 says to fix the CSS, not the spec.
- **Known ceiling.** The override sanitiser is line-based and cannot express a value containing a semicolon or a CSS comment. That is what `themes.md` specifies and no token value needs either. Upgrade path if it bites: parse with `CSSStyleDeclaration` (`el.style.cssText = input`, then read back only `--*` properties), which the browser validates for free — but it is unavailable in the node-environment unit tests, which is why it is not the first choice.
- **Deliberately not built.** No live preview of a preset before committing to it, no "reset to defaults" button (clear the textarea, pick System), no per-token colour pickers, no import/export of themes. Each is a feature request, not a gap in this spec.

## PR convergence ledger

- **2026-08-31 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 02a077f9…; threads_open: 0; action: escalated:autofix produced no changes (its Phase 3/4 confirmation gates cannot be satisfied non-interactively); head_sha: 2cd077f.
- **2026-08-31 iter 1b** — findings applied by hand in the interactive session (1 BLOCKING, 8 IMPORTANT, 4 MINOR), each verified in Chromium or under `--sequence.shuffle` before and after; head_sha: 0cc237e.
- **2026-08-31 iter 2** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: d514bf67…; threads_open: 0; action: escalated:findings need parent-session fixes; head_sha: a7addd1. Caught two regressions from iter 1's own fixes — the `scrollIntoView` call turned every CI leg red (vitest exits 1 on unhandled rejections while still printing "736 passed"), and the new debounce outlived `closeSettings`.
- **2026-08-31 iter 2b** — findings applied by hand (1 BLOCKING, 5 IMPORTANT, 5 MINOR) plus four new regression tests; the debounce test was verified to fail without its fix. Gate re-checked on **exit codes**, not just the summary line.
- **2026-08-31 rebase** — rebased onto `origin/main` (clean). `main` landed `lib/preserve-focus.ts`, which restores focus after sidebar/grid re-renders, so a test now pins that a re-render cannot reach into an open dialog; the two changes were developed in parallel and nothing else pinned that contract.
- **2026-08-31 iter 3** — verdict: **APPROVE**; mergeable: MERGEABLE; findings_hash: empty; threads_open: 0; action: stop; head_sha: 0938faf. Zero BLOCKING, zero IMPORTANT, no regressions from iter 2's fixes; three agent claims refuted on inspection. Two MINORs: the `aria-describedby` link on the custom-tokens box (fixed) and the boot gate's missing balanced-paren check (left as the documented cheap-gate trade-off — reaching it needs a hand-edited store, and the worst case is one paint without overrides).
- **Stage note** — the spec stays at `IMPLEMENT`, not `GATE`. `/hs-review-loop` §4a advances REVIEW→GATE by default, but this programme's decision log (2026-08-30) says the umbrella spec is gated **once, after phase 6**, because its success criteria span all six phases; phase PRs merge on green CI plus a converged review loop. Phase 4 left it at `IMPLEMENT` for the same reason.

## Gate verdict (advisory — phase-scoped)

The umbrella spec is gated once after phase 6 (see the programme decision log), so `/hs-merge-gate` refuses at `stage: IMPLEMENT` and its success criteria include phase-6 items this PR never scoped. This entry is the same three dimensions run against **phase 5's own scope** instead, and is advisory: it does not advance any stage.

- **2026-08-31** — verdict: PASS (advisory); checks: 3 dimensions / 0 failed / 0 followups; followups: none; one-line: phase-5 scope delivered, docs accurate, no scope bleed.
  - 2026-08-31 dimensions:
    - acceptance — PASS — all 8 phase-5 criteria exercised, most through the Playwright mock harness rather than by reading code; full local gate green on exit codes (biome, tsc, 747 vitest, 229 e2e, ui-lint --strict, go build).
    - non-goals — PASS — `session-term.ts` / `state.ts` / `keyboard.ts` diffs confined to `applyXtermTheme`, the `term.options.theme` type and the project-editor trap; every control on `main`'s four static dialogs accounted for in the rebuilt TS; the only IA change is the sanctioned Custom-agents-above-Appearance ordering plus the pinned Updates section.
    - doc accuracy — PASS — changeset valid and `CHANGELOG.md` / `index.md` untouched; every claim in `themes.md`, `README.md`, `components.md` and `patterns.md` re-checked against the code, including that no `--ansi-*` token exists.
  - Refuted: the non-goals worker reported `.worktrees-project` as a styling regression (its rule was deleted with the worktrees shell CSS). Measured in Chromium: the span sits inside `.hv-dialog__title-suffix`, and `color`/`font-weight` inherit — it renders `rgb(102,102,102)` at weight 400 against the title's `rgb(221,221,221)` at 600, i.e. muted and normal-weight as on `main`. The redundant class was dropped rather than re-styled.

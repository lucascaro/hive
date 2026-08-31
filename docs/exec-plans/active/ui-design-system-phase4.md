# UI design system — Phase 4: chrome (banner, status bar, tile header, launcher rows, empty/phase states)

**PR:** https://github.com/lucascaro/hive/pull/301
**Branch:** `feature/ui-design-system-phase4`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reskin every non-sidebar, non-dialog surface — banners, status bar, grid tile headers, launcher and command-palette rows, empty state, phase checklist, boot card — onto tokens and the `src/ui/` primitive layer, and add the two primitives this phase needs (`button`, `banner`). After this phase the only surfaces still carrying hand-written classes and Unicode glyphs are the dialogs (Phase 5).

**Architecture:** Two new primitives join `src/ui/`: `button({label,kind,icon,onClick})` returning an `HTMLButtonElement`, and `banner({text,kind,actions,onDismiss})` returning a small handle (`el`, `setText`, `action(id)`, `show`, `hide`) so `banners.ts` can keep its per-build dismissal, disabled-while-restarting and download-URL logic without reaching into markup. Each primitive owns `src/theme/components/<name>.css`. Feature modules stop building `<button>`/`<div>` chrome by hand: `banners.ts` constructs both banners in TS (their markup leaves `index.html`), `dom.ts` gains a two-slot status bar whose right slot renders mode hints through `kbd()`, `session-term.ts`'s tile header switches to `stateIcon()` + `iconButton()`, and `view.ts` / `launcher.ts` / `command-palette.ts` render rows from primitives. The status-bar mode hints are computed by a new pure `modeHints(view, isMac)` in `src/lib/status.ts` so the mapping from view mode to shortcut is unit-testable and stays in sync with `keymap`.

**Tech Stack:** Vanilla TS, Vite 8, vitest 4 (`test/unit` node, `test/dom` jsdom), Playwright 1.62 (`test/e2e` + `wails-mock.ts`), `scripts/ui-lint.sh`. No new dependencies.

**Spec:** `docs/design-docs/ui/components.md` (`button`, `iconButton`, `kbd`, `banner`, `statusBar`, `launcherItem`, `Grid tile header`, "What is *not* a primitive"), `patterns.md` (empty and loading states, errors, keyboard hints, hover-revealed actions, motion), `tokens.md`, `icons.md`, `AGENTS.md` › UX Best Practices.

## Global Constraints

- **Prerequisite:** Phases 1–3 are landed. This phase assumes `src/ui/` already exports `icon(name, { size? })`, `stateIcon(state)`, `updateStateIcon(el, state)`, `iconButton({ icon, label, onClick })`, `kbd(text)`, `chip(...)`, and `resolveSessionState(session, attention)`; and that `src/theme/components/` plus its aggregating stylesheet exist. If Phase 3 shipped a different aggregation mechanism (one `<link>` per component file rather than an `index.css` of `@import`s), follow whatever is already there instead of adding a second mechanism.
- **Every `path:line` reference below was read on the pre-phase-1..3 tree. Re-verify each one with `grep -n` before editing** — phases 1–3 move lines in `session-term.ts`, `view.ts`, `style.css` and `index.html`. Line numbers are navigation aids, not contracts; the quoted snippets are the contract.
- Tokens only. No hex, no `px` font sizes, no `rgba()` literals in the CSS this phase writes; derived shades use one `color-mix()` per use site. No Unicode glyph as UI in `src/app/**` or `index.html` — `·`, `…` and modifier symbols (`⌘⇧⌥⌃`) are text and stay.
- Class names are `hv-<name>` / `hv-<name>__<part>`; variants are data attributes (`data-kind`, `data-state`, `data-selected`), never extra classes.
- Every icon-only control gets `aria-label` with `title` mirrored (`iconButton()` does this; do not bypass it).
- Motion: only what `patterns.md` allows (hover 120ms, attention pulse, starting spinner). All of it reads `--motion-*`.
- Gate for every task: `npx biome ci . && npm run typecheck && npx vitest run` from `cmd/hivegui/frontend/`, plus `./scripts/ui-lint.sh --strict` from the repo root. Playwright where the task touches rendered DOM.
- Commits: conventional, one per task. Run every frontend command from `cmd/hivegui/frontend/`. Fresh worktree → `./scripts/ci-bootstrap.sh` first or `npm run typecheck` fails on missing `wailsjs/`.

---

### Task 1: `button()` primitive

**Files:**
- Create: `cmd/hivegui/frontend/src/ui/button.ts`
- Create: `cmd/hivegui/frontend/src/theme/components/button.css`
- Modify: `cmd/hivegui/frontend/src/theme/components/index.css` (add the `@import`; if Phase 3 linked components individually, add a `<link>` in `index.html` instead)
- Test: `cmd/hivegui/frontend/test/dom/ui-button.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export type ButtonKind = 'default' | 'primary' | 'danger' | 'ghost';
  export interface ButtonOpts {
    label: string;
    kind?: ButtonKind;
    icon?: IconName;
    onClick?: (e: MouseEvent) => void;
  }
  export function button(opts: ButtonOpts): HTMLButtonElement;
  ```
- Consumes: `icon`, `IconName` from `src/ui/icon.ts` (Phase 2).

- [x] **Step 1: Failing test**

```ts
// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { button } from '../../src/ui/button.js';

describe('button()', () => {
  it('renders a type=button with the label and the default kind', () => {
    const el = button({ label: 'Restart Hive' });
    expect(el.tagName).toBe('BUTTON');
    expect(el.type).toBe('button');
    expect(el.className).toBe('hv-button');
    expect(el.dataset.kind).toBe('default');
    expect(el.textContent).toBe('Restart Hive');
  });

  it('carries the kind as a data attribute, not a class', () => {
    const el = button({ label: 'Kill', kind: 'danger' });
    expect(el.dataset.kind).toBe('danger');
    expect(el.className).toBe('hv-button');
  });

  it('prepends a leading icon before the label span', () => {
    const el = button({ label: 'New session', icon: 'plus' });
    expect(el.firstElementChild?.tagName.toLowerCase()).toBe('svg');
    expect(el.querySelector('.hv-button__label')?.textContent).toBe(
      'New session',
    );
  });

  it('wires onClick', () => {
    const spy = vi.fn();
    button({ label: 'Go', onClick: spy }).click();
    expect(spy).toHaveBeenCalledTimes(1);
  });
});
```

- [x] **Step 2: Run to verify failure**

Run: `npx vitest run test/dom/ui-button.test.ts`
Expected: FAIL — cannot resolve `../../src/ui/button.js`.

- [x] **Step 3: Implement**

```ts
// The only way a feature module makes a labelled button. Kinds are
// data attributes so a variant never needs a second class and CSS can
// select on [data-kind]. docs/design-docs/ui/components.md › button.
import { icon, type IconName } from './icon.js';

export type ButtonKind = 'default' | 'primary' | 'danger' | 'ghost';

export interface ButtonOpts {
  label: string;
  kind?: ButtonKind;
  icon?: IconName;
  onClick?: (e: MouseEvent) => void;
}

export function button({
  label,
  kind = 'default',
  icon: iconName,
  onClick,
}: ButtonOpts): HTMLButtonElement {
  const el = document.createElement('button');
  el.type = 'button';
  el.className = 'hv-button';
  el.dataset.kind = kind;
  if (iconName) el.append(icon(iconName, { size: 14 }));
  // The label lives in its own span so the icon can never be squeezed
  // by text-overflow, and so CSS can target the text alone.
  const text = document.createElement('span');
  text.className = 'hv-button__label';
  text.textContent = label;
  el.append(text);
  if (onClick) el.addEventListener('click', onClick);
  return el;
}
```

- [x] **Step 4: `button.css`**

```css
/* docs/design-docs/ui/components.md › button. Height 28px, --text-md,
   --radius-sm; kinds are [data-kind], never extra classes. */
.hv-button {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  height: 28px;
  padding: 0 var(--space-3);
  border-radius: var(--radius-sm);
  font-family: var(--font-ui);
  font-size: var(--text-md);
  line-height: 1.4;
  cursor: pointer;
  background: var(--btn);
  border: 1px solid var(--btn-border);
  color: var(--fg-muted);
  transition: background var(--motion-fast) ease;
}
.hv-button[data-kind='primary'] {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--on-accent);
  font-weight: 500;
}
.hv-button[data-kind='danger'] {
  background: transparent;
  border-color: var(--state-error);
  color: var(--state-error);
}
.hv-button[data-kind='ghost'] {
  background: transparent;
  border-color: transparent;
  color: var(--fg-muted);
}
.hv-button:hover { background: var(--hover); }
.hv-button[data-kind='primary']:hover {
  background: color-mix(in srgb, var(--accent) 88%, var(--bg));
}
.hv-button:active { filter: brightness(0.92); }
.hv-button:disabled {
  opacity: 0.5;
  pointer-events: none;
}
.hv-button:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
```

Register it: append `@import './button.css';` to `src/theme/components/index.css`.

- [x] **Step 5: Run the gate**

Run: `npx vitest run test/dom/ui-button.test.ts && npm run typecheck && npx biome ci . && (cd ../../.. && ./scripts/ui-lint.sh --strict)`
Expected: 4 passed, typecheck clean, biome clean, `ui-lint: 0 violation(s)`.

- [x] **Step 6: Commit**

```bash
git add src/ui/button.ts src/theme/components/button.css src/theme/components/index.css test/dom/ui-button.test.ts
git commit -m "feat(ui): add button primitive"
```

---

### Task 2: `banner()` primitive

**Files:**
- Create: `cmd/hivegui/frontend/src/ui/banner.ts`
- Create: `cmd/hivegui/frontend/src/theme/components/banner.css`
- Modify: `cmd/hivegui/frontend/src/theme/components/index.css`
- Test: `cmd/hivegui/frontend/test/dom/ui-banner.test.ts`

**Interfaces:**
- Produces:
  ```ts
  export type BannerKind = 'error' | 'info';
  export interface BannerAction { id: string; label: string; onClick: () => void; }
  export interface BannerOpts {
    kind: BannerKind;
    text?: string;
    actions?: BannerAction[];
    onDismiss?: () => void;
    id?: string;   // stamped onto the root so e2e/dom tests keep their selectors
  }
  export interface Banner {
    el: HTMLDivElement;
    setText(text: string): void;
    action(id: string): HTMLButtonElement;  // throws on an unknown id
    show(): void;
    hide(): void;
  }
  export function banner(opts: BannerOpts): Banner;
  ```
- Consumes: `button` (Task 1), `iconButton` (Phase 2).
- Consumed by: Task 3 (`banners.ts`).

Why a handle and not a bare element: `banners.ts` needs to retitle the banner, disable the Restart button while a restart is in flight, and toggle the Download button's visibility per response — three things it does today by holding module-level element references. `action(id)` hands those back without re-querying by class.

- [x] **Step 1: Failing test**

```ts
// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { banner } from '../../src/ui/banner.js';

describe('banner()', () => {
  it('starts hidden, with the kind as data and the right aria role', () => {
    const b = banner({ kind: 'error', text: 'daemon build mismatch' });
    expect(b.el.hidden).toBe(true);
    expect(b.el.dataset.kind).toBe('error');
    expect(b.el.getAttribute('role')).toBe('alert');
    expect(b.el.querySelector('.hv-banner__text')?.textContent).toBe(
      'daemon build mismatch',
    );
  });

  it('uses role=status for the info kind', () => {
    expect(banner({ kind: 'info' }).el.getAttribute('role')).toBe('status');
  });

  it('exposes actions by id and runs their handler', () => {
    const spy = vi.fn();
    const b = banner({
      kind: 'error',
      actions: [{ id: 'restart', label: 'Restart Hive', onClick: spy }],
    });
    const btn = b.action('restart');
    expect(btn.textContent).toBe('Restart Hive');
    btn.click();
    expect(spy).toHaveBeenCalledTimes(1);
    expect(() => b.action('nope')).toThrow(/nope/);
  });

  it('renders a dismiss icon button only when onDismiss is given', () => {
    const spy = vi.fn();
    const withD = banner({ kind: 'info', onDismiss: spy });
    const dismiss = withD.el.querySelector<HTMLButtonElement>(
      '.hv-banner__dismiss',
    );
    expect(dismiss?.getAttribute('aria-label')).toBe('Dismiss');
    dismiss?.click();
    expect(spy).toHaveBeenCalledTimes(1);
    expect(
      banner({ kind: 'info' }).el.querySelector('.hv-banner__dismiss'),
    ).toBeNull();
  });

  it('show/hide/setText drive the root', () => {
    const b = banner({ kind: 'info', id: 'update-banner' });
    expect(b.el.id).toBe('update-banner');
    b.setText('Hive 2.5.0 is available.');
    b.show();
    expect(b.el.hidden).toBe(false);
    b.hide();
    expect(b.el.hidden).toBe(true);
  });
});
```

- [x] **Step 2: Run to verify failure**

Run: `npx vitest run test/dom/ui-banner.test.ts`
Expected: FAIL — cannot resolve `../../src/ui/banner.js`.

- [x] **Step 3: Implement**

```ts
// Full-width notice row above the app grid. Owns its own markup so no
// feature module hand-writes banner DOM; `banners.ts` drives it through
// the returned handle. docs/design-docs/ui/components.md › banner.
import { button } from './button.js';
import { iconButton } from './iconButton.js';

export type BannerKind = 'error' | 'info';

export interface BannerAction {
  id: string;
  label: string;
  onClick: () => void;
}

export interface BannerOpts {
  kind: BannerKind;
  text?: string;
  actions?: BannerAction[];
  onDismiss?: () => void;
  /** Stamped onto the root; keeps existing #daemon-banner selectors alive. */
  id?: string;
}

export interface Banner {
  el: HTMLDivElement;
  setText(text: string): void;
  action(id: string): HTMLButtonElement;
  show(): void;
  hide(): void;
}

export function banner({
  kind,
  text = '',
  actions = [],
  onDismiss,
  id,
}: BannerOpts): Banner {
  const el = document.createElement('div');
  el.className = 'hv-banner';
  el.dataset.kind = kind;
  // An error banner interrupts (assertive); an info banner reports.
  el.setAttribute('role', kind === 'error' ? 'alert' : 'status');
  if (id) el.id = id;
  // `hidden`, not a .hidden class: the property is the platform's own
  // channel and CSS can't accidentally lose to a later rule.
  el.hidden = true;

  const textEl = document.createElement('span');
  textEl.className = 'hv-banner__text';
  textEl.textContent = text;
  el.append(textEl);

  const byId = new Map<string, HTMLButtonElement>();
  for (const a of actions) {
    // kind 'ghost': the banner ground already carries the emphasis;
    // a filled button inside it reads as a second alert.
    const b = button({ label: a.label, kind: 'ghost', onClick: a.onClick });
    b.classList.add('hv-banner__action');
    byId.set(a.id, b);
    el.append(b);
  }

  if (onDismiss) {
    const d = iconButton({ icon: 'x', label: 'Dismiss', onClick: onDismiss });
    d.classList.add('hv-banner__dismiss');
    el.append(d);
  }

  return {
    el,
    setText: (t: string) => {
      textEl.textContent = t;
    },
    action: (aid: string) => {
      const b = byId.get(aid);
      if (!b) throw new Error(`banner: no action "${aid}"`);
      return b;
    },
    show: () => {
      el.hidden = false;
    },
    hide: () => {
      el.hidden = true;
    },
  };
}
```

If Phase 2 exported `iconButton` from a different path or filename (e.g. `src/ui/icon-button.ts`), fix the import — do not add a re-export shim.

- [x] **Step 4: `banner.css`**

```css
/* docs/design-docs/ui/components.md › banner. 36px, --text-md, left
   border carries the kind. Grid placement (rows 1 and 2 of #app) comes
   from [data-slot], set by app/banners.ts. */
.hv-banner {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  height: 36px;
  padding: 0 var(--space-4);
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  border-left: 3px solid var(--border);
  font-family: var(--font-ui);
  font-size: var(--text-md);
  color: var(--fg-muted);
  grid-column: 1 / -1;
}
.hv-banner[hidden] { display: none; }
.hv-banner[data-kind='error'] { border-left-color: var(--state-error); }
.hv-banner[data-kind='info'] { border-left-color: var(--accent); }
.hv-banner__text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--fg);
}
.hv-banner__action { flex-shrink: 0; }
.hv-banner__dismiss { flex-shrink: 0; }
.hv-banner[data-slot='daemon'] { grid-row: 1; }
.hv-banner[data-slot='update'] { grid-row: 2; }
```

Append `@import './banner.css';` to `src/theme/components/index.css`.

- [x] **Step 5: Run the gate**

Run: `npx vitest run test/dom/ui-banner.test.ts && npm run typecheck && npx biome ci .`
Expected: 5 passed, clean.

- [x] **Step 6: Commit**

```bash
git add src/ui/banner.ts src/theme/components/banner.css src/theme/components/index.css test/dom/ui-banner.test.ts
git commit -m "feat(ui): add banner primitive"
```

---

### Task 3: `banners.ts` on the primitive; banner markup leaves `index.html`

The daemon-stale and update banners are 8 hand-written elements in `index.html` (`index.html:25-34`) and ~120 lines of `pageEl` plumbing. The behaviour — per-build dismissal, per-version `localStorage` dismissal, auto-hide for transient messages, the re-entrancy guard on `restartHive()` — is all worth keeping verbatim; only the DOM construction changes.

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/banners.ts` (element lookups at `banners.ts:36-39` and `banners.ts:133-136`; `showDaemonBanner`, `hideDaemonBanner`, `showUpdateBanner`, `hideUpdateBanner`, `wireDaemonBanner`, `wireUpdateBanner`)
- Modify: `cmd/hivegui/frontend/index.html` (delete `#daemon-banner` and `#update-banner` blocks)
- Modify: `cmd/hivegui/frontend/src/style.css` (delete `#daemon-banner`/`#update-banner` rules — around `style.css:1437-1500` and the `#daemon-banner button:focus-visible` / `#update-banner button:focus-visible` entries in the focus block near `style.css:1840`; keep `#update-banner { grid-row: 2; grid-column: 1 / -1; }` deleted too, `banner.css` owns placement now)
- Modify: `cmd/hivegui/frontend/test/dom/restart-hive.test.ts` (scaffold: the banner markup is gone)

**Interfaces:**
- Unchanged public surface: `initBanners()`, `restartHive()`, `manualUpdateCheck()`, `isDaemonRestarting()`.

- [x] **Step 1: Replace the module-level element handles**

Delete the six `pageEl(...)` banner lookups and build the banners instead. Note `mount()` is called at `initBanners()` time, not on import — `banners.ts` is dragged into jsdom tests that mount only partial markup, and an import-time `document.body` write would fight them.

```ts
import { banner, type Banner } from '../ui/banner.js';

// Built lazily by initBanners(): the module is imported by jsdom tests
// that never open a banner, and a mount on import would inject markup
// into scaffolds that don't expect it.
let daemonBanner: Banner | null = null;
let updateBanner: Banner | null = null;
let daemonBannerDismissedFor: string | null = null;
let daemonRestarting = false;

function mountBanners() {
  if (daemonBanner) return;
  daemonBanner = banner({
    kind: 'error',
    id: 'daemon-banner',
    actions: [{ id: 'restart', label: 'Restart Hive', onClick: restartHive }],
    onDismiss: () => {
      // Dismissals are per-daemon-build: a *different* mismatched build
      // later still surfaces.
      daemonBannerDismissedFor = daemonBanner?.el.dataset.daemonBuild || '';
      daemonBanner?.hide();
    },
  });
  daemonBanner.el.dataset.slot = 'daemon';
  updateBanner = banner({
    kind: 'info',
    id: 'update-banner',
    actions: [
      {
        id: 'download',
        label: 'Download',
        onClick: () => {
          const url = updateBanner?.el.dataset.url;
          if (url) OpenURL(url).catch(reportFailure('open link'));
        },
      },
    ],
    onDismiss: () => {
      const v = updateBanner?.el.dataset.version || '';
      if (v) {
        try {
          localStorage.setItem(UPDATE_DISMISS_KEY, v);
        } catch {}
      }
      updateBanner?.hide();
    },
  });
  updateBanner.el.dataset.slot = 'update';
  // Prepended so the two banners are grid rows 1 and 2, above the
  // sidebar+terms row, exactly where the markup used to sit.
  const app = document.getElementById('app');
  app?.prepend(daemonBanner.el, updateBanner.el);
}
```

- [x] **Step 2: Rewrite the four show/hide helpers against the handles**

```ts
function showDaemonBanner(text: string) {
  daemonBanner?.setText(text);
  daemonBanner?.show();
}
function hideDaemonBanner() {
  daemonBanner?.hide();
}

function showUpdateBanner(
  text: string,
  { downloadUrl = '', showDownload = true, autoHideMs = 0 } = {},
) {
  if (!updateBanner) return;
  updateBanner.setText(text);
  // A banner with no trusted URL still tells the user an update exists;
  // it just doesn't offer a one-click Download for an untrusted target.
  updateBanner.action('download').hidden = !(showDownload && downloadUrl);
  updateBanner.el.dataset.url = downloadUrl;
  // Cleared on every show — only the "available" branch sets it back,
  // so dismissing a transient banner can't write a stale version.
  delete updateBanner.el.dataset.version;
  updateBanner.show();
  if (updateBannerAutoHideTimer) {
    clearTimeout(updateBannerAutoHideTimer);
    updateBannerAutoHideTimer = null;
  }
  if (autoHideMs > 0) {
    updateBannerAutoHideTimer = setTimeout(() => {
      hideUpdateBanner();
      updateBannerAutoHideTimer = null;
    }, autoHideMs);
  }
}
function hideUpdateBanner() {
  updateBanner?.hide();
}
```

`restartHive()` keeps its body; replace the two `daemonBannerRestart.disabled = …` lines with `daemonBanner?.action('restart').disabled = …` — but hoist the button once at the top of the function (`const restartBtn = daemonBanner?.action('restart');`) so the `finally` block can't throw on a null handle when the menu path runs before `initBanners()`.

- [x] **Step 3: `wireDaemonBanner` / `wireUpdateBanner` lose their listener wiring**

The click handlers moved into `mountBanners()`. Both functions keep only their `EventsOn(...)` registration and (for update) the boot-time `CheckForUpdate()` poll. `initBanners()` becomes:

```ts
export function initBanners() {
  mountBanners();
  wireDaemonBanner();
  wireUpdateBanner();
}
```

`applyUpdateInfo` writes `updateBanner.el.dataset.version = info.latest` after `showUpdateBanner(...)`, exactly as it does today against `updateBannerEl`.

- [x] **Step 4: Delete the markup and the old CSS**

Remove both `<div id="…-banner">` blocks from `index.html`. Remove the `#daemon-banner*` and `#update-banner*` rule blocks from `style.css` including the two focus-visible selectors and the `#update-banner { grid-row: 2; … }` placement rule near the top of the file.

- [x] **Step 5: Fix `test/dom/restart-hive.test.ts`**

Its scaffold hard-codes the old markup. Replace the banner block with a mount root, and call `initBanners()` before asserting. The two assertions that read `bannerEl.classList.contains('hidden')` become `bannerEl.hidden`:

```ts
  document.body.innerHTML = `
    <div id="app">
      <div id="terms"></div><ul id="projects"></ul>
      <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    </div>`;
  ({ restartHive, isDaemonRestarting, initBanners } = await import(
    '../../src/app/banners.js'
  ));
  initBanners();
  bannerEl = mustEl('daemon-banner');
  bannerText = bannerEl.querySelector('.hv-banner__text') as HTMLElement;
```

`initBanners()` also registers `EventsOn` handlers and fires `CheckForUpdate()`; the file already mocks `../bridge.js`, so extend that mock with `EventsOn: vi.fn()` and `CheckForUpdate: vi.fn().mockResolvedValue(null)` if they aren't there.

- [x] **Step 6: Run**

Run: `npx vitest run test/dom/restart-hive.test.ts && npm run typecheck && npx biome ci . && npx playwright test test/e2e/smoke.spec.ts`
Expected: green. If `smoke.spec.ts` asserts on banner markup, update the selector to `#daemon-banner .hv-banner__text`.

- [x] **Step 7: Commit**

```bash
git add src/app/banners.ts index.html src/style.css test/dom/restart-hive.test.ts
git commit -m "refactor(ui): build daemon and update banners from the banner primitive"
```

---

### Task 4: Status bar reskin — 24px bar, tokens, mode hints in the right slot

Today `#status` is a fixed pill floating bottom-right (`style.css:1410-1435`), 11px Menlo on a black `rgba()` wash, holding one string. `components.md` › statusBar wants a 24px bar on `--surface` with a top border, a persistent left slot and a right slot carrying the current mode's top 1–2 shortcuts through `kbd()` (`patterns.md` › Keyboard hints).

**Files:**
- Modify: `cmd/hivegui/frontend/index.html` (`#status` gains two slots; `#app` grid gains a row)
- Modify: `cmd/hivegui/frontend/src/lib/status.ts` (add pure `modeHints`)
- Modify: `cmd/hivegui/frontend/src/app/dom.ts` (`status` handle, `render`, new `setModeHint`)
- Modify: `cmd/hivegui/frontend/src/app/view.ts` (`setView` and `switchTo` call `setModeHint`)
- Create: `cmd/hivegui/frontend/src/theme/components/status-bar.css`
- Modify: `cmd/hivegui/frontend/src/style.css` (delete `#status` rules), `src/theme/components/index.css`
- Test: `cmd/hivegui/frontend/test/unit/status-hints.test.ts`
- Modify: `cmd/hivegui/frontend/test/e2e/silent-failures.spec.ts` (`#status` → `#status-text`)

**Interfaces:**
- Produces (in `src/lib/status.ts`):
  ```ts
  export interface ModeHint { key: string; label: string; }
  export function modeHints(view: string, mac: boolean): ModeHint[];
  ```
- Produces (in `src/app/dom.ts`): `export function setModeHint(hints: ModeHint[]): void;`

- [x] **Step 1: Failing unit test for the hint table**

```ts
import { describe, it, expect } from 'vitest';
import { modeHints } from '../../src/lib/status.js';

describe('modeHints', () => {
  it('offers grid + palette in single view', () => {
    expect(modeHints('single', true)).toEqual([
      { key: '⌘G', label: 'grid' },
      { key: '⌘K', label: 'actions' },
    ]);
  });

  it('offers focus + move in a grid view', () => {
    expect(modeHints('grid-all', true)).toEqual([
      { key: '⌘G', label: 'focus' },
      { key: '⌥↑↓←→', label: 'move' },
    ]);
    expect(modeHints('grid-project', true)).toEqual(modeHints('grid-all', true));
  });

  it('spells modifiers out off macOS', () => {
    expect(modeHints('single', false)[0].key).toBe('Ctrl+G');
  });

  it('never shows more than two hints', () => {
    for (const v of ['single', 'grid-all', 'grid-project', 'nonsense']) {
      expect(modeHints(v, true).length).toBeLessThanOrEqual(2);
    }
  });
});
```

- [x] **Step 2: Run → FAIL** (`modeHints` not exported).

- [x] **Step 3: Implement `modeHints` in `src/lib/status.ts`**

Appended below `createStatus`; the module stays pure (no DOM, no platform sniffing — `mac` is a parameter).

```ts
// The status bar's right slot. patterns.md › Keyboard hints: "the status
// bar right slot shows the current mode's top 1–2 shortcuts". Kept here,
// beside the controller, and pure so a test can assert the table without
// a DOM. Modifier spelling follows AGENTS.md (symbols on macOS, words
// elsewhere); the keys themselves must match app/keymap.ts.
export interface ModeHint {
  key: string;
  label: string;
}

export function modeHints(view: string, mac: boolean): ModeHint[] {
  const mod = mac ? '⌘' : 'Ctrl+';
  const alt = mac ? '⌥' : 'Alt+';
  if (view === 'grid-all' || view === 'grid-project') {
    return [
      { key: `${mod}G`, label: 'focus' },
      { key: `${alt}↑↓←→`, label: 'move' },
    ];
  }
  return [
    { key: `${mod}G`, label: 'grid' },
    { key: `${mod}K`, label: 'actions' },
  ];
}
```

Verify `⌘K` and `⌥`+arrows are what `src/app/keymap.ts` actually binds for the palette and grid spatial move before committing to those labels; if the palette is on a different chord, use the real one — a hint that lies is worse than no hint (AGENTS.md › Consistency).

- [x] **Step 4: Two-slot markup in `index.html`**

```html
  <div id="status" role="status" aria-live="polite">
    <span id="status-text">connecting…</span>
    <span id="status-hint"></span>
  </div>
```

and give it a grid row. In `style.css`'s `#app` block, extend the row template and place the bar:

```css
  /* Row 1 daemon banner, row 2 update banner, row 3 sidebar + terms,
     row 4 minimized tray, row 5 status bar. */
  grid-template-rows: auto auto 1fr auto auto;
```

- [x] **Step 5: `dom.ts` — render into the left slot, add `setModeHint`**

```ts
import { createStatus, type ModeHint } from '../lib/status.js';
import { kbd } from '../ui/kbd.js';

export const status = mustEl('status');
const statusText = mustEl('status-text');
const statusHint = mustEl('status-hint');

const statusCtl = createStatus({
  render: (text: string, isError: boolean) => {
    statusText.textContent = text;
    // The error tint is on the bar, not the span: the whole row flashes.
    status.classList.toggle('error', isError);
  },
  setTimer: (fn: () => void, ms: number) => window.setTimeout(fn, ms),
  clearTimer: (id: number) => window.clearTimeout(id),
  now: () => Date.now(),
});

// The right slot. Rebuilt wholesale — two hints is never enough DOM to
// be worth diffing.
export function setModeHint(hints: ModeHint[]): void {
  statusHint.replaceChildren(
    ...hints.flatMap((h) => {
      const label = document.createElement('span');
      label.className = 'hv-status__hint-label';
      label.textContent = h.label;
      return [kbd(h.key), label];
    }),
  );
}
```

`mustEl('status-text')` throws if the markup is missing — extend the jsdom scaffolds in `test/dom/*.test.ts` that mount a bare `<div id="status"></div>` (grep: `id="status"`) to the two-slot form. There are several; they all get the same three-line block.

- [x] **Step 6: Feed it from `view.ts`**

`setView()` ends with `setStatus(\`${view}${active ? \` • ${active.name}\` : ''}\`)` (`view.ts:~740`). Replace the mode-name-in-the-text with the real thing: the left slot keeps the session name, the right slot carries the mode.

```ts
  setStatus(active ? (active.name ?? '') : '');
  setModeHint(modeHints(view, isMac));
```

`switchTo()` also needs `setModeHint(modeHints(state.view, isMac))` after its `setStatus(...)` — switching sessions can fall back out of a grid view (`fallBackToSingleIfActiveHidden`), and a stale "focus / move" hint on a single pane is exactly the lying hint AGENTS.md forbids. `isMac` is already imported in `view.ts`.

- [x] **Step 7: `status-bar.css`**

```css
/* docs/design-docs/ui/components.md › statusBar. 24px, --surface, top
   border, --text-xs --fg-muted. Left slot persistent, right slot the
   current mode's shortcuts. */
#status {
  grid-row: 5;
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  height: 24px;
  padding: 0 var(--space-3);
  background: var(--surface);
  border-top: 1px solid var(--border);
  font-family: var(--font-ui);
  font-size: var(--text-xs);
  color: var(--fg-muted);
}
#status-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
#status-hint {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
  color: var(--fg-subtle);
}
/* Space between a hint's label and the next hint's key, without a
   separator glyph. */
.hv-status__hint-label { margin-right: var(--space-2); }
.hv-status__hint-label:last-child { margin-right: 0; }
#status.error { color: var(--state-error); }
```

The old `status-error-pulse` keyframes go with the old rules: `patterns.md` › Motion allows the attention pulse and the starting spinner only. The 6s dwell in `createStatus` is what makes an error readable, not an animation.

Delete `#status`, `#status.error`, `@keyframes status-error-pulse` and its `prefers-reduced-motion` override from `style.css`; append `@import './status-bar.css';` to `src/theme/components/index.css`.

- [x] **Step 8: Update the e2e selectors**

`test/e2e/silent-failures.spec.ts` asserts `page.locator('#status')` has exact text (`silent-failures.spec.ts:21,42,58,77,103,131,134`). With a hint slot, `toHaveText` on `#status` now includes the hints. Point every text assertion at `#status-text`; leave the `toHaveClass(/error/)` assertions on `#status`.

Add one assertion that the right slot exists and tracks the mode:

```ts
test('the status bar right slot shows the current mode shortcuts', async ({
  page,
}) => {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  const hint = page.locator('#status-hint');
  await expect(hint).toContainText('grid');
  await page.keyboard.press(`${mod}+Shift+g`);
  await expect(page.locator('#terms')).toHaveClass(/grid/);
  await expect(hint).toContainText('focus');
  await expect(hint.locator('kbd').first()).toBeVisible();
});
```

(`mod` is already defined in that spec, or copy the one-liner from `theme.spec.ts:14`.)

- [x] **Step 9: Run the gate**

Run: `npx vitest run && npm run typecheck && npx biome ci . && npx playwright test test/e2e/silent-failures.spec.ts && (cd ../../.. && ./scripts/ui-lint.sh --strict)`
Expected: green. The arrow glyphs `↑↓←→` and `⌘⇧⌥` are on the ui-lint allow-list from Phase 1 — if `↑↓` are not, add them to `ALLOW` in `scripts/ui-lint.sh` in this commit with a comment (they are keyboard hint characters, which `icons.md` › Rules explicitly permits).

- [x] **Step 10: Commit**

```bash
git add index.html src/lib/status.ts src/app/dom.ts src/app/view.ts src/style.css \
  src/theme/components/status-bar.css src/theme/components/index.css \
  test/unit/status-hints.test.ts test/dom test/e2e/silent-failures.spec.ts
git commit -m "feat(ui): reskin the status bar as a 24px bar with a mode-hint slot"
```

---

### Task 5: Grid tile header

`components.md` › Grid tile header: *28px, `--surface`, bottom border. `stateIcon` + name `--text-sm` 500 + `·` + window title `--fg-subtle`, hover actions right.* Today (`session-term.ts:238-306`) it is a colour dot, name, a `⎇` Unicode worktree glyph, the window title behind an em-dash `::before`, an uppercase project label, and a `–` minimize button — over a two-stop project→session colour gradient (`style.css:748-844`).

Two facts change channel: the coloured dot becomes the state icon (one of the three places `icons.md` allows a state icon), and the project colour drops out of the header background into nothing — the sidebar already carries project identity, and `README.md` principle 2 is one channel per fact. Keep `--session-color` on the host: the tile border and the attention ring still read it.

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/session-term.ts` (constructor `:238-306`, `setInfo` `:1048-1061`, `_renderTermTitle` `:1064-1073`, `setProject` `:1111-1114`, `setPhase` `:1482`)
- Modify: `cmd/hivegui/frontend/src/app/view.ts` (`renderGrid` attention toggle, `view.ts:306`)
- Create: `cmd/hivegui/frontend/src/theme/components/tile-header.css`
- Modify: `cmd/hivegui/frontend/src/style.css` (delete `.term-host .tile-*` rules and the `#terms.grid .term-host.in-grid.attention .tile-name::before` dot), `src/theme/components/index.css`
- Test: `cmd/hivegui/frontend/test/e2e/silent-failures.spec.ts` (tile rename path uses `.tile-name` — keep that class name)

**Interfaces:**
- Consumes: `stateIcon`, `updateStateIcon`, `resolveSessionState`, `iconButton` from `src/ui/`.
- Class names are preserved (`tile-header`, `tile-name`, `tile-term-title`, `tile-name-input`) because `inline-rename.ts`, `theme.spec.ts:71` and `silent-failures.spec.ts:97-121` select on them. Only the leaf that was a Unicode glyph or a colour dot changes identity.

- [x] **Step 1: Rebuild the header in the constructor**

```ts
    // Tile header (only visible in grid mode via CSS).
    // components.md › Grid tile header: state icon, name, '·', window
    // title, hover actions. The project colour is deliberately NOT in
    // this bar any more — the sidebar owns project identity (README
    // principle 2), and the tile keeps --session-color for its border.
    this.header = document.createElement('div');
    this.header.className = 'tile-header';
    this.header.setAttribute('aria-label', `Session ${info.name}`);

    this.tileState = stateIcon(resolveSessionState(info, state.attention));
    this.tileState.classList.add('tile-state');

    this.tileName = document.createElement('span');
    this.tileName.className = 'tile-name';
    this.tileName.textContent = info.name ?? '';

    // OSC-set window title from the running TUI (vim, htop, claude…).
    this.tileTermTitle = document.createElement('span');
    this.tileTermTitle.className = 'tile-term-title';

    // Hover actions, right-aligned. patterns.md › Hover-revealed
    // actions: hidden until hover or focus-within, and they replace the
    // meta column rather than pushing the text.
    this.tileActions = document.createElement('div');
    this.tileActions.className = 'tile-actions';
    this.tileWorktree = iconButton({
      icon: 'branch',
      label: 'Manage worktrees',
      onClick: (e) => {
        // The tile header also focuses/activates the tile; this click is
        // about the worktree, not about switching sessions.
        e.stopPropagation();
        const pid = this.info?.projectId ?? this.info?.project_id ?? '';
        const proj = state.projects.find((p) => p.id === pid);
        if (proj) openWorktrees(proj);
      },
    });
    this.tileWorktree.classList.add('tile-worktree');
    this.tileMinimize = iconButton({
      icon: 'minus',
      label: 'Minimize session',
      onClick: (e) => {
        e.stopPropagation();
        minimizeSession(this.info.id);
      },
    });
    this.tileMinimize.classList.add('tile-minimize');
    // Block the surrounding tile mousedown so minimizing doesn't also
    // select / switch to this tile.
    this.tileMinimize.addEventListener('mousedown', (e) => e.stopPropagation());
    this.tileActions.append(this.tileWorktree, this.tileMinimize);

    this.tileProject = document.createElement('span');
    this.tileProject.className = 'tile-project';

    this.header.append(
      this.tileState,
      this.tileName,
      this.tileTermTitle,
      this.tileProject,
      this.tileActions,
    );
```

Field declarations at `session-term.ts:143-150`: drop `tileColor`, add `tileState: SVGElement` (whatever `stateIcon()` returns — match its declared type) and `tileActions: HTMLDivElement`; `tileWorktree` / `tileMinimize` become `HTMLButtonElement`.

If `iconButton`'s `onClick` is typed `() => void` rather than `(e: MouseEvent) => void`, keep the `e.stopPropagation()` by attaching a separate `click` listener in capture phase instead of widening the primitive's signature.

- [x] **Step 2: Keep the state icon current**

`setInfo` and `setPhase` are the two edges that can change the resolved state; `renderGrid` is the one that knows about attention.

In `setInfo` (`session-term.ts:1048`), replace the `tileWorktree.style.display` juggling and add the icon refresh:

```ts
  setInfo(info: SessionInfo) {
    this.info = info;
    this.host.style.setProperty('--session-color', info.color || '#888');
    this.tileName.textContent = info.name ?? '';
    this.header.setAttribute('aria-label', `Session ${info.name}`);
    const wtBranch = info.worktreeBranch ?? info.worktree_branch;
    this.tileWorktree.hidden = !wtBranch;
    if (wtBranch) {
      this.tileWorktree.title = `Worktree: ${wtBranch} — click to manage worktrees`;
    }
    this.refreshStateIcon();
    this._renderTermTitle();
  }

  // One place resolves the tile's state icon, so the tile can never
  // disagree with the sidebar row about what a session is doing.
  refreshStateIcon() {
    updateStateIcon(
      this.tileState,
      resolveSessionState(this.info, state.attention),
    );
  }
```

Call `this.refreshStateIcon()` at the end of `setPhase` (both branches — the `!isReady` early return needs it too), and in `view.ts:306` replace the attention class toggle's neighbourhood with:

```ts
    st.host.classList.toggle('attention', state.attention.has(info.id));
    st.refreshStateIcon();
```

Keep the `.attention` class on the host: the tile border/glow still uses it, and Phase 3's sidebar does the same.

- [x] **Step 3: `_renderTermTitle` loses the em-dash**

The `—` came from CSS `content`. `·` replaces it (a text separator, allowed by `icons.md` › Rules), and it moves into the CSS of `.tile-term-title::before` in the new file. The TS is unchanged except `style.display` → `hidden`:

```ts
    this.tileTermTitle.textContent = t;
    if (t) this.tileTermTitle.title = t;
    this.tileTermTitle.hidden = !t;
```

- [x] **Step 4: `tile-header.css`**

```css
/* docs/design-docs/ui/components.md › Grid tile header. 28px, --surface,
   bottom border, state icon + name + '·' + window title, hover actions. */
.term-host .tile-header {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 28px;
  padding: 0 var(--space-2);
  background: var(--surface);
  border-bottom: 1px solid var(--border);
  font-family: var(--font-ui);
  font-size: var(--text-sm);
  color: var(--fg-muted);
  flex-shrink: 0;
}
.term-host.term-focused .tile-header {
  /* Focus reads as a session-coloured hairline under the bar, not as a
     brighter fill: the terminal is the product, chrome recedes. */
  border-bottom-color: var(--session-color, var(--accent));
  color: var(--fg);
}
.term-host .tile-state { flex-shrink: 0; }
.term-host .tile-name {
  flex: 0 1 auto;
  min-width: 0;
  max-width: 40%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 500;
  color: var(--fg);
}
.term-host .tile-term-title {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--fg-subtle);
}
.term-host .tile-term-title[hidden] { display: none; }
.term-host .tile-term-title::before {
  content: '\00b7';
  margin-right: var(--space-2);
  color: var(--fg-subtle);
}
.term-host .tile-name-input {
  flex: 1;
  min-width: 0;
  height: 20px;
  background: var(--surface-raised);
  color: var(--fg);
  border: 1px solid var(--accent);
  border-radius: var(--radius-sm);
  padding: 0 var(--space-1);
  font: inherit;
  outline: none;
}
.term-host .tile-project {
  margin-left: auto;
  flex-shrink: 0;
  color: var(--fg-subtle);
  font-size: var(--text-xs);
  text-transform: uppercase;
  letter-spacing: 0.08em;
}
/* patterns.md › Hover-revealed actions: hidden until hover or keyboard
   focus within the row, and they REPLACE the meta column. */
.term-host .tile-actions {
  display: none;
  margin-left: auto;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
}
.term-host .tile-header:hover .tile-actions,
.term-host .tile-header:focus-within .tile-actions { display: flex; }
.term-host .tile-header:hover .tile-project,
.term-host .tile-header:focus-within .tile-project { display: none; }
```

Delete from `style.css`: `.term-host .tile-header`, `.term-host.term-focused .tile-header`, `.term-host .tile-color`, `.tile-name`, `.tile-term-title(::before)`, `.tile-name-input`, `.tile-project`, `.tile-minimize(:hover)`, the `#terms.grid .term-host.in-grid.attention .tile-name::before` `●` rule (the state icon now carries attention), and the `.tile-minimize:focus-visible` entry in the focus block. Append `@import './tile-header.css';` to `src/theme/components/index.css`.

The `\00b7` escape rather than a literal `·` keeps `scripts/ui-lint.sh`'s glyph rule (which scans `src/app/**` and `index.html`, not CSS) and any future CSS glyph rule both satisfied without an allow comment.

- [x] **Step 5: Run**

Run: `npm run typecheck && npx biome ci . && npx vitest run && npx playwright test test/e2e/silent-failures.spec.ts test/e2e/minimize.spec.ts test/e2e/focus.spec.ts`
Expected: green. `theme.spec.ts:71` clicks `.tile-minimize` — that class survives, but the button is now hidden until hover; Playwright's `.click()` hovers first, so it still works. If a spec instead asserts *visibility* of `.tile-minimize` without hovering, add an explicit `.hover()` on the tile header.

- [x] **Step 6: Commit**

```bash
git add src/app/session-term.ts src/app/view.ts src/style.css \
  src/theme/components/tile-header.css src/theme/components/index.css
git commit -m "feat(ui): rebuild the grid tile header on stateIcon and iconButton"
```

---

### Task 6: Launcher and command-palette rows

`components.md` › launcherItem: *32px, `--text-md`, leading `icon` (12px) for agent kind, trailing shortcut in `--font-mono --text-xs --fg-subtle`. Selected → `--sel` + accent bar, same as session row.*

Today the launcher row (`modals/launcher.ts:243-268`) is `[agent-num][agent-dot][agent-name][install-tag]` where `agent-num` is a bare digit and `agent-dot` is a coloured circle; the palette row (`modals/command-palette.ts:46-63`) is `[palette-name][palette-shortcut]` with the shortcut as raw text. Both need `kbd()` for their hints (`patterns.md` › Keyboard hints: "Feature modules never format hints by hand") and the shared selected treatment.

The agent colour is data, not a token, and is the one thing that tells two agents apart at a glance — keep it, as the `--agent-color` swatch the row already sets, rendered at 7px like `chip()`'s swatch. There is no per-agent icon in the sprite and `icons.md` forbids adding one per agent, so the "leading icon for agent kind" slot is the swatch.

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/modals/launcher.ts` (`renderLauncherList`, `launcher.ts:214-280`)
- Modify: `cmd/hivegui/frontend/src/app/modals/command-palette.ts` (`renderPalette`, `command-palette.ts:34-64`)
- Create: `cmd/hivegui/frontend/src/theme/components/launcher-item.css`
- Modify: `cmd/hivegui/frontend/src/style.css` (delete `.launcher-item*`, `.palette-item*`, `.agent-*`, `.install-tag`, `.palette-name`, `.palette-shortcut` rules — `style.css:1036-1075`, `:1143`, `:1535-1552`), `src/theme/components/index.css`
- Test: `cmd/hivegui/frontend/test/e2e/launcher-search.spec.ts`, `launcher-stacking.spec.ts` (class names preserved; verify only)

- [x] **Step 1: Launcher row**

Replace the row-building block inside `matches.forEach` with:

```ts
  matches.forEach((a, idx) => {
    const item = document.createElement('div');
    item.className = 'launcher-item';
    if (!a.available) item.dataset.available = 'false';
    item.style.setProperty('--agent-color', a.color);

    // Number keys 1–9 select that row directly; 10+ rows show no number.
    // While a query is active the digits type into it instead, so the
    // hints come off — a visible [n] that does nothing is worse than
    // none (AGENTS.md › Key Discoverability).
    const num = document.createElement('span');
    num.className = 'agent-num';
    if (!raw && idx < 9) num.append(kbd(`[${idx + 1}]`));

    const dot = document.createElement('span');
    dot.className = 'agent-dot';

    const name = document.createElement('span');
    name.className = 'agent-name';
    name.textContent = a.name;

    item.append(num, dot, name);
    if (!a.available && a.installCmd && a.installCmd.length) {
      const tag = document.createElement('span');
      tag.className = 'install-tag';
      tag.title = a.installCmd.join(' ');
      tag.textContent = 'install?';
      item.appendChild(tag);
    }
    item.addEventListener('click', () => launchSelected(a.id));
    item.addEventListener('mouseenter', () => {
      launcherState.selected = idx;
      highlightLauncherSelection();
    });
    listEl?.appendChild(item);
    launcherState.items.push({ agent: a, el: item });
  });
```

`uninstalled` moves from a class to `data-available="false"` per the Global Constraints (variants are data attributes). Check `highlightLauncherSelection()` and the launcher's keyboard handler for a `.uninstalled` selector before deleting the class, and update the CSS selector accordingly. If `highlightLauncherSelection()` toggles a `.selected` class, switch it to `el.toggleAttribute('data-selected', …)` in the same commit so the launcher and the palette agree.

Import `kbd` from `../../ui/kbd.js` at the top of `launcher.ts`.

- [x] **Step 2: Palette row**

```ts
  paletteState.items.forEach((c, i) => {
    const row = document.createElement('div');
    row.className = 'palette-item';
    if (i === paletteState.selected) row.dataset.selected = '';
    const name = document.createElement('span');
    name.className = 'palette-name';
    name.textContent = c.name;
    const sc = document.createElement('span');
    sc.className = 'palette-shortcut';
    // kbd() is the only way a key hint renders (patterns.md).
    if (c.shortcut) sc.append(kbd(c.shortcut));
    row.append(name, sc);
    row.addEventListener('mouseenter', () => {
      paletteState.selected = i;
      for (const el of paletteList.children) {
        (el as HTMLElement).removeAttribute('data-selected');
      }
      row.dataset.selected = '';
    });
    row.addEventListener('click', () => activatePalette(i));
    paletteList.appendChild(row);
  });
```

- [x] **Step 3: `launcher-item.css`** (owns both row kinds — one anatomy, two call sites)

```css
/* docs/design-docs/ui/components.md › launcherItem / command palette
   rows. 32px, --text-md, leading swatch, trailing kbd hint; selected
   uses the same --sel + accent bar as the session row. */
.launcher-item,
.palette-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: var(--space-2);
  height: 32px;
  padding: 0 var(--space-3);
  border-radius: var(--radius-sm);
  cursor: pointer;
  font-family: var(--font-ui);
  font-size: var(--text-md);
  color: var(--fg-muted);
  transition: background var(--motion-fast) ease;
}
.launcher-item:hover,
.palette-item:hover { background: var(--hover); }
.launcher-item[data-selected],
.palette-item[data-selected] {
  background: var(--sel);
  color: var(--fg);
}
.launcher-item[data-selected]::before,
.palette-item[data-selected]::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 2px;
  background: var(--accent);
  border-radius: var(--radius-sm) 0 0 var(--radius-sm);
}
.launcher-item .agent-num {
  flex-shrink: 0;
  min-width: 2.5ch;
}
.launcher-item .agent-dot {
  flex-shrink: 0;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--agent-color, var(--fg-subtle));
}
.launcher-item .agent-name { flex: 1; min-width: 0; }
.launcher-item[data-available='false'] .agent-name { color: var(--fg-subtle); }
.launcher-item .install-tag {
  flex-shrink: 0;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
.launcher-loading,
.launcher-empty {
  padding: var(--space-2) var(--space-3);
  font-size: var(--text-sm);
  color: var(--fg-subtle);
}
.palette-name { flex: 1; min-width: 0; }
.palette-shortcut { flex-shrink: 0; }
```

Append `@import './launcher-item.css';` to `src/theme/components/index.css`.

- [x] **Step 4: Run**

Run: `npm run typecheck && npx biome ci . && npx playwright test test/e2e/launcher-search.spec.ts test/e2e/launcher-stacking.spec.ts test/e2e/ux-polish.spec.ts && npx vitest run test/dom/launcher.test.ts`
Expected: green. `launcher-search.spec.ts:52-78` drives digit selection — the digit still works, but the visible hint is now `[1]` inside a `<kbd>` rather than a bare `1`. If that spec asserts the row's exact text (`toContainText('1')` still passes; `toHaveText('1 Claude')` would not), relax it to a `kbd` locator: `await expect(launcher.locator('.launcher-item').first().locator('kbd')).toHaveText('[1]')`.

- [x] **Step 5: Commit**

```bash
git add src/app/modals/launcher.ts src/app/modals/command-palette.ts src/style.css \
  src/theme/components/launcher-item.css src/theme/components/index.css test/e2e
git commit -m "feat(ui): rebuild launcher and command-palette rows on kbd and tokens"
```

---

### Task 7: Empty state, phase checklist, boot card

`patterns.md` › Empty and loading states:
- **No projects:** centred empty state — title `--text-xl`, one-line hint, one primary `button`. No illustration.
- **Session starting:** the phase checklist restyled — steps use `icon(check)` / `state-starting` / `--fg-subtle` dot; **no Unicode**. Today they are `content: '✓'` / `'◐'` / `'·'` in CSS (`style.css:1611-1627`).
- The boot card shares the phase spinner and is the same family of surface.

`components.md` › "What is *not* a primitive" says these keep bespoke markup but must use tokens and `icon()`.

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/view.ts` (`renderEmptyState`, `view.ts:657-745`)
- Modify: `cmd/hivegui/frontend/src/app/session-term.ts` (`_showPhaseOverlay`, `session-term.ts:1506-1533`)
- Modify: `cmd/hivegui/frontend/src/app/dom.ts` (`setBootState` — the retry button becomes a `button()`)
- Modify: `cmd/hivegui/frontend/index.html` (`#boot-state` card)
- Create: `cmd/hivegui/frontend/src/theme/components/empty-state.css`
- Modify: `cmd/hivegui/frontend/src/style.css` (delete `#empty-state*`, `#boot-state*`, `.boot-state-card*`, `.phase-*` rules — `style.css:913-999`, `:1557-1627`), `src/theme/components/index.css`
- Test: `cmd/hivegui/frontend/test/dom/boot-state.test.ts`, `test/dom/session-phase.test.ts`, `test/e2e/boot-overlay.spec.ts`, `test/e2e/ux-polish.spec.ts`

- [x] **Step 1: Empty state actions become `button()`s**

In `renderEmptyState`, replace the hand-built `<button>` loop. The first action is the primary one (`patterns.md`: "one primary `button` ('New project ⌘N')"), the rest default. The label already carries the key hint from `lib/empty-state.ts` — check whether it embeds the chord in the label string; if it does, leave it (it is the AGENTS.md inline hint) and do not double it with a `kbd()`.

```ts
    if (model.actions.length) {
      const row = document.createElement('div');
      row.className = 'empty-actions';
      model.actions.forEach((a, i) => {
        row.appendChild(
          button({
            label: a.label,
            kind: i === 0 ? 'primary' : 'default',
            icon: 'plus',
            onClick: (e) => {
              // The launcher opens synchronously; without this the same
              // click bubbles to the document-level outside-click closer
              // and shuts it in the same tick.
              e.stopPropagation();
              if (a.id === 'new-session') openLauncher();
              else if (a.id === 'new-project') openProjectEditor(null);
            },
          }),
        );
      });
      el.appendChild(row);
    }
```

- [x] **Step 2: Phase steps get real icons**

In `_showPhaseOverlay`, replace the `li.className = \`phase-step ${step.state}\`` mapping:

```ts
    this.phaseSteps.replaceChildren(
      ...panel.steps.map((step) => {
        const li = document.createElement('li');
        li.className = 'phase-step';
        li.dataset.state = step.state;
        // patterns.md: done → icon(check); active → the starting state
        // icon (dotted ring, rotates); todo → a token-coloured dot drawn
        // in CSS, never a Unicode '·'.
        if (step.state === 'done') li.append(icon('check', { size: 12 }));
        else if (step.state === 'active') li.append(stateIcon('starting'));
        const label = document.createElement('span');
        label.textContent = step.label;
        li.append(label);
        return li;
      }),
    );
```

`stateIcon('starting')` must be the same call the sidebar row makes — check its signature in Phase 2's `src/ui/stateIcon.ts` and pass whatever the five-state union names ("starting" per `icons.md`). Import `icon` and `stateIcon` in `session-term.ts`.

The `.phase-spinner` element above the checklist stays a CSS ring (it is not a state icon; it is the panel's own loading indicator) but its colours come from tokens.

- [x] **Step 3: Boot retry button**

`index.html`'s `#boot-state` card keeps its spinner and text span; drop the hand-written `<button id="boot-state-retry">`:

```html
  <div id="boot-state" role="status" aria-live="polite">
    <div class="boot-state-card">
      <span class="phase-spinner" aria-hidden="true"></span>
      <span id="boot-state-text">Starting hive…</span>
    </div>
  </div>
```

and build it in `dom.ts` where `setBootState` lives, keeping the id so `boot-overlay.spec.ts:47` and `boot-state.test.ts` keep their selector:

```ts
// The retry is a real button primitive, created once and parked in the
// card. It is hidden until the boot has given up (dom.ts owns that edge).
const bootRetry = button({
  label: 'Retry',
  kind: 'primary',
  icon: 'rotate',
  onClick: () => retryBoot(),
});
bootRetry.id = 'boot-state-retry';
bootRetry.hidden = true;
document.querySelector('.boot-state-card')?.append(bootRetry);
```

Wire `retryBoot` to whatever handler `main.ts` currently attaches to `#boot-state-retry` — grep for `boot-state-retry` and move that listener here rather than leaving a `getElementById` that now runs before the element exists. The existing `.hidden` class toggles on the retry become `bootRetry.hidden = …`; update `boot-state.test.ts`'s assertion the same way.

- [x] **Step 4: `empty-state.css`**

```css
/* Empty state, boot card and phase overlay. Bespoke surfaces
   (components.md › "What is not a primitive") — tokens and icon() only. */
#empty-state {
  grid-row: 3;
  grid-column: 2;
  z-index: 5;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  color: var(--fg-muted);
  pointer-events: auto;
}
#empty-state.hidden { display: none; }
#empty-state .empty-title {
  font-family: var(--font-ui);
  font-size: var(--text-xl);
  color: var(--fg);
}
#empty-state .empty-hint {
  font-size: var(--text-sm);
  color: var(--fg-subtle);
  max-width: 60ch;
  text-align: center;
}
#empty-state .empty-actions {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-2);
}

#boot-state {
  grid-row: 3;
  grid-column: 2;
  z-index: 6;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--space-6) var(--space-4);
  background: var(--bg);
}
#boot-state.hidden { display: none; }
.boot-state-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  max-width: 40ch;
  text-align: center;
  font-family: var(--font-ui);
  font-size: var(--text-sm);
  color: var(--fg-muted);
  line-height: 1.4;
}
.boot-state-card .phase-spinner { --session-color: var(--accent); }
.boot-state-card .phase-spinner.hidden { display: none; }
#boot-state-retry[hidden] { display: none; }

.phase-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg);
  z-index: 6;
  opacity: 1;
  transition: opacity var(--motion-fast) ease-out;
}
.phase-overlay[hidden] { display: none; }
.phase-overlay.fading { opacity: 0; }
.phase-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-5) var(--space-6);
  max-width: 80%;
  text-align: center;
}
.phase-spinner {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: 2px solid color-mix(in srgb, var(--session-color, var(--fg-subtle)) 35%, transparent);
  border-top-color: var(--session-color, var(--fg-subtle));
  animation: phase-spin 700ms linear infinite;
}
@keyframes phase-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) {
  .phase-spinner { animation: none; opacity: 0.6; }
}
.phase-status {
  font-family: var(--font-ui);
  font-size: var(--text-md);
  color: var(--fg);
}
.phase-steps {
  list-style: none;
  margin: 0;
  padding: 0;
  text-align: left;
  font-size: var(--text-sm);
  color: var(--fg-subtle);
}
.phase-step {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) 0;
}
/* 'todo' has no icon: a 4px token dot in the icon's slot keeps the
   labels aligned without a Unicode bullet. */
.phase-step[data-state='todo']::before {
  content: '';
  width: 12px;
  height: 12px;
  flex-shrink: 0;
  background: radial-gradient(circle, var(--fg-subtle) 0 2px, transparent 2px);
}
.phase-step[data-state='done'] { color: var(--fg-muted); }
.phase-step[data-state='done'] .hv-icon { color: var(--state-running); }
.phase-step[data-state='active'] { color: var(--fg); }
```

Delete the corresponding blocks from `style.css` (including `@keyframes phase-spin`, `phase-pulse`, and the `#empty-state .empty-actions button*` rules — `button.css` owns those now). Append `@import './empty-state.css';` to `src/theme/components/index.css`.

- [x] **Step 5: Run**

Run: `npx vitest run test/dom/boot-state.test.ts test/dom/session-phase.test.ts && npm run typecheck && npx biome ci . && npx playwright test test/e2e/boot-overlay.spec.ts test/e2e/ux-polish.spec.ts`
Expected: green. `boot-overlay.spec.ts:20-27` uses `elementFromPoint` to prove the overlay actually covers the pane — it must still pass, which is the check that `--bg` on `#boot-state` did not become transparent.

- [x] **Step 6: Commit**

```bash
git add src/app/view.ts src/app/session-term.ts src/app/dom.ts index.html src/style.css \
  src/theme/components/empty-state.css src/theme/components/index.css test/dom test/e2e
git commit -m "feat(ui): restyle empty state, phase checklist and boot card on tokens and icons"
```

---

### Task 8: Screenshot baselines for grid view and launcher, both presets

Phase 1's `theme.spec.ts` proved the token migration moved no pixel. This phase moves pixels on purpose, so the baseline shifts from "identical to v2.4.0" to "this is what the chrome looks like now, in both shipped presets". Same `HIVE_SNAPSHOT=1` gate as Phase 1 — these are darwin-local baselines, not a CI gate (`theme.spec.ts:5-12`).

**Files:**
- Create: `cmd/hivegui/frontend/test/e2e/chrome.spec.ts`
- Create (generated): `cmd/hivegui/frontend/test/e2e/chrome.spec.ts-snapshots/*.png`

**Interfaces:**
- Produces snapshots: `grid-hive-dark.png`, `grid-hive-light.png`, `launcher-hive-dark.png`, `launcher-hive-light.png`.

- [x] **Step 1: Write the spec**

```ts
import { test, expect, type Page } from '@playwright/test';

// Phase-4 baselines: the chrome (banners, status bar, tile headers,
// launcher rows) under both shipped presets. Same HIVE_SNAPSHOT gate as
// theme.spec.ts — Playwright suffixes baselines per-platform and CI runs
// three OSes, so these are a local review artefact, not a CI gate.
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function bootWith(page: Page, theme: string) {
  await page.addInitScript(
    (t) => localStorage.setItem('hive.theme', t),
    theme,
  );
  await page.setViewportSize({ width: 1100, height: 700 });
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

test.describe('phase-4 chrome baselines', () => {
  test.skip(
    !process.env.HIVE_SNAPSHOT,
    'pixel baselines are darwin-local; run with HIVE_SNAPSHOT=1',
  );

  for (const theme of ['hive-dark', 'hive-light']) {
    test(`grid view — ${theme}`, async ({ page }) => {
      await bootWith(page, theme);
      // Three sessions so the grid tiles and every tile header renders.
      await page.evaluate((n) => window.__hive.addSession?.(n), 's2');
      await page.evaluate((n) => window.__hive.addSession?.(n), 's3');
      await page.waitForFunction(
        (n) => (window.__hive.state?.sessions.length ?? 0) >= n,
        3,
      );
      await page.keyboard.press(`${mod}+Shift+g`);
      await expect(page.locator('#terms')).toHaveClass(/grid/);
      // Attention on the non-active session so the state icon's
      // attention shape is in the frame.
      await page.evaluate(() =>
        window.__hive.emit('pty:data', 's1', btoa('\x07')),
      );
      await expect(page.locator('#projects li[data-sid="s1"]')).toHaveClass(
        /attention/,
      );
      await expect(page.locator('#status-hint')).toContainText('focus');
      await expect(page).toHaveScreenshot(`grid-${theme}.png`, {
        maxDiffPixels: 0,
        animations: 'disabled',
        mask: [page.locator('.xterm')],
      });
    });

    test(`launcher — ${theme}`, async ({ page }) => {
      await bootWith(page, theme);
      await page.keyboard.press(`${mod}+t`); // check keymap.ts for the real binding
      await expect(page.locator('#launcher')).not.toHaveClass(/hidden/);
      await expect(
        page.locator('#launcher .launcher-item').first(),
      ).toBeVisible();
      await expect(page).toHaveScreenshot(`launcher-${theme}.png`, {
        maxDiffPixels: 0,
        animations: 'disabled',
      });
    });
  }
});
```

Confirm the launcher's opening chord against `src/app/keymap.ts` before generating baselines; `⌘T` is a guess. If no chord opens it directly, click the project card's `plus` action instead — `launcher-search.spec.ts` already has that helper, reuse it rather than inventing a second one.

- [x] **Step 2: Generate**

Run: `HIVE_SNAPSHOT=1 npx playwright test test/e2e/chrome.spec.ts --update-snapshots`
Expected: four PNGs under `test/e2e/chrome.spec.ts-snapshots/`.

- [x] **Step 3: Look at them.** This is the phase's design review: open all four. Check against `components.md` — 24px status bar, 28px tile headers, 32px rows, `--sel` + accent bar on the selected launcher row, hints as `kbd`, no glyph anywhere. `hive-light` is where contrast bugs surface: `--fg-subtle` on `--surface-raised` is the pair most likely to be unreadable. Fix in the component CSS (never with a literal), regenerate, and note anything that needed a token value change for Phase 6's contrast check.

- [x] **Step 4: Confirm stable**

Run: `HIVE_SNAPSHOT=1 npx playwright test test/e2e/chrome.spec.ts`
Expected: 4 passed.

- [x] **Step 5: Phase 1's baselines are now stale**

`theme.spec.ts`'s `sidebar-classic.png` / `settings-classic.png` assert the `classic` preset, which this phase's markup changes (the status bar moved, the banners are gone from the markup, the tile header is rebuilt). `classic` is a preset of *token values*, not of markup — it cannot reproduce v2.4.0 pixels once the DOM changes. Regenerate them and update the describe block's comment to say what it now guards (preset switching, not v2.4.0 equality):

Run: `HIVE_SNAPSHOT=1 npx playwright test test/e2e/theme.spec.ts --update-snapshots`

Record this in the decision log (Task 9) — it retires the Phase 1 "pixel-identical" guarantee on purpose, and a later reader should not think the baselines rotted by accident.

- [x] **Step 6: Commit**

```bash
git add test/e2e/chrome.spec.ts test/e2e/chrome.spec.ts-snapshots test/e2e/theme.spec.ts test/e2e/theme.spec.ts-snapshots
git commit -m "test(ui): baseline grid and launcher chrome under hive-dark and hive-light"
```

---

### Task 9: Glyph sweep, `ui-lint --strict`, docs and bookkeeping

**Files:**
- Modify: whatever `./scripts/ui-lint.sh --strict` still reports outside the dialogs
- Modify: `.github/workflows/ci.yml` (only if the ui-lint step is still in warn mode)
- Modify: `docs/design-docs/ui/README.md` (Status line)
- Modify: `docs/exec-plans/active/ui-design-system.md` (Progress, decision log)
- Create: `.changesets/<pr>-ui-chrome.md`

- [x] **Step 1: Run the lint and clear what this phase owns**

Run: `./scripts/ui-lint.sh --strict; echo exit=$?`

Everything it reports in `src/app/banners.ts`, `dom.ts`, `view.ts`, `session-term.ts`, `modals/launcher.ts`, `modals/command-palette.ts` and `index.html` outside the dialog markup is this phase's. The expected remainder is the dialog `×` close buttons (`#worktrees-close`, `#settings-close`, `#help-overlay-close`, `#project-editor` actions) and the sidebar header `＋` if Phase 3 left it — Phase 5 and Phase 3's scope respectively. If `#new-project-btn`'s `＋` is still there, convert it here rather than leaving a violation:

```ts
// index.html: <button id="new-project-btn"> becomes an empty mount, or
// the sidebar header builds it. Whichever Phase 3 chose, the glyph goes:
const newProject = iconButton({
  icon: 'plus',
  label: 'New project',
  onClick: () => openProjectEditor(null),
});
newProject.id = 'new-project-btn';
newProject.title = `New project (${isMac ? '⌘N' : 'Ctrl+N'})`;
```

Do **not** silence a real violation with `/* ui-lint: allow */`; an allow comment in this phase means a token or an icon is missing, which is a spec change (`README.md` › How to change the UI).

- [x] **Step 2: If CI still runs ui-lint in warn mode, flip it**

```yaml
      - name: UI lint (tokens / icons)
        if: matrix.biome
        run: ./scripts/ui-lint.sh --strict
```

Only if Phase 2 didn't already. Check first.

- [x] **Step 3: Full local gate**

Run:
```bash
cd cmd/hivegui/frontend \
  && npx biome ci . && npm run typecheck && npx vitest run && npx playwright test \
  && cd ../../.. && ./scripts/ui-lint.sh --strict && go build ./...
```
Expected: all green, `ui-lint: 0 violation(s)` for the non-dialog tree.

- [x] **Step 4: Docs**

`docs/design-docs/ui/README.md` Status → "Phases 1–4 implemented; dialogs and theming UI are Phase 5". `docs/exec-plans/active/ui-design-system.md`: tick Phase 4 in Progress, and add decision-log entries for (a) retiring the Phase 1 pixel-identity baseline (Task 8 step 5), (b) the tile header dropping the project-colour gradient — one channel per fact, and (c) the launcher's agent swatch standing in for the "leading icon for agent kind" slot, because per-agent sprite symbols are not something `icons.md` allows.

Changeset via `/hs-changelog-update` — user-visible text: "Reskinned chrome: notice banners, a real status bar with inline mode shortcuts, grid tile headers with state icons, and consistent launcher and command-palette rows."

- [x] **Step 5: Commit and open PR**

```bash
git add docs .changesets .github cmd/hivegui/frontend
git commit -m "docs(ui): mark phase 4 of the design system implemented"
```

PR title: `feat(ui): design-system phase 4 — banners, status bar, tile headers, launcher rows`. Body: link the spec, paste the four Task 8 screenshots, paste the `ui-lint --strict` result and name what remains (dialogs, Phase 5).

---

## Self-review

- **Spec coverage:** `components.md` › button → Task 1; banner → Tasks 2–3; statusBar → Task 4; Grid tile header → Task 5; launcherItem / palette rows → Task 6; "What is not a primitive" (empty state, boot/phase panel) → Task 7. `patterns.md` › empty and loading states → Task 7; errors (status bar, 6s, no toasts) → Task 4 (the 6s dwell already lives in `createStatus`); keyboard hints (`kbd` everywhere, right slot, `[n]`/`(n)` format) → Tasks 4, 6; hover-revealed actions → Task 5; motion (pulse and spinner only, the status error animation deleted) → Tasks 4, 7. `icons.md` no-Unicode → Tasks 5, 6, 7, 9. Not in scope and not gaps: `dialog`, form fields, Settings › Appearance (Phase 5); `sessionRow`, `projectCard`, `chip`, sidebar (Phase 3); fonts, `native`/`terminal` presets, `style.css` split, contrast check (Phase 6).
- **Placeholders:** none. Three steps are deliberately conditional on what Phases 2–3 shipped (the CSS aggregation mechanism in Task 1, `iconButton`'s `onClick` signature in Task 5, `#new-project-btn` in Task 9) — each says what to check and what to do either way.
- **Type consistency:** `ButtonKind`/`ButtonOpts`/`button` used identically in Tasks 1, 2, 7, 9. `Banner`/`BannerAction`/`banner` in Tasks 2, 3. `ModeHint`/`modeHints`/`setModeHint` in Task 4 only, produced in `lib/status.ts` and consumed in `app/dom.ts` and `app/view.ts`. `resolveSessionState`/`updateStateIcon` in Task 5 only.
- **Known risk 1 — status bar text assertions.** `#status` grew a second text node, so every `toHaveText('#status')` in the suite breaks. Task 4 step 8 fixes the seven in `silent-failures.spec.ts`; grep the whole `test/` tree for `'#status'` before running the full suite, there may be more in specs not named in this plan.
- **Known risk 2 — a fifth grid row.** Moving `#status` from `position: fixed` into the `#app` grid takes 24px off the terminal area's height. Every tile refits (each `SessionTerm` has its own `ResizeObserver`), so nothing breaks, but `grid-scroll-regressions.spec.ts` and `scrollback-invariants.spec.ts` assert on viewport-relative scroll maths — run both in Task 4 even though they are not listed in its command.
- **Known risk 3 — `banners.ts` mounting into `#app`.** `mountBanners()` needs `#app` to exist. It does in `index.html`, but jsdom scaffolds that import `banners.ts` transitively (via `view.ts`/`keyboard.ts`) and then call `initBanners()` would silently drop the banners on the floor (`app?.prepend` is optional-chained). That is the correct behaviour for a test that never opens a banner; it is a bug for a test that does. Only `restart-hive.test.ts` does, and Task 3 step 5 gives it an `#app`.
- **Deliberate scope call:** the tile header loses the project→session colour gradient. It is the biggest visual change in the phase and the one most likely to draw a "but I liked that" — it is in the plan because `README.md` principle 2 says project identity has one home (the sidebar card) and the tile already encodes session identity twice (colour dot + name). If review disagrees, the cheap reversal is one `background:` rule in `tile-header.css`, not a re-plan.

## Progress

**2026-08-30** — All nine tasks implemented on `feature/ui-design-system-phase4`
(9 commits). Deviations from the plan as written, each verified against the
tree rather than the plan's pre-phase-1 line references:

- Components are linked individually from `index.html` (what phase 3 shipped),
  so there is no `src/theme/components/index.css` to append `@import`s to.
- The update banner needed a second action (`action`: Update / Updating… /
  Restart) that the plan's `mountBanners()` sketch omitted; `renderUpdateAction`
  drives it through `banner.action('action')`, and `banner()` stamps
  `data-action-id` so tests can address one action without child order.
- `modeHints` uses the chords `lib/shortcuts.ts` actually binds — `⌘G`,
  `⇧⌘K`, `⌘`+arrows — not the plan's `⌘K` / `⌥`+arrows.
- Phases 2–3 had already landed the tile's `stateIcon`, `refreshStateIcon` and
  the phase-checklist icons; task 5 and task 7 were correspondingly smaller
  (worktree glyph → `iconButton` in a hover-revealed `.tile-actions`, variant
  classes → data attributes, empty-state and boot-retry buttons → `button()`).
- The tile's window-title span is now hidden at construction: its `::before`
  separator rendered a lone `·` beside the session name until the first title
  arrived (visible in the first round of task-8 baselines).
- The tile's minimize button is **not** hover-revealed. `display: none`
  until hover leaves it unreachable by keyboard — the trap `session-row.css`
  already rejected — so `.tile-actions` renders at rest beside the project
  label. Review finding, user's call.

**Review round 1 (PR #301)** cleared three defects, two of them shipped by
this phase and invisible to the gate as originally specified:

- `style.css` — a dangling selector list dropped focus rings from five
  controls *and* broke `vite build`. The plan's per-task gate had no
  `npm run build`, so only CI caught it; it is in the gate below now.
- `button.css` / `icon-button.css` — no `[hidden]` rule, so the author-origin
  `display` outranked the UA's, making every `el.hidden = true` on a button a
  silent no-op (the update banner's Download/Update toggles among them). The
  jsdom tests could not see it: `el.hidden` read back `true` throughout.
  `test/e2e/banner-visibility.spec.ts` is the browser-level guard, verified
  to fail when the rule is removed.
- `modeHints` advertised `⌘G` as "focus" in `grid-all`, but `keyboard.ts`
  sends plain `⌘G` to `grid-project` there; `⇧⌘G` is what returns to single.

Also from that round: `refreshStateIcon()` now resolves from the tile's live
`this.phase` rather than `this.info.phase` (setPhase never writes back to
info, so the refresh it triggers was recomputing from a stale payload), the
`aria-live` region narrowed to `#status-text` so mode hints stop re-announcing
on every navigation, `modeHints` takes `ViewMode` instead of `string`, and the
rewritten banner dismissal paths (per-version, per-daemon-build) got coverage.

Gate: `biome ci`, `tsc --noEmit`, `vite build`, 694 vitest tests, 208
Playwright tests, `ui-lint --strict` (0 violations) and `go build ./...` all
green.

## PR convergence ledger

- **2026-08-30 iter 1** — verdict: REQUEST_CHANGES; mergeable: MERGEABLE; findings_hash: 5241381b689bb7ae; threads_open: 0; action: autofix+push, then escalated:risky fix needs human decision; head_sha: 286e448.

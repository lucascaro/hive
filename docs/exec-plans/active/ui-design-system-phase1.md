# UI design system — Phase 1: tokens, presets, lint (visually no-op)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce the token layer and preset mechanism under the existing UI without changing a single rendered pixel, and add the CI lint that keeps literals out from now on.

**Architecture:** `src/theme/tokens.css` declares every semantic token on `:root` with `hive-dark` values; `src/theme/themes.css` re-values them per `[data-theme]`, including a `classic` preset that reproduces today's `style.css` literals exactly. `theme.ts` stamps `data-theme` from `localStorage['hive.theme']` (default `classic` for this phase) before first paint and feeds xterm a theme object built from computed tokens. `style.css` literals are rewritten to `var(--…)` by a mapping table; a Playwright screenshot test proves before/after equality. `scripts/ui-lint.sh` greps for literals and runs in CI in warn mode.

**Tech Stack:** Vanilla TS, Vite 8, vitest 4, Playwright 1.62 (`test/e2e` + `wails-mock.ts`), bash + grep for lint. No new dependencies.

**Spec:** `docs/design-docs/ui/tokens.md`, `themes.md` (presets, xterm mapping), `README.md` (principle 3, "values come from tokens").

## Global Constraints

- Zero visual change in this phase: the `classic` preset must reproduce v2.4.0 pixel-for-pixel (screenshot test, Task 5).
- Token names exactly as in `tokens.md` (`--bg`, `--surface`, `--surface-raised`, `--border`, `--fg`, `--fg-muted`, `--fg-subtle`, `--accent`, `--on-accent`, `--sel`, `--hover`, `--btn`, `--btn-border`, `--state-running`, `--state-attention`, `--state-starting`, `--state-exited`, `--state-error`, `--term-bg`, `--term-fg`, `--font-ui`, `--font-mono`, `--text-xs..xl`, `--space-1..6`, `--radius-sm/md`, `--shadow-popover`, `--motion-fast`, `--motion-pulse`).
- No new npm dependencies. Fonts are **not** bundled in this phase (`hive-dark` falls back to the system stack until Phase 6).
- `biome ci .` must stay green (it ignores CSS/HTML; TS files added here are checked).
- Commits: conventional (`feat(theme): …`, `chore(ci): …`), each task one commit.
- Run every frontend command from `cmd/hivegui/frontend/`. Fresh worktree → `./scripts/ci-bootstrap.sh` first or `npm run typecheck` fails on missing `wailsjs/`.

---

### Task 1: Screenshot baseline of the current UI

Capture "before" so Task 5 can prove equality. Done first, before any CSS changes.

**Files:**
- Create: `cmd/hivegui/frontend/test/e2e/theme.spec.ts`
- Create (generated): `cmd/hivegui/frontend/test/e2e/theme.spec.ts-snapshots/*.png`

**Interfaces:**
- Produces: snapshot names `sidebar-classic.png`, `settings-classic.png` used by Task 5.

- [ ] **Step 1: Write the screenshot test**

Look at `test/e2e/minimize.spec.ts` for how sessions are injected through `window.__hive` and copy the same seeding so the sidebar has two projects, three sessions, one with attention, one minimized. Then:

```ts
import { test, expect } from '@playwright/test';

// Phase-1 guard: the token migration must not move a pixel. Baselines are
// captured on the pre-migration tree (Task 1) and asserted after (Task 5).
test.describe('classic preset is pixel-identical to v2.4.0', () => {
  test('sidebar + terminal', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 700 });
    await page.goto('/');
    // seed: same helper calls as minimize.spec.ts
    await expect(page.locator('#projects .project')).toHaveCount(2);
    await expect(page).toHaveScreenshot('sidebar-classic.png', {
      maxDiffPixels: 0,
      animations: 'disabled',
      mask: [page.locator('.xterm')], // terminal content is not under test
    });
  });

  test('settings dialog', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 700 });
    await page.goto('/');
    await page.keyboard.press('Meta+,'); // check keymap.ts for the real binding
    await expect(page.locator('#settings')).toBeVisible();
    await expect(page).toHaveScreenshot('settings-classic.png', {
      maxDiffPixels: 0,
      animations: 'disabled',
    });
  });
});
```

- [ ] **Step 2: Generate baselines**

Run: `npx playwright test test/e2e/theme.spec.ts --update-snapshots`
Expected: two PNGs written under `test/e2e/theme.spec.ts-snapshots/`.

- [ ] **Step 3: Confirm they pass unchanged**

Run: `npx playwright test test/e2e/theme.spec.ts`
Expected: 2 passed.

- [ ] **Step 4: Commit**

```bash
git add test/e2e/theme.spec.ts test/e2e/theme.spec.ts-snapshots
git commit -m "test(theme): capture pre-migration screenshot baselines"
```

---

### Task 2: `tokens.css` + `themes.css` with `classic` and `hive-dark`

**Files:**
- Create: `cmd/hivegui/frontend/src/theme/tokens.css`
- Create: `cmd/hivegui/frontend/src/theme/themes.css`
- Modify: `cmd/hivegui/frontend/index.html:7` (add two `<link>`s before `style.css`)

**Interfaces:**
- Produces: every token in Global Constraints, defined on `:root` and per `[data-theme]`.

- [ ] **Step 1: Write `tokens.css`** (values = `hive-dark` from `tokens.md`)

```css
/* Semantic tokens. Defaults are the hive-dark preset; themes.css re-values
   them per [data-theme]. Components read ONLY these — no literals.
   See docs/design-docs/ui/tokens.md. */
:root {
  --bg: #0f1014;
  --surface: #13141a;
  --surface-raised: #0f1015;
  --border: #22242e;
  --fg: #e6e7ee;
  --fg-muted: #a5a8b8;
  --fg-subtle: #5f6273;
  --accent: #ffb454;
  --on-accent: #15120a;
  --sel: #1c1e28;
  --hover: color-mix(in srgb, var(--sel) 60%, transparent);
  --btn: #1a1c25;
  --btn-border: #2b2e3b;
  --state-running: #5fd7a5;
  --state-attention: #ff9f43;
  --state-starting: var(--fg-subtle);
  --state-exited: var(--fg-subtle);
  --state-error: #ff6b6b;
  --term-bg: #0b0c10;
  --term-fg: #dfe1ea;

  --font-ui: "IBM Plex Sans", -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
  --font-mono: "JetBrains Mono", "SF Mono", Menlo, Consolas, monospace;
  --text-xs: 11px;
  --text-sm: 12px;
  --text-md: 13px;
  --text-lg: 14px;
  --text-xl: 16px;

  --space-1: 4px; --space-2: 8px; --space-3: 12px;
  --space-4: 16px; --space-5: 20px; --space-6: 24px;
  --radius-sm: 4px;
  --radius-md: 6px;
  --shadow-popover: 0 8px 24px rgba(0, 0, 0, 0.45);
  --motion-fast: 120ms;
  --motion-pulse: 1.6s;
}
@media (prefers-reduced-motion: reduce) {
  :root { --motion-fast: 0s; --motion-pulse: 0s; }
}
```

- [ ] **Step 2: Write `themes.css`** — `classic` maps 1:1 to today's literals. The mapping table (derived from `style.css` frequency: `#888`×62, `#2a2a2a`×34, `#f59e0b`×26, `#ddd`×24, `#fff`×20, `#1f1f1f`×16, `#000`×14, `#ccc`×10 …):

```css
/* Presets. Every block re-values EVERY token; partial presets fall through
   to hive-dark and look broken in light. docs/design-docs/ui/themes.md */

:root[data-theme="classic"] {
  --bg: #000;
  --surface: #0a0a0a;
  --surface-raised: #111;
  --border: #1f1f1f;
  --fg: #ddd;
  --fg-muted: #888;
  --fg-subtle: #666;
  --accent: #f59e0b;
  --on-accent: #000;
  --sel: #1a1a1a;
  --hover: #2a2a2a;
  --btn: #1f1f1f;
  --btn-border: transparent;
  --state-running: #22c55e;
  --state-attention: #f59e0b;
  --state-starting: #666;
  --state-exited: #555;
  --state-error: #ff9a9a;
  --term-bg: #000;
  --term-fg: #ddd;
  --font-ui: -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
  --font-mono: Menlo, Consolas, monospace;
  --radius-sm: 4px;
  --radius-md: 6px;
}

:root[data-theme="hive-dark"] { /* identical to tokens.css defaults; present so the picker has a block */ }

:root[data-theme="hive-light"] {
  --bg: #f4f4f7; --surface: #ffffff; --surface-raised: #f8f8fb; --border: #e2e3ea;
  --fg: #1a1b22; --fg-muted: #4f5262; --fg-subtle: #8a8d9c;
  --accent: #c47a12; --on-accent: #ffffff; --sel: #eceef5;
  --hover: color-mix(in srgb, var(--sel) 60%, transparent);
  --btn: #f2f3f7; --btn-border: #d9dbe3;
  --state-running: #1f9d6a; --state-attention: #d9731a; --state-error: #d64545;
  --term-bg: #ffffff; --term-fg: #1a1b22;
  --shadow-popover: 0 8px 24px rgba(20, 20, 40, 0.15);
}
```

(`native-dark`, `native-light`, `terminal` blocks are Phase 6; exact values in `themes.md`/mocks. Do not add them here.)

- [ ] **Step 3: Link them in `index.html`** before `style.css` so `style.css` can reference tokens and later overrides win by order:

```html
<link rel="stylesheet" href="./src/theme/tokens.css"/>
<link rel="stylesheet" href="./src/theme/themes.css"/>
<link rel="stylesheet" href="./src/style.css"/>
```

- [ ] **Step 4: Verify build**

Run: `npm run build`
Expected: succeeds; `dist/assets/*.css` contains `--accent`.

- [ ] **Step 5: Commit**

```bash
git add src/theme/tokens.css src/theme/themes.css index.html
git commit -m "feat(theme): add semantic tokens and classic/hive-dark/hive-light presets"
```

---

### Task 3: `theme.ts` — stamp preset before first paint

**Files:**
- Create: `cmd/hivegui/frontend/src/theme/theme.ts`
- Test: `cmd/hivegui/frontend/test/unit/theme.test.ts`
- Modify: `cmd/hivegui/frontend/src/main.ts` (first import)

**Interfaces:**
- Produces:
  ```ts
  export type ThemeName = 'classic' | 'hive-dark' | 'hive-light' | 'system';
  export const THEME_KEY = 'hive.theme';
  export const DEFAULT_THEME: ThemeName = 'classic';   // flips to 'system' in Phase 6
  export function resolveTheme(stored: string | null, prefersDark: boolean): Exclude<ThemeName,'system'>;
  export function applyTheme(name: ThemeName, doc?: Document): void;  // sets data-theme
  export function readTheme(storage?: Storage): ThemeName;
  ```

- [ ] **Step 1: Write the failing tests**

```ts
import { describe, it, expect } from 'vitest';
import { resolveTheme, DEFAULT_THEME } from '../../src/theme/theme';

describe('resolveTheme', () => {
  it('defaults to classic when nothing is stored', () => {
    expect(resolveTheme(null, true)).toBe(DEFAULT_THEME);
  });
  it('maps system to hive-dark / hive-light by OS preference', () => {
    expect(resolveTheme('system', true)).toBe('hive-dark');
    expect(resolveTheme('system', false)).toBe('hive-light');
  });
  it('passes known presets through and rejects garbage', () => {
    expect(resolveTheme('hive-light', true)).toBe('hive-light');
    expect(resolveTheme('<script>', true)).toBe(DEFAULT_THEME);
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run test/unit/theme.test.ts`
Expected: FAIL — cannot resolve `../../src/theme/theme`.

- [ ] **Step 3: Implement**

```ts
// Preset selection. Runs before first paint (imported first in main.ts) so
// the app never flashes the default preset. docs/design-docs/ui/themes.md
export type ThemeName = 'classic' | 'hive-dark' | 'hive-light' | 'system';
export const THEME_KEY = 'hive.theme';
export const DEFAULT_THEME: ThemeName = 'classic';
const PRESETS = new Set(['classic', 'hive-dark', 'hive-light']);

export function resolveTheme(
  stored: string | null,
  prefersDark: boolean,
): Exclude<ThemeName, 'system'> {
  if (stored === 'system') return prefersDark ? 'hive-dark' : 'hive-light';
  if (stored && PRESETS.has(stored)) return stored as Exclude<ThemeName, 'system'>;
  return DEFAULT_THEME === 'system'
    ? prefersDark ? 'hive-dark' : 'hive-light'
    : DEFAULT_THEME;
}

export function readTheme(storage: Storage = localStorage): ThemeName {
  try {
    const v = storage.getItem(THEME_KEY);
    return v === 'system' || PRESETS.has(v ?? '') ? (v as ThemeName) : DEFAULT_THEME;
  } catch {
    return DEFAULT_THEME;
  }
}

export function applyTheme(name: ThemeName, doc: Document = document): void {
  const prefersDark = doc.defaultView?.matchMedia?.('(prefers-color-scheme: dark)').matches ?? true;
  doc.documentElement.dataset.theme = resolveTheme(name, prefersDark);
}

// Side effect on import: stamp before anything renders.
if (typeof document !== 'undefined') applyTheme(readTheme());
```

- [ ] **Step 4: Import first in `main.ts`**

Add as the very first line: `import './theme/theme';`

- [ ] **Step 5: Run tests + typecheck**

Run: `npx vitest run test/unit/theme.test.ts && npm run typecheck && npx biome ci .`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add src/theme/theme.ts src/main.ts test/unit/theme.test.ts
git commit -m "feat(theme): resolve and stamp preset from localStorage before first paint"
```

---

### Task 4: Rewrite `style.css` literals to tokens

Mechanical. Use a script so the mapping is reviewable and rerunnable; commit the script under `scripts/` for the Phase 6 split.

**Files:**
- Create: `scripts/ui-tokenize.py`
- Modify: `cmd/hivegui/frontend/src/style.css` (whole file)

- [ ] **Step 1: Write the mapping script**

```python
#!/usr/bin/env python3
"""One-shot: replace colour/font-size literals in style.css with tokens.
Mapping is the classic preset (themes.css) inverted. Unmapped literals are
printed so they can be added to the map or left with a `/* ui-lint: allow */`."""
import re, sys, pathlib
p = pathlib.Path('cmd/hivegui/frontend/src/style.css')
s = p.read_text()
COLORS = {
  '#000': 'var(--bg)', '#000000': 'var(--bg)',
  '#0a0a0a': 'var(--surface)', '#111': 'var(--surface-raised)',
  '#1f1f1f': 'var(--border)', '#ddd': 'var(--fg)', '#888': 'var(--fg-muted)',
  '#666': 'var(--fg-subtle)', '#f59e0b': 'var(--accent)', '#1a1a1a': 'var(--sel)',
  '#2a2a2a': 'var(--hover)', '#22c55e': 'var(--state-running)',
  '#ff9a9a': 'var(--state-error)',
}
SIZES = {'11px': 'var(--text-xs)', '12px': 'var(--text-sm)', '13px': 'var(--text-md)',
         '14px': 'var(--text-lg)', '16px': 'var(--text-xl)'}
for k, v in COLORS.items():
    s = re.sub(rf'(?<![\w-]){re.escape(k)}(?![\w])', v, s, flags=re.I)
for k, v in SIZES.items():
    s = re.sub(rf'font-size:\s*{k}', f'font-size: {v}', s)
p.write_text(s)
left = sorted(set(re.findall(r'#[0-9a-fA-F]{3,8}\b', s)))
print('unmapped colours:', left)
print('unmapped font-sizes:', sorted(set(re.findall(r'font-size:\s*[\d.]+px', s))))
```

- [ ] **Step 2: Run it, then handle the remainder by hand**

Run: `python3 scripts/ui-tokenize.py`

For each printed leftover decide, in this order: (a) it's a shade of a token → `color-mix(in srgb, var(--token) N%, transparent)`; (b) it's a real design role missing from the spec (e.g. the `#fcd9a8`/`#fbb04a` attention-glow shades) → keep as `color-mix` on `--state-attention`; (c) genuinely one-off (the xterm-viewport hacks) → append `/* ui-lint: allow */` on the line. Off-scale font sizes (10, 10.5, 11.5, 12.5, 15, 18px) → nearest scale token **only if** the screenshot in Task 5 still passes; otherwise keep the literal with `/* ui-lint: allow */` and a `TODO(phase-6)` note — this phase is no-op, not cleanup.

- [ ] **Step 3: Extend `classic` for anything you added** in `themes.css` so `classic` still reproduces today's pixels.

- [ ] **Step 4: Build**

Run: `npm run build`
Expected: succeeds.

- [ ] **Step 5: Commit**

```bash
git add scripts/ui-tokenize.py cmd/hivegui/frontend/src/style.css cmd/hivegui/frontend/src/theme/themes.css
git commit -m "refactor(theme): replace style.css literals with tokens (classic preset, no visual change)"
```

---

### Task 5: Prove no visual change

**Files:**
- Test: `cmd/hivegui/frontend/test/e2e/theme.spec.ts` (from Task 1, unchanged)

- [ ] **Step 1: Run the screenshot test against the tokenised tree**

Run: `npx playwright test test/e2e/theme.spec.ts`
Expected: 2 passed with `maxDiffPixels: 0`.

- [ ] **Step 2: If it fails**, open `test-results/**/*-diff.png`, find the offending selector, and fix the mapping in `themes.css` (`classic` block) or revert that one literal with `/* ui-lint: allow */`. Do **not** update the snapshot. Repeat until green.

- [ ] **Step 3: Add a second assertion that presets actually switch**

Append to `theme.spec.ts`:

```ts
test('hive-light preset changes the sidebar ground', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('hive.theme', 'hive-light'));
  await page.goto('/');
  const bg = await page.locator('#sidebar').evaluate(
    (el) => getComputedStyle(el).backgroundColor,
  );
  expect(bg).toBe('rgb(255, 255, 255)');
});
```

Run: `npx playwright test test/e2e/theme.spec.ts` → 3 passed.

- [ ] **Step 4: Commit**

```bash
git add test/e2e/theme.spec.ts
git commit -m "test(theme): assert classic is pixel-identical and presets switch"
```

---

### Task 6: xterm theme from tokens

**Files:**
- Modify: `cmd/hivegui/frontend/src/app/session-term.ts:314` (`theme: { background: '#000000' }`)
- Modify: `cmd/hivegui/frontend/src/theme/theme.ts` (add `xtermTheme()`)
- Test: `cmd/hivegui/frontend/test/dom/xterm-theme.test.ts`

**Interfaces:**
- Produces: `export function xtermTheme(doc?: Document): { background: string; foreground: string; cursor: string; cursorAccent: string; selectionBackground: string }`

- [ ] **Step 1: Failing test** (jsdom; `test/dom` config already provides a document)

```ts
import { describe, it, expect } from 'vitest';
import { xtermTheme } from '../../src/theme/theme';

describe('xtermTheme', () => {
  it('reads --term-bg/--term-fg/--accent from the root element', () => {
    document.documentElement.style.setProperty('--term-bg', '#0b0c10');
    document.documentElement.style.setProperty('--term-fg', '#dfe1ea');
    document.documentElement.style.setProperty('--accent', '#ffb454');
    document.documentElement.style.setProperty('--on-accent', '#15120a');
    const t = xtermTheme(document);
    expect(t.background).toBe('#0b0c10');
    expect(t.foreground).toBe('#dfe1ea');
    expect(t.cursor).toBe('#ffb454');
    expect(t.cursorAccent).toBe('#15120a');
  });
});
```

- [ ] **Step 2: Run → FAIL** (`xtermTheme` not exported).

- [ ] **Step 3: Implement** in `theme.ts`:

```ts
export function xtermTheme(doc: Document = document) {
  const cs = getComputedStyle(doc.documentElement);
  const v = (n: string) => cs.getPropertyValue(n).trim();
  const accent = v('--accent');
  return {
    background: v('--term-bg'),
    foreground: v('--term-fg'),
    cursor: accent,
    cursorAccent: v('--on-accent'),
    // color-mix isn't resolvable via getPropertyValue; xterm accepts 8-digit hex.
    selectionBackground: accent.length === 7 ? `${accent}4d` : accent,
  };
}
```

- [ ] **Step 4: Use it** in `session-term.ts:314`: replace `theme: { background: '#000000' }` with `theme: xtermTheme()` (import from `'../theme/theme'`). Under `classic`, `--term-bg` is `#000` so the pixel result is unchanged; foreground/cursor now come from tokens — verify with Task 1's screenshot (terminal is masked, so also eyeball `wails dev` once).

- [ ] **Step 5: Run all**

Run: `npx vitest run && npx playwright test test/e2e/theme.spec.ts && npm run typecheck && npx biome ci .`
Expected: green.

- [ ] **Step 6: Commit**

```bash
git add src/theme/theme.ts src/app/session-term.ts test/dom/xterm-theme.test.ts
git commit -m "feat(theme): derive xterm theme from tokens"
```

---

### Task 7: `scripts/ui-lint.sh` + CI step (warn mode)

**Files:**
- Create: `scripts/ui-lint.sh`
- Create: `scripts/testdata/ui-lint/bad.css`, `scripts/testdata/ui-lint/good.css`
- Modify: `.github/workflows/ci.yml` (after the "Typecheck (frontend — tsc)" step, same `if: matrix.biome`)

**Interfaces:**
- Produces: `scripts/ui-lint.sh [--strict]` — exit 0 always unless `--strict`; prints one line per violation `path:line: <rule>: <snippet>`.

- [ ] **Step 1: Fixtures**

`bad.css`: `.a { color: #fff; font-size: 12.5px; }` and a line `content: '●';`
`good.css`: `.a { color: var(--fg); font-size: var(--text-sm); } .b { color: #fff; /* ui-lint: allow */ }`

- [ ] **Step 2: Script**

```bash
#!/usr/bin/env bash
# UI literal lint. Rules (docs/design-docs/ui/README.md, principle 3):
#   hex      — raw #rgb/#rrggbb/#rrggbbaa outside src/theme/{tokens,themes}.css
#   px-size  — font-size: <n>px outside src/theme/tokens.css
#   glyph    — non-ASCII characters in src/app/**/*.ts and index.html
#              (icons come from the sprite; text separators are allow-listed)
# A trailing `/* ui-lint: allow */` (CSS) or `// ui-lint: allow` (TS) exempts a line.
# Exit 0 in warn mode; --strict exits 1 on any violation. Phase 2 flips CI to --strict.
set -euo pipefail
cd "$(dirname "$0")/.."
FE=cmd/hivegui/frontend
strict=0; [[ "${1:-}" == "--strict" ]] && strict=1
targets=("${@:2}"); [[ ${#targets[@]} -eq 0 ]] && targets=("$FE/src" "$FE/index.html")
n=0
report() { echo "$1"; n=$((n+1)); }
while IFS= read -r line; do report "$line"; done < <(
  grep -rnE --include='*.css' '#[0-9a-fA-F]{3,8}\b' "${targets[@]}" 2>/dev/null \
    | grep -v -e 'src/theme/tokens.css' -e 'src/theme/themes.css' -e 'ui-lint: allow' \
    | sed 's/^/hex: /' || true)
while IFS= read -r line; do report "$line"; done < <(
  grep -rnE --include='*.css' 'font-size:\s*[0-9.]+px' "${targets[@]}" 2>/dev/null \
    | grep -v -e 'src/theme/tokens.css' -e 'ui-lint: allow' \
    | sed 's/^/px-size: /' || true)
ALLOW='…·⌘⇧⌥⌃←→↑↓'
while IFS= read -r line; do report "$line"; done < <(
  grep -rnP --include='*.ts' --include='*.html' "[^\x00-\x7F$ALLOW]" "$FE/src/app" "$FE/index.html" 2>/dev/null \
    | grep -v -e 'ui-lint: allow' -e '^\S*:\s*//' \
    | sed 's/^/glyph: /' || true)
echo "ui-lint: $n violation(s)"
[[ $strict -eq 1 && $n -gt 0 ]] && exit 1
exit 0
```

- [ ] **Step 3: Self-test**

Run: `scripts/ui-lint.sh --strict scripts/testdata/ui-lint/bad.css; echo exit=$?`
Expected: 3 violations (hex, px-size, glyph — the glyph rule only scans `src/app`, so expect 2 from the fixture plus whatever `src/app` currently has), `exit=1`.
Run: `scripts/ui-lint.sh --strict scripts/testdata/ui-lint/good.css` → `0 violation(s)`, exit 0.
Run: `scripts/ui-lint.sh` on the real tree → prints the current backlog (expected: the `ui-lint: allow` leftovers from Task 4 are silent; every `content: '●'`/`×`/`＋` in `src/app` and `index.html` shows up — that list is Phase 2's worklist), exit 0.

- [ ] **Step 4: CI step**

```yaml
      - name: UI lint (tokens / icons — warn mode until phase 2)
        if: matrix.biome
        run: ./scripts/ui-lint.sh
```

- [ ] **Step 5: Commit**

```bash
chmod +x scripts/ui-lint.sh
git add scripts/ui-lint.sh scripts/testdata/ui-lint .github/workflows/ci.yml
git commit -m "chore(ci): add ui-lint for colour/size/glyph literals (warn mode)"
```

---

### Task 8: Docs + changeset + plan bookkeeping

**Files:**
- Modify: `docs/design-docs/ui/README.md` (Status line → "Phase 1 implemented")
- Modify: `docs/exec-plans/active/ui-design-system.md` (Progress: tick Phase 1; decision-log entry for any mapping surprises from Task 4)
- Create: `.changesets/<pr>-ui-tokens.md` via `/hs-changelog-update` — user-visible text: "Theme presets groundwork: `localStorage['hive.theme']` accepts `classic` (default), `hive-dark`, `hive-light`, `system`. No visual change by default."

- [ ] **Step 1: Make the edits, run the full local gate**

Run: `cd cmd/hivegui/frontend && npx biome ci . && npm run typecheck && npx vitest run && npx playwright test && cd ../../.. && scripts/ui-lint.sh && go build ./...`

- [ ] **Step 2: Commit and open PR**

```bash
git add docs .changesets
git commit -m "docs(ui): mark phase 1 of the design system implemented"
```

PR title: `feat(theme): design-system phase 1 — tokens, presets, ui-lint (no visual change)`. Body: link the spec, paste the Task 5 screenshot result, paste the `ui-lint` backlog count.

---

## Self-review

- **Spec coverage:** tokens.md → Task 2; themes.md presets (classic/hive-dark/hive-light, localStorage key, xterm mapping) → Tasks 2/3/6; README principle 3 (lint) → Task 7; "both light and dark at launch" → hive-light present from Task 2, picker UI deferred to Phase 5 per master plan. Native/terminal presets, fonts, overrides textarea, contrast check: explicitly Phase 5/6 — not gaps.
- **Placeholders:** none; Task 4 step 2 is a decision procedure, not a TODO.
- **Type consistency:** `resolveTheme`/`readTheme`/`applyTheme`/`xtermTheme`/`THEME_KEY`/`DEFAULT_THEME` used identically across Tasks 3, 5, 6.
- **Known risk:** `--hover` in `classic` is a solid `#2a2a2a` while `hive-dark` uses `color-mix` — both valid CSS; Task 5 catches any place where the old code used `#2a2a2a` as something other than a hover fill (then it needs its own mapping, not `--hover`).

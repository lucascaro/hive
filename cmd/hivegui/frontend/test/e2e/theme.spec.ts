import { test, expect, type Page } from '@playwright/test';
import { PRESETS } from '../../src/theme/theme';

// Preset guard. Originally a Phase-1 proof that the token migration moved
// no pixel; Phase 4 rebuilt the chrome markup on purpose, so `classic` can
// no longer reproduce v2.4.0 pixels — a preset is a set of token values,
// not of markup. These baselines now guard preset *switching*: the same
// scene under classic / hive-dark / hive-light must stay stable.
//
// These toHaveScreenshot() assertions run ONLY when HIVE_SNAPSHOT=1: CI runs
// this suite on ubuntu-latest, macos-latest and windows-latest, Playwright
// suffixes baselines per-platform, and darwin-only PNGs would fail the
// linux/windows legs outright (maxDiffPixels: 0 is not achievable across
// renderers anyway). The baselines here are a one-time local before/after
// proof for the token migration, generated and verified locally with
// HIVE_SNAPSHOT=1 npx playwright test test/e2e/theme.spec.ts --update-snapshots.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  // The bundled @font-face rules use font-display: swap, so the system
  // fallback paints first and the real face swaps in when the woff2
  // finishes decoding. Every baseline below asserts maxDiffPixels: 0, so
  // capturing mid-swap is a guaranteed flake — and it would flake toward
  // a WRONG baseline if it lost the race during --update-snapshots.
  await page.evaluate(() => document.fonts.ready);
}

// Shared scene for the sidebar pixel baselines: two projects, three
// sessions, one minimized, one with attention. Used by the classic guard
// (Phase 1) and the hive-dark/hive-light baselines (Phase 3) alike, so all
// three screenshots show the same layout.
async function seedSidebar(page: Page) {
  // Second project, so the sidebar renders two project groups. No
  // window.__hive helper creates projects (only sessions), so seed the
  // mock's own state + emit the same 'project:event' the real
  // CreateProject binding fires (test/e2e/wails-mock.ts CreateProject).
  await page.evaluate(() => {
    const p = {
      id: 'p2',
      name: 'second',
      color: '#0af',
      cwd: '',
      order: 1,
      created: new Date().toISOString(),
    };
    window.__hive.state?.projects.push(p);
    window.__hive.emit(
      'project:event',
      JSON.stringify({ kind: 'added', project: p }),
    );
  });
  await page.waitForFunction(
    () => document.querySelectorAll('#projects .hv-project-card').length >= 2,
  );

  // Two more sessions in the default project (three total).
  await page.evaluate((n) => window.__hive.addSession?.(n), 's2');
  await page.evaluate((n) => window.__hive.addSession?.(n), 's3');
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) >= n,
    3,
  );

  // s2 minimized: click its tile's minimize button in grid view.
  await page.keyboard.press(`${mod}+Shift+g`);
  await expect(page.locator('#terms')).toHaveClass(/grid/);
  const s2Sid = await page.evaluate(
    () => window.__hive.state?.sessions.find((s) => s.name === 's2')?.id,
  );
  // Tile actions are hover-revealed (patterns.md), so the header has to
  // be hovered before the button is clickable.
  await page
    .locator(`.term-host.in-grid[data-sid="${s2Sid}"] .tile-header`)
    .hover();
  await page
    .locator(`.term-host.in-grid[data-sid="${s2Sid}"] .tile-minimize`)
    .click();
  await expect(
    page.locator(`.term-host.in-grid[data-sid="${s2Sid}"]`),
  ).toHaveCount(0);

  // Seed session s1 ("main") gets attention: every 'added' session auto-
  // activates (see events.ts session:event 'added' -> switchTo), so s3
  // is active by now and s1 is the non-active session a BEL can mark.
  // onSessionBell ignores a bell on the active+focused session, the same
  // path a real bell in the PTY stream drives.
  await page.evaluate(() => window.__hive.emit('pty:data', 's1', btoa('\x07')));
  await expect(
    page.locator('#projects li.hv-session-row[data-sid="s1"]'),
  ).toHaveAttribute('data-state', 'attention');

  await expect(page.locator('#projects .hv-project-card')).toHaveCount(2);
}

test.describe('preset switching keeps the chrome stable', () => {
  test.skip(
    !process.env.HIVE_SNAPSHOT,
    'pixel baselines are darwin-local; run with HIVE_SNAPSHOT=1',
  );

  test('sidebar + terminal', async ({ page }) => {
    await page.addInitScript(() =>
      localStorage.setItem('hive.theme', 'classic'),
    );
    await page.setViewportSize({ width: 1100, height: 700 });
    await boot(page);
    await seedSidebar(page);
    // Named for what it is: the only FULL-PAGE baseline. The per-preset
    // block below owns `sidebar-<preset>.png`, scoped to #sidebar.
    await expect(page).toHaveScreenshot('chrome-classic.png', {
      maxDiffPixels: 0,
      animations: 'disabled',
      mask: [page.locator('.xterm')], // terminal content is not under test
    });
  });

  test('settings dialog', async ({ page }) => {
    await page.addInitScript(() =>
      localStorage.setItem('hive.theme', 'classic'),
    );
    await page.setViewportSize({ width: 1100, height: 700 });
    await boot(page);
    await page.keyboard.press(`${mod}+,`);
    await expect(page.locator('#settings')).toBeVisible();
    await expect(page).toHaveScreenshot('settings-classic.png', {
      maxDiffPixels: 0,
      animations: 'disabled',
    });
  });
});

// themes.md's "Adding a preset" step 4: sidebar + dialog under every
// preset. Generated from PRESETS, so a seventh preset gets its pair by
// existing rather than by someone remembering to add a test.
//
// These are the only check that catches a preset which parses fine, clears
// contrast, and still looks broken — a token that falls through to
// hive-dark's dark surface inside a light preset paints correctly by every
// other measure we have.
//
// Element-scoped, not full-page: the terminal is live content and would
// need masking on every shot. The full-page classic guard above is the one
// place the whole chrome is pinned.
test.describe('preset baselines', () => {
  test.skip(
    !process.env.HIVE_SNAPSHOT,
    'pixel baselines are darwin-local; run with HIVE_SNAPSHOT=1',
  );

  for (const { id } of PRESETS.filter((p) => p.id !== 'system')) {
    test(`${id}: sidebar`, async ({ page }) => {
      await page.addInitScript(
        (t) => localStorage.setItem('hive.theme', t),
        id,
      );
      await page.setViewportSize({ width: 1100, height: 700 });
      await boot(page);
      await seedSidebar(page);
      await expect(page.locator('#sidebar')).toHaveScreenshot(
        `sidebar-${id}.png`,
        { maxDiffPixels: 0, animations: 'disabled' },
      );
    });

    test(`${id}: dialog`, async ({ page }) => {
      await page.addInitScript(
        (t) => localStorage.setItem('hive.theme', t),
        id,
      );
      await page.setViewportSize({ width: 1100, height: 700 });
      await boot(page);
      await page.keyboard.press(`${mod}+,`);
      await expect(page.locator('#settings')).toBeVisible();
      await expect(page.locator('#settings-panel')).toHaveScreenshot(
        `dialog-${id}.png`,
        { maxDiffPixels: 0, animations: 'disabled' },
      );
    });
  }
});

// Standing guard: presets actually switch styles (deterministic, runs on all
// platforms, not pixel-gated).
test('hive-light preset changes the sidebar ground', async ({ page }) => {
  await page.addInitScript(() =>
    localStorage.setItem('hive.theme', 'hive-light'),
  );
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );

  // Verify the theme was actually applied by checking data-theme.
  const theme = await page.evaluate(
    () => document.documentElement.dataset.theme,
  );
  expect(theme).toBe('hive-light');

  // Verify #sidebar background resolves to white through the token.
  const bg = await page
    .locator('#sidebar')
    .evaluate((el) => getComputedStyle(el).backgroundColor);
  expect(bg).toBe('rgb(255, 255, 255)');
});

// Standing guard: the xterm theme colours actually resolve from the live
// cascade under the default preset (screenshot tests mask .xterm, so this
// is the only coverage of the terminal's colour values). Reads the real
// document, not hand-set inline styles, so it proves the cascade + preset
// wiring, not just the xtermTheme() function in isolation.
test('classic preset resolves --term-bg/--term-fg to xterm v2.4.0 colours', async ({
  page,
}) => {
  // classic is a preset, not the default, since phase 6 — it has to be
  // asked for by name.
  await page.addInitScript(() => localStorage.setItem('hive.theme', 'classic'));
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );

  const theme = await page.evaluate(
    () => document.documentElement.dataset.theme,
  );
  expect(theme).toBe('classic');

  const { termBg, termFg } = await page.evaluate(() => {
    const cs = getComputedStyle(document.documentElement);
    return {
      termBg: cs.getPropertyValue('--term-bg').trim(),
      termFg: cs.getPropertyValue('--term-fg').trim(),
    };
  });
  expect(termBg).toBe('#000');
  expect(termFg).toBe('#ffffff');
});

// Standing guard: runs on every platform/CI leg (no HIVE_SNAPSHOT gate).
// scripts/ui-lint.sh's GLYPH_DENY reads source; this reads the rendered DOM
// the user actually sees. Keep this denylist identical to GLYPH_DENY in
// scripts/ui-lint.sh — copy any change there over here too.
// Coverage boundary: this only sees textContent, so it cannot catch a glyph
// delivered via CSS content: on a pseudo-element (::before/::after) — that
// source-level case is scripts/ui-lint.sh's job, which scans src/theme/.
test('no Unicode glyph is used as a control label', async ({ page }) => {
  await page.goto('/');
  const found = await page.evaluate(() => {
    const deny = /[×✕✗＋✚⎇✎▾▴●○◐◆■▶⟳↻✓✔]/;
    return [...document.querySelectorAll('button, .hv-project-card__chevron')]
      .filter((el) => deny.test(el.textContent ?? ''))
      .map((el) => `${el.tagName}#${el.id}.${el.className}`);
  });
  expect(found).toEqual([]);
});

// Settings › Appearance, driven through the real dialog. The unit tests
// prove the sanitiser; this proves the wiring — that picking a preset
// repaints the app, that a bad override line is reported instead of
// injected, and that a good one survives a reload.
test.describe('Settings > Appearance', () => {
  async function openAppearance(page: Page) {
    await boot(page);
    await page.keyboard.press(`${mod}+,`);
    await expect(page.locator('#settings')).toBeVisible();
  }

  test('picking a preset repaints the sidebar and is remembered', async ({
    page,
  }) => {
    // The default is 'system' now, so what it resolves to depends on the
    // runner's colour scheme — and if that were already light the repaint
    // assertion below would be vacuous. Pin the media query rather than
    // seeding storage: addInitScript would re-run on the reload at the end
    // of this test and overwrite the preference it is checking survived.
    await page.emulateMedia({ colorScheme: 'dark' });
    await openAppearance(page);
    await expect(page.locator('html')).toHaveAttribute(
      'data-theme',
      'hive-dark',
    );
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

    // Cancel does not revert it — Appearance is a preference, not part
    // of the agent draft.
    await page.locator('#settings-cancel').click();
    await expect(page.locator('html')).toHaveAttribute(
      'data-theme',
      'hive-light',
    );
    expect(await page.evaluate(() => localStorage.getItem('hive.theme'))).toBe(
      'hive-light',
    );

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute(
      'data-theme',
      'hive-light',
    );
  });

  // index.html's pre-paint script cannot import theme.ts, so it carries
  // its own copy of the stampable preset names. This is the guard that
  // makes the duplication safe rather than a trap for phase 6.
  test('the boot script knows every preset theme.ts stamps', async ({
    page,
  }) => {
    await boot(page);
    const script = await page.evaluate(
      () => document.head.querySelector('script:not([src])')?.textContent ?? '',
    );
    const stampable = await page
      .locator('#settings-theme option')
      .evaluateAll((os) =>
        os
          .map((o) => (o as HTMLOptionElement).value)
          .filter((v) => v !== 'system'),
      );
    // The picker is only rendered after ⌘, so seed it first.
    for (const name of stampable) expect(script).toContain(`'${name}'`);
  });

  test('the preset list is exactly what theme.ts exports', async ({ page }) => {
    await openAppearance(page);
    // Guards the "data-driven from PRESETS" requirement: phase 6 adds
    // native-*/terminal by editing theme.ts alone.
    const values = await page
      .locator('#settings-theme option')
      .evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value));
    expect(values).toEqual([
      'system',
      'hive-dark',
      'hive-light',
      'native-dark',
      'native-light',
      'terminal',
      'classic',
    ]);
  });

  // A preset listed in PRESETS but with no :root[data-theme] block in
  // themes.css is the silent failure themes.md warns about: it inherits
  // hive-dark wholesale and looks broken on a light ground, while every
  // other check stays green. Distinct grounds are the cheapest proof each
  // block exists and is actually reached — and the values come from the
  // live cascade, so a typo'd selector fails here too.
  test('every preset paints its own tokens and its own ANSI 16', async ({
    page,
  }) => {
    await openAppearance(page);
    const seen = new Map<string, string>();
    const ids = await page
      .locator('#settings-theme option')
      .evaluateAll((os) =>
        os
          .map((o) => (o as HTMLOptionElement).value)
          .filter((v) => v !== 'system'),
      );
    for (const id of ids) {
      await page.locator('#settings-theme').selectOption(id);
      await expect(page.locator('html')).toHaveAttribute('data-theme', id);
      const { ground, ansi } = await page.evaluate(() => {
        const cs = getComputedStyle(document.documentElement);
        return {
          ground: ['--bg', '--surface', '--fg', '--accent', '--term-bg']
            .map((t) => cs.getPropertyValue(t).trim())
            .join('/'),
          ansi: Array.from({ length: 16 }, (_, i) =>
            cs.getPropertyValue(`--ansi-${i}`).trim(),
          ),
        };
      });
      // themes.md's "Adding a preset" step 2: all sixteen, every preset.
      expect(ansi.filter(Boolean), `${id} ANSI`).toHaveLength(16);
      // And they reach xterm, not just the cascade — xterm caches its
      // palette, so the CSS alone proves nothing.
      await expect
        .poll(() => page.evaluate(() => window.__hive.termAnsi?.() ?? []))
        .toEqual(ansi);
      const clash = seen.get(ground);
      expect(clash, `${id} paints the same tokens as ${clash}`).toBeUndefined();
      seen.set(ground, id);
    }
    expect(seen.size).toBe(ids.length);
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

    // Applying trails typing by a debounce (settings.ts), so poll
    // rather than reading once.
    await page.locator('#settings-overrides').fill('--accent: #7aa2f7;');
    await expect.poll(accent).toBe('#7aa2f7');
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
    await expect.poll(accent).toBe('#7aa2f7');
    await expect(page.locator('#sidebar')).toBeVisible();

    // Only the sanitised text is persisted.
    await expect
      .poll(() =>
        page.evaluate(() => localStorage.getItem('hive.themeOverrides')),
      )
      .toBe('--accent: #7aa2f7;');
  });

  // The pre-paint boot script writes straight into a <style>, one paint
  // before theme.ts's sanitiser runs. The store is hand-editable, so
  // that script shape-checks it: a hostile value must never reach the
  // document at all, not merely be corrected a paint later.
  test('a hand-edited override store is not injected before paint', async ({
    page,
  }) => {
    await boot(page);
    await page.evaluate(() => {
      localStorage.setItem(
        'hive.themeOverrides',
        '--bg: image-set("https://evil.example/x.png");',
      );
    });
    await page.reload();
    const injected = await page.evaluate(
      () => document.getElementById('theme-overrides')?.textContent ?? '',
    );
    expect(injected).toBe('');

    // A CSS escape tokenizes back into url() after parsing, so a gate
    // that matches on the spelled function name is no gate at all.
    await page.evaluate(() => {
      localStorage.setItem(
        'hive.themeOverrides',
        '--bg: \\75rl("https://evil.example/x.png");',
      );
    });
    await page.reload();
    expect(
      await page.evaluate(
        () => document.getElementById('theme-overrides')?.textContent ?? '',
      ),
    ).toBe('');
    await page.evaluate(() => localStorage.removeItem('hive.themeOverrides'));
  });

  // The gap this closes: with no --ansi-* tokens, xterm kept its Tango
  // defaults under every preset, and seven of those fail WCAG AA on a
  // white ground (brightWhite at 1.16:1 — invisible). Parametrised over
  // every light preset rather than hive-light alone: the rule is
  // themes.md's "a preset on a light ground MUST re-value all sixteen",
  // and the ratios are computed here rather than pinned as hexes, so a
  // preset added to this list is checked by the rule, not by a fixture.
  for (const preset of ['hive-light', 'native-light'] as const) {
    test(`${preset} gives terminals a palette readable on its own ground`, async ({
      page,
    }) => {
      await openAppearance(page);
      await page.locator('#settings-theme').selectOption(preset);
      await expect(page.locator('html')).toHaveAttribute('data-theme', preset);

      const palette = await page.evaluate(() => {
        const cs = getComputedStyle(document.documentElement);
        return Array.from({ length: 16 }, (_, i) =>
          cs.getPropertyValue(`--ansi-${i}`).trim(),
        );
      });
      expect(palette.filter(Boolean)).toHaveLength(16);

      // Contrast against the terminal ground, computed here rather than
      // asserted as fixed hexes: the point is legibility, not the values.
      const worst = await page.evaluate((pal) => {
        const lum = (hex: string) => {
          const c = (hex.replace('#', '').match(/../g) ?? []).map((h) => {
            const v = parseInt(h, 16) / 255;
            return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
          });
          return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
        };
        const bg = getComputedStyle(document.documentElement)
          .getPropertyValue('--term-bg')
          .trim();
        return Math.min(
          ...pal.map((c) => {
            const [hi, lo] = [lum(c), lum(bg)].sort((a, b) => b - a);
            return (hi + 0.05) / (lo + 0.05);
          }),
        );
      }, palette);
      expect(worst).toBeGreaterThanOrEqual(4.5);

      // And it actually reaches the terminal, not just the CSS.
      await expect
        .poll(() => page.evaluate(() => window.__hive.termAnsi?.()[15] ?? ''))
        .toBe(palette[15]);
    });
  }

  test('an override reaches every open terminal', async ({ page }) => {
    await openAppearance(page);
    await page.locator('#settings-overrides').fill('--term-bg: #123456;');
    // Read off the live Terminal, not the DOM: xterm caches its palette,
    // so CSS alone would prove nothing (window.__hive.termThemeBg, in
    // test/e2e/wails-mock.ts).
    await expect
      .poll(() => page.evaluate(() => window.__hive.termThemeBg?.() ?? ''))
      .toBe('#123456');
  });
});

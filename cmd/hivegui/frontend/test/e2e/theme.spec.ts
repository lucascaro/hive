import { test, expect, type Page } from '@playwright/test';

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

test.describe('classic preset is pixel-identical to v2.4.0', () => {
  test.skip(
    !process.env.HIVE_SNAPSHOT,
    'pixel baselines are darwin-local; run with HIVE_SNAPSHOT=1',
  );

  test('sidebar + terminal', async ({ page }) => {
    await page.setViewportSize({ width: 1100, height: 700 });
    await boot(page);
    await seedSidebar(page);
    await expect(page).toHaveScreenshot('sidebar-classic.png', {
      maxDiffPixels: 0,
      animations: 'disabled',
      mask: [page.locator('.xterm')], // terminal content is not under test
    });
  });

  test('settings dialog', async ({ page }) => {
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
// source-level case is scripts/ui-lint.sh's job, which scans src/style.css.
test('no Unicode glyph is used as a control label', async ({ page }) => {
  await page.goto('/');
  const found = await page.evaluate(() => {
    const deny = /[×✕✗＋✚⎇✎▾▴●○◐◆■▶⟳↻✓✔]/;
    return [
      ...document.querySelectorAll(
        'button, .hv-project-card__chevron, .worktree-glyph',
      ),
    ]
      .filter((el) => deny.test(el.textContent ?? ''))
      .map((el) => `${el.tagName}#${el.id}.${el.className}`);
  });
  expect(found).toEqual([]);
});

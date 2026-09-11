import { test, expect, type Page } from '@playwright/test';

// The density setting is a claim about how many sessions fit on screen,
// so it is measured in a browser rather than asserted on CSS text. The
// spec's number — compact fits at least 40% more rows than normal — is
// the one checked here; a mock claimed 60% during review and measured 40.

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page, sessions = 8) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  for (let i = 1; i < sessions; i++) {
    await page.evaluate((n) => window.__hive.addSession?.(n), `s${i + 1}`);
  }
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) >= n,
    sessions,
  );
  // Titles on every row, so `compact` is actually dropping a line that
  // has something in it.
  await page.evaluate(() => {
    for (const s of window.__hive.state?.sessions ?? []) {
      s.title = 'npm run build --watch';
      window.__hive.emit(
        'session:event',
        JSON.stringify({ kind: 'title', session: s }),
      );
    }
  });
}

function rowHeight(page: Page) {
  return page
    .locator('#projects .hv-session-row')
    .first()
    .evaluate((el) => el.getBoundingClientRect().height);
}

async function setDensity(page: Page, value: string) {
  await page.keyboard.press(`${MOD}+,`);
  // Settings opens on Agents; density lives under Appearance.
  await page.locator('#settings-tab-appearance').click();
  await expect(page.locator('#settings-density')).toBeVisible();
  await page.locator('#settings-density').selectOption(value);
  await page.keyboard.press('Escape');
  await expect(page.locator('#settings-density')).toBeHidden();
}

test.describe('sidebar density', () => {
  test('compact fits at least 40% more rows than normal', async ({ page }) => {
    await boot(page);
    const normal = await rowHeight(page);
    expect(normal).toBeCloseTo(40, 0);

    await setDensity(page, 'compact');
    const compact = await rowHeight(page);
    // Rows per unit height scales as 1/height.
    expect(normal / compact).toBeGreaterThanOrEqual(1.4);

    await setDensity(page, 'tight');
    const tight = await rowHeight(page);
    expect(tight).toBeLessThan(normal);
    expect(tight).toBeGreaterThan(compact);
  });

  test('compact shows one line; tight keeps both', async ({ page }) => {
    await boot(page);
    const sub = page.locator('#projects .hv-session-row__sub').first();
    await expect(sub).toBeVisible();

    await setDensity(page, 'compact');
    await expect(sub).toBeHidden();

    await setDensity(page, 'tight');
    await expect(sub).toBeVisible();
  });

  test('remembers the choice across a reload', async ({ page }) => {
    await boot(page);
    await setDensity(page, 'compact');
    await page.reload();
    await page.waitForFunction(
      () => document.querySelectorAll('#projects li').length > 0,
    );
    await expect(page.locator('html')).toHaveAttribute(
      'data-density',
      'compact',
    );
  });
});

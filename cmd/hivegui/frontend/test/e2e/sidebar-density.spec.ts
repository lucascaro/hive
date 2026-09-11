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

  test('compact keeps the window title and drops the name', async ({
    page,
  }) => {
    await boot(page);
    const row = page.locator('#projects .hv-session-row').first();
    await expect(row.locator('.hv-session-row__name')).toBeVisible();
    await expect(row.locator('.hv-session-row__sub')).toBeVisible();

    await setDensity(page, 'compact');
    // The title is the line that tells two sessions on one worktree
    // apart, so it is the one that survives.
    await expect(row.locator('.hv-session-row__sub')).toBeVisible();
    await expect(row.locator('.hv-session-row__sub')).toHaveText(
      'npm run build --watch',
    );
    await expect(row.locator('.hv-session-row__name')).toBeHidden();
    // …and it sits on line 1, where the name was.
    const [sub, state] = await Promise.all([
      row.locator('.hv-session-row__sub').boundingBox(),
      row.locator('.hv-session-row__state').boundingBox(),
    ]);
    if (!sub || !state) throw new Error('row not laid out');
    expect(
      Math.abs(sub.y + sub.height / 2 - (state.y + state.height / 2)),
    ).toBeLessThan(4);

    await setDensity(page, 'tight');
    await expect(row.locator('.hv-session-row__name')).toBeVisible();
    await expect(row.locator('.hv-session-row__sub')).toBeVisible();
  });

  // subtitleFor() leaves line 2 empty for a running session that has
  // published no title. Compact must not then show a row with no line at
  // all — the name comes back.
  test('compact falls back to the name when there is no title', async ({
    page,
  }) => {
    await boot(page);
    await page.evaluate(() => {
      const s = window.__hive.state?.sessions[0];
      if (!s) throw new Error('no mock session');
      s.title = '';
      window.__hive.emit(
        'session:event',
        JSON.stringify({ kind: 'title', session: s }),
      );
    });
    const row = page.locator('#projects .hv-session-row').first();
    await expect(row.locator('.hv-session-row__sub')).toHaveText('');

    await setDensity(page, 'compact');
    await expect(row.locator('.hv-session-row__name')).toBeVisible();
    await expect(row.locator('.hv-session-row__name')).not.toHaveText('');
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

import { test, expect, type Page } from '@playwright/test';

// E2E coverage for the session-minimize feature (#202).

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

async function bootWithSessions(page: Page, count = 2) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  for (let i = 1; i < count; i++) {
    await page.evaluate((n) => window.__hive.addSession?.(n), `s${i + 1}`);
  }
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) >= n,
    count,
  );
}

async function enterGridAll(page: Page) {
  await page.keyboard.press(`${MOD}+Shift+g`);
  await expect(page.locator('#terms')).toHaveClass(/grid/);
}

test.describe('session minimize', () => {
  test('tray hidden when no sessions minimized', async ({ page }) => {
    await bootWithSessions(page, 2);
    await expect(page.locator('#minimized-tray')).toHaveClass(/hidden/);
  });

  test('minimize hides tile from grid-all view and reveals tray chip', async ({
    page,
  }) => {
    await bootWithSessions(page, 3);
    await enterGridAll(page);

    // Three tiles visible in grid.
    await expect(page.locator('.term-host.in-grid')).toHaveCount(3);

    // Click the minimize button on the first tile.
    const firstTile = page.locator('.term-host.in-grid').first();
    const sid = await firstTile.evaluate((el) => el.dataset.sid);
    if (!sid) throw new Error('the first tile carries no data-sid');
    await firstTile.locator('.tile-minimize').click();

    // The tile is removed from the grid.
    await expect(page.locator('.term-host.in-grid')).toHaveCount(2);
    // And appears in the tray.
    const tray = page.locator('#minimized-tray');
    await expect(tray).not.toHaveClass(/hidden/);
    await expect(tray.locator('.min-chip')).toHaveCount(1);
    await expect(tray.locator(`.min-chip[data-sid="${sid}"]`)).toBeVisible();
  });

  // Three sessions, not two: a grid that drops below two tiles now
  // returns to focused mode (see ordering.spec.ts), so minimizing one of
  // a pair would leave no grid to restore into.
  test('restore from tray returns session to grid', async ({ page }) => {
    await bootWithSessions(page, 3);
    await enterGridAll(page);

    const firstTile = page.locator('.term-host.in-grid').first();
    const sid = await firstTile.evaluate((el) => el.dataset.sid);
    await firstTile.locator('.tile-minimize').click();
    await expect(page.locator('.term-host.in-grid')).toHaveCount(2);

    await page.locator(`#minimized-tray .min-chip[data-sid="${sid}"]`).click();
    await expect(page.locator('.term-host.in-grid')).toHaveCount(3);
    await expect(page.locator('#minimized-tray')).toHaveClass(/hidden/);
  });

  test('minimizing a grid down to one tile returns to focused mode', async ({
    page,
  }) => {
    await bootWithSessions(page, 2);
    await enterGridAll(page);
    await page
      .locator('.term-host.in-grid')
      .first()
      .locator('.tile-minimize')
      .click();
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });

  test('minimizing the active session moves focus to another visible tile', async ({
    page,
  }) => {
    await bootWithSessions(page, 3);
    await enterGridAll(page);
    // Make the first tile active.
    const tiles = page.locator('.term-host.in-grid');
    const firstSid = await tiles.first().evaluate((el) => el.dataset.sid);
    await tiles.first().click();
    await page.waitForFunction(
      (id) =>
        window.__hive &&
        document.querySelector(`.term-host.active[data-sid="${id}"]`),
      firstSid,
    );

    // Minimize the active tile.
    await tiles.first().locator('.tile-minimize').click();

    // Active session should no longer be the minimized one.
    const activeId = await page.evaluate(() => {
      const a = document.querySelector<HTMLElement>('.term-host.active');
      return a ? a.dataset.sid : null;
    });
    expect(activeId).not.toBe(firstSid);
    expect(activeId).not.toBeNull();
  });

  test('removing a minimized session clears it from the tray', async ({
    page,
  }) => {
    await bootWithSessions(page, 2);
    await enterGridAll(page);
    const firstTile = page.locator('.term-host.in-grid').first();
    const sid = await firstTile.evaluate((el) => el.dataset.sid);
    if (!sid) throw new Error('the first tile carries no data-sid');
    await firstTile.locator('.tile-minimize').click();
    await expect(
      page.locator(`#minimized-tray .min-chip[data-sid="${sid}"]`),
    ).toBeVisible();

    await page.evaluate((id) => window.__hive.killSession?.(id), sid);
    await expect(
      page.locator(`#minimized-tray .min-chip[data-sid="${sid}"]`),
    ).toHaveCount(0);
    await expect(page.locator('#minimized-tray')).toHaveClass(/hidden/);
  });
});

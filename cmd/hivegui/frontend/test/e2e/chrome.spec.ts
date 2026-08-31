import { test, expect, type Page } from '@playwright/test';

// Phase-4 baselines: the chrome (banners, status bar, tile headers,
// launcher rows) under both shipped presets. Same HIVE_SNAPSHOT gate as
// theme.spec.ts — Playwright suffixes baselines per-platform and CI runs
// three OSes, so these are a local review artefact, not a CI gate.
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function bootWith(page: Page, theme: string) {
  await page.addInitScript((t) => localStorage.setItem('hive.theme', t), theme);
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
      await expect(page.locator('#status-hint')).toContainText('focus');
      await expect(page).toHaveScreenshot(`grid-${theme}.png`, {
        maxDiffPixels: 0,
        animations: 'disabled',
        mask: [page.locator('.xterm')],
      });
    });

    test(`launcher — ${theme}`, async ({ page }) => {
      await bootWith(page, theme);
      await page.keyboard.press(`${mod}+t`);
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

import { test, expect, type Page } from '@playwright/test';

// Spec 416 phase 4, off macOS. Plain Ctrl+J is byte 0x0a — the newline
// key Claude Code documents for every terminal — so the activity toggle
// must not take it there. The app reads the platform from the browser,
// so the page is told it is Linux regardless of the host running the
// suite.

async function bootAsLinux(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'platform', { get: () => 'Linux x86_64' });
    Object.defineProperty(navigator, 'userAgentData', {
      get: () => ({ platform: 'Linux' }),
    });
  });
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await page.waitForFunction(
    () => document.activeElement?.classList?.contains('xterm-helper-textarea'),
    null,
    { timeout: 3000 },
  );
  await page.evaluate(() => window.__hive.resetStdin());
}

test.describe('spec 416 activity keys off macOS', () => {
  test('plain Ctrl+J still reaches the terminal as a newline', async ({
    page,
  }) => {
    await bootAsLinux(page);
    await page.keyboard.press('Control+j');
    await expect
      .poll(() =>
        page.evaluate(() =>
          [...window.__hive.stdinText()].map((c) => c.charCodeAt(0)),
        ),
      )
      .toEqual([0x0a]);
    await expect(page.locator('#activity-panel')).toBeHidden();
  });

  test('Ctrl+Shift+J toggles the panel', async ({ page }) => {
    await bootAsLinux(page);
    await page.keyboard.press('Control+Shift+J');
    await expect(page.locator('#activity-panel')).toBeVisible();
    expect(await page.evaluate(() => window.__hive.stdinText())).toBe('');
    await page.keyboard.press('Control+Shift+J');
    await expect(page.locator('#activity-panel')).toBeHidden();
  });
});

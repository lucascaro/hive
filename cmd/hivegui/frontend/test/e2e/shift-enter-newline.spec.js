import { test, expect } from '@playwright/test';

// E2E coverage for #217: Shift+Enter inserts a newline in the agent's
// input (Ctrl+J / 0x0a) instead of submitting, while plain Enter still
// submits (\r / 0x0d). The Wails mock loads a real xterm, so the custom
// key handler runs exactly as in production and WriteStdin records the
// bytes that reach the PTY.

async function bootFocused(page) {
  await page.goto('/');
  await page.waitForFunction(() => document.querySelectorAll('#projects li').length > 0);
  await page.waitForFunction(
    () => document.activeElement?.classList?.contains('xterm-helper-textarea'),
    null,
    { timeout: 3000 },
  );
}

test.describe('#217 Shift+Enter newline', () => {
  test('Shift+Enter sends a newline byte (0x0a) and does not submit', async ({ page }) => {
    await bootFocused(page);
    await page.evaluate(() => window.__hive.resetStdin());
    const viewBefore = await page.evaluate(() => document.getElementById('terms').className);

    await page.keyboard.press('Shift+Enter');

    await expect
      .poll(() => page.evaluate(() => [...window.__hive.stdinText()].map((c) => c.charCodeAt(0))))
      .toEqual([0x0a]);

    // Shift+Enter must not trigger any view change (it carries no Cmd/Ctrl,
    // so the capture-phase window shortcut handler ignores it).
    const viewAfter = await page.evaluate(() => document.getElementById('terms').className);
    expect(viewAfter).toBe(viewBefore);
  });

  test('plain Enter still submits (sends \\r / 0x0d)', async ({ page }) => {
    await bootFocused(page);
    await page.evaluate(() => window.__hive.resetStdin());

    await page.keyboard.press('Enter');

    await expect
      .poll(() => page.evaluate(() => [...window.__hive.stdinText()].map((c) => c.charCodeAt(0))))
      .toEqual([0x0d]);
  });
});

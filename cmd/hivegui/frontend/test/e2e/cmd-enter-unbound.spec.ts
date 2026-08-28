import { test, expect, type Page } from '@playwright/test';

// E2E coverage for #249: ⌘/Ctrl+Enter is no longer an app binding. It used
// to toggle single ⇄ grid-project (mirroring ⌘G) and was swallowed by the
// capture-phase window handler before xterm could see it, which made the
// chord unusable inside an agent session. It is now unbound — no view
// change, no replacement behavior. ⌘G / ⇧⌘G remain the grid toggles.

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
  await page.evaluate(() => window.__hive.resetStdin());
}

// The view is read off #terms' class list, the same signal focus.spec.ts
// asserts on: grid modes add a `grid` class, focused mode does not.
const termsClass = (page: Page) =>
  page.evaluate(() => {
    const terms = document.getElementById('terms');
    if (!terms) throw new Error('#terms missing');
    return terms.className;
  });

test.describe('#249 ⌘Enter unbound', () => {
  test('⌘Enter in single mode does not enter grid', async ({ page }) => {
    await bootWithSessions(page, 2);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    const before = await termsClass(page);

    await page.keyboard.press(`${MOD}+Enter`);
    // No transition to wait for, so give the handler a frame to be wrong in.
    await page.waitForTimeout(250);

    expect(await termsClass(page)).toBe(before);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });

  test('⌘Enter in grid mode does not maximize back to single', async ({
    page,
  }) => {
    await bootWithSessions(page, 2);
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    const before = await termsClass(page);

    await page.keyboard.press(`${MOD}+Enter`);
    await page.waitForTimeout(250);

    expect(await termsClass(page)).toBe(before);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
  });

  // The control: proves the deletion took only the ⌘Enter binding and left
  // the real grid toggles working.
  test('⌘G still toggles grid ⇄ single', async ({ page }) => {
    await bootWithSessions(page, 2);
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });
});

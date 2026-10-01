import { expect, type Page, test } from '@playwright/test';

// Spec 477 (phase 1): the app boots with the user's keymap.json — here
// seeded through the mock's GetKeymap — and every surface follows it:
// the new key runs the command, the old one does nothing, and the help
// overlay and the palette show the new key and never the old one.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';
const NEW = process.platform === 'darwin' ? '⌘Y' : 'Ctrl+Y';
const OLD = process.platform === 'darwin' ? '⌘T' : 'Ctrl+T';

// Both halves, so the spec does not care which platform the page is.
const KEYMAP = {
  version: 1,
  mac: { 'new-session': ['Mod+Y'] },
  other: { 'new-session': ['Mod+Y'] },
};

async function boot(page: Page, seed: unknown) {
  await page.addInitScript((k) => {
    (window as unknown as { __hive_keymapSeed: unknown }).__hive_keymapSeed = k;
  }, seed);
  await page.goto('/');
  await page.waitForSelector('#help-btn');
}

const launcher = (page: Page) => page.locator('#launcher');

test('a keymap loaded at boot moves the shortcut everywhere', async ({
  page,
}) => {
  await boot(page, KEYMAP);

  await page.keyboard.press(`${mod}+t`);
  await expect(launcher(page)).toBeHidden();
  await page.keyboard.press(`${mod}+y`);
  await expect(launcher(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(launcher(page)).toBeHidden();

  await page.keyboard.press(`${mod}+/`);
  const overlay = page.locator('#help-overlay');
  await expect(overlay).toBeVisible();
  const row = overlay.locator('dt', { hasText: NEW });
  await expect(row).toHaveCount(1);
  await expect(
    overlay.locator('dt', { hasText: new RegExp(`^${OLD}$`) }),
  ).toHaveCount(0);
  await page.keyboard.press('Escape');

  await page.keyboard.press(`${mod}+Shift+k`);
  await page.locator('#command-palette-input').fill('new session');
  const palette = page.locator('#command-palette');
  await expect(palette).toContainText(NEW);
  await expect(
    palette.locator('.hv-kbd', { hasText: new RegExp(`^${OLD}$`) }),
  ).toHaveCount(0);
});

test('a keymap change after boot applies with no reload', async ({ page }) => {
  await boot(page, {});
  await page.evaluate(async (k) => {
    const url = '/src/store/store.ts';
    const store = await import(/* @vite-ignore */ url);
    store.setKeymap(k);
  }, KEYMAP);
  await page.keyboard.press(`${mod}+t`);
  await expect(launcher(page)).toBeHidden();
  await page.keyboard.press(`${mod}+y`);
  await expect(launcher(page)).toBeVisible();
});

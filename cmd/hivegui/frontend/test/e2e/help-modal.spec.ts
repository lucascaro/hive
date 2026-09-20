import { test, expect, type Page } from '@playwright/test';

// Spec 442. The Help modal: opened from the sidebar button and from the
// command palette, closed on Escape, and handing off to the ⌘/ shortcuts
// overlay. Its LAYOUT in the header is sidebar-header-actions.spec.ts's job;
// this file is about behaviour.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForSelector('#help-btn');
}

test('the help button opens Help, and Escape closes it', async ({ page }) => {
  await boot(page);
  const dialog = page.locator('#help-modal');
  await expect(dialog).toBeHidden();

  await page.locator('#help-btn').click();
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('.help-concepts dt')).toHaveCount(4);
  // The binding rides next to the action, per AGENTS.md › Key Discoverability.
  await expect(dialog.locator('#help-shortcuts-row .hv-kbd')).toContainText(
    '/',
  );

  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
});

test('Help is reachable from the command palette', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+Shift+k`);
  await page.locator('#command-palette-input').fill('help');
  await page.keyboard.press('Enter');
  await expect(page.locator('#help-modal')).toBeVisible();
});

test('the shortcuts row hands Help off to the ⌘/ overlay', async ({ page }) => {
  await boot(page);
  await page.locator('#help-btn').click();
  await expect(page.locator('#help-modal')).toBeVisible();

  await page.locator('#help-shortcuts-row').click();
  // The handoff closes one and opens the other; both open at once would mean
  // two aria-modal dialogs stacked on the same z-index.
  await expect(page.locator('#help-modal')).toBeHidden();
  await expect(page.locator('#help-overlay')).toBeVisible();
});

test('⌘/ inside Help does the same handoff instead of being swallowed', async ({
  page,
}) => {
  await boot(page);
  await page.locator('#help-btn').click();
  await expect(page.locator('#help-modal')).toBeVisible();

  // Help puts ⌘/ on screen; a modal that displays a binding and then eats
  // the key is the failure this asserts against.
  await page.keyboard.press(`${mod}+/`);
  await expect(page.locator('#help-modal')).toBeHidden();
  await expect(page.locator('#help-overlay')).toBeVisible();
});

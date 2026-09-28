import { expect, type Page } from '@playwright/test';

// Shared by the UI plugin specs (spec 471). The mock serves plugin files
// at /plugins/<id>/ from test/fixtures/plugins and the repo's plugins/
// (vite.config.js), and its InstallPlugin reads the real manifest there,
// so a plugin installs exactly as it would from a folder.

export const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

export async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

export async function openPluginsTab(page: Page) {
  await page.keyboard.press(`${MOD}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  await page.locator('#settings-tab-plugins').click();
  await expect(page.locator('#settings-panel-plugins')).toBeVisible();
}

export async function closeSettings(page: Page) {
  await page.keyboard.press('Escape');
  await expect(page.locator('#settings')).toBeHidden();
}

/** Installs /plugins/<id> through Settings → Plugins (the mock's Confirm
 * accepts), leaves the Plugins tab open, and returns the row. */
export async function installPlugin(page: Page, id: string) {
  await openPluginsTab(page);
  await page.locator('#settings-plugin-source').fill(`/plugins/${id}`);
  await page.locator('#settings-plugin-install').click();
  const row = page.locator(`.settings-plugin-row[data-plugin-id="${id}"]`);
  await expect(row.locator('.settings-plugin-enabled')).toBeChecked();
  return row;
}

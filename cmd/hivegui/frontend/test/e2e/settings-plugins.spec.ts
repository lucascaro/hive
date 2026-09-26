import { test, expect, type Page } from '@playwright/test';

// E2E for Settings → Plugins (#460 phase 2) against the mock bridge: the
// golden path install → trust confirm → running → disable → remove, and
// a failed install surfacing in the dialog's error slot. The mock's
// Confirm always accepts; the decline branch is covered in
// test/dom/settings-plugins.test.tsx.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function openPluginsTab(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  await page.locator('#settings-tab-plugins').click();
  await expect(page.locator('#settings-panel-plugins')).toBeVisible();
}

test('install, enable, disable and remove a plugin', async ({ page }) => {
  await openPluginsTab(page);
  await expect(page.locator('#settings-plugins-empty')).toBeVisible();

  await page.locator('#settings-plugin-source').fill('/plugins/webhook');
  await page.locator('#settings-plugin-install').click();

  // Confirm accepted → enabled → the daemon reports it running.
  const row = page.locator('.settings-plugin-row[data-plugin-id="webhook"]');
  await expect(row).toHaveAttribute('data-status', 'running');
  await expect(row.locator('.settings-plugin-enabled')).toBeChecked();
  await expect(row).toContainText('/plugins/webhook');
  await expect(page.locator('#settings-plugin-source')).toHaveValue('');

  await row.locator('.settings-plugin-enabled').click();
  await expect(row).toHaveAttribute('data-status', 'disabled');
  await expect(row.locator('.settings-plugin-enabled')).not.toBeChecked();

  await row.locator('.settings-plugin-remove').click();
  await expect(row).toHaveCount(0);
  await expect(page.locator('#settings-plugins-empty')).toBeVisible();
});

test('a failed install shows its error and adds nothing', async ({ page }) => {
  await openPluginsTab(page);
  await page.locator('#settings-plugin-source').fill('/plugins/fail');
  await page.locator('#settings-plugin-source').press('Enter');
  await expect(page.locator('#settings-error')).toContainText(
    'plugin install failed: no hive-plugin.json',
  );
  await expect(page.locator('.settings-plugin-row')).toHaveCount(0);
  // The generic control-error status line stands aside for it.
  await expect(page.locator('#status-text')).not.toContainText(
    'plugin_install_failed',
  );
});

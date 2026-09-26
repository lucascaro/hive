import { test, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { join } from 'node:path';

// Settings → Plugins against a REAL hived (#460 phase 2). The mock
// harness proves the tab's wiring; this proves the round-trip it rests
// on: INSTALL_PLUGIN with a nonce through the ws-bridge, the daemon's
// "added" echo settling this window's install, the trust confirm
// (the harness Confirm accepts), and the live status the supervisor
// reports — running after enable, stopped after disable, gone after
// remove — all without restarting the daemon.
//
// The reference webhook plugin runs unconfigured (no config.json in its
// data dir), which it tolerates: it connects and posts nowhere.

const WS_URL = process.env.WS_BRIDGE_URL;
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

test.beforeEach(async ({ page }) => {
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
  }, WS_URL);
});

test('install, enable, disable and remove the webhook plugin', async ({
  page,
}) => {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');
  const repoRoot = execFileSync('git', ['rev-parse', '--show-toplevel'], {
    encoding: 'utf8',
  }).trim();

  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
    null,
    { timeout: 10000 },
  );
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  await page.locator('#settings-tab-plugins').click();

  await page
    .locator('#settings-plugin-source')
    .fill(join(repoRoot, 'plugins', 'webhook'));
  await page.locator('#settings-plugin-install').click();

  const row = page.locator('.settings-plugin-row[data-plugin-id="webhook"]');
  await expect(row).toHaveAttribute('data-status', 'running', {
    timeout: 15000,
  });
  await expect(row.locator('.settings-plugin-enabled')).toBeChecked();

  await row.locator('.settings-plugin-enabled').click();
  await expect(row).toHaveAttribute('data-status', 'disabled');

  await row.locator('.settings-plugin-remove').click();
  await expect(row).toHaveCount(0);
});

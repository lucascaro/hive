import { test, expect, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';

// UI plugins (spec 471) against a REAL hived and hived-ws-bridge: a
// UI-only plugin installed from a folder, its module served from the
// isolated state dir by the bridge (as the app's asset server does from
// the real one) and imported into the page, its settings round-tripping
// through SET_PLUGIN_CONFIG into plugin-data/<id>/ui-config.json — and a
// plugin that throws, contained.

const WS_URL = process.env.WS_BRIDGE_URL;
const TMP = process.env.HIVE_E2E_REAL_TMP ?? '';
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';
const repoRoot = execFileSync('git', ['rev-parse', '--show-toplevel'], {
  encoding: 'utf8',
}).trim();

test.beforeEach(async ({ page }) => {
  // Fail, never skip: a missing harness must not read as a pass.
  expect(
    WS_URL,
    'WS_BRIDGE_URL not set — globalSetup did not run',
  ).toBeTruthy();
  expect(TMP).toBeTruthy();
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
  }, WS_URL);
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
    null,
    { timeout: 10000 },
  );
});

async function install(page: Page, dir: string, id: string) {
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  await page.locator('#settings-tab-plugins').click();
  await page.locator('#settings-plugin-source').fill(dir);
  await page.locator('#settings-plugin-install').click();
  const row = page.locator(`.settings-plugin-row[data-plugin-id="${id}"]`);
  await expect(row.locator('.settings-plugin-enabled')).toBeChecked({
    timeout: 15000,
  });
  return row;
}

async function remove(page: Page, id: string) {
  const row = page.locator(`.settings-plugin-row[data-plugin-id="${id}"]`);
  await row.locator('.settings-plugin-remove').click();
  await expect(row).toHaveCount(0);
}

test('session-notes loads from the state dir and saves its settings', async ({
  page,
}) => {
  const row = await install(
    page,
    path.join(repoRoot, 'plugins', 'session-notes'),
    'session-notes',
  );
  await expect(row).toHaveAttribute('data-status', 'running');
  // The settings section rendered, so the module was fetched from the
  // bridge and activated.
  await row.locator('#session-notes-show-badge').uncheck();
  const cfg = path.join(
    TMP,
    'state',
    'plugin-data',
    'session-notes',
    'ui-config.json',
  );
  await expect
    .poll(() =>
      fs.existsSync(cfg) ? JSON.parse(fs.readFileSync(cfg, 'utf8')) : null,
    )
    .toMatchObject({ showBadge: false });

  // A note, through the palette and the session view.
  await page.keyboard.press('Escape');
  await page.keyboard.press(`${mod}+Shift+K`);
  await page.locator('#command-palette-input').fill('Edit note');
  await page.keyboard.press('Enter');
  await page.locator('#session-notes-text').fill('real daemon note');
  await page.locator('#session-notes-save').click();
  await expect(page.locator('.hv-plugin-banner')).toContainText(
    'Note: real daemon note',
  );
  await expect
    .poll(() =>
      Object.values(JSON.parse(fs.readFileSync(cfg, 'utf8')).notes ?? {}),
    )
    .toContain('real daemon note');

  await page.keyboard.press(`${mod}+,`);
  await page.locator('#settings-tab-plugins').click();
  await remove(page, 'session-notes');
});

test('a UI plugin that throws is contained and can be disabled', async ({
  page,
}) => {
  const dir = path.join(
    repoRoot,
    'cmd',
    'hivegui',
    'frontend',
    'test',
    'fixtures',
    'plugins',
    'ui-throws',
  );
  const row = await install(page, dir, 'ui-throws');
  await expect(row).toHaveAttribute('data-status', 'failed', {
    timeout: 15000,
  });
  await expect(row.locator('.settings-plugin-error')).toContainText(
    'render boom',
  );
  // The rest of the app: the sidebar still lists sessions and Settings
  // still works.
  await page.keyboard.press('Escape');
  await expect(page.locator('#projects li').first()).toBeVisible();
  await page.keyboard.press(`${mod}+,`);
  await page.locator('#settings-tab-plugins').click();
  await row.locator('.settings-plugin-disable').click();
  await expect(row).toHaveAttribute('data-status', 'disabled');
  await remove(page, 'ui-throws');
});

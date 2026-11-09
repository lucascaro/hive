// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

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

// Every install lists the bundled plan-review plugin (spec 471), so the
// list is never empty; these count the plugins a test adds beside it.
const added = (page: Page) =>
  page.locator('.settings-plugin-row:not([data-plugin-id="plan-review"])');

test('install, enable, disable and remove a plugin', async ({ page }) => {
  await openPluginsTab(page);
  await expect(added(page)).toHaveCount(0);

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
  await expect(added(page)).toHaveCount(0);
});

test('restart a failed plugin', async ({ page }) => {
  await openPluginsTab(page);
  await page.locator('#settings-plugin-source').fill('/plugins/crashy');
  await page.locator('#settings-plugin-install').click();
  const row = page.locator('.settings-plugin-row[data-plugin-id="crashy"]');
  await expect(row).toHaveAttribute('data-status', 'failed');
  await expect(row).toContainText('stopped after 6 exits');
  await expect(page.locator('#status-text')).toContainText(
    'Plugin Crashy stopped after repeated crashes',
  );
  // Real layout: the button sits inside the row, beside the checkbox,
  // and is the topmost element at its centre (nothing overlaps it).
  const btn = row.locator('.settings-plugin-restart');
  const [rb, bb, cb] = await Promise.all([
    row.boundingBox(),
    btn.boundingBox(),
    row.locator('.settings-plugin-enabled').boundingBox(),
  ]);
  expect(bb && rb && bb.x >= rb.x && bb.x + bb.width <= rb.x + rb.width).toBe(
    true,
  );
  expect(bb && cb && bb.x + bb.width <= cb.x).toBe(true);
  expect(
    await btn.evaluate((el) => {
      const r = el.getBoundingClientRect();
      return (
        document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2) ===
          el ||
        el.contains(
          document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2),
        )
      );
    }),
  ).toBe(true);
  await btn.click();
  await expect(row).toHaveAttribute('data-status', 'running');
  await expect(row.locator('.settings-plugin-restart')).toHaveCount(0);
});

test('a failed install shows its error and adds nothing', async ({ page }) => {
  await openPluginsTab(page);
  await page.locator('#settings-plugin-source').fill('/plugins/fail');
  await page.locator('#settings-plugin-source').press('Enter');
  await expect(page.locator('#settings-error')).toContainText(
    'plugin install failed: no hive-plugin.json',
  );
  await expect(added(page)).toHaveCount(0);
  // The generic control-error status line stands aside for it.
  await expect(page.locator('#status-text')).not.toContainText(
    'plugin_install_failed',
  );
});

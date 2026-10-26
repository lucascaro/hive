// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { test, expect, type Page } from '@playwright/test';
import { dragEvent, dragStart } from './fixtures/drag.js';

// E2E for the ad-hoc grid (spec 533): ⌘/Ctrl-click sidebar sessions to
// tile just those. Runs against the Wails mock bridge; there is no daemon
// or wire change, so no e2e-real spec.

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';
const row = (sid: string) => `li.hv-session-row[data-sid="${sid}"]`;

// Returns the active session first, then the rest in sidebar order: the
// mock boots on its own session, which is not necessarily the first row.
async function boot(page: Page, count: number): Promise<string[]> {
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
  const rows = await page.$$eval('#projects li.hv-session-row', (els) =>
    els.map((el) => (el as HTMLElement).dataset.sid ?? ''),
  );
  const active = await page.evaluate(() => window.__hive_state?.activeId ?? '');
  return [active, ...rows.filter((id) => id !== active)];
}

const modClick = (page: Page, sid: string) =>
  page.locator(row(sid)).click({ modifiers: [MOD] });

// Sorted: tiles paint in sidebar order, which these tests do not pin.
const tileIds = async (page: Page) =>
  (
    await page.$$eval('.term-host.in-grid', (els) =>
      els.map((el) => (el as HTMLElement).dataset.sid ?? ''),
    )
  ).sort();

test.describe('ad-hoc grid', () => {
  test('modifier-click tiles exactly the selected sessions', async ({
    page,
  }) => {
    const ids = await boot(page, 4);
    // ids[0] is the active session, so clicking ids[1] seeds {ids[0], ids[1]}.
    await modClick(page, ids[1]);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    expect(await tileIds(page)).toEqual([ids[0], ids[1]].sort());

    await modClick(page, ids[3]);
    await expect(page.locator('.term-host.in-grid')).toHaveCount(3);

    await modClick(page, ids[1]);
    await expect(page.locator('.term-host.in-grid')).toHaveCount(2);
    expect(await tileIds(page)).toEqual([ids[0], ids[3]].sort());

    // The selected, non-active row is marked.
    await expect(page.locator(row(ids[3]))).toHaveAttribute(
      'data-in-selection',
      '',
    );
    await expect(page.locator(row(ids[2]))).not.toHaveAttribute(
      'data-in-selection',
      '',
    );
  });

  test('dropping below two tiles returns to single view', async ({ page }) => {
    const ids = await boot(page, 3);
    await modClick(page, ids[1]);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await modClick(page, ids[1]);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });

  test('a plain click leaves the grid and clears the selection', async ({
    page,
  }) => {
    const ids = await boot(page, 3);
    await modClick(page, ids[1]);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await page.locator(row(ids[2])).click();
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    await expect(page.locator('[data-in-selection]')).toHaveCount(0);
  });

  test('a modifier-click starts no drag; a drag leaves the selection alone', async ({
    page,
  }) => {
    const ids = await boot(page, 4);
    await modClick(page, ids[1]);
    await expect(page.locator('.hv-session-row.dragging')).toHaveCount(0);

    const before = await tileIds(page);
    await dragStart(page, row(ids[0]));
    await dragEvent(page, 'dragover', row(ids[2]), true);
    await dragEvent(page, 'drop', row(ids[2]), true);
    await expect(page.locator('.hv-drop-placeholder')).toHaveCount(0);
    expect(
      await page.evaluate(() => window.__hive_state?.gridSelection),
    ).toEqual(expect.arrayContaining([ids[0], ids[1]]));
    expect(await tileIds(page)).toEqual(before);
  });

  test('⌘G and ⇧⌘G keep their meaning', async ({ page }) => {
    await boot(page, 3);
    await page.keyboard.press(`${MOD}+Shift+g`);
    await expect(page.locator('.term-host.in-grid')).toHaveCount(3);
    await page.keyboard.press(`${MOD}+Shift+g`);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });

  test('select-then-⌘G: the setting only marks rows until ⌘G opens them', async ({
    page,
  }) => {
    const ids = await boot(page, 4);
    await page.keyboard.press(`${MOD}+,`);
    await page.locator('#settings-tab-appearance').click();
    const auto = page.locator('#settings-grid-select-auto');
    await expect(auto).toBeChecked(); // default: open as soon as selected
    await auto.uncheck();
    await page.keyboard.press('Escape');
    await expect(auto).toBeHidden();

    await modClick(page, ids[1]);
    await modClick(page, ids[3]);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    // All three marked rows, the active one included.
    await expect(page.locator('[data-in-selection]')).toHaveCount(3);

    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    expect(await tileIds(page)).toEqual([ids[0], ids[1], ids[3]].sort());
  });
});

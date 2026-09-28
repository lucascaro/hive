import { test, expect } from '@playwright/test';
import {
  boot,
  closeSettings,
  installPlugin,
  MOD,
  openPluginsTab,
} from './plugin-helpers.js';

// Spec 471 criterion 4: the example plugin in plugins/session-notes,
// installed like any folder plugin, exercises every app surface a plugin
// can add — session view (modal, panel, banner), a palette command with
// a key, a sidebar badge and a settings section.

const MAC = process.platform === 'darwin';
const TOGGLE = MAC ? '⇧⌘O' : 'Ctrl+Shift+O';

test('session-notes uses all four surfaces', async ({ page }) => {
  await boot(page);
  const row = await installPlugin(page, 'session-notes');
  await expect(row).toHaveAttribute('data-status', 'running');
  // Settings section, under the plugin's own row.
  await expect(row.locator('#session-notes-show-badge')).toBeChecked();
  await closeSettings(page);

  // Palette command → the session view modal.
  await page.keyboard.press(`${MOD}+Shift+K`);
  await page.locator('#command-palette-input').fill('Edit note');
  await expect(page.locator('#command-palette')).toContainText(
    'Edit note for this session',
  );
  await page.keyboard.press('Enter');
  const view = page.locator('#plugin-view');
  await expect(view).toBeVisible();
  await expect(view.locator('#plugin-view-title')).toContainText(
    'Session note',
  );
  await view.locator('#session-notes-text').fill('ship the plugin API');
  await view.locator('#session-notes-save').click();
  await expect(view).toBeHidden();

  // Sidebar badge and session banner.
  await expect(page.locator('#projects .hv-plugin-badge').first()).toHaveText(
    'Note',
  );
  await expect(page.locator('.hv-plugin-banner')).toContainText(
    'Note: ship the plugin API',
  );

  // The key shown in the palette works: toggle the notes panel.
  await page.keyboard.press(`${MOD}+Shift+K`);
  await page.locator('#command-palette-input').fill('notes panel');
  await expect(page.locator('#command-palette')).toContainText(TOGGLE);
  await page.keyboard.press('Escape');
  await page.keyboard.press(`${MOD}+Shift+O`);
  const panel = page.locator('#plugin-panel');
  await expect(panel).toBeVisible();
  await expect(panel).toContainText('ship the plugin API');
  await page.keyboard.press(`${MOD}+Shift+O`);
  await expect(panel).toBeHidden();

  // ⌘/ lists it under Plugins.
  await page.keyboard.press(`${MOD}+/`);
  const plugins = page
    .locator('#help-overlay-groups section')
    .filter({ has: page.locator('h4', { hasText: 'Plugins' }) });
  await expect(plugins).toContainText(TOGGLE);
  await expect(plugins).toContainText('Toggle session notes panel');
  await page.keyboard.press('Escape');

  // The settings section drives the badge; Enter in its own field is
  // the plugin's, not Settings' save-and-close.
  await openPluginsTab(page);
  const r = page.locator(
    '.settings-plugin-row[data-plugin-id="session-notes"]',
  );
  await r.locator('#session-notes-show-badge').uncheck();
  await r.locator('#session-notes-template').fill('TODO: ');
  await r.locator('#session-notes-template').press('Enter');
  await expect(page.locator('#settings')).toBeVisible();
  await closeSettings(page);
  await expect(page.locator('#projects .hv-plugin-badge')).toHaveCount(0);
});

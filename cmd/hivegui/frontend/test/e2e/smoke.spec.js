import { test, expect } from '@playwright/test';

// Smoke E2E: load the GUI with the Wails-mock bridge, verify the
// sidebar renders the bootstrap project + session, then add a session
// via the mock and verify the sidebar updates.
test('boots, shows project & session, then reflects a new session', async ({ page }) => {
  await page.goto('/');

  // Wait for the daemon-mock to push the initial snapshot.
  await page.waitForFunction(() => {
    const ul = document.getElementById('projects');
    return ul && ul.querySelectorAll('li').length > 0;
  });

  const sidebar = page.locator('#projects');
  await expect(sidebar.locator('li[data-pid="p1"]').first()).toBeVisible();

  // Drive the mock: add a session, ensure the sidebar picks it up.
  await page.evaluate(() => window.__hive.addSession('via-mock'));

  await expect(
    page.locator('#projects').getByText('via-mock').first(),
  ).toBeVisible({ timeout: 3000 });
});

// Switch into grid mode and verify the .grid class is applied to the
// terms host. Covers the grid-mode-revert regression class.
test('toggles grid view', async ({ page }) => {
  await page.goto('/');
  await page.waitForFunction(() => document.querySelectorAll('#projects li').length > 0);

  // Add a second session so the grid is non-trivial.
  await page.evaluate(() => window.__hive.addSession('two'));
  await page.waitForFunction(() => window.__hive.state.sessions.length >= 2);

  // ⌘G / Ctrl+G toggles grid mode.
  const mod = process.platform === 'darwin' ? 'Meta' : 'Control';
  await page.keyboard.press(`${mod}+g`);

  await expect(page.locator('#terms')).toHaveClass(/grid/);
});

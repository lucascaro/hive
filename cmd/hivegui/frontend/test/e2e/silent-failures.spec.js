import { test, expect } from '@playwright/test';

// E2E for the silent-failure surfacing pass: when a daemon call fails,
// the status bar must show a user-visible error instead of the action
// silently doing nothing. Failures are injected one-shot via the
// Wails-mock's window.__hive.failNext(method, message).

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page) {
  await page.goto('/');
  await page.waitForFunction(() => document.querySelectorAll('#projects li').length > 0);
}

test('boot lands a persistent non-error status', async ({ page }) => {
  await boot(page);
  // After connect, setActive overwrites "connected" with the active
  // session's name — the persistent slot ends at "main".
  await expect(page.locator('#status')).toHaveText('main');
  await expect(page.locator('#status')).not.toHaveClass(/error/);
});

test('failed CreateSession via launcher shows an error and adds no session', async ({ page }) => {
  await boot(page);
  const before = await page.evaluate(() => window.__hive.state.sessions.length);
  await page.evaluate(() => window.__hive.failNext('CreateSession', 'daemon went away'));

  // ⌘T opens the launcher; Enter activates the selected agent row.
  await page.keyboard.press(`${mod}+t`);
  await expect(page.locator('#launcher')).toBeVisible();
  await expect(page.locator('#launcher .launcher-item').first()).toBeVisible();
  await page.keyboard.press('Enter');

  const status = page.locator('#status');
  await expect(status).toHaveClass(/error/);
  await expect(status).toHaveText(/new session failed:.*daemon went away/);
  expect(await page.evaluate(() => window.__hive.state.sessions.length)).toBe(before);
});

test('failed KillSession shows an error and the session survives', async ({ page }) => {
  await boot(page);
  await page.evaluate(() => window.__hive.failNext('KillSession', 'boom'));

  await page.keyboard.press(`${mod}+w`);

  const status = page.locator('#status');
  await expect(status).toHaveClass(/error/);
  await expect(status).toHaveText(/close failed:.*boom/);
  // The bootstrap session is still listed.
  await expect(page.locator('#projects li[data-sid="s1"]')).toBeVisible();
});

test('failed rename shows an error', async ({ page }) => {
  await boot(page);
  await page.evaluate(() => window.__hive.failNext('UpdateSession', 'no daemon'));

  const row = page.locator('#projects li[data-sid="s1"] .name');
  await row.dblclick();
  const input = page.locator('#projects li[data-sid="s1"] input.name-input');
  await input.fill('renamed');
  await input.press('Enter');

  const status = page.locator('#status');
  await expect(status).toHaveClass(/error/);
  await expect(status).toHaveText(/rename failed:.*no daemon/);
});

test('error flash auto-reverts to the persistent status', async ({ page }) => {
  await boot(page);
  await page.evaluate(() => window.__hive.failNext('KillSession', 'transient'));
  await page.keyboard.press(`${mod}+w`);
  await expect(page.locator('#status')).toHaveText(/close failed/);
  // FLASH_ERROR_MS is 6s — after expiry the persistent slot (the
  // active session's name) returns.
  await expect(page.locator('#status')).toHaveText('main', { timeout: 8000 });
  await expect(page.locator('#status')).not.toHaveClass(/error/);
});

test('Enter commits a rename, Escape cancels it', async ({ page }) => {
  // Locks the fix for the dead bubble-phase rename listener: a capture
  // listener's stopPropagation() used to cancel the same input's
  // bubble-phase Enter/Escape handler, so Enter did nothing and Escape
  // could not cancel (the later blur committed anyway).
  await boot(page);
  const row = page.locator('#projects li[data-sid="s1"] .name');
  await row.dblclick();
  let input = page.locator('#projects li[data-sid="s1"] input.name-input');
  await input.fill('kept');
  await input.press('Enter');
  await expect(page.locator('#projects li[data-sid="s1"] .name')).toHaveText('kept');

  await page.locator('#projects li[data-sid="s1"] .name').dblclick();
  input = page.locator('#projects li[data-sid="s1"] input.name-input');
  await input.fill('discarded');
  await input.press('Escape');
  await expect(page.locator('#projects li[data-sid="s1"] .name')).toHaveText('kept');
});

test('launcher selects an agent and creates a session on the happy path', async ({ page }) => {
  await boot(page);
  const before = await page.evaluate(() => window.__hive.state.sessions.length);
  await page.keyboard.press(`${mod}+t`);
  await expect(page.locator('#launcher .launcher-item').first()).toContainText('Shell');
  await page.keyboard.press('Enter');
  await page.waitForFunction(
    (n) => window.__hive.state.sessions.length === n + 1,
    before,
  );
  await expect(page.locator('#launcher')).toBeHidden();
});

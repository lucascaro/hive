import { test, expect } from '@playwright/test';

// E2E for the new-session popup's filter box against the Wails mock.
// The dom suite covers the branching; this one proves the whole path
// works through the real index.html and the real key routing —
// ⌘T opens with the box focused, typing narrows the list, and Enter
// creates a session with the agent that survived the filter.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

test('typing in the launcher narrows the agent list and Enter creates that session', async ({
  page,
}) => {
  await boot(page);
  const before = await page.evaluate(() => window.__hive.state.sessions.length);

  await page.keyboard.press(`${mod}+t`);
  const launcher = page.locator('#launcher');
  await expect(launcher).toBeVisible();
  await expect(launcher.locator('.launcher-item')).toHaveCount(2);
  // The filter box takes focus on open, so the keystrokes below need
  // no explicit click.
  await expect(launcher.locator('.launcher-search')).toBeFocused();

  await page.keyboard.type('cla');
  await expect(launcher.locator('.launcher-item')).toHaveCount(1);
  await expect(launcher.locator('.launcher-item')).toContainText('Claude');

  await page.keyboard.press('Enter');
  await page.waitForFunction(
    (n) => window.__hive.state.sessions.length === n + 1,
    before,
  );
  await expect(launcher).toBeHidden();
});

test('a digit selects a row only while the filter box is empty', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+t`);
  const launcher = page.locator('#launcher');
  await expect(launcher.locator('.launcher-item')).toHaveCount(2);

  // Non-empty query: the digit is a character, not a shortcut, so the
  // launcher stays open and the query grows.
  await page.keyboard.type('cla');
  await page.keyboard.press('2');
  await expect(launcher).toBeVisible();
  await expect(launcher.locator('.launcher-search')).toHaveValue('cla2');

  // Empty query: the digit launches. Clear via the keyboard the way a
  // user would.
  for (let i = 0; i < 4; i++) await page.keyboard.press('Backspace');
  await expect(launcher.locator('.launcher-search')).toHaveValue('');
  const before = await page.evaluate(() => window.__hive.state.sessions.length);
  await page.keyboard.press('2');
  await page.waitForFunction(
    (n) => window.__hive.state.sessions.length === n + 1,
    before,
  );
  await expect(launcher).toBeHidden();
});

// Focus restore after the launcher closes. This one MUST live in the
// e2e layer: the bug only exists because hiding an element does not
// synchronously clear document.activeElement in a real engine, and
// jsdom has no layout, so the dom suite's Escape test passes either way.
test('closing the launcher restores terminal focus even from the worktree checkbox', async ({
  page,
}) => {
  await boot(page);
  const helperFocused = () =>
    page.evaluate(() =>
      document.activeElement?.classList?.contains('xterm-helper-textarea'),
    );

  await page.keyboard.press(`${mod}+t`);
  await expect(page.locator('#launcher .launcher-search')).toBeFocused();
  // The mousedown handler deliberately exempts the worktree row so its
  // checkbox stays clickable — which means focus really does land on
  // the checkbox, not the filter box.
  await page.locator('#launcher .launcher-worktree input').click();
  await expect(
    page.locator('#launcher .launcher-worktree input'),
  ).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(page.locator('#launcher')).toBeHidden();
  await expect.poll(helperFocused).toBe(true);
});

test('a query that matches nothing shows the empty row and Enter does nothing', async ({
  page,
}) => {
  await boot(page);
  const before = await page.evaluate(() => window.__hive.state.sessions.length);

  await page.keyboard.press(`${mod}+t`);
  await page.keyboard.type('zzz');
  const launcher = page.locator('#launcher');
  await expect(launcher.locator('.launcher-item')).toHaveCount(0);
  await expect(launcher.locator('.launcher-empty')).toHaveText(
    'No agents match',
  );

  await page.keyboard.press('Enter');
  await expect(launcher).toBeVisible();
  expect(await page.evaluate(() => window.__hive.state.sessions.length)).toBe(
    before,
  );
});

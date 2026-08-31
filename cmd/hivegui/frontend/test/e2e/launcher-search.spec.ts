import { test, expect, type Page } from '@playwright/test';

// E2E for the new-session popup's filter box against the Wails mock.
// The dom suite covers the branching; this one proves the whole path
// works through the real index.html and the real key routing —
// ⌘T opens with the box focused, typing narrows the list, and Enter
// creates a session with the agent that survived the filter.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

test('typing in the launcher narrows the agent list and Enter creates that session', async ({
  page,
}) => {
  await boot(page);
  const before = await page.evaluate(
    () => window.__hive.state?.sessions.length ?? 0,
  );

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
    (n) => (window.__hive.state?.sessions.length ?? 0) === n + 1,
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
  const before = await page.evaluate(
    () => window.__hive.state?.sessions.length ?? 0,
  );
  await page.keyboard.press('2');
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) === n + 1,
    before,
  );
  await expect(launcher).toBeHidden();
});

// Focus restore after the launcher closes. This one MUST live in the
// e2e layer: the bug only exists because hiding an element does not
// synchronously clear document.activeElement in a real engine, and
// jsdom has no layout, so the dom suite's Escape test passes either way.
test('the worktree checkbox toggles without stealing focus from the filter box', async ({
  page,
}) => {
  await boot(page);
  const helperFocused = () =>
    page.evaluate(() =>
      document.activeElement?.classList?.contains('xterm-helper-textarea'),
    );

  await page.keyboard.press(`${mod}+t`);
  const search = page.locator('#launcher .launcher-search');
  const box = page.locator('#launcher .launcher-worktree input');
  await expect(search).toBeFocused();
  const wasChecked = await box.isChecked();

  // mousedown preventDefault blocks the focus shift but not the click
  // activation, so the box still toggles...
  await box.click();
  await expect(box).toBeChecked({ checked: !wasChecked });
  // ...and the filter box still owns the keyboard, so search-as-you-type
  // keeps working after touching the checkbox.
  await expect(search).toBeFocused();
  await page.keyboard.type('cla');
  await expect(search).toHaveValue('cla');
  await expect(page.locator('#launcher .launcher-item')).toHaveCount(1);

  // And closing still hands the keyboard back to the terminal.
  await page.keyboard.press('Escape');
  await expect(page.locator('#launcher')).toBeHidden();
  await expect.poll(helperFocused).toBe(true);
});

// keyboard.ts bails out unconditionally while the launcher is open and
// the launcher's own listener is scoped to #launcher, so any click that
// leaves the launcher visible while moving focus out of it would strand
// the keyboard with no handler at all — Escape included.
test('clicking a project action other than + closes the launcher', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+t`);
  await expect(page.locator('#launcher')).toBeVisible();

  const card = page.locator('#projects .hv-project-card').first();
  await card.locator('.hv-project-card__header').hover();
  await card.locator('[data-action="edit"]').click();
  await expect(page.locator('#launcher')).toBeHidden();
});

test('a query that matches nothing shows the empty row and Enter does nothing', async ({
  page,
}) => {
  await boot(page);
  const before = await page.evaluate(
    () => window.__hive.state?.sessions.length ?? 0,
  );

  await page.keyboard.press(`${mod}+t`);
  await page.keyboard.type('zzz');
  const launcher = page.locator('#launcher');
  await expect(launcher.locator('.launcher-item')).toHaveCount(0);
  await expect(launcher.locator('.launcher-empty')).toHaveText(
    'No agents match',
  );

  await page.keyboard.press('Enter');
  await expect(launcher).toBeVisible();
  expect(
    await page.evaluate(() => window.__hive.state?.sessions.length ?? 0),
  ).toBe(before);
});

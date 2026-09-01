import { test, expect, type Page } from '@playwright/test';

// E2E for undo-close against the Wails mock. The DOM suite already
// covers the banner's copy in isolation; what only a real click path
// can prove is the wiring — that ⌘W actually routes through
// closeActiveSession (and therefore notes the close), that the banner
// mounts into the live layout, and that ⌘Z reaches the daemon call.
//
// The ⌘Z leg matters most: ⇧⌘T was the obvious binding and turned out
// to be New Session in Worktree. A spec that presses the chord and
// asserts a session came back is what stops that from recurring.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

// Closing the last session leaves document.activeElement on <body>,
// and Chromium then delivers no keydown at all for a ⌘-chord. A real
// window always has something focused; clicking the app restores that
// precondition so the chord under test is actually dispatched.
async function focusApp(page: Page) {
  await page.locator('#app').click({ position: { x: 4, y: 4 } });
}

const banner = (page: Page) => page.locator('[data-slot="undo-close"]');

test('closing a session offers an undo banner', async ({ page }) => {
  await boot(page);
  await expect(banner(page)).toBeHidden();

  const name = await page.evaluate(
    () => window.__hive.state?.sessions[0]?.name ?? '',
  );
  await page.keyboard.press(`${mod}+w`);

  await expect(banner(page)).toBeVisible();
  await expect(banner(page)).toContainText(name);
  await expect(banner(page).locator('[data-action-id="undo"]')).toHaveText(
    'Undo',
  );
});

test('the undo button brings the session back', async ({ page }) => {
  await boot(page);
  const before = await page.evaluate(
    () => window.__hive.state?.sessions.length ?? 0,
  );

  await page.keyboard.press(`${mod}+w`);
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) === n - 1,
    before,
  );

  await banner(page).locator('[data-action-id="undo"]').click();

  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) === n,
    before,
  );
  // The banner switches from an offer to a report of what was lost —
  // it must not simply vanish and imply a clean undo.
  await expect(banner(page)).toContainText('Scrollback is gone');
});

test('⌘Z reopens the last closed session', async ({ page }) => {
  await boot(page);
  const before = await page.evaluate(
    () => window.__hive.state?.sessions.length ?? 0,
  );

  await page.keyboard.press(`${mod}+w`);
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) === n - 1,
    before,
  );

  await focusApp(page);
  await page.keyboard.press(`${mod}+z`);

  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) === n,
    before,
  );
});

test('⌘Z does not open a new session in a worktree', async ({ page }) => {
  // The regression this whole binding audit came out of: ⇧⌘T was the
  // first choice for reopen and is already New Session in Worktree.
  // With nothing closed, ⌘Z must do nothing at all — certainly not
  // create anything.
  await boot(page);
  const before = await page.evaluate(
    () => window.__hive.state?.sessions.length ?? 0,
  );

  await page.keyboard.press(`${mod}+z`);
  await page.waitForTimeout(200);

  expect(
    await page.evaluate(() => window.__hive.state?.sessions.length ?? 0),
  ).toBe(before);
});

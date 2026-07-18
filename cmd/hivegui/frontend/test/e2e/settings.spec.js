import { test, expect } from '@playwright/test';

// E2E for the settings modal and custom agents: the ⌘, binding, the
// native menu:settings event, and the round-trip that puts a custom
// agent into the ⌘T launcher. These drive the real index.html markup
// and keyboard wiring, which the jsdom unit spec (test/dom/settings)
// stubs out.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page) {
  await page.goto('/');
  await page.waitForFunction(() => document.querySelectorAll('#projects li').length > 0);
}

async function addAgent(page, name, cmd) {
  await page.locator('#settings-agent-add').click();
  const row = page.locator('.settings-agent-row').last();
  await row.locator('.settings-agent-name').fill(name);
  await row.locator('.settings-agent-cmd').fill(cmd);
}

test('⌘, opens settings, Esc closes it, typing reaches the terminal again', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  await expect(page.locator('#settings')).toContainText('Custom agents');

  await page.keyboard.press('Escape');
  await expect(page.locator('#settings')).toBeHidden();

  // Wait for focus to actually land before typing. toBeHidden() only
  // proves the class flipped, which is synchronous — but closeSettings
  // restores focus through setFocusedTile, which defers the real
  // focus() into a requestAnimationFrame with a retry chain
  // (src/app/focus.js). Typing in that gap sends the keys nowhere and
  // the assertion below fails with an empty stdin, which is what this
  // test did once on a loaded macOS CI runner.
  await expect(page.getByRole('textbox', { name: 'Terminal input' })).toBeFocused();

  // Focus is back on the terminal: typed keys land in stdin.
  await page.evaluate(() => window.__hive.resetStdin());
  await page.keyboard.type('hi');
  await expect.poll(() => page.evaluate(() => window.__hive.stdinText())).toContain('hi');
});

test('settings owns the keyboard while open', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();

  // ⌘G would flip to grid if the shortcut leaked past the modal.
  await page.keyboard.press(`${mod}+g`);
  await expect(page.locator('#terms')).not.toHaveClass(/grid/);

  // Typing into a field must not reach the terminal behind the backdrop.
  await page.evaluate(() => window.__hive.resetStdin());
  await page.locator('#settings-agent-add').click();
  await page.keyboard.type('typed');
  expect(await page.evaluate(() => window.__hive.stdinText())).not.toContain('typed');
});

test('Tab stays inside the dialog but still walks the form fields', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'Tabbed', 'tabbed');

  // Tab must move between the row's own inputs, not pin to one element
  // the way the single-control help overlay does.
  await page.locator('.settings-agent-name').focus();
  await page.keyboard.press('Tab');
  await expect.poll(() => page.evaluate(() => document.activeElement?.className))
    .toContain('settings-agent-cmd');

  // aria-modal promises focus never leaves: tabbing off the last
  // control wraps to the first instead of reaching the terminal.
  for (let i = 0; i < 12; i++) await page.keyboard.press('Tab');
  await expect.poll(() => page.evaluate(() => document.getElementById('settings').contains(document.activeElement)))
    .toBe(true);
});

test('re-opening settings does not wipe an in-progress draft', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'Half Typed', 'halftyped --flag');

  // On macOS the native File ▸ Settings… accelerator wins ⌘, over the
  // webview, so this arrives as menu:settings with the modal already
  // open — the path that used to hit `draft = []` and discard the row.
  await page.evaluate(() => window.__hive.emit('menu:settings'));
  await expect(page.locator('#settings')).toBeVisible();

  const row = page.locator('.settings-agent-row').first();
  await expect(row.locator('.settings-agent-name')).toHaveValue('Half Typed');
  await expect(row.locator('.settings-agent-cmd')).toHaveValue('halftyped --flag');
});

test('native menu event opens settings', async ({ page }) => {
  await boot(page);
  // On macOS the menu accelerator intercepts ⌘, before the webview's
  // keydown listener ever sees it, so this path must work on its own.
  await page.evaluate(() => window.__hive.emit('menu:settings'));
  await expect(page.locator('#settings')).toBeVisible();
});

test('settings is reachable from the command palette', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+Shift+k`);
  await page.locator('#command-palette-input').fill('settings');
  await page.keyboard.press('Enter');
  await expect(page.locator('#settings')).toBeVisible();
});

test('a saved custom agent shows up in the launcher', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('#settings')).toBeVisible();

  await addAgent(page, 'Claude Lite', 'claude --model haiku');
  await page.locator('#settings-save').click();
  await expect(page.locator('#settings')).toBeHidden();

  // The launcher renders whatever ListAgents returns, so a custom
  // agent needs no launcher-specific code to appear.
  await page.keyboard.press(`${mod}+t`);
  await expect(page.locator('#launcher')).toContainText('Claude Lite');
});

test('custom agents round-trip: reopening settings shows the saved agent', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'My Tool', 'mytool --fast');
  await page.locator('#settings-save').click();
  await expect(page.locator('#settings')).toBeHidden();

  await page.keyboard.press(`${mod}+,`);
  const row = page.locator('.settings-agent-row').first();
  await expect(row.locator('.settings-agent-name')).toHaveValue('My Tool');
  await expect(row.locator('.settings-agent-cmd')).toHaveValue('mytool --fast');
});

test('deleting a custom agent removes it', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'Doomed', 'doomed');
  await page.locator('#settings-save').click();
  await expect(page.locator('#settings')).toBeHidden();

  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('.settings-agent-row')).toHaveCount(1);
  await page.locator('.settings-agent-delete').first().click();
  await page.locator('#settings-save').click();
  await expect(page.locator('#settings')).toBeHidden();

  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('.settings-agent-row')).toHaveCount(0);
  await expect(page.locator('#settings-agents-list')).toContainText('No custom agents yet');
});

test('cancel discards edits', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+,`);
  await addAgent(page, 'Ghost', 'ghost');
  await page.locator('#settings-cancel').click();
  await expect(page.locator('#settings')).toBeHidden();

  await page.keyboard.press(`${mod}+,`);
  await expect(page.locator('.settings-agent-row')).toHaveCount(0);
});

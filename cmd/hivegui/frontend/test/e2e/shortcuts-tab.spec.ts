import { expect, type Page, test } from '@playwright/test';

// Spec 477 (phase 2): Settings › Shortcuts against the real keyboard
// pipeline. A key pressed into a capture button is recorded, not run —
// even the chord that closes Settings — and after Save the new key runs
// the command, the old one does not, and the palette shows the new one.

const mac = process.platform === 'darwin';
const mod = mac ? 'Meta' : 'Control';
const NEW = mac ? '⌘Y' : 'Ctrl+Y';
const OLD = mac ? '⌘T' : 'Ctrl+T';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForSelector('#help-btn');
}

const row = (page: Page, command: string) =>
  page.locator(`.hv-shortcut-row[data-command="${command}"]`);
const keys = (page: Page, command: string) =>
  row(page, command).locator('.hv-shortcut-key kbd');
const calls = (page: Page, method: string) =>
  page.evaluate(
    (m) =>
      (
        window as unknown as {
          __hive: { bridgeCalls?: (m: string) => { args: unknown[] }[] };
        }
      ).__hive.bridgeCalls?.(m) ?? [],
    method,
  );

async function openShortcuts(page: Page) {
  await page.keyboard.press(`${mod}+Comma`);
  await expect(page.locator('#settings')).toBeVisible();
  await page.locator('#settings-tab-shortcuts').click();
}

test('rebinding a command in the tab moves its key everywhere', async ({
  page,
}) => {
  await boot(page);
  await openShortcuts(page);

  // The chord that would close Settings is captured instead — and since
  // Settings holds it, it is a conflict that blocks Save until answered.
  await row(page, 'new-session')
    .locator('[data-action="add-shortcut"]')
    .click();
  await page.keyboard.press(`${mod}+Comma`);
  await expect(page.locator('#settings')).toBeVisible();
  await expect(row(page, 'new-session')).toHaveAttribute(
    'data-conflict',
    'source',
  );
  await expect(row(page, 'settings')).toHaveAttribute(
    'data-conflict',
    'holder',
  );
  await expect(page.locator('#settings-save')).toBeDisabled();
  // Focus waits on the answer, and the footer says why Save is off —
  // laid out beside the buttons, not over them.
  await expect(
    row(page, 'new-session').locator('[data-action="reassign"]'),
  ).toBeFocused();
  const blocked = page.locator('#settings-save-blocked');
  await expect(blocked).toBeVisible();
  const [note, save] = await Promise.all([
    blocked.boundingBox(),
    page.locator('#settings-save').boundingBox(),
  ]);
  expect(note && save).toBeTruthy();
  if (note && save) {
    const overlaps =
      note.x < save.x + save.width &&
      save.x < note.x + note.width &&
      note.y < save.y + save.height &&
      save.y < note.y + note.height;
    expect(overlaps).toBe(false);
  }
  await row(page, 'new-session')
    .locator('[data-action="cancel-reassign"]')
    .click();
  await expect(
    row(page, 'new-session').locator('[data-action="add-shortcut"]'),
  ).toBeFocused();
  await expect(page.locator('#settings-save-blocked')).toHaveCount(0);
  await expect(page.locator('#settings-save')).toBeEnabled();

  await row(page, 'new-session')
    .locator('[data-action="add-shortcut"]')
    .click();
  await page.keyboard.press(`${mod}+y`);
  await expect(keys(page, 'new-session')).toHaveText([OLD, NEW]);
  // Typing the key did not run it: no launcher behind the dialog.
  await expect(page.locator('#launcher')).toBeHidden();
  await row(page, 'new-session')
    .locator('[data-action="remove-shortcut"]')
    .first()
    .click();
  await expect(keys(page, 'new-session')).toHaveText([NEW]);

  await page.locator('#settings-save').click();
  await expect(page.locator('#settings')).toBeHidden();
  expect(await calls(page, 'SaveKeymap')).toHaveLength(1);
  if (mac) {
    // The menu was stripped for each capture and given back after.
    expect(
      (await calls(page, 'SuspendMenuAccelerators')).map((c) => c.args[0]),
    ).toEqual([true, false, true, false]);
  }

  await page.keyboard.press(`${mod}+t`);
  await expect(page.locator('#launcher')).toBeHidden();
  await page.keyboard.press(`${mod}+y`);
  await expect(page.locator('#launcher')).toBeVisible();
  await page.keyboard.press('Escape');

  await page.keyboard.press(`${mod}+Shift+k`);
  await page.locator('#command-palette-input').fill('new session');
  const palette = page.locator('#command-palette');
  await expect(palette).toContainText(NEW);
  await expect(
    palette.locator('.hv-kbd', { hasText: new RegExp(`^${OLD}$`) }),
  ).toHaveCount(0);
});

test('Escape in a capture button cancels the capture, not Settings', async ({
  page,
}) => {
  await boot(page);
  await openShortcuts(page);
  await row(page, 'worktrees').locator('[data-action="add-shortcut"]').click();
  await expect(
    row(page, 'worktrees').locator('.hv-shortcut-capture'),
  ).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(page.locator('#settings')).toBeVisible();
  await expect(
    row(page, 'worktrees').locator('.hv-shortcut-capture'),
  ).toHaveCount(0);
  // The next Escape is the dialog's again.
  await page.keyboard.press('Escape');
  await expect(page.locator('#settings')).toBeHidden();
});

// Phase 3 (criterion 9): an imported keymap goes through a preview that
// changes nothing until Confirm; after Save its keys run the commands,
// and Export hands Go what the tab shows. Escape cancels the import (the
// real key-scope pipeline, not a synthetic event), not Settings.
test('importing a keymap file, then exporting it', async ({ page }) => {
  await boot(page);
  const half = mac ? 'mac' : 'other';
  await page.evaluate(
    (text) => {
      (window as unknown as { __hive_importText: string }).__hive_importText =
        text;
    },
    JSON.stringify({
      version: 1,
      [half]: { 'new-session': ['Mod+Y'], 'no-such-thing': ['Mod+U'] },
    }),
  );
  await openShortcuts(page);
  const imp = (command: string) =>
    page.locator(`.hv-import-row[data-command="${command}"]`);

  // Escape inside the preview cancels it and leaves Settings open.
  await page.locator('#settings-shortcuts-import').click();
  await expect(page.locator('.hv-shortcuts-import')).toBeVisible();
  await expect(
    page.locator('input[name="shortcut-import-mode"]').first(),
  ).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(page.locator('.hv-shortcuts-import')).toHaveCount(0);
  await expect(page.locator('#settings')).toBeVisible();

  await page.locator('#settings-shortcuts-import').click();
  await page
    .locator('input[name="shortcut-import-mode"][value="replace"]')
    .check();
  await expect(imp('no-such-thing')).toHaveAttribute('data-state', 'skipped');
  await expect(imp('new-session')).toHaveAttribute('data-state', 'ok');
  await expect(page.locator('#settings-save')).toBeDisabled();
  // The footer's reason fits beside the buttons: Save does not wrap.
  const [cancelBox, saveBox] = await Promise.all([
    page.locator('#settings-cancel').boundingBox(),
    page.locator('#settings-save').boundingBox(),
  ]);
  expect(Math.abs((cancelBox?.y ?? 0) - (saveBox?.y ?? 99))).toBeLessThan(2);
  await page.locator('[data-action="confirm-import"]').click();
  await expect(keys(page, 'new-session')).toHaveText([NEW]);

  await page.locator('#settings-shortcuts-export').click();
  await expect
    .poll(async () => (await calls(page, 'ExportKeymap')).map((c) => c.args[0]))
    .toEqual([{ [half]: { 'new-session': ['Mod+Y'] } }]);

  await page.locator('#settings-save').click();
  await expect(page.locator('#settings')).toBeHidden();
  await page.keyboard.press(`${mod}+t`);
  await expect(page.locator('#launcher')).toBeHidden();
  await page.keyboard.press(`${mod}+y`);
  await expect(page.locator('#launcher')).toBeVisible();
});

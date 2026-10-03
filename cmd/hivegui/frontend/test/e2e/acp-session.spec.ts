import { test, expect, type Page } from '@playwright/test';

// Spec 496 end to end against the Wails mock: the launcher creates an
// ACP session, its tile shows a transcript instead of a terminal, a
// prompt goes out from the prompt box, the permission request is
// answered in place, and the streamed reply lands as one message. The
// mock's scripted agent (wails-mock.ts, "ACP sessions") sends the same
// message shapes the daemon does.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

test('an ACP session runs a prompt, a permission and a reply in its transcript', async ({
  page,
}) => {
  await boot(page);
  const before = await page.evaluate(
    () => window.__hive.state?.sessions.length ?? 0,
  );

  await page.keyboard.press(`${mod}+t`);
  const launcher = page.locator('#launcher');
  await expect(launcher).toBeVisible();
  // Terminal is the default.
  await expect(
    launcher.locator('.launcher-kind input[value="pty"]'),
  ).toBeChecked();
  await launcher.locator('.launcher-kind input[value="acp"]').click();
  // Codex cannot run as ACP in the mock: disabled, with its reason.
  await expect(
    launcher.locator('.launcher-item', { hasText: 'Codex' }),
  ).toHaveAttribute('data-available', 'false');
  await launcher.locator('.launcher-item', { hasText: 'Claude' }).click();
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) === n + 1,
    before,
  );

  const tile = page.locator('.term-host.acp.visible');
  await expect(tile.locator('.acp-transcript')).toBeVisible();
  // The terminal body is hidden under the transcript.
  await expect(tile.locator('.term-body')).toHaveCSS('visibility', 'hidden');

  const prompt = tile.locator('textarea[data-acp-prompt]');
  await expect(prompt).toBeFocused();
  await page.keyboard.type('hello');
  await page.keyboard.press('Enter');

  await expect(tile.locator('.acp-item--user')).toHaveText('hello');
  const card = tile.locator('.acp-permission');
  await expect(card).toContainText('Read file');
  await card.getByRole('button', { name: 'Allow' }).click();

  await expect(card).toHaveCount(0);
  await expect(tile.locator('.acp-item--agent')).toHaveText(
    'echo: hello [allow]',
  );
  await expect(tile.locator('.acp-item--tool')).toContainText('completed');

  // Focus leaves the prompt box when the user picks a terminal tile:
  // the prompt is this tile's input, not a control that keeps the
  // keyboard (lib/focus.ts). It starts in the prompt...
  await expect(prompt).toBeFocused();
  // ...and the switch is made from the keyboard, which is where the bug lived: a click moves
  // focus by itself, a session switch does not.
  await page.keyboard.press(`${mod}+ArrowUp`);
  await expect(page.locator('.term-host.acp.visible')).toHaveCount(0);
  await expect(
    page.locator('.term-host.visible .xterm-helper-textarea').first(),
  ).toBeFocused();
});

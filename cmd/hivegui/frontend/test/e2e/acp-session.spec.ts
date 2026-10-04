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
  // Its reason ellipsizes at half the row; the agent name keeps its room.
  const codexRow = launcher.locator('.launcher-item', { hasText: 'Codex' });
  const widths = await codexRow.evaluate((row) => ({
    row: row.getBoundingClientRect().width,
    name: (
      row.querySelector('.agent-name') as HTMLElement
    ).getBoundingClientRect().width,
    tag: (
      row.querySelector('.install-tag') as HTMLElement
    ).getBoundingClientRect().width,
  }));
  expect(widths.name).toBeGreaterThan(30);
  expect(widths.tag).toBeLessThanOrEqual(widths.row / 2 + 1);
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
  // Usable once the phase overlay is gone; it covers the transcript
  // until then, and a click on the card would only retry under it.
  await expect(tile.locator('.phase-overlay')).toBeHidden();
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

test('an ACP tile keeps its place at the bottom across a switch away and back', async ({
  page,
}) => {
  // A hidden tile is display:none (layout.css), which reads as
  // scrollTop 0 while hidden; Chromium and WebKit both restore the
  // offset on show, so the transcript comes back at the newest message.
  // This pins that, rather than a re-pin Hive would have to add.
  await boot(page);
  await page.keyboard.press(`${mod}+t`);
  const launcher = page.locator('#launcher');
  await launcher.locator('.launcher-kind input[value="acp"]').click();
  await launcher.locator('.launcher-item', { hasText: 'Claude' }).click();
  const tile = page.locator('.term-host.acp.visible');
  const prompt = tile.locator('textarea[data-acp-prompt]');
  await expect(prompt).toBeFocused();
  // The session is usable once its phase overlay is gone; before that
  // the overlay sits over the permission card and a click retries.
  await expect(tile.locator('.phase-overlay')).toBeHidden();
  // Long enough to overflow the log several times over.
  await prompt.fill(
    Array.from({ length: 120 }, (_, i) => `line ${i}`).join('\n'),
  );
  await page.keyboard.press('Enter');
  // Scrolled up to the card (as clicking it can do): answering re-pins
  // the log, so the reply is what the user sees.
  await expect(tile.locator('.acp-permission')).toBeVisible();
  await tile.locator('.acp-transcript__log').evaluate((el) => {
    el.scrollTop = 0;
    el.dispatchEvent(new Event('scroll'));
  });
  await tile
    .locator('.acp-permission')
    .getByRole('button', { name: 'Allow' })
    .click();
  await expect(tile.locator('.acp-item--agent')).toContainText('[allow]');
  const log = tile.locator('.acp-transcript__log');
  const atBottom = () =>
    log.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight < 8);
  await expect.poll(atBottom).toBe(true);
  expect(
    await log.evaluate((el) => el.scrollHeight > el.clientHeight * 2),
  ).toBe(true);
  const sid = await tile.getAttribute('data-sid');

  await page.keyboard.press(`${mod}+ArrowUp`);
  await expect(page.locator('.term-host.acp.visible')).toHaveCount(0);
  await page.keyboard.press(`${mod}+ArrowDown`);
  const back = page.locator(
    `.term-host.acp.visible[data-sid="${sid}"] .acp-transcript__log`,
  );
  await expect(back).toBeVisible();
  await expect
    .poll(() =>
      back.evaluate(
        (el) => el.scrollHeight - el.scrollTop - el.clientHeight < 8,
      ),
    )
    .toBe(true);
});

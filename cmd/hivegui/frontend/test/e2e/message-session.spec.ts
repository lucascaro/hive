import { test, expect, type Page } from '@playwright/test';

// E2E for session messaging (spec 338) against the mock bridge: that
// ⇧⌘M reaches the note sheet through the real keyboard pipeline, that a
// busy session turns Send into a queue offer with the text kept, that the
// sheet is actually on top of what it covers, and that the bell toggles
// from both its chord and the row.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects .hv-session-row').length > 0,
  );
}

const sheet = (page: Page) => page.locator('#message-session');
const sent = (page: Page) =>
  page.evaluate(() => window.__hive.sentMessages?.() ?? []);
const activeId = (page: Page) =>
  page.evaluate(
    () =>
      document
        .querySelector('#projects .hv-session-row[data-selected]')
        ?.getAttribute('data-sid') ?? '',
  );

test('⇧⌘M opens the sheet on the active session and Enter sends', async ({
  page,
}) => {
  await boot(page);
  const id = await activeId(page);
  await page.keyboard.press(`${mod}+Shift+m`);
  await expect(sheet(page)).toBeVisible();
  await expect(page.locator('#message-session-text')).toBeFocused();
  await page.locator('#message-session-text').fill('rebase onto main');
  await page.keyboard.press('Enter');
  // The mock plays the daemon: an idle session takes it, so the sheet closes.
  await expect(sheet(page)).toBeHidden();
  expect(await sent(page)).toEqual([
    { id, text: 'rebase onto main', queue: false },
  ]);
});

test('the sheet is on top: elementFromPoint at its centre is inside it', async ({
  page,
}) => {
  await boot(page);
  await page.keyboard.press(`${mod}+Shift+m`);
  await expect(sheet(page)).toBeVisible();
  const onTop = await page.evaluate(() => {
    const box = document.querySelector(
      '#message-session .hv-modal, #message-session > *',
    );
    if (!box) return false;
    const r = box.getBoundingClientRect();
    const hit = document.elementFromPoint(
      r.left + r.width / 2,
      r.top + r.height / 2,
    );
    return (
      !!hit &&
      document.getElementById('message-session')?.contains(hit) === true
    );
  });
  expect(onTop).toBe(true);
});

test('a busy session keeps the text and offers to queue', async ({ page }) => {
  await boot(page);
  const id = await activeId(page);
  await page.evaluate((i) => window.__hive.setSessionState?.(i, 'working'), id);
  await page.keyboard.press(`${mod}+Shift+m`);
  await page.locator('#message-session-text').fill('after the build');
  await page.keyboard.press('Enter');
  await expect(page.locator('#message-session-busy')).toBeVisible();
  await expect(page.locator('#message-session-text')).toHaveValue(
    'after the build',
  );
  await expect(sheet(page)).toBeVisible();
  await page.locator('#message-session-queue').click();
  await expect(sheet(page)).toBeHidden();
  expect(await sent(page)).toEqual([
    { id, text: 'after the build', queue: false },
    { id, text: 'after the build', queue: true },
  ]);
});

test('Escape drops the note and sends nothing', async ({ page }) => {
  await boot(page);
  await page.keyboard.press(`${mod}+Shift+m`);
  await page.locator('#message-session-text').fill('never mind');
  await page.keyboard.press('Escape');
  await expect(sheet(page)).toBeHidden();
  expect(await sent(page)).toEqual([]);
});

test('the bell: chord arms, the row shows it, the row button disarms', async ({
  page,
}) => {
  await boot(page);
  const row = page.locator('#projects .hv-session-row[data-selected]');
  await expect(row.locator('.hv-session-row__bell')).toHaveCount(0);
  await page.keyboard.press(
    process.platform === 'darwin' ? 'Meta+Alt+KeyB' : 'Control+Alt+Shift+KeyB',
  );
  await expect(row.locator('.hv-session-row__bell')).toHaveCount(1);
  await row.hover();
  await row.locator('[data-action="notify-when-idle"]').click();
  await expect(row.locator('.hv-session-row__bell')).toHaveCount(0);
});

test('an idle_ping notifies and the row bell clears', async ({ page }) => {
  await boot(page);
  const id = await activeId(page);
  const row = page.locator('#projects .hv-session-row[data-selected]');
  await page.keyboard.press(
    process.platform === 'darwin' ? 'Meta+Alt+KeyB' : 'Control+Alt+Shift+KeyB',
  );
  await expect(row.locator('.hv-session-row__bell')).toHaveCount(1);
  await page.evaluate((i) => {
    const s = window.__hive.state?.sessions.find((x) => x.id === i);
    if (!s) return;
    s.notify_when_idle = false;
    window.__hive.emit(
      'session:event',
      JSON.stringify({ kind: 'idle_ping', session: s }),
    );
  }, id);
  await expect(row.locator('.hv-session-row__bell')).toHaveCount(0);
});

import { test, expect, type Page } from '@playwright/test';

// E2E for idea capture and the inbox against the mock bridge.
//
// The behaviours worth a browser test are the ones the jsdom suite
// cannot show: that ⌘I and ⇧⌘I actually reach the two modals through
// the real keyboard pipeline, that a captured idea comes back over the
// daemon's fan-out and lands on the sidebar badge, and that the badge
// is the mouse path to the inbox.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

const sheet = (page: Page) => page.locator('#quick-idea');
const inbox = (page: Page) => page.locator('#idea-inbox');
const badge = (page: Page) =>
  page.locator('#projects .hv-project-card__ideas').first();
const rows = (page: Page) => page.locator('#idea-inbox-list .idea-row');

// Capture one idea through the sheet, the way a user does.
async function capture(page: Page, text: string, kind?: string) {
  await page.keyboard.press(`${mod}+i`);
  await expect(sheet(page)).toBeVisible();
  // Clicked on the label, not the input: the radio itself is
  // off-screen (ideas.css) so the label IS the control.
  if (kind)
    await page.locator(`#quick-idea-kind [data-kind="${kind}"]`).click();
  await page.locator('#quick-idea-text').fill(text);
  await page.keyboard.press('Enter');
  await expect(sheet(page)).toBeHidden();
}

test('⌘I captures an idea and the project card counts it', async ({ page }) => {
  await boot(page);
  // Nothing captured yet — the badge is absent, not a zero.
  await expect(badge(page)).toHaveCount(0);
  await capture(page, 'the grid loses focus after ⌘G twice');
  await expect(badge(page)).toHaveText('1');
  await capture(page, 'second one');
  await expect(badge(page)).toHaveText('2');
});

test('⇧⌘I opens the inbox with what was captured', async ({ page }) => {
  await boot(page);
  await capture(page, 'a bug in the launcher', 'bug');
  await page.keyboard.press(`${mod}+Shift+i`);
  await expect(inbox(page)).toBeVisible();
  await expect(rows(page)).toHaveCount(1);
  await expect(rows(page).first()).toContainText('a bug in the launcher');
  // The kind the user picked rides along.
  await expect(rows(page).first().locator('.idea-kind')).toHaveText('bug');
  await page.keyboard.press(`${mod}+Shift+i`);
  await expect(inbox(page)).toBeHidden();
});

test('the badge opens the inbox too', async ({ page }) => {
  await boot(page);
  await capture(page, 'from the mouse');
  await badge(page).click();
  await expect(inbox(page)).toBeVisible();
});

test('Done takes an idea out of the inbox and off the badge', async ({
  page,
}) => {
  await boot(page);
  await capture(page, 'triage me');
  await page.keyboard.press(`${mod}+Shift+i`);
  await rows(page).first().getByRole('button', { name: 'Done' }).click();
  await expect(rows(page)).toHaveCount(0);
  await expect(page.locator('#idea-inbox-empty')).toBeVisible();
  await page.keyboard.press('Escape');
  // Done is not delete — the note is kept — but it stops counting.
  await expect(badge(page)).toHaveCount(0);
});

test('Delete is gated by the confirm', async ({ page }) => {
  await boot(page);
  await capture(page, 'not sure about this one');
  await page.keyboard.press(`${mod}+Shift+i`);
  await rows(page).first().getByRole('button', { name: 'Delete' }).click();
  await page.locator('.choice-dialog button[data-choice="cancel"]').click();
  await expect(rows(page)).toHaveCount(1);
  await rows(page).first().getByRole('button', { name: 'Delete' }).click();
  await page.locator('.choice-dialog button[data-choice="delete"]').click();
  await expect(rows(page)).toHaveCount(0);
});

test('ideas the daemon already had show up at boot', async ({ page }) => {
  // ?slowConnect stalls the mock handshake, which is the window in
  // which the seed has to land: the boot LIST_IDEAS goes out as soon as
  // ConnectControl resolves, and it is what delivers these.
  await page.goto('/?slowConnect=800');
  await page.evaluate(() => {
    window.__hive.seedIdeas?.([
      {
        id: 'i-seed',
        project_id: 'p1',
        kind: 'feedback',
        text: 'filed from a shell before this window opened',
        status: 'open',
        created: new Date().toISOString(),
        updated: new Date().toISOString(),
      },
    ]);
  });
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await expect(badge(page)).toHaveText('1');
});

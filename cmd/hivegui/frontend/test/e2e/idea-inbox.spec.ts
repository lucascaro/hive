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

// The spec's headline flow, end to end in a browser: capture → count →
// start → the prompt actually in the session's output. jsdom can show
// the calls; only this can show the launcher opening under the right
// card and the daemon's answer painting into a terminal.
test('Start session seeds the new session with the note', async ({ page }) => {
  await boot(page);
  await capture(page, 'sidebar drag handle is 1px off', 'bug');
  await badge(page).click();
  await rows(page)
    .first()
    .getByRole('button', { name: 'Start session' })
    .click();

  // The launcher takes over, seeded with what the session will open
  // with — shaped as an instruction so the agent knows what to DO with
  // the note rather than just what was noticed, and editable, because
  // the note was jotted mid-task.
  await expect(inbox(page)).toBeHidden();
  const launcher = page.locator('#launcher');
  await expect(launcher).toBeVisible();
  await expect(page.locator('#launcher-prompt')).toHaveValue(
    /find the root cause/,
  );
  await expect(page.locator('#launcher-prompt')).toHaveValue(
    /sidebar drag handle is 1px off/,
  );

  // Down to Claude first. Shell is the first row and cannot be handed
  // a prompt at all, so launching it here would assert delivery the
  // daemon refuses — which is exactly what this test used to do, and
  // it stayed green because the mock delivered unconditionally.
  await expect(page.locator('#launcher-prompt-warn')).toContainText(
    'cannot take an opening prompt',
  );
  await page.keyboard.press('ArrowDown');
  // Empties rather than unmounting: the live region has to predate its
  // own content or screen readers miss the announcement.
  await expect(page.locator('#launcher-prompt-warn')).toHaveText('');
  await page.keyboard.press('Enter');
  await expect(launcher).toBeHidden();

  // The prompt reaches the PTY — the mock plays the daemon's half —
  // and only then does the idea flip, which is what puts the glyph on
  // the new session's row and takes it off the badge.
  const term = page.locator('.hv-session-row__idea').first();
  await expect(term).toHaveCount(1);
  await expect(term).toHaveAttribute('title', /sidebar drag handle is 1px off/);
  // Started is not done: the idea outlives the session by design, and
  // taking it out of the inbox stays an explicit action.
  await expect(badge(page)).toHaveText('1');
});

// A layout assertion, and it has to be a real browser: vitest has no
// CSS at all, and this theme sets box-sizing per rule rather than
// globally — so the field's own padding and border pushed it out
// through the right edge of the popup while every jsdom test stayed
// green. Measured at 354px inside a 350px popup before the fix.
test('the opening prompt stays inside the launcher popup', async ({ page }) => {
  await boot(page);
  await capture(
    page,
    'the sidebar drag handle is one pixel off and the whole row jumps ' +
      'when you grab it near the bottom edge of a collapsed project card',
    'bug',
  );
  await badge(page).click();
  await rows(page)
    .first()
    .getByRole('button', { name: 'Start session' })
    .click();

  const field = await page.locator('#launcher-prompt').boundingBox();
  const popup = await page.locator('#launcher').boundingBox();
  expect(field).not.toBeNull();
  expect(popup).not.toBeNull();
  if (!field || !popup) return;
  expect(field.x).toBeGreaterThanOrEqual(popup.x - 0.5);
  expect(field.x + field.width).toBeLessThanOrEqual(
    popup.x + popup.width + 0.5,
  );
  expect(field.y + field.height).toBeLessThanOrEqual(
    popup.y + popup.height + 0.5,
  );
  // The popup itself stays on screen, and nothing scrolls sideways.
  expect(popup.x + popup.width).toBeLessThanOrEqual(
    page.viewportSize()?.width ?? 0,
  );
  expect(
    await page.evaluate(() => {
      const el = document.getElementById('launcher');
      return el ? el.scrollWidth - el.clientWidth : 0;
    }),
  ).toBeLessThanOrEqual(1);
});

test('Edit corrects the note, its kind and its project', async ({ page }) => {
  await boot(page);
  await capture(page, 'misfiled note');
  await badge(page).click();
  await rows(page).first().getByRole('button', { name: 'Edit' }).click();

  // The same three controls capture offered, pre-filled — the fields
  // capture asked for are exactly the fields that can be wrong. The
  // inbox gives way to it: this app never stacks two dialogs.
  await expect(inbox(page)).toBeHidden();
  await expect(sheet(page)).toBeVisible();
  await expect(page.locator('#quick-idea-text')).toHaveValue('misfiled note');
  await page.locator('#quick-idea-text').fill('sharper wording');
  await page.locator('#quick-idea-kind [data-kind="bug"]').click();
  await page.locator('#quick-idea-save').click();
  await expect(sheet(page)).toBeHidden();

  await badge(page).click();
  await expect(rows(page).first()).toContainText('sharper wording');
  await expect(rows(page).first().locator('.idea-kind')).toHaveText('bug');
});

// The other half of the same rule, and the one a user hits by accident:
// Shell is the launcher's first row, so Start session → Enter lands on
// it. Nothing is delivered, and — the part that matters — the note
// stays in the inbox instead of being marked started for work that
// never happened.
test('an agent that cannot take the prompt keeps the idea in the inbox', async ({
  page,
}) => {
  await boot(page);
  await capture(page, 'do not lose this note', 'bug');
  await badge(page).click();
  await rows(page)
    .first()
    .getByRole('button', { name: 'Start session' })
    .click();

  await expect(page.locator('#launcher-prompt-warn')).toContainText(
    'cannot take an opening prompt',
  );
  // Shell is still selected: launch it exactly as an unwary user would.
  await page.keyboard.press('Enter');
  await expect(page.locator('#launcher')).toBeHidden();

  // The session exists, but no idea glyph — nothing was handed over.
  await expect(page.locator('.hv-session-row')).not.toHaveCount(0);
  await expect(page.locator('.hv-session-row__idea')).toHaveCount(0);
  // And the note is still there to start again.
  await expect(badge(page)).toHaveText('1');
  await badge(page).click();
  await expect(rows(page).first()).toContainText('do not lose this note');
});

import { test, expect, type Page } from '@playwright/test';
import { closeSettings, openPluginsTab } from './plugin-helpers.js';

// E2E for plan review (#457) against the mock bridge, as the bundled
// plan-review plugin (spec 471): the user enables it in Settings →
// Plugins, an agent asks for a review, the plugin raises it through the
// real event and keyboard pipeline, and the user's answer is exactly
// what the agent would get.

const PLAN = [
  '# Add a greeting',
  '',
  '1. Create `hello.txt` containing hi',
  '2. Verify with `cat hello.txt`',
].join('\n');

const row = (page: Page) =>
  page.locator('.settings-plugin-row[data-plugin-id="plan-review"]');

async function firstSession(page: Page): Promise<string> {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  return page.evaluate(() => window.__hive.state?.sessions[0].id ?? '');
}

/** Boots and enables the bundled plugin the way a user does. Returns
 * once the window has announced the review UI, so a review parks. */
async function boot(page: Page): Promise<string> {
  const id = await firstSession(page);
  await openPluginsTab(page);
  await row(page).locator('.settings-plugin-enabled').check();
  await closeSettings(page);
  await expect
    .poll(() =>
      page.evaluate((sid) => {
        const r = window.__hive.requestPlanReview?.(sid, '# warm-up');
        if (r) window.__hive.withdrawPlanReview?.(sid);
        return r ?? null;
      }, id),
    )
    .not.toBeNull();
  // The warm-up raised a view; its withdrawal takes it down again.
  await expect(modal(page)).toBeHidden();
  return id;
}

const modal = (page: Page) => page.locator('#plugin-view');
const banner = (page: Page) =>
  page.locator('.hv-plugin-banner[data-plugin-id="plan-review"]');
const request = (page: Page, id: string, plan = PLAN) =>
  page.evaluate(([sid, p]) => window.__hive.requestPlanReview?.(sid, p) ?? '', [
    id,
    plan,
  ] as const);
const answers = (page: Page) =>
  page.evaluate(() => window.__hive.planReviewAnswers?.() ?? []);

// Select the text of the first match inside the plan, as a drag would.
async function selectInPlan(page: Page, selector: string) {
  await page.evaluate((sel) => {
    const node = document.querySelector(`#plan-review-body ${sel}`);
    if (!node) throw new Error(`no ${sel} in the plan`);
    const range = document.createRange();
    range.selectNodeContents(node);
    const s = window.getSelection();
    s?.removeAllRanges();
    s?.addRange(range);
  }, selector);
  await page.locator('#plan-review-body').dispatchEvent('mouseup');
}

test('a review raises itself, and a deny carries the comment and its quote', async ({
  page,
}) => {
  const id = await boot(page);
  const reviewId = await request(page, id);
  await expect(modal(page)).toBeVisible();
  await expect(page.locator('#plan-review-body h3')).toHaveText(
    'Add a greeting',
  );
  await expect(page.locator('#plan-review-body ol li')).toHaveCount(2);

  await selectInPlan(page, 'li code');
  await page.locator('#plan-review-comment-start').click();
  await page.locator('#plan-review-comment').fill('Call it greeting.txt');
  await page.locator('#plan-review-comment-add').click();
  await page.locator('#plan-review-feedback').fill('And add a test.');
  await page.locator('#plan-review-deny').click();

  await expect(modal(page)).toBeHidden();
  expect(await answers(page)).toEqual([
    {
      session_id: id,
      review_id: reviewId,
      decision: 'deny',
      comments: [{ quote: 'hello.txt', text: 'Call it greeting.txt' }],
      feedback: 'And add a test.',
    },
  ]);
});

test('approve sends the review id and closes', async ({ page }) => {
  const id = await boot(page);
  const reviewId = await request(page, id);
  await expect(modal(page)).toBeVisible();
  await page.locator('#plan-review-approve').click();
  await expect(modal(page)).toBeHidden();
  expect(await answers(page)).toEqual([
    expect.objectContaining({
      session_id: id,
      review_id: reviewId,
      decision: 'approve',
    }),
  ]);
});

test('Escape defers without answering; the bar brings it back', async ({
  page,
}) => {
  const id = await boot(page);
  await request(page, id);
  await expect(modal(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(modal(page)).toBeHidden();
  expect(await answers(page)).toEqual([]);
  await expect(banner(page)).toContainText(
    'is waiting for you to review its plan',
  );
  await banner(page).locator('.hv-plugin-banner__action').click();
  await expect(modal(page)).toBeVisible();
});

test('a review answered elsewhere closes the modal', async ({ page }) => {
  const id = await boot(page);
  await request(page, id);
  await expect(modal(page)).toBeVisible();
  await page.evaluate((sid) => window.__hive.withdrawPlanReview?.(sid), id);
  await expect(modal(page)).toBeHidden();
  await expect(banner(page)).toHaveCount(0);
});

test('the plan follows the theme', async ({ page }) => {
  const id = await boot(page);
  await request(page, id);
  await expect(modal(page)).toBeVisible();
  const bg = () =>
    page
      .locator('#plan-review-body')
      .evaluate((el) => getComputedStyle(el).backgroundColor);
  const before = await bg();
  const want = await page.evaluate(() =>
    getComputedStyle(document.documentElement)
      .getPropertyValue('--surface')
      .trim(),
  );
  expect(want).not.toBe('');
  // Swap to another preset and the plan's surface follows it.
  await page.evaluate(() => {
    const root = document.documentElement;
    root.dataset.theme = root.dataset.theme === 'light' ? 'dark' : 'light';
  });
  await expect.poll(bg).not.toBe(before);
});

// Criterion 3: installed on every machine, disabled, and not removable.
// Disabled, nothing is raised: the agent falls back to its terminal.
test('ships installed and disabled, and reviews nothing until enabled', async ({
  page,
}) => {
  const id = await firstSession(page);
  await openPluginsTab(page);
  await expect(row(page)).toBeVisible();
  await expect(row(page).locator('.settings-plugin-enabled')).not.toBeChecked();
  await expect(row(page).locator('.settings-plugin-remove')).toHaveCount(0);
  await expect(row(page).locator('.settings-plugin-source')).toHaveText(
    'Ships with Hive',
  );
  await closeSettings(page);
  expect(await request(page, id)).toBe('');
  await expect(modal(page)).toBeHidden();
});

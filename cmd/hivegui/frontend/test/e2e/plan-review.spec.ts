import { test, expect, type Page } from '@playwright/test';

// E2E for plan review (#457) against the mock bridge: an agent asks for a
// review, the GUI raises it through the real event and keyboard
// pipeline, and the user's answer is exactly what the agent would get.

const PLAN = [
  '# Add a greeting',
  '',
  '1. Create `hello.txt` containing hi',
  '2. Verify with `cat hello.txt`',
].join('\n');

async function boot(page: Page): Promise<string> {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  return page.evaluate(() => window.__hive.state?.sessions[0].id ?? '');
}

const modal = (page: Page) => page.locator('#plan-review');
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
  await expect(page.locator('#plan-review-bar')).toBeVisible();
  await page.locator('#plan-review-bar-open').click();
  await expect(modal(page)).toBeVisible();
});

test('a review answered elsewhere closes the modal', async ({ page }) => {
  const id = await boot(page);
  await request(page, id);
  await expect(modal(page)).toBeVisible();
  await page.evaluate((sid) => window.__hive.withdrawPlanReview?.(sid), id);
  await expect(modal(page)).toBeHidden();
  await expect(page.locator('#plan-review-bar')).toHaveCount(0);
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

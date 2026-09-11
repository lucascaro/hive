import { test, expect, type Page } from '@playwright/test';

// The session colour bar is also the colour control, so the only claims
// worth making about it are layout claims: it widens into a gutter the
// row already reserves, it is the top element at its own centre (not
// covered by the hover-revealed action buttons), and those buttons do
// not move when it grows. jsdom can see none of that.

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

function firstRow(page: Page) {
  return page.locator('#projects .hv-session-row').first();
}

test.describe('the colour bar is the picker', () => {
  test('widens on hover without moving the action buttons', async ({
    page,
  }) => {
    await boot(page);
    const row = firstRow(page);
    const bar = row.locator('.hv-session-row__colour');

    const rest = await bar.boundingBox();
    const actionsAtRest = await row
      .locator('.hv-session-row__actions')
      .boundingBox();
    if (!rest || !actionsAtRest) throw new Error('row not laid out');
    expect(rest.width).toBeCloseTo(3, 0);

    await row.hover();
    await expect
      .poll(async () => (await bar.boundingBox())?.width)
      .toBeGreaterThan(11);
    const actionsOnHover = await row
      .locator('.hv-session-row__actions')
      .boundingBox();
    if (!actionsOnHover) throw new Error('actions not laid out');
    // The gutter is reserved at all times, so the reveal costs no reflow.
    expect(actionsOnHover.x).toBeCloseTo(actionsAtRest.x, 1);
    expect(actionsOnHover.width).toBeCloseTo(actionsAtRest.width, 1);
    // …and the buttons stay clear of the widened bar.
    expect(actionsOnHover.x + actionsOnHover.width).toBeLessThanOrEqual(
      (await bar.boundingBox())!.x + 1,
    );
  });

  test('is the element a click at its centre lands on', async ({ page }) => {
    await boot(page);
    const row = firstRow(page);
    await row.hover();
    const bar = row.locator('.hv-session-row__colour');
    await expect
      .poll(async () => (await bar.boundingBox())?.width)
      .toBeGreaterThan(11);
    const box = await bar.boundingBox();
    if (!box) throw new Error('no bar');
    const hit = await page.evaluate(
      ({ x, y }) =>
        document.elementFromPoint(x, y)?.closest('.hv-session-row__colour') !==
        null,
      { x: box.x + box.width / 2, y: box.y + box.height / 2 },
    );
    expect(hit).toBe(true);
  });

  test('carries the session colour and opens a native colour input', async ({
    page,
  }) => {
    await boot(page);
    const row = firstRow(page);
    const colour = await row.evaluate((el) =>
      getComputedStyle(el).getPropertyValue('--session-color').trim(),
    );
    expect(colour).not.toBe('');
    const painted = await row
      .locator('.hv-session-row__colour')
      .evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(painted).not.toBe('rgba(0, 0, 0, 0)');
    await expect(row.locator('.hv-session-row__colour input')).toHaveAttribute(
      'type',
      'color',
    );
  });
});

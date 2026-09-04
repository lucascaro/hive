import { test, expect, type Page } from '@playwright/test';

// Layout check for the sidebar header's two action buttons (spec 323).
// The DOM test proves SidebarHeaderControls renders #check-updates-btn
// next to #new-project-btn; only a real browser proves the header actually
// LAYS THEM OUT that way. The header used to be `justify-content:
// space-between` with two children, which with three children would
// fling the two buttons to opposite ends of the sidebar — a bug no
// jsdom assertion can see.

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForSelector('#check-updates-btn');
}

test('both header buttons sit together on the right of the brand', async ({
  page,
}) => {
  await boot(page);
  const brand = page.locator('#sidebar header .brand');
  const add = page.locator('#new-project-btn');
  const check = page.locator('#check-updates-btn');

  await expect(add).toBeVisible();
  await expect(check).toBeVisible();
  await expect(check).toHaveAttribute('aria-label', 'Check for updates');

  const brandBox = (await brand.boundingBox())!;
  const addBox = (await add.boundingBox())!;
  const checkBox = (await check.boundingBox())!;

  // Adjacent, in order, and not flung apart: the gap between them is
  // the header's 6px, not the width of the whole sidebar.
  expect(checkBox.x).toBeGreaterThan(addBox.x);
  expect(checkBox.x - (addBox.x + addBox.width)).toBeLessThanOrEqual(8);
  // The brand keeps the slack, so the pair is pushed to the right edge.
  expect(addBox.x).toBeGreaterThan(brandBox.x + brandBox.width - 1);

  // Same visual weight — both are the 22px icon-button primitive.
  expect(Math.round(addBox.width)).toBe(22);
  expect(Math.round(addBox.height)).toBe(22);
  expect(Math.round(checkBox.width)).toBe(22);
  expect(Math.round(checkBox.height)).toBe(22);

  // Vertically centred against each other, not stacked or offset.
  expect(Math.abs(addBox.y - checkBox.y)).toBeLessThan(1);
});

test('the check-updates button is reachable and hit-testable', async ({
  page,
}) => {
  await boot(page);
  const check = page.locator('#check-updates-btn');
  const box = (await check.boundingBox())!;
  // elementFromPoint, not just visibility: a header sibling overlapping
  // the button would still report "visible" while eating every click.
  const topmost = await page.evaluate(
    ([x, y]) => document.elementFromPoint(x, y)?.closest('button')?.id ?? '',
    [box.x + box.width / 2, box.y + box.height / 2],
  );
  expect(topmost).toBe('check-updates-btn');

  // Focusable by keyboard. Not asserted via .focus() + toBeFocused():
  // the app pulls focus back to the active terminal, so the assertion
  // would race the refocus rather than test the button.
  await expect(check).toBeEnabled();
  expect(await check.evaluate((el) => el.tabIndex)).toBeGreaterThanOrEqual(0);
});

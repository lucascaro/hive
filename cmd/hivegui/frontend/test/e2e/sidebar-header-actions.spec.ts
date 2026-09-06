import { test, expect, type Page } from '@playwright/test';

// Layout check for the sidebar header's three action buttons (specs 323, 351).
// The DOM tests prove SidebarHeaderControls renders #check-updates-btn and
// #whats-new-btn next to #new-project-btn; only a real browser proves the
// header actually LAYS THEM OUT that way. The header used to be
// `justify-content: space-between` with two children, which with three
// children would fling them to opposite ends of the sidebar — a bug no
// jsdom assertion can see, and the reason this file exists.

const BUTTONS = ['new-project-btn', 'check-updates-btn', 'whats-new-btn'];

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForSelector('#whats-new-btn');
}

test('all three header buttons sit together on the right of the brand', async ({
  page,
}) => {
  await boot(page);
  const brand = page.locator('#sidebar header .brand');
  const brandBox = (await brand.boundingBox())!;

  const boxes = [];
  for (const id of BUTTONS) {
    const btn = page.locator(`#${id}`);
    await expect(btn).toBeVisible();
    boxes.push((await btn.boundingBox())!);
  }

  await expect(page.locator('#check-updates-btn')).toHaveAttribute(
    'aria-label',
    'Check for updates',
  );
  // Unread on a fresh browser profile, so the name carries that too.
  await expect(page.locator('#whats-new-btn')).toHaveAttribute(
    'aria-label',
    /^What's new/,
  );

  // Adjacent, in order, and not flung apart: each gap is the header's 6px,
  // not the width of the whole sidebar.
  for (let i = 1; i < boxes.length; i++) {
    expect(boxes[i].x).toBeGreaterThan(boxes[i - 1].x);
    expect(
      boxes[i].x - (boxes[i - 1].x + boxes[i - 1].width),
    ).toBeLessThanOrEqual(8);
  }

  // The brand keeps the slack, so the cluster is pushed to the right edge.
  expect(boxes[0].x).toBeGreaterThan(brandBox.x + brandBox.width - 1);

  // Same visual weight — all three are the 22px icon-button primitive.
  for (const box of boxes) {
    expect(Math.round(box.width)).toBe(22);
    expect(Math.round(box.height)).toBe(22);
  }

  // One row, vertically centred against each other, not stacked or offset.
  for (const box of boxes.slice(1)) {
    expect(Math.abs(box.y - boxes[0].y)).toBeLessThan(1);
  }
});

for (const id of ['check-updates-btn', 'whats-new-btn']) {
  test(`the ${id} button is reachable and hit-testable`, async ({ page }) => {
    await boot(page);
    const btn = page.locator(`#${id}`);
    const box = (await btn.boundingBox())!;
    // elementFromPoint, not just visibility: a header sibling overlapping
    // the button would still report "visible" while eating every click.
    // The unread dot on the gift is an ::after on the button itself, so it
    // must not register as a separate hit target either.
    const topmost = await page.evaluate(
      ([x, y]) => document.elementFromPoint(x, y)?.closest('button')?.id ?? '',
      [box.x + box.width / 2, box.y + box.height / 2],
    );
    expect(topmost).toBe(id);

    // Focusable by keyboard. Not asserted via .focus() + toBeFocused():
    // the app pulls focus back to the active terminal, so the assertion
    // would race the refocus rather than test the button.
    await expect(btn).toBeEnabled();
    expect(await btn.evaluate((el) => el.tabIndex)).toBeGreaterThanOrEqual(0);
  });
}

test("the gift opens What's New, and Escape closes it", async ({ page }) => {
  await boot(page);
  const gift = page.locator('#whats-new-btn');
  const dialog = page.locator('#whats-new');

  // Unread on a fresh profile: nothing has been read, so the dot is up.
  await expect(gift).toHaveClass(/hv-unread/);
  await expect(dialog).toBeHidden();

  await gift.click();
  await expect(dialog).toBeVisible();
  // Newest release first, and at least one feature under it.
  await expect(dialog.locator('.whats-new-release').first()).toBeVisible();
  await expect(dialog.locator('.whats-new-release li').first()).toBeVisible();
  // Reading it clears the dot — and says so in words, not only in CSS.
  await expect(gift).not.toHaveClass(/hv-unread/);
  await expect(gift).toHaveAttribute('aria-label', "What's new");

  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
});

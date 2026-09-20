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

// Spec 434. The DOM test proves the total is a child of .brand; only a real
// browser proves it LAYS OUT right after "Hive" rather than being flung
// right with the button cluster by .brand's `margin-right: auto`.
test('the session total sits inside the brand, right after Hive', async ({
  page,
}) => {
  await boot(page);
  // The mock seeds one session; a second checks the plural title too.
  await page.evaluate((n) => window.__hive.addSession?.(n), 's2');
  await page.waitForFunction(
    () => (window.__hive.state?.sessions.length ?? 0) >= 2,
  );

  const count = page.locator('#sidebar header .brand > .brand-count');
  await expect(count).toHaveText('2');
  await expect(count).toHaveAttribute('title', '2 sessions');

  const brandBox = (await page
    .locator('#sidebar header .brand')
    .boundingBox())!;
  const countBox = (await count.boundingBox())!;
  const newBtnBox = (await page.locator('#new-project-btn').boundingBox())!;

  // Contained in the brand box, not overflowing it.
  expect(countBox.x).toBeGreaterThanOrEqual(brandBox.x);
  expect(countBox.x + countBox.width).toBeLessThanOrEqual(
    brandBox.x + brandBox.width + 0.5,
  );
  // Right after the "Hive" glyphs, not pushed to the far side.
  expect(countBox.x - brandBox.x).toBeLessThan(60);
  // And clear of the button cluster.
  expect(countBox.x + countBox.width).toBeLessThanOrEqual(newBtnBox.x);
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

// #436: a background update check reports as a dot on ⤓, not a banner.
// jsdom proves the class; only a real layout proves the ::after dot is
// actually painted on this button.
test('a background update shows a dot on the check-for-updates button, not a banner', async ({
  page,
}) => {
  await boot(page);
  const btn = page.locator('#check-updates-btn');
  const dot = () =>
    btn.evaluate((el) => {
      const cs = getComputedStyle(el, '::after');
      return { content: cs.content, width: Number.parseFloat(cs.width) || 0 };
    });

  // Baseline: the mock's boot poll reports nothing, so no dot yet — the
  // dot below is this feature's, not some other rule's.
  await expect(btn).not.toHaveClass(/hv-unread/);
  expect((await dot()).content).toBe('none');

  await page.evaluate(() =>
    window.__hive.emit('update:available', {
      available: true,
      current: '2.4.0',
      latest: '2.5.0',
      url: '',
      stage: 'available',
      channel: 'release',
    }),
  );

  await expect(btn).toHaveClass(/hv-unread/);
  await expect(btn).toHaveAttribute(
    'aria-label',
    'Check for updates — update available',
  );
  const after = await dot();
  expect(after.content).not.toBe('none');
  expect(after.width).toBeGreaterThan(0);
  await expect(page.locator('#update-banner')).toBeHidden();
});

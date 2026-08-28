import { test, expect, type Page } from '@playwright/test';

// Layout coverage for the window-title line under each sidebar session
// name (#248). The DOM tests in test/dom cover the render logic, but
// jsdom computes no styles at all — every claim this feature makes is a
// CSS claim (the line sits *under* the name, the row still centers its
// dot and swatch, a long title ellipses instead of widening the
// sidebar, a titleless row keeps its old height). Those can only be
// checked where a real layout engine runs.

const LONG_TITLE =
  'deploying the production cluster and waiting for every health check to go green';

async function boot(page: Page, count = 2) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  for (let i = 1; i < count; i++) {
    await page.evaluate((n) => window.__hive.addSession?.(n), `s${i + 1}`);
  }
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) >= n,
    count,
  );
}

// setTitle drives the production path: the daemon reports a title as an
// ordinary SESSION_EVENT(updated), so that is what the spec emits.
async function setTitle(page: Page, index: number, title: string) {
  await page.evaluate(
    ({ index, title }) => {
      const s = window.__hive.state?.sessions[index];
      if (!s) throw new Error(`no mock session at ${index}`);
      s.title = title;
      window.__hive.emit(
        'session:event',
        JSON.stringify({ kind: 'updated', session: s }),
      );
    },
    { index, title },
  );
}

function rows(page: Page) {
  return page.locator('#projects .session-item');
}

test.describe('#248 sidebar window titles', () => {
  test('the title renders on its own line below the session name', async ({
    page,
  }) => {
    await boot(page);
    await setTitle(page, 0, 'npm run build');

    const row = rows(page).first();
    const title = row.locator('.session-title');
    await expect(title).toHaveText('npm run build');

    const nameBox = await row.locator('.name').boundingBox();
    const titleBox = await title.boundingBox();
    if (!nameBox || !titleBox) throw new Error('name or title not laid out');

    // Below, not beside: the top of the title clears the bottom of the
    // name. A flex-direction slip would put them side by side and this
    // is the assertion that catches it.
    expect(titleBox.y).toBeGreaterThanOrEqual(nameBox.y + nameBox.height - 1);
    // Left-aligned with the name, so the rows form one clean column.
    expect(Math.abs(titleBox.x - nameBox.x)).toBeLessThanOrEqual(1);
    // Visibly quieter than the name.
    const nameSize = await row
      .locator('.name')
      .evaluate((el) => Number.parseFloat(getComputedStyle(el).fontSize));
    const titleSize = await title.evaluate((el) =>
      Number.parseFloat(getComputedStyle(el).fontSize),
    );
    expect(titleSize).toBeLessThan(nameSize);
  });

  test('the status dot and color swatch stay clickable on a two-line row', async ({
    page,
  }) => {
    await boot(page);
    await setTitle(page, 0, 'npm run build');

    const row = rows(page).first();
    // AGENTS.md: status dots must appear on every session row. A taller
    // row must not push them out or let the text column cover them —
    // elementFromPoint is the only honest check for that.
    for (const sel of ['.dot', '.swatch']) {
      const box = await row.locator(sel).boundingBox();
      if (!box) throw new Error(`${sel} has no box`);
      const hit = await page.evaluate(
        ({ x, y, sel }) => {
          const el = document.elementFromPoint(x, y);
          return !!el?.closest(sel);
        },
        {
          x: box.x + box.width / 2,
          y: box.y + box.height / 2,
          sel,
        },
      );
      expect(hit, `${sel} is covered at its own center`).toBe(true);
    }
  });

  test('a long title ellipses instead of widening the sidebar', async ({
    page,
  }) => {
    await boot(page);
    const before = await page.locator('#projects').evaluate((el) => ({
      width: el.getBoundingClientRect().width,
      // The list already overflows slightly at rest, independent of this
      // feature. The claim under test is that a long title adds nothing
      // to it — so measure the delta, not the absolute.
      overflow: el.scrollWidth - el.clientWidth,
    }));

    await setTitle(page, 0, LONG_TITLE);
    const title = rows(page).first().locator('.session-title');
    await expect(title).toBeVisible();

    const after = await page
      .locator('#projects')
      .evaluate((el) => el.getBoundingClientRect().width);
    expect(after).toBeCloseTo(before.width, 0);

    const overflow = await title.evaluate((el) => ({
      scroll: el.scrollWidth,
      client: el.clientWidth,
      lines: el.getClientRects().length,
      ellipsis: getComputedStyle(el).textOverflow,
    }));
    // Clipped, on one line, with an ellipsis — not wrapped to two.
    expect(overflow.scroll).toBeGreaterThan(overflow.client);
    expect(overflow.lines).toBe(1);
    expect(overflow.ellipsis).toBe('ellipsis');

    // And the title adds no horizontal overflow of its own.
    const listOverflow = await page
      .locator('#projects')
      .evaluate((el) => el.scrollWidth - el.clientWidth);
    expect(listOverflow).toBeLessThanOrEqual(before.overflow);
  });

  test('a session with no title keeps the row height it always had', async ({
    page,
  }) => {
    await boot(page);
    const heights = await rows(page).evaluateAll((els) =>
      els.map((el) => el.getBoundingClientRect().height),
    );

    // Give the FIRST session a title; the second stays untitled.
    await setTitle(page, 0, 'npm run build');
    await expect(rows(page).first().locator('.session-title')).toBeVisible();

    const after = await rows(page).evaluateAll((els) =>
      els.map((el) => el.getBoundingClientRect().height),
    );
    // The titled row grew, the untitled one did not move at all — no
    // reserved empty slot, no list-wide jitter as titles come and go.
    expect(after[0]).toBeGreaterThan(heights[0]);
    expect(after[1]).toBeCloseTo(heights[1], 1);
    await expect(rows(page).nth(1).locator('.session-title')).toBeHidden();
  });

  test('renaming a session still works with the title line present', async ({
    page,
  }) => {
    await boot(page);
    await setTitle(page, 0, 'npm run build');

    const row = rows(page).first();
    await row.dblclick();
    const input = row.locator('.name-input');
    await expect(input).toBeVisible();

    // The input replaces .name inside the stacked text column; in a
    // column flex `flex: 1` would stretch it vertically instead of
    // filling the row, so check it actually spans the column.
    const inputBox = await input.boundingBox();
    const textBox = await row.locator('.session-text').boundingBox();
    if (!inputBox || !textBox) throw new Error('rename input not laid out');
    expect(inputBox.width).toBeGreaterThan(textBox.width * 0.8);
    // The title stays put underneath while the name is being edited.
    await expect(row.locator('.session-title')).toBeVisible();
  });
});

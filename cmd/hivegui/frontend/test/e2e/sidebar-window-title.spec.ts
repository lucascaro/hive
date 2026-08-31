import { test, expect, type Page } from '@playwright/test';

// Layout coverage for the window-title line under each sidebar session
// name (#248). The DOM tests in test/dom cover the render logic, but
// jsdom computes no styles at all — every claim this feature makes is a
// CSS claim (the line sits *under* the name, the row still centers its
// dot and swatch, a long title ellipses instead of widening the
// sidebar, a titleless row keeps its old height). Those can only be
// checked where a real layout engine runs.

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

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

// setTitle drives the production path: the daemon reports a title under
// its own SESSION_EVENT kind, so that is what the spec emits.
async function setTitle(page: Page, index: number, title: string) {
  await page.evaluate(
    ({ index, title }) => {
      const s = window.__hive.state?.sessions[index];
      if (!s) throw new Error(`no mock session at ${index}`);
      s.title = title;
      window.__hive.emit(
        'session:event',
        JSON.stringify({ kind: 'title', session: s }),
      );
    },
    { index, title },
  );
}

function rows(page: Page) {
  return page.locator('#projects .hv-session-row');
}

test.describe('#248 sidebar window titles', () => {
  test('the title renders on its own line below the session name', async ({
    page,
  }) => {
    await boot(page);
    await setTitle(page, 0, 'npm run build');

    const row = rows(page).first();
    const title = row.locator('.hv-session-row__sub');
    await expect(title).toHaveText('npm run build');

    const nameBox = await row.locator('.hv-session-row__name').boundingBox();
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
      .locator('.hv-session-row__name')
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
    for (const sel of ['.hv-session-row__state', '.hv-session-row__swatch']) {
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
    const title = rows(page).first().locator('.hv-session-row__sub');
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

  // The row is a fixed two-line 40px slot now (components.md ›
  // sessionRow), so a title arriving must move NOTHING — not the row it
  // lands on, not the untitled row below it. That is a strictly stronger
  // form of the old "no list-wide jitter" claim, which only asked that
  // the untitled row stay put while the titled one grew.
  test('a title arriving resizes no row at all', async ({ page }) => {
    await boot(page);
    const heights = await rows(page).evaluateAll((els) =>
      els.map((el) => el.getBoundingClientRect().height),
    );

    // Give the FIRST session a title; the second stays untitled.
    await setTitle(page, 0, 'npm run build');
    await expect(rows(page).first().locator('.hv-session-row__sub')).toHaveText(
      'npm run build',
    );

    const after = await rows(page).evaluateAll((els) =>
      els.map((el) => el.getBoundingClientRect().height),
    );
    expect(after[0]).toBeCloseTo(heights[0], 1);
    expect(after[1]).toBeCloseTo(heights[1], 1);
    // Line 2 of the untitled row is blank, not gone: the slot is always
    // there, so nothing reflows when a title shows up in it.
    await expect(rows(page).nth(1).locator('.hv-session-row__sub')).toHaveText(
      '',
    );
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

    // The input replaces the name inside the stacked text column; in a
    // column flex `flex: 1` would stretch it vertically instead of
    // filling the row, so check it actually spans the column.
    const inputBox = await input.boundingBox();
    const textBox = await row.locator('.hv-session-row__text').boundingBox();
    if (!inputBox || !textBox) throw new Error('rename input not laid out');
    expect(inputBox.width).toBeGreaterThan(textBox.width * 0.8);
    // The title stays put underneath while the name is being edited.
    await expect(row.locator('.hv-session-row__sub')).toBeVisible();
  });
  // New in phase 3: line 2 is never blank-and-hidden — when there is no
  // window title it carries the state words instead (components.md ›
  // sessionRow).
  test('a titleless row shows state words on line 2, not an empty line', async ({
    page,
  }) => {
    await boot(page);
    const row = rows(page).first();
    // A freshly booted mock session is ready and titleless.
    await expect(row.locator('.hv-session-row__sub')).toHaveText('');

    // Kill it: the subtitle becomes the state word, and the name is
    // struck through rather than removed (patterns.md › Exited sessions).
    const sid = await row.getAttribute('data-sid');
    await page.evaluate((id) => {
      const s = window.__hive.state?.sessions.find((x) => x.id === id);
      if (!s) throw new Error('no mock session');
      s.alive = false;
      window.__hive.emit(
        'session:event',
        JSON.stringify({ kind: 'updated', session: s }),
      );
    }, sid);

    await expect(row).toHaveAttribute('data-state', 'exited');
    await expect(row.locator('.hv-session-row__sub')).toHaveText('Exited');
    const deco = await row
      .locator('.hv-session-row__name')
      .evaluate((el) => getComputedStyle(el).textDecorationLine);
    expect(deco).toContain('line-through');
  });

  test('the row is 40px and the key hint shows the number the mod key selects', async ({
    page,
  }) => {
    await boot(page, 3);
    const first = rows(page).first();
    const box = await first.boundingBox();
    expect(box?.height).toBeGreaterThanOrEqual(40);
    await expect(first.locator('.hv-kbd')).toHaveText('[1]');

    const secondSid = await rows(page).nth(1).getAttribute('data-sid');
    await page.keyboard.press(`${MOD}+2`);
    await expect(
      page.locator('.hv-session-row[data-selected]'),
    ).toHaveAttribute('data-sid', secondSid ?? '');
  });
});

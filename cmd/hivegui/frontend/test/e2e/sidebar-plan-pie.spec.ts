import { test, expect, type Page } from '@playwright/test';

// The plan indicator's whole claim is geometric: it takes a cell that
// was already empty, so no row grows and the agent code is untouched.
// vitest/jsdom computes no layout and cannot answer that, which is why
// spec 416 calls it out as a Playwright row. Measured here in a real
// browser, at every density.

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  // A title on every row, so `compact` is dropping a line with content
  // in it — which is what makes the truncation measurement meaningful.
  await page.evaluate(() => {
    for (const s of window.__hive.state?.sessions ?? []) {
      s.title = 'npm run build --watch --verbose --color=always';
      // The seeded session carries no agent, so the row renders no
      // agent code to compare against.
      s.agent = 'claude';
      window.__hive.emit(
        'session:event',
        JSON.stringify({ kind: 'title', session: s }),
      );
    }
  });
  await expect(
    page.locator('#projects .hv-session-row .hv-session-row__agent').first(),
  ).toBeVisible();
}

async function setDensity(page: Page, value: string) {
  await page.keyboard.press(`${MOD}+,`);
  await page.locator('#settings-tab-appearance').click();
  await expect(page.locator('#settings-density')).toBeVisible();
  await page.locator('#settings-density').selectOption(value);
  await page.keyboard.press('Escape');
  await expect(page.locator('#settings-density')).toBeHidden();
}

const row = (page: Page) => page.locator('#projects .hv-session-row').first();
const pie = (page: Page) => row(page).locator('.hv-session-row__plan');

function box(page: Page, selector: string) {
  return row(page)
    .locator(selector)
    .evaluate((el) => {
      const r = el.getBoundingClientRect();
      return {
        top: r.top,
        bottom: r.bottom,
        left: r.left,
        width: r.width,
        height: r.height,
      };
    });
}

const rowHeight = (page: Page) =>
  row(page).evaluate((el) => el.getBoundingClientRect().height);

async function firstSessionId(page: Page) {
  return page.evaluate(() => window.__hive.state?.sessions[0]?.id ?? '');
}

async function setPlan(page: Page, done: number, total: number, tool = '') {
  const id = await firstSessionId(page);
  await page.evaluate(
    ([i, d, t, tool]) =>
      window.__hive.setSessionPlan?.(
        i as string,
        d as number,
        t as number,
        tool as string,
      ),
    [id, done, total, tool] as const,
  );
  if (total > 0) await expect(pie(page)).toBeVisible();
}

async function setSubagents(page: Page, running: number) {
  const id = await firstSessionId(page);
  await page.evaluate(
    ([i, n]) => window.__hive.setSessionSubagents?.(i as string, n as number),
    [id, running] as const,
  );
}

async function setTitle(page: Page, title: string) {
  await page.evaluate((t) => {
    const s = window.__hive.state?.sessions[0];
    if (!s) return;
    s.title = t;
    window.__hive.emit(
      'session:event',
      JSON.stringify({ kind: 'title', session: s }),
    );
  }, title);
}

const badge = (page: Page) => row(page).locator('.hv-session-row__subagents');

test.describe('sidebar plan indicator', () => {
  test('absent until the session has a plan', async ({ page }) => {
    await boot(page);
    await expect(pie(page)).toHaveCount(0);
    await setPlan(page, 1, 4);
    await expect(pie(page)).toHaveCount(1);
  });

  test('does not change row height, at any density', async ({ page }) => {
    await boot(page);

    for (const [density, expected] of [
      ['normal', 40],
      ['tight', 34],
      ['compact', 28],
    ] as const) {
      if (density !== 'normal') await setDensity(page, density);

      // Measure without a plan, then with one. The row must not move.
      await setPlan(page, 0, 0);
      await expect(pie(page)).toHaveCount(0);
      const without = await rowHeight(page);
      expect(without).toBeCloseTo(expected, 0);

      await setPlan(page, 2, 5, 'Bash');
      const withPlan = await rowHeight(page);
      expect(withPlan).toBeCloseTo(expected, 0);
      expect(withPlan).toBe(without);
    }
  });

  test('leaves the agent code untouched', async ({ page }) => {
    await boot(page);
    const before = await row(page)
      .locator('.hv-session-row__agent')
      .evaluate((el) => {
        const cs = getComputedStyle(el);
        return { size: cs.fontSize, family: cs.fontFamily };
      });

    await setPlan(page, 3, 5, 'Edit');

    const after = await row(page)
      .locator('.hv-session-row__agent')
      .evaluate((el) => {
        const cs = getComputedStyle(el);
        return { size: cs.fontSize, family: cs.fontFamily };
      });
    expect(after).toEqual(before);
  });

  test('sits under the state icon at normal density, not over it', async ({
    page,
  }) => {
    await boot(page);
    await setPlan(page, 2, 4, 'Bash');

    const icon = await box(page, '.hv-session-row__state');
    const mark = await box(page, '.hv-session-row__plan');

    // Row 2 is strictly below row 1 — the two boxes must not overlap.
    expect(mark.top).toBeGreaterThanOrEqual(icon.bottom - 1);
    // Same column, so horizontally centred on the icon.
    const iconCentre = icon.left + icon.width / 2;
    const markCentre = mark.left + mark.width / 2;
    expect(Math.abs(iconCentre - markCentre)).toBeLessThan(2);
    // 12px at normal density.
    expect(mark.width).toBeCloseTo(12, 0);
  });

  test('moves beside the state icon at compact, where row 2 does not exist', async ({
    page,
  }) => {
    await boot(page);
    await setDensity(page, 'compact');
    await setPlan(page, 2, 4, 'Bash');

    const icon = await box(page, '.hv-session-row__state');
    const mark = await box(page, '.hv-session-row__plan');

    // Same line now: the boxes overlap vertically instead of stacking.
    expect(mark.top).toBeLessThan(icon.bottom);
    expect(mark.bottom).toBeGreaterThan(icon.top);
    // And to the icon's right, not on top of it.
    expect(mark.left).toBeGreaterThanOrEqual(icon.left + icon.width - 1);
    expect(mark.width).toBeCloseTo(10, 0);
    // The row is still 28px — widening column 1 must not grow it.
    expect(await rowHeight(page)).toBeCloseTo(28, 0);
  });

  test('compact: the width the pie takes comes off the title, and is bounded', async ({
    page,
  }) => {
    await boot(page);
    await setDensity(page, 'compact');

    const titleWidth = () =>
      row(page)
        .locator('.hv-session-row__sub')
        .evaluate((el) => el.getBoundingClientRect().width);

    await setPlan(page, 0, 0);
    const without = await titleWidth();
    await setPlan(page, 2, 5, 'Bash');
    const withPlan = await titleWidth();

    // This is the accepted cost of the compact placement, asserted
    // rather than assumed: column 1 goes 14px → 24px, so the title
    // loses 10px and nothing more.
    expect(without - withPlan).toBeCloseTo(10, 0);
  });

  test('a plan off the hook tier reads as not-live', async ({ page }) => {
    await boot(page);
    const id = await firstSessionId(page);
    await setPlan(page, 2, 4, 'Bash');
    await page.evaluate(
      (i) => window.__hive.setSessionState?.(i, 'working', 'hook'),
      id,
    );
    const live = await pie(page).evaluate((el) =>
      getComputedStyle(el).getPropertyValue('--hv-plan-color').trim(),
    );

    // Back to the heuristic tier: the hook has gone quiet.
    await page.evaluate(
      (i) => window.__hive.setSessionState?.(i, 'working', 'heuristic'),
      id,
    );
    const stale = await pie(page).evaluate((el) =>
      getComputedStyle(el).getPropertyValue('--hv-plan-color').trim(),
    );

    expect(stale).not.toEqual(live);
  });

  test('stays tucked under the icon when row 2 has no title', async ({
    page,
  }) => {
    // Centred in row 2, the mark sank to the row's bottom edge whenever
    // there was no title beside it, and read as a stray blob.
    await boot(page);
    await setTitle(page, '');
    await setPlan(page, 0, 3, 'Bash');
    const icon = await box(page, '.hv-session-row__state');
    const mark = await box(page, '.hv-session-row__plan');
    expect(mark.top - icon.bottom).toBeGreaterThanOrEqual(0);
    expect(mark.top - icon.bottom).toBeLessThanOrEqual(5);
  });

  test('an empty plan is an outline in the state colour, not a grey disc', async ({
    page,
  }) => {
    await boot(page);
    await setPlan(page, 0, 3, 'Bash');
    const paint = await pie(page).evaluate((el) => {
      const cs = getComputedStyle(el);
      return { shadow: cs.boxShadow, image: cs.backgroundImage };
    });
    expect(paint.shadow).toContain('inset');
    expect(paint.image).not.toContain('color-mix');
  });
});

test.describe('sidebar subagent badge', () => {
  test('rides the pie without changing row height, at any density', async ({
    page,
  }) => {
    await boot(page);
    for (const [density, expected] of [
      ['normal', 40],
      ['tight', 34],
      ['compact', 28],
    ] as const) {
      if (density !== 'normal') await setDensity(page, density);
      await setPlan(page, 2, 5, 'Agent');
      await setSubagents(page, 0);
      await expect(badge(page)).toHaveCount(0);
      const without = await rowHeight(page);

      await setSubagents(page, 3);
      await expect(badge(page)).toHaveText('3');
      const withBadge = await rowHeight(page);
      expect(withBadge).toBeCloseTo(expected, 0);
      expect(withBadge).toBe(without);

      // Visible, not clipped by the row or covered by a neighbour: the
      // topmost element at the badge's centre is the badge itself.
      const hit = await badge(page).evaluate((el) => {
        const r = el.getBoundingClientRect();
        const top = document.elementFromPoint(
          r.left + r.width / 2,
          r.top + r.height / 2,
        );
        return top === el;
      });
      expect(hit, `badge is covered or clipped at ${density}`).toBe(true);
    }
  });

  test('shows on an empty outline when there is no plan', async ({ page }) => {
    await boot(page);
    await setPlan(page, 0, 0);
    await expect(pie(page)).toHaveCount(0);
    await setSubagents(page, 2);
    await expect(pie(page)).toHaveCount(1);
    await expect(badge(page)).toHaveText('2');
    expect(await rowHeight(page)).toBeCloseTo(40, 0);
  });
});

import { test, expect, type Page } from '@playwright/test';

// Both sidebar headers pin, and they nest: the project label at the top of
// the scroller, a worktree group's branch header directly beneath it. The
// failure this guards against is invisible in CSS text — an `overflow:
// hidden` on the group panel makes the panel the nearest scroll container,
// so its sticky header sticks to a box that never scrolls and silently does
// nothing.

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

// A group, plus enough plain sessions after it to make the list scroll.
async function seedScrollableGroup(page: Page) {
  await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('alpha', 'feat/sticky'),
  );
  await page.waitForFunction(() =>
    (window.__hive.state?.sessions ?? []).some((s) => !!s.worktree_path),
  );
  const wt = await page.evaluate(
    () =>
      (window.__hive.state?.sessions ?? []).find((s) => !!s.worktree_path)
        ?.worktree_path ?? '',
  );
  await page.evaluate(
    (p) => window.__hive.createSessionInWorktree?.('beta', p),
    wt,
  );
  for (let i = 0; i < 14; i++) {
    await page.evaluate((n) => window.__hive.addSession?.(n), `filler${i}`);
  }
  await page.waitForFunction(
    () => (window.__hive.state?.sessions.length ?? 0) >= 16,
  );
  await page.waitForSelector('.hv-worktree-group__header');
}

test.describe('sticky sidebar headers', () => {
  test('keeps the project label and the group branch on screen', async ({
    page,
  }) => {
    await boot(page);
    await seedScrollableGroup(page);

    const scroller = page.locator('#projects');
    const label = page.locator('.hv-project-card__header').first();
    const header = page.locator('.hv-worktree-group__header').first();

    // Scroll to the middle of the group, so its rows are the ones in view.
    await header.evaluate((el) => {
      const panel = el.closest('.hv-worktree-group');
      const rows = panel?.querySelectorAll('.hv-session-row');
      rows?.[rows.length - 1]?.scrollIntoView({ block: 'center' });
    });

    const boxes = await page.evaluate(() => {
      const r = (sel: string) =>
        document.querySelector(sel)?.getBoundingClientRect();
      const s = r('#projects');
      const l = r('.hv-project-card__header');
      const h = r('.hv-worktree-group__header');
      if (!s || !l || !h) throw new Error('nothing to measure');
      return {
        scroller: { top: s.top, bottom: s.bottom },
        label: { top: l.top, bottom: l.bottom },
        header: { top: h.top, bottom: h.bottom },
      };
    });

    // Both inside the scroller's viewport…
    expect(boxes.label.top).toBeGreaterThanOrEqual(boxes.scroller.top - 1);
    expect(boxes.label.bottom).toBeLessThanOrEqual(boxes.scroller.bottom + 1);
    expect(boxes.header.top).toBeGreaterThanOrEqual(boxes.scroller.top - 1);
    expect(boxes.header.bottom).toBeLessThanOrEqual(boxes.scroller.bottom + 1);
    // …and nested, not stacked on top of each other.
    expect(boxes.header.top).toBeGreaterThanOrEqual(boxes.label.bottom - 1);

    await expect(scroller).toBeVisible();
  });

  test('pins the project label at every scroll position', async ({ page }) => {
    await boot(page);
    await seedScrollableGroup(page);

    const tops: number[] = [];
    for (const y of [0, 60, 160, 320]) {
      await page.locator('#projects').evaluate((el, top) => {
        el.scrollTop = top;
      }, y);
      tops.push(
        await page.evaluate(() => {
          const s = document
            .querySelector('#projects')
            ?.getBoundingClientRect();
          const l = document
            .querySelector('.hv-project-card__header')
            ?.getBoundingClientRect();
          if (!s || !l) throw new Error('nothing to measure');
          return l.top - s.top;
        }),
      );
    }
    // Sticky pins to the scrollport's PADDING box, and #projects carries
    // 4px of it — so "pinned" is 4, not 0. The first entry is the
    // unscrolled position, where the label sits at its natural offset.
    expect(tops[0]).toBeGreaterThan(4);
    for (const t of tops.slice(1)) expect(t).toBeLessThanOrEqual(5);
  });
});

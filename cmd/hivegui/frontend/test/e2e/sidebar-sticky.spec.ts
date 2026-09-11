import { test, expect, type Page } from '@playwright/test';
import { seedScrollableGroup } from './fixtures/seed-worktree-group.js';

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

test.describe('worktree group panel', () => {
  test('the chevron actually hides the rows, and says so', async ({ page }) => {
    await boot(page);
    await seedScrollableGroup(page);
    const panel = page.locator('.hv-worktree-group').first();
    const rows = panel.locator('.hv-session-row');
    await expect(rows.first()).toBeVisible();

    await panel.locator('.hv-worktree-group__chevron').click();
    await expect(rows.first()).toBeHidden();
    await expect(panel.locator('.hv-worktree-group__chevron')).toHaveAttribute(
      'aria-expanded',
      'false',
    );

    await panel.locator('.hv-worktree-group__chevron').click();
    await expect(rows.first()).toBeVisible();
  });

  // AGENTS.md: a session's state must never go silent to save space. The
  // rows are display:none while collapsed, so the header carries the count.
  test('a collapsed panel still reports attention inside it', async ({
    page,
  }) => {
    await boot(page);
    await seedScrollableGroup(page);
    const panel = page.locator('.hv-worktree-group').first();
    await panel.locator('.hv-worktree-group__chevron').click();
    await expect(panel.locator('.hv-worktree-group__alert')).toHaveCount(0);

    await page.evaluate(() => {
      const s = (window.__hive.state?.sessions ?? []).find(
        (x) => !!x.worktree_path,
      );
      if (!s) throw new Error('no worktree session');
      s.needs_attention = true;
      window.__hive.emit(
        'session:event',
        JSON.stringify({ kind: 'attention', session: s }),
      );
    });
    await expect(panel.locator('.hv-worktree-group__alert')).toContainText('1');
  });
});

test.describe('sticky sidebar headers', () => {
  test('keeps the project label and the group branch on screen', async ({
    page,
  }) => {
    await boot(page);
    await seedScrollableGroup(page);

    const header = page.locator('.hv-worktree-group__header').first();

    // Scroll to the middle of the group, so its rows are the ones in view.
    await header.evaluate((el) => {
      const panel = el.closest('.hv-worktree-group');
      const rows = panel?.querySelectorAll('.hv-session-row');
      rows?.[rows.length - 1]?.scrollIntoView({ block: 'center' });
    });
    // The group sits near the top of the list, so centring its last row
    // can clamp scrollTop to 0 — and then every assertion below would
    // hold for a header that is merely sitting where it was laid out.
    // Sticky is only under test once the list has actually scrolled.
    expect(
      await page.locator('#projects').evaluate((el) => el.scrollTop),
    ).toBeGreaterThan(0);

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

import { test, expect, type Locator, type Page } from '@playwright/test';
import { seedScrollableGroup } from './fixtures/seed-worktree-group.js';

// The worktree group is a full-bleed band, not an inset card (issue #392).
// None of this is visible to jsdom — test/dom has no CSS — so geometry is
// the only place the change can be verified at all.
//
// Every assertion below fails against the pre-#392 CSS: the panel's 8px
// side margin and 1px border pushed a member row 9px right of an ungrouped
// row and took 9px off its right edge, and the panel, its header and its
// colour bar all carried --radius-md.

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

// A measurement that cannot silently succeed. A selector that matches
// nothing returns null from boundingBox(), and `null?.left` compared
// against `null?.left` is a passing test that measured nothing — which is
// how the first draft of this file would have failed.
async function box(scope: Page, selector: string) {
  const el: Locator = scope.locator(selector).first();
  await expect(el, `no element matched ${selector}`).toHaveCount(1);
  const b = await el.boundingBox();
  if (!b) throw new Error(`${selector} matched but has no layout box`);
  return b;
}

// Rows are direct children of their list: ProjectCard renders
// ul.hv-project-card__rows and WorktreeGroup renders
// ul.hv-worktree-group__rows. The direct-child combinator is what keeps
// "ungrouped" from also matching the group's members, which are nested
// two lists deeper inside the same card.
const UNGROUPED = '.hv-project-card__rows > li.hv-session-row';
const GROUPED = '.hv-worktree-group__rows > li.hv-session-row';

test.describe('worktree group band', () => {
  test('a grouped row starts at the same x as an ungrouped one', async ({
    page,
  }) => {
    await boot(page);
    await seedScrollableGroup(page);
    const grouped = await box(page, `${GROUPED} .hv-session-row__state`);
    const loose = await box(page, `${UNGROUPED} .hv-session-row__state`);
    expect(Math.abs(grouped.x - loose.x)).toBeLessThanOrEqual(1);
  });

  test('a grouped row gives up no width on the right', async ({ page }) => {
    await boot(page);
    await seedScrollableGroup(page);
    const grouped = await box(page, `${GROUPED} .hv-session-row__sub`);
    const loose = await box(page, `${UNGROUPED} .hv-session-row__sub`);
    const right = (b: { x: number; width: number }) => b.x + b.width;
    expect(Math.abs(right(grouped) - right(loose))).toBeLessThanOrEqual(1);
  });

  test('the band has no rounded corners', async ({ page }) => {
    await boot(page);
    await seedScrollableGroup(page);
    // The default preset. `terminal` zeroes --radius-md (themes.css), so
    // running this there would pass for the wrong reason.
    await expect(page.locator('html')).not.toHaveAttribute(
      'data-theme',
      'terminal',
    );
    const radii = await page.evaluate(() => {
      const panel = document.querySelector('.hv-worktree-group');
      const header = document.querySelector('.hv-worktree-group__header');
      if (!panel || !header) throw new Error('no worktree group on screen');
      return {
        panel: getComputedStyle(panel).borderRadius,
        header: getComputedStyle(header).borderRadius,
        // The shared colour bar, which the card treatment also rounded.
        bar: getComputedStyle(panel, '::after').borderRadius,
      };
    });
    expect(radii).toEqual({ panel: '0px', header: '0px', bar: '0px' });
  });

  test('the sticky group header paints its own ground', async ({ page }) => {
    await boot(page);
    await seedScrollableGroup(page);
    // Member rows scroll UNDER this header, so it needs a fully opaque
    // background of its own — background-color does not inherit, and the
    // panel's identical ground does not paint behind a sticky child that
    // has moved. Hit-testing cannot see this (elementFromPoint returns the
    // header whether or not it is painted) and neither can a bare
    // "not transparent" check, which rgba(…, 0.6) passes while still
    // showing the rows through. Alpha is the assertion.
    const alpha = await page.evaluate(() => {
      const header = document.querySelector('.hv-worktree-group__header');
      if (!header) throw new Error('no worktree group on screen');
      const bg = getComputedStyle(header).backgroundColor;
      const m = bg.match(/^rgba?\(([^)]+)\)$/);
      if (!m) throw new Error(`unparseable background-color: ${bg}`);
      const parts = m[1].split(',').map((s) => Number.parseFloat(s.trim()));
      return parts.length === 4 ? parts[3] : 1;
    });
    expect(alpha).toBe(1);
  });

  // A named group puts TWO things in a header cell that was sized for
  // one, at the 220px sidebar floor (issue #395). jsdom cannot see this:
  // test/dom has no CSS, so the only place the squeeze can surface is a
  // real layout.
  //
  // toBeVisible() would be vacuous here — a branch ellipsed to zero width
  // is still "visible" to Playwright. The assertions are measurements:
  // the branch keeps a real box, that box stays inside the header, and
  // the header does not scroll horizontally.
  test('a named group keeps its branch legible at the 220px floor', async ({
    page,
  }) => {
    await boot(page);
    await seedScrollableGroup(page);

    const sidebar = await page
      .locator('#sidebar')
      .evaluate((el) => el.getBoundingClientRect().width);
    expect(sidebar, 'sidebar is not at its design floor').toBeLessThanOrEqual(
      221,
    );

    const before = await box(page, '.hv-worktree-group__branch');
    expect(before.width, 'branch has no width before naming').toBeGreaterThan(
      12,
    );

    // Name the group through the same bridge call the sidebar makes.
    await page.evaluate(() => {
      const s = (window.__hive.state?.sessions ?? []).find(
        (x) => !!x.worktree_path,
      );
      if (!s) throw new Error('no worktree session seeded');
      return window.__hive.setWorktreeLabel?.(
        s.project_id ?? 'p1',
        s.worktree_path ?? '',
        'authentication refactor',
      );
    });
    await page.waitForSelector('.hv-worktree-group__label');

    const label = await box(page, '.hv-worktree-group__label');
    expect(label.width, 'the name rendered with no width').toBeGreaterThan(12);

    const after = await box(page, '.hv-worktree-group__branch');
    expect(
      after.width,
      'the name squeezed the branch out of the header',
    ).toBeGreaterThan(12);

    // Both sit inside the header's box rather than spilling past it.
    const header = await box(page, '.hv-worktree-group__header');
    expect(after.x + after.width).toBeLessThanOrEqual(
      header.x + header.width + 1,
    );
    expect(label.x).toBeGreaterThanOrEqual(header.x - 1);

    // And the header itself does not scroll sideways.
    const overflow = await page.evaluate(() => {
      const h = document.querySelector('.hv-worktree-group__header');
      if (!h) throw new Error('no group header');
      return h.scrollWidth - h.clientWidth;
    });
    expect(overflow, 'the group header overflows horizontally').toBeLessThanOrEqual(1);
  });
});

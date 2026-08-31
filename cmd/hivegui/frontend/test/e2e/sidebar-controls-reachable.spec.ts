import { test, expect, type Page } from '@playwright/test';

// Two hover-reveal regressions that only a real browser can see: jsdom
// applies no CSS, so `display: none` on the revealed containers is
// invisible to the unit suite.
//
//  1. `.hv-session-row__actions` used `display: none` -> `display: flex`.
//     `display: none` also removes the buttons from the tab order, and
//     a row without a worktree has NOTHING focusable before them, so
//     `:focus-within` could never fire from a forward Tab. Minimize /
//     kill / restart were reachable only by shift-tabbing backwards out
//     of the colour swatch. Fixed by stacking meta+actions in one grid
//     cell and swapping opacity + pointer-events instead.
//  2. `.hv-project-card__actions` shipped five 24px icon buttons where
//     the legacy stylesheet shrank them to 18px, leaving the project
//     name ~6px wide at the 220px sidebar floor whenever the header was
//     hovered.

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.hv-project-card').length > 0,
  );
}

test('forward Tab reaches a session row action button on a worktree-less row', async ({
  page,
}) => {
  await boot(page);

  const row = page.locator('#projects li.hv-session-row').first();
  // The default mock session has no worktree — the common case, and the
  // one with no focusable element before the actions container.
  await expect(row.locator('.hv-session-row__worktree')).toHaveCount(0);

  // focus.ts arms a 500ms guard that yanks focus back to the active
  // terminal; let it lapse so this measures the tab order, not the guard.
  await page.waitForTimeout(700);
  await page.locator('.hv-project-card__chevron').first().focus();
  await expect(page.locator('.hv-project-card__chevron').first()).toBeFocused();

  // Walk forward. The project header's own five actions come first
  // (the chevron's focus reveals them), then the row.
  const seen: string[] = [];
  for (let i = 0; i < 16; i++) {
    await page.keyboard.press('Tab');
    const id = await page.evaluate(() => {
      const ae = document.activeElement;
      if (!(ae instanceof HTMLElement)) return 'none';
      const scope = ae.closest('.hv-session-row')
        ? 'row'
        : ae.closest('.hv-project-card__header')
          ? 'header'
          : 'other';
      return `${scope}:${ae.dataset.action ?? ae.tagName.toLowerCase()}`;
    });
    seen.push(id);
    if (id === 'row:minimize') break;
  }

  expect(seen, `tab order was ${seen.join(' -> ')}`).toContain('row:minimize');
});

test('the project name stays legible while the header is hovered at the 220px floor', async ({
  page,
}) => {
  await boot(page);

  const card = page.locator('#projects li.hv-project-card').first();
  const header = card.locator('.hv-project-card__header');
  const name = card.locator('.hv-project-card__name');

  // Sanity: the sidebar is at its design floor, so this is the tightest
  // the header ever gets.
  const sidebar = await page
    .locator('#sidebar')
    .evaluate((el) => el.getBoundingClientRect().width);
  expect(sidebar).toBeLessThanOrEqual(221);

  await header.hover();
  await expect(card.locator('.hv-project-card__actions')).toBeVisible();

  const box = await name.boundingBox();
  expect(box, 'project name has no box while hovered').not.toBeNull();
  // Measured: 3px before the 18px rule was re-established, 33px after —
  // "a couple of characters, narrow but present", which is exactly what
  // the legacy stylesheet's comment claimed for the same 18px box.
  expect(box?.width ?? 0).toBeGreaterThan(24);
});

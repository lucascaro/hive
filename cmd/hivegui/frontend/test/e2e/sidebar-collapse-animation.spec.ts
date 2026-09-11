import { test, expect, type Page } from '@playwright/test';

// Collapsing a project card or a worktree group animates rather than
// snapping. The measurement that matters is not "is there a transition
// property" — it is that the box is at an intermediate height partway
// through, and that the clip which makes that read as a slide is NOT left
// on afterwards (a permanent overflow:hidden here makes the box the
// nearest scroll container and silently kills the sticky headers inside
// it — the exact failure test/e2e/sidebar-sticky.spec.ts guards).

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.hv-project-card').length > 0,
  );
}

function heightOf(page: Page, sel: string) {
  return page.locator(sel).evaluate((el) => el.getBoundingClientRect().height);
}

test.describe('collapse animation', () => {
  test('a project card animates shut instead of snapping', async ({ page }) => {
    await boot(page);
    const body = '.hv-project-card__body';
    const open = await heightOf(page, body);
    expect(open).toBeGreaterThan(0);

    await page.locator('.hv-project-card__chevron').first().click();
    // Partway through: shrinking, but not yet gone.
    await page.waitForTimeout(60);
    const mid = await heightOf(page, body);
    expect(mid).toBeLessThan(open);
    expect(mid).toBeGreaterThan(0);

    await expect.poll(() => heightOf(page, body)).toBe(0);
  });

  test('a worktree group animates, and drops the clip when it settles', async ({
    page,
  }) => {
    await boot(page);
    await page.evaluate(() =>
      window.__hive.createSessionWithWorktree?.('alpha', 'feat/anim'),
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
    await page.waitForSelector('.hv-worktree-group__header');

    const body = '.hv-worktree-group__body';
    const open = await heightOf(page, body);
    await page.locator('.hv-worktree-group__chevron').first().click();
    await page.waitForTimeout(60);
    const mid = await heightOf(page, body);
    expect(mid).toBeLessThan(open);
    expect(mid).toBeGreaterThan(0);
    await expect.poll(() => heightOf(page, body)).toBe(0);

    // Expand again, and the clip must come back off — otherwise every
    // sticky header inside is dead.
    await page.locator('.hv-worktree-group__chevron').first().click();
    await expect.poll(() => heightOf(page, body)).toBeGreaterThan(0);
    await expect
      .poll(() =>
        page
          .locator('.hv-project-card__body')
          .evaluate((el) => getComputedStyle(el).overflow),
      )
      .toBe('visible');
    await expect
      .poll(() =>
        page.locator(body).evaluate((el) => getComputedStyle(el).overflow),
      )
      .toBe('visible');
  });
});

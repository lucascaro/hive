import { test, expect, type Page } from '@playwright/test';

// Collapsing a project card or a worktree group animates rather than
// snapping. The measurement that matters is not "is there a transition
// property" — it is that the box is at an intermediate height partway
// through, and that the clip which makes that read as a slide is NOT left
// on afterwards (a permanent overflow:hidden here makes the box the
// nearest scroll container and silently kills the sticky headers inside
// it — the exact failure test/e2e/sidebar-sticky.spec.ts guards).

// Slow the transition down for the duration of a test. Sampling "partway
// through" against the real 160ms is a race the CI runner wins: the click
// round-trip alone can outrun the animation, and the intermediate height
// is then 0. An inline custom property beats the stylesheet (and the
// reduced-motion media query), so the window is deterministic everywhere.
async function slowCollapse(page: Page, ms = 2000) {
  await page.evaluate((v) => {
    document.documentElement.style.setProperty('--motion-collapse', v);
  }, `${ms}ms`);
}

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
  // With motion reduced there is no transition to clip for, so the clip must
  // never be armed: `overflow: hidden` on the project body makes it the
  // nearest scroll container and the sticky group headers inside it stop
  // working for as long as it lasts.
  test('arms no clip window when motion is reduced', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await boot(page);
    const card = page.locator('.hv-project-card').first();
    await card.locator('.hv-project-card__chevron').click();
    await card.locator('.hv-project-card__chevron').click();
    // Read it the frame after the click, where the 160ms timer would still
    // be holding the clip on.
    const state = await card.evaluate((el) => ({
      animating: el.hasAttribute('data-animating'),
      overflow: getComputedStyle(
        el.querySelector('.hv-project-card__body') as HTMLElement,
      ).overflow,
    }));
    expect(state.animating).toBe(false);
    expect(state.overflow).toBe('visible');
  });

  test('a project card animates shut instead of snapping', async ({ page }) => {
    await boot(page);
    await slowCollapse(page);
    const body = '.hv-project-card__body';
    const open = await heightOf(page, body);
    expect(open).toBeGreaterThan(0);

    await page.locator('.hv-project-card__chevron').first().click();
    // Partway through: shrinking, but not yet gone.
    await page.waitForTimeout(300);
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

    await slowCollapse(page);
    const body = '.hv-worktree-group__body';
    const open = await heightOf(page, body);
    await page.locator('.hv-worktree-group__chevron').first().click();
    await page.waitForTimeout(300);
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

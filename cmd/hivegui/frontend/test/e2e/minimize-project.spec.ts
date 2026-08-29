import { test, expect, type Page } from '@playwright/test';

// Layout check for the minimized-projects tray. The DOM test proves the
// rows move; only a real browser proves the tray actually sits at the
// bottom of the sidebar — #projects has to take the free space, or the
// tray floats directly under the last project row instead of hugging
// the footer.

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.project').length > 0,
  );
}

test('a minimized project drops to a tray at the bottom of the sidebar', async ({
  page,
}) => {
  await boot(page);
  const listed = page.locator('#projects > li.project');
  const before = await listed.count();
  const first = listed.first();
  const pid = await first.getAttribute('data-pid');

  await first.locator('.project-actions button[aria-label^="Minimize"]').click();

  await expect(listed).toHaveCount(before - 1);
  const chip = page.locator(`.min-project-chip[data-pid="${pid}"]`);
  await expect(chip).toBeVisible();

  // The tray hugs the bottom: its box sits below the project list and
  // above the version footer, with no gap left between it and the
  // footer beyond the footer's own height.
  const boxes = await page.evaluate(() => {
    const r = (sel: string) =>
      document.querySelector(sel)?.getBoundingClientRect();
    return {
      list: r('#projects')?.bottom ?? 0,
      trayTop: r('#minimized-projects')?.top ?? 0,
      trayBottom: r('#minimized-projects')?.bottom ?? 0,
      footerTop: r('#sidebar-hints')?.top ?? 0,
    };
  });
  expect(boxes.trayTop).toBeGreaterThanOrEqual(boxes.list - 1);
  expect(Math.abs(boxes.trayBottom - boxes.footerTop)).toBeLessThan(2);

  // Restore puts the row back.
  await chip.locator('.min-project-restore').click();
  await expect(listed).toHaveCount(before);
  await expect(page.locator('#minimized-projects')).toBeHidden();
});

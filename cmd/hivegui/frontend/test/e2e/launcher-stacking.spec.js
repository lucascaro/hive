import { test, expect } from '@playwright/test';

// Regression: the agent launcher (#launcher) must paint ABOVE the
// sidebar resize strip (#sidebar-resizer) where they overlap. They are
// both positioned with z-index 20 but live in the same (root) stacking
// context, so the launcher — later in the DOM — wins the tie. When the
// launcher sat at z-index 10 the 5px resize strip painted over the menu.
//
// Asserted via elementFromPoint at the real overlap rather than by
// reading the z-index value, so it catches the actual paint result (a
// future stacking-context change on an ancestor could break layering
// without touching either z-index).
test('agent launcher paints above the sidebar resize bar', async ({ page }) => {
  await page.goto('/');
  await page.waitForFunction(() => document.querySelectorAll('#projects li').length > 0);

  const mod = process.platform === 'darwin' ? 'Meta' : 'Control';
  await page.keyboard.press(`${mod}+t`);
  await page.waitForSelector('#launcher:not(.hidden) .launcher-item', { timeout: 5000 });

  const probe = await page.evaluate(() => {
    const L = document.getElementById('launcher');
    const R = document.getElementById('sidebar-resizer');
    const lr = L.getBoundingClientRect();
    const rr = R.getBoundingClientRect();
    const ox1 = Math.max(lr.left, rr.left), ox2 = Math.min(lr.right, rr.right);
    const oy1 = Math.max(lr.top, rr.top), oy2 = Math.min(lr.bottom, rr.bottom);
    const overlaps = ox2 > ox1 && oy2 > oy1;
    if (!overlaps) return { overlaps: false, within: 'n/a' };
    const el = document.elementFromPoint((ox1 + ox2) / 2, (oy1 + oy2) / 2);
    let n = el, within = 'neither';
    while (n) {
      if (n.id === 'launcher') { within = 'launcher'; break; }
      if (n.id === 'sidebar-resizer') { within = 'resizer'; break; }
      n = n.parentElement;
    }
    return { overlaps: true, within };
  });

  // The menu must actually overlap the strip for this test to be
  // meaningful — if it stops overlapping, the geometry changed and this
  // guard needs revisiting rather than silently passing.
  expect(probe.overlaps, 'launcher must overlap the resize strip').toBe(true);
  expect(probe.within, 'the resize strip must not paint over the launcher').toBe('launcher');
});

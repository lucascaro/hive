import { test, expect } from '@playwright/test';

// Regression: the agent launcher (#launcher) must paint ABOVE the
// sidebar resize strip (#sidebar-resizer) where they overlap.
//
// The strip is transparent until hovered, when it transitions to an
// amber background — so the bug ("the drag bar shows on top") is only
// visible WHILE the strip is hovered. The earlier version of this test
// used elementFromPoint without hovering and so never exercised the
// failing state. Here we hover the strip and read the ACTUAL pixel the
// browser paints over the launcher: it must be the launcher's dark
// background, not the strip's amber.
//
// Verified to FAIL at the old z-index 10 (the overlap pixel comes back
// amber) and PASS once #launcher clears the strip.
const isAmber = ([r, g, b]) => r > 60 && r > b + 30; // warm + clearly not gray

test('agent launcher paints above the sidebar resize bar (even when hovered)', async ({ page }) => {
  await page.goto('/');
  await page.waitForFunction(() => document.querySelectorAll('#projects li').length > 0);

  const mod = process.platform === 'darwin' ? 'Meta' : 'Control';
  await page.keyboard.press(`${mod}+t`);
  await page.waitForSelector('#launcher:not(.hidden) .launcher-item', { timeout: 5000 });

  const pts = await page.evaluate(() => {
    const L = document.getElementById('launcher').getBoundingClientRect();
    const R = document.getElementById('sidebar-resizer').getBoundingClientRect();
    const overlapX = (Math.max(L.left, R.left) + Math.min(L.right, R.right)) / 2;
    const overlapY = (Math.max(L.top, R.top) + Math.min(L.bottom, R.bottom)) / 2;
    const overlaps = Math.min(L.right, R.right) > Math.max(L.left, R.left)
      && Math.min(L.bottom, R.bottom) > Math.max(L.top, R.top);
    return {
      overlaps, overlapX, overlapY,
      stripX: (R.left + R.right) / 2,
      stripBelowY: Math.min(R.bottom - 20, L.bottom + 200), // pure strip, below the menu
    };
  });
  expect(pts.overlaps, 'launcher must overlap the resize strip for this test to mean anything').toBe(true);

  // Hover the strip on a segment that is NOT under the menu, so the whole
  // strip (including the part crossing the menu) takes its amber :hover bg.
  await page.mouse.move(pts.stripX, pts.stripBelowY);
  await page.waitForTimeout(250); // 120ms background transition + margin

  const buf = await page.screenshot();
  const { overOverlap, overStrip } = await page.evaluate(async ({ b64, pts }) => {
    const img = new Image();
    await new Promise((res, rej) => { img.onload = res; img.onerror = rej; img.src = 'data:image/png;base64,' + b64; });
    const c = document.createElement('canvas');
    c.width = img.width; c.height = img.height;
    const ctx = c.getContext('2d');
    ctx.drawImage(img, 0, 0);
    const px = (x, y) => Array.from(ctx.getImageData(Math.round(x), Math.round(y), 1, 1).data);
    return { overOverlap: px(pts.overlapX, pts.overlapY), overStrip: px(pts.stripX, pts.stripBelowY) };
  }, { b64: buf.toString('base64'), pts });

  // Sanity: the hover actually engaged (the bare strip is amber). Without
  // this, a no-op hover would let the real assertion pass for free.
  expect(isAmber(overStrip), `hover should make the bare strip amber, got ${overStrip}`).toBe(true);
  // The real assertion: the strip's amber must NOT bleed over the menu.
  expect(isAmber(overOverlap), `resize strip painted over the launcher, got ${overOverlap}`).toBe(false);
});

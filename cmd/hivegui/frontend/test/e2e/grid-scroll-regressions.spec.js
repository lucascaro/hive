import { test, expect } from '@playwright/test';

// Regression coverage for #208 (R1, R2, plus an R-control case that
// pins the legitimate window-resize replay path so the fix can't
// accidentally swallow it).
//
//   R1 — First grid-mode entry after restart fires a spurious
//        scrollback replay because the baseline was captured against
//        the xterm default (80) before fit. Symptom: scroll in tiles
//        is broken on first entry.
//   R2 — Minimizing one tile in a grid reflows the remaining tiles;
//        ResizeObserver fires _onBodyResize on each, crossing the
//        4-col threshold against the now-stale baseline, again
//        firing a spurious replay. Symptom: scrollback drops/dupes
//        in the surviving tiles.
//   R-control — A real window resize that crosses the threshold MUST
//        still trigger a replay (the existing #200 behavior). The
//        fix guards rebaseline to layout-driven contexts only; this
//        test enforces that guard.

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

async function bootWithSessions(page, count = 2) {
  await page.goto('/');
  await page.waitForFunction(() => document.querySelectorAll('#projects li').length > 0);
  for (let i = 1; i < count; i++) {
    await page.evaluate((n) => window.__hive.addSession(n), `s${i + 1}`);
  }
  await page.waitForFunction((n) => window.__hive.state.sessions.length >= n, count);
  await page.evaluate(() => { window.__hive.resetStdin(); window.__hive.resetReplay(); });
}

async function enterGridAll(page) {
  await page.keyboard.press(`${MOD}+Shift+g`);
  await expect(page.locator('#terms')).toHaveClass(/grid/);
}

// Pause long enough for any debounced replay (REPLAY_DEBOUNCE_MS = 100ms)
// to have fired or been cancelled. 250ms is comfortably past that window
// while still keeping the test fast.
async function settleReplay(page) {
  await page.waitForTimeout(250);
}

test.describe('#208 grid-mode scroll regressions', () => {
  test('R1: cold-start in grid mode settles without endless replays', async ({ page }) => {
    // Pre-seed grid-all so the very first render is the grid. This
    // exercises ensureAttached's rebaselineReplayCols('first-attach')
    // hook on tiles that have no prior baseline. Without the hook,
    // every subsequent fit (DPR / visibility / RO retry) trips the
    // 4-col threshold against a stale 80-default baseline and fires
    // replay-after-replay. With the hook, replays settle quickly.
    await page.addInitScript(() => {
      try { localStorage.setItem('hive.view', 'grid-all'); } catch {}
    });
    await bootWithSessions(page, 2);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    // Let initial-attach + RO cascades settle, then reset the
    // counter and measure steady-state.
    await page.waitForTimeout(400);
    await page.evaluate(() => window.__hive.resetReplay());
    // After settle, idle ResizeObserver re-fires must not produce
    // any further replays. Pre-fix this could fire repeatedly as
    // DPR / fit jitter kept crossing the stale baseline.
    await page.waitForTimeout(300);
    const replays = await page.evaluate(() => window.__hive.replayCount());
    expect(replays).toBe(0);
  });

  test('R2: minimizing one tile in a 3-tile grid does not fire a spurious replay in the remaining tiles', async ({ page }) => {
    await bootWithSessions(page, 3);
    await enterGridAll(page);
    // Let initial-attach replays (if any) settle, then reset the
    // counter so the minimize step is measured in isolation.
    await settleReplay(page);
    await page.evaluate(() => window.__hive.resetReplay());

    // Minimize the first tile. The remaining two tiles' column widths
    // change as the grid reflows — pre-fix this triggered replay.
    const firstTile = page.locator('.term-host.in-grid').first();
    await firstTile.locator('.tile-minimize').click();
    await expect(page.locator('.term-host.in-grid')).toHaveCount(2);
    await settleReplay(page);

    const replays = await page.evaluate(() => window.__hive.replayCount());
    expect(replays).toBe(0);
  });

  test('R2 restore: restoring a minimized tile does not fire a spurious replay in the others', async ({ page }) => {
    await bootWithSessions(page, 3);
    await enterGridAll(page);
    const firstTile = page.locator('.term-host.in-grid').first();
    const sid = await firstTile.evaluate((el) => el.dataset.sid);
    await firstTile.locator('.tile-minimize').click();
    await expect(page.locator('.term-host.in-grid')).toHaveCount(2);
    await settleReplay(page);
    await page.evaluate(() => window.__hive.resetReplay());

    // Restore from tray; remaining tiles narrow again.
    await page.locator(`#minimized-tray .min-chip[data-sid="${sid}"]`).click();
    await expect(page.locator('.term-host.in-grid')).toHaveCount(3);
    await settleReplay(page);

    const replays = await page.evaluate(() => window.__hive.replayCount());
    expect(replays).toBe(0);
  });

  test('R-control: a real window resize that materially changes tile width still triggers a scrollback replay', async ({ page }) => {
    await bootWithSessions(page, 2);
    await enterGridAll(page);
    await settleReplay(page);
    await page.evaluate(() => window.__hive.resetReplay());

    // Bring up the viewport from a narrow size to a wide size; tile
    // cols change by well over the 4-col threshold.
    await page.setViewportSize({ width: 600, height: 600 });
    await page.waitForTimeout(50);
    await page.setViewportSize({ width: 1400, height: 800 });
    // Allow the 100ms debounce + a margin to settle.
    await page.waitForTimeout(400);

    const replays = await page.evaluate(() => window.__hive.replayCount());
    // At least one tile must have requested a replay — that's the
    // intended #200 behavior. The fix must not have killed it.
    expect(replays).toBeGreaterThan(0);
  });
});

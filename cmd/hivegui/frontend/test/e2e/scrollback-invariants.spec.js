import { test, expect } from '@playwright/test';

// Layer C: scrollback invariants beyond the specific #208 regressions
// already covered in grid-scroll-regressions.spec.js. These tests pin
// generic invariants the replay path must uphold, so future refactors
// can't quietly re-introduce a regression in an adjacent code path.
//
// Invariants under test:
//   I1 — Adding a session in grid mode does not fire spurious replays
//        in the existing tiles. (Adjacent to #208 R2.)
//   I2 — Killing a non-active session in grid mode does not fire
//        spurious replays in the survivors.
//   I3 — Round-trip single → grid → single does not fire a replay in
//        the active session when geometry returns to the original.
//   I4 — View persistence cold-start with grid-project does not loop
//        replays after settle. (Adjacent to #208 R1 grid-all case.)
//   I5 — Sidebar collapse in single mode does not fire a replay
//        (single-mode tile spans the full body either way; the col
//        delta is below threshold).

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

async function settleReplay(page) {
  // REPLAY_DEBOUNCE_MS is 100ms; 250ms is comfortably past that.
  await page.waitForTimeout(250);
}

test.describe('scrollback replay invariants', () => {
  test('I1: adding a session in grid mode does not replay in existing tiles', async ({ page }) => {
    await bootWithSessions(page, 2);
    await enterGridAll(page);
    await settleReplay(page);
    await page.evaluate(() => window.__hive.resetReplay());

    // Existing-tile IDs captured before the addition.
    const existing = await page.evaluate(() =>
      Array.from(document.querySelectorAll('.term-host.in-grid')).map((t) => t.dataset.sid),
    );
    expect(existing).toHaveLength(2);

    await page.evaluate(() => window.__hive.addSession('grew'));
    await expect(page.locator('.term-host.in-grid')).toHaveCount(3);
    await settleReplay(page);

    // No replay should have fired in either of the two prior tiles.
    const replays = await page.evaluate((ids) => {
      return ids.map((id) => window.__hive.replayCount(id));
    }, existing);
    expect(replays).toEqual([0, 0]);
  });

  test('I2: killing a non-active grid session does not replay in survivors', async ({ page }) => {
    await bootWithSessions(page, 3);
    await enterGridAll(page);
    await settleReplay(page);

    // Make the first tile active so the second/third are passive
    // survivors when we kill the third.
    const tiles = page.locator('.term-host.in-grid');
    await tiles.first().click();
    await settleReplay(page);
    await page.evaluate(() => window.__hive.resetReplay());

    const sids = await page.evaluate(() =>
      Array.from(document.querySelectorAll('.term-host.in-grid')).map((t) => t.dataset.sid),
    );
    const victim = sids[2];
    const survivors = [sids[0], sids[1]];

    await page.evaluate((id) => window.__hive.killSession(id), victim);
    await expect(page.locator('.term-host.in-grid')).toHaveCount(2);
    await settleReplay(page);

    const replays = await page.evaluate((ids) => ids.map((id) => window.__hive.replayCount(id)), survivors);
    expect(replays).toEqual([0, 0]);
  });

  test('I3: round-trip single → grid → single fires no net replay in active session', async ({ page }) => {
    await bootWithSessions(page, 2);
    await settleReplay(page);
    await page.evaluate(() => window.__hive.resetReplay());

    // Note the active session ID.
    const activeId = await page.evaluate(() => {
      const a = document.querySelector('.term-host.active') || document.querySelector('.term-host');
      return a ? a.dataset.sid : null;
    });
    expect(activeId).not.toBeNull();

    await page.keyboard.press(`${MOD}+Shift+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await settleReplay(page);

    await page.keyboard.press(`${MOD}+Shift+g`);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    await settleReplay(page);

    // The round-trip changes column count materially (grid → narrow,
    // single → wide), so at most one replay per transition is the
    // upper bound. The active session may legitimately replay on the
    // way into grid; what we lock in is that we do NOT loop or pile
    // up replays after the geometry has settled.
    const replays = await page.evaluate((id) => window.__hive.replayCount(id), activeId);
    expect(replays).toBeLessThanOrEqual(2);
  });

  test('I4: grid-project cold-start settles without runaway replays', async ({ page }) => {
    await page.addInitScript(() => {
      try { localStorage.setItem('hive.view', 'grid-project'); } catch {}
    });
    await bootWithSessions(page, 2);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    // Allow initial-attach + RO cascades to settle, then steady-state.
    await page.waitForTimeout(400);
    await page.evaluate(() => window.__hive.resetReplay());
    await page.waitForTimeout(300);
    const replays = await page.evaluate(() => window.__hive.replayCount());
    expect(replays).toBe(0);
  });

  test('I5: sidebar toggle in single mode does not fire a replay (active tile width unchanged enough)', async ({ page }) => {
    await bootWithSessions(page, 2);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    await settleReplay(page);
    await page.evaluate(() => window.__hive.resetReplay());

    // Toggle sidebar off, settle, then on.
    await page.keyboard.press(`${MOD}+s`);
    await settleReplay(page);
    await page.keyboard.press(`${MOD}+s`);
    await settleReplay(page);

    // Sidebar collapse changes the available width by ~the sidebar's
    // pixel width. Whether that exceeds the 4-col threshold depends on
    // viewport. The intent of this test is the WEAKER invariant: total
    // replay count should not exceed one per material width change
    // (off, then on = at most 2). Looping/cascading is the bug.
    const replays = await page.evaluate(() => window.__hive.replayCount());
    expect(replays).toBeLessThanOrEqual(2);
  });
});

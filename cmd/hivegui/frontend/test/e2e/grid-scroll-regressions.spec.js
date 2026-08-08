import { test, expect } from '@playwright/test';

// Regression coverage for #208 (R1, R2) plus both halves of the
// resize-replay contract — R-follow (must not replay a follower) and
// R-reader (must still replay a reader), so no fix can quietly swallow
// the legitimate window-resize replay path.
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
//   R-follow — A real window resize must NOT replay a tile that is
//        following the bottom (the bounce fix). The reflow-replay only
//        matters for a reader scrolled up into history; for a follower it
//        just thrashes the viewport under live output.
//   R-reader — The positive control that keeps the three zero-assertions
//        above honest: same resize, tile scrolled UP, replay MUST fire.
//        Without it nothing here would notice the replay path going dead.

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

async function bootWithSessions(page, count = 2) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  for (let i = 1; i < count; i++) {
    await page.evaluate((n) => window.__hive.addSession(n), `s${i + 1}`);
  }
  await page.waitForFunction(
    (n) => window.__hive.state.sessions.length >= n,
    count,
  );
  await page.evaluate(() => {
    window.__hive.resetStdin();
    window.__hive.resetReplay();
  });
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
  test('R1: cold-start in grid mode settles without endless replays', async ({
    page,
  }) => {
    // Pre-seed grid-all so the very first render is the grid. This
    // exercises ensureAttached's rebaselineReplayCols('first-attach')
    // hook on tiles that have no prior baseline. Without the hook,
    // every subsequent fit (DPR / visibility / RO retry) trips the
    // 4-col threshold against a stale 80-default baseline and fires
    // replay-after-replay. With the hook, replays settle quickly.
    await page.addInitScript(() => {
      try {
        localStorage.setItem('hive.view', 'grid-all');
      } catch {}
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

  test('R2: minimizing one tile in a 3-tile grid does not fire a spurious replay in the remaining tiles', async ({
    page,
  }) => {
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

  test('R2 restore: restoring a minimized tile does not fire a spurious replay in the others', async ({
    page,
  }) => {
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

  // R-follow: the bounce fix. The reflow-replay (#200) re-wraps user-facing
  // HISTORY at the new width — but a follower is looking at the newest output,
  // not history, so replaying for them just tears the buffer down under live
  // output and makes the viewport thrash (the "lots of scrolling on mode
  // switch" report). New contract: a real width-changing resize does NOT
  // replay a tile that is following the bottom. The complementary case —
  // a tile scrolled UP into history DOES replay — is R-reader below.
  test('R-follow: a real window resize does NOT replay a tile that is following the bottom', async ({
    page,
  }) => {
    await bootWithSessions(page, 2);
    await enterGridAll(page);
    await settleReplay(page);
    await page.evaluate(() => window.__hive.resetReplay());

    // Freshly-attached tiles are followers (_followBottom = true). A pure
    // width change must not replay any of them.
    await page.setViewportSize({ width: 600, height: 600 });
    await page.waitForTimeout(50);
    await page.setViewportSize({ width: 1400, height: 800 });
    await page.waitForTimeout(400);

    const replays = await page.evaluate(() => window.__hive.replayCount());
    expect(replays).toBe(0);
  });

  // R-reader: the positive control, and the other half of R-follow's
  // contract. Three of the four tests above assert `replays === 0`, so on
  // their own they would all still pass if the replay request path were
  // dead — a broken ResizeObserver, a never-armed debounce, a
  // RequestScrollbackReplay that never fires. This test is what makes the
  // zeroes mean something: same width change, tile scrolled UP into
  // history, replay MUST fire.
  //
  // Runs in SINGLE view on purpose. In grid, the container ResizeObserver
  // routes a viewport change through renderGrid → ensureAttached, which
  // re-latches _followBottom to true for every tile (see attachDeferred) —
  // so a grid tile cannot be held scrolled-up across a resize by design.
  // The single-view observer early-returns (view.js), so the scrolled-up
  // state survives and the real wiring is exercised end to end.
  test('R-reader: a real window resize DOES replay a tile scrolled up into history', async ({
    page,
  }) => {
    await bootWithSessions(page, 1);
    await settleReplay(page);

    // Give the term real scrollback, then scroll up with a genuine wheel
    // gesture — the app's own capture-phase handler is what stamps
    // _lastUserScrollTs, and only a gesture-attributed move may clear
    // follow-intent (parse-driven drift deliberately cannot).
    const scrolledUp = await page.evaluate(async () => {
      const st = window.__hive_state.terms.get(window.__hive_state.activeId);
      await new Promise((r) => st.term.write('line\r\n'.repeat(500), r));
      st.host.dispatchEvent(
        new WheelEvent('wheel', {
          deltaY: -600,
          deltaMode: 0,
          bubbles: true,
          cancelable: true,
        }),
      );
      return st._followBottom;
    });
    expect(scrolledUp).toBe(false);

    await page.evaluate(() => window.__hive.resetReplay());
    await page.setViewportSize({ width: 1400, height: 800 });
    await page.waitForTimeout(400);

    // Still a reader (nothing yanked them back), and the reflow replay fired.
    expect(
      await page.evaluate(
        () =>
          window.__hive_state.terms.get(window.__hive_state.activeId)
            ._followBottom,
      ),
    ).toBe(false);
    expect(
      await page.evaluate(() => window.__hive.replayCount()),
    ).toBeGreaterThan(0);
  });

  // R-drag: the bottom re-pin must not fight a selection drag. xterm
  // auto-scrolls the viewport while the button is held past the top edge,
  // and that scroll carries no wheel and no keydown — so before the
  // pointer-down latch, the re-pin classified it as parse drift and
  // snapped straight back, making it impossible to select upwards.
  // Confirmed by hand against a live PTY before this test was written.
  test('R-drag: an upward viewport move with the pointer held is NOT re-pinned to the bottom', async ({
    page,
  }) => {
    await bootWithSessions(page, 1);
    await settleReplay(page);

    await page.evaluate(async () => {
      const st = window.__hive_state.terms.get(window.__hive_state.activeId);
      await new Promise((r) => st.term.write('line\r\n'.repeat(500), r));
      st.term.scrollToBottom();
      st.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    });
    // Hold well past USER_SCROLL_GRACE_MS (250ms) before the viewport
    // moves. This is the case that needs the pointer-down latch rather
    // than a mousedown timestamp: xterm's auto-scroll repeats on its own
    // timer while the button is held STILL, so there is no later event to
    // re-stamp from and the grace window would have expired.
    await page.waitForTimeout(400);

    const { viewportY, baseY, following } = await page.evaluate(() => {
      const st = window.__hive_state.terms.get(window.__hive_state.activeId);
      // Move the viewport the way the auto-scroll does — programmatically,
      // with no wheel or key event of its own.
      st.term.scrollLines(-30);
      const buf = st.term.buffer.active;
      return {
        viewportY: buf.viewportY,
        baseY: buf.baseY,
        following: st._followBottom,
      };
    });
    // Still up in history, and follow-intent released — a gesture-driven
    // move away from the bottom is exactly what clears it.
    expect(baseY - viewportY).toBeGreaterThan(1);
    expect(following).toBe(false);

    // Releasing outside the tile must clear the latch, or every later
    // cap-trim drift would read as user-driven.
    expect(
      await page.evaluate(() => {
        window.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
        return window.__hive_state.terms.get(window.__hive_state.activeId)
          ._pointerDown;
      }),
    ).toBe(false);
  });
});

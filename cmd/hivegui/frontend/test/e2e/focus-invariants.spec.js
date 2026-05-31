import { test, expect } from '@playwright/test';

// Layer C: focus invariants across UI actions that aren't already
// covered by focus.spec.js (single↔grid + keystrokes) or by
// sidebar-focus-regression.spec.js (#208 R3 in grid mode).
//
// The contract we lock in: after ANY layout-mutating UI action,
// document.activeElement is the live xterm-helper-textarea inside a
// .term-host carrying .term-focused. The visual focus and keyboard
// focus must stay aligned. Drift between them is the regression
// pattern behind #181 / #182 / #208.
//
// Gaps filled:
//   F1 — Adding a session in grid mode preserves focus on the prior
//        active tile.
//   F2 — Killing a non-active session preserves focus on the active
//        tile (no migration needed, but the kill triggers a reflow).
//   F3 — Sidebar toggle in single mode preserves keyboard focus
//        (#208 R3 only tested grid).
//   F4 — Adding a project does not steal focus.

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

async function bootWithSessions(page, count = 2) {
  await page.goto('/');
  await page.waitForFunction(() => document.querySelectorAll('#projects li').length > 0);
  for (let i = 1; i < count; i++) {
    await page.evaluate((n) => window.__hive.addSession(n), `s${i + 1}`);
  }
  await page.waitForFunction((n) => window.__hive.state.sessions.length >= n, count);
  await page.evaluate(() => window.__hive.resetStdin());
}

async function enterGridAll(page) {
  await page.keyboard.press(`${MOD}+Shift+g`);
  await expect(page.locator('#terms')).toHaveClass(/grid/);
}

async function waitForHelperFocus(page, timeout = 2000) {
  await page.waitForFunction(() => {
    const ae = document.activeElement;
    return !!(ae && ae.classList && ae.classList.contains('xterm-helper-textarea'));
  }, null, { timeout });
}

// Assert focus is on a helper-textarea inside a tile carrying
// .term-focused (the unified keyboard+visual contract).
async function assertAlignedFocus(page, timeout = 2000) {
  await page.waitForFunction(() => {
    const ae = document.activeElement;
    if (!ae || !ae.classList || !ae.classList.contains('xterm-helper-textarea')) return false;
    const host = ae.closest('.term-host');
    return !!(host && host.classList.contains('term-focused'));
  }, null, { timeout });
}

test.describe('focus invariants', () => {
  test('F1: adding a session in grid mode keeps focus aligned (helper textarea inside .term-focused)', async ({ page }) => {
    // The app intentionally moves focus to the newly-added session;
    // the invariant we lock in is the WEAKER but more important one:
    // wherever focus lands, it MUST be on a helper-textarea inside a
    // tile carrying .term-focused. Drift between visual and keyboard
    // focus is the #181/#208 regression pattern.
    await bootWithSessions(page, 2);
    await enterGridAll(page);
    await waitForHelperFocus(page);

    await page.evaluate(() => window.__hive.addSession('newcomer'));
    await expect(page.locator('.term-host.in-grid')).toHaveCount(3);
    await assertAlignedFocus(page);
  });

  test('F2: killing a non-active session preserves focus on the active tile', async ({ page }) => {
    await bootWithSessions(page, 3);
    await enterGridAll(page);
    await waitForHelperFocus(page);

    // Focus the first tile explicitly.
    const tiles = page.locator('.term-host.in-grid');
    await tiles.first().click();
    await waitForHelperFocus(page);
    const activeSid = await page.evaluate(() => {
      const host = document.activeElement?.closest('.term-host');
      return host ? host.dataset.sid : null;
    });

    // Kill a different (non-active) session.
    const victim = await tiles.nth(2).evaluate((el) => el.dataset.sid);
    expect(victim).not.toBe(activeSid);
    await page.evaluate((id) => window.__hive.killSession(id), victim);
    await expect(page.locator('.term-host.in-grid')).toHaveCount(2);
    await assertAlignedFocus(page);

    const after = await page.evaluate(() => {
      const host = document.activeElement?.closest('.term-host');
      return host ? host.dataset.sid : null;
    });
    expect(after).toBe(activeSid);
  });

  test('F3: sidebar toggle in single mode preserves keyboard focus', async ({ page }) => {
    // #208 R3 only covered grid mode + post-window-resize. This
    // exercises the simpler single-mode path so toggleSidebar's
    // refocus stays universal.
    await bootWithSessions(page, 2);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    await waitForHelperFocus(page);

    await page.keyboard.press(`${MOD}+s`);
    await page.waitForTimeout(80);
    await page.keyboard.press(`${MOD}+s`);
    await assertAlignedFocus(page);

    // Typing should reach the active session.
    await page.evaluate(() => window.__hive.resetStdin());
    await page.keyboard.type('single');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()), { timeout: 2000 })
      .toContain('single');
  });

  test('F4: creating a project does not steal focus', async ({ page }) => {
    await bootWithSessions(page, 2);
    await waitForHelperFocus(page);

    await page.evaluate(() => window.__hive.emit('project:event', JSON.stringify({
      kind: 'added',
      project: {
        id: 'p-new', name: 'fresh', color: '#0ff', cwd: '',
        order: 1, created: new Date().toISOString(),
      },
    })));
    // Add takes one rAF to render in the sidebar.
    await page.waitForTimeout(50);
    await assertAlignedFocus(page);
  });
});

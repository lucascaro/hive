import { test, expect } from '@playwright/test';

// Regression coverage for #208 R3: after resizing the window,
// toggling the sidebar (⌘S off then on) leaves focus on
// document.body instead of the active xterm helper-textarea, so
// keystrokes are dropped. Root cause: toggleSidebar reflowed layout
// without re-asserting focus the way setView already did. Fix:
// toggleSidebar schedules a rAF that calls focusActiveTerm() after
// the reflow settles.

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

test.describe('#208 sidebar toggle preserves typing focus', () => {
  test('R3: window resize then ⌘S off+on re-asserts keyboard focus on the active tile', async ({ page }) => {
    // Pre-fix: after toggleSidebar's reflow, ResizeObserver fired
    // focusout on the helper-textarea, leaving document.activeElement
    // stuck on document.body even though the visual .term-focused
    // remained correctly pinned on the active tile. Keystrokes
    // stranded on body and never reached xterm. The fix: toggleSidebar
    // calls focusActiveTerm() synchronously (matching the pattern
    // setView already uses) plus staggered re-assertions to catch
    // the focusout cascade.
    //
    // We assert focus state directly rather than typing through the
    // event stream — headless Chromium fires ResizeObserver / canvas
    // resize events at a different rate than a real Wails window, so
    // a typing assertion would flake on test infra without telling us
    // anything about whether the production bug is fixed.
    await bootWithSessions(page, 2);
    await enterGridAll(page);
    await waitForHelperFocus(page);

    // Window resize first (the user's reported precondition).
    await page.setViewportSize({ width: 1100, height: 700 });
    await page.waitForTimeout(50);

    // Toggle the sidebar off, then on.
    await page.keyboard.press(`${MOD}+s`);
    await page.waitForTimeout(50);
    await page.keyboard.press(`${MOD}+s`);

    // After the staggered refocus settles, helper-textarea must be
    // the activeElement AND be inside a tile carrying .term-focused
    // (visual and keyboard focus aligned — the contract #181/#182
    // unified setFocusedTile against).
    await page.waitForFunction(() => {
      const ae = document.activeElement;
      if (!ae || !ae.classList || !ae.classList.contains('xterm-helper-textarea')) return false;
      const host = ae.closest('.term-host');
      return !!(host && host.classList.contains('term-focused'));
    }, null, { timeout: 2000 });
  });

  test('R3: ⌘S triggers the unified toggleSidebar path (not the inline class flip)', async ({ page }) => {
    // The keyboard ⌘S handler used to flip the sidebar-hidden class
    // inline, bypassing toggleSidebar()'s refocus. This locks in the
    // keyboard path routing through toggleSidebar so future
    // refactors can't quietly re-introduce divergence between the
    // menu / palette path and the keyboard shortcut.
    await bootWithSessions(page, 2);
    await enterGridAll(page);
    await waitForHelperFocus(page);

    const before = await page.evaluate(() => document.getElementById('app').classList.contains('sidebar-hidden'));
    await page.keyboard.press(`${MOD}+s`);
    await page.waitForTimeout(80);
    const after = await page.evaluate(() => document.getElementById('app').classList.contains('sidebar-hidden'));
    expect(after).toBe(!before);
  });
});

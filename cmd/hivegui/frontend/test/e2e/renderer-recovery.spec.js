import { test, expect } from '@playwright/test';

// Layer C: integration smoke for the WebGL context-loss recovery
// path (#190 / #198). The recovery helpers themselves are unit-
// tested in test/unit/renderer-recovery.test.js — this spec covers
// the missing piece: that a real WEBGL_lose_context event on a real
// xterm canvas, in a real DOM, doesn't throw, doesn't leave the
// terminal mute, and doesn't surface console errors.
//
// We can't meaningfully assert "the pixels look right" in headless
// Chromium without an image diff stack, so we lean on behavioural
// invariants: typing still routes to stdin after the context loss.

async function bootWithSessions(page, count = 2) {
  await page.goto('/');
  await page.waitForFunction(() => document.querySelectorAll('#projects li').length > 0);
  for (let i = 1; i < count; i++) {
    await page.evaluate((n) => window.__hive.addSession(n), `s${i + 1}`);
  }
  await page.waitForFunction((n) => window.__hive.state.sessions.length >= n, count);
  await page.evaluate(() => window.__hive.resetStdin());
}

async function waitForHelperFocus(page, timeout = 2000) {
  await page.waitForFunction(() => {
    const ae = document.activeElement;
    return !!(ae && ae.classList && ae.classList.contains('xterm-helper-textarea'));
  }, null, { timeout });
}

test.describe('renderer context-loss recovery', () => {
  test('simulated WebGL context loss does not break input or surface errors', async ({ page }) => {
    const consoleErrors = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text());
    });
    page.on('pageerror', (err) => consoleErrors.push(`pageerror: ${err.message}`));

    await bootWithSessions(page, 1);
    await waitForHelperFocus(page);

    // Try to fire WEBGL_lose_context against the active term's canvas.
    // Headless Chromium DOES expose this extension; if for some reason
    // it isn't available (skipped renderer init, DOM fallback path),
    // skip the test rather than failing — the unit tests still cover
    // the recovery helper's contract.
    const lost = await page.evaluate(() => {
      const canvas = document.querySelector('.term-host.active .xterm canvas')
        || document.querySelector('.term-host .xterm canvas');
      if (!canvas) return false;
      const gl = canvas.getContext('webgl2') || canvas.getContext('webgl');
      if (!gl) return false;
      const ext = gl.getExtension('WEBGL_lose_context');
      if (!ext) return false;
      ext.loseContext();
      // Restore later so the recovery cycle (dispose → reattach →
      // refresh) has a chance to land cleanly.
      setTimeout(() => { try { ext.restoreContext(); } catch {} }, 50);
      return true;
    });
    test.skip(!lost, 'WEBGL_lose_context not available in this Chromium build');

    // Give the recovery cycle a moment to settle.
    await page.waitForTimeout(300);
    await waitForHelperFocus(page);

    // Typing still routes — the integration didn't go mute.
    await page.evaluate(() => window.__hive.resetStdin());
    await page.keyboard.type('recover');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()), { timeout: 2000 })
      .toContain('recover');

    expect(consoleErrors, `console errors:\n${consoleErrors.join('\n')}`).toEqual([]);
  });
});

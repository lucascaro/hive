import { test, expect } from '@playwright/test';

// Session back/forward (Ctrl+- / Ctrl+Shift+-) in a real browser.
//
// This exists because the jsdom test can't cover the chord that
// actually ships on macOS. jsdom reports a non-mac navigator, so
// lib/platform.js isMac is false there and only the Ctrl+Alt+- branch
// is exercised. Chromium on darwin reports "MacIntel", so this spec
// drives the real mac chord — plain Ctrl+-, no Alt — through the real
// capture-phase keydown listener.
//
// The load-bearing detail: app/keyboard.js gates most bindings behind
// cmdOrCtrl(), which rejects plain Ctrl on macOS. The nav dispatch is
// placed AHEAD of that gate for exactly this reason. A regression that
// moves it below the gate passes every unit test and silently kills
// the feature on the primary platform.

const isMac = process.platform === 'darwin';
const MOD = isMac ? 'Meta' : 'Control';
// The shipping chord on this platform: Ctrl+- on mac, Ctrl+Alt+- elsewhere.
const BACK = isMac ? 'Control+-' : 'Control+Alt+-';
const FORWARD = isMac ? 'Control+Shift+-' : 'Control+Alt+Shift+-';

async function bootWithSessions(page, count) {
  await page.goto('/');
  await page.waitForFunction(() => document.querySelectorAll('#projects li').length > 0);
  for (let i = 1; i < count; i++) {
    await page.evaluate((n) => window.__hive.addSession(n), `s${i + 1}`);
  }
  await page.waitForFunction((n) => window.__hive_state.sessions.length >= n, count);
}

const activeId = (page) => page.evaluate(() => window.__hive_state.activeId);
const sessionIds = (page) => page.evaluate(
  () => window.__hive_state.sessions.map((s) => s.id),
);

test.describe('session back / forward history', () => {
  test('walks back and forward through clicked sessions', async ({ page }) => {
    await bootWithSessions(page, 3);
    const [a, b, c] = await sessionIds(page);

    // Click, not keyboard — the sidebar path is what users actually do,
    // and it reaches setActive through switchTo.
    for (const id of [a, b, c]) {
      await page.click(`#projects li[data-sid="${id}"]`);
      await expect.poll(() => activeId(page)).toBe(id);
    }

    await page.keyboard.press(BACK);
    await expect.poll(() => activeId(page)).toBe(b);
    await page.keyboard.press(BACK);
    await expect.poll(() => activeId(page)).toBe(a);
    await page.keyboard.press(FORWARD);
    await expect.poll(() => activeId(page)).toBe(b);
    await page.keyboard.press(FORWARD);
    await expect.poll(() => activeId(page)).toBe(c);
  });

  test('does not steal the platform zoom chord', async ({ page }) => {
    // On mac zoom is ⌘-; on Windows/Linux it is Ctrl+-, which is why
    // this feature takes Ctrl+Alt+- there. Either way the zoom chord
    // must change the font size and must NOT navigate.
    await bootWithSessions(page, 2);
    const [a, b] = await sessionIds(page);
    await page.click(`#projects li[data-sid="${a}"]`);
    await page.click(`#projects li[data-sid="${b}"]`);
    await expect.poll(() => activeId(page)).toBe(b);

    const before = await page.evaluate(() => window.__hive_state.fontSize);
    await page.keyboard.press(`${MOD}+-`);
    await expect.poll(() => page.evaluate(() => window.__hive_state.fontSize)).toBe(before - 1);
    expect(await activeId(page)).toBe(b); // did not navigate

    await page.keyboard.press(`${MOD}+0`); // restore
  });

  test('records ⌘1-9 switches too, not just clicks', async ({ page }) => {
    await bootWithSessions(page, 3);
    const [a, b] = await sessionIds(page);

    await page.keyboard.press(`${MOD}+1`);
    await expect.poll(() => activeId(page)).toBe(a);
    await page.keyboard.press(`${MOD}+2`);
    await expect.poll(() => activeId(page)).toBe(b);

    await page.keyboard.press(BACK);
    await expect.poll(() => activeId(page)).toBe(a);
  });
});

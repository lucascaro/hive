import { test, expect, type Page } from '@playwright/test';

// E2E coverage for #327: ⌘/Ctrl+Enter focuses the active session from a
// grid view, and is unbound everywhere else.
//
// History: #249 unbound the chord outright. It had toggled single ⇄
// grid-project (mirroring ⌘G) and was swallowed by the capture-phase
// window handler before xterm could see it, which made it unusable inside
// an agent session. #327 re-bound it for grid views ONLY — so the
// single-view half stays unclaimed and the key still reaches the agent,
// which is the whole point of the carve-out. ⌘G / ⇧⌘G remain the toggles.

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

async function bootWithSessions(page: Page, count = 2) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  for (let i = 1; i < count; i++) {
    await page.evaluate((n) => window.__hive.addSession?.(n), `s${i + 1}`);
  }
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) >= n,
    count,
  );
  await page.evaluate(() => window.__hive.resetStdin());
}

// The view is read off #terms' class list, the same signal focus.spec.ts
// asserts on: grid modes add a `grid` class, focused mode does not.
const termsClass = (page: Page) =>
  page.evaluate(() => {
    const terms = document.getElementById('terms');
    if (!terms) throw new Error('#terms missing');
    return terms.className;
  });

test.describe('#327 ⌘Enter focuses the active session from grid', () => {
  // The load-bearing half: in focused mode the chord belongs to the agent
  // CLI, so the app must not claim it. Regression guard for #217/#327.
  test('⌘Enter in single mode does not enter grid', async ({ page }) => {
    await bootWithSessions(page, 2);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    const before = await termsClass(page);

    await page.keyboard.press(`${MOD}+Enter`);
    // No transition to wait for, so give the handler a frame to be wrong in.
    await page.waitForTimeout(250);

    expect(await termsClass(page)).toBe(before);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });

  test('⌘Enter in grid mode focuses the active session', async ({ page }) => {
    await bootWithSessions(page, 2);
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);

    await page.keyboard.press(`${MOD}+Enter`);

    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });

  // One-way: ⌘Enter must not become a second ⌘G. Pressing it again from
  // the single view it just produced has to leave you there.
  test('⌘Enter does not toggle back into grid', async ({ page }) => {
    await bootWithSessions(page, 2);
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await page.keyboard.press(`${MOD}+Enter`);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    const before = await termsClass(page);

    await page.keyboard.press(`${MOD}+Enter`);
    await page.waitForTimeout(250);

    expect(await termsClass(page)).toBe(before);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });

  // The control: proves ⌘Enter's one-way binding left the real grid
  // toggles working.
  test('⌘G still toggles grid ⇄ single', async ({ page }) => {
    await bootWithSessions(page, 2);
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });
});

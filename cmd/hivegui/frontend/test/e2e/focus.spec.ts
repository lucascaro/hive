import { test, expect, type Page } from '@playwright/test';

// E2E coverage for the grid-mode focus pipeline (#159, #181, #186).
// The Wails mock loads a real xterm so .xterm-helper-textarea exists
// and behaves as in production. We assert focus state via
// page.evaluate so the assertion sees the live DOM.

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
  // Reset stdinLog so the count starts fresh.
  await page.evaluate(() => window.__hive.resetStdin());
}

// Inspect live focus state. Returns:
//   { termsClass, focusedHosts, activeIsHelper, activeInsideFocusedHost }
async function focusState(page: Page) {
  return page.evaluate(() => {
    const terms = document.getElementById('terms');
    const focusedHosts = Array.from(
      document.querySelectorAll<HTMLElement>('.term-host.term-focused'),
    ).map(
      (h) => h.dataset.sessionId || h.getAttribute('data-id') || '<unknown>',
    );
    const ae = document.activeElement;
    const activeIsHelper = !!ae?.classList?.contains('xterm-helper-textarea');
    const focusedHost = ae ? ae.closest('.term-host') : null;
    const focusedHasClass = focusedHost
      ? focusedHost.classList.contains('term-focused')
      : false;
    return {
      termsClass: terms ? terms.className : '',
      focusedHosts,
      activeIsHelper,
      activeInsideFocusedHost: focusedHasClass,
    };
  });
}

test.describe('grid-mode focus pipeline', () => {
  test('single → grid-all preserves keyboard focus on the active session', async ({
    page,
  }) => {
    await bootWithSessions(page, 2);
    // Confirm we're in single mode and an xterm helper is focused.
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    await page.keyboard.press(`${MOD}+Shift+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    // Allow setFocusedTile's rAF (+ at most one retry rAF) to settle.
    await page.waitForFunction(
      () => {
        const ae = document.activeElement;
        return !!ae?.classList?.contains('xterm-helper-textarea');
      },
      null,
      { timeout: 2000 },
    );
    const fs = await focusState(page);
    expect(fs.activeIsHelper).toBe(true);
    expect(fs.activeInsideFocusedHost).toBe(true);
    expect(fs.focusedHosts.length).toBe(1);
  });

  test('single → grid-project preserves keyboard focus', async ({ page }) => {
    await bootWithSessions(page, 2);
    // ⌘G, not ⌘Enter: the ⌘Enter grid toggle was unbound in #249. This
    // test is about the focus pipeline, so it just needs any route into
    // grid-project mode.
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await page.waitForFunction(
      () => {
        const ae = document.activeElement;
        return !!ae?.classList?.contains('xterm-helper-textarea');
      },
      null,
      { timeout: 2000 },
    );
    const fs = await focusState(page);
    expect(fs.activeIsHelper).toBe(true);
    expect(fs.activeInsideFocusedHost).toBe(true);
  });

  test('keystrokes reach the active session after single → grid-all (the user-visible bug)', async ({
    page,
  }) => {
    await bootWithSessions(page, 2);
    await page.keyboard.press(`${MOD}+Shift+g`);
    await page.waitForFunction(
      () => {
        const ae = document.activeElement;
        return !!ae?.classList?.contains('xterm-helper-textarea');
      },
      null,
      { timeout: 2000 },
    );
    await page.keyboard.type('hello');
    // The active session's id at boot is the first session (s1 in the mock).
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()), {
        timeout: 2000,
      })
      .toContain('hello');
  });

  test('round-trip: single → grid → single keeps focus and keystrokes wired', async ({
    page,
  }) => {
    await bootWithSessions(page, 2);
    await page.keyboard.press(`${MOD}+Shift+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await page.keyboard.press(`${MOD}+Shift+g`);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    await page.waitForFunction(
      () => {
        const ae = document.activeElement;
        return !!ae?.classList?.contains('xterm-helper-textarea');
      },
      null,
      { timeout: 2000 },
    );
    await page.evaluate(() => window.__hive.resetStdin());
    await page.keyboard.type('back');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()), {
        timeout: 2000,
      })
      .toContain('back');
  });

  test('cold-start in grid mode receives keystrokes (#187 persistence path)', async ({
    page,
  }) => {
    // Pre-seed the persisted view BEFORE the app boots.
    await page.addInitScript(() => {
      try {
        localStorage.setItem('hive.view', 'grid-all');
      } catch {}
    });
    await bootWithSessions(page, 2);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await page.waitForFunction(
      () => {
        const ae = document.activeElement;
        return !!ae?.classList?.contains('xterm-helper-textarea');
      },
      null,
      { timeout: 3000 },
    );
    await page.keyboard.type('cold');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()), {
        timeout: 2000,
      })
      .toContain('cold');
  });
});

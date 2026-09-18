import { test, expect, type Page } from '@playwright/test';

// Spec 431: ⌘F / Ctrl+Shift+F opens one find box whose source is chosen
// from the terminal's buffer type.
//
// Driven off macOS throughout. On macOS the real entry point is the
// native menu accelerator, which intercepts the key before the webview
// — Playwright drives the webview directly, so pressing Meta+F here
// would exercise a path production never takes. That accelerator is a
// stated coverage gap, checked by hand in the built app; what the
// menu's handler does once fired is covered by the dom suite.

async function bootAsLinux(page: Page) {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'platform', { get: () => 'Linux x86_64' });
    Object.defineProperty(navigator, 'userAgentData', {
      get: () => ({ platform: 'Linux' }),
    });
  });
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await page.waitForFunction(
    () => document.activeElement?.classList?.contains('xterm-helper-textarea'),
    null,
    { timeout: 3000 },
  );
  await page.evaluate(() => window.__hive.resetStdin());
}

/** The focused session's id. */
async function activeId(page: Page): Promise<string> {
  return page.evaluate(() => {
    const el = document.querySelector('.term-focused');
    return el?.getAttribute('data-sid') ?? '';
  });
}

// The focus contract from focus-invariants.spec.ts: keyboard focus sits
// on a helper textarea inside a tile that carries .term-focused. This is
// criterion 4's real assertion — "restores the terminal to its prior
// state" is not observable as anything else from out here.
async function assertAlignedFocus(page: Page) {
  await expect
    .poll(
      () =>
        page.evaluate(() => {
          const ae = document.activeElement as HTMLElement | null;
          if (!ae?.classList.contains('xterm-helper-textarea')) return false;
          return Boolean(ae.closest('.term-focused'));
        }),
      { timeout: 3000 },
    )
    .toBe(true);
}

test.describe('spec 431 find in session', () => {
  // Plain Ctrl+F is 0x06 — readline's forward-char. Taking it would
  // break moving the cursor right in every shell and agent input line.
  test('plain Ctrl+F still reaches the terminal as forward-char', async ({
    page,
  }) => {
    await bootAsLinux(page);
    await page.keyboard.press('Control+f');
    await expect
      .poll(() =>
        page.evaluate(() =>
          [...window.__hive.stdinText()].map((c) => c.charCodeAt(0)),
        ),
      )
      .toEqual([0x06]);
    await expect(page.locator('.hv-find')).toHaveCount(0);
  });

  test('Ctrl+Shift+F opens the box and Escape closes it', async ({ page }) => {
    await bootAsLinux(page);

    await page.keyboard.press('Control+Shift+f');
    await expect(page.locator('.hv-find')).toBeVisible();

    // Criterion 1: the input is focused with no click.
    await expect
      .poll(() =>
        page.evaluate(() =>
          Boolean(
            (document.activeElement as HTMLElement | null)?.hasAttribute(
              'data-find-input',
            ),
          ),
        ),
      )
      .toBe(true);

    await page.keyboard.press('Escape');
    await expect(page.locator('.hv-find')).toHaveCount(0);

    // Criterion 4: the terminal has its keyboard focus back, on a tile
    // still marked focused.
    await assertAlignedFocus(page);
  });

  test('the close control dismisses it too', async ({ page }) => {
    await bootAsLinux(page);
    await page.keyboard.press('Control+Shift+f');
    await expect(page.locator('.hv-find')).toBeVisible();

    await page.locator('[data-find-close]').click();
    await expect(page.locator('.hv-find')).toHaveCount(0);
    await assertAlignedFocus(page);
  });

  // Criterion 7, the normal-buffer half: a shell session searches the
  // terminal, not a transcript, and is never asked which.
  test('a normal-buffer session uses the terminal as its source', async ({
    page,
  }) => {
    await bootAsLinux(page);
    await page.keyboard.press('Control+Shift+f');
    await expect(page.locator('[data-find-source="buffer"]')).toBeVisible();
  });

  // Criterion 7, the alt-screen half, plus criterion 6: the transcript
  // is the source, and it finds text the terminal buffer does not hold.
  test('an alt-screen session searches its transcript', async ({ page }) => {
    await bootAsLinux(page);
    const id = await activeId(page);
    expect(id).not.toBe('');

    await page.evaluate((sid) => {
      window.__hive.setTranscript?.(sid, [
        'first line of the conversation',
        'a needle that scrolled away long ago',
        'last line',
      ]);
      // Put the terminal on the alternate screen (DECSET 1049), which
      // is what an agent does and what makes the buffer unsearchable.
      // ESC is built at runtime: a literal escape byte here would not
      // survive serialization into the page.
      const esc = String.fromCharCode(27);
      window.__hive.emit('pty:data', sid, btoa(esc + '[?1049h'));
    }, id);

    await page.keyboard.press('Control+Shift+f');
    await expect(page.locator('[data-find-source="transcript"]')).toBeVisible();

    await page.locator('[data-find-input]').fill('needle');

    // The match is found even though it is nowhere in the one screenful
    // the alternate buffer holds.
    await expect(page.locator('[data-find-count]')).toHaveText('1/1');
    await expect(page.locator('.hv-find-hit')).toHaveText('needle');
  });

  // Criterion 9: a session with no readable transcript says so, rather
  // than showing an empty box that looks broken.
  test('an alt-screen session with no transcript says so', async ({ page }) => {
    await bootAsLinux(page);
    const id = await activeId(page);

    await page.evaluate((sid) => {
      // Put the terminal on the alternate screen (DECSET 1049), which
      // is what an agent does and what makes the buffer unsearchable.
      // ESC is built at runtime: a literal escape byte here would not
      // survive serialization into the page.
      const esc = String.fromCharCode(27);
      window.__hive.emit('pty:data', sid, btoa(esc + '[?1049h'));
    }, id);

    await page.keyboard.press('Control+Shift+f');
    await expect(page.locator('[data-find-unavailable]')).toBeVisible();
    await expect(page.locator('[data-find-unavailable]')).toContainText(
      'No searchable history',
    );
  });

  // Criterion 3: Enter and Shift+Enter move the active match.
  test('Enter and Shift+Enter step between matches', async ({ page }) => {
    await bootAsLinux(page);
    const id = await activeId(page);

    await page.evaluate((sid) => {
      window.__hive.setTranscript?.(sid, [
        'needle one',
        'filler',
        'needle two',
        'filler',
        'needle three',
      ]);
      // Put the terminal on the alternate screen (DECSET 1049), which
      // is what an agent does and what makes the buffer unsearchable.
      // ESC is built at runtime: a literal escape byte here would not
      // survive serialization into the page.
      const esc = String.fromCharCode(27);
      window.__hive.emit('pty:data', sid, btoa(esc + '[?1049h'));
    }, id);

    await page.keyboard.press('Control+Shift+f');
    await page.locator('[data-find-input]').fill('needle');
    await expect(page.locator('[data-find-count]')).toHaveText('1/3');

    await page.keyboard.press('Enter');
    await expect(page.locator('[data-find-count]')).toHaveText('2/3');

    await page.keyboard.press('Shift+Enter');
    await expect(page.locator('[data-find-count]')).toHaveText('1/3');

    // And it wraps rather than sticking at the ends.
    await page.keyboard.press('Shift+Enter');
    await expect(page.locator('[data-find-count]')).toHaveText('3/3');
  });
});

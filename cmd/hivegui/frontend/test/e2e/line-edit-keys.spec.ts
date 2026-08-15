import { test, expect, type Page } from '@playwright/test';

// ⌘←/⌘→ must reach the PTY as start/end-of-line, end to end.
//
// PR #274 stopped the app from swallowing these keys in focused mode,
// which was necessary but not sufficient: xterm.js emits NOTHING for a
// meta-modified arrow (`case 37: if (e.metaKey) break`), so the keys went
// from "switched sessions" to "did nothing at all". The sequence has to be
// written by the app. This spec asserts the bytes on the wire, because
// that is the only layer where the difference between "not intercepted"
// and "actually works" is visible.
//
// macOS only: elsewhere the chord is Ctrl+←/→, which means word-wise
// movement and which xterm already encodes itself.
const isMac = process.platform === 'darwin';

const LINE_START = '\x01'; // Ctrl+A
const LINE_END = '\x05'; // Ctrl+E

async function bootFocusedTerminal(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await page.waitForFunction(
    () =>
      !!document.activeElement?.classList?.contains('xterm-helper-textarea'),
    null,
    { timeout: 2000 },
  );
  await page.evaluate(() => window.__hive.resetStdin());
}

test.describe('macOS line-edit keys reach the terminal', () => {
  test.skip(!isMac, 'the ⌘ chord only exists on macOS');

  test('⌘← sends start-of-line and ⌘→ sends end-of-line', async ({ page }) => {
    await bootFocusedTerminal(page);
    await page.keyboard.type('some text');
    await page.keyboard.press('Meta+ArrowLeft');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()), {
        timeout: 2000,
      })
      .toBe(`some text${LINE_START}`);

    await page.keyboard.press('Meta+ArrowRight');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()), {
        timeout: 2000,
      })
      .toBe(`some text${LINE_START}${LINE_END}`);
  });

  test('⇧⌘←/→ move the cursor too — a PTY has no selection to extend', async ({
    page,
  }) => {
    await bootFocusedTerminal(page);
    await page.keyboard.press('Shift+Meta+ArrowLeft');
    await page.keyboard.press('Shift+Meta+ArrowRight');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()), {
        timeout: 2000,
      })
      .toBe(`${LINE_START}${LINE_END}`);
  });

  test('the keys still do not change the active session', async ({ page }) => {
    await bootFocusedTerminal(page);
    const activeId = () =>
      page.evaluate(
        () =>
          document.querySelector<HTMLElement>('li.session-item.selected')
            ?.dataset.sid ?? null,
      );
    // Add the second session FIRST: creating one switches to it, which
    // would otherwise look like the arrows moved the selection.
    await page.evaluate(() => window.__hive.addSession?.('second'));
    await expect
      .poll(() =>
        page.evaluate(() => window.__hive.state?.sessions.length ?? 0),
      )
      .toBe(2);
    const before = await activeId();
    expect(before).not.toBeNull();
    await page.keyboard.press('Meta+ArrowLeft');
    await page.keyboard.press('Meta+ArrowRight');
    expect(await activeId()).toBe(before);
  });

  test('⌘←/→ still navigate tiles in grid mode, and send no bytes', async ({
    page,
  }) => {
    await bootFocusedTerminal(page);
    await page.evaluate(() => window.__hive.addSession?.('second'));
    await expect
      .poll(() =>
        page.evaluate(() => window.__hive.state?.sessions.length ?? 0),
      )
      .toBe(2);
    await page.keyboard.press('Meta+Shift+g');
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await page.evaluate(() => window.__hive.resetStdin());

    await page.keyboard.press('Meta+ArrowRight');
    // Grid mode owns the key: the window handler consumes it before the
    // terminal's custom handler can turn it into a line-edit byte.
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()))
      .toBe('');
  });
});

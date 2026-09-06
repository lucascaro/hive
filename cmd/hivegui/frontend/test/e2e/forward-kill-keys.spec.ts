import { test, expect, type Page } from '@playwright/test';

// ⌥⌦/⌘⌦ must reach the PTY as kill-word-forward / kill-to-end-of-line,
// end to end.
//
// xterm.js DOES encode the forward-delete key, but its `case 46` branch
// emits \x1b[3;<mods+1>~ as soon as any modifier is held — \x1b[3;3~
// for ⌥⌦ and \x1b[3;9~ for ⌘⌦. Nothing binds either, so before #362 the
// chords were silent. The app has to write the readline bytes itself,
// the same way ⌘⌫ → \x15 and ⌘←/→ → \x01/\x05 already do.
//
// This spec asserts the bytes on the wire because that is the only layer
// where "the predicate returns the right string" and "the dispatch is
// actually wired up" are distinguishable — a block placed after an
// earlier `return` would still pass every unit test.
//
// macOS only: elsewhere Ctrl+⌦ already means kill-word and xterm encodes
// it as \x1b[3;5~ on its own.
const isMac = process.platform === 'darwin';

const KILL_WORD = '\x1bd'; // meta-d
const KILL_LINE = '\x0b'; // Ctrl+K

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

test.describe('macOS forward-delete kill keys reach the terminal', () => {
  test.skip(!isMac, 'the ⌥/⌘ chords only exist on macOS');

  test('⌥⌦ sends kill-word-forward and ⌘⌦ sends kill-to-end-of-line', async ({
    page,
  }) => {
    await bootFocusedTerminal(page);
    await page.keyboard.type('some text');
    await page.keyboard.press('Alt+Delete');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()), {
        timeout: 2000,
      })
      .toBe(`some text${KILL_WORD}`);

    await page.keyboard.press('Meta+Delete');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()), {
        timeout: 2000,
      })
      .toBe(`some text${KILL_WORD}${KILL_LINE}`);
  });

  test('a bare ⌦ is left to xterm, which already encodes it', async ({
    page,
  }) => {
    await bootFocusedTerminal(page);
    await page.keyboard.press('Delete');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()), {
        timeout: 2000,
      })
      .toBe('\x1b[3~');
  });

  test('the keys still do not change the active session', async ({ page }) => {
    await bootFocusedTerminal(page);
    const activeId = () =>
      page.evaluate(
        () =>
          document.querySelector<HTMLElement>(
            'li.hv-session-row[data-selected]',
          )?.dataset.sid ?? null,
      );
    // Add the second session FIRST: creating one switches to it, which
    // would otherwise look like the chord moved the selection.
    await page.evaluate(() => window.__hive.addSession?.('second'));
    await expect
      .poll(() =>
        page.evaluate(() => window.__hive.state?.sessions.length ?? 0),
      )
      .toBe(2);
    const before = await activeId();
    expect(before).not.toBeNull();
    await page.keyboard.press('Alt+Delete');
    await page.keyboard.press('Meta+Delete');
    expect(await activeId()).toBe(before);
  });
});

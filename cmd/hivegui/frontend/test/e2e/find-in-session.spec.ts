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

  // Criterion 5: the normal-buffer search actually finds text, including
  // off-screen scrollback, and steps with wrap-around.
  //
  // Asserts real counts against the real @xterm/addon-search, not just
  // that the source is 'buffer'. The first version of this spec only
  // checked the source, and the whole buffer search shipped reporting
  // 0/0: the addon's match decorations are a proposed xterm API, every
  // findNext threw without allowProposedApi, and the throw was caught.
  test('a normal-buffer session finds and steps through matches', async ({
    page,
  }) => {
    await bootAsLinux(page);
    const id = await activeId(page);
    await page.evaluate((sid) => {
      // 80 lines is well past one screen, so line 5 is off-screen
      // scrollback while line 75 is on screen at the bottom.
      const lines = Array.from(
        { length: 80 },
        (_, i) =>
          `line ${i} ${i === 5 || i === 40 || i === 75 ? 'needle' : 'hay'}\r\n`,
      ).join('');
      window.__hive.emit('pty:data', sid, btoa(lines));
    }, id);

    // How far the viewport sits above the bottom of the scrollback.
    const fromBottom = () =>
      page.evaluate(() => {
        const vp = document.querySelector(
          '.term-focused .xterm-viewport',
        ) as HTMLElement | null;
        return vp ? vp.scrollHeight - vp.clientHeight - vp.scrollTop : -1;
      });

    await page.keyboard.press('Control+Shift+f');
    await page.locator('[data-find-input]').fill('needle');
    await expect(page.locator('[data-find-count]')).toHaveText('1/3');

    // Bottom to top: the first match is the NEWEST (line 75, on screen),
    // so the viewport has not moved off the bottom. Were the first match
    // the oldest (line 5), the addon would have scrolled up to it.
    await expect.poll(fromBottom).toBeLessThanOrEqual(2);

    // Enter goes to the next OLDER match — up the output.
    await page.keyboard.press('Enter');
    await expect(page.locator('[data-find-count]')).toHaveText('2/3');
    await page.keyboard.press('Enter');
    await expect(page.locator('[data-find-count]')).toHaveText('3/3');
    // The oldest match is off-screen scrollback: the viewport moved up.
    await expect.poll(fromBottom).toBeGreaterThan(2);

    // Shift+Enter comes back down toward the newest.
    await page.keyboard.press('Shift+Enter');
    await expect(page.locator('[data-find-count]')).toHaveText('2/3');
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
      window.__hive.emit('pty:data', sid, btoa(`${esc}[?1049h`));
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
      window.__hive.emit('pty:data', sid, btoa(`${esc}[?1049h`));
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
      window.__hive.emit('pty:data', sid, btoa(`${esc}[?1049h`));
    }, id);

    const active = page.locator('.hv-find-line-active');

    await page.keyboard.press('Control+Shift+f');
    await page.locator('[data-find-input]').fill('needle');
    await expect(page.locator('[data-find-count]')).toHaveText('1/3');
    // Bottom to top: 1/3 is the NEWEST match, the last line.
    await expect(active).toContainText('needle three');

    // Enter goes to the next OLDER match.
    await page.keyboard.press('Enter');
    await expect(page.locator('[data-find-count]')).toHaveText('2/3');
    await expect(active).toContainText('needle two');

    await page.keyboard.press('Shift+Enter');
    await expect(page.locator('[data-find-count]')).toHaveText('1/3');
    await expect(active).toContainText('needle three');

    // And it wraps rather than sticking at the ends: newer than the
    // newest is the oldest.
    await page.keyboard.press('Shift+Enter');
    await expect(page.locator('[data-find-count]')).toHaveText('3/3');
    await expect(active).toContainText('needle one');
  });

  // The bar sits in exactly the same place whichever source is active.
  // Measured in a real browser, because vitest cannot see layout: an
  // earlier version rendered the transcript first and the bar at the
  // bottom of the tile in takeover mode.
  test('the bar is in the same place in both modes', async ({ page }) => {
    await bootAsLinux(page);
    const id = await activeId(page);

    await page.keyboard.press('Control+Shift+f');
    const bar = page.locator('.hv-find-bar');
    await expect(page.locator('[data-find-source="buffer"]')).toBeVisible();
    const inBuffer = await bar.boundingBox();
    await page.keyboard.press('Escape');

    await page.evaluate((sid) => {
      window.__hive.setTranscript?.(sid, ['one', 'two', 'three']);
      const esc = String.fromCharCode(27);
      window.__hive.emit('pty:data', sid, btoa(`${esc}[?1049h`));
    }, id);
    await page.keyboard.press('Control+Shift+f');
    await expect(page.locator('[data-find-source="transcript"]')).toBeVisible();
    const inTranscript = await bar.boundingBox();

    if (!inBuffer || !inTranscript) throw new Error('bar has no box');
    // Right and top edges within a pixel; the bar must not move.
    const right = (b: { x: number; width: number }) => b.x + b.width;
    expect(Math.abs(right(inTranscript) - right(inBuffer))).toBeLessThanOrEqual(
      1,
    );
    expect(Math.abs(inTranscript.y - inBuffer.y)).toBeLessThanOrEqual(1);
  });

  // Criterion 8, transcript half: text the agent writes while the box is
  // open becomes findable without reopening — and the user stays on the
  // match they were reading rather than being yanked to the new one.
  test('the transcript updates live while the box is open', async ({
    page,
  }) => {
    await bootAsLinux(page);
    const id = await activeId(page);
    await page.evaluate((sid) => {
      window.__hive.setTranscript?.(sid, ['start', 'the first needle', 'end']);
      const esc = String.fromCharCode(27);
      window.__hive.emit('pty:data', sid, btoa(`${esc}[?1049h`));
    }, id);

    await page.keyboard.press('Control+Shift+f');
    await page.locator('[data-find-input]').fill('needle');
    await expect(page.locator('[data-find-count]')).toHaveText('1/1');

    // The agent writes a new message, and prints to the terminal.
    await page.evaluate((sid) => {
      window.__hive.setTranscript?.(sid, [
        'start',
        'the first needle',
        'end',
        'a brand new needle',
      ]);
      window.__hive.emit('pty:data', sid, btoa('more output\r\n'));
    }, id);

    // Found without reopening. 2/2: the new match is now the newest, and
    // the user is still on the one they were reading, which is second.
    await expect(page.locator('[data-find-count]')).toHaveText('2/2');
    await expect(page.locator('.hv-find-line-active')).toContainText(
      'the first needle',
    );
  });

  // Criterion 8, buffer half: the addon re-runs the search itself on new
  // output; the count must follow it rather than go stale.
  test('the buffer count updates live while the box is open', async ({
    page,
  }) => {
    await bootAsLinux(page);
    const id = await activeId(page);
    await page.evaluate((sid) => {
      window.__hive.emit('pty:data', sid, btoa('one needle\r\n'));
    }, id);

    await page.keyboard.press('Control+Shift+f');
    await page.locator('[data-find-input]').fill('needle');
    await expect(page.locator('[data-find-count]')).toHaveText('1/1');

    await page.evaluate((sid) => {
      window.__hive.emit('pty:data', sid, btoa('another needle\r\n'));
    }, id);
    await expect(page.locator('[data-find-count]')).toHaveText(/\/2$/);
  });

  // The transcript pane opens on the most recent output, and follows the
  // active match as typing moves it. Layout, so it is asserted in a real
  // browser: vitest cannot see scroll positions.
  test('the transcript opens at the bottom and follows the match while typing', async ({
    page,
  }) => {
    await bootAsLinux(page);
    const id = await activeId(page);
    await page.evaluate((sid) => {
      // Far more lines than fit, so the pane really scrolls. Two
      // distinctive words: one early, one mid-way.
      const lines = Array.from({ length: 200 }, (_, i) => {
        if (i === 20) return 'an early marker: zebra';
        if (i === 120) return 'a later marker: zeppelin';
        return `ordinary line ${i}`;
      });
      window.__hive.setTranscript?.(sid, lines);
      const esc = String.fromCharCode(27);
      window.__hive.emit('pty:data', sid, btoa(`${esc}[?1049h`));
    }, id);

    const pane = page.locator('.hv-find-body');
    // Whether an element's box lies inside the pane's visible area.
    const inView = (sel: string) =>
      page.evaluate((s) => {
        const body = document.querySelector('.hv-find-body');
        const el = document.querySelector(s);
        if (!body || !el) return false;
        const b = body.getBoundingClientRect();
        const r = el.getBoundingClientRect();
        return r.top >= b.top && r.bottom <= b.bottom;
      }, sel);

    await page.keyboard.press('Control+Shift+f');
    await expect(page.locator('[data-find-source="transcript"]')).toBeVisible();

    // Opened at the bottom: the newest line is visible and the pane is
    // scrolled to its end.
    await expect(page.locator('.hv-find-line').last()).toContainText(
      'ordinary line 199',
    );
    await expect
      .poll(() =>
        pane.evaluate((b) => b.scrollHeight - b.clientHeight - b.scrollTop),
      )
      .toBeLessThanOrEqual(1);
    // Addressed by line: lines are nested per message now, so
    // :last-child matches the first line of every message.
    await expect.poll(() => inView('[data-find-line="199"]')).toBe(true);

    // Typing moves the match; each new position is brought into view.
    const input = page.locator('[data-find-input]');
    await input.pressSequentially('ze');
    await input.pressSequentially('p');
    await expect(page.locator('.hv-find-line-active')).toContainText(
      'zeppelin',
    );
    await expect.poll(() => inView('.hv-find-line-active')).toBe(true);

    await input.fill('zebra');
    await expect(page.locator('.hv-find-line-active')).toContainText('zebra');
    await expect.poll(() => inView('.hv-find-line-active')).toBe(true);
  });

  // The find chord is a toggle: pressed while the box has focus it
  // closes the box, rather than selecting the query text.
  test('the find chord closes an open box', async ({ page }) => {
    await bootAsLinux(page);
    await page.keyboard.press('Control+Shift+f');
    await expect(page.locator('.hv-find')).toBeVisible();
    await page.locator('[data-find-input]').fill('needle');
    await page.keyboard.press('Control+Shift+f');
    await expect(page.locator('.hv-find')).toHaveCount(0);
    await assertAlignedFocus(page);
  });

  // Escape closes the box and nothing else: in particular it must never
  // reach the session, where it would interrupt an agent.
  for (const alt of [false, true]) {
    test(`Escape never reaches the session (${alt ? 'transcript' : 'buffer'})`, async ({
      page,
    }) => {
      await bootAsLinux(page);
      const id = await activeId(page);
      if (alt) {
        await page.evaluate((sid) => {
          window.__hive.setTranscript?.(sid, ['a needle']);
          const esc = String.fromCharCode(27);
          window.__hive.emit('pty:data', sid, btoa(`${esc}[?1049h`));
        }, id);
      }
      await page.evaluate(() => window.__hive.resetStdin());
      await page.keyboard.press('Control+Shift+f');
      await page.locator('[data-find-input]').fill('needle');
      await page.keyboard.press('Escape');
      await expect(page.locator('.hv-find')).toHaveCount(0);
      await assertAlignedFocus(page);
      // Give any stray key time to arrive before asserting its absence.
      await page.waitForTimeout(200);
      expect(await page.evaluate(() => window.__hive.stdinText())).toBe('');
    });
  }

  // The transcript pane used to hold one fixed window, so scrolling up
  // stopped dead and the history looked cut off. It now loads older
  // history as the reader nears the top, and each load leaves the line
  // they were reading where it was on screen.
  test('scrolling up loads older history without losing your place', async ({
    page,
  }) => {
    await bootAsLinux(page);
    const id = await activeId(page);
    await page.evaluate((sid) => {
      const lines = Array.from({ length: 1000 }, (_, i) => `history line ${i}`);
      window.__hive.setTranscript?.(sid, lines);
      const esc = String.fromCharCode(27);
      window.__hive.emit('pty:data', sid, btoa(`${esc}[?1049h`));
    }, id);

    await page.keyboard.press('Control+Shift+f');
    await expect(page.locator('.hv-find-line').last()).toContainText(
      'history line 999',
    );

    const pane = page.locator('.hv-find-body');
    // The first line on screen and its distance from the pane's top.
    const topLine = () =>
      pane.evaluate((b) => {
        for (const el of b.querySelectorAll<HTMLElement>('[data-find-line]')) {
          if (el.offsetTop + el.offsetHeight > b.scrollTop) {
            return {
              line: Number(el.dataset.findLine),
              off: el.offsetTop - b.scrollTop,
            };
          }
        }
        return null;
      });

    const firstLoaded = () =>
      page.evaluate(() =>
        Number(
          document
            .querySelector('.hv-find-body [data-find-line]')
            ?.getAttribute('data-find-line') ?? -1,
        ),
      );

    // Go to the top of what is loaded; that is near the edge, so the
    // block above loads. Whatever line was on screen must still be on
    // screen, at the same place, once it lands. Repeat to the start.
    for (let round = 0; round < 10; round++) {
      const loadedFrom = await firstLoaded();
      if (loadedFrom === 0) break;
      await pane.evaluate((b) => {
        b.scrollTop = 0;
      });
      const before = await topLine();
      await expect.poll(firstLoaded).toBeLessThan(loadedFrom);
      const after = await topLine();
      expect(before).not.toBeNull();
      // After a prepend the anchor line is still the first on screen...
      expect(after?.line).toBe(before?.line);
      // ...and has not moved.
      expect(
        Math.abs((after?.off ?? 0) - (before?.off ?? 0)),
      ).toBeLessThanOrEqual(2);
    }
    // All the way back to the very first line of the transcript.
    await expect(page.locator('[data-find-line="0"]')).toContainText(
      'history line 0',
    );
    // And never the whole transcript in the DOM at once.
    expect(await page.locator('.hv-find-line').count()).toBeLessThanOrEqual(
      1200,
    );
  });

  // With no query, the transcript follows new output while the reader is
  // at the bottom, like a terminal.
  test('the plain transcript follows new output at the bottom', async ({
    page,
  }) => {
    await bootAsLinux(page);
    const id = await activeId(page);
    const base = Array.from({ length: 50 }, (_, i) => `old line ${i}`);
    await page.evaluate(
      ([sid, lines]) => {
        window.__hive.setTranscript?.(sid as string, lines as string[]);
        const esc = String.fromCharCode(27);
        window.__hive.emit('pty:data', sid as string, btoa(`${esc}[?1049h`));
      },
      [id, base],
    );
    await page.keyboard.press('Control+Shift+f');
    await expect(page.locator('.hv-find-line').last()).toContainText(
      'old line 49',
    );

    await page.evaluate(
      ([sid, lines]) => {
        window.__hive.setTranscript?.(sid as string, [
          ...(lines as string[]),
          'fresh output arrives',
        ]);
        window.__hive.emit('pty:data', sid as string, btoa('x'));
      },
      [id, base],
    );
    await expect(page.locator('.hv-find-line').last()).toContainText(
      'fresh output arrives',
    );
    // Still pinned to the bottom.
    await expect
      .poll(() =>
        page
          .locator('.hv-find-body')
          .evaluate((b) => b.scrollHeight - b.clientHeight - b.scrollTop),
      )
      .toBeLessThanOrEqual(4);
  });

  // The real pi transcript that looked cut off: it ends in a long tool
  // output, so the first window starts in the middle of it. When older
  // history loads, the output's true start arrives and its collapse
  // re-cuts — the line the view was anchored on stops being rendered,
  // and the pane fell back to the top. A reader at the bottom must stay
  // at the bottom.
  test('opens on the latest output even when it ends in a long tool result', async ({
    page,
  }) => {
    await bootAsLinux(page);
    const id = await activeId(page);
    await page.evaluate((sid) => {
      const lines: {
        text: string;
        msg: number;
        kind: string;
        tool?: string;
      }[] = [];
      for (let i = 0; i < 60; i++)
        lines.push({ text: `earlier ${i}`, msg: 0, kind: 'assistant' });
      lines.push({ text: 'run the thing', msg: 1, kind: 'user' });
      for (let i = 0; i < 320; i++) {
        lines.push({ text: `output ${i}`, msg: 2, kind: 'tool', tool: 'bash' });
      }
      window.__hive.setTranscript?.(sid, lines);
      const esc = String.fromCharCode(27);
      window.__hive.emit('pty:data', sid, btoa(`${esc}[?1049h`));
    }, id);

    await page.keyboard.press('Control+Shift+f');
    // The whole history is reachable and loaded back to its start...
    await expect(page.locator('[data-find-line="0"]')).toBeAttached();
    // ...the long output is collapsed under its tool's name...
    await expect(page.locator('[data-tx-more]')).toContainText(
      'Show 312 more lines',
    );
    await expect(page.locator('.hv-tx-user')).toContainText('run the thing');
    // ...and the view sits at the bottom: the most recent output.
    await expect
      .poll(() =>
        page
          .locator('.hv-find-body')
          .evaluate((b) => b.scrollHeight - b.clientHeight - b.scrollTop),
      )
      .toBeLessThanOrEqual(4);
  });
});

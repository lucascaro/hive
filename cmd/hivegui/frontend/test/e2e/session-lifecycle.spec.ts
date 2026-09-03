import { test, expect, type Page } from '@playwright/test';

// E2E for the session lifecycle UI: the loading panel a starting
// session shows in its own tile, and the get-out-of-the-way behaviour
// when one is closed.
//
// Before this, ⌘T left the user staring at a black xterm painting the
// login shell's startup output, and ⌘W left a frozen tile that painted
// `[attach failed: …]` in red until the daemon finished its git
// cleanup seconds later.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

// Hold each lifecycle phase long enough to observe it, the way a real
// `git worktree add` would.
async function holdPhases(page: Page, ms = 400) {
  await page.evaluate((n) => window.__hive.phaseHold?.(n), ms);
}

test('a starting session shows a loading panel with its steps, not a black terminal', async ({
  page,
}) => {
  await boot(page);
  await holdPhases(page);

  const id = await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('feature-x'),
  );

  const overlay = page.locator(`.term-host[data-sid="${id}"] .phase-overlay`);
  await expect(overlay).toBeVisible();
  await expect(overlay).toContainText('Registered session');
  await expect(overlay).toContainText('Creating worktree feature-x');
  await expect(overlay).toContainText('Starting shell');

  // ...and it goes away on its own once the session is ready.
  await expect(overlay).toBeHidden({ timeout: 5000 });
});

test('no attach error is ever painted into a starting or closing pane', async ({
  page,
}) => {
  await boot(page);
  await holdPhases(page);

  await page.evaluate(() => window.__hive.addSession?.('doomed'));
  // Poke the UI while the session is mid-create: a grid repaint used
  // to re-dial the not-yet-existing session and write the error into
  // the pane.
  await page.keyboard.press(`${mod}+g`);
  await page.keyboard.press(`${mod}+g`);

  await expect
    .poll(() => page.evaluate(() => window.__hive.state?.sessions.length ?? 0))
    .toBeGreaterThan(1);

  const id = await page.evaluate(
    () => window.__hive.state?.sessions.find((s) => s.name === 'doomed')?.id,
  );
  await page.evaluate((sid) => window.__hive.killSession?.(sid as string), id);

  // The tile dims and stops taking input the moment the daemon starts
  // tearing it down.
  await expect(
    page.locator(`.term-host[data-sid="${id}"].closing`),
  ).toHaveCount(1);
  await expect(page.locator(`.term-host[data-sid="${id}"]`)).toHaveCount(0, {
    timeout: 5000,
  });

  // TermTile types `term.buffer.active` only as the scroll positions
  // the app itself reads, so reach for the real xterm shape here.
  const buffers = await page.evaluate(() =>
    Array.from(window.__hive_state?.terms.values() ?? [])
      .map((t) => {
        const buf = (
          t.term as unknown as {
            buffer?: {
              active?: {
                length: number;
                getLine(i: number): { translateToString(t: boolean): string };
              };
            };
          } | null
        )?.buffer?.active;
        if (!buf) return '';
        let out = '';
        for (let i = 0; i < buf.length; i++)
          out += buf.getLine(i)?.translateToString(true) ?? '';
        return out;
      })
      .join('\n'),
  );
  expect(buffers).not.toContain('attach failed');
  expect(buffers).not.toContain('hived:');
});

test('closing the active session moves focus immediately, before it is removed', async ({
  page,
}) => {
  await boot(page);
  await page.evaluate(() => window.__hive.addSession?.('second'));
  await expect
    .poll(() => page.evaluate(() => window.__hive.state?.sessions.length ?? 0))
    .toBe(2);
  // A long hold: focus must move at `closing`, not wait for `removed`.
  await holdPhases(page, 2000);

  const activeId = await page.evaluate(
    () => window.__hive_state?.activeId ?? null,
  );
  expect(activeId).toBeTruthy();
  await page.keyboard.press(`${mod}+w`);

  // Focus moves on the `closing` event, well before `removed` lands.
  await expect
    .poll(() => page.evaluate(() => window.__hive_state?.activeId ?? null), {
      timeout: 1500,
    })
    .not.toBe(activeId);
  // The doomed tile is still on screen — this is the window that used
  // to be full of errors.
  await expect(page.locator(`.term-host[data-sid="${activeId}"]`)).toHaveCount(
    1,
  );
});

test('closing a session in grid mode repaints the grid', async ({ page }) => {
  await boot(page);
  for (const n of ['g2', 'g3', 'g4']) {
    await page.evaluate((name) => window.__hive.addSession?.(name), n);
  }
  await expect
    .poll(() => page.evaluate(() => window.__hive.state?.sessions.length ?? 0))
    .toBe(4);
  await page.keyboard.press(`${mod}+g`);
  await expect(page.locator('.term-host.in-grid')).toHaveCount(4);

  // The layout signature: the container's track template plus each
  // tile's row span. applyGridLayout gives the tile above a trailing empty
  // cell a `span 2` so the grid has no hole — which is exactly what a
  // missing repaint leaves behind.
  const layout = () =>
    page.evaluate(() => {
      const host = document.querySelector<HTMLElement>('#terms');
      return {
        cols: host?.style.gridTemplateColumns ?? '',
        rows: host?.style.gridTemplateRows ?? '',
        spans: Array.from(
          document.querySelectorAll<HTMLElement>('.term-host.in-grid'),
        ).map((el) => el.style.gridRow || 'span 1'),
      };
    });
  const before = await layout();
  expect(before.spans).toEqual(['span 1', 'span 1', 'span 1', 'span 1']);

  // Kill a NON-active tile. The only grid repaint on the removal path
  // used to be the switchTo that fires when the *active* session goes
  // away — so the tiles vanished but the CSS grid template kept the
  // old shape, leaving a hole. Focus now moves at `closing`, which
  // means switchTo doesn't run on removal at all.
  const victim = await page.evaluate(
    () => window.__hive.state?.sessions.find((s) => s.name === 'g2')?.id,
  );
  await page.evaluate(
    (id) => window.__hive.killSession?.(id as string),
    victim,
  );

  await expect(page.locator('.term-host.in-grid')).toHaveCount(3);
  // 4 → 3 keeps a 2×2 track grid, so the hole shows up as a missing
  // row span, not a changed template: the tile above the now-empty
  // trailing cell grows to fill it.
  await expect
    .poll(() => layout().then((l) => l.spans))
    .toEqual(['span 1', 'span 2', 'span 1']);

  // ...and the same when the tile being closed IS the active one.
  const active = await page.evaluate(() => window.__hive_state?.activeId);
  await page.evaluate(
    (id) => window.__hive.killSession?.(id as string),
    active,
  );
  await expect(page.locator('.term-host.in-grid')).toHaveCount(2);
  // Two tiles are always side by side: a single row of two columns.
  await expect
    .poll(() => layout().then((l) => `${l.cols} / ${l.rows}`))
    .toBe('repeat(2, 1fr) / repeat(1, 1fr)');
});

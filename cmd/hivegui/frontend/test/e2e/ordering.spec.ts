import { test, expect, type Page } from '@playwright/test';

// Session ordering + the keybindings that drive it, against the mock
// bridge — which models order the same way the daemon does (splice at
// the anchor on create, delete-then-insert on an order update).
//
// Ordering is the point of these fixes, so the assertions are on the
// sidebar's actual row order and the grid's actual tile order, not on
// which call was made.

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page, count = 3) {
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
}

// Sidebar row order, top to bottom, as session ids.
function sidebarIds(page: Page) {
  return page.evaluate(() =>
    Array.from(document.querySelectorAll<HTMLElement>('li.session-item')).map(
      (li) => li.dataset.sid ?? '',
    ),
  );
}

// Grid tile order, as they sit in the DOM.
function tileIds(page: Page) {
  return page.evaluate(() =>
    Array.from(
      document.querySelectorAll<HTMLElement>('#terms .term-host.in-grid'),
    ).map((h) => h.dataset.sid ?? ''),
  );
}

// The active session is read off the sidebar, not window.__hive.state —
// that is the MOCK's state (the daemon's side), which has no activeId.
function activeId(page: Page) {
  return page.evaluate(
    () =>
      document.querySelector<HTMLElement>('li.session-item.selected')?.dataset
        .sid ?? null,
  );
}

async function activate(page: Page, id: string) {
  await page.locator(`li.session-item[data-sid="${id}"]`).click();
  await expect.poll(() => activeId(page)).toBe(id);
}

test.describe('session ordering', () => {
  test('a new session appears directly below the active session', async ({
    page,
  }) => {
    await boot(page, 3);
    const before = await sidebarIds(page);
    await activate(page, before[0]);
    // ⌘T opens the launcher; the ⌘P-free path here goes through the
    // mock directly with the same anchor the app passes.
    await page.evaluate(
      (anchor) => window.__hive.addSession?.('inserted', anchor),
      before[0],
    );
    await expect.poll(() => sidebarIds(page)).toHaveLength(4);
    const after = await sidebarIds(page);
    expect(after[1]).not.toBe(before[1]);
    expect(after).toEqual([before[0], after[1], before[1], before[2]]);
  });

  test('a duplicated session appears directly below its source (⌘P)', async ({
    page,
  }) => {
    await boot(page, 3);
    // ⌘P refuses when the source session resolves no cwd, and the mock's
    // seed project has none — hand it one the way the daemon would.
    await page.evaluate(() => {
      const p = window.__hive.state?.projects?.[0];
      if (!p) throw new Error('mock has no seed project');
      p.cwd = '/tmp/hive-mock';
      window.__hive.emit?.(
        'project:event',
        JSON.stringify({ kind: 'updated', project: p }),
      );
    });
    const before = await sidebarIds(page);
    await activate(page, before[1]);
    await page.keyboard.press(`${MOD}+p`);
    await expect.poll(() => sidebarIds(page)).toHaveLength(4);
    const after = await sidebarIds(page);
    expect(after).toEqual([before[0], before[1], after[2], before[2]]);
  });

  test('⇧⌘↓ moves the session down one row', async ({ page }) => {
    await boot(page, 3);
    const before = await sidebarIds(page);
    await activate(page, before[0]);
    await page.keyboard.press(`${MOD}+Shift+ArrowDown`);
    await expect
      .poll(() => sidebarIds(page))
      .toEqual([before[1], before[0], before[2]]);
  });

  test('⇧⌘↓ on the last session wraps to the top of its project', async ({
    page,
  }) => {
    await boot(page, 3);
    const before = await sidebarIds(page);
    await activate(page, before[2]);
    await page.keyboard.press(`${MOD}+Shift+ArrowDown`);
    await expect
      .poll(() => sidebarIds(page))
      .toEqual([before[2], before[0], before[1]]);
    // Still the same project — the move never escapes into another one.
    const pids = await page.evaluate(
      () =>
        window.__hive.state?.sessions.map(
          (s: { project_id?: string }) => s.project_id,
        ) ?? [],
    );
    expect(new Set(pids).size).toBe(1);
  });

  test('⇧⌘↑ on the first session wraps to the bottom of its project', async ({
    page,
  }) => {
    await boot(page, 3);
    const before = await sidebarIds(page);
    await activate(page, before[0]);
    await page.keyboard.press(`${MOD}+Shift+ArrowUp`);
    await expect
      .poll(() => sidebarIds(page))
      .toEqual([before[1], before[2], before[0]]);
  });

  // The regression the unit suite pins at the pure-function level, run
  // end to end: alternate creates across two projects so the daemon's
  // flat order interleaves them and every session's display position
  // stops matching its .order. Sending a display position here used to
  // be accepted and change nothing.
  test('reorders correctly when projects interleave in the daemon order', async ({
    page,
  }) => {
    await page.goto('/');
    await page.waitForFunction(
      () => document.querySelectorAll('#projects li').length > 0,
    );
    // Second project, announced the way the daemon would.
    await page.evaluate(() => {
      const p = {
        id: 'p2',
        name: 'other',
        color: '#f80',
        cwd: '',
        order: 1,
        created: new Date().toISOString(),
      };
      window.__hive.state?.projects.push(p);
      window.__hive.emit?.(
        'project:event',
        JSON.stringify({ kind: 'added', project: p }),
      );
    });
    // r.order ends up p1, p2, p1, p2, p1 — interleaved.
    await page.evaluate(async () => {
      await window.__hive.addSession?.('t1', undefined, 'p2');
      await window.__hive.addSession?.('s2', undefined, 'p1');
      await window.__hive.addSession?.('t2', undefined, 'p2');
      await window.__hive.addSession?.('s3', undefined, 'p1');
    });
    await expect
      .poll(() =>
        page.evaluate(() => window.__hive.state?.sessions.length ?? 0),
      )
      .toBe(5);

    const idsIn = (pid: string) =>
      page.evaluate(
        (p) =>
          Array.from(
            document.querySelectorAll<HTMLElement>(
              `li.project[data-pid="${p}"] li.session-item`,
            ),
          ).map((li) => li.dataset.sid ?? ''),
        pid,
      );
    const p1Before = await idsIn('p1');
    expect(p1Before).toHaveLength(3);
    const p2Before = await idsIn('p2');

    // Move the middle session of p1 down one row.
    await activate(page, p1Before[1]);
    await page.keyboard.press(`${MOD}+Shift+ArrowDown`);
    await expect
      .poll(() => idsIn('p1'))
      .toEqual([p1Before[0], p1Before[2], p1Before[1]]);
    // The other project is untouched.
    expect(await idsIn('p2')).toEqual(p2Before);
  });

  test('reordering while in grid mode reorders the tiles', async ({ page }) => {
    await boot(page, 3);
    const before = await sidebarIds(page);
    await activate(page, before[0]);
    await page.keyboard.press(`${MOD}+Shift+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await expect.poll(() => tileIds(page)).toEqual(before);
    // In grid mode the arrows are spatial navigation, so the reorder
    // comes from the Session menu — which must reorder in every view.
    await page.evaluate(() =>
      window.__hive.emit?.('menu:move-session-forward'),
    );
    await expect
      .poll(() => tileIds(page))
      .toEqual([before[1], before[0], before[2]]);
  });
});

test.describe('keybinding regressions', () => {
  test('⌘← in focused mode is left to the terminal', async ({ page }) => {
    await boot(page, 3);
    const before = await sidebarIds(page);
    await activate(page, before[1]);
    // Record the APP's verdict on the key, in the capture phase. It must
    // be capture, not bubble: on Linux MOD is Control and xterm handles
    // Ctrl+← itself, calling preventDefault + stopPropagation on the
    // textarea — which is the outcome we want, but a bubble listener
    // never runs to see it. Capture on window fires before any deeper
    // handler, and after the app's own capture listener (registered at
    // import time), so what it reads is exactly what the app decided.
    await page.evaluate(() => {
      window.__arrowPrevented = null;
      window.addEventListener(
        'keydown',
        (e) => {
          if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
            window.__arrowPrevented = e.defaultPrevented;
          }
        },
        true,
      );
    });
    await page.keyboard.press(`${MOD}+ArrowLeft`);
    await expect
      .poll(() => page.evaluate(() => window.__arrowPrevented))
      .toBe(false);
    expect(await activeId(page)).toBe(before[1]);
    expect(await sidebarIds(page)).toEqual(before);
  });

  test('⌘G with a single session does not enter grid mode', async ({
    page,
  }) => {
    await boot(page, 1);
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    await page.keyboard.press(`${MOD}+Shift+g`);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });

  test('killing a grid down to one session returns to focused mode', async ({
    page,
  }) => {
    await boot(page, 2);
    await page.keyboard.press(`${MOD}+Shift+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    const ids = await sidebarIds(page);
    await page.evaluate((id) => window.__hive.killSession?.(id), ids[1]);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });
});

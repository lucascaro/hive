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
    Array.from(document.querySelectorAll<HTMLElement>('li.hv-session-row')).map(
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
      document.querySelector<HTMLElement>('li.hv-session-row[data-selected]')
        ?.dataset.sid ?? null,
  );
}

async function activate(page: Page, id: string) {
  await page.locator(`li.hv-session-row[data-sid="${id}"]`).click();
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

  // The regression this whole fix is about, end to end: a kill used to
  // leave a hole in the daemon's .order sequence, the next create
  // reused a number that was still in use, and the reorder — which
  // sends a sibling's .order as an absolute index — landed in the
  // wrong slot or clamped to the end.
  test('⇧⌘↓ still moves correctly after a kill and a create', async ({
    page,
  }) => {
    await boot(page, 4);
    const before = await sidebarIds(page);
    await page.evaluate((id) => window.__hive.killSession?.(id), before[0]);
    await expect.poll(() => sidebarIds(page)).toHaveLength(3);
    await page.evaluate(() => window.__hive.addSession?.('fresh'));
    await expect.poll(() => sidebarIds(page)).toHaveLength(4);

    // Nobody shares an order value, and the sequence has no holes.
    const orders = await page.evaluate(
      () => window.__hive.state?.sessions.map((s) => s.order) ?? [],
    );
    expect(orders).toEqual([0, 1, 2, 3]);

    const rows = await sidebarIds(page);
    await activate(page, rows[0]);
    await page.keyboard.press(`${MOD}+Shift+ArrowDown`);
    await expect
      .poll(() => sidebarIds(page))
      .toEqual([rows[1], rows[0], rows[2], rows[3]]);
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
              `li.hv-project-card[data-pid="${p}"] li.hv-session-row`,
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

  // Drag-to-reorder. The regression (spec 305): the drop slot was resolved
  // against the sibling list that still CONTAINED the dragged row, so a row
  // dragged downwards landed one position below where it was dropped.
  //
  // The drag is driven by dispatched DragEvents sharing one DataTransfer
  // rather than by locator.dragTo(): headless Chromium does not synthesise
  // native HTML5 drag from mouse input here (verified — no dragstart fires),
  // and the app's handlers are what these tests are about. Everything past
  // the event dispatch is real: real layout, real CSS, real reorder call.
  // What this cannot cover is the browser's own decision to *begin* a drag —
  // notably that hiding the source element must be deferred a tick or the
  // drag is cancelled. That one needs a human in the built app.
  async function dragStart(page: Page, sid: string) {
    await page.evaluate((id) => {
      const w = window as unknown as { __dt?: DataTransfer };
      w.__dt = new DataTransfer();
      const row = document.querySelector<HTMLElement>(
        `li.hv-session-row[data-sid="${id}"]`,
      );
      if (!row) throw new Error(`no row ${id}`);
      row.dispatchEvent(
        new DragEvent('dragstart', {
          bubbles: true,
          cancelable: true,
          dataTransfer: w.__dt,
        }),
      );
    }, sid);
    // beginDrag defers taking the row out of the flow by one tick.
    await page.waitForFunction(
      (id) =>
        document
          .querySelector(`li.hv-session-row[data-sid="${id}"]`)
          ?.classList.contains('dragging') ?? false,
      sid,
    );
  }

  // Both dragover and drop carry the cursor's y, which is what decides the
  // above/below half; the two must agree or the drop lands somewhere the
  // placeholder never showed.
  async function dragEvent(
    page: Page,
    type: 'dragover' | 'drop',
    sid: string,
    above: boolean,
  ) {
    await page.evaluate(
      ({ kind, id, top }) => {
        const w = window as unknown as { __dt?: DataTransfer };
        const row = document.querySelector<HTMLElement>(
          `li.hv-session-row[data-sid="${id}"]`,
        );
        if (!row) throw new Error(`no row ${id}`);
        const r = row.getBoundingClientRect();
        row.dispatchEvent(
          new DragEvent(kind, {
            bubbles: true,
            cancelable: true,
            dataTransfer: w.__dt,
            clientY: top ? r.top + 2 : r.bottom - 2,
          }),
        );
      },
      { kind: type, id: sid, top: above },
    );
  }

  test('a dragged session lands exactly where it was dropped', async ({
    page,
  }) => {
    await boot(page, 4);
    const before = await sidebarIds(page);

    await dragStart(page, before[0]);
    await dragEvent(page, 'dragover', before[2], true);
    await expect(page.locator('.hv-drop-placeholder')).toHaveCount(1);
    await dragEvent(page, 'drop', before[2], true);

    // Above the target, not below it. Pre-fix this produced
    // [1, 2, 0, 3] — one slot too low.
    await expect
      .poll(() => sidebarIds(page))
      .toEqual([before[1], before[0], before[2], before[3]]);
  });

  test('a session dropped on a lower half lands below that row', async ({
    page,
  }) => {
    await boot(page, 4);
    const before = await sidebarIds(page);

    await dragStart(page, before[0]);
    await dragEvent(page, 'dragover', before[2], false);
    await dragEvent(page, 'drop', before[2], false);

    await expect
      .poll(() => sidebarIds(page))
      .toEqual([before[1], before[2], before[0], before[3]]);
  });

  test('dragging a session upwards lands it above the target', async ({
    page,
  }) => {
    await boot(page, 4);
    const before = await sidebarIds(page);

    await dragStart(page, before[3]);
    await dragEvent(page, 'dragover', before[1], true);
    await dragEvent(page, 'drop', before[1], true);

    await expect
      .poll(() => sidebarIds(page))
      .toEqual([before[0], before[3], before[1], before[2]]);
  });

  // The review regression: an "insert above" placeholder is inserted where the
  // cursor already is, so the row it displaces moves down and the release
  // lands on the PLACEHOLDER, not on a row. A placeholder that is not itself
  // a drop target loses the drop silently.
  test('a drop released on the placeholder still reorders', async ({
    page,
  }) => {
    await boot(page, 4);
    const before = await sidebarIds(page);

    await dragStart(page, before[0]);
    await dragEvent(page, 'dragover', before[2], true);
    const ph = page.locator('.hv-drop-placeholder');
    await expect(ph).toHaveCount(1);

    // The placeholder must keep the drag alive on its own …
    const allowed = await page.evaluate(() => {
      const w = window as unknown as { __dt?: DataTransfer };
      const el = document.querySelector('.hv-drop-placeholder') as HTMLElement;
      const e = new DragEvent('dragover', {
        bubbles: true,
        cancelable: true,
        dataTransfer: w.__dt,
      });
      el.dispatchEvent(e);
      return e.defaultPrevented;
    });
    expect(allowed).toBe(true);

    // … and resolve its own slot when the drop lands on it.
    await page.evaluate(() => {
      const w = window as unknown as { __dt?: DataTransfer };
      const el = document.querySelector('.hv-drop-placeholder') as HTMLElement;
      el.dispatchEvent(
        new DragEvent('drop', {
          bubbles: true,
          cancelable: true,
          dataTransfer: w.__dt,
        }),
      );
    });

    await expect
      .poll(() => sidebarIds(page))
      .toEqual([before[1], before[0], before[2], before[3]]);
  });

  // vitest is CSS-blind, so the "content does not jump" half of the fix can
  // only be asserted here: the dragged row leaves the flow and the
  // placeholder takes over its exact box, which keeps the list's total
  // height — and everything above the insertion point — pinned for the whole
  // gesture.
  test('the drag placeholder keeps the sidebar height stable', async ({
    page,
  }) => {
    await boot(page, 4);
    const ids = await sidebarIds(page);
    const listHeight = () =>
      page.evaluate(
        () => document.querySelector('#projects')?.scrollHeight ?? 0,
      );
    const topOf = (sid: string) =>
      page.evaluate(
        (id) =>
          document
            .querySelector(`li.hv-session-row[data-sid="${id}"]`)
            ?.getBoundingClientRect().top ?? 0,
        sid,
      );

    const heightBefore = await listHeight();
    const firstRowBefore = await topOf(ids[0]);

    await dragStart(page, ids[3]);
    await dragEvent(page, 'dragover', ids[2], true);
    await expect(page.locator('.hv-drop-placeholder')).toHaveCount(1);

    // The dragged row is out of the flow and the placeholder occupies its
    // box, so the list is exactly as tall as it was.
    expect(await listHeight()).toBe(heightBefore);
    // Rows before the insertion point do not move at all.
    expect(await topOf(ids[0])).toBe(firstRowBefore);

    await dragEvent(page, 'drop', ids[2], true);
    await expect(page.locator('.hv-drop-placeholder')).toHaveCount(0);
    await expect.poll(listHeight).toBe(heightBefore);
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

import { test, expect, type Page } from '@playwright/test';
import type { SessionTerm } from '../../src/app/session-term.js';

// The invariant the whole tile-chrome port is built around: React draws
// the header and the overlays, and never the terminal. A repaint of the
// chrome must not recreate `.term-host` — a recreated host means a
// remounted xterm, a re-taken WebGL slot (8 process-wide) and a
// re-dialled PTY attachment, which is the bug the React migration exists
// to avoid.
//
// Node identity cannot cross page.evaluate, so each host is tagged once
// and the tag is read back off the node the terms registry holds: a
// recreated host would carry no tag. The scrollback rides along as the
// observable consequence — a remounted terminal starts empty. It is
// counted by content, not by `buffer.active.length`: the grid reflows
// the tiles, and a row-count change moves that number legitimately.
//
// The four gestures are the ones that repaint the chrome for a reason
// other than the tile itself: a view switch (GridView's layout pass), a
// reorder (same pass, different order), a minimize/restore (the tile
// leaves and re-enters the grid scope) and a theme switch (which
// repaints every terminal's palette imperatively).

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';
const TAG = 'chrome-stability';
const MARKER = 'chrome-stability-line';
const MARKER_LINES = 20;

async function boot(page: Page, sessions = 3) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  for (let i = 1; i < sessions; i++) {
    await page.evaluate((n) => window.__hive.addSession?.(n), `s${i + 1}`);
  }
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) >= n,
    sessions,
  );
  // Wait for every tile to be attached before anything writes to it. A
  // deferred ensureAttached() replays the scrollback, and replay-begin
  // calls term.reset() (lib/scrollback.ts) — which would wipe markers
  // written before it and read back as a phantom host remount, the exact
  // bug this spec exists to detect.
  await page.waitForFunction((n) => {
    const terms = window.__hive_state?.terms;
    if (!terms || terms.size < n) return false;
    return [...terms.values()].every((t) => (t as SessionTerm).attached);
  }, sessions);
}

// Tag every live host and give each terminal something in its buffer, so
// a silent remount shows up as both a missing tag and a shorter buffer.
async function tagHosts(page: Page): Promise<string[]> {
  return page.evaluate(
    async ({ tag, marker, lines }) => {
      const app = window.__hive_state;
      const ids: string[] = [];
      for (const [id, t] of app?.terms ?? []) {
        const st = t as SessionTerm;
        st.host.dataset.stability = tag;
        await new Promise<void>((r) =>
          st.term.write(`${marker}\r\n`.repeat(lines), r),
        );
        ids.push(id);
      }
      return ids;
    },
    { tag: TAG, marker: MARKER, lines: MARKER_LINES },
  );
}

// [tag, lines still holding our marker] per id, read off the node the
// registry currently holds.
type BufferPeek = {
  buffer: {
    active: {
      length: number;
      getLine(i: number): { translateToString(trim?: boolean): string } | null;
    };
  };
};

async function hosts(page: Page, ids: string[]) {
  return page.evaluate(
    ({ list, marker }) => {
      const app = window.__hive_state;
      return list.map((id) => {
        const st = app?.terms.get(id) as SessionTerm | undefined;
        if (!st) throw new Error(`no tile for ${id}`);
        const buf = (st.term as unknown as BufferPeek).buffer.active;
        let hits = 0;
        for (let i = 0; i < buf.length; i++) {
          if (buf.getLine(i)?.translateToString(true) === marker) hits++;
        }
        return [st.host.dataset.stability ?? null, hits] as [
          string | null,
          number,
        ];
      });
    },
    { list: ids, marker: MARKER },
  );
}

test('terminal hosts survive every chrome repaint', async ({ page }) => {
  await boot(page);
  const ids = await tagHosts(page);
  expect(ids.length).toBeGreaterThan(2);
  const before = await hosts(page, ids);
  expect(before).toEqual(ids.map(() => [TAG, MARKER_LINES]));

  // 1. View switch: single ⇄ grid, twice, so both directions run.
  await page.keyboard.press(`${MOD}+Shift+g`);
  await expect(page.locator('#terms')).toHaveClass(/grid/);
  await page.keyboard.press(`${MOD}+g`);
  await page.keyboard.press(`${MOD}+Shift+g`);
  await expect(page.locator('#terms')).toHaveClass(/grid/);

  // 2. Reorder. In grid mode the arrows are spatial navigation, so this
  //    is the Session-menu path ordering.spec.ts uses.
  await page.evaluate(() => window.__hive.emit('menu:move-session-forward'));
  await page.waitForTimeout(100);

  // 3. Minimize and restore: the tile leaves the grid scope and comes
  //    back, which is a full layout pass on both edges.
  const sid = await page
    .locator('.term-host.in-grid')
    .first()
    .evaluate((el) => el.dataset.sid);
  await page
    .locator('.term-host.in-grid')
    .first()
    .locator('.tile-minimize')
    .click();
  await expect(
    page.locator(`#minimized-tray .hv-chip[data-sid="${sid}"]`),
  ).toBeVisible();
  await page.locator(`#minimized-tray .hv-chip[data-sid="${sid}"]`).click();
  await expect(page.locator('#minimized-tray')).toHaveClass(/hidden/);

  // 4. Theme switch: applyXtermTheme() walks every live terminal.
  await page.keyboard.press(`${MOD}+,`);
  await expect(page.locator('#settings')).toBeVisible();
  // Settings opens on the Agents tab; the theme picker lives on Appearance.
  await page.locator('#settings-tab-appearance').click();
  await expect(page.locator('#settings-panel-appearance')).toBeVisible();
  await page.locator('#settings-theme').selectOption('hive-light');
  await expect(page.locator('html')).toHaveAttribute(
    'data-theme',
    'hive-light',
  );
  await page.keyboard.press('Escape');
  await expect(page.locator('#settings')).toBeHidden();

  // Same nodes, same buffers.
  expect(await hosts(page, ids)).toEqual(before);
});

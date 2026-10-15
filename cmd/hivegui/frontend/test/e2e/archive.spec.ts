import { test, expect, type Page } from '@playwright/test';

// E2E for archive / unarchive (spec 518) against the Wails mock. The DOM
// suite pins the list and the flows in isolation; what only a real click
// path proves is the wiring: that the palette commands exist, that
// archiving asks nothing even for a session with uncommitted work, that
// the list shows EVERY archived session (not just the latest), and that
// Enter in the list brings the right one back.

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

async function palette(page: Page, query: string) {
  // Archiving the focused session leaves document.activeElement on
  // <body>, and Chromium then delivers no keydown for a ⌘-chord. A real
  // window always has something focused; clicking the app restores that.
  await page.locator('#app').click({ position: { x: 4, y: 4 } });
  await page.keyboard.press(`${mod}+Shift+k`);
  const input = page.locator('#command-palette-input');
  await input.fill(query);
  await page.keyboard.press('Enter');
}

const sessionCount = (page: Page) =>
  page.evaluate(() => window.__hive.state?.sessions.length ?? 0);

async function archiveActive(page: Page) {
  const before = await sessionCount(page);
  // Archive acts on the focused session, and removing the previous one
  // leaves none focused: select the first row, as a user would.
  await page.locator('#projects .hv-session-row').first().click();
  await palette(page, 'Archive Session');
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) === n - 1,
    before,
  );
}

test('archiving removes the session and asks nothing, even with uncommitted work', async ({
  page,
}) => {
  await boot(page);
  // A worktree with uncommitted changes is exactly where Close would
  // stop and ask; Archive must not, because it never touches the worktree.
  await page.evaluate(() => {
    const s = window.__hive.state?.sessions[0];
    window.__hive.seedWorktrees?.(
      [
        {
          path: '/mock/.worktrees/wip',
          branch: 'wip',
          uncommitted: true,
        },
      ],
      [],
    );
    if (s) {
      s.worktree_path = '/mock/.worktrees/wip';
      s.worktree_branch = 'wip';
    }
  });
  await archiveActive(page);
  await expect(page.locator('.choice-dialog')).toBeHidden();
  await expect(page.locator('[data-slot="undo-close"]')).toBeHidden();
});

test('the list shows every archived session and filters them', async ({
  page,
}) => {
  await boot(page);
  // The mock seeds a single session; archiving two needs a second.
  await page.evaluate(() => window.__hive.addSession?.('second'));
  await page.waitForFunction(
    () => (window.__hive.state?.sessions.length ?? 0) >= 2,
  );
  const names: string[] = [];
  for (let i = 0; i < 2; i++) {
    names.push(
      await page.evaluate(() => window.__hive.state?.sessions[0]?.name ?? ''),
    );
    await archiveActive(page);
  }

  await palette(page, 'Archived Sessions');
  const rows = page.locator('#archived-sessions .archived-row');
  await expect(rows).toHaveCount(2);
  for (const n of names) {
    await expect(page.locator('#archived-sessions')).toContainText(n);
  }

  // Layout is invisible to the DOM suite: prove the row's actions are
  // really on top and clickable, not covered by the backdrop or the list.
  const hit = await page.evaluate(() => {
    const b = document.querySelector<HTMLElement>(
      '#archived-sessions .archived-row button',
    );
    if (!b) return false;
    const r = b.getBoundingClientRect();
    const top = document.elementFromPoint(
      r.left + r.width / 2,
      r.top + r.height / 2,
    );
    return top === b || b.contains(top);
  });
  expect(hit).toBe(true);
  if (process.env.ARCHIVE_SHOT) {
    await page.screenshot({ path: process.env.ARCHIVE_SHOT });
  }

  await page.locator('#archived-sessions-input').fill(names[0]);
  await expect(rows).toHaveCount(1);
  await page.locator('#archived-sessions-input').fill('zzz-no-match');
  await expect(page.locator('#archived-sessions-nomatch')).toBeVisible();
});

test('Enter in the list unarchives the highlighted session and says what was lost', async ({
  page,
}) => {
  await boot(page);
  const name = await page.evaluate(
    () => window.__hive.state?.sessions[0]?.name ?? '',
  );
  await archiveActive(page);
  const before = await sessionCount(page);

  await palette(page, 'Archived Sessions');
  await expect(page.locator('#archived-sessions .archived-row')).toHaveCount(1);
  await page.keyboard.press('Enter');

  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) === n + 1,
    before,
  );
  await expect(page.locator('#archived-sessions')).toBeHidden();
  await expect(page.locator('#status')).toContainText(`Unarchived “${name}”`);
  await expect(page.locator('#status')).toContainText('Scrollback is gone');
});

test('Escape closes the list; delete asks before discarding', async ({
  page,
}) => {
  await boot(page);
  await archiveActive(page);
  await palette(page, 'Archived Sessions');
  await expect(page.locator('#archived-sessions')).toBeVisible();

  await page
    .locator('#archived-sessions .archived-row button', { hasText: 'Delete' })
    .click();
  await expect(page.locator('.choice-dialog')).toBeVisible();
  await page.locator('.choice-dialog button', { hasText: 'Cancel' }).click();
  await expect(page.locator('#archived-sessions .archived-row')).toHaveCount(1);

  await page.keyboard.press('Escape');
  await expect(page.locator('#archived-sessions')).toBeHidden();
});

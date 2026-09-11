// Playwright-SIDE seeder (unlike fixtures/xterm-reflow.ts, which is a
// browser-side module the page loads). Shared by sidebar-sticky.spec.ts and
// sidebar-group-flatten.spec.ts: two copies of this would drift, and the
// drift is silent — a copy that stops producing a scrollable list turns
// every sticky assertion vacuous.
import type { Page } from '@playwright/test';

// A group with enough plain sessions BEFORE it to push it down the list,
// and more after it so the list scrolls past. Sessions before the group
// are load-bearing: with the group at the top, scrolling its rows into
// view clamps scrollTop to 0 and the sticky assertions would hold for a
// header that never left its laid-out position.
export async function seedScrollableGroup(page: Page) {
  for (let i = 0; i < 8; i++) {
    await page.evaluate((n) => window.__hive.addSession?.(n), `before${i}`);
  }
  await page.waitForFunction(
    () => (window.__hive.state?.sessions.length ?? 0) >= 9,
  );
  await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('alpha', 'feat/sticky'),
  );
  await page.waitForFunction(() =>
    (window.__hive.state?.sessions ?? []).some((s) => !!s.worktree_path),
  );
  const wt = await page.evaluate(
    () =>
      (window.__hive.state?.sessions ?? []).find((s) => !!s.worktree_path)
        ?.worktree_path ?? '',
  );
  await page.evaluate(
    (p) => window.__hive.createSessionInWorktree?.('beta', p),
    wt,
  );
  for (let i = 0; i < 8; i++) {
    await page.evaluate((n) => window.__hive.addSession?.(n), `after${i}`);
  }
  await page.waitForFunction(
    () => (window.__hive.state?.sessions.length ?? 0) >= 19,
  );
  await page.waitForSelector('.hv-worktree-group__header');
}

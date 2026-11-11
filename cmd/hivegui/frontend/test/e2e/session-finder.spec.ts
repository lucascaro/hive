// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { expect, type Page, test } from '@playwright/test';

// Go to session (spec 544) against the Wails mock: the real chord through
// the real key listener, the real modal, and the real focus pipeline.

const isMac = process.platform === 'darwin';
const CHORD = isMac ? 'Meta+p' : 'Control+p';

const activeId = (page: Page) =>
  page.evaluate(() => window.__hive_state?.activeId ?? null);
const finder = (page: Page) => page.locator('#session-finder');
const rows = (page: Page) => page.locator('#session-finder .finder-row');
const input = (page: Page) => page.locator('#session-finder-input');

async function boot(page: Page, extra = 2) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await page.evaluate(async (n) => {
    for (let i = 2; i <= n + 1; i++) await window.__hive.addSession?.(`s${i}`);
  }, extra);
  await page.waitForFunction(
    (n) => (window.__hive_state?.sessions.length ?? 0) >= n + 1,
    extra,
  );
}

async function openFinder(page: Page) {
  await page.keyboard.press(CHORD);
  await expect(finder(page)).toBeVisible();
  await expect(input(page)).toBeFocused();
}

/** Mirrors the daemon announcing a changed session. */
async function updateSession(
  page: Page,
  id: string,
  patch: Record<string, unknown>,
) {
  await page.evaluate(
    ([sid, p]) => {
      const s = window.__hive.state?.sessions.find((x) => x.id === sid);
      if (!s) throw new Error(`no session ${sid}`);
      Object.assign(s, p);
      window.__hive.emit(
        'session:event',
        JSON.stringify({ kind: 'updated', session: s }),
      );
    },
    [id, patch] as const,
  );
}

async function termFocused(page: Page) {
  await expect
    .poll(() =>
      page.evaluate(() =>
        document.activeElement?.classList?.contains('xterm-helper-textarea'),
      ),
    )
    .toBe(true);
}

test.describe('session finder', () => {
  test('chord, type a branch fragment, Enter: lands in a session in another, collapsed project', async ({
    page,
  }) => {
    await boot(page, 1);
    // A second project holding the target, then collapsed so its rows are
    // not in the sidebar at all.
    const target = await page.evaluate(async () => {
      const p = {
        id: 'p-zeta',
        name: 'zeta-proj',
        color: '#0af',
        cwd: '',
        order: 1,
        created: new Date().toISOString(),
      };
      window.__hive.state?.projects.push(p);
      window.__hive.emit(
        'project:event',
        JSON.stringify({ kind: 'added', project: p }),
      );
      return window.__hive.addSession?.('far-away', undefined, 'p-zeta');
    });
    await updateSession(page, target as string, {
      worktree_branch: 'feat/needle-branch',
    });
    await page.getByRole('button', { name: 'Collapse zeta-proj' }).click();
    await expect(page.locator('[data-collapsed]')).toHaveCount(1);

    // Adding the target activated it; go home to a session in p1 first.
    const home = await page.evaluate(
      () =>
        window.__hive_state?.sessions.find((x) => x.project_id !== 'p-zeta')
          ?.id,
    );
    await page.click(`#projects li[data-sid="${home}"]`);
    await expect.poll(() => activeId(page)).toBe(home);
    const before = home;
    await page.evaluate(() => (document.activeElement as HTMLElement)?.blur());
    await openFinder(page);
    await input(page).fill('needle');
    await expect(rows(page)).toHaveCount(1);
    await page.keyboard.press('Enter');

    await expect(finder(page)).toBeHidden();
    await expect.poll(() => activeId(page)).toBe(target);
    expect(await activeId(page)).not.toBe(before);
    // Focus is in the terminal, not just the sidebar selection.
    await termFocused(page);
  });

  test('matches each field on its own: name, title, branch, project', async ({
    page,
  }) => {
    await boot(page, 3);
    const ids = await page.evaluate(
      () => window.__hive_state?.sessions.map((s) => s.id) ?? [],
    );
    await updateSession(page, ids[0], { name: 'qqnameqq' });
    await updateSession(page, ids[1], { title: 'qqtitleqq' });
    await updateSession(page, ids[2], { worktree_branch: 'qqbranchqq' });
    const projectName = await page.evaluate(
      () => window.__hive_state?.projects[0]?.name ?? '',
    );
    await openFinder(page);
    for (const [q, id] of [
      ['qqnameqq', ids[0]],
      ['qqtitleqq', ids[1]],
      ['qqbranchqq', ids[2]],
    ] as const) {
      await input(page).fill(q);
      await expect(rows(page)).toHaveCount(1);
      await expect(rows(page).first()).toHaveAttribute('data-sid', id);
    }
    await input(page).fill(projectName);
    await expect(rows(page)).toHaveCount(ids.length);
  });

  test('empty query: chord then Enter flips back to the previous session', async ({
    page,
  }) => {
    await boot(page, 2);
    const ids = await page.evaluate(
      () => window.__hive_state?.sessions.map((s) => s.id) ?? [],
    );
    await page.click(`#projects li[data-sid="${ids[0]}"]`);
    await expect.poll(() => activeId(page)).toBe(ids[0]);
    await page.click(`#projects li[data-sid="${ids[1]}"]`);
    await expect.poll(() => activeId(page)).toBe(ids[1]);

    await openFinder(page);
    await expect(rows(page).first()).toHaveAttribute('data-sid', ids[0]);
    await page.keyboard.press('Enter');
    await expect.poll(() => activeId(page)).toBe(ids[0]);
    await termFocused(page);
  });

  test('Escape closes, keeps the selection and returns focus to the terminal', async ({
    page,
  }) => {
    await boot(page, 1);
    const before = await activeId(page);
    await openFinder(page);
    await page.keyboard.press('Escape');
    await expect(finder(page)).toBeHidden();
    expect(await activeId(page)).toBe(before);
    await termFocused(page);
  });

  test('the chord pressed again closes the finder', async ({ page }) => {
    await boot(page, 1);
    await openFinder(page);
    await page.keyboard.press(CHORD);
    await expect(finder(page)).toBeHidden();
  });

  test('a rename and a branch change are matched by their new value', async ({
    page,
  }) => {
    await boot(page, 1);
    const id = (await page.evaluate(
      () => window.__hive_state?.sessions[0]?.id,
    )) as string;
    await updateSession(page, id, {
      name: 'renamed-live',
      worktree_branch: 'branch-live',
    });
    await openFinder(page);
    await input(page).fill('renamed-live');
    await expect(rows(page)).toHaveCount(1);
    await input(page).fill('branch-live');
    await expect(rows(page)).toHaveCount(1);
    await input(page).fill('s1');
    // the old name no longer matches
    await expect(rows(page).filter({ hasText: 'renamed-live' })).toHaveCount(0);
  });

  test('a minimized target is restored on the way in', async ({ page }) => {
    await boot(page, 2);
    const ids = await page.evaluate(
      () => window.__hive_state?.sessions.map((s) => s.id) ?? [],
    );
    await page.click(`#projects li[data-sid="${ids[0]}"]`);
    await page.evaluate(async (id) => {
      const url = '/src/app/view.ts';
      const m = await import(/* @vite-ignore */ url);
      m.minimizeSession(id);
    }, ids[1]);
    await updateSession(page, ids[1], { name: 'hidden-target' });
    await openFinder(page);
    await input(page).fill('hidden-target');
    await page.keyboard.press('Enter');
    await expect.poll(() => activeId(page)).toBe(ids[1]);
    expect(
      await page.evaluate(async (id) => {
        const url = '/src/app/view.ts';
        const m = await import(/* @vite-ignore */ url);
        return m.isSessionHidden(id);
      }, ids[1]),
    ).toBe(false);
    await termFocused(page);
  });

  test('100 sessions: open and filter stay fast', async ({ page }) => {
    await boot(page, 0);
    await page.evaluate(async () => {
      for (let i = 2; i <= 100; i++)
        await window.__hive.addSession?.(`bulk-${i}`);
    });
    await page.waitForFunction(
      () => (window.__hive_state?.sessions.length ?? 0) >= 100,
    );
    const open = await page.evaluate(async (chordIsMac) => {
      const t0 = performance.now();
      window.dispatchEvent(
        new KeyboardEvent('keydown', {
          key: 'p',
          code: 'KeyP',
          metaKey: chordIsMac,
          ctrlKey: !chordIsMac,
          bubbles: true,
          cancelable: true,
        }),
      );
      await new Promise<void>((r) =>
        requestAnimationFrame(() => requestAnimationFrame(() => r())),
      );
      return performance.now() - t0;
    }, isMac);
    await expect(finder(page)).toBeVisible();
    await expect(rows(page)).toHaveCount(100);
    // Generous CI-safe bound: a perceptible lag is hundreds of ms.
    expect(open).toBeLessThan(1000);
    const t1 = Date.now();
    await input(page).fill('bulk-9');
    await expect(rows(page)).not.toHaveCount(100);
    expect(Date.now() - t1).toBeLessThan(1500);
  });
});

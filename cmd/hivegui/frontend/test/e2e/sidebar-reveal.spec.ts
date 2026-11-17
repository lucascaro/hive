// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { test, expect, type Page } from '@playwright/test';
import { seedScrollableGroup } from './fixtures/seed-worktree-group.js';

// #563: whichever way the active session changes, its sidebar row ends up
// fully visible — scrolled into #projects clear of the sticky project and
// worktree-group headers, or peeked out of a collapsed card or group.
// jsdom cannot see any of this (no layout, no stylesheet), so the real
// visibility claims live here. "Visible" is checked with elementFromPoint
// at the row's top and bottom edges: a row under a sticky header is inside
// the scroller's rect and still covered.

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';
// nav-back: Ctrl+- on macOS, Ctrl+Alt+- elsewhere (src/lib/bindings.ts).
const NAV_BACK = process.platform === 'darwin' ? 'Control+-' : 'Control+Alt+-';

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  // A short sidebar, so the seeded list scrolls.
  await page.setViewportSize({ width: 1100, height: 420 });
  await seedScrollableGroup(page);
}

const activeId = (page: Page) =>
  page.evaluate(() => window.__hive_state?.activeId ?? null);

const sidOf = (page: Page, name: string) =>
  page.evaluate(
    (n) => window.__hive.state?.sessions.find((s) => s.name === n)?.id ?? '',
    name,
  );

// The first painted row (the mock seeds one session before the fixture's).
const firstRow = (page: Page) =>
  page.evaluate(
    () =>
      document.querySelector<HTMLElement>('#projects .hv-session-row')?.dataset
        .sid ?? '',
  );

const scrollTop = (page: Page) =>
  page.evaluate(() => document.getElementById('projects')?.scrollTop ?? -1);

const scrollTo = (page: Page, y: number | 'bottom') =>
  page.evaluate((to) => {
    const el = document.getElementById('projects');
    if (el) el.scrollTop = to === 'bottom' ? el.scrollHeight : to;
  }, y);

// True when the row's top and bottom edges both sit inside #projects and
// are not covered by anything (a sticky header) at those points.
async function fullyVisible(page: Page, sid: string): Promise<boolean> {
  return page.evaluate((id) => {
    const scroller = document.getElementById('projects');
    const row = Array.from(
      document.querySelectorAll<HTMLElement>('#projects .hv-session-row'),
    ).find((el) => el.dataset.sid === id && el.getClientRects().length > 0);
    if (!scroller || !row) return false;
    const r = row.getBoundingClientRect();
    const v = scroller.getBoundingClientRect();
    if (r.top < v.top || r.bottom > v.bottom) return false;
    const x = r.left + r.width / 2;
    const hit = (y: number) => row.contains(document.elementFromPoint(x, y));
    return hit(r.top + 1) && hit(r.bottom - 1);
  }, sid);
}

async function expectRevealed(page: Page, sid: string) {
  await expect.poll(() => activeId(page)).toBe(sid);
  await expect.poll(() => fullyVisible(page, sid)).toBe(true);
}

test.describe('the sidebar follows the active session (#563)', () => {
  test('keyboard nav reveals an off-screen row below the sticky headers', async ({
    page,
  }) => {
    await boot(page);
    const first = await firstRow(page);
    // Land at the top of the list, then scroll it away to the bottom.
    await page.locator('#projects .hv-session-row').first().click();
    await scrollTo(page, 'bottom');
    expect(await fullyVisible(page, first)).toBe(false);
    // ⌘↓ then ⌘↑ lands back on the first row: an upward reveal.
    await page.keyboard.press(`${MOD}+ArrowDown`);
    await page.keyboard.press(`${MOD}+ArrowUp`);
    await expectRevealed(page, first);

    // And downward: from the top to the last row.
    const last = await sidOf(page, 'after7');
    await scrollTo(page, 0);
    for (let i = 0; i < 30 && (await activeId(page)) !== last; i++) {
      await page.keyboard.press(`${MOD}+ArrowDown`);
    }
    await expectRevealed(page, last);
  });

  test('upward nav clears both sticky headers for a grouped row', async ({
    page,
  }) => {
    await boot(page);
    const alpha = await sidOf(page, 'alpha');
    const beta = await sidOf(page, 'beta');
    await page.locator(`#projects .hv-session-row[data-sid="${beta}"]`).click();
    // Scroll until alpha (the group's first member) sits 4px under the
    // pinned project + group header stack: both headers are stuck then, so
    // the row is inside the scroller's rect and still covered.
    await page.evaluate((id) => {
      const scroller = document.getElementById('projects');
      const row = document.querySelector(
        `#projects .hv-session-row[data-sid="${id}"]`,
      );
      const cardHead = row
        ?.closest('.hv-project-card')
        ?.querySelector<HTMLElement>('.hv-project-card__header');
      const groupHead = row
        ?.closest('.hv-worktree-group')
        ?.querySelector<HTMLElement>('.hv-worktree-group__header');
      if (!scroller || !row || !cardHead || !groupHead) return;
      const covered =
        scroller.getBoundingClientRect().top +
        cardHead.offsetHeight +
        groupHead.offsetHeight -
        4;
      scroller.scrollTop += row.getBoundingClientRect().top - covered;
    }, alpha);
    expect(await fullyVisible(page, alpha)).toBe(false);
    await page.keyboard.press(`${MOD}+ArrowUp`);
    await expectRevealed(page, alpha);
  });

  test('session finder, nav history and ⌘B each reveal the row', async ({
    page,
  }) => {
    await boot(page);
    const first = await firstRow(page);
    const last = await sidOf(page, 'after7');

    // Session finder.
    await page.locator('#projects .hv-session-row').first().click();
    await page.keyboard.press(`${MOD}+p`);
    await page.locator('#command-palette-input').fill('after7');
    await page.keyboard.press('Enter');
    await expectRevealed(page, last);

    // Nav history: back to where we came from, which is scrolled away.
    await scrollTo(page, 'bottom');
    await page.keyboard.press(NAV_BACK);
    await expectRevealed(page, first);

    // ⌘B: the next session needing attention, off-screen.
    await page.evaluate((id) => window.__hive.ringBell?.(id), last);
    await scrollTo(page, 0);
    await page.keyboard.press(`${MOD}+b`);
    await expectRevealed(page, last);
  });

  test('a grid tile click reveals the row', async ({ page }) => {
    await boot(page);
    const last = await sidOf(page, 'after7');
    await page.locator('#projects .hv-session-row').first().click();
    await page.keyboard.press(`${MOD}+Shift+g`);
    const tile = page.locator(`#terms .term-host.in-grid[data-sid="${last}"]`);
    await expect(tile).toBeVisible();
    await scrollTo(page, 0);
    await tile.locator('.tile-name').click();
    await expectRevealed(page, last);
  });

  test('a session that spawns as active is revealed', async ({ page }) => {
    await boot(page);
    await scrollTo(page, 0);
    // session:event `added` → switchTo, with no user input (events.ts).
    await page.evaluate(() => window.__hive.addSession?.('spawned'));
    await expect.poll(() => sidOf(page, 'spawned')).not.toBe('');
    await expectRevealed(page, await sidOf(page, 'spawned'));
  });

  test('clicking a fully visible row does not move the list', async ({
    page,
  }) => {
    await boot(page);
    await scrollTo(page, 120);
    const before = await scrollTop(page);
    const sid = await page.evaluate(() => {
      const scroller = document.getElementById('projects');
      if (!scroller) return '';
      const v = scroller.getBoundingClientRect();
      // A row comfortably mid-view, clear of the sticky headers.
      return (
        Array.from(
          document.querySelectorAll<HTMLElement>('#projects .hv-session-row'),
        ).find((r) => {
          const b = r.getBoundingClientRect();
          return b.top > v.top + 80 && b.bottom < v.bottom - 10;
        })?.dataset.sid ?? ''
      );
    });
    expect(sid).not.toBe('');
    await page.locator(`#projects .hv-session-row[data-sid="${sid}"]`).click();
    await expect.poll(() => activeId(page)).toBe(sid);
    expect(await scrollTop(page)).toBe(before);

    // Same for a keyboard step onto a neighbour that is already visible.
    await page.keyboard.press(`${MOD}+ArrowDown`);
    await expect.poll(() => activeId(page)).not.toBe(sid);
    expect(await scrollTop(page)).toBe(before);
  });

  test('a collapsed project peeks the active row', async ({ page }) => {
    await boot(page);
    const card = page.locator('.hv-project-card').first();
    const first = await firstRow(page);
    const second = await sidOf(page, 'before0');
    await page
      .locator(`#projects .hv-session-row[data-sid="${first}"]`)
      .click();
    await card.locator('.hv-project-card__chevron').click();
    await expect(card.locator('.hv-project-card__chevron')).toHaveAttribute(
      'aria-expanded',
      'false',
    );
    await page.keyboard.press(`${MOD}+ArrowDown`);
    await expect.poll(() => activeId(page)).toBe(second);
    const peek = card.locator('.hv-project-card__peek .hv-session-row');
    await expect(peek).toHaveCount(1);
    await expect(peek).toBeVisible();
    await expect(peek).toHaveAttribute('data-sid', second);
    // Everything else stays collapsed. toBeHidden auto-waits out the
    // 160ms visibility delay of the collapse transition (use-collapse.ts).
    await expect(
      card.locator(
        `.hv-project-card__body .hv-session-row[data-sid="${first}"]`,
      ),
    ).toBeHidden();
    await expect(card.locator('.hv-project-card__chevron')).toHaveAttribute(
      'aria-expanded',
      'false',
    );
    expect(await fullyVisible(page, second)).toBe(true);
  });

  test('a switch during an expand animation is revealed once it settles', async ({
    page,
  }) => {
    await boot(page);
    const first = await firstRow(page);
    const last = await sidOf(page, 'after7');
    const card = page.locator('.hv-project-card').first();
    const chevron = card.locator('.hv-project-card__chevron');
    await page.locator(`#projects .hv-session-row[data-sid="${last}"]`).click();
    await page
      .locator(`#projects .hv-session-row[data-sid="${first}"]`)
      .click();
    await chevron.click();
    await expect(chevron).toHaveAttribute('aria-expanded', 'false');
    // Expand, then go back to the last row while the body is still growing:
    // the list is too short to scroll that far yet, so the first reveal
    // clamps, and only a reveal after the 160ms transition lands the row.
    await chevron.click();
    await page.keyboard.press(NAV_BACK);
    await expectRevealed(page, last);
  });

  test('a collapsed worktree group peeks the active row', async ({ page }) => {
    await boot(page);
    const alpha = await sidOf(page, 'alpha');
    const beta = await sidOf(page, 'beta');
    const group = page.locator('.hv-worktree-group').first();
    await page
      .locator(`#projects .hv-session-row[data-sid="${alpha}"]`)
      .click();
    await group.locator('.hv-worktree-group__chevron').click();
    await page.keyboard.press(`${MOD}+ArrowDown`);
    await expect.poll(() => activeId(page)).toBe(beta);
    const peek = group.locator('.hv-worktree-group__peek .hv-session-row');
    await expect(peek).toHaveCount(1);
    await expect(peek).toHaveAttribute('data-sid', beta);
    await expect(peek).toBeVisible();
    await expect(
      group.locator(
        `.hv-worktree-group__body .hv-session-row[data-sid="${alpha}"]`,
      ),
    ).toBeHidden();
    await expect(group).toHaveAttribute('data-collapsed', '');
    await expectRevealed(page, beta);
  });
});

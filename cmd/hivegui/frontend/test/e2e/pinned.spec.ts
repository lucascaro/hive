// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { test, expect, type Page } from '@playwright/test';

// Spec 566: pinned responses. The claims here are layout and focus — the
// Pinned section stops at its cap and scrolls inside itself, the timeline
// keeps room, the sidebar badge never grows a row, and nothing about pins
// ever takes the keyboard. jsdom measures none of it.

const MAC = process.platform === 'darwin';
const TOGGLE = MAC ? 'Meta+j' : 'Control+Shift+J';
const MOD = MAC ? 'Meta' : 'Control';
// focus.ts re-asserts terminal focus for 500ms after a focus drive; a
// check inside that window would pass even if a click stole focus.
const PAST_FOCUS_GUARD_MS = 700;

const many = (n: number) =>
  Array.from({ length: n }, (_, i) => ({
    title: `Pinned answer number ${i + 1}`,
    body: `Body **${i + 1}**`,
  }));

async function boot(page: Page, pins = many(3)) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
  await page.waitForFunction(
    () => document.activeElement?.classList?.contains('xterm-helper-textarea'),
    null,
    { timeout: 3000 },
  );
  const id = await page.evaluate(
    () => window.__hive.state?.sessions[0]?.id ?? '',
  );
  await page.evaluate(
    ([sid, p]) => {
      window.__hive.setSessionState?.(sid as string, 'working', 'hook');
      window.__hive.setActivity?.(sid as string, {
        stale_at: new Date(Date.now() + 600_000).toISOString(),
        plan: [{ id: 'a', text: 'do it', status: 'active', tools: 1 }],
        events: Array.from({ length: 40 }, (_, i) => ({
          tool: 'Read',
          target: `f${i}.ts`,
          call_id: `c${i}`,
          plan_idx: 0,
          started_at: new Date(Date.now() - 1000).toISOString(),
          ended_at: new Date().toISOString(),
          duration_ms: 10,
          ok: true,
        })),
      });
      window.__hive.setPins?.(
        sid as string,
        p as { title: string; body: string }[],
      );
    },
    [id, pins] as const,
  );
  return id;
}

const isTermFocused = (page: Page) =>
  page.evaluate(() =>
    document.activeElement?.classList?.contains('xterm-helper-textarea'),
  );

const height = (page: Page, sel: string) =>
  page
    .locator(sel)
    .first()
    .evaluate((el) => el.getBoundingClientRect().height);

test.describe('spec 566 pinned responses', () => {
  test('overflowing pins scroll inside a capped section and the timeline keeps height', async ({
    page,
  }) => {
    await boot(page, many(60));
    await page.keyboard.press(TOGGLE);
    const panel = page.locator('#activity-panel');
    await expect(panel.locator('.hv-activity__pin')).toHaveCount(60);

    const panelH = await height(page, '#activity-panel .hv-activity--panel');
    const pinsH = await height(page, '#activity-panel .hv-activity__pins');
    // At most 30% of the panel, plus a pixel of rounding.
    expect(pinsH).toBeLessThanOrEqual(panelH * 0.3 + 1);
    const list = panel.locator('.hv-activity__pin-list');
    const { scrollH, clientH } = await list.evaluate((el) => ({
      scrollH: el.scrollHeight,
      clientH: el.clientHeight,
    }));
    expect(scrollH).toBeGreaterThan(clientH);
    // The plan and the timeline still get real room.
    expect(
      await height(page, '#activity-panel .hv-activity__timeline'),
    ).toBeGreaterThan(40);
    expect(
      await height(page, '#activity-panel .hv-activity__plan'),
    ).toBeGreaterThan(0);

    // Scrolling the list moves the list, not the panel.
    await list.hover();
    await page.mouse.wheel(0, 300);
    await expect
      .poll(() => list.evaluate((el) => el.scrollTop))
      .toBeGreaterThan(0);
    expect(
      await panel.locator('.hv-activity--panel').evaluate((el) => el.scrollTop),
    ).toBe(0);
  });

  test('newest first, one line each; a click opens the body', async ({
    page,
  }) => {
    await boot(page);
    await page.keyboard.press(TOGGLE);
    const rows = page.locator('#activity-panel .hv-activity__pin');
    await expect(rows).toHaveCount(3);
    await expect(rows.first().locator('.hv-activity__pin-title')).toHaveText(
      'Pinned answer number 1',
    );
    const one = await height(page, '#activity-panel .hv-activity__pin-row');
    expect(one).toBeLessThan(24);
    await rows.first().locator('.hv-activity__pin-main').click();
    await expect(
      rows.first().locator('.hv-activity__pin-body strong'),
    ).toHaveText('1');
  });

  test('clicking pins, copy and unpin never takes the keyboard', async ({
    page,
  }) => {
    const id = await boot(page);
    await page.keyboard.press(TOGGLE);
    const panel = page.locator('#activity-panel');
    await expect(panel.locator('.hv-activity__pin')).toHaveCount(3);
    await page.waitForTimeout(PAST_FOCUS_GUARD_MS);

    await panel.locator('.hv-activity__pin-main').first().click();
    await panel.locator('.hv-activity__pins-toggle').click();
    await panel.locator('.hv-activity__pins-toggle').click();
    await panel.locator('[aria-label="Copy pinned response"]').first().click();
    await panel.locator('[aria-label="Unpin"]').last().click();
    await expect(panel.locator('.hv-activity__pin')).toHaveCount(2);
    await page.waitForTimeout(PAST_FOCUS_GUARD_MS);
    expect(await isTermFocused(page)).toBe(true);

    await page.evaluate(() => window.__hive.resetStdin());
    await page.keyboard.type('hi');
    await expect
      .poll(() => page.evaluate((sid) => window.__hive.stdinText(sid), id))
      .toContain('hi');
  });

  test('copy puts the body on the clipboard', async ({ page }) => {
    await boot(page);
    await page.keyboard.press(TOGGLE);
    await page
      .locator('#activity-panel [aria-label="Copy pinned response"]')
      .nth(1)
      .click();
    await expect
      .poll(() => page.evaluate(() => window.__hive.clipboard?.()))
      .toBe('Body **2**');
  });

  test('unpin all confirms with its keys shown; cancel keeps pins; terminal refocused', async ({
    page,
  }) => {
    const id = await boot(page);
    await page.keyboard.press(TOGGLE);
    const panel = page.locator('#activity-panel');
    await expect(panel.locator('.hv-activity__pin')).toHaveCount(3);

    await panel.locator('.hv-activity__pins-clear').click();
    const dialog = page.locator('#choice-dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog.locator('.hv-dialog__hint')).toHaveText([
      '[enter] Unpin all',
      '[esc] Cancel',
    ]);
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();
    await expect(panel.locator('.hv-activity__pin')).toHaveCount(3);
    expect(
      await page.evaluate((sid) => window.__hive.pins?.(sid).length, id),
    ).toBe(3);
    await expect.poll(() => isTermFocused(page)).toBe(true);

    // The reported bug: Enter used to cancel here. It must unpin.
    await panel.locator('.hv-activity__pins-clear').click();
    await page.keyboard.press('Enter');
    await expect(panel.locator('.hv-activity__pins')).toHaveCount(0);
    expect(
      await page.evaluate((sid) => window.__hive.pins?.(sid).length, id),
    ).toBe(0);
    await expect.poll(() => isTermFocused(page)).toBe(true);
  });

  test('a pin the agent adds shows up live, newest first', async ({ page }) => {
    const id = await boot(page);
    await page.keyboard.press(TOGGLE);
    const rows = page.locator('#activity-panel .hv-activity__pin');
    await expect(rows).toHaveCount(3);
    await page.evaluate(
      (sid) => window.__hive.addPin?.(sid, 'Fresh', 'new'),
      id,
    );
    await expect(rows).toHaveCount(4);
    await expect(rows.first().locator('.hv-activity__pin-title')).toHaveText(
      'Fresh',
    );
  });

  test('badge count matches the inspector count', async ({ page }) => {
    const id = await boot(page);
    await page.keyboard.press(TOGGLE);
    const badge = page
      .locator('#projects .hv-session-row')
      .first()
      .locator('.hv-session-row__pins');
    await expect(badge).toHaveText('3');
    await expect(page.locator('#activity-panel .hv-activity__pin')).toHaveCount(
      3,
    );
    await page.evaluate(
      (sid) => window.__hive.addPin?.(sid, 'Fresh', 'new'),
      id,
    );
    await expect(badge).toHaveText('4');
    await expect(page.locator('#activity-panel .hv-activity__pin')).toHaveCount(
      4,
    );
  });

  test('sidebar pin badge does not change row height, at any density', async ({
    page,
  }) => {
    const id = await boot(page, []);
    const row = page.locator('#projects .hv-session-row').first();
    const rowH = () => row.evaluate((el) => el.getBoundingClientRect().height);
    for (const [density, expected] of [
      ['normal', 40],
      ['tight', 34],
      ['compact', 28],
    ] as const) {
      if (density !== 'normal') {
        await page.keyboard.press(`${MOD}+,`);
        await page.locator('#settings-tab-appearance').click();
        await page.locator('#settings-density').selectOption(density);
        await page.keyboard.press('Escape');
        await expect(page.locator('#settings-density')).toBeHidden();
      }
      await page.evaluate((sid) => window.__hive.setPins?.(sid, []), id);
      await expect(row.locator('.hv-session-row__pins')).toHaveCount(0);
      const without = await rowH();
      expect(without).toBeCloseTo(expected, 0);
      await page.evaluate(
        (sid) => window.__hive.setPins?.(sid, [{ title: 't', body: 'b' }]),
        id,
      );
      await expect(row.locator('.hv-session-row__pins')).toBeVisible();
      expect(await rowH()).toBe(without);
    }
  });

  test('activity-grid tile lists pins without focus or keystrokes', async ({
    page,
  }) => {
    await boot(page);
    // A grid needs more than one session.
    await page.evaluate(() => window.__hive.addSession?.('s2'));
    await page.waitForFunction(
      () => (window.__hive.state?.sessions.length ?? 0) >= 2,
    );
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await page.keyboard.press(TOGGLE);
    await expect(page.locator('#terms')).toHaveClass(/activity/);
    const tilePins = page.locator('.activity-tile .hv-activity__tile-pin');
    await expect(tilePins.first()).toBeVisible();
    await expect(
      page.locator('.activity-tile button, .activity-tile [tabindex]'),
    ).toHaveCount(0);
    await tilePins.first().click();
    await page.evaluate(() => window.__hive.resetStdin());
    await page.keyboard.type('zz');
    await page.waitForTimeout(300);
    expect(await page.evaluate(() => window.__hive.stdinText())).toBe('');
  });
});

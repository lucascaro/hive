// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { type Page, expect, test } from '@playwright/test';

// Regression (#580): the new-session launcher opened at a fixed offset
// under its project's header, so for a project near the bottom of the
// window it ran off the bottom edge and its lower rows were unreachable.
// It now opens below the header when it fits, above it when it does not,
// and otherwise is clamped into the window and scrolls.
//
// Each test asserts its layout preconditions first, from the popup's
// measured height, so a layout drift that lands in a different branch
// fails loudly instead of passing for the wrong reason.

const GAP = 4;
const MARGIN = 8;

const header = (pid: string) =>
  `.hv-project-card[data-pid="${pid}"] .hv-project-card__header`;

async function openLauncherFor(page: Page, pid: string) {
  const h = page.locator(header(pid));
  await h.hover();
  await h.locator('[data-action="new"]').click();
  await page.waitForSelector('#launcher:not(.hidden) .launcher-item');
}

type Rects = {
  header: { top: number; bottom: number };
  popup: {
    top: number;
    bottom: number;
    height: number;
    scrollHeight: number;
    clientHeight: number;
  };
  vh: number;
};

function rects(page: Page, pid: string): Promise<Rects> {
  return page.evaluate((sel) => {
    const h = document.querySelector(sel);
    const l = document.getElementById('launcher');
    if (!h || !l) throw new Error('header or #launcher missing');
    const hr = h.getBoundingClientRect();
    const lr = l.getBoundingClientRect();
    return {
      header: { top: hr.top, bottom: hr.bottom },
      popup: {
        top: lr.top,
        bottom: lr.bottom,
        height: lr.height,
        scrollHeight: l.scrollHeight,
        clientHeight: l.clientHeight,
      },
      vh: window.innerHeight,
    };
  }, header(pid));
}

// The agent list loads after the popup opens, so the size (and with it
// the placement) settles a tick later. Wait for two equal readings.
async function settled(page: Page, pid: string): Promise<Rects> {
  let last = '';
  let r = await rects(page, pid);
  await expect
    .poll(async () => {
      r = await rects(page, pid);
      const key = JSON.stringify(r);
      const same = key === last;
      last = key;
      return same;
    })
    .toBe(true);
  return r;
}

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('.hv-project-card').length > 0,
  );
});

test('opens below its project header when there is room', async ({ page }) => {
  await openLauncherFor(page, 'p1');
  const r = await settled(page, 'p1');
  expect(r.popup.top).toBeGreaterThanOrEqual(r.header.bottom);
  expect(r.popup.bottom).toBeLessThanOrEqual(r.vh);
});

test('flips above the header when a project near the bottom would overflow', async ({
  page,
}) => {
  // Fill the seed project so a second project's card sits well down the
  // sidebar, then shrink the window until that card is at the bottom.
  const pid = await page.evaluate(async () => {
    for (let i = 0; i < 14; i++) await window.__hive.addSession?.(`s-${i}`);
    return (await window.__hive.addProject?.('bottom')) ?? '';
  });
  const h = page.locator(header(pid));
  await expect(h).toBeVisible();
  const box = await h.boundingBox();
  if (!box) throw new Error('no header box');
  await page.setViewportSize({
    width: 1200,
    height: Math.ceil(box.y + box.height + 40),
  });
  await expect(h).toBeInViewport();

  await openLauncherFor(page, pid);
  const r = await settled(page, pid);

  // The bug is real here: below the header, the popup would overflow.
  expect(r.header.bottom + GAP + r.popup.height).toBeGreaterThan(r.vh);
  // And above fits, so this is the flip branch, not the clamp.
  expect(r.header.top - GAP - r.popup.height).toBeGreaterThanOrEqual(MARGIN);

  expect(r.popup.bottom).toBeLessThanOrEqual(r.header.top);
  expect(r.popup.top).toBeGreaterThanOrEqual(0);
});

test('clamps and scrolls when neither side fits', async ({ page }) => {
  await page.setViewportSize({ width: 1200, height: 220 });
  await expect(page.locator(header('p1'))).toBeInViewport();

  await openLauncherFor(page, 'p1');
  const r = await settled(page, 'p1');

  // Neither side fits the popup's natural height.
  expect(r.header.bottom + GAP + r.popup.scrollHeight).toBeGreaterThan(
    r.vh - MARGIN,
  );
  expect(r.header.top - GAP - r.popup.scrollHeight).toBeLessThan(MARGIN);

  expect(r.popup.top).toBeGreaterThanOrEqual(0);
  expect(r.popup.bottom).toBeLessThanOrEqual(r.vh);
  expect(r.popup.scrollHeight).toBeGreaterThan(r.popup.clientHeight);

  // Every row is reachable: the last one scrolls into view and is the
  // element actually hit at its centre.
  const hit = await page.evaluate(() => {
    const items = document.querySelectorAll('#launcher .launcher-item');
    const last = items[items.length - 1] as HTMLElement | undefined;
    if (!last) return false;
    last.scrollIntoView({ block: 'nearest' });
    const b = last.getBoundingClientRect();
    const at = document.elementFromPoint(
      b.left + b.width / 2,
      b.top + b.height / 2,
    );
    return !!at && last.contains(at);
  });
  expect(hit).toBe(true);
});

import { test, expect, type Page } from '@playwright/test';
// The persisted key is namespaced by the daemon's state-dir id (#340).
// Kept in sync by hand with MOCK_STATE_DIR_ID in wails-mock.ts: that
// module runs in the browser and imports the store, so importing it from
// the Playwright node context fails on `import.meta`.
const MIN_KEY = 'hive.minimizedProjects.mock1234';

// Layout check for the minimized-projects tray. The DOM test proves the
// rows move; only a real browser proves the tray actually sits at the
// bottom of the sidebar — #projects has to take the free space, or the
// tray floats directly under the last project row instead of hugging
// the footer.

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.hv-project-card').length > 0,
  );
}

test('a minimized project drops to a tray at the bottom of the sidebar', async ({
  page,
}) => {
  await boot(page);
  const listed = page.locator('#projects > li.hv-project-card');
  const before = await listed.count();
  const first = listed.first();
  const pid = await first.getAttribute('data-pid');

  // Card actions are hover-revealed (patterns.md › Hover-revealed
  // actions), so the pointer has to be on the header first.
  await first.locator('.hv-project-card__header').hover();
  await first
    .locator('.hv-project-card__header [data-action="minimize"]')
    .click();

  await expect(listed).toHaveCount(before - 1);
  const chip = page.locator(`#minimized-projects .hv-chip[data-pid="${pid}"]`);
  await expect(chip).toBeVisible();

  // The tray hugs the bottom: its box sits below the project list and
  // above the version footer, with no gap left between it and the
  // footer beyond the footer's own height.
  const boxes = await page.evaluate(() => {
    const r = (sel: string) =>
      document.querySelector(sel)?.getBoundingClientRect();
    return {
      list: r('#projects')?.bottom ?? 0,
      trayTop: r('#minimized-projects')?.top ?? 0,
      trayBottom: r('#minimized-projects')?.bottom ?? 0,
      footerTop: r('#sidebar-hints')?.top ?? 0,
    };
  });
  expect(boxes.trayTop).toBeGreaterThanOrEqual(boxes.list - 1);
  expect(Math.abs(boxes.trayBottom - boxes.footerTop)).toBeLessThan(2);

  // Restore puts the row back.
  await chip.locator('.hv-chip__restore').click();
  await expect(listed).toHaveCount(before);
  await expect(page.locator('#minimized-projects')).toBeHidden();
});

// A minimized project has no session rows left, so its chip is the only
// surface a bell inside it can reach (patterns.md › Attention bubbling).
test('a bell inside a minimized project lights its chip', async ({ page }) => {
  await boot(page);
  const first = page.locator('#projects > li.hv-project-card').first();
  const pid = await first.getAttribute('data-pid');
  const sid = await first
    .locator('.hv-session-row')
    .first()
    .getAttribute('data-sid');
  // onSessionBell ignores a bell on the active+focused session, and the
  // seed session is active on boot. A second one takes the focus, so the
  // first is a session a bell can actually mark.
  await page.evaluate(() => window.__hive.addSession?.('s2'));
  await page.waitForFunction(
    () => (window.__hive.state?.sessions.length ?? 0) >= 2,
  );

  await first.locator('.hv-project-card__header').hover();
  await first
    .locator('.hv-project-card__header [data-action="minimize"]')
    .click();
  const chip = page.locator(`#minimized-projects .hv-chip[data-pid="${pid}"]`);
  await expect(chip).toBeVisible();
  await expect(chip).not.toHaveAttribute('data-state', 'attention');

  await page.evaluate((id) => window.__hive.ringBell?.(id), sid as string);
  await expect(chip).toHaveAttribute('data-state', 'attention');
});

// jsdom applies no CSS, so it will happily "click" a control the theme
// has display:none'd — exactly the defect the row's worktree button had.
// Both chip controls are hit-tested at their own centre and tabbed to.
test('both chip controls are clickable and keyboard-reachable', async ({
  page,
}) => {
  await boot(page);
  const first = page.locator('#projects > li.hv-project-card').first();
  const pid = await first.getAttribute('data-pid');
  await first.locator('.hv-project-card__header').hover();
  await first
    .locator('.hv-project-card__header [data-action="minimize"]')
    .click();

  const chip = page.locator(`#minimized-projects .hv-chip[data-pid="${pid}"]`);
  await expect(chip).toBeVisible();

  for (const sel of ['.hv-chip__open', '.hv-chip__restore']) {
    const box = await chip.locator(sel).boundingBox();
    if (!box) throw new Error(`${sel} has no box`);
    const hit = await page.evaluate(
      ({ x, y, sel }) => {
        const el = document.elementFromPoint(x, y);
        return !!el?.closest(sel);
      },
      { x: box.x + box.width / 2, y: box.y + box.height / 2, sel },
    );
    expect(hit, `${sel} is not the hit target at its own centre`).toBe(true);

    // Focusable for real: a display:none or visibility:hidden control
    // silently refuses focus, and .focus() would still "succeed".
    const focused = await chip.locator(sel).evaluate((el) => {
      (el as HTMLElement).focus();
      return document.activeElement === el;
    });
    expect(focused, `${sel} cannot take keyboard focus`).toBe(true);
  }
});

// The tray is a full-width vertical list, not a wrapped row of pills: the
// chip spans the tray, the restore + hugs the right edge, and clicking the
// dead space next to a short name restores instead of doing nothing.
test('a project chip spans the tray, right-aligns +, and restores on any click', async ({
  page,
}) => {
  await boot(page);
  const listed = page.locator('#projects > li.hv-project-card');
  const before = await listed.count();
  const first = listed.first();
  const pid = await first.getAttribute('data-pid');
  await first.locator('.hv-project-card__header').hover();
  await first
    .locator('.hv-project-card__header [data-action="minimize"]')
    .click();

  const chip = page.locator(`#minimized-projects .hv-chip[data-pid="${pid}"]`);
  await expect(chip).toBeVisible();

  // Measure against the tray's own content box rather than a fixed
  // tolerance: clientWidth excludes the scrollbar the tray grows once
  // enough projects are minimized, and reading the padding off the
  // computed style keeps the assertion honest if the tray is restyled.
  const trayInner = await page.locator('#minimized-projects').evaluate((el) => {
    const cs = getComputedStyle(el);
    return (
      el.clientWidth -
      Number.parseFloat(cs.paddingLeft) -
      Number.parseFloat(cs.paddingRight)
    );
  });
  const chipBox = (await chip.boundingBox())!;
  const restoreBox = (await chip.locator('.hv-chip__restore').boundingBox())!;

  // No 240px cap: the chip fills the tray's content box.
  expect(chipBox.width).toBeCloseTo(trayInner, 0);
  // + sits at the right edge, not next to the label.
  expect(restoreBox.x + restoreBox.width).toBeGreaterThan(
    chipBox.x + chipBox.width - 12,
  );

  // Click the slack between the label and the +. Assert first that the
  // point really is dead space — with a long enough project name this
  // would land on the label and pass without testing anything.
  const slackX = restoreBox.x - 12;
  const slackY = chipBox.y + chipBox.height / 2;
  const onLabel = await page.evaluate(
    ({ x, y }) => !!document.elementFromPoint(x, y)?.closest('.hv-chip__label'),
    { x: slackX, y: slackY },
  );
  expect(onLabel, 'the click point is on the label, not the slack').toBe(false);
  await page.mouse.click(slackX, slackY);
  await expect(listed).toHaveCount(before);
});

// Repro for #340: a minimized project must still be minimized after the
// window reloads. Persistence lives in a per-daemon localStorage key and is
// pruned against the project:list snapshot on reconnect, so only a real
// reload exercises the write → read → prune round trip.
test('a minimized project stays minimized across a reload', async ({
  page,
}) => {
  await boot(page);
  const listed = page.locator('#projects > li.hv-project-card');
  const before = await listed.count();
  const first = listed.first();
  const pid = await first.getAttribute('data-pid');

  await first.locator('.hv-project-card__header').hover();
  await first
    .locator('.hv-project-card__header [data-action="minimize"]')
    .click();
  await expect(listed).toHaveCount(before - 1);

  expect(
    await page.evaluate((key) => localStorage.getItem(key), MIN_KEY),
  ).toContain(pid);

  await page.reload();

  // The tray chip is the boot signal here: the mock seeds one project,
  // so a correct restore leaves #projects empty.
  await expect(
    page.locator(`#minimized-projects .hv-chip[data-pid="${pid}"]`),
  ).toBeVisible();
  await expect(listed).toHaveCount(before - 1);
});

// Losing the daemon id must cost persistence, not the app. StateDirID
// throws when the Wails bridge is unavailable — and so does LogFrontend,
// so an unwrapped log call inside that catch would reject the bootstrap
// IIFE and ConnectControl would never run.
test('boot still connects when StateDirID fails', async ({ page }) => {
  await page.goto('/?failStateDirID=1');
  // Project cards only exist once the daemon snapshot arrived, so this
  // rendering IS the proof ConnectControl ran. (The status bar has moved
  // past "connected" to the project name by then.)
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.hv-project-card').length > 0,
  );

  // Persistence is off, not falling back to the shared un-suffixed key —
  // writing that key is what let one daemon's GUI clobber another's.
  const listed = page.locator('#projects > li.hv-project-card');
  await listed.first().locator('.hv-project-card__header').hover();
  await listed
    .first()
    .locator('.hv-project-card__header [data-action="minimize"]')
    .click();
  await expect(page.locator('#minimized-projects .hv-chip')).toHaveCount(1);
  expect(
    await page.evaluate(() => localStorage.getItem('hive.minimizedProjects')),
  ).toBeNull();
});

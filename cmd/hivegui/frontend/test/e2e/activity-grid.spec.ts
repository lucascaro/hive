import { test, expect, type Page } from '@playwright/test';

// Spec 416 phase 4: the activity grid. Every tile's terminal body swaps
// for the activity renderer and back. The claims are paint and input:
// the tile really shows activity (not an overlay over a live terminal),
// the terminal keeps its size underneath, and no keystroke reaches a
// PTY nobody can see — including after the focus drives that ⌘→, a
// re-layout or a new session trigger.

const MAC = process.platform === 'darwin';
const TOGGLE = MAC ? 'Meta+j' : 'Control+Shift+J';
const TO_GRID = MAC ? 'Meta+Shift+J' : 'Control+Alt+Shift+J';
const MOD = MAC ? 'Meta' : 'Control';

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
  await page.waitForFunction(
    () => document.activeElement?.classList?.contains('xterm-helper-textarea'),
    null,
    { timeout: 3000 },
  );
  await page.evaluate(() => {
    for (const s of window.__hive.state?.sessions ?? []) {
      window.__hive.setSessionState?.(s.id, 'working', 'hook');
      window.__hive.setActivity?.(s.id, {
        stale_at: new Date(Date.now() + 600_000).toISOString(),
        plan: [
          { id: 'a', text: 'one', status: 'done', tools: 1 },
          { id: 'b', text: 'two', status: 'active', tools: 1 },
        ],
        events: [
          {
            tool: 'Read',
            target: `${s.name}.md`,
            call_id: `${s.id}-r`,
            plan_idx: 1,
            started_at: new Date(Date.now() - 2000).toISOString(),
            ended_at: new Date(Date.now() - 1000).toISOString(),
            duration_ms: 1000,
            ok: true,
          },
        ],
      });
    }
    window.__hive.resetStdin();
  });
}

// Per in-grid tile: what paints at the body's centre, the body's
// computed visibility and width.
const tiles = (page: Page) =>
  page.evaluate(() =>
    Array.from(document.querySelectorAll('#terms .term-host.in-grid')).map(
      (host) => {
        const body = host.querySelector('.term-body') as HTMLElement;
        const r = body.getBoundingClientRect();
        const at = document.elementFromPoint(
          r.left + r.width / 2,
          r.top + r.height / 2,
        );
        return {
          activity: !!at?.closest('.activity-tile'),
          xterm: !!at?.closest('.xterm'),
          visibility: getComputedStyle(body).visibility,
          width: body.clientWidth,
          pips: host.querySelectorAll('.hv-activity__pip').length,
        };
      },
    ),
  );

async function expectNoTyping(page: Page, label: string) {
  await page.evaluate(() => window.__hive.resetStdin());
  await page.keyboard.type('zz');
  await page.waitForTimeout(300);
  expect(await page.evaluate(() => window.__hive.stdinText()), label).toBe('');
}

test.describe('spec 416 activity grid', () => {
  test('toggles every tile to activity and back, terminals untouched', async ({
    page,
  }) => {
    await boot(page);
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    const before = await tiles(page);
    expect(before.every((t) => t.xterm)).toBe(true);

    await page.keyboard.press(TOGGLE);
    await expect(page.locator('#terms')).toHaveClass(/activity/);
    await expect
      .poll(async () => (await tiles(page)).every((t) => t.activity))
      .toBe(true);
    // The pips come from the snapshot GetActivity fetches, which can land
    // after the tile first paints — poll rather than sample once.
    await expect
      .poll(async () => (await tiles(page)).every((t) => t.pips === 2))
      .toBe(true);
    const on = await tiles(page);
    for (const [i, t] of on.entries()) {
      expect(t.visibility).toBe('hidden');
      // Hidden, not display:none: the terminal keeps its size.
      expect(t.width).toBe(before[i].width);
    }
    await expectNoTyping(page, 'after ⌘J into the activity grid');

    await page.keyboard.press(TOGGLE);
    await expect(page.locator('#terms')).not.toHaveClass(/activity/);
    await expect
      .poll(async () => (await tiles(page)).every((t) => t.xterm))
      .toBe(true);
    for (const t of await tiles(page)) expect(t.visibility).toBe('visible');
    // The active terminal takes the keyboard again.
    await page.waitForFunction(() =>
      document.activeElement?.classList?.contains('xterm-helper-textarea'),
    );
    await page.evaluate(() => window.__hive.resetStdin());
    await page.keyboard.type('ok');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()))
      .toContain('ok');
  });

  test('⌘⇧J reaches the activity grid from single view, and typing goes nowhere', async ({
    page,
  }) => {
    await boot(page);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    await page.keyboard.press(TO_GRID);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await expect(page.locator('#terms')).toHaveClass(/activity/);
    await expect
      .poll(async () => (await tiles(page)).every((t) => t.activity))
      .toBe(true);
    await expectNoTyping(page, 'right after ⌘⇧J from single');
  });

  test('grid keys still work, and never hand focus to a hidden terminal', async ({
    page,
  }) => {
    await boot(page);
    await page.keyboard.press(TO_GRID);
    await expect(page.locator('#terms')).toHaveClass(/activity/);

    const activeBefore = await page.evaluate(
      () => window.__hive_state?.activeId,
    );
    await page.keyboard.press(`${MOD}+ArrowRight`);
    await expect
      .poll(() => page.evaluate(() => window.__hive_state?.activeId))
      .not.toBe(activeBefore);
    await expectNoTyping(page, 'after ⌘→');

    await page.evaluate(() => window.__hive.addSession?.('late'));
    await expect(page.locator('#terms .term-host.in-grid')).toHaveCount(4);
    await expect
      .poll(async () => (await tiles(page)).every((t) => t.activity))
      .toBe(true);
    await expectNoTyping(page, 'after a new session');

    // ⌘Enter opens the active session with its terminal live.
    await page.keyboard.press(`${MOD}+Enter`);
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
    await page.waitForFunction(() =>
      document.activeElement?.classList?.contains('xterm-helper-textarea'),
    );
    await page.evaluate(() => window.__hive.resetStdin());
    await page.keyboard.type('yo');
    await expect
      .poll(() => page.evaluate(() => window.__hive.stdinText()))
      .toContain('yo');
  });

  test('a delta appears in its tile feed', async ({ page }) => {
    await boot(page);
    await page.keyboard.press(TO_GRID);
    await expect(page.locator('#terms')).toHaveClass(/activity/);
    const id = await page.evaluate(
      () => window.__hive.state?.sessions[0]?.id ?? '',
    );
    await page.evaluate((sid) => {
      window.__hive.emitActivity?.({
        session_id: sid,
        events: [
          {
            tool: 'Bash',
            target: 'go test ./...',
            call_id: 'live',
            plan_idx: 1,
            started_at: new Date().toISOString(),
          },
        ],
        stale_at: new Date(Date.now() + 600_000).toISOString(),
      });
    }, id);
    await expect(
      page
        .locator(
          `.term-host[data-sid="${id}"] .activity-tile .hv-activity__call`,
        )
        .first(),
    ).toContainText('go test ./...');
  });

  test('with one session, ⌘⇧J opens the panel instead', async ({ page }) => {
    await boot(page, 1);
    await page.keyboard.press(TO_GRID);
    await expect(page.locator('#activity-panel')).toBeVisible();
    await expect(page.locator('#terms')).not.toHaveClass(/grid/);
  });
});

// Spec 428: the tile's body is the plan's tasks, and the tool feed takes
// whatever height they leave — down to none. Every claim here is about
// computed layout, which vitest cannot answer.
test.describe('spec 428 tasks-first tile', () => {
  // Seeds `total` steps into the first session, with `activeAt` current.
  async function longPlan(page: Page, total = 40, activeAt = 35) {
    const id = await page.evaluate(
      () => window.__hive.state?.sessions[0]?.id ?? '',
    );
    // A delta, not setActivity (which only seeds the snapshot the tile
    // fetches once at mount) and not a full frame (the store drops
    // snapshots nobody requested — store/activity.ts). A plan update on
    // the wire carries the whole plan, which is exactly this.
    await page.evaluate(
      ({ sid, n, at }) => {
        window.__hive.emitActivity?.({
          session_id: sid,
          stale_at: new Date(Date.now() + 600_000).toISOString(),
          plan: Array.from({ length: n }, (_, i) => ({
            id: `p${i}`,
            text: `step number ${i}`,
            status: i < at ? 'done' : i === at ? 'active' : 'pending',
            tools: 1,
          })),
          events: [
            {
              tool: 'Bash',
              target: 'go test ./...',
              call_id: 'long',
              plan_idx: at,
              started_at: new Date(Date.now() - 2000).toISOString(),
              ended_at: new Date(Date.now() - 1000).toISOString(),
              duration_ms: 1000,
              ok: true,
            },
          ],
        });
      },
      { sid: id, n: total, at: activeAt },
    );
    return id;
  }

  // Heights and offsets of one tile's parts, all in one round trip.
  const metrics = (page: Page, sid: string) =>
    page.evaluate((id) => {
      const host = document.querySelector(
        `.term-host[data-sid="${id}"]`,
      ) as HTMLElement;
      const tile = host.querySelector('.activity-tile') as HTMLElement;
      const plan = tile.querySelector('.hv-activity__plan') as HTMLElement;
      const feed = tile.querySelector('.hv-activity__timeline') as HTMLElement;
      const head = tile.querySelector('.hv-activity__head') as HTMLElement;
      const active = tile.querySelector(
        '.hv-activity__step[data-status="active"]',
      ) as HTMLElement;
      const doneRow = tile.querySelector(
        '.hv-activity__step[data-status="done"] .hv-activity__step-row',
      ) as HTMLElement;
      const grid = document.getElementById('terms') as HTMLElement;
      const r = (el: HTMLElement | null) =>
        el ? el.getBoundingClientRect() : null;
      return {
        feedHeight: feed?.clientHeight ?? -1,
        calls: tile.querySelectorAll('.hv-activity__call').length,
        headHeight: head?.clientHeight ?? 0,
        planScrolls: plan ? plan.scrollHeight > plan.clientHeight : false,
        planRect: r(plan),
        activeRect: r(active),
        activeColor: active
          ? getComputedStyle(
              active.querySelector('.hv-activity__step-row') as HTMLElement,
            ).color
          : '',
        doneColor: doneRow ? getComputedStyle(doneRow).color : '',
        // Nothing may have scrolled the grid or the tile's own host: both
        // are overflow:hidden with no scrollbar to undo an offset.
        outer: [
          grid.scrollTop,
          grid.scrollLeft,
          host.scrollTop,
          host.scrollLeft,
        ],
      };
    }, sid);

  test('the tile shows the plan task text, not just pips', async ({ page }) => {
    await boot(page);
    await page.keyboard.press(TO_GRID);
    await expect(page.locator('#terms')).toHaveClass(/activity/);
    const texts = page.locator('.activity-tile .hv-activity__step-text');
    await expect(texts.first()).toHaveText('one');
    await expect(
      page
        .locator('.activity-tile .hv-activity__step[data-status="active"]')
        .first()
        .locator('.hv-activity__step-text'),
    ).toHaveText('two');
    // Every tile, not just the active one.
    expect(await texts.count()).toBe(6);
  });

  test('a short plan still leaves the feed room', async ({ page }) => {
    await boot(page);
    await page.keyboard.press(TO_GRID);
    await expect(page.locator('#terms')).toHaveClass(/activity/);
    const id = await page.evaluate(
      () => window.__hive.state?.sessions[0]?.id ?? '',
    );
    const m = await metrics(page, id);
    expect(m.feedHeight).toBeGreaterThan(0);
    expect(m.calls).toBeGreaterThan(0);
  });

  test('a plan taller than the tile leaves the feed no height', async ({
    page,
  }) => {
    await boot(page);
    await page.keyboard.press(TO_GRID);
    await expect(page.locator('#terms')).toHaveClass(/activity/);
    const id = await longPlan(page);
    await expect.poll(async () => (await metrics(page, id)).feedHeight).toBe(0);
    const m = await metrics(page, id);
    // The rows stay in the DOM behind `overflow: hidden`; zero height is
    // the claim, and it is what the poll above just proved.
    expect(m.planScrolls).toBe(true);
    // The wrapping pip strip stays two rows; the head does not shrink, so
    // an uncapped one would eat the list it summarizes.
    expect(m.headHeight).toBeLessThanOrEqual(24);
    // Still the activity tile that paints, not the terminal underneath.
    const [t] = await page.evaluate((sid) => {
      const body = document.querySelector(
        `.term-host[data-sid="${sid}"] .term-body`,
      ) as HTMLElement;
      const r = body.getBoundingClientRect();
      const at = document.elementFromPoint(
        r.left + r.width / 2,
        r.top + r.height / 2,
      );
      return [!!at?.closest('.activity-tile')];
    }, id);
    expect(t).toBe(true);
  });

  test('the current step is scrolled fully into view, and nothing else moves', async ({
    page,
  }) => {
    await boot(page);
    await page.keyboard.press(TO_GRID);
    await expect(page.locator('#terms')).toHaveClass(/activity/);
    const id = await longPlan(page);
    await expect
      .poll(async () => {
        const m = await metrics(page, id);
        if (!m.activeRect || !m.planRect) return false;
        return (
          m.activeRect.top >= m.planRect.top - 0.5 &&
          m.activeRect.bottom <= m.planRect.bottom + 0.5
        );
      })
      .toBe(true);
    expect((await metrics(page, id)).outer).toEqual([0, 0, 0, 0]);
  });

  test('the current step reads as current', async ({ page }) => {
    await boot(page);
    await page.keyboard.press(TO_GRID);
    await expect(page.locator('#terms')).toHaveClass(/activity/);
    const id = await longPlan(page);
    const m = await metrics(page, id);
    expect(m.activeColor).not.toBe('');
    expect(m.activeColor).not.toBe(m.doneColor);
  });

  test('wheel-scrolling the task list steals no focus and no keystroke', async ({
    page,
  }) => {
    await boot(page);
    await page.keyboard.press(TO_GRID);
    await expect(page.locator('#terms')).toHaveClass(/activity/);
    const id = await longPlan(page);
    const plan = page.locator(
      `.term-host[data-sid="${id}"] .activity-tile .hv-activity__plan`,
    );
    await plan.hover();
    await page.mouse.wheel(0, 120);
    await page.waitForTimeout(100);
    expect(
      await page.evaluate(() =>
        document.activeElement?.closest('.activity-tile')
          ? 'inside'
          : 'outside',
      ),
    ).toBe('outside');
    expect((await metrics(page, id)).outer).toEqual([0, 0, 0, 0]);
    await expectNoTyping(page, 'after wheeling the task list');
  });

  test('a session with no plan gives the whole tile to the feed', async ({
    page,
  }) => {
    await boot(page);
    await page.keyboard.press(TO_GRID);
    await expect(page.locator('#terms')).toHaveClass(/activity/);
    const id = await page.evaluate(
      () => window.__hive.state?.sessions[0]?.id ?? '',
    );
    await page.evaluate((sid) => {
      window.__hive.emitActivity?.({
        session_id: sid,
        stale_at: new Date(Date.now() + 600_000).toISOString(),
        plan: [],
        events: [
          {
            tool: 'Read',
            target: 'only.md',
            call_id: 'solo',
            plan_idx: -1,
            started_at: new Date(Date.now() - 2000).toISOString(),
            ended_at: new Date(Date.now() - 1000).toISOString(),
            duration_ms: 1000,
            ok: true,
          },
        ],
      });
    }, id);
    await expect(
      page.locator(
        `.term-host[data-sid="${id}"] .activity-tile .hv-activity__plan`,
      ),
    ).toHaveCount(0);
    const m = await metrics(page, id);
    expect(m.feedHeight).toBeGreaterThan(0);
    // A plan delta does not clear the events boot() seeded; it merges.
    expect(m.calls).toBeGreaterThan(0);
  });
});

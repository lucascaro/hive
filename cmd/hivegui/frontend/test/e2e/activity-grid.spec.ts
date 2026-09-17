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
    const on = await tiles(page);
    for (const [i, t] of on.entries()) {
      expect(t.visibility).toBe('hidden');
      // Hidden, not display:none: the terminal keeps its size.
      expect(t.width).toBe(before[i].width);
      expect(t.pips).toBe(2);
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

import { test, expect, type Page } from '@playwright/test';

// Spec 416 phase 4: the inspector panel beside the terminal. The claims
// here are layout and focus — the panel takes a real column, the
// terminal refits into what is left, and nothing about the panel ever
// takes the keyboard. jsdom computes neither, so they are measured here.

const MAC = process.platform === 'darwin';
// The app picks its chords from the browser's platform, which Playwright's
// Chromium reports as the host's.
const TOGGLE = MAC ? 'Meta+j' : 'Control+Shift+J';
const MOD = MAC ? 'Meta' : 'Control';
// focus.ts arms a 500ms guard that re-asserts terminal focus after any
// focus drive. A click checked inside it would pass even if the panel
// stole focus, because the guard would put it back.
const PAST_FOCUS_GUARD_MS = 700;

async function boot(page: Page, staleInMs = 600_000) {
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
    ([sid, staleIn]) => {
      window.__hive.setSessionState?.(sid as string, 'working', 'hook');
      window.__hive.setActivity?.(sid as string, {
        stale_at: new Date(Date.now() + (staleIn as number)).toISOString(),
        plan: [
          { id: 'a', text: 'read the spec', status: 'done', tools: 3 },
          { id: 'b', text: 'write the panel', status: 'active', tools: 1 },
          { id: 'c', text: 'ship it', status: 'pending' },
        ],
        events: Array.from({ length: 60 }, (_, i) => ({
          tool: i % 2 ? 'Read' : 'Edit',
          target: `file-${i}.ts`,
          call_id: `c${i}`,
          plan_idx: i < 50 ? 0 : 1,
          started_at: new Date(Date.now() - 60_000 + i).toISOString(),
          ended_at: new Date(Date.now() - 59_000 + i).toISOString(),
          duration_ms: 1000,
          ok: true,
        })),
      });
    },
    [id, staleInMs] as const,
  );
  return id;
}

const isTermFocused = (page: Page) =>
  page.evaluate(() =>
    document.activeElement?.classList?.contains('xterm-helper-textarea'),
  );

const termBox = (page: Page) =>
  page.locator('#terms .term-host.visible').evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { left: r.left, right: r.right, width: r.width };
  });

const termCols = (page: Page) =>
  page.evaluate(
    () =>
      (
        Array.from(window.__hive_state?.terms.values() ?? []).find((t) =>
          t.host.classList.contains('visible'),
        )?.term as unknown as { cols: number } | undefined
      )?.cols ?? 0,
  );

// The column count once layout has settled: two reads 300ms apart that
// agree. Read straight after boot it can still be moving (font load,
// sidebar width); on Windows CI a pre-settle 145 became the baseline the
// steady 127 was compared against.
async function settledCols(page: Page): Promise<number> {
  let prev = -1;
  for (let i = 0; i < 20; i++) {
    const cur = await termCols(page);
    if (cur > 0 && cur === prev) return cur;
    prev = cur;
    await page.waitForTimeout(300);
  }
  throw new Error(`terminal cols never settled (last ${prev})`);
}

test.describe('spec 416 inspector panel', () => {
  test('opens beside the terminal, refits it, and closes again', async ({
    page,
  }) => {
    await boot(page);
    const colsBefore = await settledCols(page);
    const before = await termBox(page);
    await expect(page.locator('#activity-panel')).toBeHidden();
    // Diagnostic (Windows CI ends at fewer cols than it started with after
    // the panel closes): record every body resize, what fit proposed and
    // whether it threw, printed only if the final assertion fails.
    await page.evaluate(() => {
      type Fit = {
        fit(): void;
        proposeDimensions(): { cols: number; rows: number } | undefined;
      };
      const t = Array.from(window.__hive_state?.terms.values() ?? []).find(
        (x) => x.host.classList.contains('visible'),
      ) as unknown as { body: HTMLElement; fit: Fit; term: { cols: number } };
      const log: unknown[] = [];
      (window as unknown as { __refitLog: unknown[] }).__refitLog = log;
      const t0 = performance.now();
      const at = () => Math.round(performance.now() - t0);
      new ResizeObserver(() =>
        log.push({ at: at(), ro: t.body.clientWidth, cols: t.term.cols }),
      ).observe(t.body);
      const orig = t.fit.fit.bind(t.fit);
      t.fit.fit = () => {
        let proposed: unknown;
        try {
          proposed = t.fit.proposeDimensions();
        } catch (e) {
          proposed = `propose threw: ${e}`;
        }
        try {
          orig();
          log.push({
            at: at(),
            fit: t.body.clientWidth,
            proposed,
            cols: t.term.cols,
          });
        } catch (e) {
          log.push({
            at: at(),
            fit: t.body.clientWidth,
            proposed,
            threw: String(e),
          });
          throw e;
        }
      };
    });

    await page.keyboard.press(TOGGLE);
    const panel = page.locator('#activity-panel');
    await expect(panel).toBeVisible();
    await expect(panel.locator('.hv-activity__step')).toHaveCount(3);

    // A real column: the point at its centre is the panel, not a
    // terminal painted underneath.
    const hit = await panel.evaluate((el) => {
      const r = el.getBoundingClientRect();
      const at = document.elementFromPoint(
        r.left + r.width / 2,
        r.top + r.height / 2,
      );
      return !!at && el.contains(at);
    });
    expect(hit).toBe(true);

    await expect
      .poll(async () => (await termBox(page)).right)
      .toBeLessThan(before.right - 100);
    await expect.poll(() => termCols(page)).toBeLessThan(colsBefore);
    expect(await isTermFocused(page)).toBe(true);

    await page.keyboard.press(TOGGLE);
    await expect(panel).toBeHidden();
    await expect
      .poll(async () => (await termBox(page)).right)
      .toBe(before.right);
    try {
      await expect.poll(() => termCols(page)).toBe(colsBefore);
    } catch (err) {
      const trace = await page.evaluate(() => {
        const t = Array.from(window.__hive_state?.terms.values() ?? []).find(
          (x) => x.host.classList.contains('visible'),
        ) as unknown as {
          body: HTMLElement;
          fit: { proposeDimensions(): unknown };
          term: { cols: number; element?: HTMLElement };
        };
        return {
          log: (window as unknown as { __refitLog: unknown[] }).__refitLog,
          final: {
            body: t.body.clientWidth,
            xterm: t.term.element?.clientWidth,
            proposed: t.fit.proposeDimensions(),
            cols: t.term.cols,
          },
        };
      });
      console.log(`REFIT-TRACE ${JSON.stringify(trace)}`);
      throw err;
    }
  });

  test('clicking and scrolling the panel never takes the keyboard', async ({
    page,
  }) => {
    const id = await boot(page);
    await page.keyboard.press(TOGGLE);
    const panel = page.locator('#activity-panel');
    await expect(panel.locator('.hv-activity__step')).toHaveCount(3);
    await page.waitForTimeout(PAST_FOCUS_GUARD_MS);

    await panel.locator('.hv-activity__step-row').first().click();
    await expect(panel.locator('.hv-activity__step').first()).toHaveAttribute(
      'data-open',
      '',
    );
    await page.waitForTimeout(PAST_FOCUS_GUARD_MS);
    expect(await isTermFocused(page)).toBe(true);

    const timeline = panel.locator('.hv-activity__timeline');
    await timeline.hover();
    await page.mouse.wheel(0, 400);
    await page.mouse.down();
    await page.mouse.up();
    await page.waitForTimeout(PAST_FOCUS_GUARD_MS);
    expect(await isTermFocused(page)).toBe(true);

    await page.evaluate(() => window.__hive.resetStdin());
    await page.keyboard.type('hi');
    await expect
      .poll(() => page.evaluate((sid) => window.__hive.stdinText(sid), id))
      .toContain('hi');
  });

  test('only the active step starts open, with the item tally on collapsed steps', async ({
    page,
  }) => {
    await boot(page);
    await page.keyboard.press(TOGGLE);
    const steps = page.locator('#activity-panel .hv-activity__step');
    await expect(steps).toHaveCount(3);
    await expect(steps.nth(0)).not.toHaveAttribute('data-open', '');
    await expect(steps.nth(1)).toHaveAttribute('data-open', '');
    await expect(steps.nth(0).locator('.hv-activity__pill')).toHaveText('3');
  });

  test('a delta shows up live', async ({ page }) => {
    const id = await boot(page);
    await page.keyboard.press(TOGGLE);
    await expect(
      page.locator('#activity-panel .hv-activity__step'),
    ).toHaveCount(3);
    await page.evaluate((sid) => {
      window.__hive.emitActivity?.({
        session_id: sid,
        events: [
          {
            tool: 'Bash',
            target: 'npm test',
            call_id: 'live-1',
            plan_idx: 1,
            started_at: new Date().toISOString(),
          },
        ],
        stale_at: new Date(Date.now() + 600_000).toISOString(),
      });
    }, id);
    await expect(
      page
        .locator('#activity-panel .hv-activity__timeline .hv-activity__call')
        .first(),
    ).toContainText('npm test');
  });

  test('is hidden in grid view', async ({ page }) => {
    await boot(page);
    await page.evaluate(() => window.__hive.addSession?.('second'));
    await page.keyboard.press(TOGGLE);
    await expect(page.locator('#activity-panel')).toBeVisible();
    await page.keyboard.press(`${MOD}+g`);
    await expect(page.locator('#terms')).toHaveClass(/grid/);
    await expect(page.locator('#activity-panel')).toBeHidden();
  });

  test('keeps its column with the sidebar hidden', async ({ page }) => {
    await boot(page);
    await page.keyboard.press(TOGGLE);
    await page.keyboard.press(`${MOD}+s`);
    await expect(page.locator('#app')).toHaveClass(/sidebar-hidden/);
    const panel = page.locator('#activity-panel');
    await expect(panel).toBeVisible();
    // The sidebar's track is really gone, not left as an empty column.
    // Polled: #app animates grid-template-columns.
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            document
              .querySelector('#terms .term-host.visible')
              ?.getBoundingClientRect().left ?? 999,
        ),
      )
      .toBeLessThan(20);
    const geo = await page.evaluate(() => {
      const p = document
        .getElementById('activity-panel')
        ?.getBoundingClientRect();
      const t = document
        .querySelector('#terms .term-host.visible')
        ?.getBoundingClientRect();
      if (!p || !t) return null;
      const at = document.elementFromPoint(
        p.left + p.width / 2,
        p.top + p.height / 2,
      );
      return {
        panelHit:
          !!at && !!document.getElementById('activity-panel')?.contains(at),
        panelWidth: p.width,
        termRight: t.right,
        panelLeft: p.left,
      };
    });
    expect(geo?.panelHit).toBe(true);
    expect(geo?.panelWidth).toBeGreaterThan(200);
    expect(geo?.termRight ?? 0).toBeLessThanOrEqual(geo?.panelLeft ?? 0);
  });

  test('fresh while working before stale_at', async ({ page }) => {
    await boot(page);
    await page.keyboard.press(TOGGLE);
    await expect(
      page.locator('#activity-panel .hv-activity__step'),
    ).toHaveCount(3);
    await expect(
      page.locator('#activity-panel .hv-activity'),
    ).not.toHaveAttribute('data-stale', '');
  });

  test('stale while working past stale_at; not once at rest', async ({
    page,
  }) => {
    const id = await boot(page, -120_000);
    await page.keyboard.press(TOGGLE);
    const root = page.locator('#activity-panel .hv-activity');
    await expect(root).toHaveAttribute('data-stale', '');
    await expect(
      page.locator('#activity-panel .hv-activity__stale'),
    ).toContainText('Stale for 2m');
    // The desaturation is real CSS, not just an attribute.
    const colors = await page.evaluate(() => {
      const tool = document.querySelector('#activity-panel .hv-activity__tool');
      const probe = document.createElement('span');
      probe.style.color = 'var(--fg-subtle)';
      document.body.appendChild(probe);
      const subtle = getComputedStyle(probe).color;
      probe.remove();
      return { tool: tool ? getComputedStyle(tool).color : '', subtle };
    });
    expect(colors.tool).toBe(colors.subtle);

    await page.evaluate(
      (sid) => window.__hive.setSessionState?.(sid, 'waiting_input', 'hook'),
      id,
    );
    await expect(root).not.toHaveAttribute('data-stale', '');
  });

  test('a shell session shows the no-activity empty state', async ({
    page,
  }) => {
    const id = await boot(page);
    await page.evaluate((sid) => {
      window.__hive.setSessionState?.(sid, 'idle', '');
      window.__hive.setActivity?.(sid, { plan: [], events: [] });
    }, id);
    await page.keyboard.press(TOGGLE);
    await expect(
      page.locator('#activity-panel .hv-activity__empty'),
    ).toContainText('No activity data');
  });
});

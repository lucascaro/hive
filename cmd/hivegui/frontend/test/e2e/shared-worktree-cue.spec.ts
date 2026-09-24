import { expect, type Page, test } from '@playwright/test';

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

// The shared-worktree cue, in a real browser (spec 384, restyled in 455).
//
// jsdom is CSS-blind: the dom tests can only assert that `data-wt-shared`
// and the count are in the markup. Whether the cue is actually PAINTED —
// the group's rail in the session colour, and each member's own colour
// bar at its right edge, not clipped and not covered by the resizer — is
// a question only a real engine answers, and getting it wrong is
// invisible to every other layer of this suite.

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

// Two sessions in one worktree: one creates it, the second resumes into it.
async function seedSharedPair(page: Page) {
  await page.evaluate(() =>
    window.__hive.createSessionWithWorktree?.('alpha', 'feat/x'),
  );
  await page.waitForFunction(() =>
    (window.__hive.state?.sessions ?? []).some((s) => !!s.worktree_path),
  );
  const path = await page.evaluate(
    () =>
      (window.__hive.state?.sessions ?? []).find((s) => !!s.worktree_path)
        ?.worktree_path ?? '',
  );
  await page.evaluate(
    (p) => window.__hive.createSessionInWorktree?.('beta', p),
    path,
  );
  await page.waitForFunction(
    (p) =>
      (window.__hive.state?.sessions ?? []).filter((s) => s.worktree_path === p)
        .length === 2,
    path,
  );
  await page.waitForFunction(
    () =>
      document.querySelectorAll('li.hv-session-row[data-wt-shared]').length ===
      2,
  );
}

// A member's own colour bar (its right-edge swatch, also its colour
// picker) and the group's rail, plus whether a hit test at the row's
// right edge still lands inside the group — i.e. not on the resizer. Each
// row's bar is read from the ROW: the group has no bar of its own since
// spec 455, and reading a removed pseudo-element would compare two
// transparent values and pass whatever the colours were.
function barOf(page: Page, nth: number) {
  return page.evaluate((n) => {
    const rows = document.querySelectorAll<HTMLElement>(
      'li.hv-session-row[data-wt-shared]',
    );
    const li = rows[n];
    const panel = li.closest<HTMLElement>('.hv-worktree-group');
    if (!panel) throw new Error('member is not inside a group panel');
    const bar = li.querySelector<HTMLElement>('.hv-session-row__colour');
    if (!bar) throw new Error('member has no colour bar');
    const rail = panel.querySelector<HTMLElement>('.hv-worktree-group__rows');
    if (!rail) throw new Error('group has no rail');
    const r = li.getBoundingClientRect();
    const hit = document.elementFromPoint(r.right - 1, r.top + r.height / 2);
    return {
      width: getComputedStyle(bar).width,
      background: getComputedStyle(bar).backgroundColor,
      railWidth: getComputedStyle(rail).borderLeftWidth,
      railColor: getComputedStyle(rail).borderLeftColor,
      sessionColor: getComputedStyle(panel)
        .getPropertyValue('--session-color')
        .trim(),
      hitInsidePanel: !!hit && (hit === panel || panel.contains(hit)),
      sid: li.dataset.sid ?? '',
    };
  }, nth);
}

test.describe('shared worktree cue', () => {
  test('paints the rail and each member bar in the session colour', async ({
    page,
  }) => {
    await boot(page);
    await seedSharedPair(page);
    await page.mouse.move(600, 5); // bars at their resting width

    const first = await barOf(page, 0);
    expect(first.width).toBe('3px');
    // Painted, not transparent, and not falling through to the fallback.
    expect(first.background).not.toBe('rgba(0, 0, 0, 0)');
    expect(first.sessionColor).not.toBe('');
    expect(first.railWidth).toBe('1px');
    expect(first.railColor).toBe(first.background);
    // The right edge is still inside the panel's own box — not clipped
    // away by an ancestor's overflow, and not under the resizer.
    expect(first.hitInsidePanel).toBe(true);
  });

  // The rule this replaced lived on the ROW, and a bad rebase once brought
  // it back: both bars then painted, and on an attention row the stale
  // rule and the pulse overlay fought over the same ::after. Assert the
  // row's own ::after is gone, not just that the panel's exists.
  test('paints no second bar on the member rows themselves', async ({
    page,
  }) => {
    await boot(page);
    await seedSharedPair(page);
    const rowBars = await page.evaluate(() =>
      Array.from(
        document.querySelectorAll<HTMLElement>(
          'li.hv-session-row[data-wt-shared]',
        ),
      ).map((li) => getComputedStyle(li, '::after').content),
    );
    expect(rowBars.length).toBeGreaterThan(0);
    for (const content of rowBars) expect(content).toBe('none');
  });

  test('gives both members of a group the same colour', async ({ page }) => {
    await boot(page);
    await seedSharedPair(page);
    await page.mouse.move(600, 5);
    const [a, b] = [await barOf(page, 0), await barOf(page, 1)];
    expect(a.sid).not.toBe(b.sid);
    expect(a.background).not.toBe('rgba(0, 0, 0, 0)');
    expect(a.background).toBe(b.background);
  });

  test('names the branch once, at the top of the panel', async ({ page }) => {
    await boot(page);
    await seedSharedPair(page);
    const header = page.locator('.hv-worktree-group__header').first();
    await expect(header.locator('.hv-worktree-group__branch')).toContainText(
      'feat/',
    );
    await expect(header.locator('.hv-worktree-group__count')).toHaveText('2');
  });

  test('shows the group size on the branch glyph', async ({ page }) => {
    await boot(page);
    await seedSharedPair(page);
    const counts = await page.evaluate(() =>
      Array.from(
        document.querySelectorAll<HTMLElement>(
          'li.hv-session-row[data-wt-shared] .hv-session-row__worktree-count',
        ),
      ).map((el) => el.textContent),
    );
    expect(counts).toEqual(['2', '2']);
  });

  test('leaves an unshared session with no bar', async ({ page }) => {
    await boot(page);
    await seedSharedPair(page);
    const solo = await page.evaluate(() => {
      const li = document.querySelector<HTMLElement>(
        'li.hv-session-row:not([data-wt-shared])',
      );
      if (!li) return null;
      return !!li.closest('.hv-worktree-group');
    });
    expect(solo).toBe(false);
  });

  test('paints the group as adjacent rows', async ({ page }) => {
    await boot(page);
    await seedSharedPair(page);
    const flags = await page.evaluate(() =>
      Array.from(
        document.querySelectorAll<HTMLElement>('li.hv-session-row'),
      ).map((li) => li.hasAttribute('data-wt-shared')),
    );
    const first = flags.indexOf(true);
    expect(flags[first + 1]).toBe(true);
  });

  // The bug this ordering exists to prevent, reported from the running app:
  // "cmd up/down iterate not in the order I see but the order the daemon
  // still holds". Clustering used to happen only where the sidebar painted,
  // so orderedSessions() — which drives ⌘↑/⌘↓, ⌘1-9, the tray and the
  // palette — still walked the daemon's flat r.order. Two orders is the bug;
  // this asserts there is one.
  test('keyboard navigation walks the order the rows are painted in', async ({
    page,
  }) => {
    await boot(page);
    // Seed so the two orders genuinely DISAGREE: alpha takes a worktree,
    // gamma is created next (so it sits between them in r.order), and beta
    // then joins alpha's worktree. r.order is alpha,gamma,beta; the rows
    // paint alpha,beta,gamma. A fixture where the two coincide would pass
    // against the very bug this test exists for.
    await page.evaluate(() =>
      window.__hive.createSessionWithWorktree?.('alpha', 'feat/x'),
    );
    await page.waitForFunction(() =>
      (window.__hive.state?.sessions ?? []).some((s) => !!s.worktree_path),
    );
    const wt = await page.evaluate(
      () =>
        (window.__hive.state?.sessions ?? []).find((s) => !!s.worktree_path)
          ?.worktree_path ?? '',
    );
    await page.evaluate(() => window.__hive.addSession?.('gamma'));
    await page.waitForFunction(() =>
      (window.__hive.state?.sessions ?? []).some((s) => s.name === 'gamma'),
    );
    await page.evaluate(
      (p) => window.__hive.createSessionInWorktree?.('beta', p),
      wt,
    );
    await page.waitForFunction(
      () =>
        document.querySelectorAll('li.hv-session-row[data-wt-shared]')
          .length === 2,
    );

    // Guard the fixture itself: if r.order ever matched the painted order,
    // this test would prove nothing.
    const daemonOrder = await page.evaluate(() =>
      [...(window.__hive.state?.sessions ?? [])]
        .sort((a, b) => (a.order ?? 0) - (b.order ?? 0))
        .map((s) => s.id),
    );

    const painted = await page.evaluate(() =>
      Array.from(
        document.querySelectorAll<HTMLElement>('li.hv-session-row'),
      ).map((li) => li.dataset.sid ?? ''),
    );
    expect(painted).not.toEqual(daemonOrder);

    // Walk with ⌘↓ from the top and record where selection lands.
    const visited: string[] = [];
    for (let i = 0; i < painted.length; i++) {
      const sid = await page.evaluate(
        () =>
          document.querySelector<HTMLElement>(
            'li.hv-session-row[data-selected]',
          )?.dataset.sid ?? '',
      );
      if (sid) visited.push(sid);
      await page.keyboard.press(`${MOD}+ArrowDown`);
    }

    // The walk is cyclic, so compare as a rotation of the painted order.
    const start = painted.indexOf(visited[0]);
    const expected = [
      ...painted.slice(start),
      ...painted.slice(0, start),
    ].slice(0, visited.length);
    expect(visited).toEqual(expected);
  });
});

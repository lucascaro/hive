import { expect, type Page, test } from '@playwright/test';

const MOD = process.platform === 'darwin' ? 'Meta' : 'Control';

// The shared-worktree cue, in a real browser (spec 384).
//
// jsdom is CSS-blind: the dom tests can only assert that `data-wt-shared`
// and the count are in the markup. Whether the bar is actually PAINTED —
// 3px of the session's colour, at the right edge, not clipped by the grid
// and not covered by the colour swatch that shares that end of the row —
// is a question only a real engine answers, and getting it wrong is
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

// The group panel's ::after bar — its geometry and fill, plus whether a
// hit test at the row's right edge still lands inside the panel. The bar
// belongs to the PANEL now, not to each row: members share a colour, so a
// bar per row would be three marks for one fact.
function barOf(page: Page, nth: number) {
  return page.evaluate((n) => {
    const rows = document.querySelectorAll<HTMLElement>(
      'li.hv-session-row[data-wt-shared]',
    );
    const li = rows[n];
    const panel = li.closest<HTMLElement>('.hv-worktree-group');
    if (!panel) throw new Error('member is not inside a group panel');
    const cs = getComputedStyle(panel, '::after');
    const r = li.getBoundingClientRect();
    const hit = document.elementFromPoint(r.right - 1, r.top + r.height / 2);
    return {
      width: cs.width,
      background: cs.backgroundColor,
      content: cs.content,
      sessionColor: getComputedStyle(panel)
        .getPropertyValue('--session-color')
        .trim(),
      hitInsidePanel: !!hit && (hit === panel || panel.contains(hit)),
      sid: li.dataset.sid ?? '',
    };
  }, nth);
}

test.describe('shared worktree cue', () => {
  test('paints a 3px bar in the session colour at the right edge', async ({
    page,
  }) => {
    await boot(page);
    await seedSharedPair(page);

    const first = await barOf(page, 0);
    expect(first.content).not.toBe('none'); // the ::after exists at all
    expect(first.width).toBe('3px');
    // Painted, not transparent, and not falling through to the fallback.
    expect(first.background).not.toBe('rgba(0, 0, 0, 0)');
    expect(first.sessionColor).not.toBe('');
    // The right edge is still inside the panel's own box — i.e. the bar
    // is not in an area clipped away by an ancestor's overflow.
    expect(first.hitInsidePanel).toBe(true);
  });

  test('gives both members of a group the same colour', async ({ page }) => {
    await boot(page);
    await seedSharedPair(page);
    const [a, b] = [await barOf(page, 0), await barOf(page, 1)];
    expect(a.sid).not.toBe(b.sid);
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

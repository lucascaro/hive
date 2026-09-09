import { expect, type Page, test } from '@playwright/test';

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

// The ::after bar's computed geometry and fill, plus whether a hit test at
// the row's right edge still lands inside the row.
function barOf(page: Page, nth: number) {
  return page.evaluate((n) => {
    const rows = document.querySelectorAll<HTMLElement>(
      'li.hv-session-row[data-wt-shared]',
    );
    const li = rows[n];
    const cs = getComputedStyle(li, '::after');
    const r = li.getBoundingClientRect();
    const hit = document.elementFromPoint(r.right - 1, r.top + r.height / 2);
    return {
      width: cs.width,
      background: cs.backgroundColor,
      content: cs.content,
      sessionColor: getComputedStyle(li)
        .getPropertyValue('--session-color')
        .trim(),
      hitInsideRow: !!hit && (hit === li || li.contains(hit)),
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
    // The row's right edge is still inside the row's own box — i.e. the
    // bar is not sitting in an area clipped away by an ancestor's
    // overflow. (It cannot be covered by the swatch that shares this end
    // of the row: ::after paints above in-flow children.)
    expect(first.hitInsideRow).toBe(true);
  });

  test('gives both members of a group the same colour', async ({ page }) => {
    await boot(page);
    await seedSharedPair(page);
    const [a, b] = [await barOf(page, 0), await barOf(page, 1)];
    expect(a.sid).not.toBe(b.sid);
    expect(a.background).toBe(b.background);
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
      return getComputedStyle(li, '::after').content;
    });
    expect(solo).toBe('none');
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
});

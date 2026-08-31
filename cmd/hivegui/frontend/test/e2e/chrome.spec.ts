import { test, expect, type Page } from '@playwright/test';

// Phase-4 chrome: banners, status bar, tile headers, launcher rows.
//
// Two kinds of assertion live here, and they are deliberately NOT gated
// alike:
//
//   - Structure and geometry (heights, slots, kbd hints, the selected
//     treatment) come straight out of the component CSS. They are the same
//     on every platform, they are what components.md actually specifies,
//     and jsdom cannot see any of them — so they run in CI, everywhere.
//   - Pixel baselines are a darwin-local review artefact: Playwright
//     suffixes snapshots per-platform and CI runs three OSes, so committed
//     darwin PNGs would fail the linux/windows legs outright. Those stay
//     behind HIVE_SNAPSHOT, like theme.spec.ts's.
//
// Gating both together is what let a `display`/`[hidden]` specificity bug
// ship green: the only assertions that could have caught it were skipped in
// CI along with the screenshots.
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function bootWith(page: Page, theme: string) {
  await page.addInitScript((t) => localStorage.setItem('hive.theme', t), theme);
  await page.setViewportSize({ width: 1100, height: 700 });
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

// Three sessions, all-sessions grid: every tile header renders.
async function enterGrid(page: Page) {
  await page.evaluate((n) => window.__hive.addSession?.(n), 's2');
  await page.evaluate((n) => window.__hive.addSession?.(n), 's3');
  await page.waitForFunction(
    (n) => (window.__hive.state?.sessions.length ?? 0) >= n,
    3,
  );
  await page.keyboard.press(`${mod}+Shift+g`);
  await expect(page.locator('#terms')).toHaveClass(/grid/);
}

const boxHeight = (page: Page, sel: string) =>
  page
    .locator(sel)
    .first()
    .evaluate((el) => Math.round(el.getBoundingClientRect().height));

test.describe('phase-4 chrome structure', () => {
  // components.md gives each surface a height. A wrong one means a token or
  // a rule went missing, which no unit test can observe.
  test('status bar is a 24px two-slot bar carrying kbd hints', async ({
    page,
  }) => {
    await bootWith(page, 'hive-dark');
    expect(await boxHeight(page, '#status')).toBe(24);

    // Single view: the hints name the way out of it.
    const hint = page.locator('#status-hint');
    await expect(hint).toContainText('grid');
    await expect(hint).toContainText('actions');
    // patterns.md: hints render through kbd(), never as bare text.
    await expect(hint.locator('kbd').first()).toBeVisible();

    // The left slot is the live region; the hints must not be inside it,
    // or every navigation re-announces a static shortcut.
    await expect(page.locator('#status-text')).toHaveAttribute(
      'aria-live',
      'polite',
    );
    await expect(page.locator('#status-hint')).not.toHaveAttribute(
      'aria-live',
      'polite',
    );
  });

  test('mode hints track the view and name bound chords', async ({ page }) => {
    await bootWith(page, 'hive-dark');
    const hint = page.locator('#status-hint');
    await expect(hint).toContainText('grid');

    await enterGrid(page);
    await expect(hint).toContainText('focus');
    await expect(hint).toContainText('move');
    // keyboard.ts sends plain ⌘G to grid-project from grid-all; only ⇧⌘G
    // returns to a single pane. A hint naming ⌘G here would lie.
    await expect(hint.locator('kbd').first()).toHaveText(/⇧⌘G|Ctrl\+Shift\+G/);
  });

  test('grid tile header is 28px with a state icon and visible actions', async ({
    page,
  }) => {
    await bootWith(page, 'hive-dark');
    await enterGrid(page);

    expect(await boxHeight(page, '.term-host.in-grid .tile-header')).toBe(28);
    const header = page.locator('.term-host.in-grid .tile-header').first();
    await expect(header.locator('.hv-state-icon')).toBeVisible();
    await expect(header.locator('.tile-name')).toBeVisible();
    // Not hover-revealed: display:none until hover is unreachable by
    // keyboard, and it is the trap session-row.css already rejected.
    await expect(header.locator('.tile-minimize')).toBeVisible();
    await expect(header.locator('.tile-minimize')).toHaveAttribute(
      'aria-label',
      /minimi[sz]e/i,
    );
  });

  test('launcher rows are 32px, hinted, and carry the selected treatment', async ({
    page,
  }) => {
    await bootWith(page, 'hive-dark');
    await page.keyboard.press(`${mod}+t`);
    await expect(page.locator('#launcher')).not.toHaveClass(/hidden/);

    const rows = page.locator('#launcher .launcher-item');
    await expect(rows.first()).toBeVisible();
    expect(await boxHeight(page, '#launcher .launcher-item')).toBe(32);

    // The digit hint is a kbd, and the top match is selected via the data
    // attribute the shared row CSS keys on.
    await expect(rows.first().locator('kbd')).toHaveText('[1]');
    await expect(rows.first()).toHaveAttribute('data-selected', '');
    await expect(rows.nth(1)).not.toHaveAttribute('data-selected', '');
  });

  // hive-light is where a contrast or a missing-token bug surfaces: a rule
  // that only resolved against the dark palette collapses to a transparent
  // or unreadable value here.
  test('both shipped presets paint the chrome', async ({ page }) => {
    for (const theme of ['hive-dark', 'hive-light']) {
      await bootWith(page, theme);
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
      expect(await boxHeight(page, '#status')).toBe(24);
      const bg = await page
        .locator('#status')
        .evaluate((el) => getComputedStyle(el).backgroundColor);
      // A missing --surface resolves to transparent, which reads as "the
      // bar vanished" rather than as a failure anywhere else.
      expect(bg).not.toBe('rgba(0, 0, 0, 0)');
    }
  });
});

test.describe('phase-4 chrome baselines', () => {
  test.skip(
    !process.env.HIVE_SNAPSHOT,
    'pixel baselines are darwin-local; run with HIVE_SNAPSHOT=1',
  );

  for (const theme of ['hive-dark', 'hive-light']) {
    test(`grid view — ${theme}`, async ({ page }) => {
      await bootWith(page, theme);
      await enterGrid(page);
      await expect(page).toHaveScreenshot(`grid-${theme}.png`, {
        maxDiffPixels: 0,
        animations: 'disabled',
        mask: [page.locator('.xterm')],
      });
    });

    test(`launcher — ${theme}`, async ({ page }) => {
      await bootWith(page, theme);
      await page.keyboard.press(`${mod}+t`);
      await expect(page.locator('#launcher')).not.toHaveClass(/hidden/);
      await expect(
        page.locator('#launcher .launcher-item').first(),
      ).toBeVisible();
      await expect(page).toHaveScreenshot(`launcher-${theme}.png`, {
        maxDiffPixels: 0,
        animations: 'disabled',
      });
    });
  }
});

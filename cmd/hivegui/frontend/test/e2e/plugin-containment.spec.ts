import { test, expect, type Page } from '@playwright/test';
import {
  boot,
  closeSettings,
  installPlugin,
  openPluginsTab,
} from './plugin-helpers.js';

// Spec 471 criterion 5: a UI plugin that throws, never finishes loading
// or rendering, or floods the app leaves the rest of the GUI usable —
// switching sessions, typing into a terminal, opening Settings — and can
// then be disabled from Settings. Fixtures: test/fixtures/plugins.

async function assertAppUsable(page: Page) {
  // Switch sessions from the sidebar.
  const second = await page.evaluate(async () => {
    await window.__hive.addSession?.('second');
    return window.__hive.state?.sessions.at(-1)?.id ?? '';
  });
  const row = page.locator(`#projects .hv-session-row[data-sid="${second}"]`);
  await row.click();
  await expect(row).toHaveAttribute('data-selected', '');
  // Type into its terminal.
  await page.locator('.term-focused .xterm-helper-textarea').first().focus();
  await page.keyboard.type('still-alive');
  await expect
    .poll(() => page.evaluate((sid) => window.__hive.stdinText(sid), second))
    .toContain('still-alive');
  // Settings still opens.
  await openPluginsTab(page);
}

const cases: { id: string; fails: boolean }[] = [
  { id: 'ui-throws', fails: true },
  { id: 'ui-hang-import', fails: true },
  { id: 'ui-hang-activate', fails: true },
  { id: 'ui-hang-render', fails: true },
  { id: 'ui-flood', fails: false },
];

for (const c of cases) {
  test(`${c.id}: the app stays usable and the plugin can be disabled`, async ({
    page,
  }) => {
    test.setTimeout(45_000);
    await boot(page);
    const row = await installPlugin(page, c.id);
    if (c.fails) {
      // The import timeout is 10s; everything else fails sooner.
      await expect(row).toHaveAttribute('data-status', 'failed', {
        timeout: 15_000,
      });
      await expect(row.locator('.settings-plugin-error')).toBeVisible();
    } else {
      await expect(row.locator('#ui-flood-tick')).toBeVisible();
    }
    await closeSettings(page);
    await assertAppUsable(page);

    const again = page.locator(
      `.settings-plugin-row[data-plugin-id="${c.id}"]`,
    );
    if (c.fails) await again.locator('.settings-plugin-disable').click();
    else await again.locator('.settings-plugin-enabled').uncheck();
    await expect(again).toHaveAttribute('data-status', 'disabled');
    await closeSettings(page);
    await expect(page.locator('.hv-plugin-badge')).toHaveCount(0);
  });
}

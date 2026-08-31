import { test, expect, type Page } from '@playwright/test';

// The regression this file exists for: `.hv-button` set
// `display: inline-flex` in author origin, which outranks the UA's
// `[hidden] { display: none }`. Every `el.hidden = true` on a banner
// button was therefore a silent no-op — and the jsdom tests could not
// see it, because `el.hidden` was faithfully `true` the whole time.
// Only a real layout engine tells these apart, so the assertion has to
// be toBeHidden()/toBeVisible(), not a property read.
async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

test('a hidden banner button is actually not rendered', async ({ page }) => {
  await boot(page);

  // "Checking for updates…" is the transient shape: no download URL, so
  // the Download action must be hidden, while the banner itself shows.
  await page.evaluate(() =>
    // Go emits a struct here, not a JSON string — the banner handlers
    // read fields off the payload directly.
    window.__hive.emit('update:available', {
      available: true,
      current: '2.4.0',
      latest: '2.5.0',
      url: '',
      stage: 'available',
      channel: 'release',
    }),
  );

  const banner = page.locator('#update-banner');
  await expect(banner).toBeVisible();
  await expect(banner.locator('[data-action-id="download"]')).toBeHidden();

  // And a banner that was never shown stays invisible even though its
  // markup exists from boot.
  await expect(page.locator('#daemon-banner')).toBeHidden();
});

test('the daemon banner shows and dismisses in a real layout', async ({
  page,
}) => {
  await boot(page);
  await page.evaluate(() =>
    window.__hive.emit('daemon:stale', {
      severity: 'mismatch',
      daemonBuild: 'daemon-1',
      guiBuild: 'gui-1',
    }),
  );

  const banner = page.locator('#daemon-banner');
  await expect(banner).toBeVisible();
  // Assert the mismatch copy, not just visibility: a wrong-shaped payload
  // would fall through to the "could not verify" branch and still show a
  // banner, passing this test for the wrong reason.
  await expect(banner).toContainText("doesn't match this GUI");
  await expect(banner.locator('[data-action-id="restart"]')).toBeVisible();

  await banner.locator('.hv-banner__dismiss').click();
  await expect(banner).toBeHidden();
});

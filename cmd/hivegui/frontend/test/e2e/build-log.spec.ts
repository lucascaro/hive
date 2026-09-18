import { test, expect, type Page } from '@playwright/test';

// A failed latest-channel build offers its full output. The Go side strips
// ANSI before it gets here (streamBuildOutput, covered in Go); what this
// pins is the browser half — the button's visibility follows the failure,
// the log is text (never markup), it scrolls, and it opens on the tail.

async function boot(page: Page) {
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li').length > 0,
  );
}

const failure = {
  available: true,
  current: 'aaaaaaa',
  latest: 'bbbbbbb',
  channel: 'latest',
  stage: 'error',
  message: 'build.sh failed (exit status 1): xcrun: error: license',
  canApply: true,
  hasBuildLog: true,
};

test('a failed build offers a scrollable log that opens on the tail', async ({
  page,
}) => {
  await boot(page);
  const lines = Array.from({ length: 400 }, (_, i) => `line ${i}`);
  lines.push('<b>not markup</b> xcrun: error: license not accepted');
  await page.evaluate((text) => {
    window.__hive.setBuildLog?.(text);
  }, lines.join('\n'));
  await page.evaluate((info) => {
    window.__hive.emit('update:progress', info);
  }, failure);

  const banner = page.locator('#update-banner');
  const view = banner.locator('[data-action-id="log"]');
  await expect(view).toBeVisible();
  await view.click();

  const text = page.locator('#build-log-text');
  await expect(text).toBeVisible();
  await expect(text).toBeFocused();
  // Rendered as text: the tag arrives literally, no <b> element exists.
  await expect(text).toContainText('<b>not markup</b>');
  await expect(text.locator('b')).toHaveCount(0);

  // The dialog body scrolls, and it opened scrolled to the end.
  const scroll = await page.evaluate(() => {
    const body = document.querySelector('#build-log .hv-dialog__body');
    if (!body) return null;
    return {
      scrollable: body.scrollHeight > body.clientHeight,
      atEnd: body.scrollTop + body.clientHeight >= body.scrollHeight - 2,
    };
  });
  expect(scroll).toEqual({ scrollable: true, atEnd: true });
  // The last line is actually on screen, not just in the DOM.
  const last = page.getByText('xcrun: error: license not accepted');
  await expect(last).toBeInViewport();

  await page.keyboard.press('Escape');
  await expect(page.locator('#build-log')).toBeHidden();
});

test('the log button leaves with the failure', async ({ page }) => {
  await boot(page);
  await page.evaluate((info) => {
    window.__hive.emit('update:progress', info);
  }, failure);
  const view = page.locator('#update-banner [data-action-id="log"]');
  await expect(view).toBeVisible();

  // A failure that was not a build has nothing to show.
  await page.evaluate((info) => {
    window.__hive.emit('update:progress', { ...info, hasBuildLog: false });
  }, failure);
  await expect(view).toBeHidden();

  // Nor does a later non-error banner inherit it.
  await page.evaluate((info) => {
    window.__hive.emit('update:progress', info);
  }, failure);
  await expect(view).toBeVisible();
  await page.evaluate((info) => {
    window.__hive.emit('update:progress', {
      ...info,
      stage: 'staging',
      message: 'Building…',
    });
  }, failure);
  await expect(view).toBeHidden();
});

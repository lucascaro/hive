import { test, expect } from '@playwright/test';

// The boot overlay is the answer to a blank window: on a cold machine
// the daemon can take seconds to bind its socket, and until the first
// session list lands the pane cannot tell "no sessions" from "no
// daemon yet". Layout is asserted with elementFromPoint — a spinner
// that is in the DOM but painted behind the terminal host is the same
// black pane the user reported.
test('covers the pane while the daemon is slow, then retires', async ({
  page,
}) => {
  await page.goto('/?slowConnect=1500');

  const boot = page.locator('#boot-state');
  await expect(boot).toBeVisible();
  await expect(boot.locator('.phase-spinner')).toBeVisible();

  // Actually on top in the main pane, not merely present.
  const onTop = await page.evaluate(() => {
    const el = document.getElementById('boot-state');
    if (!el) return 'missing';
    const r = el.getBoundingClientRect();
    const hit = document.elementFromPoint(
      r.left + r.width / 2,
      r.top + r.height / 2,
    );
    return hit?.closest('#boot-state')
      ? 'boot'
      : (hit?.id ?? hit?.className ?? 'other');
  });
  expect(onTop).toBe('boot');

  // The first session list retires it.
  await expect(boot).toBeHidden({ timeout: 10000 });
  await expect(page.locator('#terms')).toBeVisible();
});

// A daemon that never comes up must not leave the GUI dialing (and
// re-spawning hived) forever behind a spinner. The boot path gives up
// after a bounded number of attempts and hands the user a Retry.
test('gives up after bounded attempts and offers a retry', async ({ page }) => {
  // The initial connect plus all 5 bounded retries fail; the click
  // that follows succeeds.
  await page.goto('/?failConnect=6');

  const boot = page.locator('#boot-state');
  const retry = page.locator('#boot-state-retry');
  await expect(retry).toBeVisible({ timeout: 20000 });
  await expect(boot).toContainText('Could not reach the hive daemon');
  await expect(boot.locator('.phase-spinner')).toBeHidden();

  await retry.click();
  await expect(boot).toBeHidden({ timeout: 10000 });
});

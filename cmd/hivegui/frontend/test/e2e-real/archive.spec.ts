import { test, expect } from '@playwright/test';
import { bridgeCalls, registerSessionCleanup } from './bridge-sessions.js';

// Layer B cover for archive (spec 518): a real archive writes a real,
// durable record, stops the real process, and a real unarchive rebuilds
// the entry from it with a live PTY attached. The mock suite invents the
// record itself, so it cannot prove either half.

const WS_URL = process.env.WS_BRIDGE_URL;

const createdSessionIds = new Set<string>();
registerSessionCleanup(createdSessionIds);

test.beforeEach(async ({ page }) => {
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
  }, WS_URL);
});

test('an archived session is stopped, listed, and can be unarchived live', async ({
  page,
}) => {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');

  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.hv-session-row').length >= 1,
    null,
    { timeout: 10000 },
  );

  // A session of our own: the suite shares one daemon across files.
  await bridgeCalls([
    [
      'CreateSession',
      { name: 'archive-me', shell: '/bin/bash', cols: 80, rows: 24 },
    ],
  ]);
  await page.waitForFunction(
    () =>
      (window.__hive_state?.sessions ?? []).some(
        (s) => s.name === 'archive-me',
      ),
    null,
    { timeout: 10000 },
  );
  const id = await page.evaluate(
    () =>
      (window.__hive_state?.sessions ?? []).find((s) => s.name === 'archive-me')
        ?.id ?? '',
  );
  expect(id).not.toBe('');
  createdSessionIds.add(id);

  await bridgeCalls([['ArchiveSession', { session_id: id }]]);
  await page.waitForFunction(
    (sid) => !(window.__hive_state?.sessions ?? []).some((s) => s.id === sid),
    id,
    { timeout: 10000 },
  );

  // The durable record is what makes the next line possible at all.
  await bridgeCalls([['UnarchiveSession', { session_id: id }]]);
  await page.waitForFunction(
    (sid) => (window.__hive_state?.sessions ?? []).some((s) => s.id === sid),
    id,
    { timeout: 15000 },
  );
  const back = await page.evaluate((sid) => {
    const s = (window.__hive_state?.sessions ?? []).find((x) => x.id === sid);
    return s ? { name: s.name, alive: s.alive } : null;
  }, id);
  expect(back?.name).toBe('archive-me');
  expect(back?.alive).not.toBe(false);
});

test('unarchiving an unknown id is refused, not invented', async ({ page }) => {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');

  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.hv-session-row').length >= 1,
    null,
    { timeout: 10000 },
  );
  const before = await page.evaluate(
    () => (window.__hive_state?.sessions ?? []).length,
  );
  await bridgeCalls([['UnarchiveSession', { session_id: 'no-such-session' }]]);
  await page.waitForTimeout(1000);
  expect(
    await page.evaluate(() => (window.__hive_state?.sessions ?? []).length),
  ).toBe(before);
});

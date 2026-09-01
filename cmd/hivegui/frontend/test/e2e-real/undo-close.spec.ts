import { test, expect } from '@playwright/test';
import { bridgeCalls, registerSessionCleanup } from './bridge-sessions.js';

// Layer B cover for undo-close: a real close writes a real tombstone
// under the daemon's state dir, and a real restore rebuilds the entry
// from it and spawns a fresh PTY.
//
// The mock suite can only prove the wiring, because it invents the
// tombstone itself. What only the real daemon can prove is that the
// record survives the teardown it is written in front of, and that the
// restored session comes back with a live process attached — the two
// things the whole design rests on.

const WS_URL = process.env.WS_BRIDGE_URL;

// Module scope: registerSessionCleanup installs an afterAll hook.
const createdSessionIds = new Set<string>();
registerSessionCleanup(createdSessionIds);

test.beforeEach(async ({ page }) => {
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
  }, WS_URL);
});

test('a closed session can be reopened through the real daemon', async ({
  page,
}) => {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');

  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.hv-session-row').length >= 1,
    null,
    { timeout: 10000 },
  );

  // Work on a session of our own rather than the bootstrap one: this
  // spec closes it, and the suite shares a single daemon across every
  // file, so removing "main" would leak into whatever runs next.
  const beforeCount = await page.evaluate(
    () => document.querySelectorAll('#projects li.hv-session-row').length,
  );
  await bridgeCalls([
    [
      'CreateSession',
      { name: 'undo-me', shell: '/bin/bash', cols: 80, rows: 24 },
    ],
  ]);
  await page.waitForFunction(
    (n) => document.querySelectorAll('#projects li.hv-session-row').length > n,
    beforeCount,
    { timeout: 10000 },
  );

  const id = await page.evaluate(
    () =>
      (window.__hive_state?.sessions ?? []).find((s) => s.name === 'undo-me')
        ?.id ?? '',
  );
  expect(id).not.toBe('');
  createdSessionIds.add(id);

  await bridgeCalls([['KillSession', { session_id: id, force: true }]]);
  await page.waitForFunction(
    (sid) => !(window.__hive_state?.sessions ?? []).some((s) => s.id === sid),
    id,
    { timeout: 10000 },
  );

  // The tombstone the close wrote — before its own teardown — is what
  // makes the next line possible at all.
  await bridgeCalls([['RestoreSession', { session_id: id }]]);
  await page.waitForFunction(
    (sid) => (window.__hive_state?.sessions ?? []).some((s) => s.id === sid),
    id,
    { timeout: 15000 },
  );

  const restored = await page.evaluate((sid) => {
    const s = (window.__hive_state?.sessions ?? []).find((x) => x.id === sid);
    return s ? { name: s.name, alive: s.alive } : null;
  }, id);
  // Same id, same name — and a live process, not a dead tile: the
  // restore has to spawn, not merely re-add a row.
  expect(restored?.name).toBe('undo-me');
  expect(restored?.alive).not.toBe(false);
});

test('restoring a session that was never closed is refused, not invented', async ({
  page,
}) => {
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

  await bridgeCalls([['RestoreSession', { session_id: 'no-such-session' }]]);
  await page.waitForTimeout(1000);

  expect(
    await page.evaluate(() => (window.__hive_state?.sessions ?? []).length),
  ).toBe(before);
});

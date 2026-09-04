import { test, expect, type Page } from '@playwright/test';
import { bridgeCalls, registerSessionCleanup } from './bridge-sessions.js';
import { settleShell } from './term-harness.js';

// Rows 1–5 of the spec-336 smoke checklist, against the REAL daemon and
// the real frontend: the heuristic tier's working/idle flip on output,
// and the whole bell loop — PTY BEL → daemon waiting_input → attention
// broadcast → sidebar glyph → keystroke / switch-to → daemon clear →
// glyph back to idle. The mock suite cannot prove this: it invents the
// daemon's half.

const WS_URL = process.env.WS_BRIDGE_URL;
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

const createdSessionIds = new Set<string>();
registerSessionCleanup(createdSessionIds);

test.beforeEach(async ({ page }) => {
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
  }, WS_URL);
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.hv-session-row').length >= 1,
    null,
    { timeout: 10000 },
  );
  await settleShell(page);
});

/** data-state of the sidebar glyph for the row whose name is `name`. */
function rowState(page: Page, name: string): Promise<string | null> {
  return page.evaluate((n) => {
    const rows = [...document.querySelectorAll('#projects li.hv-session-row')];
    const row = rows.find((r) => r.textContent?.includes(n));
    return (
      row?.querySelector('svg.hv-state-icon')?.getAttribute('data-state') ??
      null
    );
  }, name);
}

async function expectRowState(
  page: Page,
  name: string,
  want: string,
  timeout = 6000,
) {
  await expect
    .poll(() => rowState(page, name), { timeout, intervals: [100, 250] })
    .toBe(want);
}

test('row 1: output flips the glyph to working, quiet flips it back to idle', async ({
  page,
}) => {
  await expectRowState(page, 'main', 'running');
  await page.keyboard.type('seq 1 400; sleep 1; seq 1 400\n');
  await expectRowState(page, 'main', 'working');
  // QuietAfter is 2 s; the ticker samples every 500 ms.
  await expectRowState(page, 'main', 'running', 8000);
});

test('rows 4–5: a bell in the session you are watching stays until you type', async ({
  page,
}) => {
  await expectRowState(page, 'main', 'running');
  await page.keyboard.type("printf '\\a'\n");
  await expectRowState(page, 'main', 'attention');
  // The shell repaints its prompt after the bell; that must not clear it,
  // and neither must merely sitting here focused.
  await page.waitForTimeout(3000);
  expect(await rowState(page, 'main')).toBe('attention');
  // A keystroke is the answer.
  await page.keyboard.type(' ');
  await expect
    .poll(() => rowState(page, 'main'), { timeout: 6000 })
    .not.toBe('attention');
  await expectRowState(page, 'main', 'running', 8000);
});

test('rows 2–3: a bell in another session lights it; switching to it clears it', async ({
  page,
}) => {
  const before = await page.evaluate(
    () => document.querySelectorAll('#projects li.hv-session-row').length,
  );
  await bridgeCalls([
    [
      'CreateSession',
      { name: 'second', shell: '/bin/bash', cols: 80, rows: 24 },
    ],
  ]);
  await page.waitForFunction(
    (n) => document.querySelectorAll('#projects li.hv-session-row').length > n,
    before,
    { timeout: 10000 },
  );
  for (const id of await page.evaluate(() =>
    (window.__hive_state?.sessions ?? [])
      .filter((s) => s.name === 'second')
      .map((s) => s.id),
  )) {
    createdSessionIds.add(id);
  }
  // Back in main, arm a delayed bell, then leave before it rings.
  await page.keyboard.press(`${mod}+1`);
  await page.waitForTimeout(300);
  await page.keyboard.type("sleep 2; printf '\\a'\n");
  await page.keyboard.press(`${mod}+2`);
  await page.waitForTimeout(300);
  await expectRowState(page, 'main', 'attention', 8000);
  // Being parked elsewhere does not clear it.
  await page.waitForTimeout(2000);
  expect(await rowState(page, 'main')).toBe('attention');
  // Arriving at it does.
  await page.keyboard.press(`${mod}+1`);
  await expect
    .poll(() => rowState(page, 'main'), { timeout: 6000 })
    .not.toBe('attention');
});

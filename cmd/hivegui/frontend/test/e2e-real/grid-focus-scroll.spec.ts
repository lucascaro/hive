// Regression guard for the black tile: entering grid and switching
// between sessions left the terminal scrolled out of its own box.
//
// Focusing xterm's helper textarea makes the browser scroll the nearest
// scrollable ancestor to reveal it. That ancestor is `.term-body`,
// which is `overflow: hidden` and carries roughly a viewport of scroll
// slack, so the terminal was scrolled entirely out of view and the tile
// rendered solid black until any resize clamped scrollTop back to 0.
// Measured on the unfixed build: scrollTop=352 against clientHeight=324.
//
// This lives in e2e-real, not the DOM suite: jsdom has no layout, so
// scrollTop/scrollHeight are always 0 there and the bug is invisible.
// The scenario is deliberately NOT minimised further — a smaller flood
// or fewer tiles stops reproducing (verified: a 400-line, single-tile
// version passes on the unfixed build).
import { expect, type Page, test } from '@playwright/test';
import { bridgeCalls, registerSessionCleanup } from './bridge-sessions.js';

const WS_URL = process.env.WS_BRIDGE_URL;
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

// The reporter is on a Retina display; the scroll slack depends on the
// laid-out size, so pin the ratio rather than inherit the runner's.
test.use({ deviceScaleFactor: 2 });

test.beforeEach(async ({ page }) => {
  await page.addInitScript((url) => {
    (window as unknown as { __WS_BRIDGE_URL?: string }).__WS_BRIDGE_URL = url;
  }, WS_URL);
});

// preventScroll here too. Without it this helper does the very thing
// the fix removes, and the guard would then depend on being called
// before the flood (no scroll slack yet) rather than on the fix — a
// spec that fails against correct code once someone reorders it, which
// is exactly the spec-245 failure mode. The assertion is about the
// APP's focus path during ⌘G / ⌘2 / ⌘1, not about this setup call.
async function focusFirstTerm(page: Page) {
  await page.evaluate(() => {
    const helper = document.querySelector<HTMLTextAreaElement>(
      '.term-host .xterm-helper-textarea',
    );
    if (!helper) throw new Error('no xterm helper textarea');
    helper.focus({ preventScroll: true });
  });
}

// Sessions this spec creates, removed once the file finishes — the
// e2e-real suite shares one daemon, so a leak lands in the next file
// (session-phases asserts on the session count).
const createdSessionIds = new Set<string>();
registerSessionCleanup(createdSessionIds);

// Extra sessions so grid splits the width the way the report does.
async function addSessions(page: Page, n: number) {
  const before = await page.evaluate(
    () => document.querySelectorAll('#projects li.hv-session-row').length,
  );
  await bridgeCalls(
    Array.from({ length: n }, (_, i) => [
      'CreateSession',
      { name: `extra${i}`, shell: '/bin/bash', cols: 80, rows: 24 },
    ]) as Array<[string, object]>,
  );
  await page.waitForFunction(
    (want) =>
      document.querySelectorAll('#projects li.hv-session-row').length >= want,
    before + n,
    { timeout: 15000 },
  );
  for (const id of await page.evaluate(
    () =>
      (
        window as unknown as {
          __hive_state?: { sessions?: Array<{ id: string; name?: string }> };
        }
      ).__hive_state?.sessions
        ?.filter((x) => x.name?.startsWith('extra'))
        .map((x) => x.id) ?? [],
  )) {
    createdSessionIds.add(id);
  }
}

test('a grid tile is not scrolled out of its own box when refocused', async ({
  page,
}) => {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set');
  await page.goto('/');
  await page.waitForFunction(
    () => !!document.querySelector('.term-host .xterm-helper-textarea'),
    null,
    { timeout: 15000 },
  );
  await focusFirstTerm(page);
  await page.keyboard.type('stty -echo\n');
  await page.waitForTimeout(200);
  await addSessions(page, 3);
  await page.keyboard.press(`${mod}+1`);
  await page.waitForTimeout(400);
  await focusFirstTerm(page);

  await page.keyboard.type(
    `awk 'BEGIN{for(j=0;j<40000;j++) printf "ROW_%06d ................................................\\n", j}'; echo PUMP_DONE\n`,
  );
  const tailText = () =>
    page.evaluate(() => {
      const st = [
        ...((
          window as unknown as {
            __hive_state?: { terms?: Map<string, unknown> };
          }
        ).__hive_state?.terms?.values() ?? []),
      ][0] as {
        term?: {
          buffer?: {
            active?: {
              length: number;
              getLine: (
                i: number,
              ) => { translateToString: () => string } | undefined;
            };
          };
        };
      };
      const buf = st?.term?.buffer?.active;
      if (!buf) return '';
      const out: string[] = [];
      for (let i = Math.max(0, buf.length - 40); i < buf.length; i++) {
        out.push(buf.getLine(i)?.translateToString() ?? '');
      }
      return out.join('\n');
    });
  // Output must have STOPPED: a tile still receiving bytes repaints on
  // its own and hides the symptom.
  await expect
    .poll(async () => ((await tailText()).includes('PUMP_DONE') ? 1 : 0), {
      timeout: 60000,
      intervals: [500],
    })
    .toBe(1);
  await page.waitForTimeout(1500);

  await page.keyboard.press(`${mod}+g`);
  await page.waitForTimeout(1200);
  await page.keyboard.press(`${mod}+2`);
  await page.waitForTimeout(800);
  await page.keyboard.press(`${mod}+1`);
  await page.waitForTimeout(1500);

  const scrolled = await page.evaluate(() => {
    const body = document.querySelector<HTMLElement>('.term-host .term-body');
    return {
      scrollTop: body?.scrollTop ?? -1,
      clientHeight: body?.clientHeight ?? -1,
    };
  });
  // Leave the app the way we found it: single view. This file sorts
  // before scroll-codex, and leaving grid mode behind changed what its
  // ⌘G toggles do — its replay-decision guard then saw zero decisions
  // and went flaky (2 runs in 3). Shared-daemon suites make housekeeping
  // part of the test.
  await page.keyboard.press(`${mod}+g`);
  await page.waitForTimeout(500);

  expect(
    scrolled.scrollTop,
    `tile scrolled out of its own box (scrollTop=${scrolled.scrollTop}, clientHeight=${scrolled.clientHeight}) — it renders solid black until a resize clamps this to 0`,
  ).toBe(0);
});

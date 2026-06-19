import { test, expect } from '@playwright/test';

// Real-browser verification of xterm.js (5.5) scrollback reflow on resize.
// jsdom (test/dom/xterm-reflow.test.js) proved content survives a resize but
// could NOT confirm rewrapping. This decides whether the daemon's full-ring
// replay-on-resize is needed for correctness, or whether xterm reflows on its
// own (in which case the normal-buffer replay can be dropped — the freeze fix).
const FIXTURE = '/test/e2e/fixtures/xterm-reflow.html';

async function makeTerm(page, cols, rows) {
  await page.goto(FIXTURE);
  await page.waitForFunction(() => window.__reflowReady === true);
  await page.evaluate(([c, r]) => window.__reflow.make(c, r), [cols, rows]);
}

const xLine = (page) =>
  page.evaluate(() => window.__reflow.lines().find((l) => l.text.startsWith('x')));

// Observed (real Chromium): the LIVE cursor line does NOT rewrap on resize.
test('live cursor line does not rewrap on widen (documents the limitation)', async ({ page }) => {
  await makeTerm(page, 20, 8);
  const sixty = 'x'.repeat(60);
  await page.evaluate((s) => window.__reflow.write(s), sixty);
  let line = await xLine(page);
  expect(line.rows).toBe(3); // ceil(60/20)
  await page.evaluate(() => window.__reflow.resize(40, 8));
  line = await xLine(page);
  expect(line.text).toBe(sixty); // content intact
  expect(line.rows).toBe(3); // NOT 2 — the live line is not reflowed
});

// The decisive case for shells: a wrapped line COMMITTED and scrolled into
// history. If xterm reflows this, the normal-buffer replay is redundant and
// can be dropped; if not, the replay is doing real work and must stay.
test('committed scrollback line: does xterm rewrap on widen?', async ({ page }) => {
  await makeTerm(page, 20, 4);
  const sixty = 'x'.repeat(60);
  // Commit the wrapped line, then push it up into scrollback with filler.
  await page.evaluate(async (s) => {
    await window.__reflow.write(s + '\r\n');
    for (let i = 0; i < 8; i++) await window.__reflow.write(`f${i}\r\n`);
  }, sixty);
  let line = await xLine(page);
  expect(line.text).toBe(sixty);
  expect(line.rows).toBe(3); // ceil(60/20) before resize

  await page.evaluate(() => window.__reflow.resize(40, 4));
  line = await xLine(page);
  expect(line.text).toBe(sixty); // content intact
  expect(line.rows).toBe(2); // reflowed to ceil(60/40) IFF xterm rewraps history
});

test('scrollback content survives resize', async ({ page }) => {
  await makeTerm(page, 20, 4);
  await page.evaluate(async () => {
    for (let i = 0; i < 30; i++) await window.__reflow.write(`line-${i}\r\n`);
  });
  await page.evaluate(() => window.__reflow.resize(40, 4));
  const texts = await page.evaluate(() =>
    window.__reflow.lines().map((l) => l.text.trim()));
  expect(texts).toContain('line-0'); // scrolled-off line still present
  expect(texts).toContain('line-29');
});

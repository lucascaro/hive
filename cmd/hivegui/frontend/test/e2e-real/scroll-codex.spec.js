import { test, expect } from '@playwright/test';

// Repro harness for the "scrolling jumps around with Codex when
// switching to grid mode or back" report. The mock-Wails e2e layer
// never emits scrollback_replay_begin/done, so the client-side replay
// state machine (reset, multi-chunk restream, done-snap,
// _replayWantsBottom) is exercised ONLY here, against a real hived.
//
// Codex's signature is a high, continuous output rate: there is
// almost always unparsed pty:data backlog inside xterm's async write
// queue when a replay begins. These tests put the real stack into
// that regime — a second session makes the grid split columns, so
// every grid↔single toggle crosses REPLAY_COL_THRESHOLD and fires a
// real replay (the scroll trace proves it; an invariant that holds
// over zero replays proves nothing). Invariants:
//   I1 (integrity): after any mode switch / replay, every emitted
//       marker line appears exactly once, in order.
//   I2 (anchoring): after a deliberate mode switch the viewport is at
//       the bottom and STAYS there while output continues; a reader
//       scrolled up into history is never yanked to the bottom by a
//       resize-triggered replay.

const WS_URL = process.env.WS_BRIDGE_URL;

test.beforeEach(async ({ page }) => {
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
    // Arm the scroll tracer (window.__hive_scrolltrace) before main.js loads.
    try { localStorage.setItem('hive.debug', '1'); } catch {}
  }, WS_URL);
});

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function bootWithTerm(page) {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.session-item').length >= 1,
    null, { timeout: 10000 },
  );
  await page.waitForFunction(
    () => !!document.querySelector('.term-host .xterm-helper-textarea'),
    null, { timeout: 10000 },
  );
  await focusFirstTerm(page);
  await page.keyboard.type('stty -echo\n');
  await page.waitForTimeout(200);
}

async function focusFirstTerm(page) {
  await page.evaluate(() => {
    const helper = document.querySelector('.term-host.active .xterm-helper-textarea')
      || document.querySelector('.term-host .xterm-helper-textarea');
    helper.focus();
  });
}

// Adds a second session by speaking the bridge's JSON-RPC protocol
// directly from Node (the GUI's launcher path can't run here: the
// ws-bridge implements only the session-lifecycle methods, and
// ListAgents falls into its empty-success default). The daemon
// broadcasts session:event(added) to every control conn, so the
// page's sidebar updates on its own. With two tiles, grid mode splits
// the width and the col delta always crosses REPLAY_COL_THRESHOLD.
async function addSecondSession(page) {
  // Node < 22 has no global WebSocket; fall back to the ws package.
  const WS = globalThis.WebSocket ?? (await import('ws')).WebSocket;
  const ws = new WS(WS_URL);
  await new Promise((res, rej) => { ws.onopen = res; ws.onerror = rej; });
  const send = (id, method, params = {}) => ws.send(JSON.stringify({ id, method, params }));
  const waitFor = (id) => new Promise((res) => {
    ws.addEventListener('message', function h(ev) {
      const m = JSON.parse(ev.data);
      if (m.id === id) { ws.removeEventListener('message', h); res(m); }
    });
  });
  send(1, 'ConnectControl');
  await waitFor(1);
  send(2, 'CreateSession', { name: 'second', shell: '/bin/bash', cols: 80, rows: 24 });
  const resp = await waitFor(2);
  ws.close();
  if (resp.error) throw new Error(`CreateSession via bridge: ${resp.error}`);
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.session-item').length >= 2,
    null, { timeout: 10000 },
  );
  // Back to the original "main" session (⌘1 = first in display order).
  await page.keyboard.press(`${mod}+1`);
  await page.waitForTimeout(300);
}

// Reads the FIRST session's buffer as text lines via xterm's buffer
// API (WebGL paints to canvas; the DOM holds nothing readable).
function bufferLines(page) {
  return page.evaluate(() => {
    const terms = window.__hive_state?.terms;
    if (!terms) return [];
    const st = [...terms.values()][0];
    const buf = st?.term?.buffer?.active;
    if (!buf) return [];
    const out = [];
    for (let i = 0; i < buf.length; i++) {
      out.push(buf.getLine(i)?.translateToString(true) || '');
    }
    return out;
  });
}

function scrollState(page) {
  return page.evaluate(() => {
    const terms = window.__hive_state?.terms;
    const st = terms ? [...terms.values()][0] : null;
    const buf = st?.term?.buffer?.active;
    if (!buf) return null;
    return { viewportY: buf.viewportY, baseY: buf.baseY, type: buf.type };
  });
}

function traceTags(page, tag) {
  return page.evaluate(
    (t) => (window.__hive_scrolltrace || []).filter((e) => e.tag === t).length,
    tag,
  );
}

// Starts a bounded high-rate marker pump inside the real bash session.
// Bursty: awk floods `burst` lines flat-out, then sleeps — keeps
// xterm's async write queue loaded the way codex output does, without
// an unbounded loop that could leak past teardown.
async function startMarkerPump(page, count, burst = 40) {
  await page.keyboard.type(
    `i=0; while [ $i -lt ${count} ]; do awk -v s=$i -v n=${burst} 'BEGIN{for(j=s;j<s+n;j++) printf "HIVE_SCROLL_%06d ................................................\\n", j}'; i=$((i+${burst})); sleep 0.05; done; echo HIVE_PUMP_DONE\n`,
  );
}

function extractMarkers(lines) {
  const out = [];
  for (const l of lines) {
    const m = l.match(/HIVE_SCROLL_(\d{6})/);
    if (m) out.push(parseInt(m[1], 10));
  }
  return out;
}

test('markers survive grid↔single toggles under continuous output, exactly once and in order', async ({ page }) => {
  await bootWithTerm(page);
  await addSecondSession(page);
  await startMarkerPump(page, 1200);

  // Toggle to grid and back twice while the pump is printing. With two
  // tiles the grid split changes cols by tens of columns, firing real
  // scrollback replays with live bytes still in flight.
  for (let i = 0; i < 2; i++) {
    await page.waitForTimeout(700);
    await page.keyboard.press(`${mod}+g`);
    await page.waitForTimeout(700);
    await page.keyboard.press(`${mod}+g`);
  }

  await expect.poll(
    async () => (await bufferLines(page)).join('\n'),
    { timeout: 30000, intervals: [250, 500] },
  ).toContain('HIVE_PUMP_DONE');
  await page.waitForTimeout(1200); // let any trailing replay land

  // Non-vacuity: the scenario must have fired at least one real replay.
  expect(await traceTags(page, 'replay-request')).toBeGreaterThan(0);
  expect(await traceTags(page, 'scrollback_replay_done')).toBeGreaterThan(0);

  const markers = extractMarkers(await bufferLines(page));
  expect(markers.length).toBeGreaterThan(0);

  // I1a: strictly increasing (no out-of-order interleave).
  const unsorted = markers.filter((m, i) => i > 0 && m <= markers[i - 1]);
  expect(unsorted, `out-of-order/duplicate markers: ${unsorted.slice(0, 10)}`).toEqual([]);

  // I1b: no duplicates (a backlog-after-reset replay paints lines twice).
  const dupes = markers.filter((m, i) => markers.indexOf(m) !== i);
  expect(dupes, `duplicated markers: ${[...new Set(dupes)].slice(0, 10)}`).toEqual([]);
});

test('viewport stays anchored at bottom after a mode switch under continuous output', async ({ page }) => {
  await bootWithTerm(page);
  await addSecondSession(page);
  await startMarkerPump(page, 1500);
  await page.waitForTimeout(700);

  await page.keyboard.press(`${mod}+g`);

  // From after the deliberate-snap window (250ms + focus settle) until
  // well past replay completion, the viewport must track the bottom —
  // output is still streaming and the user just asked for this view.
  const samples = [];
  for (let t = 600; t <= 2400; t += 200) {
    await page.waitForTimeout(200);
    const s = await scrollState(page);
    if (s) samples.push(s);
  }
  const offBottom = samples.filter((s) => s.viewportY !== s.baseY);
  expect(offBottom, `viewport left the bottom: ${JSON.stringify(offBottom.slice(0, 5))}`).toEqual([]);

  await page.keyboard.press(`${mod}+g`);
  const samples2 = [];
  for (let t = 600; t <= 2400; t += 200) {
    await page.waitForTimeout(200);
    const s = await scrollState(page);
    if (s) samples2.push(s);
  }
  const offBottom2 = samples2.filter((s) => s.viewportY !== s.baseY);
  expect(offBottom2, `viewport left the bottom after return: ${JSON.stringify(offBottom2.slice(0, 5))}`).toEqual([]);

  // Non-vacuity: replays must actually have fired across the toggles.
  expect(await traceTags(page, 'replay-request')).toBeGreaterThan(0);
});

test('a reader scrolled into history is not yanked to the bottom by a resize replay', async ({ page }) => {
  await bootWithTerm(page);
  // Fill scrollback, then stop output so the read position is stable.
  await startMarkerPump(page, 200);
  await expect.poll(
    async () => (await bufferLines(page)).join('\n'),
    { timeout: 15000, intervals: [250, 500] },
  ).toContain('HIVE_PUMP_DONE');

  // Scroll up with a real wheel gesture so the clamped wheel handler runs.
  const term = page.locator('.term-host .term-body').first();
  await term.hover();
  for (let i = 0; i < 6; i++) {
    await page.mouse.wheel(0, -300);
    await page.waitForTimeout(50);
  }
  const before = await scrollState(page);
  expect(before.viewportY).toBeLessThan(before.baseY);

  // A viewport resize big enough to cross REPLAY_COL_THRESHOLD fires a
  // real replay. The replay-done must respect the reader's position.
  await page.setViewportSize({ width: 860, height: 600 });
  await page.waitForTimeout(1500);

  expect(await traceTags(page, 'replay-request')).toBeGreaterThan(0);
  const after = await scrollState(page);
  expect(after.viewportY, 'replay-done yanked the reader to the bottom').toBeLessThan(after.baseY);
});

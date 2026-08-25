import { test, expect, type Page } from '@playwright/test';
// `state.terms` is a Map<string, TermTile> — the deliberately narrow structural
// view app modules use (app/state.ts). These specs poke the concrete tile the
// map actually holds, which is what carries the real xterm Terminal, so they
// assert SessionTerm rather than widening TermTile for every app caller and
// DOM-test stub (wave 5b's rule).
import type { SessionTerm } from '../../src/app/session-term.js';

// `wheelDeltaY` is a legacy read-only getter, not a WheelEventInit member, so
// it rides along here and is installed on the instance (see dispatchWheel).
// `view` is omitted because comparing the app's augmented Window back to lib
// DOM's Window trips a structural check in the clipboard types; no spec passes
// it, and the event's default view is the page's own.
type WheelInit = Omit<WheelEventInit, 'view'> & { wheelDeltaY?: number };

// The scrollLines spy below lives only for the duration of one spec. Local
// types rather than a `declare global`: these are not part of the app's
// runtime surface, and a global declaration would advertise them to src/ too
// (see test/e2e/hive-global.d.ts on why that file's globals are program-wide).
type SpyWindow = Window & { __takeoverCalls?: number };
type SpyTerm = SessionTerm & { __wheelWrapped?: boolean };

// Repro for "the terminal won't scroll by mouse wheel or trackpad on one of my
// Macs" (selection-drag still scrolls). PR #229 theorized a WKWebView quirk
// (deltaMode LINE/PAGE or deltaY=0 with the value only in legacy wheelDeltaY)
// and normalized the math in lib/wheel-scroll.ts — yet the Mac still doesn't
// scroll, which means the root cause is OUTSIDE that pure function.
//
// The existing scroll-codex.spec drives scrolling via page.mouse.wheel, which
// in Chromium ONLY emits pixel-mode (deltaMode 0) events. So it can never
// exercise the line-mode handler path or catch a term.scrollLines() no-op —
// which is exactly why CI stays green while the Mac fails.
//
// These tests dispatch SYNTHETIC wheel events of each suspect shape directly at
// .term-host (where SessionTerm's capture-phase listener lives) and assert the
// terminal viewport ACTUALLY moves — exercising handler → wheelToScrollLines →
// term.scrollLines → viewport end-to-end, not the math in isolation. This is an
// integration layer CI currently lacks; it will lock in whichever fix we land.
//
// NOTE: this harness runs Chromium, not WKWebView, so it reproduces branches
// B (lines still 0) / C (scrollLines no-op) / D (re-pin) deterministically. For
// branch A (no JS wheel event at all) the affected-Mac scroll trace is the
// authority — Chromium always delivers the synthetic event we dispatch.

const WS_URL = process.env.WS_BRIDGE_URL;

test.beforeEach(async ({ page }) => {
  // Quarantined on CI: flaky setup under CI CPU contention (spec 245). Runs
  // locally via `npm run test:e2e:real`. Re-gate per spec 245 (10 green runs).
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
    // Arm the scroll tracer (window.__hive_scrolltrace) before main.ts loads,
    // so a failure attaches the derived `lines` per wheel event below.
    try {
      localStorage.setItem('hive.debug', '1');
    } catch {}
  }, WS_URL);
});

// On failure, attach the armed scroll trace so the artifact shows the raw
// delta we dispatched vs. the line count the handler derived (lines===0 ⇒ math
// gap; lines!==0 with no viewport move ⇒ scrollLines no-op). Best-effort.
test.afterEach(async ({ page }, testInfo) => {
  if (testInfo.status !== testInfo.expectedStatus) {
    try {
      const trace = await page.evaluate(() => window.__hive_scrolltrace);
      await testInfo.attach('scrolltrace', {
        body: JSON.stringify(trace ?? null),
        contentType: 'application/json',
      });
    } catch {
      /* page closed / nav race — keep the real failure */
    }
  }
});

async function bootWithTerm(page: Page) {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.session-item').length >= 1,
    null,
    { timeout: 10000 },
  );
  await page.waitForFunction(
    () => !!document.querySelector('.term-host .xterm-helper-textarea'),
    null,
    { timeout: 10000 },
  );
  await page.evaluate(() => {
    const helper =
      document.querySelector<HTMLTextAreaElement>(
        '.term-host.active .xterm-helper-textarea',
      ) ||
      document.querySelector<HTMLTextAreaElement>(
        '.term-host .xterm-helper-textarea',
      );
    // The waitForFunction above already proved it exists; throwing here says
    // so rather than turning a broken wait into a silently unfocused term.
    if (!helper) throw new Error('no xterm helper textarea to focus');
    helper.focus();
  });
  await page.keyboard.type('stty -echo\n');
  await page.waitForTimeout(200);
}

// Bounded high-rate marker pump (bursty: flood, sleep) — fills scrollback so
// there is history above the viewport to scroll INTO, then stops so the read
// position is stable.
async function startMarkerPump(page: Page, count: number, burst = 40) {
  await page.keyboard.type(
    `i=0; while [ $i -lt ${count} ]; do awk -v s=$i -v n=${burst} 'BEGIN{for(j=s;j<s+n;j++) printf "HIVE_SCROLL_%06d ................................................\\n", j}'; i=$((i+${burst})); sleep 0.05; done; echo HIVE_PUMP_DONE\n`,
  );
}

function bufferLines(page: Page) {
  return page.evaluate(() => {
    const st = [...(window.__hive_state?.terms?.values() || [])][0] as
      | SessionTerm
      | undefined;
    const buf = st?.term?.buffer?.active;
    if (!buf) return [];
    const out = [];
    for (let i = 0; i < buf.length; i++) {
      out.push(buf.getLine(i)?.translateToString(true) || '');
    }
    return out;
  });
}

function scrollState(page: Page) {
  return page.evaluate(() => {
    const st = [...(window.__hive_state?.terms?.values() || [])][0] as
      | SessionTerm
      | undefined;
    const buf = st?.term?.buffer?.active;
    if (!buf) return null;
    return { viewportY: buf.viewportY, baseY: buf.baseY, type: buf.type };
  });
}

// Every direct-assertion site below has already waited for the term to attach,
// so a null there is a broken wait rather than a state worth asserting on.
// expect.poll() keeps using scrollState(), which must tolerate the
// not-yet-attached window.
async function mustScrollState(page: Page) {
  const s = await scrollState(page);
  if (!s) throw new Error('no term buffer — the boot wait did not hold');
  return s;
}

// Dispatch a real WheelEvent at the element actually under the terminal's
// center (via elementFromPoint — the same hit-test the browser does), so the
// event must propagate up through the capture phase to SessionTerm's listener
// on .term-host. This is faithful to a webview-delivered gesture: it exercises
// not just the handler math but that the event PATH reaches the handler at all.
async function dispatchWheel(page: Page, init: WheelInit, times = 6) {
  await page.evaluate(
    ({ init, times }) => {
      const host =
        document.querySelector<HTMLElement>('.term-host.active') ||
        document.querySelector<HTMLElement>('.term-host');
      if (!host) throw new Error('no .term-host to dispatch the wheel at');
      const r = host.getBoundingClientRect();
      const target =
        document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2) ||
        host;
      // `wheelDeltaY` is a legacy read-only getter, not a WheelEvent constructor
      // member — it can't be passed via the init dict, so override it on the
      // instance to simulate a webview that only populates the deprecated field.
      const { wheelDeltaY, ...ctor } = init;
      for (let i = 0; i < times; i++) {
        const ev = new WheelEvent('wheel', {
          bubbles: true,
          cancelable: true,
          ...ctor,
        });
        if (wheelDeltaY !== undefined) {
          Object.defineProperty(ev, 'wheelDeltaY', {
            value: wheelDeltaY,
            configurable: true,
          });
        }
        target.dispatchEvent(ev);
      }
    },
    { init, times },
  );
  await page.waitForTimeout(100);
}

// Count whether the GUI's wheel takeover (term.scrollLines) actually ran for a
// dispatched gesture. Distinguishes "handler bailed, event went to the app"
// (count 0 — correct under mouse tracking / alt buffer) from "handler ran"
// (count > 0). Wraps scrollLines once per session and resets the counter per call.
async function countTakeover(page: Page, init: WheelInit) {
  await page.evaluate(() => {
    const w = window as SpyWindow;
    const st = [...(window.__hive_state?.terms?.values() || [])][0] as
      | SpyTerm
      | undefined;
    w.__takeoverCalls = 0;
    if (st && !st.__wheelWrapped) {
      const orig = st.term.scrollLines.bind(st.term);
      st.term.scrollLines = (n: number) => {
        w.__takeoverCalls = (w.__takeoverCalls ?? 0) + 1;
        return orig(n);
      };
      st.__wheelWrapped = true;
    }
  });
  await dispatchWheel(page, init);
  return page.evaluate(() => (window as SpyWindow).__takeoverCalls ?? 0);
}

// Make the running program enable mouse tracking (DECSET 1000), as Claude/vim
// do. The bytes flow shell → pty → xterm, which sets term.modes.mouseTrackingMode.
async function enableMouseTracking(page: Page) {
  await page.keyboard.type("printf '\\033[?1000h'\n");
  await page.waitForFunction(
    () => {
      const st = [...(window.__hive_state?.terms?.values() || [])][0] as
        | SessionTerm
        | undefined;
      return (
        st?.term?.modes?.mouseTrackingMode &&
        st.term.modes.mouseTrackingMode !== 'none'
      );
    },
    null,
    { timeout: 10000 },
  );
}

// Switch the session into the alternate screen buffer (DECSET 1049), as a
// full-screen TUI does — there is no scrollback there, so scrollLines is a no-op.
async function enterAltBuffer(page: Page) {
  await page.keyboard.type("printf '\\033[?1049h'\n");
  await page.waitForFunction(
    () => {
      const st = [...(window.__hive_state?.terms?.values() || [])][0] as
        | SessionTerm
        | undefined;
      return st?.term?.buffer?.active?.type === 'alternate';
    },
    null,
    { timeout: 10000 },
  );
}

// Fill scrollback and confirm the viewport is following the bottom, so any
// subsequent upward move is unambiguously the wheel gesture's doing.
async function primeScrollback(page: Page) {
  await bootWithTerm(page);
  await startMarkerPump(page, 200);
  await expect
    .poll(async () => (await bufferLines(page)).join('\n'), {
      timeout: 30000,
      intervals: [250, 500],
    })
    .toContain('HIVE_PUMP_DONE');
  const at = await mustScrollState(page);
  expect(at.baseY, 'scrollback should be populated').toBeGreaterThan(0);
  expect(at.viewportY, 'viewport should start at the bottom').toBe(at.baseY);
}

// The control: pixel-mode deltaY is the normal macOS-trackpad / Chromium path
// and the shape that works on the user's other Macs. If this ever fails the
// takeover itself is broken; it also guards against a fix regressing the
// working path.
test('pixel-mode wheel scrolls into history (control — the working path)', async ({
  page,
}) => {
  await primeScrollback(page);
  await dispatchWheel(page, { deltaMode: 0, deltaY: -300 });
  const after = await mustScrollState(page);
  expect(after.viewportY, 'pixel wheel did not scroll up').toBeLessThan(
    after.baseY,
  );
});

// The suspected unscrollable-Mac shape: a wheel notch reported as a line count
// (deltaMode 1) rather than pixels.
test('line-mode wheel scrolls into history (deltaMode 1)', async ({ page }) => {
  await primeScrollback(page);
  await dispatchWheel(page, { deltaMode: 1, deltaY: -3 });
  const after = await mustScrollState(page);
  expect(after.viewportY, 'line-mode wheel did not scroll up').toBeLessThan(
    after.baseY,
  );
});

// The other suspected shape: standard deltaY is 0 and only the deprecated
// wheelDeltaY (opposite sign, pixel-scale) carries the gesture.
test('legacy wheelDeltaY-only wheel scrolls into history (deltaY 0)', async ({
  page,
}) => {
  await primeScrollback(page);
  await dispatchWheel(page, { deltaMode: 0, deltaY: 0, wheelDeltaY: 120 });
  const after = await mustScrollState(page);
  expect(
    after.viewportY,
    'wheelDeltaY-only wheel did not scroll up',
  ).toBeLessThan(after.baseY);
});

// THE ACTUAL REGRESSION ("scrolls in pi, not in Claude" on the same machine).
// When the running program enables mouse tracking it expects the wheel as
// mouse events; the GUI's capture-phase preventDefault + scrollLines takeover
// swallowed the gesture, so the app could never scroll. The takeover must bail
// and let xterm forward the wheel to the app.
test('mouse-tracking session forwards the wheel to the app, not the scrollback takeover', async ({
  page,
}) => {
  await primeScrollback(page);
  // Sanity: in the plain normal buffer (pi) the takeover runs.
  expect(
    await countTakeover(page, { deltaMode: 1, deltaY: -3 }),
    'takeover should run in a plain normal buffer',
  ).toBeGreaterThan(0);
  // Claude enables mouse tracking → the takeover must NOT swallow the wheel.
  await enableMouseTracking(page);
  expect(
    await countTakeover(page, { deltaMode: 1, deltaY: -3 }),
    'takeover swallowed the wheel under mouse tracking (Claude could not scroll)',
  ).toBe(0);
});

// Full-screen TUIs run in the alternate buffer where scrollLines is a no-op;
// the takeover must bail there too so the app receives the wheel.
test('alternate-buffer session does not swallow the wheel', async ({
  page,
}) => {
  await bootWithTerm(page);
  await enterAltBuffer(page);
  expect(
    await countTakeover(page, { deltaMode: 1, deltaY: -3 }),
    'takeover ran in the alternate buffer',
  ).toBe(0);
});

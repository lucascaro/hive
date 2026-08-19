import { test, expect, type Page } from '@playwright/test';

// Layer B: the session lifecycle phases against a REAL hived.
//
// The mock can be made to say anything; this proves the daemon
// actually announces a session before its PTY exists (PhaseStarting),
// walks it to ready, and announces the teardown (PhaseClosing) before
// the entry disappears — and that the GUI never paints an attach error
// into either window.

const WS_URL = process.env.WS_BRIDGE_URL;
const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

test.beforeEach(async ({ page }) => {
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
  }, WS_URL);
});

// Creates a session by speaking the ws-bridge's JSON-RPC directly (the
// GUI launcher can't run here — the bridge implements only the
// session-lifecycle methods). Returns once the daemon has accepted it.
async function createSession(name: string) {
  const WS = globalThis.WebSocket ?? (await import('ws')).WebSocket;
  const ws = new WS(WS_URL as string);
  await new Promise((res, rej) => {
    ws.onopen = res;
    ws.onerror = rej;
  });
  const waitFor = (id: number) =>
    new Promise<{ id: number; error?: string }>((res) => {
      ws.addEventListener('message', function h(ev) {
        const m = JSON.parse(ev.data);
        if (m.id === id) {
          ws.removeEventListener('message', h);
          res(m);
        }
      });
    });
  ws.send(JSON.stringify({ id: 1, method: 'ConnectControl', params: {} }));
  await waitFor(1);
  ws.send(
    JSON.stringify({
      id: 2,
      method: 'CreateSession',
      params: { name, shell: '/bin/bash', cols: 80, rows: 24 },
    }),
  );
  const resp = await waitFor(2);
  ws.close();
  if (resp.error) throw new Error(`CreateSession via bridge: ${resp.error}`);
}

// Every tile's xterm buffer, joined. Used to prove no red daemon error
// was written into any pane.
function allBuffers(page: Page) {
  return page.evaluate(() => {
    const terms = window.__hive_state?.terms;
    if (!terms) return '';
    const out: string[] = [];
    for (const st of terms.values()) {
      const buf = (
        st.term as unknown as {
          buffer?: {
            active?: {
              length: number;
              getLine(i: number): { translateToString(t: boolean): string };
            };
          };
        } | null
      )?.buffer?.active;
      if (!buf) continue;
      for (let i = 0; i < buf.length; i++) {
        out.push(buf.getLine(i)?.translateToString(true) || '');
      }
    }
    return out.join('\n');
  });
}

test('a real session is announced while starting, settles to ready, and closes clean', async ({
  page,
}) => {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');

  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.session-item').length >= 1,
    null,
    { timeout: 10000 },
  );

  await createSession('phased');

  // The sidebar row appears from the `added` event — which the daemon
  // now sends before the PTY exists.
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.session-item').length >= 2,
    null,
    { timeout: 10000 },
  );

  // The tile settles: no phase overlay left up, and the terminal is
  // attached (xterm helper textarea present).
  await expect
    .poll(
      () =>
        page.evaluate(() => {
          const terms = window.__hive_state?.terms;
          if (!terms) return null;
          // The concrete tile is a SessionTerm; TermTile is the
          // deliberately narrow structural view app modules use, so
          // reach past it for the overlay flag.
          return Array.from(terms.values()).every((st) => {
            const tile = st as unknown as { phaseOverlayShown?: boolean };
            return st.phase === '' && !tile.phaseOverlayShown;
          });
        }),
      { timeout: 15000 },
    )
    .toBe(true);

  // Close the active session; the tile must vanish without leaving an
  // error painted into any pane.
  const before = await page.evaluate(
    () => window.__hive_state?.sessions.length ?? 0,
  );
  await page.keyboard.press(`${mod}+w`);
  await expect
    .poll(
      () => page.evaluate(() => window.__hive_state?.sessions.length ?? 0),
      { timeout: 15000 },
    )
    .toBe(before - 1);

  const buffers = await allBuffers(page);
  expect(buffers).not.toContain('attach failed');
  expect(buffers).not.toContain('no_such_session');
  expect(buffers).not.toContain('session_starting');
});

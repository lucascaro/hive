import { test, expect } from '@playwright/test';

// Layer B smoke: real-daemon end-to-end. Boots the frontend against a
// real hived daemon (via hived-ws-bridge), waits for the bootstrap
// session to render, types into the active terminal, and verifies the
// echoed output appears in xterm's buffer. This single test proves
// the whole stack — wire handshake, control snapshots, attach, PTY
// fanout, base64 transport, xterm rendering — is wired correctly.
//
// Any payload-shape drift between hived (snake_case) and the
// frontend's reader (`snake_case ?? camelCase`) surfaces here because
// the daemon's real frames are flowing through, not a mock.

const WS_URL = process.env.WS_BRIDGE_URL;

test.beforeEach(async ({ page }) => {
  // Inject the bridge URL BEFORE main.js loads so the bridge module's
  // ensureWS() can resolve it.
  await page.addInitScript((url) => { window.__WS_BRIDGE_URL = url; }, WS_URL);
});

test('boots, sees bootstrap session, echoes typed input through real hived', async ({ page }) => {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');

  await page.goto('/');
  // Sidebar must render the daemon's bootstrap session (name "main").
  await page.waitForFunction(() => {
    return document.querySelectorAll('#projects li.session-item').length >= 1;
  }, null, { timeout: 10000 });
  await expect(page.locator('#projects li.session-item').first()).toContainText('main');

  // Wait for the active term to attach (xterm helper textarea exists).
  await page.waitForFunction(() => {
    return !!document.querySelector('.term-host .xterm-helper-textarea');
  }, null, { timeout: 10000 });

  // Disable tty echo so the typed bytes don't appear in the buffer
  // alongside the output — keeps the assertion robust.
  await page.evaluate(async () => {
    const helper = document.querySelector('.term-host.active .xterm-helper-textarea')
      || document.querySelector('.term-host .xterm-helper-textarea');
    helper.focus();
  });
  await page.keyboard.type('stty -echo\n');
  await page.waitForTimeout(200);

  // Type a marker and a literal echo command. Bash sees this AFTER
  // stty -echo so the only thing rendered is the output line.
  await page.keyboard.type('echo HIVE_REAL_MARK_$((40+2))\n');

  // Poll the xterm buffer for the marker. We read via xterm's own
  // buffer API (not DOM) because the WebGL renderer paints to canvas;
  // .xterm-rows is not populated under that path.
  await expect.poll(async () => {
    return page.evaluate(() => {
      const terms = window.__hive_state?.terms;
      if (!terms) return null;
      const out = [];
      for (const st of terms.values()) {
        const buf = st.term?.buffer?.active;
        if (!buf) continue;
        for (let i = 0; i < buf.length; i++) {
          out.push(buf.getLine(i)?.translateToString(true) || '');
        }
      }
      return out.join('\n');
    });
  }, { timeout: 5000, intervals: [100, 200, 500] }).toContain('HIVE_REAL_MARK_42');
});

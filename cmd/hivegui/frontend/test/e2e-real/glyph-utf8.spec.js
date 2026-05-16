import { test, expect } from '@playwright/test';

// Layer B regression cover for the multi-byte-glyph class (#195).
// The daemon emits raw PTY bytes; the frontend's per-session
// TextDecoder must NOT leak partial UTF-8 sequences across session
// boundaries (the bug shared a single decoder across all sessions,
// turning a half-printed CJK char in session A into garbage at the
// start of session B's next chunk).
//
// We can't easily force a chunk boundary mid-sequence over the real
// daemon, so this spec exercises the simpler, more robust invariant:
// a session whose output contains multi-byte UTF-8 characters renders
// them as code points (not replacement chars or literal byte values).
// The mock-Wails harness can't catch this because it controls the
// bytes itself; only the real wire path proves the decoder is wired.

const WS_URL = process.env.WS_BRIDGE_URL;

test.beforeEach(async ({ page }) => {
  await page.addInitScript((url) => { window.__WS_BRIDGE_URL = url; }, WS_URL);
});

test('multi-byte UTF-8 round-trips through the real wire path without corruption', async ({ page }) => {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');

  await page.goto('/');
  await page.waitForFunction(() => {
    return document.querySelectorAll('#projects li.session-item').length >= 1
        && !!document.querySelector('.term-host .xterm-helper-textarea');
  }, null, { timeout: 10000 });

  await page.evaluate(() => {
    const helper = document.querySelector('.term-host.active .xterm-helper-textarea')
      || document.querySelector('.term-host .xterm-helper-textarea');
    helper.focus();
  });
  await page.keyboard.type('stty -echo\n');
  await page.waitForTimeout(200);

  // Use printf %b so the bytes are emitted literally without bash
  // interpreting the multibyte sequence as a glob/expansion. The
  // sequence covers 2-byte (é U+00E9), 3-byte (中 U+4E2D), and 4-byte
  // (😀 U+1F600) UTF-8 paths.
  await page.keyboard.type("printf '%s\\n' 'GLYPH é中😀 END'\n");

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
  }, { timeout: 5000, intervals: [100, 200, 500] }).toContain('GLYPH é中😀 END');
});

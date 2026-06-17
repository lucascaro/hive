import { test, expect } from '@playwright/test';

// Regression guard for the restream strand: a FOLLOWING viewport must stay
// pinned to the bottom for the WHOLE resize-replay restream, not just end
// there. The begin handler's term.reset() wipes the viewport to the top and
// cap-trim then strands it in history; on the buggy build the viewport sits
// mid-history for ~1s (until replay-done re-snaps it) — the user-reported
// "scrolling jumps" (trace signature: following:true, tiny sinceReplayMs).
// The existing scroll-codex specs assert only the FINAL position / the replay
// DECISION (wants=false), so they miss this transient. Here we sample the
// gap (baseY - viewportY) across the restream and assert the follower is
// never left stranded. Deterministic: the strand window is ~1s wide, far
// larger than the sampling interval.

const WS_URL = process.env.WS_BRIDGE_URL;

test.beforeEach(async ({ page }) => {
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
    try { localStorage.setItem('hive.debug', '1'); } catch {}
  }, WS_URL);
});

test.afterEach(async ({ page }, testInfo) => {
  if (testInfo.status !== testInfo.expectedStatus) {
    try {
      const trace = await page.evaluate(() => window.__hive_scrolltrace);
      await testInfo.attach('scrolltrace', { body: JSON.stringify(trace ?? null), contentType: 'application/json' });
    } catch { /* ignore */ }
  }
});

async function bootWithTerm(page) {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set');
  await page.goto('/');
  await page.waitForFunction(() => document.querySelectorAll('#projects li.session-item').length >= 1, null, { timeout: 10000 });
  await page.waitForFunction(() => !!document.querySelector('.term-host .xterm-helper-textarea'), null, { timeout: 10000 });
  await page.evaluate(() => {
    const h = document.querySelector('.term-host.active .xterm-helper-textarea') || document.querySelector('.term-host .xterm-helper-textarea');
    h.focus();
  });
  await page.keyboard.type('stty -echo\n');
  await page.waitForTimeout(200);
}

function scrollState(page) {
  return page.evaluate(() => {
    const st = [...(window.__hive_state?.terms?.values() || [])][0];
    const buf = st?.term?.buffer?.active;
    if (!buf) return null;
    return { viewportY: buf.viewportY, baseY: buf.baseY, type: buf.type };
  });
}

function traceTags(page, tag) {
  return page.evaluate((t) => (window.__hive_scrolltrace || []).filter((e) => e.tag === t).length, tag);
}

test('single full-buffer session: no transient viewport-jump on threshold-crossing resizes', async ({ page }) => {
  await bootWithTerm(page);
  // Flood well past the 5000-line cap so cap-trim + bottom-follow loss is armed.
  await page.keyboard.type(
    `awk 'BEGIN{for(j=0;j<60000;j++) printf "HIVE_SCROLL_%06d ................................................\\n", j}'; echo HIVE_PUMP_DONE\n`,
  );
  await expect.poll(async () => (await scrollState(page))?.baseY ?? 0,
    { timeout: 30000, intervals: [200, 400] }).toBeGreaterThan(4500);

  // The user has NOT scrolled — they are following the bottom. Fire spaced
  // threshold-crossing resizes while the flood is still parsing, and sample
  // the viewport gap throughout: a follower must stay pinned to the bottom
  // across the WHOLE restream, not just end there. The gap (baseY-viewportY)
  // strands wide-open on the buggy build (the viewport sits mid-history for
  // ~1s until replay-done re-snaps it) and stays ~0 once the restream re-pin
  // holds it down.
  const widths = [780, 1240, 820, 1200, 900, 1100];
  let maxGapWhileFollowing = 0;
  let strandedSamples = 0;
  for (const w of widths) {
    await page.setViewportSize({ width: w, height: 640 });
    // Sample a few times across each resize's replay window.
    for (let i = 0; i < 4; i++) {
      await page.waitForTimeout(90);
      const s = await page.evaluate(() => {
        const st = [...(window.__hive_state?.terms?.values() || [])][0];
        const buf = st?.term?.buffer?.active;
        if (!buf) return null;
        return { gap: buf.baseY - buf.viewportY, baseY: buf.baseY, following: st._followBottom };
      });
      // Only meaningful once the buffer has real scrollback and the user is
      // still a follower (never scrolled).
      if (s && s.following && s.baseY > 1000) {
        maxGapWhileFollowing = Math.max(maxGapWhileFollowing, s.gap);
        if (s.gap > 100) strandedSamples++;
      }
    }
  }
  const replays = await traceTags(page, 'replay-request');
  expect(replays, 'no replays fired — test is vacuous').toBeGreaterThan(0);
  // The invariant: a follower is never left stranded up in history while a
  // resize replay restreams. A brief one-frame reset blip is tolerable; a
  // sustained strand (multiple samples off-bottom) is the bug.
  expect(strandedSamples, `follower stranded mid-history for ${strandedSamples} samples (maxGap=${maxGapWhileFollowing})`).toBeLessThanOrEqual(1);

  // And it must settle AT the bottom. Poll (not a fixed wait): a 60k-line
  // replay re-parse can outlast any fixed sleep on a slow runner — matching
  // the convergence pattern the sibling scroll-codex specs use.
  await expect.poll(async () => page.evaluate(() => {
    const st = [...(window.__hive_state?.terms?.values() || [])][0];
    const buf = st?.term?.buffer?.active;
    return buf ? buf.baseY - buf.viewportY : null;
  }), { timeout: 12000, intervals: [250, 500] }).toBe(0);
});

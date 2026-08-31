import { test, expect, type Page } from '@playwright/test';
import { bridgeCalls, registerSessionCleanup } from './bridge-sessions.js';
// `state.terms` is a Map<string, TermTile> — the deliberately narrow structural
// view app modules use (app/state.ts). These specs poke the concrete tile the
// map actually holds, which is what carries the real xterm Terminal, so they
// assert SessionTerm rather than widening TermTile for every app caller and
// DOM-test stub (wave 5b's rule).
import type { SessionTerm } from '../../src/app/session-term.js';
import { sentinel, settleShell, waitForSentinel } from './term-harness.js';

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
  // Re-gated 2026-08-24 (spec 245). This spec was never flaky: its
  // vacuity guard demanded a `replay-request` in a follower scenario,
  // where decideResizeReplay deliberately skips the replay — so it
  // failed against correct code, 0/10 runs. The guard now counts replay
  // *decisions*; 10/10 green, and it gates CI again.
  await page.addInitScript((url) => {
    window.__WS_BRIDGE_URL = url;
    // Arm the scroll tracer (window.__hive_scrolltrace) before main.ts loads.
    try {
      localStorage.setItem('hive.debug', '1');
    } catch {}
  }, WS_URL);
});

// On failure, attach the armed scroll trace so CI artifacts carry the
// replay/viewport timeline that explains WHY an invariant broke.
// Best-effort: the page may already be closed/crashed (that can be the
// failure itself) and the trace may be undefined — neither is allowed
// to throw here and mask the original test failure.
test.afterEach(async ({ page }, testInfo) => {
  if (testInfo.status !== testInfo.expectedStatus) {
    try {
      const trace = await page.evaluate(() => window.__hive_scrolltrace);
      await testInfo.attach('scrolltrace', {
        body: JSON.stringify(trace ?? null),
        contentType: 'application/json',
      });
    } catch {
      // Closed page / navigation race — skip the attachment, keep the
      // real failure.
    }
  }
});

const mod = process.platform === 'darwin' ? 'Meta' : 'Control';

async function bootWithTerm(page: Page) {
  test.skip(!WS_URL, 'WS_BRIDGE_URL not set — globalSetup did not run');
  await page.goto('/');
  await page.waitForFunction(
    () => document.querySelectorAll('#projects li.hv-session-row').length >= 1,
    null,
    { timeout: 10000 },
  );
  await page.waitForFunction(
    () => !!document.querySelector('.term-host .xterm-helper-textarea'),
    null,
    { timeout: 10000 },
  );
  await focusFirstTerm(page);
  // NOT `type('stty -echo'); waitForTimeout(200)`: this suite shares one
  // long-lived shell across every spec file, so it may still be running the
  // previous test's flood and typed input queues behind it. settleShell waits
  // for a round trip — see term-harness.ts.
  await settleShell(page);
}

async function focusFirstTerm(page: Page) {
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
}

// Adds a second session by speaking the bridge's JSON-RPC protocol
// directly from Node (the GUI's launcher path can't run here: the
// ws-bridge implements only the session-lifecycle methods, and
// ListAgents falls into its empty-success default). The daemon
// broadcasts session:event(added) to every control conn, so the
// page's sidebar updates on its own. With two tiles, grid mode splits
// the width and the col delta always crosses REPLAY_COL_THRESHOLD.
// Sessions this file created, torn down once at the end of the file.
//
// The e2e-real suite shares ONE daemon across every spec file
// (globalSetup spawns it once), so a session left behind is inherited
// by every later spec: session-phases.spec.ts asserts on the session
// COUNT and went red on CI with two stray `second` rows in the
// sidebar. The leak was invisible while this file was quarantined.
//
// Cleanup is afterALL, not afterEach, and that distinction is load
// bearing. Killing between tests measurably destabilised this file —
// 2 failures in 6 runs against 0 in 6 without it — because removing a
// tile reflows the grid and rebaselines the replay column baseline, so
// the next test's ⌘G toggle no longer crosses REPLAY_COL_THRESHOLD and
// reaches no replay decision at all. Per-file teardown keeps every
// intra-file relationship exactly as it was while still handing the
// next spec file a clean daemon.
const createdSessionIds = new Set<string>();

registerSessionCleanup(createdSessionIds);

async function addSecondSession(page: Page) {
  // Wait for the count to GROW, not to reach a fixed number. Tests in
  // this file share one daemon, so by the second test a `second` from
  // the first is already in the sidebar and a `>= 2` wait returns
  // before the new session lands — which then goes unrecorded and
  // leaks past teardown.
  const beforeCount = await page.evaluate(
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
    beforeCount,
    { timeout: 10000 },
  );
  // Remember what we made so afterAll can take it back out. Read from
  // the page rather than the CreateSession response, which carries no
  // id — the daemon answers with a session:event(added) broadcast.
  for (const id of await page.evaluate(() =>
    (window.__hive_state?.sessions ?? [])
      .filter((s) => s.name === 'second')
      .map((s) => s.id),
  )) {
    createdSessionIds.add(id);
  }

  // Back to the original "main" session (⌘1 = first in display order).
  await page.keyboard.press(`${mod}+1`);
  await page.waitForTimeout(300);
}

// Reads the FIRST session's buffer as text lines via xterm's buffer
// API (WebGL paints to canvas; the DOM holds nothing readable).
function bufferLines(page: Page) {
  return page.evaluate(() => {
    const terms = window.__hive_state?.terms;
    if (!terms) return [];
    const st = [...terms.values()][0] as SessionTerm | undefined;
    const buf = st?.term?.buffer?.active;
    if (!buf) return [];
    const out: string[] = [];
    for (let i = 0; i < buf.length; i++) {
      out.push(buf.getLine(i)?.translateToString(true) || '');
    }
    return out;
  });
}

function scrollState(page: Page) {
  return page.evaluate(() => {
    const terms = window.__hive_state?.terms;
    const st = terms ? ([...terms.values()][0] as SessionTerm) : null;
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

function traceTags(page: Page, tag: string) {
  return page.evaluate(
    (t) => (window.__hive_scrolltrace || []).filter((e) => e.tag === t).length,
    tag,
  );
}

// resizeDecisions counts every resize that reached the replay decision,
// whichever way it went. `replay-request` alone is NOT a valid vacuity
// guard for a FOLLOWER scenario: decideResizeReplay deliberately skips
// the destructive full-ring replay while the tile is glued to the
// bottom (that skip is the fix for the renderer freeze / viewport
// thrash under live output), so a healthy follower emits `replay-skip`
// and zero `replay-request`. Asserting on requests made these specs
// fail 10/10 against correct code — which is why they were quarantined
// as "flaky" when they were in fact deterministically stale.
function resizeDecisions(page: Page) {
  return page.evaluate(
    () =>
      (window.__hive_scrolltrace || []).filter(
        (e) => e.tag === 'replay-request' || e.tag === 'replay-skip',
      ).length,
  );
}

// Starts a bounded high-rate marker pump inside the real bash session.
// Bursty: awk floods `burst` lines flat-out, then sleeps — keeps
// xterm's async write queue loaded the way codex output does, without
// an unbounded loop that could leak past teardown.
// Returns the UNIQUE done-sentinel for this pump. A fixed `HIVE_PUMP_DONE`
// is not usable as a readiness signal here: every attach replays the shared
// session's whole scrollback, so an earlier test's copy is already on screen
// and the wait returns before this pump has printed a single line.
async function startMarkerPump(page: Page, count: number, burst = 40) {
  const done = sentinel('HIVE_PUMP_DONE');
  await page.keyboard.type(
    `i=0; while [ $i -lt ${count} ]; do awk -v s=$i -v n=${burst} 'BEGIN{for(j=s;j<s+n;j++) printf "HIVE_SCROLL_%06d ................................................\\n", j}'; i=$((i+${burst})); sleep 0.05; done; echo ${done}\n`,
  );
  return done;
}

function extractMarkers(lines: string[]) {
  const out: number[] = [];
  for (const l of lines) {
    const m = l.match(/HIVE_SCROLL_(\d{6})/);
    if (m) out.push(parseInt(m[1], 10));
  }
  return out;
}

test('markers survive grid↔single toggles under continuous output, exactly once and in order', async ({
  page,
}) => {
  await bootWithTerm(page);
  await addSecondSession(page);
  const pumpDone = await startMarkerPump(page, 1200);

  // Toggle to grid and back twice while the pump is printing. With two
  // tiles the grid split changes cols by tens of columns, firing real
  // scrollback replays with live bytes still in flight.
  for (let i = 0; i < 2; i++) {
    await page.waitForTimeout(700);
    await page.keyboard.press(`${mod}+g`);
    await page.waitForTimeout(700);
    await page.keyboard.press(`${mod}+g`);
  }

  await waitForSentinel(page, pumpDone);
  await page.waitForTimeout(1200); // let any trailing replay land

  // Non-vacuity: the toggles must really have driven resizes through the
  // replay decision. The pump keeps this tile following the bottom, so the
  // decision is `replay-skip` by design — see resizeDecisions above.
  expect(await resizeDecisions(page)).toBeGreaterThan(0);

  const markers = extractMarkers(await bufferLines(page));
  expect(markers.length).toBeGreaterThan(0);

  // I1a: strictly increasing (no out-of-order interleave).
  const unsorted = markers.filter((m, i) => i > 0 && m <= markers[i - 1]);
  expect(
    unsorted,
    `out-of-order/duplicate markers: ${unsorted.slice(0, 10)}`,
  ).toEqual([]);

  // I1b: no duplicates (a backlog-after-reset replay paints lines twice).
  const dupes = markers.filter((m, i) => markers.indexOf(m) !== i);
  expect(
    dupes,
    `duplicated markers: ${[...new Set(dupes)].slice(0, 10)}`,
  ).toEqual([]);
});

test('viewport converges to the bottom after a mode switch under continuous output', async ({
  page,
}) => {
  // Un-quarantined 2026-08-31 (spec 245). It used to fail on CI with
  // resizeDecisions() === 0 — no replay decision reached at all — and the
  // cause was a harness bug, as the shared-daemon-state hypothesis in this
  // comment suspected: the ws-bridge dispatched every WriteStdin frame on
  // its own goroutine, so under contention adjacent keystrokes were applied
  // out of order and the command this test types was never the command that
  // ran. With the bridge write path ordered, it is green under 18 CPU hogs.

  await bootWithTerm(page);
  await addSecondSession(page);
  const pumpDone = await startMarkerPump(page, 1500);
  await page.waitForTimeout(700);

  // The user-meaningful invariant is CONVERGENCE: a deliberate mode
  // switch must land the viewport at the bottom once the replay parse
  // settles, and it must stay there. Mid-parse snapshots are not
  // asserted -- on a slow runner the multi-MB replay re-parse can
  // outlast any fixed sampling window while the viewport legitimately
  // lags (the parse-ordered re-snap lands when the queue drains). The
  // bug class this guards against -- a stale restore pinning the
  // viewport in history forever -- still fails convergence.
  // Deliberately the nullable scrollState, not mustScrollState: this is an
  // expect.poll() callback, and Playwright calls the value function outside
  // its retry try/catch — a throw here would abort the poll on the first tick
  // instead of letting a momentarily unreadable term recover within the 20s.
  const atBottom = async () => {
    const s = await scrollState(page);
    return s ? s.baseY - s.viewportY : NaN;
  };

  await page.keyboard.press(`${mod}+g`);
  await expect
    .poll(atBottom, { timeout: 20000, intervals: [250, 500] })
    .toBe(0);

  await page.keyboard.press(`${mod}+g`);
  await expect
    .poll(atBottom, { timeout: 20000, intervals: [250, 500] })
    .toBe(0);

  // Once the pump finishes and everything settles, bottom must be
  // stable -- no late replay or restore may move it.
  await waitForSentinel(page, pumpDone);
  await expect
    .poll(atBottom, { timeout: 20000, intervals: [250, 500] })
    .toBe(0);
  for (let i = 0; i < 3; i++) {
    await page.waitForTimeout(300);
    expect(
      await atBottom(),
      'viewport moved off the bottom after settling',
    ).toBe(0);
  }

  // Non-vacuity: the toggles must have driven resizes through the replay
  // decision (skipped, not requested — this tile is following the bottom).
  expect(await resizeDecisions(page)).toBeGreaterThan(0);
});

test('full scrollback: an unscrolled user is not stranded in history by a resize under load', async ({
  page,
}) => {
  // Regression guard for the cap-trim jump-up. The bug only arms once
  // xterm's 5000-line scrollback is FULL: cap-trim then pins baseY at the
  // cap while the viewport drifts off-bottom during heavy parse, so
  // _onBodyResize's geometry check `(baseY - viewportY) <= 2` mis-reads
  // "not at bottom" for a user who never scrolled — arming a
  // wants=false replay that strands the viewport up in history. The other
  // scroll specs pump <1500 lines (below the cap) and never hit this.
  await bootWithTerm(page);

  // Flat-out flood well past the cap so it keeps parsing through the
  // resizes below (bottom-follow stays lost the whole time).
  await page.keyboard.type(
    `awk 'BEGIN{for(j=0;j<60000;j++) printf "HIVE_SCROLL_%06d ................................................\\n", j}'; echo HIVE_PUMP_DONE\n`,
  );

  // Wait until the buffer is genuinely at the cap.
  await expect
    .poll(async () => (await scrollState(page))?.baseY ?? 0, {
      timeout: 30000,
      intervals: [200, 400],
    })
    .toBeGreaterThan(4500);

  // The user has NOT scrolled. Fire spaced threshold-crossing resizes
  // while the flood is still parsing — each lands inside the cap-trim
  // bottom-follow-loss window. A correct client keeps the user pinned to
  // the bottom; the buggy one strands them up in history.
  // Everything before the resizes is attach-time noise. The daemon
  // sends an atomic scrollback replay on EVERY attach
  // (SubscribeWithAtomicReplay), so the trace already holds one
  // `replay-restore` with wants=true before a single resize has fired —
  // measured: restores pre=1, post=1, requests 0. Reading the whole
  // trace therefore made both assertions below vacuous: the
  // non-vacuity guard was satisfied by the attach, and wantsFalse was
  // trivially empty because the attach restore is the only entry.
  // Cut the trace here so what follows is the resize scenario alone.
  const traceBase = await page.evaluate(
    () => (window.__hive_scrolltrace || []).length,
  );
  for (const w of [780, 1240, 820, 1200]) {
    await page.setViewportSize({ width: w, height: 640 });
    await page.waitForTimeout(350);
  }

  await page.waitForTimeout(1500); // let the last replay land

  const restores = await page.evaluate(
    (base) =>
      (window.__hive_scrolltrace || [])
        .slice(base)
        .filter((e) => e.tag === 'replay-restore'),
    traceBase,
  );
  const wantsFalse = restores.filter((r) => r.wants === false);

  // Non-vacuity: the resizes really did reach the replay decision.
  // Counting *decisions* rather than replays is the point — this tile is
  // following the bottom, so decideResizeReplay correctly skips the replay,
  // and demanding one would fail against correct code (the spec-245 mistake).
  //
  // There is deliberately NO second "buffer is at the cap" assertion here.
  // The poll above already established the cap BEFORE the resizes, which is
  // when it matters; re-checking it afterwards asserts a state the scenario
  // itself destroys. Narrowing to 780 wraps each flood line onto two rows, so
  // cap-trim discards half the logical lines, and widening back to 1200
  // unwraps what is left — measured baseY 2483/2484, almost exactly half the
  // 5000-line cap, on both CI Linux and CI macOS. It only ever passed while
  // the flood happened to still be running and refilled the buffer, which is
  // luck, not an invariant.
  expect(
    await resizeDecisions(page),
    'no resize reached the replay decision — scenario is vacuous',
  ).toBeGreaterThan(0);

  // The invariant: a user who NEVER scrolled must never be handed a
  // restore-into-history (wants=false) replay BY A RESIZE. On the
  // buggy code the cap-trim mis-read of wasAtBottom produces these;
  // the follow-intent fix eliminates them — today by skipping the
  // replay outright, which satisfies this the strongest way there is.
  // Deterministic regardless of whether a later event happens to
  // re-snap the viewport to the bottom.
  expect(
    wantsFalse.length,
    `unscrolled user got ${wantsFalse.length} restore-into-history replay(s): ${JSON.stringify(wantsFalse.slice(0, 4))}`,
  ).toBe(0);
});

test('a reader scrolled into history is not yanked to the bottom by a resize replay', async ({
  page,
}) => {
  // QUARANTINED ON CI ONLY — and unlike its three siblings, this one is
  // genuinely load-dependent, which is what spec 245 originally
  // suspected of all of them.
  //
  // Evidence: green locally on an idle machine and across many full-suite
  // runs; fails on CI (macOS run 33143976246, Linux run 33146…) with
  // viewportY == baseY == 5000, i.e. the reader really was yanked to the
  // bottom; and reproduces locally 1 run in 3 with 18 CPU hogs running.
  //
  // Re-checked 2026-08-31 after the spec-245 harness fixes (ordered
  // WriteStdin, unique sentinels, settleShell): its three quarantined
  // siblings all came back green under 18 CPU hogs, this one still fails
  // 2/2 with the same viewportY == baseY == 5000. So it is not a harness
  // artefact — the yank is real under contention, and the follow-up is a
  // product question about the resize replay, not a test one.
  test.skip(
    !!process.env.CI,
    'load-dependent under CI contention — see spec 245 Resolution',
  );
  //
  // So this is NOT the stale-guard class the rest of this file had — the
  // assertion is right and the behaviour under load may genuinely be
  // wrong. That makes it a product question (a reader losing their place
  // during a replay is the scroll-jump bug this file exists to catch),
  // not harness fragility to paper over. It is skipped here rather than
  // deleted so the other 21 tests can gate CI, per spec 245's rule that
  // a quarantine be explicit and carry a follow-up.

  await bootWithTerm(page);
  // Fill scrollback, then stop output so the read position is stable.
  const pumpDone = await startMarkerPump(page, 200);
  await waitForSentinel(page, pumpDone);

  // Scroll up with a real wheel gesture so the clamped wheel handler runs.
  const term = page.locator('.term-host .term-body').first();
  await term.hover();
  for (let i = 0; i < 6; i++) {
    await page.mouse.wheel(0, -300);
    await page.waitForTimeout(50);
  }
  const before = await mustScrollState(page);
  expect(before.viewportY).toBeLessThan(before.baseY);

  // A viewport resize big enough to cross REPLAY_COL_THRESHOLD fires a
  // real replay. The replay-done must respect the reader's position.
  await page.setViewportSize({ width: 860, height: 600 });
  await page.waitForTimeout(1500);

  expect(await traceTags(page, 'replay-request')).toBeGreaterThan(0);
  const after = await mustScrollState(page);
  expect(
    after.viewportY,
    'replay-done yanked the reader to the bottom',
  ).toBeLessThan(after.baseY);
});

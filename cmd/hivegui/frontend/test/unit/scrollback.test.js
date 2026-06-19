import { describe, it, expect, vi } from 'vitest';
import {
  shouldRequestReplay,
  decideResizeReplay,
  REPLAY_COL_THRESHOLD,
  REPLAY_DEBOUNCE_MS,
  handleScrollbackEvent,
  applyRebaseline,
  abandonReplays,
} from '../../src/lib/scrollback.js';

describe('decideResizeReplay — alt-screen replay skip (freeze fix)', () => {
  it('skips the replay on the alternate screen', () => {
    expect(decideResizeReplay({ bufferType: 'alternate', cols: 100, baselineCols: 232 }).replay)
      .toBe(false);
  });

  it('does NOT advance the baseline when skipping (so alt→normal still corrects)', () => {
    // The bug this guards: advancing the baseline on a skipped alt-screen
    // resize would let a later normal-buffer resize fall under the threshold,
    // leaving normal scrollback wrapped at the old width forever.
    expect(decideResizeReplay({ bufferType: 'alternate', cols: 100, baselineCols: 232 }).baseline)
      .toBe(232);
  });

  it('replays and advances the baseline on the normal screen', () => {
    expect(decideResizeReplay({ bufferType: 'normal', cols: 100, baselineCols: 232 }))
      .toEqual({ replay: true, baseline: 100 });
  });
});

describe('shouldRequestReplay', () => {
  it('returns true on grid → single (large widen)', () => {
    expect(shouldRequestReplay(40, 200)).toBe(true);
  });

  it('returns true on single → grid (large shrink)', () => {
    expect(shouldRequestReplay(200, 40)).toBe(true);
  });

  it('returns true exactly at the threshold', () => {
    expect(shouldRequestReplay(80, 80 + REPLAY_COL_THRESHOLD)).toBe(true);
    expect(shouldRequestReplay(80, 80 - REPLAY_COL_THRESHOLD)).toBe(true);
  });

  it('returns false below the threshold (kerning jitter)', () => {
    expect(shouldRequestReplay(80, 81)).toBe(false);
    expect(shouldRequestReplay(80, 83)).toBe(false);
    expect(shouldRequestReplay(80, 80)).toBe(false);
  });

  it('returns false when prevCols is missing (first measurement)', () => {
    expect(shouldRequestReplay(undefined, 80)).toBe(false);
    expect(shouldRequestReplay(0, 80)).toBe(false);
  });

  it('returns false when nextCols is zero (hidden tile)', () => {
    expect(shouldRequestReplay(80, 0)).toBe(false);
  });

  it('accepts a custom threshold', () => {
    expect(shouldRequestReplay(80, 82, 2)).toBe(true);
    expect(shouldRequestReplay(80, 81, 2)).toBe(false);
  });
});

describe('debounce timing constant', () => {
  it('is small enough for live use', () => {
    expect(REPLAY_DEBOUNCE_MS).toBeGreaterThan(0);
    expect(REPLAY_DEBOUNCE_MS).toBeLessThan(1000);
  });
});

describe('handleScrollbackEvent', () => {
  // Mock term with an xterm-like async write queue: write(data, cb)
  // enqueues; flush() "parses" entries in order, firing callbacks.
  // This models the property the handler now depends on — reset and
  // viewport placement are parse-ordered, not event-ordered.
  function makeSt({ baseY = 0, viewportY = 0 } = {}) {
    const queue = [];
    const order = [];
    // Shared buffer object so scrollToLine can mutate viewportY
    // from within the term object literal (where `st` isn't in scope yet).
    const buf = { active: { baseY, viewportY } };
    const term = {
      buffer: buf,
      reset: vi.fn(() => order.push('reset')),
      scrollToBottom: vi.fn(() => order.push('scrollToBottom')),
      scrollToLine: vi.fn((n) => {
        order.push(`scrollToLine:${n}`);
        // xterm's scrollToLine sets viewportY synchronously
        buf.active.viewportY = n;
      }),
      write: vi.fn((data, cb) => {
        queue.push({ data, cb });
        if (data) order.push(`parse:${data}`);
      }),
    };
    const flush = () => {
      while (queue.length) {
        const entry = queue.shift();
        // A real parser would consume entry.data here; our `order`
        // log records data entries at enqueue time which is fine for
        // relative ordering because flush preserves queue order.
        entry.cb?.();
      }
    };
    return { st: { term, decoder: new TextDecoder('utf-8') }, flush, order, queue };
  }

  it('begin refreshes the decoder immediately (decode order is event order)', () => {
    const { st } = makeSt();
    const beforeDecoder = st.decoder;
    expect(handleScrollbackEvent(st, 'scrollback_replay_begin')).toBe(true);
    expect(st.decoder).not.toBe(beforeDecoder);
  });

  it('begin resets parse-ordered, not synchronously — backlog cannot repaint after the wipe', () => {
    const { st, flush } = makeSt();
    // Simulate codex-rate backlog already sitting in the queue.
    st.term.write('backlog-bytes');
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    // Replay bytes arrive after begin.
    st.term.write('replay-bytes');
    // Event time: nothing reset yet (queue not parsed).
    expect(st.term.reset).not.toHaveBeenCalled();
    flush();
    expect(st.term.reset).toHaveBeenCalledTimes(1);
    // The reset callback was enqueued after the backlog and before the
    // replay bytes: backlog parses, THEN reset, THEN replay paints.
    const calls = st.term.write.mock.calls.map((c) => c[0]);
    expect(calls).toEqual(['backlog-bytes', '', 'replay-bytes']);
  });

  it('begin captures the reader distance from bottom — at parse time, not event time', () => {
    const { st, flush } = makeSt({ baseY: 100, viewportY: 60 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    expect(st._replayPrevFromBottom).toBeUndefined(); // parse-ordered like the reset
    flush();
    expect(st._replayPrevFromBottom).toBe(40);
  });

  it('tracks in-flight replays: begin increments at event time, done decrements at parse time', () => {
    // Drives the SessionTerm onScroll re-pin that keeps a follower glued to
    // the bottom for the whole restream. The decrement must be parse-ordered
    // (in finish), so the pin holds until the restream is fully parsed.
    const { st, flush } = makeSt({ baseY: 50, viewportY: 50 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    expect(st._replaysInFlight).toBe(1);
    handleScrollbackEvent(st, 'scrollback_replay_done');
    expect(st._replaysInFlight, 'still in flight until the restream parses').toBe(1);
    flush();
    expect(st._replaysInFlight).toBe(0);
  });

  it('overlapping replays: counter holds >0 until the LAST done parses, never negative', () => {
    const { st, flush } = makeSt({ baseY: 50, viewportY: 50 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    expect(st._replaysInFlight).toBe(2);
    handleScrollbackEvent(st, 'scrollback_replay_done');
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st._replaysInFlight).toBe(0);
  });

  it('abandonReplays clears the in-flight count when a begin never gets its done', () => {
    // Disconnect / reattach / revival wipe the buffer without a replay-done;
    // without this the counter leaks >0 and pins the viewport forever.
    const { st } = makeSt({ baseY: 50, viewportY: 50 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    expect(st._replaysInFlight).toBe(1);
    abandonReplays(st);
    expect(st._replaysInFlight).toBe(0);
    expect(() => abandonReplays(null)).not.toThrow();
  });

  it('capture is parse-ordered: backlog parsed before the wipe counts toward the distance', () => {
    const { st, flush } = makeSt({ baseY: 100, viewportY: 60 });
    // Codex-rate backlog already queued at begin time; parsing it adds
    // 10 lines while the scrolled-up reader's viewportY stays put.
    st.term.write('backlog-bytes', () => { st.term.buffer.active.baseY = 110; });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    st._replayWantsBottom = false;
    // The replay re-streams everything, backlog included.
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 115; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    // d = 110 - 60 = 50 (backlog included); restore = 115 - 50 = 65.
    // An event-time capture would have measured 40 and restored to 75
    // — a backlog's-worth of lines below the reader's content.
    expect(st.term.scrollToLine).toHaveBeenCalledWith(65);
  });

  it('done snaps to bottom by default — after the queue is parsed', () => {
    const { st, flush } = makeSt();
    st._followBottom = true; // replay-done checks _followBottom
    expect(handleScrollbackEvent(st, 'scrollback_replay_done')).toBe(true);
    expect(st.term.scrollToBottom).not.toHaveBeenCalled(); // not at event time
    flush();
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(st.term.reset).not.toHaveBeenCalled();
  });

  it('done snaps when _replayWantsBottom === true and clears the flag', () => {
    const { st, flush } = makeSt();
    st._followBottom = true; // replay-done checks _followBottom
    st._replayWantsBottom = true;
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(st._replayWantsBottom).toBeUndefined();
  });

  it('done restores the reading position when _replayWantsBottom === false', () => {
    const { st, flush } = makeSt({ baseY: 100, viewportY: 60 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    st._replayWantsBottom = false;
    // The replay bytes parse after the reset and rebuild the buffer
    // with a new baseY — modeled as a queued write whose "parse"
    // bumps baseY, so the capture (queued at begin) still sees 100.
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 120; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).not.toHaveBeenCalled();
    expect(st.term.scrollToLine).toHaveBeenCalledWith(80); // 120 - 40
    expect(st._replayWantsBottom).toBeUndefined();
    expect(st._replayPrevFromBottom).toBeUndefined();
  });

  it('restore target clamps at 0 when history shrank', () => {
    const { st, flush } = makeSt({ baseY: 50, viewportY: 0 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    st._replayWantsBottom = false;
    // Rebuilt buffer is much shorter than the captured distance (50).
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 10; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToLine).toHaveBeenCalledWith(0);
  });

  it('falls back to synchronous behavior when term has no write()', () => {
    const st = {
      term: { reset: vi.fn(), scrollToBottom: vi.fn() },
      decoder: new TextDecoder('utf-8'),
    };
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    expect(st.term.reset).toHaveBeenCalledTimes(1);
    st._followBottom = true; // replay-done checks _followBottom
    handleScrollbackEvent(st, 'scrollback_replay_done');
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
  });

  it('unknown event kinds are no-ops', () => {
    const { st } = makeSt();
    expect(handleScrollbackEvent(st, 'something_else')).toBe(false);
    expect(st.term.reset).not.toHaveBeenCalled();
    expect(st.term.scrollToBottom).not.toHaveBeenCalled();
  });

  it('null / undefined st is a no-op (no throw)', () => {
    expect(handleScrollbackEvent(null, 'scrollback_replay_begin')).toBe(false);
    expect(handleScrollbackEvent(undefined, 'scrollback_replay_begin')).toBe(false);
    expect(handleScrollbackEvent({}, 'scrollback_replay_begin')).toBe(false);
  });

  // _followBottom is the cap-trim fix's source of truth for "is the user
  // at the bottom?" in _onBodyResize. A replay-done must respect it —
  // this is the fix for the scroll-jump bug: the replay-done handler
  // must not override the user's scroll intent by snapping to bottom.
  it('done with wants=true AND _followBottom=true snaps to bottom', () => {
    const { st, flush } = makeSt();
    st._followBottom = true; // user was following
    st._replayWantsBottom = true;
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(st._followBottom).toBe(true);
  });

  it('done with wants=true BUT _followBottom=false does NOT snap (scroll-jump fix)', () => {
    const { st, flush } = makeSt();
    st._followBottom = false; // user had scrolled up
    st._replayWantsBottom = true; // replay wants bottom, but user intent wins
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).not.toHaveBeenCalled();
    // _followBottom stays false — user was not following, restore path ran
    expect(st._followBottom).toBe(false);
  });

  it('mid-replay scroll: fromBottom=0 with _followBottom=false does NOT restore to bottom', () => {
    // User was at bottom at replay start (baseY === viewportY → fromBottom=0),
    // scrolled up mid-replay (_followBottom=false), replay finishes. The
    // restore path must NOT call scrollToLine(baseY) which would yank them
    // back — that's the scroll-jump we are fixing.
    // Use baseY === viewportY so the begin handler's capture reads 0.
    const { st, flush } = makeSt({ baseY: 50, viewportY: 50 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    // Capture reads 50 - 50 = 0 → fromBottom = 0
    st._followBottom = false; // user scrolled up during replay
    st._replayWantsBottom = true;
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 100; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    // fromBottom is 0, so scrollToLine is skipped — viewport stays put
    expect(st.term.scrollToLine).not.toHaveBeenCalled();
    // _followBottom stays false
    expect(st._followBottom).toBe(false);
  });

  it('done with wants=false un-follows ONLY when a restore actually ran', () => {
    const { st, flush } = makeSt({ baseY: 100, viewportY: 60 });
    handleScrollbackEvent(st, 'scrollback_replay_begin'); // captures fromBottom
    st._followBottom = true;
    st._replayWantsBottom = false;
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 120; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToLine).toHaveBeenCalledWith(80); // restore ran
    expect(st._followBottom).toBe(false);
  });

  it('done with wants=false does NOT un-follow when no restore ran (guards stranding)', () => {
    // wants=false but no begin/capture → _replayPrevFromBottom undefined →
    // scrollToLine is skipped. _followBottom must be left untouched: a user
    // who scrolled back to the bottom while a stale replay was in flight
    // must not be armed for a phantom restore-into-history on the next resize.
    const { st, flush } = makeSt({ baseY: 100, viewportY: 100 });
    st._followBottom = true;
    st._replayWantsBottom = false;
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToLine).not.toHaveBeenCalled();
    expect(st._followBottom).toBe(true);
  });
});

describe('applyRebaseline clears stale restore intent (the pair)', () => {
  it('deletes _replayWantsBottom so a later replay-done does not read stale false', () => {
    const cleared = vi.fn();
    const st = {
      term: { cols: 120 },
      _replayBaselineCols: 80,
      _replayTimer: 42,
      _replayWantsBottom: false,
    };
    applyRebaseline(st, cleared);
    expect(cleared).toHaveBeenCalledWith(42);
    expect(st._replayBaselineCols).toBe(120);
    expect(st._replayTimer).toBe(0);
    expect(st._replayWantsBottom).toBeUndefined();
  });

  it('deletes _replayPrevFromBottom so a latched done cannot restore a stale position', () => {
    const st = {
      term: { cols: 120 },
      _replayBaselineCols: 80,
      _replayWantsBottom: false,
      _replayPrevFromBottom: 40,
    };
    applyRebaseline(st, () => {});
    expect(st._replayWantsBottom).toBeUndefined();
    expect(st._replayPrevFromBottom).toBeUndefined();
  });

  it('is a no-op on the intent pair when both were unset', () => {
    const st = { term: { cols: 100 }, _replayBaselineCols: 80 };
    applyRebaseline(st, () => {});
    expect(st._replayWantsBottom).toBeUndefined();
    expect(st._replayPrevFromBottom).toBeUndefined();
  });
});

describe('shouldRequestReplay against baseline (debounce edge case)', () => {
  // Simulates main.js's baseline-relative debounce: compare *current*
  // cols against baseline-at-last-replay (not just-previous
  // measurement). The reviewer flagged r614 — 80→84→83 should NOT
  // trigger (final delta 3 < threshold 4). Conversely 80→90→89 SHOULD
  // trigger (final delta 9), even though the single 90→89 step is
  // sub-threshold.
  it('baseline-relative threshold catches multi-step crossings', () => {
    const baseline = 80;
    expect(shouldRequestReplay(baseline, 90)).toBe(true);
    expect(shouldRequestReplay(baseline, 89)).toBe(true);
    expect(shouldRequestReplay(baseline, 83)).toBe(false);
    expect(shouldRequestReplay(baseline, 84)).toBe(true);
  });
});

// ---------- scroll-jump regression tests ----------
//
// These tests cover the scroll-jump bug (viewport jumping to 0 during
// replays) and the stale _followBottom bug (viewport not snapping to
// bottom on initial attach when _followBottom was false from a previous
// session). They serve as both documentation of the bugs and regression
// guards — if someone changes the replay-done handler in the future,
// these tests will catch regressions.

describe('scroll-jump bug regression — viewport must not jump to 0 during replays', () => {
  // The original bug: the replay-done handler called scrollToBottom()
  // unconditionally, overriding _followBottom. This caused the viewport
  // to jump to position 0 during replays (resize, attach, etc.) even
  // when the user had scrolled up. The fix: check _followBottom before
  // snapping to bottom.

  it('replay with _followBottom=true snaps to bottom (user was following)', () => {
    // Scenario: user is at bottom, heavy output triggers a resize replay.
    // The replay should snap to bottom — user is following.
    const { st, flush } = makeSt({ baseY: 5000, viewportY: 5000 });
    st._followBottom = true;
    st._replayWantsBottom = true;
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(st._followBottom).toBe(true);
  });

  it('replay with _followBottom=false does NOT snap (user scrolled up)', () => {
    // Scenario: user scrolled up to read history, a resize triggers a
    // replay. The replay must NOT snap to bottom — user intent wins.
    // This is the core scroll-jump fix.
    const { st, flush } = makeSt({ baseY: 5000, viewportY: 4800 });
    st._followBottom = false; // user scrolled up
    st._replayWantsBottom = true; // replay wants bottom
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).not.toHaveBeenCalled();
    // _followBottom stays false — user was not following
    expect(st._followBottom).toBe(false);
  });

  it('replay with _followBottom=false and fromBottom=0 does NOT yank to bottom', () => {
    // Scenario: user was at bottom when replay started (fromBottom=0),
    // scrolled up mid-replay (_followBottom=false), replay finishes.
    // The restore path must NOT call scrollToLine(baseY) which would
    // yank them back to bottom — that's the scroll-jump.
    const { st, flush } = makeSt({ baseY: 5000, viewportY: 5000 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    // Capture reads 5000 - 5000 = 0 → fromBottom = 0
    st._followBottom = false; // scrolled up mid-replay
    st._replayWantsBottom = true;
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 5000; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    // fromBottom is 0, so scrollToLine is skipped — viewport stays put
    expect(st.term.scrollToLine).not.toHaveBeenCalled();
    expect(st._followBottom).toBe(false);
  });

  it('replay with _followBottom=false and fromBottom>0 restores correct position', () => {
    // Scenario: user scrolled up BEFORE the replay started (fromBottom>0),
    // replay finishes. The restore should put them back at their position.
    const { st, flush } = makeSt({ baseY: 5000, viewportY: 4700 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    // Capture reads 5000 - 4700 = 300 → fromBottom = 300
    st._followBottom = false; // user scrolled up before replay
    st._replayWantsBottom = false;
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 5000; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    // Restore: target = 5000 - 300 = 4700
    expect(st.term.scrollToLine).toHaveBeenCalledWith(4700);
    // After restore, user is at 4700, distance from bottom = 300 > 2
    expect(st._followBottom).toBe(false);
  });

  it('replay with _followBottom=false and fromBottom>0 snaps to bottom when restore lands there', () => {
    // Scenario: user scrolled up slightly before replay (fromBottom=1),
    // restore lands at bottom (baseY - 1 is at bottom). _followBottom
    // should be set to true after restore.
    const { st, flush } = makeSt({ baseY: 5000, viewportY: 4999 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    // Capture reads 5000 - 4999 = 1 → fromBottom = 1
    st._followBottom = false;
    st._replayWantsBottom = false;
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 5000; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    // Restore: target = 5000 - 1 = 4999
    expect(st.term.scrollToLine).toHaveBeenCalledWith(4999);
    // After restore, viewportY = 4999, baseY = 5000, distance = 1 <= 2
    expect(st._followBottom).toBe(true);
  });
});

describe('stale _followBottom bug — initial attach must snap to bottom', () => {
  // The stale _followBottom bug: when a user scrolled up in a previous
  // session, _followBottom was false when they reopened Hive. The initial
  // attach replay then skipped snap-to-bottom, landing the viewport in
  // the middle instead of the bottom. The fix: reset _followBottom = true
  // in ensureAttached() before OpenSession().
  //
  // These tests verify the scrollback.js side: when _followBottom is
  // true (as it should be after the ensureAttached fix), the replay
  // snaps to bottom.

  it('initial attach replay snaps to bottom when _followBottom=true', () => {
    // This is what happens after the ensureAttached fix sets
    // _followBottom = true before OpenSession().
    const { st, flush } = makeSt({ baseY: 5000, viewportY: 5000 });
    st._followBottom = true; // set by ensureAttached fix
    st._replayWantsBottom = true; // default for initial attach
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(st._followBottom).toBe(true);
  });

  it('initial attach replay with _followBottom=false would NOT snap (regression guard)', () => {
    // This test documents the bug scenario: if _followBottom is false
    // (e.g., ensureAttached fix is missing), the replay would NOT snap.
    // The ensureAttached fix prevents this by setting _followBottom = true.
    const { st, flush } = makeSt({ baseY: 5000, viewportY: 5000 });
    st._followBottom = false; // stale from previous session (bug)
    st._replayWantsBottom = true;
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).not.toHaveBeenCalled();
    // This is the BUG: viewport stays at 0 instead of snapping to bottom.
    // The ensureAttached fix prevents _followBottom from being false here.
    expect(st._followBottom).toBe(false);
  });

  it('resize replay respects user scroll state (not affected by stale _followBottom)', () => {
    // Resize replays should use the user's CURRENT scroll state, not
    // a stale value. If the user scrolled up before a resize, the
    // replay should restore their position.
    const { st, flush } = makeSt({ baseY: 5000, viewportY: 4500 });
    handleScrollbackEvent(st, 'scrollback_replay_begin');
    // Capture reads 5000 - 4500 = 500 → fromBottom = 500
    st._followBottom = false; // user scrolled up before resize
    st._replayWantsBottom = true; // resize replay wants bottom
    st.term.write('replay-bytes', () => { st.term.buffer.active.baseY = 5000; });
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    // _followBottom is false, so no snap — user stays scrolled up
    expect(st.term.scrollToBottom).not.toHaveBeenCalled();
    // fromBottom is 500 > 0, so scrollToLine is called
    expect(st.term.scrollToLine).toHaveBeenCalledWith(4500);
  });
});

describe('scroll-jump: heavy output at scrollback cap', () => {
  // Scenario: heavy agent output at the scrollback cap causes xterm to
  // cap-trim (pin baseY at the cap while viewportY drifts). The re-pin
  // logic in onScroll must keep a FOLLOWING viewport glued to the bottom
  // for the WHOLE replay restream.

  it('replay with _replaysInFlight>0 and _followBottom=true stays at bottom', () => {
    // During a replay, the re-pin logic in onScroll keeps the viewport
    // at bottom when _followBottom is true. The replay-done handler
    // should also snap to bottom.
    const { st, flush } = makeSt({ baseY: 5000, viewportY: 5000 });
    st._followBottom = true;
    st._replaysInFlight = 1; // replay in progress
    st._replayWantsBottom = true;
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(st._replaysInFlight).toBe(0);
    expect(st._followBottom).toBe(true);
  });

  it('replay with _replaysInFlight>0 and _followBottom=false does NOT snap', () => {
    // During a replay, if the user scrolled up (_followBottom=false),
    // the re-pin logic doesn't fire, and the replay-done handler
    // should not snap to bottom either.
    const { st, flush } = makeSt({ baseY: 5000, viewportY: 4800 });
    st._followBottom = false; // user scrolled up during replay
    st._replaysInFlight = 1;
    st._replayWantsBottom = true;
    handleScrollbackEvent(st, 'scrollback_replay_done');
    flush();
    expect(st.term.scrollToBottom).not.toHaveBeenCalled();
    expect(st._replaysInFlight).toBe(0);
    expect(st._followBottom).toBe(false);
  });
});

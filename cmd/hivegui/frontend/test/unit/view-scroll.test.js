import { describe, it, expect, vi } from 'vitest';
import { snapVisibleTermsToBottom } from '../../src/lib/view-scroll.js';
import { handleScrollbackEvent } from '../../src/lib/scrollback.js';

function makeTerm({ attached = true, h = 200 } = {}) {
  return {
    attached,
    body: { clientHeight: h },
    term: { scrollToBottom: vi.fn() },
  };
}

describe('snapVisibleTermsToBottom', () => {
  it('calls scrollToBottom on attached, visible terms', () => {
    const a = makeTerm();
    const b = makeTerm();
    snapVisibleTermsToBottom([a, b]);
    expect(a.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(b.term.scrollToBottom).toHaveBeenCalledTimes(1);
  });

  it('skips detached terms (deferred attach)', () => {
    const t = makeTerm({ attached: false });
    snapVisibleTermsToBottom([t]);
    expect(t.term.scrollToBottom).not.toHaveBeenCalled();
  });

  it('skips zero-height terms (display:none / not laid out)', () => {
    const t = makeTerm({ h: 0 });
    snapVisibleTermsToBottom([t]);
    expect(t.term.scrollToBottom).not.toHaveBeenCalled();
  });

  it('is a no-op on empty / null input', () => {
    expect(() => snapVisibleTermsToBottom([])).not.toThrow();
    expect(() => snapVisibleTermsToBottom(null)).not.toThrow();
    expect(() => snapVisibleTermsToBottom(undefined)).not.toThrow();
  });

  it('handles malformed entries without throwing', () => {
    const good = makeTerm();
    snapVisibleTermsToBottom([null, undefined, {}, { attached: true }, good]);
    expect(good.term.scrollToBottom).toHaveBeenCalledTimes(1);
  });

  it('accepts an iterator (e.g. Map.values())', () => {
    const m = new Map();
    const a = makeTerm();
    const b = makeTerm();
    m.set('a', a);
    m.set('b', b);
    snapVisibleTermsToBottom(m.values());
    expect(a.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(b.term.scrollToBottom).toHaveBeenCalledTimes(1);
  });

  it('overrides stale _replayWantsBottom=false on snapped terms', () => {
    // Mode switches are deliberate "land at bottom" actions. A same-tick
    // _onBodyResize armed by show() may have set wants-bottom=false; the
    // snap must mark the term to honor bottom on the upcoming replay-done.
    const t = makeTerm();
    t._replayWantsBottom = false;
    snapVisibleTermsToBottom([t]);
    expect(t.term.scrollToBottom).toHaveBeenCalledTimes(1);
    expect(t._replayWantsBottom).toBe(true);
  });

  it('does not set _replayWantsBottom on skipped terms', () => {
    const detached = makeTerm({ attached: false });
    const hidden = makeTerm({ h: 0 });
    snapVisibleTermsToBottom([detached, hidden]);
    expect(detached._replayWantsBottom).toBeUndefined();
    expect(hidden._replayWantsBottom).toBeUndefined();
  });

  it('clears a captured restore distance on snapped terms (intent pair)', () => {
    const t = makeTerm();
    t._replayWantsBottom = false;
    t._replayPrevFromBottom = 40;
    snapVisibleTermsToBottom([t]);
    expect(t._replayWantsBottom).toBe(true);
    expect(t._replayPrevFromBottom).toBeUndefined();
  });

  it('re-asserts bottom after the write queue drains (parse-ordered re-snap)', () => {
    // On a slow machine the mode-switch replay's multi-MB re-parse may
    // still be queued when the 250ms mode-snap fires. xterm loses
    // bottom-follow during the heavy parse, so the synchronous
    // scrollToBottom alone leaves the viewport stranded off-bottom.
    // The snap must also enqueue a parse-ordered re-snap that runs
    // only after the queue flushes.
    const queue = [];
    const st = {
      attached: true,
      body: { clientHeight: 200 },
      term: {
        scrollToBottom: vi.fn(),
        write: vi.fn((data, cb) => { queue.push(cb); }),
      },
    };
    // Heavy replay parse still in flight: pending queue entries ahead
    // of whatever the snap enqueues.
    queue.push(() => {}, () => {});

    snapVisibleTermsToBottom([st]);
    // Synchronous snap fires now — instant feedback on fast machines —
    // but the parse-ordered re-snap must NOT have run yet.
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);

    // Queue drains: replay parse completes, then the re-snap runs.
    while (queue.length) queue.shift()?.();
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(2);
    expect(st._replayWantsBottom).toBe(true);
    expect(st._replayPrevFromBottom).toBeUndefined();
  });

  it('snap mid-queue beats an already-latched replay-restore (no scrollToLine)', () => {
    // The replay-done EVENT latches wantsBottom at event time, but its
    // `finish` consumes the captured distance at PARSE time. A mode
    // snap landing between the two (event handled, queue not flushed)
    // can only win by deleting the captured distance — setting
    // wantsBottom=true arrives too late for the latched done. Mock an
    // xterm-like async write queue to pin that ordering down.
    const queue = [];
    const st = {
      attached: true,
      body: { clientHeight: 200 },
      term: {
        buffer: { active: { baseY: 100, viewportY: 100 } },
        reset: vi.fn(),
        scrollToBottom: vi.fn(),
        scrollToLine: vi.fn(),
        write: vi.fn((data, cb) => { queue.push(cb); }),
      },
    };
    const flush = () => { while (queue.length) queue.shift()?.(); };

    // Armed state: reader was scrolled up — wants-bottom false with a
    // captured distance from the begin's (already-parsed) capture.
    st._replayWantsBottom = false;
    st._replayPrevFromBottom = 40;

    // Done EVENT arrives first: latches wantsBottom=false, queues finish.
    handleScrollbackEvent(st, 'scrollback_replay_done');
    // Mode snap fires mid-queue (deliberate user action).
    snapVisibleTermsToBottom([st]);
    expect(st.term.scrollToBottom).toHaveBeenCalledTimes(1);
    // Parse-time finish runs: the snap must stand — captured distance
    // is gone, so no scrollToLine back into history.
    flush();
    expect(st.term.scrollToLine).not.toHaveBeenCalled();
    // The snap's wants-bottom=true survives for the NEXT replay-done
    // (the latched done consumed the false at event time).
    expect(st._replayWantsBottom).toBe(true);
    expect(st._replayPrevFromBottom).toBeUndefined();
  });
});

import { describe, it, expect, vi } from 'vitest';
import { snapVisibleTermsToBottom } from '../../src/lib/view-scroll.js';

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
});

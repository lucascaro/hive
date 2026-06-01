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
});

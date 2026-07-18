import { describe, it, expect } from 'vitest';
import { isShiftEnter, isHelpOverlayKey, NEWLINE_SEQ } from '../../src/lib/keymap.js';

// Minimal fake keydown event with all modifier flags defaulted off.
function ev(overrides = {}) {
  return { metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, key: 'Enter', ...overrides };
}

describe('isShiftEnter', () => {
  it('fires for bare Shift+Enter', () => {
    expect(isShiftEnter(ev({ shiftKey: true }))).toBe(true);
  });

  it('does not fire for plain Enter (no modifier) — preserves submit', () => {
    expect(isShiftEnter(ev())).toBe(false);
  });

  it('does not fire when another modifier is also held', () => {
    expect(isShiftEnter(ev({ shiftKey: true, metaKey: true }))).toBe(false);
    expect(isShiftEnter(ev({ shiftKey: true, ctrlKey: true }))).toBe(false);
    expect(isShiftEnter(ev({ shiftKey: true, altKey: true }))).toBe(false);
  });

  it('does not fire for Shift + a non-Enter key', () => {
    expect(isShiftEnter(ev({ shiftKey: true, key: 'a' }))).toBe(false);
  });

  it('fires for numpad Enter (key is "Enter", code is "NumpadEnter")', () => {
    expect(isShiftEnter(ev({ shiftKey: true, code: 'NumpadEnter' }))).toBe(true);
  });

  it('is platform-independent (no isMac gate)', () => {
    // The predicate reads only event flags, so it behaves identically
    // on every platform — Shift+Enter is the cross-platform newline key.
    expect(isShiftEnter(ev({ shiftKey: true }))).toBe(true);
  });
});

describe('isHelpOverlayKey', () => {
  // "?" is Shift+/ on a US layout, so the browser reports key === '?'
  // directly — the predicate matches on the character, not on shiftKey.
  const cases = [
    ['Cmd+/', { metaKey: true, key: '/' }, true],
    ['Cmd+?', { metaKey: true, key: '?' }, true],
    ['Ctrl+/', { ctrlKey: true, key: '/' }, true],
    ['Ctrl+?', { ctrlKey: true, key: '?' }, true],
    // Shift is incidental: Cmd+Shift+/ still reports key '?' in a real
    // browser, but an explicit shiftKey must not change the verdict.
    ['Cmd+Shift+? (shift flag set)', { metaKey: true, shiftKey: true, key: '?' }, true],
  ];
  for (const [name, e, want] of cases) {
    it(`fires for ${name}`, () => {
      expect(isHelpOverlayKey(ev(e))).toBe(want);
    });
  }

  it('does not fire for a bare "?" — it must reach the terminal', () => {
    // This is the load-bearing negative: "?" is an ordinary character
    // users type constantly. Swallowing it would break typing.
    expect(isHelpOverlayKey(ev({ key: '?' }))).toBe(false);
    expect(isHelpOverlayKey(ev({ shiftKey: true, key: '?' }))).toBe(false);
  });

  it('does not fire for a bare "/"', () => {
    expect(isHelpOverlayKey(ev({ key: '/' }))).toBe(false);
  });

  it('does not fire for other Cmd/Ctrl keys', () => {
    expect(isHelpOverlayKey(ev({ metaKey: true, key: 'k' }))).toBe(false);
    expect(isHelpOverlayKey(ev({ ctrlKey: true, key: '.' }))).toBe(false);
  });
});

describe('NEWLINE_SEQ', () => {
  it('is Ctrl+J / LF (0x0a) — the byte agents accept as a newline', () => {
    expect(NEWLINE_SEQ).toBe('\x0a');
    expect(NEWLINE_SEQ.charCodeAt(0)).toBe(10);
  });
});

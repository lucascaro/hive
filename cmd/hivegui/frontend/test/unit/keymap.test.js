import { describe, it, expect } from 'vitest';
import { isCmdEnter, NEWLINE_SEQ } from '../../src/lib/keymap.js';

// Minimal fake keydown event with all modifier flags defaulted off.
function ev(overrides = {}) {
  return { metaKey: false, ctrlKey: false, altKey: false, shiftKey: false, key: 'Enter', ...overrides };
}

describe('isCmdEnter', () => {
  it('fires for bare Cmd+Enter on mac', () => {
    expect(isCmdEnter(ev({ metaKey: true }), true)).toBe(true);
  });

  it('does not fire for plain Enter (no modifier) — preserves submit', () => {
    expect(isCmdEnter(ev(), true)).toBe(false);
  });

  it('does not fire when another modifier is also held', () => {
    expect(isCmdEnter(ev({ metaKey: true, ctrlKey: true }), true)).toBe(false);
    expect(isCmdEnter(ev({ metaKey: true, altKey: true }), true)).toBe(false);
    expect(isCmdEnter(ev({ metaKey: true, shiftKey: true }), true)).toBe(false);
  });

  it('does not fire for Cmd + a non-Enter key', () => {
    expect(isCmdEnter(ev({ metaKey: true, key: 'a' }), true)).toBe(false);
  });

  it('does not fire on non-mac even with metaKey down (platform gate)', () => {
    expect(isCmdEnter(ev({ metaKey: true }), false)).toBe(false);
  });

  it('fires for numpad Enter (key is "Enter", code is "NumpadEnter")', () => {
    expect(isCmdEnter(ev({ metaKey: true, code: 'NumpadEnter' }), true)).toBe(true);
  });
});

describe('NEWLINE_SEQ', () => {
  it('is Ctrl+J / LF (0x0a) — the byte agents accept as a newline', () => {
    expect(NEWLINE_SEQ).toBe('\x0a');
    expect(NEWLINE_SEQ.charCodeAt(0)).toBe(10);
  });
});

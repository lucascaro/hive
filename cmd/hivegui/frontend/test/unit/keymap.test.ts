import { describe, it, expect } from 'vitest';
import {
  isShiftEnter,
  macLineEditSeq,
  macForwardKillSeq,
  LINE_START_SEQ,
  LINE_END_SEQ,
  KILL_WORD_FORWARD_SEQ,
  KILL_LINE_FORWARD_SEQ,
  isHelpOverlayKey,
  navHistoryKey,
  NEWLINE_SEQ,
} from '../../src/lib/keymap.js';

// Minimal fake keydown event with all modifier flags defaulted off.
function ev(overrides = {}) {
  return {
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    key: 'Enter',
    ...overrides,
  };
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
    expect(isShiftEnter(ev({ shiftKey: true, code: 'NumpadEnter' }))).toBe(
      true,
    );
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
    [
      'Cmd+Shift+? (shift flag set)',
      { metaKey: true, shiftKey: true, key: '?' },
      true,
    ],
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

describe('navHistoryKey', () => {
  describe('macOS — Ctrl+- / Ctrl+Shift+-', () => {
    it('fires for Ctrl+-', () => {
      expect(navHistoryKey(ev({ ctrlKey: true, key: '-' }), true)).toBe('back');
    });

    it('fires forward for Ctrl+Shift+-', () => {
      expect(
        navHistoryKey(ev({ ctrlKey: true, shiftKey: true, key: '-' }), true),
      ).toBe('forward');
    });

    it('accepts "_" — the shifted "-" on a US layout', () => {
      expect(
        navHistoryKey(ev({ ctrlKey: true, shiftKey: true, key: '_' }), true),
      ).toBe('forward');
    });

    it('falls back to the physical Minus key for other layouts', () => {
      expect(
        navHistoryKey(ev({ ctrlKey: true, key: 'Dead', code: 'Minus' }), true),
      ).toBe('back');
    });

    it('does not fire for ⌘- — that is zoom out', () => {
      expect(navHistoryKey(ev({ metaKey: true, key: '-' }), true)).toBe(null);
    });

    it('does not fire for ⌃⌥- — Alt is the non-mac chord, kept free here', () => {
      expect(
        navHistoryKey(ev({ ctrlKey: true, altKey: true, key: '-' }), true),
      ).toBe(null);
    });

    it('does not fire for Ctrl+Cmd+- (both modifiers held)', () => {
      expect(
        navHistoryKey(ev({ ctrlKey: true, metaKey: true, key: '-' }), true),
      ).toBe(null);
    });
  });

  describe('Windows / Linux — Ctrl+Alt+- / Ctrl+Alt+Shift+-', () => {
    it('does NOT fire for plain Ctrl+- — that is zoom out', () => {
      // Load-bearing negative. On non-mac, app/keyboard.ts binds Ctrl+-
      // to bumpFontSize(-1) via the cmdOrCtrl gate. Claiming it here
      // would silently remove zoom out on two of three platforms.
      expect(navHistoryKey(ev({ ctrlKey: true, key: '-' }), false)).toBe(null);
      expect(
        navHistoryKey(ev({ ctrlKey: true, shiftKey: true, key: '_' }), false),
      ).toBe(null);
    });

    it('fires for Ctrl+Alt+-', () => {
      expect(
        navHistoryKey(ev({ ctrlKey: true, altKey: true, key: '-' }), false),
      ).toBe('back');
    });

    it('fires forward for Ctrl+Alt+Shift+-', () => {
      expect(
        navHistoryKey(
          ev({ ctrlKey: true, altKey: true, shiftKey: true, key: '-' }),
          false,
        ),
      ).toBe('forward');
    });

    it('does not fire for Alt+- without Ctrl', () => {
      expect(navHistoryKey(ev({ altKey: true, key: '-' }), false)).toBe(null);
    });
  });

  it('does not fire for a bare "-" — it must reach the terminal', () => {
    expect(navHistoryKey(ev({ key: '-' }), true)).toBe(null);
    expect(navHistoryKey(ev({ key: '-' }), false)).toBe(null);
  });

  it('does not fire for Ctrl + another key', () => {
    expect(navHistoryKey(ev({ ctrlKey: true, key: '=' }), true)).toBe(null);
    expect(
      navHistoryKey(ev({ ctrlKey: true, altKey: true, key: '=' }), false),
    ).toBe(null);
  });
});

describe('NEWLINE_SEQ', () => {
  it('is Ctrl+J / LF (0x0a) — the byte agents accept as a newline', () => {
    expect(NEWLINE_SEQ).toBe('\x0a');
    expect(NEWLINE_SEQ.charCodeAt(0)).toBe(10);
  });
});

// ⌘←/⌘→ are start/end of line in every macOS terminal, but nothing
// produces those bytes for us: xterm.js explicitly emits NOTHING for a
// meta-modified arrow (`case 37: if (e.metaKey) break` in its key
// handler), and the browser does not translate the chord either. So the
// GUI has to do it, exactly as it already does for Cmd+Backspace -> Ctrl+U.
describe('macLineEditSeq', () => {
  const cmdLeft = ev({ metaKey: true, key: 'ArrowLeft' });
  const cmdRight = ev({ metaKey: true, key: 'ArrowRight' });

  it('maps cmd+left to start-of-line and cmd+right to end-of-line on mac', () => {
    expect(macLineEditSeq(cmdLeft, true)).toBe(LINE_START_SEQ);
    expect(macLineEditSeq(cmdRight, true)).toBe(LINE_END_SEQ);
  });

  it('sends the readline control bytes, not an escape sequence', () => {
    // Ctrl+A / Ctrl+E: what iTerm2's "Natural Text Editing" preset sends,
    // and what bash, zsh, and the agent CLIs all understand unconfigured.
    expect(LINE_START_SEQ).toBe('\x01');
    expect(LINE_END_SEQ).toBe('\x05');
  });

  it('does nothing off mac — ctrl+arrows are word-wise there and xterm emits them', () => {
    expect(
      macLineEditSeq(ev({ ctrlKey: true, key: 'ArrowLeft' }), false),
    ).toBeNull();
    expect(
      macLineEditSeq(ev({ metaKey: true, key: 'ArrowLeft' }), false),
    ).toBeNull();
  });

  it('ignores other arrows and unmodified arrows', () => {
    expect(
      macLineEditSeq(ev({ metaKey: true, key: 'ArrowUp' }), true),
    ).toBeNull();
    expect(macLineEditSeq(ev({ key: 'ArrowLeft' }), true)).toBeNull();
  });

  it('does not fire when Ctrl or Alt is also held', () => {
    // opt+left is word-left and ctrl+left is a pane binding — neither is ours.
    expect(
      macLineEditSeq(
        ev({ metaKey: true, altKey: true, key: 'ArrowLeft' }),
        true,
      ),
    ).toBeNull();
    expect(
      macLineEditSeq(
        ev({ metaKey: true, ctrlKey: true, key: 'ArrowLeft' }),
        true,
      ),
    ).toBeNull();
  });

  it('fires with Shift held so shift+cmd+arrows at least move the cursor', () => {
    // A PTY has no selection to extend, so shift can only be honored as
    // the plain move. Doing nothing would be the more surprising outcome.
    expect(
      macLineEditSeq(
        ev({ metaKey: true, shiftKey: true, key: 'ArrowLeft' }),
        true,
      ),
    ).toBe(LINE_START_SEQ);
  });
});

describe('macForwardKillSeq', () => {
  const optDelete = ev({ altKey: true, key: 'Delete' });
  const cmdDelete = ev({ metaKey: true, key: 'Delete' });

  it('maps opt+forward-delete to kill-word and cmd+forward-delete to kill-line on mac', () => {
    expect(macForwardKillSeq(optDelete, true)).toBe(KILL_WORD_FORWARD_SEQ);
    expect(macForwardKillSeq(cmdDelete, true)).toBe(KILL_LINE_FORWARD_SEQ);
  });

  it('sends the readline bytes, not the CSI sequence xterm would emit', () => {
    // xterm's `case 46` turns any modified forward delete into
    // \x1b[3;<mods+1>~ — \x1b[3;3~ and \x1b[3;9~ here — which nothing
    // binds. meta-d and Ctrl+K are what readline actually listens for.
    expect(KILL_WORD_FORWARD_SEQ).toBe('\x1bd');
    expect(KILL_LINE_FORWARD_SEQ).toBe('\x0b');
  });

  it('does nothing off mac — ctrl+delete is kill-word there and xterm encodes it', () => {
    expect(macForwardKillSeq(optDelete, false)).toBeNull();
    expect(macForwardKillSeq(cmdDelete, false)).toBeNull();
  });

  it('ignores Backspace, which must keep its own bindings', () => {
    // The one mistake that would silently rebind ⌫: xterm sends \x7f
    // (\x1b\x7f under opt) and session-term maps cmd+backspace to \x15.
    expect(
      macForwardKillSeq(ev({ altKey: true, key: 'Backspace' }), true),
    ).toBeNull();
    expect(
      macForwardKillSeq(ev({ metaKey: true, key: 'Backspace' }), true),
    ).toBeNull();
  });

  it('ignores a bare forward delete so xterm\u2019s \\x1b[3~ still reaches the PTY', () => {
    expect(macForwardKillSeq(ev({ key: 'Delete' }), true)).toBeNull();
  });

  it('does not fire when ctrl is held — ctrl+delete belongs to the shell', () => {
    expect(
      macForwardKillSeq(
        ev({ ctrlKey: true, altKey: true, key: 'Delete' }),
        true,
      ),
    ).toBeNull();
    expect(
      macForwardKillSeq(
        ev({ ctrlKey: true, metaKey: true, key: 'Delete' }),
        true,
      ),
    ).toBeNull();
  });

  it('fires with Shift held, matching macLineEditSeq', () => {
    // ⇧⌘⌦ selects to end of line in a text field; a PTY has no selection,
    // so the kill is the closest honest thing.
    expect(
      macForwardKillSeq(
        ev({ metaKey: true, shiftKey: true, key: 'Delete' }),
        true,
      ),
    ).toBe(KILL_LINE_FORWARD_SEQ);
  });

  it('ignores opt+cmd together rather than guessing which kill was meant', () => {
    expect(
      macForwardKillSeq(
        ev({ altKey: true, metaKey: true, key: 'Delete' }),
        true,
      ),
    ).toBeNull();
  });
});

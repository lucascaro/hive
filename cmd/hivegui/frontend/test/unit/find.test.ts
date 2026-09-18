import { describe, expect, it } from 'vitest';
import {
  findSourceFor,
  formatCount,
  groupMessages,
  messageLabel,
  highlightSegments,
  mayUseSearchAddon,
  newestFirstIndex,
  reanchorIndex,
  stepIndex,
  transcriptScrollTarget,
  visibleLineCount,
  unavailableMessage,
} from '../../src/lib/find';
import { findKey } from '../../src/lib/keymap';

describe('findSourceFor', () => {
  it('routes the alternate buffer to the transcript', () => {
    expect(findSourceFor('alternate')).toBe('transcript');
  });

  it('routes the normal buffer to the terminal buffer', () => {
    expect(findSourceFor('normal')).toBe('buffer');
  });

  // An unknown buffer type must not take the terminal away from the
  // user; buffer search degrades to finding less, never to finding the
  // wrong thing.
  it('treats an unknown or missing buffer type as normal', () => {
    expect(findSourceFor(undefined)).toBe('buffer');
    expect(findSourceFor('')).toBe('buffer');
    expect(findSourceFor('something-new')).toBe('buffer');
  });
});

describe('mayUseSearchAddon', () => {
  // The invariant that makes the addon-poisoning defect unreachable:
  // any search run while the alternate buffer is active permanently
  // breaks the addon for the normal buffer for the rest of the
  // session's life.
  it('is false on the alternate buffer', () => {
    expect(mayUseSearchAddon('alternate')).toBe(false);
  });

  it('is true on the normal buffer', () => {
    expect(mayUseSearchAddon('normal')).toBe(true);
    expect(mayUseSearchAddon(undefined)).toBe(true);
  });
});

describe('highlightSegments', () => {
  it('splits around a single match', () => {
    expect(highlightSegments('a needle here', [{ col: 2, len: 6 }])).toEqual([
      { text: 'a ', hit: false, at: -1, start: 0 },
      { text: 'needle', hit: true, at: 0, start: 2 },
      { text: ' here', hit: false, at: -1, start: 8 },
    ]);
  });

  it('handles a match at the start of the line', () => {
    expect(highlightSegments('needle here', [{ col: 0, len: 6 }])).toEqual([
      { text: 'needle', hit: true, at: 0, start: 0 },
      { text: ' here', hit: false, at: -1, start: 6 },
    ]);
  });

  it('handles a match at the end of the line', () => {
    expect(highlightSegments('a needle', [{ col: 2, len: 6 }])).toEqual([
      { text: 'a ', hit: false, at: -1, start: 0 },
      { text: 'needle', hit: true, at: 0, start: 2 },
    ]);
  });

  it('numbers multiple matches on one line', () => {
    const segs = highlightSegments('x y x', [
      { col: 0, len: 1 },
      { col: 4, len: 1 },
    ]);
    expect(segs.filter((s) => s.hit).map((s) => s.at)).toEqual([0, 1]);
  });

  it('returns the whole line when there are no matches', () => {
    expect(highlightSegments('plain', [])).toEqual([
      { text: 'plain', hit: false, at: -1, start: 0 },
    ]);
  });

  // A stale match list paired with a fresh line must degrade to
  // highlighting nothing, never to a crash or a wrong slice.
  it('skips out-of-range and overlapping matches', () => {
    expect(highlightSegments('short', [{ col: 99, len: 3 }])).toEqual([
      { text: 'short', hit: false, at: -1, start: 0 },
    ]);
    expect(highlightSegments('short', [{ col: 0, len: 0 }])).toEqual([
      { text: 'short', hit: false, at: -1, start: 0 },
    ]);
    const overlapping = highlightSegments('abcdef', [
      { col: 0, len: 3 },
      { col: 1, len: 3 },
    ]);
    expect(overlapping.filter((s) => s.hit)).toHaveLength(1);
  });

  // A match running past the end of the line is clipped, not dropped:
  // the daemon truncates long lines, so the tail of a match can be cut.
  it('clips a match that runs past the end of the line', () => {
    expect(highlightSegments('abc', [{ col: 1, len: 99 }])).toEqual([
      { text: 'a', hit: false, at: -1, start: 0 },
      { text: 'bc', hit: true, at: 0, start: 1 },
    ]);
  });
});

describe('formatCount', () => {
  it('is 1-based for the user', () => {
    expect(formatCount(0, 17)).toBe('1/17');
    expect(formatCount(16, 17)).toBe('17/17');
  });

  it('renders the empty case', () => {
    expect(formatCount(0, 0)).toBe('0/0');
  });

  // The xterm addon hard-caps its reported count at 1000; a bare 1000
  // would be a confidently wrong number.
  it('marks a capped total', () => {
    expect(formatCount(0, 1000, true)).toBe('1/1000+');
  });
});

describe('stepIndex', () => {
  it('wraps forward past the end', () => {
    expect(stepIndex(2, 3, 1)).toBe(0);
  });

  it('wraps backward past the start', () => {
    expect(stepIndex(0, 3, -1)).toBe(2);
  });

  it('is a no-op with no matches', () => {
    expect(stepIndex(0, 0, 1)).toBe(0);
  });
});

describe('unavailableMessage', () => {
  // "This agent keeps none" and "it should have one and none was found"
  // must not read the same: the second is transient, and conflating
  // them tells a real Claude session it has no history.
  it('distinguishes an unsupported agent from a missing file', () => {
    expect(unavailableMessage('unsupported_agent')).not.toBe(
      unavailableMessage('no_transcript_file'),
    );
  });

  it('has a fallback for an unknown reason', () => {
    expect(unavailableMessage('something-new')).toBeTruthy();
  });
});

describe('findKey', () => {
  const ev = (o: Partial<Record<string, unknown>> = {}) => ({
    key: 'f',
    code: 'KeyF',
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    ...o,
  });

  it('fires on Ctrl+Shift+F off macOS', () => {
    expect(findKey(ev({ ctrlKey: true, shiftKey: true }), false)).toBe(true);
  });

  // Plain Ctrl+F is 0x06 — readline's forward-char, live in every
  // agent's input line.
  it('never fires on plain Ctrl+F', () => {
    expect(findKey(ev({ ctrlKey: true }), false)).toBe(false);
    expect(findKey(ev({ ctrlKey: true }), true)).toBe(false);
  });

  // On macOS the native menu accelerator intercepts before the webview,
  // so a keydown branch there would be dead code that only fires in
  // tests.
  it('does not fire on macOS, where the menu owns the chord', () => {
    expect(findKey(ev({ metaKey: true }), true)).toBe(false);
    expect(findKey(ev({ metaKey: true, shiftKey: true }), true)).toBe(false);
  });

  it('ignores other keys and stray modifiers', () => {
    expect(
      findKey(
        ev({ key: 'g', code: 'KeyG', ctrlKey: true, shiftKey: true }),
        false,
      ),
    ).toBe(false);
    expect(
      findKey(ev({ ctrlKey: true, shiftKey: true, altKey: true }), false),
    ).toBe(false);
    expect(
      findKey(ev({ ctrlKey: true, shiftKey: true, metaKey: true }), false),
    ).toBe(false);
  });
});

describe('theme colour mixing for find highlights', () => {
  it('parses both hex forms', async () => {
    const { parseHex } = await import('../../src/theme/theme');
    expect(parseHex('#000')).toEqual([0, 0, 0]);
    expect(parseHex('#f59e0b')).toEqual([245, 158, 11]);
  });

  // The addon rejects anything but #RRGGBB, so anything else must be
  // reported as unparseable rather than guessed at.
  it('rejects non-hex colours', async () => {
    const { parseHex } = await import('../../src/theme/theme');
    expect(parseHex('color-mix(in srgb, red 50%, blue)')).toBeNull();
    expect(parseHex('')).toBeNull();
    expect(parseHex('#12')).toBeNull();
  });

  it('mixes at the given weight and always emits #rrggbb', async () => {
    const { mixHex } = await import('../../src/theme/theme');
    expect(mixHex('#ffffff', '#000', 1)).toBe('#ffffff');
    expect(mixHex('#ffffff', '#000', 0)).toBe('#000000');
    expect(mixHex('#ffffff', '#000000', 0.5)).toBe('#808080');
    expect(mixHex('#fff', '#fff', 1)).toBe('#ffffff');
  });

  it('returns the first colour unchanged when either is not hex', async () => {
    const { mixHex } = await import('../../src/theme/theme');
    expect(mixHex('#abcdef', 'transparent', 0.5)).toBe('#abcdef');
  });
});

describe('newestFirstIndex', () => {
  // The addon counts top-down; the box counts from the bottom.
  it('maps the bottom-most result to 0', () => {
    expect(newestFirstIndex(2, 3)).toBe(0);
    expect(newestFirstIndex(0, 3)).toBe(2);
  });

  it('clamps the addon’s -1 (beyond the highlight limit) to 0', () => {
    expect(newestFirstIndex(-1, 5)).toBe(0);
  });

  it('is 0 with no results', () => {
    expect(newestFirstIndex(0, 0)).toBe(0);
  });
});

describe('reanchorIndex', () => {
  // New output prepends newer matches; the user must stay on theirs.
  it('follows the same match after newer ones are prepended', () => {
    const next = [
      { line: 50, col: 0 },
      { line: 40, col: 0 },
      { line: 10, col: 3 },
    ];
    expect(reanchorIndex({ line: 10, col: 3 }, next)).toBe(2);
  });

  it('falls back to the newest when the match is gone', () => {
    expect(reanchorIndex({ line: 1, col: 1 }, [{ line: 9, col: 0 }])).toBe(0);
  });

  it('is 0 for a fresh search with no previous match', () => {
    expect(reanchorIndex(undefined, [{ line: 9, col: 0 }])).toBe(0);
  });
});

describe('transcriptScrollTarget', () => {
  const pane = { scrollHeight: 2000, clientHeight: 500 };

  // The transcript opens on the most recent output.
  it('goes to the bottom when there is no active match', () => {
    expect(transcriptScrollTarget({ ...pane, hasActive: false })).toBe(1500);
  });

  it('centres a rendered active match', () => {
    expect(
      transcriptScrollTarget({
        ...pane,
        hasActive: true,
        activeTop: 1000,
        activeHeight: 20,
      }),
    ).toBe(760); // 1000 - (500 - 20) / 2
  });

  it('clamps at both ends of the pane', () => {
    expect(
      transcriptScrollTarget({ ...pane, hasActive: true, activeTop: 10 }),
    ).toBe(0);
    expect(
      transcriptScrollTarget({ ...pane, hasActive: true, activeTop: 1990 }),
    ).toBe(1500);
  });

  // While typing, the match lands before its window of lines. Jumping to
  // the bottom in that gap would make the pane move twice.
  it('holds still while the active match is not rendered yet', () => {
    expect(transcriptScrollTarget({ ...pane, hasActive: true })).toBeNull();
  });

  it('is 0 when the content fits', () => {
    expect(
      transcriptScrollTarget({
        scrollHeight: 300,
        clientHeight: 500,
        hasActive: false,
      }),
    ).toBe(0);
  });
});

describe('groupMessages', () => {
  it('groups consecutive lines of one message', () => {
    const g = groupMessages([
      { line: 0, msg: 1, kind: 'user' },
      { line: 1, msg: 1, kind: 'user' },
      { line: 2, msg: 2, kind: 'assistant' },
    ]);
    expect(g.map((x) => x.lines.length)).toEqual([2, 1]);
    expect(g.map((x) => x.kind)).toEqual(['user', 'assistant']);
  });

  it('carries the tool name of a tool-output message', () => {
    const g = groupMessages([{ line: 0, msg: 5, kind: 'tool', tool: 'Bash' }]);
    expect(g[0].tool).toBe('Bash');
  });

  // An older daemon sends no message ids: every line stands alone rather
  // than all collapsing into one giant group.
  it('keeps lines without a message id separate', () => {
    const g = groupMessages([{ line: 0 }, { line: 1 }]);
    expect(g).toHaveLength(2);
  });
});

describe('messageLabel', () => {
  it('labels prompts and tool output, not assistant text', () => {
    expect(messageLabel({ kind: 'user' })).toBe('You');
    expect(messageLabel({ kind: 'tool', tool: 'Bash' })).toBe('Bash');
    expect(messageLabel({ kind: 'tool' })).toBe('Tool output');
    expect(messageLabel({ kind: 'assistant' })).toBe('');
  });
});

describe('visibleLineCount', () => {
  const lines = (n: number) =>
    Array.from({ length: n }, (_, i) => ({ line: i }));

  it('collapses long tool output', () => {
    expect(
      visibleLineCount({
        kind: 'tool',
        lines: lines(300),
        hasMatch: false,
        expanded: false,
      }),
    ).toEqual({ shown: 8, hidden: 292 });
  });

  it('never collapses prose', () => {
    expect(
      visibleLineCount({
        kind: 'assistant',
        lines: lines(300),
        hasMatch: false,
        expanded: false,
      }),
    ).toEqual({ shown: 300, hidden: 0 });
  });

  it('leaves short tool output alone', () => {
    expect(
      visibleLineCount({
        kind: 'tool',
        lines: lines(12),
        hasMatch: false,
        expanded: false,
      }),
    ).toEqual({ shown: 12, hidden: 0 });
  });

  // Search must never be hidden behind a collapse.
  it('shows everything when a line matches', () => {
    expect(
      visibleLineCount({
        kind: 'tool',
        lines: lines(300),
        hasMatch: true,
        expanded: false,
      }),
    ).toEqual({ shown: 300, hidden: 0 });
  });

  it('shows everything once expanded', () => {
    expect(
      visibleLineCount({
        kind: 'tool',
        lines: lines(300),
        hasMatch: false,
        expanded: true,
      }),
    ).toEqual({ shown: 300, hidden: 0 });
  });
});

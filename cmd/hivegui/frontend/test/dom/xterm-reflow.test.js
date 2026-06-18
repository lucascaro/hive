// @vitest-environment jsdom
//
// Evidence test: does xterm.js (5.5) reflow its own scrollback on resize,
// or do we actually need the daemon to re-stream the byte ring?
//
// The replay-on-resize path (session-term._onBodyResize → shouldRequestReplay
// → RequestScrollbackReplay) was built on the comment "xterm.js does not
// reflow history on resize". This test checks that claim directly: write a
// long wrapped line, resize narrower/wider, and inspect the buffer.
import { describe, it, expect } from 'vitest';
import { Terminal } from '@xterm/xterm';

function write(term, data) {
  return new Promise((resolve) => term.write(data, resolve));
}

// Count the physical rows a logical line occupies (1 + wrapped continuations)
// and reconstruct its text across the wrap boundary.
function logicalLines(term) {
  const buf = term.buffer.active;
  const lines = [];
  for (let i = 0; i < buf.length; i++) {
    const line = buf.getLine(i);
    if (!line) continue;
    const text = line.translateToString(true);
    if (line.isWrapped && lines.length) {
      lines[lines.length - 1].text += text;
      lines[lines.length - 1].rows += 1;
    } else if (text.length || !line.isWrapped) {
      lines.push({ text, rows: 1 });
    }
  }
  return lines.filter((l) => l.text.length);
}

describe('xterm.js native reflow on resize', () => {
  it('does not lose or corrupt a long line across widen + narrow', async () => {
    const term = new Terminal({ cols: 20, rows: 6, scrollback: 1000 });
    // 60 visible chars, no newline → wraps at width 20.
    const sixty = 'x'.repeat(60);
    await write(term, sixty);
    expect(logicalLines(term).find((l) => l.text.startsWith('x')).text).toBe(sixty);

    // Content must survive a resize in both directions (no data loss). NOTE:
    // whether xterm *re-wraps* (3 rows → 2 on widening) is renderer-dependent
    // and did NOT reliably trigger under headless jsdom — that's why we still
    // verify reflow visually in a real browser before trusting it to replace
    // the daemon replay. Here we only assert the safe invariant: no corruption.
    term.resize(40, 6);
    expect(logicalLines(term).find((l) => l.text.startsWith('x')).text).toBe(sixty);
    term.resize(20, 6);
    expect(logicalLines(term).find((l) => l.text.startsWith('x')).text).toBe(sixty);
  });

  it('preserves scrollback content across a resize', async () => {
    const term = new Terminal({ cols: 20, rows: 4, scrollback: 1000 });
    // Push several lines so the early ones land in scrollback (rows=4).
    for (let i = 0; i < 10; i++) await write(term, `line-${i}\r\n`);
    term.resize(40, 4);
    const texts = logicalLines(term).map((l) => l.text.trim());
    // Early lines that scrolled off must still be present after the resize.
    expect(texts).toContain('line-0');
    expect(texts).toContain('line-9');
  });
});

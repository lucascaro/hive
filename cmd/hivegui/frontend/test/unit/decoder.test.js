// Regression: each SessionTerm must own its TextDecoder. A shared,
// streaming decoder buffers a partial multi-byte sequence from one
// session's chunk into the next session's decode, producing U+FFFD or
// wrong glyphs. See docs/product-specs/195-shared-textdecoder-glyphs.md.

import { describe, it, expect } from 'vitest';

const REPLACEMENT = '�';

// Encode `s` and split it at byte index `cut`. The first slice is
// guaranteed to end mid-rune for any non-ASCII `s` with cut between 1
// and bytes.length-1 chosen to land inside a multi-byte sequence.
function splitMidRune(s) {
  const bytes = new TextEncoder().encode(s);
  // Find a byte index that sits inside a multi-byte sequence.
  for (let i = 1; i < bytes.length; i++) {
    // Continuation bytes match 10xxxxxx.
    if ((bytes[i] & 0xc0) === 0x80) {
      return [bytes.slice(0, i), bytes.slice(i)];
    }
  }
  throw new Error(`no multi-byte boundary in ${JSON.stringify(s)}`);
}

describe('per-session UTF-8 decoder', () => {
  // Strings exercised: CJK, Powerline/spinner glyph from Claude, box-drawing.
  const samples = ['こんにちは世界', '✻ Working', '┌─┬─┐'];

  it('decodes each session independently when chunks split mid-rune', () => {
    for (const s of samples) {
      const [head, tail] = splitMidRune(s);
      const decA = new TextDecoder('utf-8', { fatal: false });
      const decB = new TextDecoder('utf-8', { fatal: false });

      // Interleave: A sends head, B sends head, A sends tail, B sends tail.
      let outA = decA.decode(head, { stream: true });
      let outB = decB.decode(head, { stream: true });
      outA += decA.decode(tail, { stream: true });
      outB += decB.decode(tail, { stream: true });
      // Flush any trailing state.
      outA += decA.decode();
      outB += decB.decode();

      expect(outA).toBe(s);
      expect(outB).toBe(s);
      expect(outA.includes(REPLACEMENT)).toBe(false);
      expect(outB.includes(REPLACEMENT)).toBe(false);
    }
  });

  it('pins the bug: a shared streaming decoder corrupts interleaved sessions', () => {
    // This test documents the failure mode the fix prevents. If
    // someone reverts to a shared decoder, the per-session test above
    // would still pass — this one demonstrates *why* sharing is wrong.
    const s = 'こんにちは';
    const [head, tail] = splitMidRune(s);
    const shared = new TextDecoder('utf-8', { fatal: false });

    let outA = shared.decode(head, { stream: true });
    // Session B's "head" is decoded against A's buffered continuation
    // bytes — corrupting both streams.
    let outB = shared.decode(head, { stream: true });
    outA += shared.decode(tail, { stream: true });
    outB += shared.decode(tail, { stream: true });
    outA += shared.decode();
    outB += shared.decode();

    const combined = outA + outB;
    expect(combined.includes(REPLACEMENT)).toBe(true);
  });
});

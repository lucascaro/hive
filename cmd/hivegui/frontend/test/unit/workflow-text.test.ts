// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { describe, expect, it } from 'vitest';
import {
  ago,
  formatBytes,
  plainText,
  showControls,
  stripCsi,
} from '../../src/lib/workflow-text.js';

describe('showControls', () => {
  it('leaves a plain path unchanged', () => {
    expect(showControls('/Users/me/hive/.hive/workflows/review-loop.ts')).toBe(
      '/Users/me/hive/.hive/workflows/review-loop.ts',
    );
    expect(showControls('naïve/日本語.ts')).toBe('naïve/日本語.ts');
  });

  it('escapes C0, DEL and C1 controls visibly', () => {
    expect(showControls('a\u0000b\u001Bc\u007Fd\u009Be')).toBe(
      'a\\u{0000}b\\u{001B}c\\u{007F}d\\u{009B}e',
    );
    expect(showControls('a\nb')).toBe('a\\u{000A}b');
  });

  it('escapes every bidi override and isolate, so a name cannot reorder', () => {
    for (const cp of [
      0x202a, 0x202b, 0x202c, 0x202d, 0x202e, 0x2066, 0x2067, 0x2068, 0x2069,
    ]) {
      const hex = cp.toString(16).toUpperCase();
      expect(showControls(`evil${String.fromCharCode(cp)}gnp.ts`)).toBe(
        `evil\\u{${hex}}gnp.ts`,
      );
    }
  });

  it('escapes marks, zero-width characters and the BOM too', () => {
    expect(showControls('a\u200Eb\u200Bc\uFEFFd')).toBe(
      'a\\u{200E}b\\u{200B}c\\u{FEFF}d',
    );
  });

  it('escapes soft hyphens, variation selectors, tag characters and Hangul fillers', () => {
    expect(showControls('a\u00ADb\uFE0Fc\u3164d')).toBe(
      'a\\u{00AD}b\\u{FE0F}c\\u{3164}d',
    );
    expect(showControls('x\u{E0041}y')).toBe('x\\u{E0041}y');
  });

  it('keeps the characters it is told to', () => {
    expect(showControls('a\nb\tc', '\n\t')).toBe('a\nb\tc');
    expect(plainText('a\nb\u202Ec')).toBe('a\nb\\u{202E}c');
  });
});

describe('stripCsi', () => {
  it('removes SGR colour, cursor moves and OSC, keeps newlines and tabs', () => {
    const out = stripCsi(
      '\u001B[31mFAIL\u001B[0m\u001B[2K\u001B[1;5H\tok\n\u001B]0;title\u0007done\u001B]8;;http://x\u001B\\',
    );
    expect(out).toBe('FAIL\tok\ndone');
  });

  it('handles the 8-bit CSI and bare two-byte escapes', () => {
    expect(stripCsi('\u009B31mred\u001B=\u001Bcend')).toBe('redend');
  });

  it('turns CRLF into a newline and drops a lone CR', () => {
    expect(stripCsi('a\r\nb\rc')).toBe('a\nbc');
  });

  it('shows whatever control or bidi character survives', () => {
    expect(stripCsi('a\u0007b\u202Ec')).toBe('a\\u{0007}b\\u{202E}c');
  });
});

describe('formatting', () => {
  it('formats relative times', () => {
    const now = Date.parse('2026-11-09T12:00:00Z');
    expect(ago('2026-11-09T11:59:30Z', now)).toBe('just now');
    expect(ago('2026-11-09T11:58:00Z', now)).toBe('2 min ago');
    expect(ago('2026-11-09T09:00:00Z', now)).toBe('3 h ago');
    expect(ago('2026-11-08T11:00:00Z', now)).toBe('yesterday');
    expect(ago('not a time', now)).toBe('');
  });

  it('formats byte counts', () => {
    expect(formatBytes(12)).toBe('12 B');
    expect(formatBytes(2150)).toBe('2.1 KB');
  });
});

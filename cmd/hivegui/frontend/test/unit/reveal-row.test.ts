// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { describe, expect, it } from 'vitest';
import { revealDelta } from '../../src/lib/reveal-row';

// A 400px view at y=100 with 26px of sticky header pinned over its top:
// the usable band is [126, 500].
const view = { viewTop: 100, viewBottom: 500, inset: 26 };

describe('revealDelta (#563)', () => {
  it('returns 0 for a row fully inside the band', () => {
    expect(revealDelta({ ...view, rowTop: 126, rowBottom: 150 })).toBe(0);
    expect(revealDelta({ ...view, rowTop: 476, rowBottom: 500 })).toBe(0);
  });

  it('scrolls up just enough to clear the sticky inset', () => {
    // Partly under the header: top at 110, 16px short of the band.
    expect(revealDelta({ ...view, rowTop: 110, rowBottom: 134 })).toBe(-16);
    // Far above the view.
    expect(revealDelta({ ...view, rowTop: -300, rowBottom: -276 })).toBe(-426);
  });

  it('scrolls down just enough to show the bottom edge', () => {
    expect(revealDelta({ ...view, rowTop: 490, rowBottom: 514 })).toBe(14);
    expect(revealDelta({ ...view, rowTop: 900, rowBottom: 924 })).toBe(424);
  });

  it('aligns the top of a row taller than the band', () => {
    expect(revealDelta({ ...view, rowTop: 300, rowBottom: 900 })).toBe(174);
  });
});

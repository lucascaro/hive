// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { describe, expect, it } from 'vitest';
import { placePopup } from '../../src/lib/popup-place';

// A project header 20px tall in an 800px window. GAP is 4, MARGIN is 8.
const header = (top: number) => ({ top, bottom: top + 20 });

describe('placePopup (#580)', () => {
  it('places below the anchor when it fits', () => {
    expect(placePopup(header(40), 300, 800)).toEqual({
      top: 64,
      side: 'below',
    });
  });

  it('flips above the anchor when below would overflow', () => {
    // below: 724 + 300 > 792. above: 700 - 4 - 300 = 396.
    expect(placePopup(header(700), 300, 800)).toEqual({
      top: 396,
      side: 'above',
    });
  });

  it('clamps to the viewport when neither side fits', () => {
    // 220px window, header at 40: below 64 + 180 > 212, above 40 - 4 - 180 < 8.
    const r = placePopup(header(40), 180, 220);
    expect(r.top).toBe(220 - 8 - 180);
    expect(r.top + 180).toBeLessThanOrEqual(220 - 8);
    expect(r.top).toBeGreaterThanOrEqual(8);
  });

  it('never goes above the top margin when the popup is taller than the window', () => {
    expect(placePopup(header(40), 900, 220).top).toBe(8);
  });

  it('stays above once flipped, even when the popup shrinks', () => {
    // 60px would fit below (724 + 60 <= 792), but the popup already sits
    // above: flipping back while the user filters would jump it.
    expect(placePopup(header(700), 60, 800, 'above')).toEqual({
      top: 636,
      side: 'above',
    });
  });

  it('no anchor: the fixed spot, clamped', () => {
    expect(placePopup(null, 100, 800)).toEqual({ top: 64, side: 'below' });
    expect(placePopup(null, 300, 300).top).toBe(8);
  });

  it('height 0 (hidden) returns the provisional top and keeps the side', () => {
    expect(placePopup(header(700), 0, 800)).toEqual({
      top: 724,
      side: undefined,
    });
    expect(placePopup(header(700), 0, 800, 'above').side).toBe('above');
  });
  it('window shrink: stays above but is clamped back inside the window', () => {
    // Opened above in an 800px window (top 396), then the window shrinks
    // to 500: the anchor rect is stale, and 396 + 300 > 492.
    const r = placePopup(header(700), 300, 500, 'above');
    expect(r).toEqual({ top: 500 - 8 - 300, side: 'above' });
  });

  it('anchor scrolled above the window: clamps to the top margin', () => {
    // header(-30): bottom -10, so below would be -6.
    expect(placePopup(header(-30), 100, 800)).toEqual({
      top: 8,
      side: 'below',
    });
  });
});

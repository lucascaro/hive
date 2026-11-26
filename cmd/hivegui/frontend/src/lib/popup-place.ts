// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Vertical placement for a fixed popup anchored to an element (#580): the
// new-session launcher opens under its project's header, and for a
// project near the bottom of the sidebar that ran it off the window.
//
// Below the anchor when it fits, as before. Otherwise above it, which
// keeps the anchor (the header naming the project the session lands in)
// visible. Otherwise clamped into the window; the popup's CSS max-height
// and overflow make the rest scroll.

export type PopupSide = 'below' | 'above';

export interface AnchorRect {
  top: number;
  bottom: number;
}

const GAP = 4;
const MARGIN = 8;
// Where the popup sits when there is nothing to anchor to.
const FALLBACK_TOP = 64;

// prev is the side the popup is already on. Once above, it stays above:
// filtering the list shrinks the popup, and flipping back below on a
// keystroke would move it out from under the pointer.
//
// height 0 means the popup is not laid out yet (display:none). There is
// nothing to fit, so it returns the provisional below-the-anchor top and
// leaves the side as it was, for the first real measurement to decide.
export function placePopup(
  anchor: AnchorRect | null,
  height: number,
  viewportH: number,
  prev?: PopupSide,
): { top: number; side: PopupSide | undefined } {
  const below = anchor ? anchor.bottom + GAP : FALLBACK_TOP;
  if (height <= 0) return { top: below, side: prev };
  // Every placement ends inside the window's margins: the anchor rect is
  // read once at open, so a later window shrink, or an anchor scrolled
  // partly out of view, must not leave the popup past an edge.
  const fit = (top: number) =>
    Math.max(MARGIN, Math.min(top, viewportH - MARGIN - height));
  const fitsBelow = below >= MARGIN && below + height <= viewportH - MARGIN;
  if (prev !== 'above' && fitsBelow) return { top: below, side: 'below' };
  if (anchor) {
    const above = anchor.top - GAP - height;
    if (above >= MARGIN) return { top: fit(above), side: 'above' };
  }
  return { top: fit(below), side: prev ?? 'below' };
}

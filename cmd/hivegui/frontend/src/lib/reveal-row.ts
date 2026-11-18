// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Bring a sidebar row into view after the active session changes (#563).
//
// scrollTop by hand rather than scrollIntoView, as ActivityTile and FindBox
// do: scrollIntoView walks every scrollable ancestor, and it cannot see
// the sticky project and worktree-group headers stacked over the top of
// #projects, so a "nearest" reveal would park the row underneath them.

export interface RevealGeometry {
  rowTop: number;
  rowBottom: number;
  viewTop: number;
  viewBottom: number;
  /** Height of the sticky headers covering the top of the view. */
  inset: number;
}

/** How far to scroll so the row sits fully inside
 *  [viewTop + inset, viewBottom]. 0 when it already does, which is what
 *  keeps a click on a visible row from moving the list. A row taller than
 *  the band aligns to its top. */
export function revealDelta(g: RevealGeometry): number {
  const top = g.viewTop + g.inset;
  if (g.rowTop < top) return g.rowTop - top;
  if (g.rowBottom > g.viewBottom) {
    return Math.min(g.rowBottom - g.viewBottom, g.rowTop - top);
  }
  return 0;
}

const heightOf = (el: Element | null | undefined) =>
  el instanceof HTMLElement ? el.offsetHeight : 0;

/** Sticky headers that pin over `row`: its project card's header, plus its
 *  worktree group's header when the row sits inside a group (its body or
 *  its peek). Measured, because the group header has no fixed height. */
export function stickyInset(row: Element): number {
  const card = row.closest('.hv-project-card');
  const group = row.closest('.hv-worktree-group');
  return (
    heightOf(card?.querySelector(':scope > .hv-project-card__header')) +
    heightOf(group?.querySelector(':scope > .hv-worktree-group__header'))
  );
}

/** Scroll `scroller` the minimum needed to show `row` clear of the sticky
 *  headers. A no-op while the scroller has no height — the sidebar hidden
 *  (⌘S), where scrolling it would only surprise the user later. */
export function revealRow(row: Element, scroller: HTMLElement): void {
  if (scroller.clientHeight === 0) return;
  const r = row.getBoundingClientRect();
  const v = scroller.getBoundingClientRect();
  const d = revealDelta({
    rowTop: r.top,
    rowBottom: r.bottom,
    viewTop: v.top,
    viewBottom: v.bottom,
    // A stuck header pins to the scroller's CONTENT edge, so it sits below
    // the scroller's top padding, not at its border edge.
    inset:
      (parseFloat(getComputedStyle(scroller).paddingTop) || 0) +
      stickyInset(row),
  });
  if (d !== 0) scroller.scrollTop += d;
}

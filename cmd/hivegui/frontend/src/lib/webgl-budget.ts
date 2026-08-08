// Process-wide WebGL context budget (#magenta-blocks).
//
// Every SessionTerm wants its own xterm WebGL renderer, but browsers /
// WebView2 cap simultaneous WebGL contexts (~16 in Chromium). A grid of
// many tiles blows past that cap on startup: contexts get force-lost,
// the glyph texture-atlas unbinds, and the shader samples uninitialized
// texture memory — the tiles fill with solid magenta blocks.
//
// We cap ourselves well under the browser limit. The first N tiles get
// GPU rendering; the rest fall back to xterm's DOM renderer (visually
// identical, just slower) instead of thrashing the GL context pool.

// ponytail: fixed budget below the Chromium/WebView2 simultaneous-context
// cap (~16). Lower it if magenta still appears on low-end GPUs.
export const WEBGL_CONTEXT_BUDGET = 8;

let active = 0;

// Try to reserve a GL context slot. Returns true if the caller may build
// a WebglAddon, false if the budget is exhausted (caller uses DOM renderer).
export function acquireWebglSlot(budget = WEBGL_CONTEXT_BUDGET): boolean {
  if (active >= budget) return false;
  active++;
  return true;
}

// Release a previously-acquired slot. Floors at 0 so an over-release
// (double dispose) can't hand out phantom slots.
export function releaseWebglSlot(): void {
  if (active > 0) active--;
}

export function activeWebglSlots(): number {
  return active;
}

// Test-only: reset the module-level counter between cases.
export function _resetWebglBudget(): void {
  active = 0;
}

// Sliding-window loss counter for the context-loss storm guard. A WebGL
// context that dies immediately after every reattach loops forever and
// freezes the GUI; this decides when a tile should stop reattaching.
//
// `s` is the tile's mutable state ({ start, count }); `now` is a
// monotonic ms clock. Returns { count, stormed } — stormed=true means
// the tile has lost its context more than `max` times within `windowMs`
// and should give up on WebGL.
// Mutable per-tile state; both fields start absent on a fresh tile.
export interface WebglLossState {
  start?: number;
  count?: number;
}

export function recordWebglLoss(
  s: WebglLossState,
  now: number,
  max: number,
  windowMs: number,
): { count: number; stormed: boolean } {
  if (now - (s.start || 0) > windowMs) {
    s.start = now;
    s.count = 0;
  }
  s.count = (s.count || 0) + 1;
  return { count: s.count, stormed: s.count > max };
}

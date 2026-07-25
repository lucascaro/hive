// Back / forward navigation history over visited sessions.
//
// Pure module: no DOM, no imports, unit-testable — same idiom as
// lib/collapsed.js and lib/minimized.js. Operates on a plain history
// object `{ back: [], fwd: [] }` passed in by the caller (it lives on
// app/state.js as state.nav).
//
// Semantics are VS Code's, not alt-tab's: `back` is a stack of
// DEPARTED session ids, and navigating somewhere new discards the
// forward branch. app/focus.js records a departure from setActive —
// the sole writer of state.activeId — so every way of changing the
// active session is captured, including the paths that never go
// through switchTo (tile mousedown, gridSpatialMove).
//
// Every read skips ids that no longer exist, via the caller's `exists`
// predicate. That is what makes correctness independent of pruning:
// pruneNav only keeps NAV_CAP meaningful, it is not load-bearing.

// Deep enough that no realistic session walk hits it, shallow enough
// that a stale window can't accumulate unbounded ids. ⌘-arrow walking
// a grid pushes one entry per cell, which is the main way to fill it.
export const NAV_CAP = 50;

export function createNavHistory() {
  return { back: [], fwd: [] };
}

// pushNav records leaving `fromId`. Called with the OUTGOING active id
// before it is overwritten. Falsy ids (no active session — an empty
// project, a just-killed session) are not history entries, and a
// repeat of the top entry is a no-op so a re-select of the same
// session can't stack duplicates.
export function pushNav(h, fromId) {
  if (!fromId) return;
  if (h.back[h.back.length - 1] === fromId) return;
  h.back.push(fromId);
  if (h.back.length > NAV_CAP) h.back.shift();
  h.fwd.length = 0;
}

// goBack pops the most recent still-existing departure and returns it,
// pushing the current id onto the forward stack so goForward can undo
// it. Returns null when there is nowhere to go — the caller reports
// that to the user rather than switching.
export function goBack(h, currentId, exists) {
  return step(h.back, h.fwd, currentId, exists);
}

export function goForward(h, currentId, exists) {
  return step(h.fwd, h.back, currentId, exists);
}

// step is goBack and goForward's shared body: they are mirror images,
// differing only in which stack is popped and which is pushed.
function step(from, to, currentId, exists) {
  while (from.length) {
    const id = from.pop();
    if (!exists(id)) continue; // session died while it sat on the stack
    if (id === currentId) continue; // can't "go" to where we already are
    if (currentId && to[to.length - 1] !== currentId) to.push(currentId);
    return id;
  }
  return null;
}

// pruneNav drops ids that no longer exist from both stacks. Reads
// already skip them (see step), so this is housekeeping: without it a
// window that churns through sessions fills NAV_CAP with dead ids and
// the live history silently shortens.
// Filters in place rather than reassigning, so a caller holding a
// reference to h.back / h.fwd can't be left looking at a stale array.
export function pruneNav(h, exists) {
  keepInPlace(h.back, exists);
  keepInPlace(h.fwd, exists);
  return h;
}

function keepInPlace(arr, exists) {
  let w = 0;
  for (let r = 0; r < arr.length; r++) {
    if (exists(arr[r])) arr[w++] = arr[r];
  }
  arr.length = w;
}

// Collapse/expand animation state for the sidebar's two collapsible
// surfaces (project card, worktree group panel).
//
// The animation is a CSS grid 0fr↔1fr transition, which needs the
// collapsing box to CLIP — and clipping is exactly what must not be left
// on permanently: an `overflow: hidden` ancestor becomes the nearest
// scroll container, so the sticky group header inside a project body
// would stick to a box that never scrolls and silently do nothing. So the
// clip is applied only while collapsed (where it is free) and while a
// transition is actually running, which is what this hook reports.
//
// A timer rather than `transitionend`: a surface that mounts already
// collapsed never fires one, and with `prefers-reduced-motion` the
// duration is 0s, where the event may not fire at all. The clip lingering
// a few frames past an instant collapse is invisible; a missed cleanup
// that leaves the clip on forever is not.
import { useEffect, useRef, useState } from 'react';

/** Must match --motion-collapse in theme/tokens.css. */
export const COLLAPSE_MS = 160;

export function useCollapseTransition(collapsed: boolean): boolean {
  const [animating, setAnimating] = useState(false);
  // Seeded with the mount-time value, so mounting in either state is not a
  // transition — and so the effect genuinely READS `collapsed` rather than
  // listing it as a trigger the body never touches.
  const prev = useRef(collapsed);
  useEffect(() => {
    if (prev.current === collapsed) return;
    prev.current = collapsed;
    setAnimating(true);
    const t = setTimeout(() => setAnimating(false), COLLAPSE_MS);
    return () => clearTimeout(t);
  }, [collapsed]);
  return animating;
}

// filterMinimized returns sessions whose id is NOT in minimizedSet.
// Pure helper: extracted so the grid filter can be unit-tested without
// the DOM. minimizedSet is any object with .has(id); usually a Set.
export function filterMinimized(sessions, minimizedSet) {
  if (!minimizedSet || typeof minimizedSet.has !== 'function') return sessions;
  return sessions.filter((s) => !minimizedSet.has(s.id));
}

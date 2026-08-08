// filterMinimized returns sessions whose id is NOT in minimizedSet.
// Pure helper: extracted so the grid filter can be unit-tested without
// the DOM. minimizedSet is any object with .has(id); usually a Set.
// Generic so the caller's session type survives the filter.
export function filterMinimized<T extends { id: string }>(
  sessions: T[],
  minimizedSet: { has(id: string): boolean } | null | undefined,
): T[] {
  if (!minimizedSet || typeof minimizedSet.has !== 'function') return sessions;
  return sessions.filter((s) => !minimizedSet.has(s.id));
}

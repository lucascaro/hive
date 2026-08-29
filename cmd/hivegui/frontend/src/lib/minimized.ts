import { readProjectId } from './wire.js';

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

// filterHidden is filterMinimized plus project-level minimization: a
// session is hidden when its own id is minimized OR the project it
// belongs to is. The two sets stay separate on purpose — restoring a
// project must not forget which of its sessions the user had minimized
// individually — so this is the one place the union is expressed.
export function filterHidden<
  T extends { id: string; projectId?: string; project_id?: string },
>(
  sessions: T[],
  minimizedSessions: { has(id: string): boolean } | null | undefined,
  minimizedProjects: { has(id: string): boolean } | null | undefined,
): T[] {
  const rest = filterMinimized(sessions, minimizedSessions);
  if (!minimizedProjects || typeof minimizedProjects.has !== 'function') {
    return rest;
  }
  return rest.filter((s) => !minimizedProjects.has(readProjectId(s)));
}

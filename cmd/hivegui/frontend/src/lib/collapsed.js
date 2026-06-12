// Persistence helpers for the sidebar's collapsed-project set.
// Mirrors lib/view.js: pure functions, tolerant of garbage input,
// unit-testable without localStorage.

export const COLLAPSED_STORAGE_KEY = 'hive.collapsedProjects';

// raw: the localStorage string (or null). Returns a Set of project id
// strings; anything malformed degrades to an empty/filtered set.
export function loadCollapsed(raw) {
  if (!raw) return new Set();
  try {
    const arr = JSON.parse(raw);
    if (!Array.isArray(arr)) return new Set();
    return new Set(arr.filter((x) => typeof x === 'string' && x !== ''));
  } catch {
    return new Set();
  }
}

export function serializeCollapsed(set) {
  return JSON.stringify([...set]);
}

// Drop ids that no longer correspond to a live project so the stored
// key can't grow forever. Returns { set, changed }.
export function pruneCollapsed(set, projectIds) {
  const live = new Set(projectIds);
  const next = new Set([...set].filter((id) => live.has(id)));
  return { set: next, changed: next.size !== set.size };
}

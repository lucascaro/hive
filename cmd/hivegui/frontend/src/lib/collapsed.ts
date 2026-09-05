// Persistence helpers for the sidebar's per-project id sets — the
// collapsed set and the minimized set. The helpers are key-agnostic:
// they take the raw string and return a Set, so a second key costs a
// constant, not a second module.
// Mirrors lib/view.ts: pure functions, tolerant of garbage input,
// unit-testable without localStorage.

export const COLLAPSED_STORAGE_KEY = 'hive.collapsedProjects';

// Projects the user has minimized out of the main sidebar list into
// the tray at its bottom. Separate key from COLLAPSED_STORAGE_KEY:
// collapsing hides a project's sessions in the sidebar, minimizing
// takes the whole project out of the list and out of grid views.
export const MINIMIZED_PROJECTS_STORAGE_KEY = 'hive.minimizedProjects';

// Suffix a base storage key with the id of the daemon state dir this
// GUI is attached to. Every hivegui process shares ONE webview
// localStorage (WKWebView keys its store on the bundle id, not on the
// socket), while each daemon owns a registry with its own project
// UUIDs — so an unsuffixed key lets one instance prune away another's
// ids as "projects that no longer exist" (#340).
//
// An empty namespace returns the bare key. That case means "we could
// not identify the daemon"; callers must treat it as a signal to stop
// persisting rather than as a usable key — writing the bare key back
// is what re-creates the shared-key bug.
export function namespacedKey(base: string, ns: string): string {
  return ns ? `${base}.${ns}` : base;
}

// raw: the localStorage string (or null). Returns a Set of project id
// strings; anything malformed degrades to an empty/filtered set.
export function loadCollapsed(raw: string | null | undefined): Set<string> {
  if (!raw) return new Set();
  try {
    const arr: unknown = JSON.parse(raw);
    if (!Array.isArray(arr)) return new Set();
    return new Set(
      arr.filter((x): x is string => typeof x === 'string' && x !== ''),
    );
  } catch {
    return new Set();
  }
}

export function serializeCollapsed(set: Iterable<string>): string {
  return JSON.stringify([...set]);
}

// Drop ids that no longer correspond to a live project so the stored
// key can't grow forever. Returns { set, changed }.
export function pruneCollapsed(
  set: ReadonlySet<string>,
  projectIds: Iterable<string>,
): { set: Set<string>; changed: boolean } {
  const live = new Set(projectIds);
  const next = new Set([...set].filter((id) => live.has(id)));
  return { set: next, changed: next.size !== set.size };
}

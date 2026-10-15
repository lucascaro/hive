// Pure helpers for the archived-sessions list (spec 518): filtering and
// grouping, kept out of the component so they can be tested without a DOM.

import type { ArchivedSessionInfo } from '../app/state.js';

export interface ArchivedGroup {
  /** The project id, or '' for sessions with none. */
  key: string;
  name: string;
  items: ArchivedSessionInfo[];
}

/** Case-insensitive substring match over what a row shows: name,
 * project, agent and worktree branch. Every whitespace-separated word
 * must match somewhere, so "api claude" narrows rather than widens. */
export function filterArchived(
  list: readonly ArchivedSessionInfo[],
  query: string,
  projectName: (a: ArchivedSessionInfo) => string,
): ArchivedSessionInfo[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return [...list];
  return list.filter((a) => {
    const hay = [a.name, projectName(a), a.agent ?? '', a.worktree_branch ?? '']
      .join('\n')
      .toLowerCase();
    return words.every((w) => hay.includes(w));
  });
}

/** Group by project, keeping the daemon's newest-first order inside each
 * group and ordering groups by their newest member. */
export function groupArchived(
  list: readonly ArchivedSessionInfo[],
  projectName: (a: ArchivedSessionInfo) => string,
): ArchivedGroup[] {
  const groups = new Map<string, ArchivedGroup>();
  for (const a of list) {
    const key = a.project_id ?? '';
    let g = groups.get(key);
    if (!g) {
      g = { key, name: projectName(a), items: [] };
      groups.set(key, g);
    }
    g.items.push(a);
  }
  return [...groups.values()];
}

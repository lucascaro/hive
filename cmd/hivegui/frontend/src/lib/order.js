// Pure session/project ordering helpers.

import { readProjectId } from './wire.js';

// orderSessions returns sessions sorted by their owning project's
// order, then by intra-project order. Stable for ties. Pure.
export function orderSessions(sessions, projects) {
  const projOrder = new Map();
  for (const p of projects) projOrder.set(p.id, p.order ?? 0);
  const tagged = sessions.map((s, i) => ({
    s,
    i,
    po: projOrder.get(readProjectId(s)) ?? 0,
    so: s.order ?? 0,
  }));
  tagged.sort((a, b) => (a.po - b.po) || (a.so - b.so) || (a.i - b.i));
  return tagged.map((t) => t.s);
}

// sessionsForProject filters and sorts sessions belonging to a
// single project.
export function sessionsForProject(sessions, projectId) {
  return sessions
    .filter((s) => readProjectId(s) === projectId)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
}

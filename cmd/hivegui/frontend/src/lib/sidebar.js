// Pure sidebar-tree model + a tiny DOM renderer.
//
// Splitting "what to show" from "how to render it" lets us unit-test
// the structure without booting xterm or Wails. main.js can keep its
// fancy renderer; this module is what the test harness exercises and
// what a future refactor can converge on.

import { readProjectId } from './wire.js';

// buildSidebarModel returns:
//   [{
//     project: ProjectInfo,
//     active: bool,
//     collapsed: bool,
//     sessions: SessionInfo[]   // sorted by order
//   }, ...]
//
// `sessions` is the flat list from the daemon; `projects` defines
// the display order. `activeProjectId` is the project currently in
// focus. `collapsed` is a Set of project ids the user collapsed.
export function buildSidebarModel({ projects, sessions, activeProjectId, collapsed }) {
  const c = collapsed instanceof Set ? collapsed : new Set(collapsed || []);
  return projects.map((p) => ({
    project: p,
    active: p.id === activeProjectId,
    collapsed: c.has(p.id),
    sessions: sessions
      .filter((s) => readProjectId(s) === p.id)
      .sort((a, b) => (a.order ?? 0) - (b.order ?? 0)),
  }));
}

// renderSidebar builds a <ul> tree from a model. Returns the root UL.
// Each project becomes <li class="project [active] [collapsed]"
// data-pid=...> with its name + a child <ul> of sessions; each
// session is <li class="session [alive] [dead] [attention]" data-sid=...>.
// Tests assert structure; production main.js uses a richer renderer.
export function renderSidebar(doc, model, { attention } = {}) {
  const att = attention instanceof Set ? attention : new Set(attention || []);
  const root = doc.createElement('ul');
  root.className = 'projects';
  for (const node of model) {
    const li = doc.createElement('li');
    li.className = 'project';
    if (node.active) li.classList.add('active');
    if (node.collapsed) li.classList.add('collapsed');
    li.dataset.pid = node.project.id;
    const name = doc.createElement('span');
    name.className = 'project-name';
    name.textContent = node.project.name;
    li.appendChild(name);

    const sub = doc.createElement('ul');
    sub.className = 'sessions';
    for (const s of node.sessions) {
      const sli = doc.createElement('li');
      sli.className = 'session';
      sli.classList.add(s.alive ? 'alive' : 'dead');
      if (att.has(s.id)) sli.classList.add('attention');
      sli.dataset.sid = s.id;
      sli.dataset.pid = node.project.id;
      sli.textContent = s.name;
      sub.appendChild(sli);
    }
    li.appendChild(sub);
    root.appendChild(li);
  }
  return root;
}

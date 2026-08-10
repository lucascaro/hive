// ---------- sidebar render ----------
//
// Moved verbatim from main.js. View/focus callbacks (switchTo,
// switchToProject, confirmAndDeleteProject, renderEmptyState,
// refocusActiveTerm) are injected via initSidebar(deps) — they still
// live in main.ts until later stages.

import { UpdateSession, UpdateProject } from '../bridge.js';
import {
  state,
  saveCollapsed,
  type ProjectInfo,
  type SessionInfo,
} from './state.js';
import { projectsUL, reportFailure } from './dom.js';
import { activeProjectId } from './selectors.js';
import { openLauncher } from './modals/launcher.js';
import { openProjectEditor } from './modals/project-editor.js';
import { beginInlineRename } from './inline-rename.js';

// Per-module, not a shared deps union: sidebar wants refocusActiveTerm
// where view wants focusActiveTerm, and one union type would loosen both.
// Exported so wave 7 can check main.ts's injection site against it.
export interface SidebarDeps {
  switchTo: (id: string) => void;
  switchToProject: (pid: string) => void;
  confirmAndDeleteProject: (p: ProjectInfo) => void;
  renderEmptyState: () => void;
  refocusActiveTerm: () => void;
}

let deps: SidebarDeps = {
  switchTo: () => {},
  switchToProject: () => {},
  confirmAndDeleteProject: () => {},
  renderEmptyState: () => {},
  refocusActiveTerm: () => {},
};

export function initSidebar(injected: SidebarDeps) {
  deps = injected;
}

export function renderSidebar() {
  projectsUL.innerHTML = '';
  const activePID = activeProjectId();
  for (const p of state.projects) {
    projectsUL.appendChild(renderProject(p, activePID));
  }
  deps.renderEmptyState();
}

// updateSidebarSelection toggles the .selected / .active /
// .attention classes on existing DOM nodes without rebuilding them.
// Selection-only or attention-only changes call this instead of
// renderSidebar so consecutive clicks on a session-item still match
// up as a dblclick pair (the rebuild between clicks was eating the
// dblclick because the LI was a different node by the second click).
export function updateSidebarSelection() {
  const activePID = activeProjectId();
  for (const el of projectsUL.querySelectorAll<HTMLElement>('.project')) {
    el.classList.toggle('active', el.dataset.pid === activePID);
  }
  for (const el of projectsUL.querySelectorAll<HTMLElement>('.session-item')) {
    const sid = el.dataset.sid;
    el.classList.toggle('selected', sid === state.activeId);
    // `sid ?? ''` rather than widening the Set: an unset data-sid and the
    // empty string are both absent from state.attention, so this is the
    // same false the old `has(undefined)` produced.
    el.classList.toggle('attention', state.attention.has(sid ?? ''));
  }
  // The switch paths (switchTo / switchToProject / shiftActiveProject)
  // end here without a sidebar rebuild — re-evaluate the empty state
  // so it appears when an empty project is selected and clears when a
  // live session becomes visible again.
  deps.renderEmptyState();
}

function renderProject(p: ProjectInfo, activePID: string): HTMLLIElement {
  const li = document.createElement('li');
  li.className = 'project';
  li.dataset.pid = p.id;
  if (state.collapsed.has(p.id)) li.classList.add('collapsed');
  if (p.id === activePID) li.classList.add('active');
  li.style.setProperty('--project-color', p.color || '#888');
  li.draggable = true;

  const header = document.createElement('div');
  header.className = 'project-header';

  // A real <button> so the caret is keyboard-operable and can carry
  // aria-expanded; :focus-visible shows a ring only for keyboard focus.
  const caret = document.createElement('button');
  caret.type = 'button';
  caret.className = 'caret';
  caret.textContent = '▾';
  const collapsedNow = state.collapsed.has(p.id);
  caret.setAttribute('aria-expanded', String(!collapsedNow));
  caret.setAttribute(
    'aria-label',
    `${collapsedNow ? 'Expand' : 'Collapse'} ${p.name}`,
  );
  caret.addEventListener('click', (e) => {
    e.stopPropagation();
    if (state.collapsed.has(p.id)) state.collapsed.delete(p.id);
    else state.collapsed.add(p.id);
    saveCollapsed();
    renderSidebar();
  });

  const colorEl = document.createElement('span');
  colorEl.className = 'project-color';

  const name = document.createElement('span');
  name.className = 'project-name';
  name.textContent = p.name ?? '';
  name.title = p.cwd ? `${p.name} — ${p.cwd}` : (p.name ?? '');

  const actions = document.createElement('span');
  actions.className = 'project-actions';

  const newBtn = document.createElement('button');
  newBtn.textContent = '+';
  newBtn.title = 'New session in this project';
  newBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    openLauncher(p.id);
  });

  const editBtn = document.createElement('button');
  editBtn.textContent = '✎';
  editBtn.title = 'Edit project';
  editBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    openProjectEditor(p);
  });

  const delBtn = document.createElement('button');
  delBtn.textContent = '✕';
  delBtn.title = 'Delete project';
  delBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    deps.confirmAndDeleteProject(p);
  });

  actions.append(newBtn, editBtn, delBtn);

  header.append(caret, colorEl, name, actions);
  header.addEventListener('click', (e) => {
    // Only fire when clicking the row background, color block, or name —
    // not on buttons / caret / inline inputs. Each of those stops
    // propagation in its own handler so we shouldn't see them here,
    // but be defensive.
    const t = e.target;
    if (!(t instanceof Element)) return;
    if (t.closest('.project-actions') || t === caret) return;
    deps.switchToProject(p.id);
  });
  header.addEventListener('dblclick', (e) => {
    if (e.target === name || e.target === header) beginRenameProject(p, name);
  });
  li.appendChild(header);

  const ul = document.createElement('ul');
  ul.className = 'project-sessions';
  const sessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id) === p.id)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  for (const s of sessions) {
    ul.appendChild(renderSession(s, p.color || '#888'));
  }
  li.appendChild(ul);

  // ---- Drag-to-reorder for projects ----
  // dragstart bubbles, so a session-item drag fires here too after
  // its own handler runs. We must not preventDefault in that case
  // (it would cancel the session drag). For drags that originate on
  // the project chrome (action buttons, rename input) we DO want to
  // abort, since the li itself is the closest draggable.
  li.addEventListener('dragstart', (e) => {
    const t = e.target;
    if (t instanceof Element) {
      if (t.closest('.session-item')) {
        // Bubbled from an inner session drag — leave it alone.
        return;
      }
      if (t.closest('.project-actions') || t.closest('.project-name-input')) {
        e.preventDefault();
        return;
      }
    }
    const dt = e.dataTransfer;
    if (!dt) return;
    dt.effectAllowed = 'move';
    dt.setData('text/x-hive-project', p.id);
    li.classList.add('dragging');
  });
  li.addEventListener('dragend', () => {
    li.classList.remove('dragging');
    document
      .querySelectorAll('.project.drop-above, .project.drop-below')
      .forEach((el) => {
        el.classList.remove('drop-above', 'drop-below');
      });
  });
  li.addEventListener('dragover', (e) => {
    const dt = e.dataTransfer;
    if (!dt?.types.includes('text/x-hive-project')) return;
    e.preventDefault();
    dt.dropEffect = 'move';
    // Use the header's bounds (not the whole li): with sessions
    // expanded, the li is tall, the cursor is almost always above
    // its midpoint, and the indicator would land far from the
    // cursor. Anchoring both the hit-test and the visual to the
    // header keeps them in sync.
    const r = header.getBoundingClientRect();
    const above = e.clientY - r.top < r.height / 2;
    li.classList.toggle('drop-above', above);
    li.classList.toggle('drop-below', !above);
  });
  li.addEventListener('dragleave', (e) => {
    // Only clear when leaving the li entirely; dragover into a child
    // re-fires and re-asserts the right class.
    // `contains(null)` is false, so a relatedTarget that isn't a Node
    // takes the same "left the li" branch it takes today.
    if (!(e.relatedTarget instanceof Node) || !li.contains(e.relatedTarget)) {
      li.classList.remove('drop-above', 'drop-below');
    }
  });
  li.addEventListener('drop', (e) => {
    const dt = e.dataTransfer;
    if (!dt?.types.includes('text/x-hive-project')) return;
    e.preventDefault();
    const pid = dt.getData('text/x-hive-project');
    li.classList.remove('drop-above', 'drop-below');
    if (!pid || pid === p.id) return;
    const r = header.getBoundingClientRect();
    const above = e.clientY - r.top < r.height / 2;
    reorderDroppedProject(pid, p.id, above);
  });
  return li;
}

// reorderDroppedProject converts an above/below drop into the new
// Order index expected by UpdateProject. The daemon's moveProjectLocked
// removes the dragged project then inserts at newOrder, so we
// compensate when the source sits before the target.
function reorderDroppedProject(
  draggedID: string,
  targetID: string,
  above: boolean,
) {
  const ordered = [...state.projects].sort(
    (a, b) => (a.order ?? 0) - (b.order ?? 0),
  );
  const targetIdx = ordered.findIndex((p) => p.id === targetID);
  const draggedIdx = ordered.findIndex((p) => p.id === draggedID);
  if (targetIdx < 0 || draggedIdx < 0) return;
  let newOrder = above ? targetIdx : targetIdx + 1;
  if (draggedIdx < newOrder) newOrder -= 1;
  if (newOrder === draggedIdx) return;
  UpdateProject(draggedID, '', '', '', newOrder).catch(
    reportFailure('reorder project'),
  );
}

function renderSession(s: SessionInfo, projectColor: string): HTMLLIElement {
  const li = document.createElement('li');
  li.className = 'session-item';
  if (s.id === state.activeId) li.classList.add('selected');
  if (!s.alive) li.classList.add('dead');
  if (state.attention.has(s.id)) li.classList.add('attention');
  li.style.setProperty('--session-color', s.color || '#888');
  li.style.setProperty('--project-color', projectColor || '#888');
  li.dataset.sid = s.id;
  li.dataset.pid = s.projectId ?? s.project_id ?? '';
  li.draggable = true;

  const dot = document.createElement('span');
  dot.className = 'dot';

  const name = document.createElement('span');
  name.className = 'name';
  name.textContent = s.name ?? '';

  // Worktree glyph: shown when the session is backed by a git
  // worktree. Tooltip = branch name.
  const wtBranch = s.worktreeBranch ?? s.worktree_branch;
  let glyph: HTMLSpanElement | null = null;
  if (wtBranch) {
    glyph = document.createElement('span');
    glyph.className = 'worktree-glyph';
    glyph.textContent = '⎇';
    glyph.title = `Worktree: ${wtBranch}`;
  }

  const swatch = document.createElement('span');
  swatch.className = 'swatch';
  const colorInput = document.createElement('input');
  colorInput.type = 'color';
  colorInput.value = s.color || '#888888';
  // Read the value off the in-scope input rather than narrowing e.target:
  // the listener is bound to this element, so colorInput IS the target.
  colorInput.addEventListener('input', () => {
    UpdateSession(s.id, '', colorInput.value, -1).catch(
      reportFailure('color change'),
    );
  });
  swatch.appendChild(colorInput);

  if (glyph) {
    li.append(dot, name, glyph, swatch);
  } else {
    li.append(dot, name, swatch);
  }
  li.addEventListener('click', (e) => {
    if (e.target === colorInput || e.target === swatch) return;
    deps.switchTo(s.id);
  });
  li.addEventListener('dblclick', () => beginRenameSession(s, li, name));

  // ---- Drag-to-reorder ----
  // Same-project drops only; cross-project moves are not supported
  // yet (would require also updating project_id on the wire).
  li.addEventListener('dragstart', (e) => {
    const dt = e.dataTransfer;
    if (!dt) return;
    dt.effectAllowed = 'move';
    dt.setData('text/x-hive-session', s.id);
    li.classList.add('dragging');
  });
  li.addEventListener('dragend', () => {
    li.classList.remove('dragging');
    document
      .querySelectorAll('.session-item.drop-above, .session-item.drop-below')
      .forEach((el) => {
        el.classList.remove('drop-above', 'drop-below');
      });
  });
  li.addEventListener('dragover', (e) => {
    const dt = e.dataTransfer;
    if (!dt?.types.includes('text/x-hive-session')) return;
    e.preventDefault();
    dt.dropEffect = 'move';
    const r = li.getBoundingClientRect();
    const above = e.clientY - r.top < r.height / 2;
    li.classList.toggle('drop-above', above);
    li.classList.toggle('drop-below', !above);
  });
  li.addEventListener('dragleave', () => {
    li.classList.remove('drop-above', 'drop-below');
  });
  li.addEventListener('drop', (e) => {
    e.preventDefault();
    const sid = e.dataTransfer?.getData('text/x-hive-session');
    li.classList.remove('drop-above', 'drop-below');
    if (!sid || sid === s.id) return;
    const dragged = state.sessions.find((x) => x.id === sid);
    if (!dragged) return;
    const draggedPID = dragged.projectId ?? dragged.project_id ?? '';
    const targetPID = s.projectId ?? s.project_id ?? '';
    if (draggedPID !== targetPID) return; // cross-project: not supported yet
    const r = li.getBoundingClientRect();
    const above = e.clientY - r.top < r.height / 2;
    reorderDroppedSession(sid, s.id, above);
  });
  return li;
}

// reorderDroppedSession converts a drop position ("above" or "below"
// the target row) into a global Order argument for UpdateSession.
// The daemon's moveLocked treats the argument as a global index into
// r.order; we pick the global Order of whichever neighbor sits at
// the project-relative drop slot (after pretending the dragged
// session is gone).
function reorderDroppedSession(
  draggedID: string,
  targetID: string,
  above: boolean,
) {
  const target = state.sessions.find((s) => s.id === targetID);
  if (!target) return;
  const projID = target.projectId ?? target.project_id ?? '';
  const projSessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id ?? '') === projID)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  const targetIdx = projSessions.findIndex((s) => s.id === targetID);
  if (targetIdx < 0) return;
  let projIdx = above ? targetIdx : targetIdx + 1;
  const pretend = projSessions.filter((s) => s.id !== draggedID);
  if (pretend.length === 0) return;
  if (projIdx > pretend.length) projIdx = pretend.length;

  // Find the global index in r.order that we want the dragged session
  // to land at. We approximate using global Order values: pretend[i]
  // currently has some Order value, and moveLocked accepts a global
  // index. Easiest: walk the global ordered list of all sessions and
  // count to the slot we want.
  const globalOrdered = [...state.sessions].sort(
    (a, b) => (a.order ?? 0) - (b.order ?? 0),
  );
  let globalTargetIdx: number;
  if (projIdx >= pretend.length) {
    // Drop after the last neighbor: land just past it.
    const last = pretend[pretend.length - 1];
    globalTargetIdx = globalOrdered.findIndex((x) => x.id === last.id) + 1;
  } else {
    const neighbor = pretend[projIdx];
    globalTargetIdx = globalOrdered.findIndex((x) => x.id === neighbor.id);
  }
  if (globalTargetIdx < 0) return;
  // moveLocked is "remove from current pos, then insert at newOrder"
  // — so if dragged is currently *before* the target index, the
  // index shifts by 1 after removal. Compensate.
  const draggedGlobalIdx = globalOrdered.findIndex((x) => x.id === draggedID);
  if (draggedGlobalIdx >= 0 && draggedGlobalIdx < globalTargetIdx) {
    globalTargetIdx -= 1;
  }
  UpdateSession(draggedID, '', '', globalTargetIdx).catch(
    reportFailure('reorder'),
  );
}

function beginRenameSession(
  sess: SessionInfo,
  _li: HTMLLIElement,
  nameEl: HTMLSpanElement,
) {
  beginInlineRename({
    className: 'name-input',
    value: sess.name ?? '',
    mount: (input) => nameEl.replaceWith(input),
    unmount: (input) => input.replaceWith(nameEl),
    onCommit: (next) =>
      UpdateSession(sess.id, next, '', -1).catch(reportFailure('rename')),
    onDone: () => deps.refocusActiveTerm(),
  });
}

function beginRenameProject(proj: ProjectInfo, nameEl: HTMLSpanElement) {
  beginInlineRename({
    className: 'project-name-input',
    value: proj.name ?? '',
    mount: (input) => nameEl.replaceWith(input),
    unmount: (input) => input.replaceWith(nameEl),
    onCommit: (next) =>
      UpdateProject(proj.id, next, '', '', -1).catch(
        reportFailure('rename project'),
      ),
    onDone: () => deps.refocusActiveTerm(),
  });
}

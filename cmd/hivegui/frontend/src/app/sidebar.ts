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
import { projectsUL, minimizedProjectsUL, reportFailure } from './dom.js';
import { phaseOf, isReady, isStarting, isClosing } from '../lib/phase-steps.js';
import { activeProjectId } from './selectors.js';
import { openLauncher } from './modals/launcher.js';
import { openProjectEditor } from './modals/project-editor.js';
import { openWorktrees } from './modals/worktrees.js';
import { beginInlineRename } from './inline-rename.js';
import { readProjectId } from '../lib/wire.js';
import { displayTitle } from '../lib/term-title.js';

// Per-module, not a shared deps union: sidebar wants refocusActiveTerm
// where view wants focusActiveTerm, and one union type would loosen both.
// Exported so wave 7 can check main.ts's injection site against it.
export interface SidebarDeps {
  switchTo: (id: string) => void;
  switchToProject: (pid: string) => void;
  // Owned by view.ts, not sidebar.ts: minimizing a project repaints
  // the grid and can move focus, and view.ts already imports this
  // module — a direct import back would close the cycle.
  minimizeProject: (pid: string) => void;
  restoreProject: (pid: string) => void;
  // The session-level twin of the pair above, same owner (view.ts) and
  // same reason: minimizing repaints the grid and can move focus.
  minimizeSession: (id: string) => void;
  restoreSession: (id: string) => void;
  confirmAndDeleteProject: (p: ProjectInfo) => void;
  renderEmptyState: () => void;
  refocusActiveTerm: () => void;
}

let deps: SidebarDeps = {
  switchTo: () => {},
  switchToProject: () => {},
  minimizeProject: () => {},
  restoreProject: () => {},
  minimizeSession: () => {},
  restoreSession: () => {},
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
    if (state.minimizedProjects.has(p.id)) continue;
    projectsUL.appendChild(renderProject(p, activePID));
  }
  renderMinimizedProjects(activePID);
  deps.renderEmptyState();
}

// renderMinimizedProjects rebuilds the name-only chip list pinned to
// the bottom of the sidebar. Chips render in project order — the same
// order the rows would have if nothing were minimized — so restoring
// one is visibly a no-op on ordering.
function renderMinimizedProjects(activePID: string) {
  const tray = minimizedProjectsUL;
  // pageEl, so a jsdom test that mounts only #projects still renders.
  if (!tray) return;
  tray.innerHTML = '';
  const minimized = state.projects.filter((p) =>
    state.minimizedProjects.has(p.id),
  );
  tray.classList.toggle('hidden', minimized.length === 0);
  for (const p of minimized) {
    tray.appendChild(renderProjectChip(p, activePID));
  }
}

function renderProjectChip(p: ProjectInfo, activePID: string): HTMLLIElement {
  const li = document.createElement('li');
  li.className = 'min-project-chip';
  li.dataset.pid = p.id;
  li.style.setProperty('--project-color', p.color || '#888');
  if (p.id === activePID) li.classList.add('active');

  // A real <button>, like the session tray's chip (app/view.ts): the
  // chip body is an action, and on a bare <li> it would be mouse-only.
  const open = document.createElement('button');
  open.type = 'button';
  open.className = 'min-project-open';
  open.title = p.cwd ? `${p.name} — ${p.cwd}` : (p.name ?? '');
  open.setAttribute('aria-label', `Restore ${p.name}`);
  // Clicking the row restores the project — the same thing the ＋
  // does. A minimized row is a thing you put away; the only reason to
  // click it is to get it back, so making the whole row the target
  // beats a 12px button as the sole way out.
  open.addEventListener('click', () => deps.restoreProject(p.id));

  const dot = document.createElement('span');
  dot.className = 'min-project-color';

  const name = document.createElement('span');
  name.className = 'min-project-name';
  name.textContent = p.name ?? '';
  open.append(dot, name);

  const restore = document.createElement('button');
  restore.type = 'button';
  restore.className = 'min-project-restore';
  restore.textContent = '＋';
  restore.title = `Restore ${p.name}`;
  restore.setAttribute('aria-label', `Restore ${p.name}`);
  restore.addEventListener('click', (e) => {
    e.stopPropagation();
    deps.restoreProject(p.id);
  });

  li.append(open, restore);
  return li;
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
  // The chips need the same treatment as the rows: switching projects
  // (chip click, ⌘[ / ⌘], any switchTo) repaints selection without a
  // rebuild, so a chip that only learned its state at render time would
  // keep a stale highlight and lie about which project is current.
  for (const el of minimizedProjectsUL?.querySelectorAll<HTMLElement>(
    '.min-project-chip',
  ) ?? []) {
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

// updateSidebarTitles patches the window-title line on existing rows
// instead of rebuilding them, for the same reason updateSidebarSelection
// exists: renderSidebar wipes projectsUL.innerHTML and recreates every
// node and listener. Titles change as often as the running program
// decides to change them — an agent rewrites its title as it works — so
// routing them through a full rebuild would thrash the sidebar and eat
// dblclick pairs (see the comment on updateSidebarSelection).
export function updateSidebarTitles() {
  for (const el of projectsUL.querySelectorAll<HTMLElement>('.session-item')) {
    const s = state.sessions.find((x) => x.id === el.dataset.sid);
    const slot = el.querySelector<HTMLElement>('.session-title');
    if (!s || !slot) continue;
    applyTitle(slot, s);
  }
}

// applyTitle writes one row's title slot. Shared by the initial render
// and the in-place update so the two can't drift on the suppression rule
// or the tooltip.
function applyTitle(slot: HTMLElement, s: SessionInfo) {
  const t = displayTitle(s.title, s.name);
  slot.textContent = t;
  slot.title = t;
  // hidden rather than a class toggle: an empty title must not leave the
  // row taller than a titleless one, and `hidden` is the one signal that
  // also keeps the text out of the accessibility tree.
  slot.hidden = t === '';
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

  const wtBtn = document.createElement('button');
  wtBtn.textContent = '⎇';
  // The binding is shown inline, per the key-discoverability rule.
  wtBtn.title = 'Worktrees in this project (⌘E)';
  wtBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    openWorktrees(p);
  });

  const editBtn = document.createElement('button');
  editBtn.textContent = '✎';
  editBtn.title = 'Edit project';
  editBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    openProjectEditor(p);
  });

  const minBtn = document.createElement('button');
  // Same glyph as the grid tile's minimize control
  // (app/session-term.ts) — one gesture, two scopes.
  minBtn.textContent = '–';
  minBtn.title = 'Minimize project (hide from sidebar and grid)';
  minBtn.setAttribute('aria-label', `Minimize ${p.name}`);
  minBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    deps.minimizeProject(p.id);
  });

  const delBtn = document.createElement('button');
  delBtn.textContent = '✕';
  delBtn.title = 'Delete project';
  delBtn.addEventListener('click', (e) => {
    e.stopPropagation();
    deps.confirmAndDeleteProject(p);
  });

  actions.append(newBtn, wtBtn, editBtn, minBtn, delBtn);

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
  // A session that hasn't finished starting isn't dead — it has no PTY
  // yet. Marking it dead would grey the row out for the whole create.
  const phase = phaseOf(s);
  if (!s.alive && isReady(phase)) li.classList.add('dead');
  if (isStarting(phase)) li.classList.add('starting');
  if (isClosing(phase)) li.classList.add('closing');
  if (state.attention.has(s.id)) li.classList.add('attention');
  li.style.setProperty('--session-color', s.color || '#888');
  li.style.setProperty('--project-color', projectColor || '#888');
  li.dataset.sid = s.id;
  li.dataset.pid = s.projectId ?? s.project_id ?? '';
  li.draggable = true;

  const dot = document.createElement('span');
  dot.className = 'dot';
  if (!isReady(phase)) {
    // Same vocabulary as the tile's loading panel, so the row and the
    // pane never disagree about what the session is doing.
    dot.title = isClosing(phase) ? 'Closing…' : 'Starting…';
  }

  const name = document.createElement('span');
  name.className = 'name';
  name.textContent = s.name ?? '';

  // The window title the running program published (OSC 0/2), shown
  // under the name so the row says what the session is *doing*, not just
  // what it was called at creation. Hidden when there is no title, so a
  // titleless row keeps exactly the height it has always had.
  const titleEl = document.createElement('span');
  titleEl.className = 'session-title';
  applyTitle(titleEl, s);

  // Name and title stack; the dot, worktree glyph and swatch stay
  // centered against the pair. The wrapper carries min-width: 0 so the
  // ellipsis on both lines still works inside the flex row.
  const text = document.createElement('span');
  text.className = 'session-text';
  text.append(name, titleEl);

  // Worktree glyph: shown when the session is backed by a git
  // worktree. Tooltip = branch name.
  const wtBranch = s.worktreeBranch ?? s.worktree_branch;
  let glyph: HTMLSpanElement | null = null;
  if (wtBranch) {
    glyph = document.createElement('span');
    glyph.className = 'worktree-glyph clickable';
    glyph.textContent = '⎇';
    // The same glyph marks the project row's worktree button, so it
    // has to do the same thing here — an indicator that looks like the
    // control next to it but ignores clicks reads as broken.
    glyph.title = `Worktree: ${wtBranch} — click to manage worktrees`;
    glyph.setAttribute('role', 'button');
    glyph.addEventListener('click', (e) => {
      // Don't also switch to the session the glyph sits on.
      e.stopPropagation();
      const proj = state.projects.find((p) => p.id === readProjectId(s));
      if (proj) openWorktrees(proj);
    });
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

  // The same control the grid tile carries (app/session-term.ts), on
  // the row — so a session can be pushed out of the grid without first
  // finding its tile. It toggles, because once a row is minimized the
  // tray chip is the only way back and the row is right here.
  const isMin = state.minimized.has(s.id);
  if (isMin) li.classList.add('minimized');
  const minBtn = document.createElement('button');
  minBtn.type = 'button';
  minBtn.className = 'session-minimize';
  minBtn.textContent = isMin ? '＋' : '–';
  minBtn.title = isMin ? 'Restore to the grid' : 'Minimize (hide from grid)';
  minBtn.setAttribute(
    'aria-label',
    `${isMin ? 'Restore' : 'Minimize'} ${s.name ?? 'session'}`,
  );
  minBtn.addEventListener('click', (e) => {
    // Don't also switch to the session the button sits on.
    e.stopPropagation();
    if (isMin) deps.restoreSession(s.id);
    else deps.minimizeSession(s.id);
  });

  if (glyph) {
    li.append(dot, text, glyph, minBtn, swatch);
  } else {
    li.append(dot, text, minBtn, swatch);
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
//
// Kept separate from lib/reorder.ts's reorderTarget on purpose: that one
// answers "swap with the adjacent sibling" (⇧⌘↑/↓) and can name the
// target's own .order directly, while a drop can land between any two
// rows and needs the shift compensation below. Both rely on the same
// invariant — .order IS the index into the daemon's r.order — so if you
// change that assumption, change it in both.
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

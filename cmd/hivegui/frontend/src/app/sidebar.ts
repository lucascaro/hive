// ---------- sidebar render ----------
//
// Moved verbatim from main.js. View/focus callbacks (switchTo,
// switchToProject, confirmAndDeleteProject, renderEmptyState,
// refocusActiveTerm) are injected via initSidebar(deps) — they still
// live in main.ts until later stages.

import {
  UpdateSession,
  UpdateProject,
  RestartSession,
  KillSession,
  Confirm,
} from '../bridge.js';
import { noteLocalClose } from './undo-close.js';
import { state, type ProjectInfo, type SessionInfo } from './state.js';
import { toggleCollapsed } from '../store/store.js';
import { projectsUL, minimizedProjectsUL, reportFailure } from './dom.js';
import { chip } from '../ui/chip.js';
import { sessionRow, updateSessionRow } from '../ui/session-row.js';
import {
  projectCard,
  updateProjectCard,
  type ProjectCardState,
} from '../ui/project-card.js';
import { sessionState } from '../lib/session-state.js';
import { activeProjectId, orderedSessions } from './selectors.js';
import { openLauncher } from './modals/launcher.js';
import { openProjectEditor } from './modals/project-editor.js';
import { openWorktrees } from './modals/worktrees.js';
import { beginInlineRename } from './inline-rename.js';
import { readProjectId } from '../lib/wire.js';
import { preserveFocus } from '../lib/preserve-focus.js';
import { dropTargetIndex } from '../lib/reorder.js';
import {
  beginDrag,
  moveTo as movePlaceholder,
  endDrag,
} from '../lib/drag-placeholder.js';

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

// renderSidebar rebuilds every node under #projects. It is the structural
// path — a project or session appearing, disappearing or changing order —
// and it is wrapped in preserveFocus because `innerHTML = ''` destroys the
// focused element outright and the browser drops focus to <body>.
// State-only changes must NOT come here; see updateSidebarRows.
export function renderSidebar() {
  preserveFocus(projectsUL, () => {
    projectsUL.innerHTML = '';
    const activePID = activeProjectId();
    const hints = keyHints();
    const indexOf = (id: string) => hints.get(id) ?? null;
    for (const p of state.projects) {
      if (state.minimizedProjects.has(p.id)) continue;
      projectsUL.appendChild(renderProject(p, activePID, indexOf));
    }
    renderMinimizedProjects(activePID);
    deps.renderEmptyState();
  });
}

// sidebarShape is the ordered list of what renderSidebar would put in the
// DOM — one entry per project card and per session row, in render order.
// Both callers below derive it from the SAME traversal renderSidebar uses,
// so the in-place path can never disagree with the rebuild about whether
// the shape moved.
function sidebarShape(): string[] {
  const out: string[] = [];
  for (const p of state.projects) {
    if (state.minimizedProjects.has(p.id)) continue;
    out.push(`p:${p.id}`);
    for (const s of state.sessions
      .filter((s) => readProjectId(s) === p.id)
      .sort((a, b) => (a.order ?? 0) - (b.order ?? 0)))
      out.push(`s:${s.id}`);
  }
  return out;
}

// domShape reads the same list back off the DOM.
function domShape(): string[] {
  return Array.from(
    projectsUL.querySelectorAll<HTMLElement>(
      '.hv-project-card, .hv-session-row',
    ),
  ).map((el) =>
    el.classList.contains('hv-project-card')
      ? `p:${el.dataset.pid ?? ''}`
      : `s:${el.dataset.sid ?? ''}`,
  );
}

// updateSidebarRows is the state-only path: it patches the existing nodes
// and rebuilds only when the sidebar's shape actually moved.
//
// `session:event` kind `updated` is the hot one. The daemon emits it on
// every phase step (starting -> worktree -> ready), once per surviving
// session when a kill recompacts the order, and whenever the 200ms
// agent-session-id capture poll finally lands — which can be up to 30s
// after a spawn (internal/registry/create.go). Routing each of those
// through renderSidebar wiped and rebuilt the whole list, which destroyed
// whatever the user had focused and ate dblclick pairs mid-rename. The
// same reasoning already routes `title` events through updateSidebarTitles.
export function updateSidebarRows() {
  const want = sidebarShape();
  const have = domShape();
  if (want.length !== have.length || want.some((v, i) => have[i] !== v)) {
    renderSidebar();
    return;
  }
  updateSidebarSelection();
}

// keyHints maps a session id to the digit ⌘n actually selects it with.
// app/keyboard.ts resolves ⌘n against orderedSessions()[n-1], so the hint
// has to be read off the same list — a per-project counter would label
// rows with keys that jump somewhere else entirely.
function keyHints(): Map<string, number> {
  const hints = new Map<string, number>();
  orderedSessions()
    .slice(0, 9)
    .forEach((s, i) => {
      hints.set(s.id, i + 1);
    });
  return hints;
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

// projectHasAttention reports whether any session in the project is
// ringing. A minimized project has no session rows in the sidebar, so
// the chip is the only surface left to carry the bell — without this a
// BEL inside a minimized project is invisible until ⌘B finds it.
function projectHasAttention(pid: string): boolean {
  return state.sessions.some(
    (s) => readProjectId(s) === pid && state.attention.has(s.id),
  );
}

function renderProjectChip(p: ProjectInfo, activePID: string): HTMLSpanElement {
  const el = chip({
    label: p.name ?? '',
    color: p.color,
    active: p.id === activePID,
    title: p.cwd ? `${p.name} — ${p.cwd}` : (p.name ?? ''),
    ariaLabel: `Restore ${p.name}`,
    // Clicking the chip body restores the project — the same thing the
    // restore control does. A minimized row is a thing you put away; the
    // only reason to click it is to get it back.
    onClick: () => deps.restoreProject(p.id),
    onRestore: () => deps.restoreProject(p.id),
    restoreLabel: `Restore ${p.name}`,
  });
  el.dataset.pid = p.id;
  // The chip is the only surface left carrying a bell for a project whose
  // rows are gone (patterns.md › Attention bubbling).
  if (projectHasAttention(p.id)) el.dataset.state = 'attention';
  return el;
}

// updateSidebarSelection re-applies selection and attention to the
// existing card and row nodes without rebuilding them. Selection-only or
// attention-only changes call this instead of renderSidebar so
// consecutive clicks on a session row still match up as a dblclick pair
// (the rebuild between clicks was eating the dblclick because the LI was
// a different node by the second click).
export function updateSidebarSelection() {
  const activePID = activeProjectId();
  for (const el of projectsUL.querySelectorAll<HTMLElement>(
    '.hv-project-card',
  )) {
    const pid = el.dataset.pid ?? '';
    const p = state.projects.find((x) => x.id === pid);
    // Every field of the card's state is recomputed, not just `active`:
    // attention on a card is the union of its sessions', so a bell that
    // arrives without a rebuild would otherwise leave a collapsed card
    // silent and its count line stale.
    updateProjectCard(
      el,
      p?.name ?? 'project',
      projectCardState(pid, activePID),
    );
  }
  // The chips need the same treatment as the rows: switching projects
  // (chip click, ⌘[ / ⌘], any switchTo) repaints selection without a
  // rebuild, so a chip that only learned its state at render time would
  // keep a stale highlight and lie about which project is current.
  for (const el of minimizedProjectsUL?.querySelectorAll<HTMLElement>(
    '.hv-chip',
  ) ?? []) {
    if (el.dataset.pid === activePID) el.dataset.active = '';
    else delete el.dataset.active;
    if (projectHasAttention(el.dataset.pid ?? ''))
      el.dataset.state = 'attention';
    else delete el.dataset.state;
  }
  patchRows();
  // The switch paths (switchTo / switchToProject / shiftActiveProject)
  // end here without a sidebar rebuild — re-evaluate the empty state
  // so it appears when an empty project is selected and clears when a
  // live session becomes visible again.
  deps.renderEmptyState();
}

// projectCardState derives one card's whole visual state from `state`, so
// the build path and the in-place patch path can never disagree about it.
function projectCardState(pid: string, activePID: string): ProjectCardState {
  const sessions = state.sessions.filter((s) => readProjectId(s) === pid);
  const attentionCount = sessions.filter((s) =>
    state.attention.has(s.id),
  ).length;
  return {
    collapsed: state.collapsed.has(pid),
    active: pid === activePID,
    attention: attentionCount > 0,
    sessionCount: sessions.length,
    attentionCount,
  };
}

// patchRows re-applies every row's state from `state` without rebuilding a
// node — the invariant updateSidebarSelection was created for: a rebuild
// between two clicks replaces the <li> and the dblclick pair that starts a
// rename never forms.
function patchRows() {
  const hints = keyHints();
  for (const el of projectsUL.querySelectorAll<HTMLLIElement>(
    '.hv-session-row',
  )) {
    const s = state.sessions.find((x) => x.id === el.dataset.sid);
    if (!s) continue;
    updateSessionRow(el, s, {
      state: sessionState(s, state.attention.has(s.id)),
      selected: s.id === state.activeId,
      minimized: state.minimized.has(s.id),
      index: hints.get(s.id) ?? null,
    });
  }
}

// updateSidebarTitles re-applies existing rows' state — the title line
// included — instead of rebuilding them, for the same reason
// updateSidebarSelection exists: renderSidebar wipes projectsUL.innerHTML
// and recreates every node and listener. Titles change as often as the
// running program decides to change them — an agent rewrites its title as
// it works — so routing them through a full rebuild would thrash the
// sidebar and eat dblclick pairs (see the comment on
// updateSidebarSelection). updateSessionRow writes only what actually
// changed, so a title-only event costs a text assignment per row.
export function updateSidebarTitles() {
  patchRows();
}

function renderProject(
  p: ProjectInfo,
  activePID: string,
  indexOf: (id: string) => number | null,
): HTMLLIElement {
  const sessions = state.sessions
    .filter((s) => readProjectId(s) === p.id)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));

  const { root, header, body, name } = projectCard({
    project: p,
    ...projectCardState(p.id, activePID),
    onSelect: () => deps.switchToProject(p.id),
    onToggleCollapse: () => {
      toggleCollapsed(p.id);
      renderSidebar();
    },
    onNewSession: () => openLauncher(p.id),
    onMinimize: () => deps.minimizeProject(p.id),
    onWorktrees: () => openWorktrees(p),
    onEdit: () => openProjectEditor(p),
    onDelete: () => deps.confirmAndDeleteProject(p),
  });

  header.addEventListener('dblclick', (e) => {
    if (e.target === name || e.target === header) beginRenameProject(p, name);
  });

  for (const s of sessions) body.appendChild(renderSession(s, indexOf(s.id)));

  wireProjectDrag(root, header, p);
  return root;
}

// ---- Drag-to-reorder for projects ----
// dragstart bubbles, so a session-row drag fires here too after its own
// handler runs. We must not preventDefault in that case (it would cancel
// the session drag). For drags that originate on the project chrome
// (action buttons, rename input) we DO want to abort, since the card
// itself is the closest draggable.
function wireProjectDrag(
  root: HTMLLIElement,
  header: HTMLElement,
  p: ProjectInfo,
) {
  const commit = (target: HTMLElement, above: boolean, e: DragEvent) => {
    const pid = e.dataTransfer?.getData('text/x-hive-project');
    const targetPID = target.dataset.pid ?? '';
    if (!pid || !targetPID || pid === targetPID) return;
    reorderDroppedProject(pid, targetPID, above);
  };

  root.addEventListener('dragstart', (e) => {
    const t = e.target;
    if (t instanceof Element) {
      if (t.closest('.hv-session-row')) {
        // Bubbled from an inner session drag — leave it alone.
        return;
      }
      if (
        t.closest('.hv-project-card__actions') ||
        t.closest('.project-name-input')
      ) {
        e.preventDefault();
        return;
      }
    }
    const dt = e.dataTransfer;
    if (!dt) return;
    dt.effectAllowed = 'move';
    dt.setData('text/x-hive-project', p.id);
    beginDrag(root, commit);
  });
  root.addEventListener('dragend', (e) => {
    if (e.target instanceof Element && e.target.closest('.hv-session-row')) {
      // Bubbled from an inner session drag, which owns its own teardown.
      return;
    }
    endDrag();
  });
  root.addEventListener('dragover', (e) => {
    const dt = e.dataTransfer;
    if (!dt?.types.includes('text/x-hive-project')) return;
    e.preventDefault();
    dt.dropEffect = 'move';
    // Use the header's bounds (not the whole card): with sessions
    // expanded, the card is tall, the cursor is almost always above
    // its midpoint, and the placeholder would land far from the
    // cursor. Anchoring both the hit-test and the visual to the
    // header keeps them in sync.
    const r = header.getBoundingClientRect();
    movePlaceholder(root, e.clientY - r.top < r.height / 2);
  });
  root.addEventListener('drop', (e) => {
    const dt = e.dataTransfer;
    if (!dt?.types.includes('text/x-hive-project')) return;
    e.preventDefault();
    const r = header.getBoundingClientRect();
    const above = e.clientY - r.top < r.height / 2;
    endDrag();
    commit(root, above, e);
  });
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

function renderSession(s: SessionInfo, index: number | null): HTMLLIElement {
  // These callbacks outlive the SessionInfo they were built from. Rows are
  // no longer rebuilt on every `updated` (see updateSidebarRows), so a
  // handler that closed over `s` would keep answering from whatever the
  // session looked like when the row was first drawn — and `s` at that
  // moment is typically a session still in phase `starting`, before the
  // daemon has reported it alive or given it a worktree. killSession in
  // particular branches on `alive`, and a stale `false` there sends
  // force=true, which skips the dirty-worktree refusal entirely.
  // So: capture the id, read the session at call time.
  const live = (): SessionInfo =>
    state.sessions.find((x) => x.id === s.id) ?? s;

  const li = sessionRow({
    session: s,
    state: sessionState(s, state.attention.has(s.id)),
    selected: s.id === state.activeId,
    minimized: state.minimized.has(s.id),
    index,
    onSelect: () => deps.switchTo(s.id),
    onMinimize: () => deps.minimizeSession(s.id),
    onRestore: () => deps.restoreSession(s.id),
    onRestart: () =>
      RestartSession(s.id).catch(reportFailure('restart session')),
    onKill: () => killSession(live()),
    onWorktrees: () => {
      const proj = state.projects.find((p) => p.id === readProjectId(live()));
      if (proj) openWorktrees(proj);
    },
    onColor: (hex) =>
      UpdateSession(s.id, '', hex, -1).catch(reportFailure('color change')),
  });

  const name = li.querySelector<HTMLElement>('.hv-session-row__name');
  if (name) {
    li.addEventListener('dblclick', () => beginRenameSession(live(), name));
  }
  wireSessionDrag(li, s);
  return li;
}

// killSession routes a live session through the native confirm (AGENTS.md:
// destructive actions never skip it) and a dead one straight through, which
// is the rule session-term.ts's _closeDead already follows — there is
// nothing left to lose once the process is gone.
//
// force=false on the live branch, like every other kill path (main.ts's
// close-session command, ⌘W, the menu): it lets the daemon refuse with
// worktree_dirty so events.ts can ask the real three-way question
// (cancel / close / close and delete the worktree). Forcing here would
// make this confirm — which only mentions scrollback — silently agree to
// throwing away uncommitted changes. The dead branch keeps force=true:
// there is no process to refuse and no worktree state worth guarding.
function killSession(s: SessionInfo) {
  const alive = s.alive !== false;
  if (!alive) {
    KillSession(s.id, true).catch(reportFailure('kill session'));
    return;
  }
  Confirm(
    'Kill session',
    `Kill ${s.name ?? 'this session'}? Its scrollback is lost.`,
  )
    .then((ok) => {
      if (!ok) return;
      noteLocalClose(s.id, s.name ?? 'Session');
      KillSession(s.id, false).catch(reportFailure('kill session'));
    })
    .catch(reportFailure('kill session'));
}

// ---- Drag-to-reorder ----
// Same-project drops only; cross-project moves are not supported
// yet (would require also updating project_id on the wire).
function wireSessionDrag(li: HTMLLIElement, s: SessionInfo) {
  // Shared by the row's own drop handler and the placeholder's: an "insert
  // above" spacer sits under the cursor, so the release often lands on the
  // spacer rather than on any row.
  const commit = (target: HTMLElement, above: boolean, e: DragEvent) => {
    const sid = e.dataTransfer?.getData('text/x-hive-session');
    const targetSID = target.dataset.sid ?? '';
    if (!sid || !targetSID || sid === targetSID) return;
    const dragged = state.sessions.find((x) => x.id === sid);
    const dropped = state.sessions.find((x) => x.id === targetSID);
    if (!dragged || !dropped) return;
    // cross-project: not supported yet (would need project_id on the wire).
    if (readProjectId(dragged) !== readProjectId(dropped)) return;
    reorderDroppedSession(sid, targetSID, above);
  };

  li.addEventListener('dragstart', (e) => {
    const dt = e.dataTransfer;
    if (!dt) return;
    dt.effectAllowed = 'move';
    dt.setData('text/x-hive-session', s.id);
    beginDrag(li, commit);
  });
  li.addEventListener('dragend', endDrag);
  li.addEventListener('dragover', (e) => {
    const dt = e.dataTransfer;
    if (!dt?.types.includes('text/x-hive-session')) return;
    e.preventDefault();
    dt.dropEffect = 'move';
    const r = li.getBoundingClientRect();
    movePlaceholder(li, e.clientY - r.top < r.height / 2);
  });
  li.addEventListener('drop', (e) => {
    e.preventDefault();
    const r = li.getBoundingClientRect();
    const above = e.clientY - r.top < r.height / 2;
    endDrag();
    commit(li, above, e);
  });
}

// reorderDroppedSession hands the drop to lib/reorder.ts's dropTargetIndex
// and forwards the result. The index math lives there, next to the keyboard
// path's reorderTarget, because both rest on the same invariant — a session's
// .order IS its index in the daemon's r.order — and because a pure function
// is the only way to table-test the off-by-one this used to have.
function reorderDroppedSession(
  draggedID: string,
  targetID: string,
  above: boolean,
) {
  const order = dropTargetIndex(state.sessions, draggedID, targetID, above);
  if (order === null) return;
  UpdateSession(draggedID, '', '', order).catch(reportFailure('reorder'));
}

function beginRenameSession(sess: SessionInfo, nameEl: HTMLElement) {
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

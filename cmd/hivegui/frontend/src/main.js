import '@xterm/xterm/css/xterm.css';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';

import {
  ConnectControl, OpenSession, CloseAttach,
  WriteStdin, ResizeSession,
  CreateSession, KillSession, UpdateSession, ListAgents,
  CreateProject, KillProject, UpdateProject,
  LaunchDir, PickDirectory,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

// ---------- session terminal ----------

class SessionTerm {
  constructor(info) {
    this.info = info;
    this.host = document.createElement('div');
    this.host.className = 'term-host';
    this.host.dataset.sid = info.id;
    this.host.style.setProperty('--session-color', info.color || '#888');

    // Tile header (only visible in grid mode via CSS).
    this.header = document.createElement('div');
    this.header.className = 'tile-header';
    this.tileColor = document.createElement('span');
    this.tileColor.className = 'tile-color';
    this.tileName = document.createElement('span');
    this.tileName.className = 'tile-name';
    this.tileName.textContent = info.name;
    this.tileProject = document.createElement('span');
    this.tileProject.className = 'tile-project';
    this.header.append(this.tileColor, this.tileName, this.tileProject);

    this.body = document.createElement('div');
    this.body.className = 'term-body';

    this.host.append(this.header, this.body);
    document.getElementById('terms').appendChild(this.host);

    this.term = new Terminal({
      fontFamily: 'Menlo, "DejaVu Sans Mono", monospace',
      fontSize: 14,
      cursorBlink: true,
      scrollback: 5000,
      theme: { background: '#000000' },
    });
    this.fit = new FitAddon();
    this.term.loadAddon(this.fit);
    this.term.open(this.body);
    this.attached = false;
    this.phase = 'replay';

    this.term.onData((data) => {
      const bytes = new TextEncoder().encode(data);
      let bin = '';
      for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
      WriteStdin(this.info.id, btoa(bin));
    });

    // Click anywhere on the tile (header or body) selects this session.
    this.host.addEventListener('mousedown', () => {
      if (state.activeId !== this.info.id) {
        state.activeId = this.info.id;
        renderSidebar();
        if (state.view === 'single') {
          // Switch terms in single mode; in grid mode every tile is
          // already visible so there's nothing else to do.
          showSingle(this.info.id);
        } else {
          renderGrid();
          this.term.focus();
        }
      }
    });
  }

  setInfo(info) {
    this.info = info;
    this.host.style.setProperty('--session-color', info.color || '#888');
    this.tileName.textContent = info.name;
  }

  setProjectName(name) {
    this.tileProject.textContent = name || '';
  }

  show() {
    this.host.classList.add('visible');
    this.refit();
    this.term.focus();
  }

  hide() {
    this.host.classList.remove('visible');
  }

  refit() {
    try { this.fit.fit(); } catch {}
    if (this.attached) {
      ResizeSession(this.info.id, this.term.cols, this.term.rows);
    }
  }

  async ensureAttached() {
    if (this.attached) return;
    this.fit.fit();
    try {
      await OpenSession(this.info.id, this.term.cols, this.term.rows);
      this.attached = true;
    } catch (err) {
      this.term.write(`\r\n\x1b[31m[attach failed: ${err}]\x1b[0m\r\n`);
    }
  }

  writeData(b64) {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    this.term.write(decoder.decode(bytes, { stream: true }));
  }

  destroy() {
    CloseAttach(this.info.id).catch(() => {});
    this.term.dispose();
    this.host.remove();
  }
}

const decoder = new TextDecoder('utf-8', { fatal: false });

// ---------- app state ----------

const state = {
  projects: [],            // ProjectInfo[] in display order
  sessions: [],            // SessionInfo[] in display order
  collapsed: new Set(),    // project ids that are collapsed
  terms: new Map(),        // session id -> SessionTerm
  activeId: null,
  view: 'single',          // 'single' | 'grid-project' | 'grid-all'
  gridProjectId: null,     // project shown in grid-project mode
};

const termsHost = document.getElementById('terms');
termsHost.classList.add('single');

const projectsUL = document.getElementById('projects');
const status = document.getElementById('status');

function setStatus(text, isError = false) {
  status.textContent = text;
  status.classList.toggle('error', isError);
}

// orderedSessions returns sessions sorted by (project order, session order)
// so navigation always matches what the user sees.
function orderedSessions() {
  const projOrder = new Map(state.projects.map((p, i) => [p.id, i]));
  return [...state.sessions].sort((a, b) => {
    const pa = projOrder.get(a.projectId ?? a.project_id ?? '') ?? 1e9;
    const pb = projOrder.get(b.projectId ?? b.project_id ?? '') ?? 1e9;
    if (pa !== pb) return pa - pb;
    return (a.order ?? 0) - (b.order ?? 0);
  });
}

function activeProjectId() {
  // In grid-project mode the user's "current project" is whichever
  // project is in grid focus, even if active session momentarily
  // points at another project.
  if (state.view === 'grid-project' && state.gridProjectId) {
    return state.gridProjectId;
  }
  if (state.activeId) {
    const s = state.sessions.find((x) => x.id === state.activeId);
    const pid = s?.projectId ?? s?.project_id;
    if (pid) return pid;
  }
  return state.projects[0]?.id ?? '';
}

// ---------- sidebar render ----------

function renderSidebar() {
  projectsUL.innerHTML = '';
  const activePID = activeProjectId();
  for (const p of state.projects) {
    projectsUL.appendChild(renderProject(p, activePID));
  }
}

function renderProject(p, activePID) {
  const li = document.createElement('li');
  li.className = 'project';
  li.dataset.pid = p.id;
  if (state.collapsed.has(p.id)) li.classList.add('collapsed');
  if (p.id === activePID) li.classList.add('active');
  li.style.setProperty('--project-color', p.color || '#888');

  const header = document.createElement('div');
  header.className = 'project-header';

  const caret = document.createElement('span');
  caret.className = 'caret';
  caret.textContent = '▾';
  caret.addEventListener('click', (e) => {
    e.stopPropagation();
    if (state.collapsed.has(p.id)) state.collapsed.delete(p.id);
    else state.collapsed.add(p.id);
    renderSidebar();
  });

  const colorEl = document.createElement('span');
  colorEl.className = 'project-color';

  const name = document.createElement('span');
  name.className = 'project-name';
  name.textContent = p.name;
  name.title = p.cwd ? `${p.name} — ${p.cwd}` : p.name;

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

  actions.append(newBtn, editBtn);

  header.append(caret, colorEl, name, actions);
  header.addEventListener('click', (e) => {
    // Only fire when clicking the row background, color block, or name —
    // not on buttons / caret / inline inputs. Each of those stops
    // propagation in its own handler so we shouldn't see them here,
    // but be defensive.
    if (e.target.closest('.project-actions') || e.target === caret) return;
    switchToProject(p.id);
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
    ul.appendChild(renderSession(s));
  }
  li.appendChild(ul);
  return li;
}

function renderSession(s) {
  const li = document.createElement('li');
  li.className = 'session-item';
  if (s.id === state.activeId) li.classList.add('selected');
  if (!s.alive) li.classList.add('dead');
  li.style.setProperty('--session-color', s.color || '#888');
  li.dataset.sid = s.id;

  const dot = document.createElement('span');
  dot.className = 'dot';

  const name = document.createElement('span');
  name.className = 'name';
  name.textContent = s.name;

  const swatch = document.createElement('span');
  swatch.className = 'swatch';
  const colorInput = document.createElement('input');
  colorInput.type = 'color';
  colorInput.value = s.color || '#888888';
  colorInput.addEventListener('input', (e) => {
    UpdateSession(s.id, '', e.target.value, -1);
  });
  swatch.appendChild(colorInput);

  li.append(dot, name, swatch);
  li.addEventListener('click', (e) => {
    if (e.target === colorInput || e.target === swatch) return;
    switchTo(s.id);
  });
  li.addEventListener('dblclick', () => beginRenameSession(s, li, name));
  return li;
}

function beginRenameSession(sess, li, nameEl) {
  const input = document.createElement('input');
  input.type = 'text';
  input.className = 'name-input';
  input.value = sess.name;
  nameEl.replaceWith(input);
  input.focus();
  input.select();
  const finish = (commit) => {
    if (commit && input.value.trim() && input.value !== sess.name) {
      UpdateSession(sess.id, input.value.trim(), '', -1);
    } else {
      renderSidebar();
    }
  };
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') finish(true);
    else if (e.key === 'Escape') finish(false);
  });
  input.addEventListener('blur', () => finish(true));
}

function beginRenameProject(proj, nameEl) {
  const input = document.createElement('input');
  input.type = 'text';
  input.className = 'project-name-input';
  input.value = proj.name;
  nameEl.replaceWith(input);
  input.focus();
  input.select();
  const finish = (commit) => {
    if (commit && input.value.trim() && input.value !== proj.name) {
      UpdateProject(proj.id, input.value.trim(), '', '', -1);
    } else {
      renderSidebar();
    }
  };
  input.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') finish(true);
    else if (e.key === 'Escape') finish(false);
  });
  input.addEventListener('blur', () => finish(true));
}

function ensureTerm(info) {
  let st = state.terms.get(info.id);
  if (!st) {
    st = new SessionTerm(info);
    state.terms.set(info.id, st);
  } else {
    st.setInfo(info);
  }
  const proj = state.projects.find((p) => p.id === (info.projectId ?? info.project_id));
  st.setProjectName(proj?.name ?? '');
  return st;
}

function showSingle(id) {
  termsHost.classList.add('single');
  termsHost.classList.remove('grid');
  // Hide everything except the active tile.
  for (const [sid, st] of state.terms) {
    if (sid === id) st.show();
    else st.hide();
    st.host.classList.remove('in-grid', 'active');
  }
  const st = id ? state.terms.get(id) : null;
  if (st) st.ensureAttached();
}

function switchTo(id) {
  if (id === state.activeId && state.view === 'single') {
    state.terms.get(id)?.term.focus();
    return;
  }
  state.activeId = id;
  let info = null;
  if (id) {
    info = state.sessions.find((s) => s.id === id);
    if (info) ensureTerm(info);
  }
  // Retarget the grid scope if the new session belongs to a different
  // project than the one currently shown in grid-project mode.
  if (state.view === 'grid-project' && info) {
    const pid = info.projectId ?? info.project_id;
    if (pid && pid !== state.gridProjectId) state.gridProjectId = pid;
  }
  if (state.view === 'single') showSingle(id);
  else renderGrid();
  renderSidebar();
  setStatus(info ? info.name : '');
}

// switchToProject activates a project: in grid-project mode it
// retargets the grid, and in any mode it makes the project's first
// session the active one.
function switchToProject(pid) {
  if (!pid) return;
  if (state.view === 'grid-project') state.gridProjectId = pid;
  const sessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id) === pid)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  if (sessions[0]) switchTo(sessions[0].id);
  else {
    // Project has no live sessions — still show its scope.
    state.activeId = null;
    if (state.view === 'single') showSingle(null);
    else renderGrid();
    renderSidebar();
  }
}

// gridLayout caches the (rows, cols) chosen for the current scope so
// the keyboard navigation and resize logic don't have to recompute.
let gridLayout = { rows: 1, cols: 1, sessions: [] };

// computeGridDims picks (rows, cols) that fills the container without
// scrolling, biasing tile aspect toward typical terminal proportions
// (~1.6 wide-to-tall). Reorders sessions row-major so that arrow
// navigation feels predictable.
function computeGridDims(n, w, h) {
  if (n <= 0) return { rows: 1, cols: 1 };
  // Target tile aspect ratio (width / height). Empirically a
  // terminal looks fine between 1.4 and 1.8; pick 1.6.
  const targetAspect = 1.6;
  let best = { rows: 1, cols: n, score: Infinity };
  for (let cols = 1; cols <= n; cols++) {
    const rows = Math.ceil(n / cols);
    const tileW = w / cols;
    const tileH = h / rows;
    if (tileW <= 0 || tileH <= 0) continue;
    const aspect = tileW / tileH;
    // log distance from target, plus a small penalty for empty cells
    // in the last row to prefer balanced grids.
    const empty = rows * cols - n;
    const score = Math.abs(Math.log(aspect / targetAspect)) + empty * 0.05;
    if (score < best.score) best = { rows, cols, score };
  }
  return best;
}

// renderGrid lays out every tile that should be visible in the
// current grid scope. Tiles for other sessions are hidden but kept
// alive (so their xterm scrollback persists across mode switches).
function renderGrid() {
  termsHost.classList.remove('single');
  termsHost.classList.add('grid');
  const gridSessions = gridScopeSessions();
  const gridIDs = new Set(gridSessions.map((s) => s.id));

  // Ensure every grid session has a SessionTerm and is attached.
  // Move tiles into the desired DOM order (row-major) so that flexbox
  // / CSS grid honors the navigation order without us having to set
  // grid-row/column explicitly.
  for (const info of gridSessions) {
    const st = ensureTerm(info);
    st.host.classList.add('in-grid');
    st.host.classList.toggle('active', info.id === state.activeId);
    st.ensureAttached();
    termsHost.appendChild(st.host); // re-order to keep DOM == nav order
  }
  // Hide / unmark tiles outside the scope.
  for (const [sid, st] of state.terms) {
    if (!gridIDs.has(sid)) {
      st.host.classList.remove('in-grid', 'active');
    }
  }

  // Pick (rows, cols) that fills the container.
  const w = termsHost.clientWidth || 800;
  const h = termsHost.clientHeight || 600;
  const { rows, cols } = computeGridDims(gridSessions.length, w, h);
  termsHost.style.gridTemplateColumns = `repeat(${cols}, 1fr)`;
  termsHost.style.gridTemplateRows = `repeat(${rows}, 1fr)`;
  gridLayout = { rows, cols, sessions: gridSessions };

  // Refit each visible tile after the layout settles.
  requestAnimationFrame(() => {
    for (const info of gridSessions) {
      state.terms.get(info.id)?.refit();
    }
  });
}

// gridSpatialMove moves the active tile in the given direction within
// the current grid layout. delta = {col, row}. No-ops if the target
// cell is outside the grid or empty.
function gridSpatialMove(dCol, dRow) {
  const { rows, cols, sessions } = gridLayout;
  if (sessions.length === 0) return;
  const idx = sessions.findIndex((s) => s.id === state.activeId);
  if (idx < 0) {
    state.activeId = sessions[0].id;
    renderGrid();
    renderSidebar();
    return;
  }
  const r = Math.floor(idx / cols);
  const c = idx % cols;
  const nr = r + dRow;
  const nc = c + dCol;
  if (nr < 0 || nr >= rows || nc < 0 || nc >= cols) return;
  const target = nr * cols + nc;
  if (target >= sessions.length) return; // empty cell on last row
  state.activeId = sessions[target].id;
  renderGrid();
  renderSidebar();
  state.terms.get(state.activeId)?.term.focus();
  setStatus(sessions[target].name);
}

function shiftActiveProject(delta) {
  if (state.projects.length === 0) return;
  const cur = activeProjectId();
  const i = state.projects.findIndex((p) => p.id === cur);
  if (i < 0) return;
  const next = state.projects[(i + delta + state.projects.length) % state.projects.length];
  if (state.view === 'grid-project') {
    state.gridProjectId = next.id;
  }
  const sessions = state.sessions
    .filter((s) => (s.projectId ?? s.project_id) === next.id)
    .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  if (sessions[0]) state.activeId = sessions[0].id;
  if (state.view === 'single') showSingle(state.activeId);
  else renderGrid();
  renderSidebar();
  setStatus(`${next.name}`);
}

// gridScopeSessions returns the list of sessions that should be tiled
// in the current grid view.
function gridScopeSessions() {
  if (state.view === 'grid-all') return orderedSessions();
  if (state.view === 'grid-project') {
    const pid = state.gridProjectId || activeProjectId();
    return state.sessions
      .filter((s) => (s.projectId ?? s.project_id) === pid)
      .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  }
  return [];
}

function setView(view) {
  state.view = view;
  if (view === 'grid-project') {
    state.gridProjectId = activeProjectId();
  }
  if (view === 'single') {
    showSingle(state.activeId);
  } else {
    renderGrid();
  }
  renderSidebar();
  const ord = orderedSessions();
  const active = ord.find((s) => s.id === state.activeId);
  setStatus(`${view}${active ? ' • ' + active.name : ''}`);
}


// ---------- daemon events ----------

EventsOn('project:list', (jsonStr) => {
  const { projects } = JSON.parse(jsonStr);
  state.projects = projects || [];
  renderSidebar();
});

EventsOn('project:event', (jsonStr) => {
  const ev = JSON.parse(jsonStr);
  const i = state.projects.findIndex((p) => p.id === ev.project.id);
  if (ev.kind === 'added') {
    if (i < 0) state.projects.push(ev.project);
  } else if (ev.kind === 'removed') {
    if (i >= 0) state.projects.splice(i, 1);
    state.collapsed.delete(ev.project.id);
  } else if (ev.kind === 'updated') {
    if (i >= 0) state.projects[i] = ev.project;
  }
  state.projects.sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
  renderSidebar();
});

EventsOn('session:list', (jsonStr) => {
  const { sessions } = JSON.parse(jsonStr);
  state.sessions = sessions || [];
  renderSidebar();
  if (!state.activeId && state.sessions.length > 0) {
    switchTo(orderedSessions()[0].id);
  }
});

EventsOn('session:event', (jsonStr) => {
  const ev = JSON.parse(jsonStr);
  const i = state.sessions.findIndex((s) => s.id === ev.session.id);
  if (ev.kind === 'added') {
    if (i < 0) state.sessions.push(ev.session);
    renderSidebar();
    switchTo(ev.session.id);
    return;
  }
  if (ev.kind === 'removed') {
    let nextId = null;
    if (state.activeId === ev.session.id) {
      const ord = orderedSessions();
      const idx = ord.findIndex((s) => s.id === ev.session.id);
      const nb = idx > 0 ? ord[idx - 1] : ord[idx + 1];
      nextId = nb?.id ?? null;
    }
    if (i >= 0) state.sessions.splice(i, 1);
    const t = state.terms.get(ev.session.id);
    if (t) {
      t.destroy();
      state.terms.delete(ev.session.id);
    }
    if (state.activeId === ev.session.id) {
      state.activeId = null;
      if (nextId) switchTo(nextId);
    }
  } else if (ev.kind === 'updated') {
    if (i >= 0) state.sessions[i] = ev.session;
  }
  renderSidebar();
});

EventsOn('pty:data', (id, b64) => {
  state.terms.get(id)?.writeData(b64);
});

EventsOn('pty:event', (id, jsonStr) => {
  try {
    const ev = JSON.parse(jsonStr);
    const st = state.terms.get(id);
    if (st && ev.kind === 'scrollback_replay_done') st.phase = 'live';
  } catch { /* ignore */ }
});

EventsOn('pty:disconnect', (id) => {
  const st = state.terms.get(id);
  if (st) st.attached = false;
});

EventsOn('pty:error', (id, jsonStr) => {
  const st = state.terms.get(id);
  if (st) {
    try {
      const e = JSON.parse(jsonStr);
      st.term.write(`\r\n\x1b[31m[hived: ${e.code}: ${e.message}]\x1b[0m\r\n`);
    } catch {}
  }
});

EventsOn('control:disconnect', () => {
  setStatus('control disconnected', true);
});

// ---------- agent launcher ----------

const launcherEl = document.getElementById('launcher');
const launcherState = { items: [], selected: 0, projectId: null };

function highlightLauncherSelection() {
  launcherState.items.forEach((it, i) => {
    it.el.classList.toggle('selected', i === launcherState.selected);
    if (i === launcherState.selected) it.el.scrollIntoView({ block: 'nearest' });
  });
}

function moveLauncherSelection(delta) {
  const n = launcherState.items.length;
  if (n === 0) return;
  let i = launcherState.selected;
  for (let step = 0; step < n; step++) {
    i = (i + delta + n) % n;
    if (launcherState.items[i].agent.available) break;
  }
  launcherState.selected = i;
  highlightLauncherSelection();
}

function activateLauncherSelection() {
  const it = launcherState.items[launcherState.selected];
  if (!it || !it.agent.available) return;
  CreateSession(it.agent.id, launcherState.projectId || activeProjectId(), '', '', 0, 0);
  closeLauncher();
}

function openLauncher(projectId) {
  launcherState.projectId = projectId || activeProjectId();
  ListAgents()
    .then((agents) => {
      launcherEl.innerHTML = '';
      launcherState.items = [];
      // Anchor next to the resolved project's + button so the user
      // can see which project the new session lands in. Falls back
      // to the global new-project button if the project's row isn't
      // currently in the DOM (e.g. its header is offscreen).
      const anchorEl =
        document.querySelector(`.project[data-pid="${launcherState.projectId}"] .project-actions button`) ??
        document.getElementById('new-project-btn');
      const r = anchorEl.getBoundingClientRect();
      launcherEl.style.left = `${r.left}px`;
      launcherEl.style.top = `${r.bottom + 4}px`;
      let firstAvailable = -1;
      agents.forEach((a, idx) => {
        const item = document.createElement('div');
        item.className = 'launcher-item' + (a.available ? '' : ' unavailable');
        item.style.setProperty('--agent-color', a.color);
        const dot = document.createElement('span');
        dot.className = 'agent-dot';
        const name = document.createElement('span');
        name.className = 'agent-name';
        name.textContent = a.name;
        item.append(dot, name);
        if (!a.available && a.installCmd && a.installCmd.length) {
          const tag = document.createElement('span');
          tag.className = 'install-tag';
          tag.title = a.installCmd.join(' ');
          tag.textContent = 'install';
          item.appendChild(tag);
        }
        if (a.available) {
          if (firstAvailable < 0) firstAvailable = idx;
          item.addEventListener('click', () => {
            CreateSession(a.id, launcherState.projectId, '', '', 0, 0);
            closeLauncher();
          });
          item.addEventListener('mouseenter', () => {
            launcherState.selected = idx;
            highlightLauncherSelection();
          });
        }
        launcherEl.appendChild(item);
        launcherState.items.push({ agent: a, el: item });
      });
      launcherState.selected = firstAvailable >= 0 ? firstAvailable : 0;
      highlightLauncherSelection();
      launcherEl.classList.remove('hidden');
    })
    .catch(() => {});
}

function closeLauncher() {
  launcherEl.classList.add('hidden');
  launcherState.items = [];
}

document.addEventListener('click', (e) => {
  const inAction = e.target.closest('.project-actions');
  if (!launcherEl.contains(e.target) && !inAction) closeLauncher();
});

// ---------- project editor (new + edit) ----------

const editorEl = document.getElementById('project-editor');
const editorTitle = document.getElementById('project-editor-title');
const editorName = document.getElementById('project-editor-name');
const editorCwd = document.getElementById('project-editor-cwd');
const editorColor = document.getElementById('project-editor-color');
const editorState = { editing: null }; // null = create; else project object

function openProjectEditor(project) {
  editorState.editing = project || null;
  editorTitle.textContent = project ? 'Edit project' : 'New project';
  editorName.value = project?.name ?? '';
  editorColor.value = project?.color || '#f59e0b';
  if (project) {
    editorCwd.value = project.cwd ?? '';
  } else {
    LaunchDir().then((d) => { editorCwd.value = d || ''; }).catch(() => {});
    editorCwd.value = '';
  }
  editorEl.classList.remove('hidden');
  setTimeout(() => editorName.focus(), 0);
}

function closeProjectEditor() {
  editorEl.classList.add('hidden');
  editorState.editing = null;
}

function saveProjectEditor() {
  const name = editorName.value.trim();
  const cwd = editorCwd.value.trim();
  const color = editorColor.value;
  if (!name) return;
  if (editorState.editing) {
    UpdateProject(editorState.editing.id, name, color, cwd, -1);
  } else {
    CreateProject(name, color, cwd);
  }
  closeProjectEditor();
}

document.getElementById('project-editor-cancel').addEventListener('click', closeProjectEditor);
document.getElementById('project-editor-save').addEventListener('click', saveProjectEditor);
document.getElementById('project-editor-browse').addEventListener('click', async () => {
  try {
    const picked = await PickDirectory(editorCwd.value || '');
    if (picked) editorCwd.value = picked;
  } catch (err) {
    // Silently ignore (user cancelled, or platform refused).
  }
});
editorEl.addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && (e.target === editorName || e.target === editorCwd)) {
    e.preventDefault();
    saveProjectEditor();
  } else if (e.key === 'Escape') {
    closeProjectEditor();
  }
});
document.getElementById('new-project-btn').addEventListener('click', () => openProjectEditor(null));

// ---------- keyboard ----------

window.addEventListener('keydown', (e) => {
  if (!launcherEl.classList.contains('hidden')) {
    const handle = (fn) => { e.preventDefault(); e.stopPropagation(); fn(); };
    if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey)) return handle(() => moveLauncherSelection(+1));
    if (e.key === 'ArrowUp'   || (e.key === 'Tab' && e.shiftKey))  return handle(() => moveLauncherSelection(-1));
    if (e.key === 'Enter')   return handle(activateLauncherSelection);
    if (e.key === 'Escape')  return handle(closeLauncher);
    if ((e.metaKey || e.ctrlKey) && (e.key === 'n' || e.key === 'N')) return handle(closeLauncher);
  }
  if (!editorEl.classList.contains('hidden')) {
    return; // editor's own listener handles keys
  }

  const meta = e.metaKey || e.ctrlKey;
  if (!meta) return;
  const swallow = () => { e.preventDefault(); e.stopPropagation(); };

  if ((e.key === 'p' || e.key === 'P') && e.shiftKey) {
    swallow();
    openProjectEditor(null);
  } else if (e.key === 's' || e.key === 'S') {
    swallow();
    const app = document.getElementById('app');
    app.classList.toggle('sidebar-hidden');
    // Re-fit visible terminals after the layout transition.
    setTimeout(() => {
      if (state.view === 'single') {
        state.terms.get(state.activeId)?.refit();
      } else {
        for (const info of gridScopeSessions()) state.terms.get(info.id)?.refit();
      }
    }, 150);
  } else if (e.key === 'g' || e.key === 'G') {
    swallow();
    if (e.shiftKey) {
      setView(state.view === 'grid-all' ? 'single' : 'grid-all');
    } else {
      setView(state.view === 'grid-project' ? 'single' : 'grid-project');
    }
  } else if (e.key === 'n' || e.key === 'N') {
    swallow();
    openLauncher();
  } else if (e.key === 'w' || e.key === 'W') {
    swallow();
    if (state.activeId) KillSession(state.activeId);
  } else if (/^[1-9]$/.test(e.key)) {
    const idx = parseInt(e.key, 10) - 1;
    const ord = orderedSessions();
    if (idx < ord.length) {
      swallow();
      switchTo(ord[idx].id);
    }
  } else if (e.key === 'ArrowLeft') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(-1, 0);
    else moveActiveSession(-1, e.shiftKey);
  } else if (e.key === 'ArrowRight') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(+1, 0);
    else moveActiveSession(+1, e.shiftKey);
  } else if (e.key === 'ArrowUp') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(0, -1);
    else moveActiveSession(-1, e.shiftKey);
  } else if (e.key === 'ArrowDown') {
    swallow();
    if (state.view !== 'single') gridSpatialMove(0, +1);
    else moveActiveSession(+1, e.shiftKey);
  } else if (e.key === '[') {
    swallow();
    shiftActiveProject(-1);
  } else if (e.key === ']') {
    swallow();
    shiftActiveProject(+1);
  }
}, true);

// moveActiveSession walks the (project_order, session_order) list.
// reorder=true moves the session within its project only.
function moveActiveSession(delta, reorder) {
  const ord = orderedSessions();
  const n = ord.length;
  if (n === 0) return;
  const idx = ord.findIndex((s) => s.id === state.activeId);
  if (idx < 0) {
    switchTo(ord[0].id);
    return;
  }
  if (reorder) {
    const cur = state.sessions.find((s) => s.id === state.activeId);
    const sib = state.sessions
      .filter((s) => (s.projectId ?? s.project_id) === (cur.projectId ?? cur.project_id))
      .sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
    const sIdx = sib.findIndex((s) => s.id === state.activeId);
    const next = (sIdx + delta + sib.length) % sib.length;
    UpdateSession(state.activeId, '', '', next);
    return;
  }
  const next = (idx + delta + n) % n;
  switchTo(ord[next].id);
}

// ---------- resize ----------

let resizeTimer = null;
window.addEventListener('resize', () => {
  if (resizeTimer) clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => {
    if (state.view === 'single') {
      const t = state.activeId && state.terms.get(state.activeId);
      if (t) t.refit();
      return;
    }
    for (const info of gridScopeSessions()) {
      state.terms.get(info.id)?.refit();
    }
  }, 50);
});

// ---------- bootstrap ----------

(async () => {
  setStatus('connecting…');
  try {
    await ConnectControl();
    setStatus('connected');
  } catch (err) {
    setStatus(`connect failed: ${err}`, true);
  }
})();

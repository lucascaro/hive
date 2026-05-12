// In-browser fake of the Wails bridge. Replaces the generated
// wailsjs/go/main/App + wailsjs/runtime/runtime modules so the
// frontend boots in plain Vite dev (and Playwright) without the
// native Wails runtime. Drives the UI through a tiny scripted
// daemon-state machine that Playwright can poke via window.__hive.

const listeners = new Map(); // event name -> [handler, ...]

function emit(name, ...args) {
  const arr = listeners.get(name) || [];
  for (const fn of arr) {
    try { fn(...args); } catch (e) { /* swallow per Wails */ }
  }
}

// --- runtime ---

export function EventsOn(name, handler) {
  if (!listeners.has(name)) listeners.set(name, []);
  listeners.get(name).push(handler);
  return () => {
    const arr = listeners.get(name) || [];
    const i = arr.indexOf(handler);
    if (i >= 0) arr.splice(i, 1);
  };
}

export function EventsOff(name) { listeners.delete(name); }
export function WindowSetTitle(t) { document.title = t; }

// --- App bindings ---

const state = {
  projects: [
    { id: 'p1', name: 'default', color: '#888', cwd: '', order: 0,
      created: new Date().toISOString() },
  ],
  sessions: [
    { id: 's1', name: 'main', color: '#abc', order: 0,
      created: new Date().toISOString(), alive: true, agent: '',
      project_id: 'p1', worktree_path: '', worktree_branch: '',
      last_error: '' },
  ],
};

function broadcast() {
  emit('project:list', JSON.stringify({ projects: state.projects }));
  emit('session:list', JSON.stringify({ sessions: state.sessions }));
}

// Wails control bindings — frontend imports these from
// ../wailsjs/go/main/App.
export async function ConnectControl() { setTimeout(broadcast, 0); return ''; }
export async function OpenSession(id) { return id; }
export async function CloseAttach(_id) { return ''; }
export async function WriteStdin(_id, _bytes) { return ''; }
export async function ResizeSession(_id, _cols, _rows) { return ''; }
export async function CreateSession(spec) {
  const id = 'mock-' + (state.sessions.length + 1);
  const s = {
    id, name: spec.name || `s${state.sessions.length + 1}`,
    color: spec.color || '#0af', order: state.sessions.length,
    created: new Date().toISOString(), alive: true, agent: spec.agent || '',
    project_id: spec.project_id || 'p1',
    worktree_path: '', worktree_branch: '', last_error: '',
  };
  state.sessions.push(s);
  emit('session:event', JSON.stringify({ kind: 'added', session: s }));
  return id;
}
export async function DuplicateSession(id) { return CreateSession({ name: 'dup' }); }
export async function KillSession(id) {
  const i = state.sessions.findIndex((s) => s.id === id);
  if (i < 0) return '';
  const [removed] = state.sessions.splice(i, 1);
  emit('session:event', JSON.stringify({ kind: 'removed', session: removed }));
  return '';
}
export async function RestartSession(_id) { return ''; }
export async function UpdateSession(req) {
  const s = state.sessions.find((x) => x.id === req.session_id);
  if (!s) return '';
  if (req.name != null) s.name = req.name;
  if (req.color != null) s.color = req.color;
  emit('session:event', JSON.stringify({ kind: 'updated', session: s }));
  return '';
}
export async function ListAgents() { return []; }
export async function CreateProject(req) {
  const id = 'p-' + (state.projects.length + 1);
  const p = { id, name: req.name || 'new', color: req.color || '#0af',
    cwd: req.cwd || '', order: state.projects.length,
    created: new Date().toISOString() };
  state.projects.push(p);
  emit('project:event', JSON.stringify({ kind: 'added', project: p }));
  return id;
}
export async function KillProject(_id) { return ''; }
export async function UpdateProject(_req) { return ''; }
export async function LaunchDir() { return ''; }
export async function PickDirectory() { return ''; }
export async function OpenNewWindow() { return ''; }
export async function CloseWindow() { return ''; }
export async function IsGitRepo(_dir) { return false; }
export async function OpenURL(_url) { return ''; }
export async function OpenTerminalAt(_dir) { return ''; }
export async function Notify(_title, _body) { return ''; }
export async function Confirm(_title, _body) { return true; }
export async function RestartDaemon() { return ''; }
export async function CheckForUpdate() { return null; }

// Test hook: lets Playwright inject events / inspect state.
if (typeof window !== 'undefined') {
  window.__hive = {
    state,
    emit,
    addSession(name) { return CreateSession({ name }); },
    killSession(id) { return KillSession(id); },
    listeners,
  };
}

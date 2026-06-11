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

// Clipboard bindings. main.js imports ClipboardGetText (runtime) and
// SetClipboardText (App), both aliased here. The mock keeps an in-memory
// clipboard so copy/paste paths don't throw at module load.
let clipboard = '';
export async function ClipboardGetText() { maybeFail('ClipboardGetText'); return clipboard; }
export async function SetClipboardText(text) { maybeFail('SetClipboardText'); clipboard = String(text ?? ''); }

// --- failure injection ---
//
// window.__hive.failNext(method, message) arms a one-shot rejection for
// the named binding, so E2E can assert that a failed daemon call
// surfaces user-visible feedback instead of being silently swallowed.
const failures = new Map(); // method name -> error message
function maybeFail(method) {
  if (failures.has(method)) {
    const msg = failures.get(method);
    failures.delete(method);
    throw new Error(msg);
  }
}

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
const stdinLog = []; // [{ id, b64, text }] — populated by WriteStdin so E2E can assert input routing.
export async function WriteStdin(id, b64) {
  let text = '';
  try {
    if (typeof atob === 'function' && typeof TextDecoder !== 'undefined') {
      // atob() returns a Latin-1 "binary string" — feed each char's
      // code unit into a Uint8Array, then decode as UTF-8 so non-ASCII
      // input round-trips correctly in E2E assertions.
      const bin = atob(b64);
      const bytes = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
      text = new TextDecoder('utf-8').decode(bytes);
    } else {
      text = Buffer.from(b64, 'base64').toString('utf-8');
    }
  } catch {}
  stdinLog.push({ id, b64, text });
  return '';
}
export async function ResizeSession(_id, _cols, _rows) { return ''; }
const replayLog = []; // [{ id, t }] — populated by RequestScrollbackReplay so E2E can detect spurious replays after layout reflows.
export async function RequestScrollbackReplay(id) {
  replayLog.push({ id, t: Date.now() });
  return '';
}
// Positional args matching the real Wails binding:
// CreateSession(agentID, projectID, name, color, cols, rows, useWorktree).
export async function CreateSession(agentID, projectID, name, color, _cols, _rows, _useWorktree) {
  maybeFail('CreateSession');
  const id = 'mock-' + (state.sessions.length + 1);
  const s = {
    id, name: name || `s${state.sessions.length + 1}`,
    color: color || '#0af', order: state.sessions.length,
    created: new Date().toISOString(), alive: true, agent: agentID || '',
    project_id: projectID || 'p1',
    worktree_path: '', worktree_branch: '', last_error: '',
  };
  state.sessions.push(s);
  emit('session:event', JSON.stringify({ kind: 'added', session: s }));
  return id;
}
// Positional: DuplicateSession(agentID, projectID, cwd).
export async function DuplicateSession(agentID, projectID, _cwd) {
  maybeFail('DuplicateSession');
  return CreateSession(agentID, projectID, 'dup', '', 0, 0, false);
}
export async function KillSession(id) {
  maybeFail('KillSession');
  const i = state.sessions.findIndex((s) => s.id === id);
  if (i < 0) return '';
  const [removed] = state.sessions.splice(i, 1);
  emit('session:event', JSON.stringify({ kind: 'removed', session: removed }));
  return '';
}
export async function RestartSession(_id) { maybeFail('RestartSession'); return ''; }
// Positional args matching the real Wails binding (and the e2e-real
// bridge): UpdateSession(id, name, color, order). Empty string / -1
// mean "no change". The old object-shaped signature silently no-op'd
// every rename driven through the UI.
export async function UpdateSession(id, name, color, _order) {
  maybeFail('UpdateSession');
  const s = state.sessions.find((x) => x.id === id);
  if (!s) return '';
  if (name) s.name = name;
  if (color) s.color = color;
  emit('session:event', JSON.stringify({ kind: 'updated', session: s }));
  return '';
}
// One real-shaped agent so launcher E2E can exercise the full
// open → select → create flow (matches internal/agent's wire shape).
export async function ListAgents() {
  maybeFail('ListAgents');
  return [{ id: 'shell', name: 'Shell', color: '#888', available: true, installCmd: [] }];
}
// Project mutations are positional too, matching the real bindings
// (cmd/hivegui/app.go): CreateProject(name, color, cwd),
// KillProject(id, killSessions), UpdateProject(id, name, color, cwd,
// order). The old object-shaped/no-op forms silently no-op'd every
// project create/save/delete driven through the UI — the same defect
// UpdateSession had.
export async function CreateProject(name, color, cwd) {
  maybeFail('CreateProject');
  const id = 'p-' + (state.projects.length + 1);
  const p = { id, name: name || 'new', color: color || '#0af',
    cwd: cwd || '', order: state.projects.length,
    created: new Date().toISOString() };
  state.projects.push(p);
  emit('project:event', JSON.stringify({ kind: 'added', project: p }));
  return id;
}
export async function KillProject(id, killSessions) {
  maybeFail('KillProject');
  const i = state.projects.findIndex((p) => p.id === id);
  if (i < 0) return '';
  if (killSessions) {
    for (let j = state.sessions.length - 1; j >= 0; j--) {
      if (state.sessions[j].project_id === id) {
        const [rs] = state.sessions.splice(j, 1);
        emit('session:event', JSON.stringify({ kind: 'removed', session: rs }));
      }
    }
  }
  const [removed] = state.projects.splice(i, 1);
  emit('project:event', JSON.stringify({ kind: 'removed', project: removed }));
  return '';
}
// Empty string / -1 mean "no change", mirroring UpdateSession (order is
// accepted but not modelled, same as UpdateSession's _order).
export async function UpdateProject(id, name, color, cwd, _order) {
  maybeFail('UpdateProject');
  const p = state.projects.find((x) => x.id === id);
  if (!p) return '';
  if (name) p.name = name;
  if (color) p.color = color;
  if (cwd) p.cwd = cwd;
  emit('project:event', JSON.stringify({ kind: 'updated', project: p }));
  return '';
}
export async function LaunchDir() { return ''; }
export async function PickDirectory() { return ''; }
export async function OpenNewWindow() { maybeFail('OpenNewWindow'); return ''; }
export async function CloseWindow() { maybeFail('CloseWindow'); return ''; }
export async function IsGitRepo(_dir) { return false; }
export async function OpenURL(_url) { maybeFail('OpenURL'); return ''; }
export async function OpenTerminalAt(_dir) { maybeFail('OpenTerminalAt'); return ''; }
export async function Notify(_title, _body) { return ''; }
export async function Confirm(_title, _body) { return true; }
export async function RestartDaemon() { return ''; }
export async function CheckForUpdate() { return null; }

// Test hook: lets Playwright inject events / inspect state.
if (typeof window !== 'undefined') {
  window.__hive = {
    state,
    emit,
    addSession(name) { return CreateSession('', 'p1', name, '', 0, 0, false); },
    killSession(id) { return KillSession(id); },
    listeners,
    stdinLog,
    stdinText(id) {
      return stdinLog
        .filter((e) => id == null || e.id === id)
        .map((e) => e.text)
        .join('');
    },
    resetStdin() { stdinLog.length = 0; },
    replayLog,
    replayCount(id) { return replayLog.filter((e) => id == null || e.id === id).length; },
    resetReplay() { replayLog.length = 0; },
    failNext(method, message = 'injected failure') { failures.set(method, message); },
  };
}

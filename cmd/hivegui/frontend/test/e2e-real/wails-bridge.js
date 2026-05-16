// Real-daemon Wails bridge for Layer B Playwright tests.
//
// Mirrors the export surface of test/e2e/wails-mock.js so the same
// Vite resolveId substitution can pick this module when
// VITE_WAILS_REAL=1 is set. Instead of running a fake state machine
// in the browser, each method round-trips a JSON-RPC call to
// hived-ws-bridge (cmd/hived-ws-bridge/), which translates to native
// wire frames against a real hived daemon and pushes events back.
//
// The WS URL is read from window.__WS_BRIDGE_URL — Playwright injects
// it via addInitScript before page.goto.

const listeners = new Map();
const pending = new Map();
let nextId = 1;
let ws = null;
let wsReady = null;

function getWsUrl() {
  if (typeof window === 'undefined') return null;
  return window.__WS_BRIDGE_URL || `ws://${location.hostname}:5176/`;
}

function ensureWS() {
  if (wsReady) return wsReady;
  wsReady = new Promise((resolve, reject) => {
    const url = getWsUrl();
    if (!url) { reject(new Error('no WS bridge URL')); return; }
    ws = new WebSocket(url);
    ws.addEventListener('open', () => resolve(ws));
    ws.addEventListener('error', (e) => reject(e));
    ws.addEventListener('message', (m) => {
      let msg;
      try { msg = JSON.parse(m.data); } catch { return; }
      if (msg.event) {
        const arr = listeners.get(msg.event) || [];
        for (const fn of arr) {
          try { fn(...(msg.args || [])); } catch { /* swallow per Wails */ }
        }
        return;
      }
      const p = pending.get(msg.id);
      if (!p) return;
      pending.delete(msg.id);
      if (msg.error) p.reject(new Error(msg.error));
      else p.resolve(msg.result);
    });
    ws.addEventListener('close', () => {
      for (const [, p] of pending) p.reject(new Error('ws closed'));
      pending.clear();
    });
  });
  return wsReady;
}

async function call(method, params) {
  await ensureWS();
  const id = nextId++;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    ws.send(JSON.stringify({ id, method, params: params || {} }));
  });
}

// --- runtime surface (matches wails-mock.js / wailsjs/runtime) ---

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

// --- App bindings (subset — covers what test specs exercise) ---

export async function ConnectControl() { return call('ConnectControl'); }
export async function OpenSession(id, cols, rows) {
  return call('OpenSession', { id, cols: cols || 0, rows: rows || 0 });
}
export async function CloseAttach(id) { return call('CloseAttach', { id }); }

const stdinLog = [];
export async function WriteStdin(id, b64) {
  let text = '';
  try {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    text = new TextDecoder('utf-8').decode(bytes);
  } catch { /* ignore decode errors — test hook only */ }
  stdinLog.push({ id, b64, text });
  return call('WriteStdin', { id, b64 });
}
export async function ResizeSession(id, cols, rows) {
  return call('ResizeSession', { id, cols, rows });
}
export async function RequestScrollbackReplay(id) {
  return call('RequestScrollbackReplay', { id });
}
export async function CreateSession(agentID, projectID, name, color, cols, rows, useWorktree) {
  // The Wails signature uses positional args; map to the bridge's
  // CreateSpec shape.
  return call('CreateSession', {
    agent: agentID || '',
    project_id: projectID || '',
    name: name || '',
    color: color || '',
    cols: cols || 80,
    rows: rows || 24,
    use_worktree: !!useWorktree,
  });
}
export async function DuplicateSession() { return CreateSession('', '', 'dup', '', 80, 24, false); }
export async function KillSession(id, force) {
  return call('KillSession', { session_id: id, force: !!force });
}
export async function RestartSession(_id) { return ''; }
export async function UpdateSession(_id, _name, _color, _order) { return ''; }
export async function ListAgents() { return []; }
export async function CreateProject() { return ''; }
export async function KillProject() { return ''; }
export async function UpdateProject() { return ''; }
export async function LaunchDir() { return ''; }
export async function PickDirectory() { return ''; }
export async function OpenNewWindow() { return ''; }
export async function CloseWindow() { return ''; }
export async function IsGitRepo() { return false; }
export async function OpenURL() { return ''; }
export async function OpenTerminalAt() { return ''; }
export async function Notify() { return ''; }
export async function Confirm() { return true; }
export async function RestartDaemon() { return ''; }
export async function CheckForUpdate() { return null; }

// Test hooks for Playwright. Smaller surface than the mock — there's
// no scripted state machine to poke; the daemon IS the state machine.
if (typeof window !== 'undefined') {
  window.__hive = {
    stdinLog,
    stdinText(id) {
      return stdinLog.filter((e) => id == null || e.id === id).map((e) => e.text).join('');
    },
    resetStdin() { stdinLog.length = 0; },
    listeners,
    emit(name, ...args) {
      const arr = listeners.get(name) || [];
      for (const fn of arr) {
        try { fn(...args); } catch { /* */ }
      }
    },
  };
}

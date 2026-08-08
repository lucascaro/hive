// Real-daemon Wails bridge for Layer B Playwright tests.
//
// Mirrors the export surface of test/e2e/wails-mock.ts so the same
// Vite resolveId substitution can pick this module when
// VITE_WAILS_REAL=1 is set. Instead of running a fake state machine
// in the browser, each method round-trips a JSON-RPC call to
// hived-ws-bridge (cmd/hived-ws-bridge/), which translates to native
// wire frames against a real hived daemon and pushes events back.
//
// The WS URL is read from window.__WS_BRIDGE_URL — Playwright injects
// it via addInitScript before page.goto.

type Handler = (...args: unknown[]) => void;

// One in-flight JSON-RPC call. `result` is whatever the bridge sends back —
// each binding below narrows it (or ignores it).
interface Pending {
  resolve: (value: unknown) => void;
  reject: (reason: Error | Event) => void;
}

// Frames from cmd/hived-ws-bridge: either an event push or a call reply.
interface Frame {
  event?: string;
  args?: unknown[];
  id?: number;
  error?: string;
  result?: unknown;
}

const listeners = new Map<string, Handler[]>();
const pending = new Map<number, Pending>();
let nextId = 1;
let wsReady: Promise<WebSocket> | null = null;

function getWsUrl() {
  if (typeof window === 'undefined') return null;
  return window.__WS_BRIDGE_URL || `ws://${location.hostname}:5176/`;
}

function ensureWS() {
  if (wsReady) return wsReady;
  wsReady = new Promise<WebSocket>((resolve, reject) => {
    const url = getWsUrl();
    if (!url) {
      reject(new Error('no WS bridge URL'));
      return;
    }
    const sock = new WebSocket(url);
    sock.addEventListener('open', () => resolve(sock));
    sock.addEventListener('error', (e) => reject(e));
    sock.addEventListener('message', (m: MessageEvent) => {
      let msg: Frame;
      try {
        msg = JSON.parse(m.data);
      } catch {
        return;
      }
      if (msg.event) {
        const arr = listeners.get(msg.event) || [];
        for (const fn of arr) {
          try {
            fn(...(msg.args || []));
          } catch {
            /* swallow per Wails */
          }
        }
        return;
      }
      if (msg.id === undefined) return;
      const p = pending.get(msg.id);
      if (!p) return;
      pending.delete(msg.id);
      if (msg.error) p.reject(new Error(msg.error));
      else p.resolve(msg.result);
    });
    sock.addEventListener('close', () => {
      for (const [, p] of pending) p.reject(new Error('ws closed'));
      pending.clear();
    });
  });
  return wsReady;
}

async function call(method: string, params?: Record<string, unknown>) {
  const sock = await ensureWS();
  const id = nextId++;
  return new Promise<unknown>((resolve, reject) => {
    pending.set(id, { resolve, reject });
    sock.send(JSON.stringify({ id, method, params: params || {} }));
  });
}

// --- runtime surface (matches wails-mock.ts / wailsjs/runtime) ---

export function EventsOn(name: string, handler: Handler) {
  const arr = listeners.get(name);
  if (arr) arr.push(handler);
  else listeners.set(name, [handler]);
  // Re-look-up rather than closing over `arr` — EventsOff replaces the
  // array, and the JS original unsubscribed against whatever is current.
  return () => {
    const cur = listeners.get(name) || [];
    const i = cur.indexOf(handler);
    if (i >= 0) cur.splice(i, 1);
  };
}
export function EventsOff(name: string) {
  listeners.delete(name);
}
export function WindowSetTitle(t: string) {
  document.title = t;
}

// --- App bindings (subset — covers what test specs exercise) ---

export async function ConnectControl() {
  return call('ConnectControl');
}
export async function OpenSession(id: string, cols: number, rows: number) {
  return call('OpenSession', { id, cols: cols || 0, rows: rows || 0 });
}
export async function CloseAttach(id: string) {
  return call('CloseAttach', { id });
}

const stdinLog: { id: string; b64: string; text: string }[] = [];
export async function WriteStdin(id: string, b64: string) {
  let text = '';
  try {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    text = new TextDecoder('utf-8').decode(bytes);
  } catch {
    /* ignore decode errors — test hook only */
  }
  stdinLog.push({ id, b64, text });
  return call('WriteStdin', { id, b64 });
}
export async function ResizeSession(id: string, cols: number, rows: number) {
  return call('ResizeSession', { id, cols, rows });
}
export async function RequestScrollbackReplay(id: string) {
  return call('RequestScrollbackReplay', { id });
}
export async function CreateSession(
  agentID: string,
  projectID: string,
  name: string,
  color: string,
  cols: number,
  rows: number,
  useWorktree: boolean,
) {
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
export async function DuplicateSession() {
  return CreateSession('', '', 'dup', '', 80, 24, false);
}
export async function KillSession(id: string, force: boolean) {
  return call('KillSession', { session_id: id, force: !!force });
}
export async function RestartSession(_id: string) {
  return '';
}
export async function UpdateSession(
  _id: string,
  _name: string,
  _color: string,
  _order: number,
) {
  return '';
}
export async function ListAgents() {
  return [];
}
export async function ListCustomAgents() {
  return [];
}
export async function SaveCustomAgents() {
  return undefined;
}
export async function CreateProject() {
  return '';
}
export async function KillProject() {
  return '';
}
export async function UpdateProject() {
  return '';
}
export async function LaunchDir() {
  return '';
}
export async function PickDirectory() {
  return '';
}
export async function OpenNewWindow() {
  return '';
}
export async function CloseWindow() {
  return '';
}
export async function IsGitRepo() {
  return false;
}
export async function OpenURL() {
  return '';
}
export async function OpenTerminalAt() {
  return '';
}
export async function Notify() {
  return '';
}
export async function LogFrontend() {
  return '';
}
export async function SetDebugTrace() {
  return '';
}
export async function Confirm() {
  return true;
}
export async function RestartDaemon() {
  return '';
}
export async function CheckForUpdate() {
  return null;
}

// Clipboard bindings. main.js imports ClipboardGetText (runtime) and
// SetClipboardText (App), both aliased to this bridge under
// VITE_WAILS_REAL=1; without these exports the ESM import throws at
// module load and the app never boots. In-memory is sufficient here.
let clipboard = '';
export async function ClipboardGetText() {
  return clipboard;
}
export async function SetClipboardText(text: unknown) {
  clipboard = String(text ?? '');
}

// Test hooks for Playwright. Smaller surface than the mock — there's
// no scripted state machine to poke; the daemon IS the state machine.
if (typeof window !== 'undefined') {
  window.__hive = {
    stdinLog,
    stdinText(id?: string) {
      return stdinLog
        .filter((e) => id == null || e.id === id)
        .map((e) => e.text)
        .join('');
    },
    resetStdin() {
      stdinLog.length = 0;
    },
    listeners,
    emit(name: string, ...args: unknown[]) {
      const arr = listeners.get(name) || [];
      for (const fn of arr) {
        try {
          fn(...args);
        } catch {
          /* */
        }
      }
    },
  };
}

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
  insertAfter?: string,
  branch?: string,
  worktreePath?: string,
  continueConversation?: boolean,
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
    // Resuming an existing worktree never creates one — the same
    // precedence the Go binding applies.
    use_worktree: worktreePath ? false : !!useWorktree,
    branch: branch || '',
    worktree_path: worktreePath || '',
    continue_conversation: !!continueConversation,
    insert_after_session_id: insertAfter || '',
  });
}
export async function ListWorktrees(projectID: string) {
  return call('ListWorktrees', { project_id: projectID || '' });
}
export async function RemoveWorktree(
  projectID: string,
  path: string,
  force: boolean,
  deleteBranch: boolean,
) {
  return call('RemoveWorktree', {
    project_id: projectID || '',
    path,
    force: !!force,
    delete_branch: !!deleteBranch,
  });
}
export async function CreateWorktree(projectID: string, branch: string) {
  return call('CreateWorktree', { project_id: projectID || '', branch });
}
export async function DeleteBranch(
  projectID: string,
  branch: string,
  force: boolean,
) {
  return call('DeleteBranch', {
    project_id: projectID || '',
    branch,
    force: !!force,
  });
}
export async function RenameWorktree(
  projectID: string,
  path: string,
  newBranch: string,
) {
  return call('RenameWorktree', {
    project_id: projectID || '',
    path,
    new_branch: newBranch,
  });
}
export async function DuplicateSession(
  _agentID?: string,
  _projectID?: string,
  _cwd?: string,
  insertAfter?: string,
) {
  return CreateSession('', '', 'dup', '', 80, 24, false, insertAfter);
}
export async function KillSession(id: string, force: boolean) {
  return call('KillSession', { session_id: id, force: !!force });
}
export async function KillSessionAndWorktree(id: string) {
  return call('KillSession', {
    session_id: id,
    force: true,
    remove_worktree: true,
  });
}
export async function RestoreSession(id: string) {
  return call('RestoreSession', { session_id: id });
}
export async function ListClosedSessions() {
  return call('ListClosedSessions', {});
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
// Non-empty, or persistence of the collapse/minimize sets stays off
// (store.ts › hydratePersistedProjectSets). The real suite runs against
// an isolated HIVE_STATE_DIR, so any stable id will do.
export async function StateDirID() {
  return 'e2ereal1';
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

// Reload and menu-bar bindings. Stubs, like the block above: the
// reload is a process relaunch the browser has no equivalent for, and
// the login item is a macOS service call. They exist because this
// module must mirror the mock's export surface — a name bridge.ts
// re-exports but neither harness defines is a module-load error that
// stops the app booting, not a missing method.
export async function ReloadGUI() {
  return '';
}
export async function RequestReloadAllGUIs() {
  return '';
}
export async function SetSessionAttention(id: string, want: boolean) {
  return call('SetSessionAttention', { session_id: id, want: !!want });
}
export async function MenuBarLoginItemStatus() {
  return 'not-registered';
}
export async function SetMenuBarLoginItem() {
  return '';
}
export async function CheckForUpdate() {
  return null;
}
export async function UpdateStatus() {
  return null;
}
export async function StartUpdate() {
  return '';
}
export async function ApplyUpdateAndRestart() {
  return '';
}
export async function GetUpdateSettings() {
  return { channel: 'release', source_repo: '' };
}
export async function SaveUpdateSettings() {
  return '';
}
export async function SourceRepoStatusFor() {
  return { path: '', detected: false, error: '' };
}

// Clipboard bindings. main.ts imports ClipboardGetText (runtime) and
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

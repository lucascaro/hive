// In-browser fake of the Wails bridge. Replaces the generated
// wailsjs/go/main/App + wailsjs/runtime/runtime modules so the
// frontend boots in plain Vite dev (and Playwright) without the
// native Wails runtime. Drives the UI through a tiny scripted
// daemon-state machine that Playwright can poke via window.__hive.

import type { ProjectInfo, SessionInfo } from '../../src/app/state.js';
// Type-only, so it is erased before Vite ever sees it — the whole point of
// this module is to stand in for wailsjs at runtime. Using the generated
// shapes (invariant 3) is what makes the mock drift-detectable.
import type { main } from '../../wailsjs/go/models';

type AgentInfo = main.AgentInfo;
type CustomAgent = main.CustomAgent;

// The wire shapes the daemon really sends carry a `created` stamp that no
// app module reads, so it is not on SessionInfo/ProjectInfo. Intersect
// rather than widen the source interfaces — the mock is the one that has
// to match the app, not the other way round.
// Exported for hive-global.d.ts, which types window.__hive.state off them.
export type MockSession = SessionInfo & { created: string };
export type MockProject = ProjectInfo & { created: string };

type Handler = (...args: unknown[]) => void;

const listeners = new Map<string, Handler[]>(); // event name -> [handler, ...]

function emit(name: string, ...args: unknown[]) {
  const arr = listeners.get(name) || [];
  for (const fn of arr) {
    try {
      fn(...args);
    } catch (_e) {
      /* swallow per Wails */
    }
  }
}

// --- runtime ---

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

// Clipboard bindings. main.js imports ClipboardGetText (runtime) and
// SetClipboardText (App), both aliased here. The mock keeps an in-memory
// clipboard so copy/paste paths don't throw at module load.
let clipboard = '';
export async function ClipboardGetText() {
  maybeFail('ClipboardGetText');
  return clipboard;
}
export async function SetClipboardText(text: unknown) {
  maybeFail('SetClipboardText');
  clipboard = String(text ?? '');
}

// --- failure injection ---
//
// window.__hive.failNext(method, message) arms a one-shot rejection for
// the named binding, so E2E can assert that a failed daemon call
// surfaces user-visible feedback instead of being silently swallowed.
const failures = new Map<string, string>(); // method name -> error message
function maybeFail(method: string) {
  const msg = failures.get(method);
  if (msg !== undefined) {
    failures.delete(method);
    throw new Error(msg);
  }
}

// window.__hive.delayNext(method, ms) arms a one-shot delay for the
// named binding, so E2E can observe in-flight UI (loading rows,
// spinners) that a microtask-fast mock would otherwise skip past.
const delays = new Map<string, number>(); // method name -> ms
async function maybeDelay(method: string) {
  const ms = delays.get(method);
  if (ms !== undefined) {
    delays.delete(method);
    await new Promise((resolve) => setTimeout(resolve, ms));
  }
}

// --- App bindings ---

const state: { projects: MockProject[]; sessions: MockSession[] } = {
  projects: [
    {
      id: 'p1',
      name: 'default',
      color: '#888',
      cwd: '',
      order: 0,
      created: new Date().toISOString(),
    },
  ],
  sessions: [
    {
      id: 's1',
      name: 'main',
      color: '#abc',
      order: 0,
      created: new Date().toISOString(),
      alive: true,
      agent: '',
      project_id: 'p1',
      worktree_path: '',
      worktree_branch: '',
      last_error: '',
    },
  ],
};

function broadcast() {
  emit('project:list', JSON.stringify({ projects: state.projects }));
  emit('session:list', JSON.stringify({ sessions: state.sessions }));
}

// Wails control bindings — frontend imports these from
// ../wailsjs/go/main/App.
export async function ConnectControl() {
  setTimeout(broadcast, 0);
  return '';
}
export async function OpenSession(id: string) {
  return id;
}
export async function CloseAttach(_id: string) {
  return '';
}
// Populated by WriteStdin so E2E can assert input routing.
const stdinLog: { id: string; b64: string; text: string }[] = [];
export async function WriteStdin(id: string, b64: string) {
  let text = '';
  try {
    // atob() returns a Latin-1 "binary string" — feed each char's
    // code unit into a Uint8Array, then decode as UTF-8 so non-ASCII
    // input round-trips correctly in E2E assertions. This module only
    // ever loads in a browser (Vite substitution), so atob/TextDecoder
    // are always there; the old Buffer fallback was dead.
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    text = new TextDecoder('utf-8').decode(bytes);
  } catch {
    /* ignore decode errors — test hook only */
  }
  stdinLog.push({ id, b64, text });
  return '';
}
export async function ResizeSession(_id: string, _cols: number, _rows: number) {
  return '';
}
// Populated by RequestScrollbackReplay so E2E can detect spurious replays
// after layout reflows.
const replayLog: { id: string; t: number }[] = [];
export async function RequestScrollbackReplay(id: string) {
  replayLog.push({ id, t: Date.now() });
  return '';
}
// Positional args matching the real Wails binding:
// CreateSession(agentID, projectID, name, color, cols, rows, useWorktree).
export async function CreateSession(
  agentID: string,
  projectID: string,
  name: string,
  color: string,
  _cols: number,
  _rows: number,
  _useWorktree: boolean,
) {
  maybeFail('CreateSession');
  const id = `mock-${state.sessions.length + 1}`;
  const s: MockSession = {
    id,
    name: name || `s${state.sessions.length + 1}`,
    color: color || '#0af',
    order: state.sessions.length,
    created: new Date().toISOString(),
    alive: true,
    agent: agentID || '',
    project_id: projectID || 'p1',
    worktree_path: '',
    worktree_branch: '',
    last_error: '',
  };
  state.sessions.push(s);
  emit('session:event', JSON.stringify({ kind: 'added', session: s }));
  return id;
}
// Positional: DuplicateSession(agentID, projectID, cwd).
export async function DuplicateSession(
  agentID: string,
  projectID: string,
  _cwd: string,
) {
  maybeFail('DuplicateSession');
  return CreateSession(agentID, projectID, 'dup', '', 0, 0, false);
}
export async function KillSession(id: string) {
  maybeFail('KillSession');
  const i = state.sessions.findIndex((s) => s.id === id);
  if (i < 0) return '';
  const [removed] = state.sessions.splice(i, 1);
  emit('session:event', JSON.stringify({ kind: 'removed', session: removed }));
  return '';
}
export async function RestartSession(_id: string) {
  maybeFail('RestartSession');
  return '';
}
// Positional args matching the real Wails binding (and the e2e-real
// bridge): UpdateSession(id, name, color, order). Empty string / -1
// mean "no change". The old object-shaped signature silently no-op'd
// every rename driven through the UI.
export async function UpdateSession(
  id: string,
  name: string,
  color: string,
  _order: number,
) {
  maybeFail('UpdateSession');
  const s = state.sessions.find((x) => x.id === id);
  if (!s) return '';
  if (name) s.name = name;
  if (color) s.color = color;
  emit('session:event', JSON.stringify({ kind: 'updated', session: s }));
  return '';
}
// Custom agents start empty; the settings modal writes into this
// module-level array so a save → reopen round-trip works in E2E.
let customAgents: CustomAgent[] = [];
// One real-shaped agent so launcher E2E can exercise the full
// open → select → create flow (matches internal/agent's wire shape).
export async function ListAgents(): Promise<AgentInfo[]> {
  await maybeDelay('ListAgents');
  maybeFail('ListAgents');
  // Mirrors Go's agent.All(): built-ins first, then custom agents.
  // The merge lives there, which is why the launcher needs no
  // custom-agent code of its own.
  return [
    {
      id: 'shell',
      name: 'Shell',
      color: '#888',
      available: true,
      installCmd: [],
    },
    // Second built-in so the launcher's filter box has something to
    // narrow. Order matters: several specs assert the FIRST
    // .launcher-item is "Shell", and a fresh browser context has an
    // empty hive.agentUsage, so the usage sort is a no-op here.
    {
      id: 'claude',
      name: 'Claude',
      color: '#d97757',
      available: true,
      installCmd: [],
    },
    ...customAgents.map((a) => ({
      id: a.id,
      name: a.name,
      color: a.color,
      available: true,
      installCmd: [],
    })),
  ];
}
export async function ListCustomAgents() {
  await maybeDelay('ListCustomAgents');
  maybeFail('ListCustomAgents');
  return customAgents;
}
export async function SaveCustomAgents(list: CustomAgent[] | null) {
  await maybeDelay('SaveCustomAgents');
  maybeFail('SaveCustomAgents');
  // Mirror Go's id assignment so the mock round-trips like the real
  // binding: ids are slugged once and never recomputed on rename.
  customAgents = (list || []).map((a) => ({
    ...a,
    id:
      a.id ||
      String(a.name || '')
        .trim()
        .toLowerCase()
        .replace(/[^\p{L}\p{N}]+/gu, '-')
        .replace(/^-|-$/g, ''),
  }));
}
// Project mutations are positional too, matching the real bindings
// (cmd/hivegui/app.go): CreateProject(name, color, cwd),
// KillProject(id, killSessions), UpdateProject(id, name, color, cwd,
// order). The old object-shaped/no-op forms silently no-op'd every
// project create/save/delete driven through the UI — the same defect
// UpdateSession had.
export async function CreateProject(name: string, color: string, cwd: string) {
  maybeFail('CreateProject');
  const id = `p-${state.projects.length + 1}`;
  const p: MockProject = {
    id,
    name: name || 'new',
    color: color || '#0af',
    cwd: cwd || '',
    order: state.projects.length,
    created: new Date().toISOString(),
  };
  state.projects.push(p);
  emit('project:event', JSON.stringify({ kind: 'added', project: p }));
  return id;
}
export async function KillProject(id: string, killSessions: boolean) {
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
export async function UpdateProject(
  id: string,
  name: string,
  color: string,
  cwd: string,
  _order: number,
) {
  maybeFail('UpdateProject');
  const p = state.projects.find((x) => x.id === id);
  if (!p) return '';
  if (name) p.name = name;
  if (color) p.color = color;
  if (cwd) p.cwd = cwd;
  emit('project:event', JSON.stringify({ kind: 'updated', project: p }));
  return '';
}
export async function LaunchDir() {
  return '';
}
export async function PickDirectory() {
  return '';
}
export async function OpenNewWindow() {
  maybeFail('OpenNewWindow');
  return '';
}
export async function CloseWindow() {
  maybeFail('CloseWindow');
  return '';
}
export async function IsGitRepo(_dir: string) {
  return false;
}
export async function OpenURL(_url: string) {
  maybeFail('OpenURL');
  return '';
}
export async function OpenTerminalAt(_dir: string) {
  maybeFail('OpenTerminalAt');
  return '';
}
export async function Notify(_title: string, _body: string) {
  return '';
}
export async function LogFrontend(_msg: string) {
  return '';
}
export async function SetDebugTrace(_on: boolean) {
  return '';
}
export async function Confirm(_title: string, _body: string) {
  return true;
}
export async function RestartDaemon() {
  return '';
}
export async function CheckForUpdate() {
  return null;
}

// Test hook: lets Playwright inject events / inspect state.
if (typeof window !== 'undefined') {
  window.__hive = {
    state,
    emit,
    addSession(name: string) {
      return CreateSession('', 'p1', name, '', 0, 0, false);
    },
    killSession(id: string) {
      return KillSession(id);
    },
    listeners,
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
    replayLog,
    replayCount(id?: string) {
      return replayLog.filter((e) => id == null || e.id === id).length;
    },
    resetReplay() {
      replayLog.length = 0;
    },
    failNext(method: string, message = 'injected failure') {
      failures.set(method, message);
    },
    delayNext(method: string, ms = 250) {
      delays.set(method, ms);
    },
  };
}

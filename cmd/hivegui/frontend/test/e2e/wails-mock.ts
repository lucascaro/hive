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
// `continued` is mock-only: it records whether the session asked the
// agent to resume its previous conversation, which the real daemon
// expresses as an argv choice the browser cannot see.
export type MockSession = SessionInfo & {
  created: string;
  continued?: boolean;
};
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

// Clipboard bindings. main.ts imports ClipboardGetText (runtime) and
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

// window.__hive.phaseHold(ms) stretches the session lifecycle phases
// the daemon walks (create: starting -> worktree -> ready; kill:
// closing -> removed) so E2E can observe the loading panel and the
// closing state. Zero (the default) still goes through the same
// phases, one task apart, so every spec exercises the real ordering.
let phaseHoldMs = 0;
function afterPhaseHold(fn: () => void) {
  setTimeout(fn, phaseHoldMs);
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

// MockWorktree mirrors wire.WorktreeInfo (snake_case on the wire).
export type MockWorktree = {
  path: string;
  branch?: string;
  detached?: boolean;
  is_main?: boolean;
  uncommitted?: boolean;
  unpushed?: number;
  unknown?: boolean;
  session_ids?: string[];
};
export type MockBranch = {
  name: string;
  upstream?: string;
  ahead?: number;
  merged?: boolean;
};

const state: {
  projects: MockProject[];
  sessions: MockSession[];
  worktrees: MockWorktree[];
  orphanBranches: MockBranch[];
} = {
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
  // Seeded empty: specs that need worktrees push them via
  // window.__hive.seedWorktrees so each spec owns its own fixture.
  worktrees: [],
  orphanBranches: [],
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
  // The real daemon replays the session's scrollback on every attach
  // and brackets it with these events. The GUI uses replay_done as the
  // cue to drop the loading panel, so a mock that never replays would
  // leave every tile spinning until the fallback timer.
  queueMicrotask(() => {
    emit('pty:event', id, JSON.stringify({ kind: 'scrollback_replay_begin' }));
    emit('pty:event', id, JSON.stringify({ kind: 'scrollback_replay_done' }));
  });
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
let nextSessionSeq = 0;
let nextProjectSeq = 0;
// state.sessions IS the order, mirroring the daemon's single global
// r.order list; renumber rewrites each session's .order to its index,
// the same invariant registry.reindexLocked keeps.
function renumber() {
  state.sessions.forEach((s, i) => {
    s.order = i;
  });
}
function renumberProjects() {
  state.projects.forEach((p, i) => {
    p.order = i;
  });
}
// emitUpdatedExcept mirrors the daemon's post-reorder fan-out: every
// session whose .order shifted is broadcast so clients don't keep stale
// values.
function emitUpdatedExcept(skipId: string) {
  for (const other of state.sessions) {
    if (other.id === skipId) continue;
    emit('session:event', JSON.stringify({ kind: 'updated', session: other }));
  }
}
// Positional args matching the real Wails binding:
// CreateSession(agentID, projectID, name, color, cols, rows, useWorktree,
// insertAfter, branch, worktreePath, continueConversation).
export async function CreateSession(
  agentID: string,
  projectID: string,
  name: string,
  color: string,
  _cols: number,
  _rows: number,
  _useWorktree: boolean,
  insertAfter?: string,
  branch?: string,
  worktreePath?: string,
  continueConversation?: boolean,
) {
  maybeFail('CreateSession');
  // Monotonic, NOT derived from the current length: after a kill the
  // length rewinds and a length-based id collides with a session that
  // is still alive, which silently drops the new row from the sidebar.
  nextSessionSeq += 1;
  const id = `mock-${nextSessionSeq}`;
  const pid = projectID || 'p1';
  const s: MockSession = {
    id,
    name: name || `s${state.sessions.length + 1}`,
    color: color || '#0af',
    order: state.sessions.length,
    created: new Date().toISOString(),
    alive: false,
    phase: 'starting',
    agent: agentID || '',
    project_id: pid,
    worktree_path: '',
    worktree_branch: '',
    last_error: '',
  };
  // Splice after the anchor only when it exists AND shares the new
  // session's project — exactly registry.insertEntry's guard.
  const anchorIdx = insertAfter
    ? state.sessions.findIndex(
        (x) => x.id === insertAfter && x.project_id === pid,
      )
    : -1;
  const pos = anchorIdx >= 0 ? anchorIdx + 1 : state.sessions.length;
  state.sessions.splice(pos, 0, s);
  renumber();
  if (pos !== state.sessions.length - 1) emitUpdatedExcept(id);
  // The daemon announces the entry before the worktree and PTY exist
  // (wire.PhaseStarting) and only later reports it ready. Mirror that
  // ordering: the GUI gates its attach on it.
  emit('session:event', JSON.stringify({ kind: 'added', session: s }));
  afterPhaseHold(() => {
    if (!state.sessions.includes(s)) return; // killed mid-create
    if (worktreePath) {
      // Resuming an existing worktree: the daemon ADOPTS it rather
      // than creating one, and the entry must carry the path — an
      // unclaimed worktree is what the startup reclaim deletes.
      const existing = state.worktrees.find((w) => w.path === worktreePath);
      s.worktree_path = worktreePath;
      s.worktree_branch = existing?.branch || '';
      s.continued = !!continueConversation;
      if (existing)
        existing.session_ids = [...(existing.session_ids ?? []), id];
      emit('session:event', JSON.stringify({ kind: 'updated', session: s }));
    } else if (_useWorktree) {
      s.phase = 'worktree';
      s.worktree_branch = branch || s.name;
      s.worktree_path = `/mock/.worktrees/${s.worktree_branch}`;
      state.worktrees.push({
        path: s.worktree_path,
        branch: s.worktree_branch,
        session_ids: [id],
      });
      emit('session:event', JSON.stringify({ kind: 'updated', session: s }));
    }
    afterPhaseHold(() => {
      if (!state.sessions.includes(s)) return;
      s.phase = '';
      s.alive = true;
      emit('session:event', JSON.stringify({ kind: 'updated', session: s }));
    });
  });
  return id;
}
// Positional: DuplicateSession(agentID, projectID, cwd, insertAfter).
export async function DuplicateSession(
  agentID: string,
  projectID: string,
  _cwd: string,
  insertAfter?: string,
) {
  maybeFail('DuplicateSession');
  return CreateSession(agentID, projectID, 'dup', '', 0, 0, false, insertAfter);
}
export async function KillSessionAndWorktree(id: string) {
  maybeFail('KillSessionAndWorktree');
  const s = state.sessions.find((x) => x.id === id);
  const path = s?.worktree_path;
  await KillSession(id);
  if (path) {
    const i = state.worktrees.findIndex((w) => w.path === path);
    if (i >= 0) state.worktrees.splice(i, 1);
  }
  return '';
}
export async function KillSession(id: string) {
  maybeFail('KillSession');
  const i = state.sessions.findIndex((s) => s.id === id);
  if (i < 0) return '';
  const doomed = state.sessions[i];
  // Teardown is announced (wire.PhaseClosing) before the session is
  // gone: on a real worktree the git cleanup in between takes seconds.
  doomed.phase = 'closing';
  emit('session:event', JSON.stringify({ kind: 'updated', session: doomed }));
  afterPhaseHold(() => {
    const j = state.sessions.findIndex((s) => s.id === id);
    if (j < 0) return;
    const [removed] = state.sessions.splice(j, 1);
    // A kill shifts every later session down a slot, so the daemon
    // recompacts .order and broadcasts the survivors (registry.Kill).
    // Leaving holes here would model a bug the daemon no longer has —
    // and it hid this one: stale .order made the next reorder, which
    // sends an absolute index, land in the wrong slot.
    renumber();
    emit(
      'session:event',
      JSON.stringify({ kind: 'removed', session: removed }),
    );
    emitUpdatedExcept(removed.id);
  });
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
  order: number,
) {
  maybeFail('UpdateSession');
  const s = state.sessions.find((x) => x.id === id);
  if (!s) return '';
  if (name) s.name = name;
  if (color) s.color = color;
  if (order >= 0) {
    // Delete-then-clamp-then-insert, the same shape as
    // registry.moveInOrder — which is what makes the target index the
    // frontend sends meaningful.
    const cur = state.sessions.indexOf(s);
    state.sessions.splice(cur, 1);
    const at = Math.min(Math.max(order, 0), state.sessions.length);
    state.sessions.splice(at, 0, s);
    renumber();
    emitUpdatedExcept(id);
  }
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
  // Monotonic for the same reason as nextSessionSeq — a length-derived
  // id collides with a live project after a delete, and the `added`
  // handler drops the duplicate instead of adding it.
  nextProjectSeq += 1;
  const id = `p-${nextProjectSeq}`;
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
  // The daemon reassigns the orphaned sessions to the first surviving
  // project rather than leaving them pointing at a deleted one
  // (registry/projects.go KillProject).
  const survivor = state.projects.find((p) => p.id !== id);
  for (let j = state.sessions.length - 1; j >= 0; j--) {
    if (state.sessions[j].project_id !== id) continue;
    if (killSessions) {
      const [rs] = state.sessions.splice(j, 1);
      emit('session:event', JSON.stringify({ kind: 'removed', session: rs }));
    } else if (survivor) {
      state.sessions[j].project_id = survivor.id;
    }
  }
  const [removed] = state.projects.splice(i, 1);
  // Same compaction the daemon does in KillProject, for both lists —
  // and the same fan-out, so the GUI doesn't keep stale .order values
  // and misplace the next project reorder.
  renumber();
  renumberProjects();
  emit('project:event', JSON.stringify({ kind: 'removed', project: removed }));
  for (const p of state.projects) {
    emit('project:event', JSON.stringify({ kind: 'updated', project: p }));
  }
  for (const s of state.sessions) {
    emit('session:event', JSON.stringify({ kind: 'updated', session: s }));
  }
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

// --- worktrees ---
//
// A small stand-in for the daemon's inventory. Every mutation replies
// with a fresh worktree:list event, exactly as the daemon replies with
// a WORKTREES frame — so the modal's refresh path is what the tests
// exercise, not a shortcut.

function worktreesPayload(projectID: string) {
  return {
    project_id: projectID,
    repo_root: '/mock',
    worktrees: [
      { path: '/mock', branch: 'main', is_main: true },
      ...state.worktrees,
    ],
    orphan_branches: state.orphanBranches,
  };
}

function emitWorktrees(projectID: string) {
  emit('worktree:list', JSON.stringify(worktreesPayload(projectID)));
}

export async function ListWorktrees(projectID: string) {
  maybeFail('ListWorktrees');
  await maybeDelay('ListWorktrees');
  emitWorktrees(projectID);
  return '';
}

export async function RemoveWorktree(
  projectID: string,
  path: string,
  force: boolean,
  deleteBranch: boolean,
) {
  maybeFail('RemoveWorktree');
  const i = state.worktrees.findIndex((w) => w.path === path);
  if (i < 0) return '';
  const w = state.worktrees[i];
  // Mirror the daemon's refusal order so the GUI's confirm-and-retry
  // path is exercised against the same codes it sees in production.
  if ((w.session_ids ?? []).length > 0) {
    emit(
      'control:error',
      JSON.stringify({
        code: 'worktree_in_use',
        message: 'sessions are running',
      }),
    );
    return '';
  }
  if (!force && w.uncommitted) {
    emit(
      'control:error',
      JSON.stringify({
        code: 'worktree_dirty',
        message: 'uncommitted changes',
      }),
    );
    return '';
  }
  if (!force && ((w.unpushed ?? 0) > 0 || w.unknown)) {
    emit(
      'control:error',
      JSON.stringify({
        code: 'worktree_unpushed',
        message: 'unpushed commits',
      }),
    );
    return '';
  }
  state.worktrees.splice(i, 1);
  if (w.branch && !deleteBranch) {
    state.orphanBranches.push({ name: w.branch });
  }
  emitWorktrees(projectID);
  return '';
}

export async function CreateWorktree(projectID: string, branch: string) {
  maybeFail('CreateWorktree');
  const i = state.orphanBranches.findIndex((b) => b.name === branch);
  if (i >= 0) state.orphanBranches.splice(i, 1);
  state.worktrees.push({ path: `/mock/.worktrees/${branch}`, branch });
  emitWorktrees(projectID);
  return '';
}

export async function DeleteBranch(
  projectID: string,
  branch: string,
  force: boolean,
) {
  maybeFail('DeleteBranch');
  const i = state.orphanBranches.findIndex((b) => b.name === branch);
  if (i < 0) return '';
  if (!force && !state.orphanBranches[i].merged) {
    emit(
      'control:error',
      JSON.stringify({ code: 'branch_unmerged', message: 'unmerged commits' }),
    );
    return '';
  }
  state.orphanBranches.splice(i, 1);
  emitWorktrees(projectID);
  return '';
}

export async function RenameWorktree(
  projectID: string,
  path: string,
  newBranch: string,
) {
  maybeFail('RenameWorktree');
  const w = state.worktrees.find((x) => x.path === path);
  if (!w) return '';
  if ((w.session_ids ?? []).length > 0) {
    emit(
      'control:error',
      JSON.stringify({
        code: 'worktree_in_use',
        message: 'sessions are running',
      }),
    );
    return '';
  }
  w.branch = newBranch;
  w.path = `/mock/.worktrees/${newBranch}`;
  emitWorktrees(projectID);
  return '';
}

// Test hook: lets Playwright inject events / inspect state.
if (typeof window !== 'undefined') {
  window.__hive = {
    state,
    emit,
    // projectId defaults to the seed project; pass it to build an
    // r.order that interleaves projects, which is where display position
    // and .order diverge.
    addSession(name: string, insertAfter?: string, projectId?: string) {
      return CreateSession(
        '',
        projectId || 'p1',
        name,
        '',
        0,
        0,
        false,
        insertAfter,
      );
    },
    createSessionWithWorktree(name: string, branch?: string) {
      return CreateSession('', 'p1', name, '', 0, 0, true, undefined, branch);
    },
    seedWorktrees(worktrees: MockWorktree[], branches: MockBranch[] = []) {
      state.worktrees.length = 0;
      state.worktrees.push(...worktrees);
      state.orphanBranches.length = 0;
      state.orphanBranches.push(...branches);
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
    phaseHold(ms = 250) {
      phaseHoldMs = ms;
    },
  };
}

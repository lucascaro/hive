// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// ---------- workflows: transport, run loader, waiting tracker (spec 530) ----------
//
// Everything the Workflows modal says to the engine goes over A's
// PLUGIN_MESSAGE to the `workflows` plugin (sendPluginMessage, plugins.ts).
// A request carries a nonce; the engine answers on `<topic>.reply` with the
// same nonce. Every window receives every reply, so a reply for another
// window's nonce finds no pending entry and is ignored.
//
// Opening a run (loadRunState) subscribes to live `run.event` pushes FIRST,
// then reads `run.get` and pages `run.nodes`. Each page carries the
// `last_seq` it reflects, so a buffered event applies to a page's keys only
// when `seq > last_seq` (lib/workflow-overlay.ts). Nothing replays events
// from seq 0; `run.events` is paged lazily for one node's detail pane.

import { flushSync } from 'react-dom';
import { Notify, SetPluginEnabled } from '../bridge.js';
import { withKey } from '../lib/chord-label.js';
import { showControls } from '../lib/workflow-text.js';
import { releaseFocus } from '../lib/focus-trap.js';
import {
  applyPage,
  emptyOverlay,
  fold,
  hasGap,
} from '../lib/workflow-overlay.js';
import {
  type IR,
  type NodeGet,
  type NodesPage,
  type RunDetail,
  type RunEvent,
  type RunSummary,
  type TypeErrorRow,
  WORKFLOWS_PLUGIN_ID,
} from '../lib/workflow-types.js';
import {
  appStore,
  closeModal,
  hideBanner,
  isModalOpen,
  modalEntry,
  openModal,
  setBanner,
} from '../store/store.js';
import {
  type WaitingRun,
  beginRun,
  clearRun,
  patchRun,
  selectKey,
  setHistory,
  setView,
  setWaiting,
  workflowsStore,
} from '../store/workflows.js';
import { shortcutLabel } from './bindings.js';
import { pageEl } from './el.js';
import { dismissChoiceDialog } from './modals/choice-dialog.js';
import { flashStatus } from './dom.js';
import { newNonce, onPluginMessage, sendPluginMessage } from './plugins.js';
import type { PluginInfo, ProjectInfo } from './state.js';
import { activeProjectId } from './selectors.js';

// ---------- deps ----------

export interface WorkflowsDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
  switchTo: (id: string) => void;
}

let deps: WorkflowsDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
  switchTo: () => {},
};

export function initWorkflows(injected: WorkflowsDeps): void {
  deps = injected;
  wire();
}

// ---------- requests ----------

/** An error the engine answered with, or the transport raised. */
export class WorkflowError extends Error {
  readonly code: string;
  readonly type_errors?: TypeErrorRow[];
  readonly errors?: string[];
  constructor(
    code: string,
    message: string,
    extra: { type_errors?: TypeErrorRow[]; errors?: string[] } = {},
  ) {
    super(message);
    this.name = 'WorkflowError';
    this.code = code;
    this.type_errors = extra.type_errors;
    this.errors = extra.errors;
  }
}

// B's per-message budget, under A's 64 KiB MaxPluginMessage. The daemon's
// own refusal carries no nonce, so it would surface only as a timeout.
export const MAX_MESSAGE_BYTES = 48 * 1024;
export const REQUEST_TIMEOUT_MS = 15_000;
// The first build may download the type checker.
export const BUILD_TIMEOUT_MS = 180_000;

interface Pending {
  resolve: (data: unknown) => void;
  reject: (e: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}
const pending = new Map<string, Pending>();

function settle(nonce: string): Pending | null {
  const p = pending.get(nonce);
  if (!p) return null;
  clearTimeout(p.timer);
  pending.delete(nonce);
  return p;
}

export function request<T>(
  topic: string,
  data: Record<string, unknown> = {},
  opts: { timeoutMs?: number } = {},
): Promise<T> {
  wire();
  const nonce = newNonce();
  const payload = { ...data, nonce };
  // Bytes on the wire, not UTF-16 code units: the budget is A's byte cap.
  if (
    new TextEncoder().encode(JSON.stringify(payload)).length > MAX_MESSAGE_BYTES
  ) {
    return Promise.reject(
      new WorkflowError('too_large', 'The input is too large to send.'),
    );
  }
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(nonce);
      reject(
        new WorkflowError('timeout', 'The workflow engine did not answer.'),
      );
    }, opts.timeoutMs ?? REQUEST_TIMEOUT_MS);
    pending.set(nonce, {
      resolve: resolve as (d: unknown) => void,
      reject,
      timer,
    });
    sendPluginMessage(WORKFLOWS_PLUGIN_ID, topic, payload).catch(
      (e: unknown) => {
        if (!settle(nonce)) return;
        reject(e instanceof Error ? e : new Error(String(e)));
      },
    );
  });
}

interface Reply {
  nonce?: string;
  ok?: boolean;
  error?: {
    code?: string;
    message?: string;
    type_errors?: TypeErrorRow[];
    errors?: string[];
  };
  type_errors?: TypeErrorRow[];
  errors?: string[];
}

let wired = false;
function wire(): void {
  if (wired) return;
  wired = true;
  onPluginMessage((m) => {
    if (m.plugin_id !== WORKFLOWS_PLUGIN_ID) return;
    if (m.topic === 'run.event') {
      const d = m.data as { run_id?: string; events?: RunEvent[] } | undefined;
      if (d?.run_id && Array.isArray(d.events)) onRunEvents(d.run_id, d.events);
      return;
    }
    if (!m.topic.endsWith('.reply')) return;
    const r = (m.data ?? {}) as Reply;
    if (!r.nonce) return;
    const p = settle(r.nonce);
    if (!p) return; // another window's request
    if (r.ok === false) {
      const e = r.error ?? {};
      p.reject(
        new WorkflowError(
          e.code ?? 'error',
          e.message ?? 'The engine refused.',
          {
            type_errors: e.type_errors ?? r.type_errors,
            errors: e.errors ?? r.errors,
          },
        ),
      );
    } else {
      p.resolve(r);
    }
  });
}

/** A `plugin_not_running` control:error carries no nonce and no plugin id,
 * and this is the only app code that sends to a plugin: reject everything
 * pending and tell the caller the generic error line can stand aside. */
export function claimWorkflowsNotRunning(e: { code?: string }): boolean {
  if (e.code !== 'plugin_not_running' || pending.size === 0) return false;
  for (const nonce of [...pending.keys()]) {
    settle(nonce)?.reject(
      new WorkflowError(
        'plugin_not_running',
        'The workflow engine is not running.',
      ),
    );
  }
  return true;
}

// ---------- plugin state ----------

export const workflowsPlugin = (): PluginInfo | undefined =>
  appStore.getState().plugins.find((p) => p.id === WORKFLOWS_PLUGIN_ID);

const engineRunning = (): boolean => {
  const p = workflowsPlugin();
  return !!p && p.enabled && p.status === 'running';
};

/** Turn on / Restart: both are SET_PLUGIN_ENABLED true. */
export function startWorkflowsPlugin(): void {
  SetPluginEnabled(WORKFLOWS_PLUGIN_ID, true).catch((e: unknown) =>
    flashStatus(`could not start the workflow engine: ${e}`, true),
  );
}

// ---------- open / close ----------

export function openWorkflows(project: ProjectInfo | null): void {
  if (!project) {
    flashStatus('Open a project first', true);
    return;
  }
  stopRun();
  setView('list', { file: null, runId: null, selectedKey: null });
  openModal({
    id: 'workflows',
    projectId: project.id,
    projectName: project.name ?? '',
  });
  deps.setFocusedTile(null);
}

export function openWorkflowsForActiveProject(): void {
  const pid = activeProjectId();
  openWorkflows(appStore.getState().projects.find((p) => p.id === pid) ?? null);
}

export function closeWorkflows(): void {
  if (!isModalOpen('workflows')) return;
  dismissChoiceDialog();
  releaseFocus(pageEl('workflows'));
  stopRun();
  flushSync(() => closeModal('workflows'));
  deps.refocusActiveTerm();
}

/** The `workflows` command: the native accelerator takes the key before
 * the webview, so the command itself must close when open. */
export function toggleWorkflows(): void {
  if (isModalOpen('workflows')) closeWorkflows();
  else openWorkflowsForActiveProject();
}

// The trust view's confirm, registered while it can act (loaded, nothing
// in flight) so the `y` key reaches it without a component in the key path.
let confirmTrust: (() => void) | null = null;
export function setTrustConfirm(fn: (() => void) | null): void {
  confirmTrust = fn;
}
/** `y` in the trust view. */
export function trustConfirmKey(): void {
  confirmTrust?.();
}

/** Escape: back one view, or close from the list. */
export function workflowsBack(): void {
  if (workflowsStore.getState().view === 'list') {
    closeWorkflows();
    return;
  }
  stopRun();
  setView('list', { file: null, runId: null, selectedKey: null });
}

export const workflowsProjectId = (): string =>
  modalEntry('workflows')?.projectId ?? '';

/** Opens the modal on a run, with a node selected when given. */
export async function openWorkflowRun(
  runId: string,
  key: string | null = null,
): Promise<void> {
  let info = runInfo.get(runId);
  if (!info) info = await ensureRunInfo(runId);
  const project = appStore
    .getState()
    .projects.find((p) => p.id === info?.project_id);
  if (!info || !project) {
    flashStatus('That workflow run is not in an open project', true);
    return;
  }
  stopRun();
  setView('run', { file: null, runId, selectedKey: key });
  openModal({
    id: 'workflows',
    projectId: project.id,
    projectName: project.name ?? '',
  });
  deps.setFocusedTile(null);
  void loadRunState(runId);
}

/** From the banner. */
export function openWaitingWorkflow(): void {
  const first = workflowsStore.getState().waiting[0];
  if (first) void openWorkflowRun(first.run_id);
}

// ---------- the run loader ----------

interface Loader {
  runId: string;
  token: number;
  loading: boolean;
  buffer: RunEvent[];
}
let loader: Loader | null = null;
let loadToken = 0;

function stopRun(): void {
  loader = null;
  loadToken += 1;
  clearRun();
}

/** Loads a run into the store: run.get, then run.nodes pages with the
 * live events buffered meanwhile merged by each page's last_seq. Used on
 * open, on a gap and on reconnect. */
export async function loadRunState(
  runId: string,
  opts: { reload?: boolean } = {},
): Promise<void> {
  const prev = workflowsStore.getState().run;
  const keep = opts.reload && prev?.runId === runId ? prev : null;
  const token = ++loadToken;
  // Subscribe first: events pushed from here on are buffered, so none
  // committed after a page's read can be missed.
  const L: Loader = {
    runId,
    token,
    loading: true,
    buffer: loader?.runId === runId ? loader.buffer : [],
  };
  loader = L;
  if (keep) patchRun({ status: 'loading', error: null });
  else beginRun(runId);
  const stale = () => loader !== L || token !== loadToken;
  try {
    // run.nodes carries no run-level state, and a reload happens exactly when
    // events were lost (a gap, a reconnect): re-read the run so a missed
    // run.finished or run.retried cannot leave the header stale. The IR does
    // not change, so a reload keeps it.
    const g = await request<{ run: RunDetail; ir: IR }>('run.get', {
      run_id: runId,
    });
    if (stale()) return;
    const detail = g.run;
    const ir = keep?.ir ?? g.ir;
    rememberRun(detail);
    let ov = emptyOverlay(runId, detail.state);
    let after: string | undefined;
    for (;;) {
      const page = await request<NodesPage>('run.nodes', {
        run_id: runId,
        ...(after ? { after } : {}),
      });
      if (stale()) return;
      ov = applyPage(ov, page, ir);
      const keys = new Set(page.nodes.map((r) => r.key));
      // Events for this page's keys: those with seq <= last_seq are
      // already in the rows (fold drops them); the rest apply now.
      const mine = L.buffer.filter((e) => e.key !== null && keys.has(e.key));
      L.buffer = L.buffer.filter((e) => e.key === null || !keys.has(e.key));
      ov = fold(ov, mine, ir);
      if (!page.more || !page.next_after) break;
      after = page.next_after;
    }
    // Keys created after every page was read, and run-level events.
    ov = fold(ov, L.buffer, ir);
    L.buffer = [];
    L.loading = false;
    patchRun({
      status: 'ready',
      error: null,
      detail,
      ir,
      overlay: ov,
    });
    syncWaitingFrom(runId, ov);
  } catch (e) {
    if (stale()) return;
    L.loading = false;
    patchRun({
      status: 'error',
      error: e instanceof Error ? e.message : String(e),
    });
  }
}

function onRunEvents(runId: string, events: RunEvent[]): void {
  trackWaiting(runId, events);
  appendHistory(runId, events);
  if (!loader || loader.runId !== runId) return;
  if (loader.loading) {
    loader.buffer.push(...events);
    return;
  }
  const run = workflowsStore.getState().run;
  if (!run?.ir || run.runId !== runId) return;
  if (hasGap(run.overlay, events)) {
    loader.buffer.push(...events);
    void loadRunState(runId, { reload: true });
    return;
  }
  const overlay = fold(run.overlay, events, run.ir);
  patchRun({ overlay, detail: detailAfter(run.detail, events) });
}

// Keeps the header's run state in step; the overlay holds the rest.
function detailAfter(
  d: RunDetail | null,
  events: RunEvent[],
): RunDetail | null {
  if (!d) return d;
  let state = d.state;
  for (const ev of events) {
    if (ev.type === 'run.finished') {
      state = (ev.data as { state?: RunDetail['state'] }).state ?? state;
    } else if (ev.type === 'run.retried') state = 'running';
  }
  return state === d.state ? d : { ...d, state };
}

// ---------- node detail ----------

/** node.get: the one place the slim run.nodes rows' missing facts (wait
 * payload, session, branch, worktree, error, conflict) and the output
 * slices come from (Q-B3, Q-B2). */
export function getNode(
  runId: string,
  key: string,
  offset = 0,
): Promise<NodeGet> {
  return request<NodeGet>('node.get', { run_id: runId, key, offset });
}

const HISTORY_KEEP = 200;
// ponytail: run.events has no key filter, so a chatty run costs several
// pages the first time a pane opens. Capped; add a server-side key filter
// if panes feel slow.
const HISTORY_MAX_PAGES = 50;
const isHistory = (t: string): boolean =>
  t.startsWith('agent.') || t === 'check.output';

/** Pages run.events once, when a node's detail pane opens. */
export async function fetchHistory(runId: string, key: string): Promise<void> {
  const keep = (events: RunEvent[]): RunEvent[] => {
    const seen = new Map<number, RunEvent>();
    for (const e of events) seen.set(e.seq, e);
    return [...seen.values()]
      .sort((a, b) => a.seq - b.seq)
      .slice(-HISTORY_KEEP);
  };
  setHistory({ runId, key, status: 'loading', events: [] });
  try {
    let after = 0;
    let mine: RunEvent[] = [];
    for (let i = 0; i < HISTORY_MAX_PAGES; i++) {
      const page = await request<{
        events: RunEvent[];
        last_seq: number;
        more: boolean;
      }>('run.events', { run_id: runId, after_seq: after, limit: 500 });
      const h = workflowsStore.getState().history;
      if (!h || h.runId !== runId || h.key !== key) return; // pane moved on
      mine = keep([
        ...mine,
        ...page.events.filter((e) => e.key === key && isHistory(e.type)),
      ]);
      if (!page.more) break;
      after = page.last_seq;
    }
    const h = workflowsStore.getState().history;
    if (!h || h.runId !== runId || h.key !== key) return;
    setHistory({ ...h, status: 'ready', events: keep([...mine, ...h.events]) });
  } catch {
    const h = workflowsStore.getState().history;
    if (h && h.runId === runId && h.key === key)
      setHistory({ ...h, status: 'error' });
  }
}

function appendHistory(runId: string, events: RunEvent[]): void {
  const h = workflowsStore.getState().history;
  if (!h || h.runId !== runId) return;
  const add = events.filter((e) => e.key === h.key && isHistory(e.type));
  if (add.length === 0) return;
  const seen = new Map<number, RunEvent>();
  for (const e of [...h.events, ...add]) seen.set(e.seq, e);
  setHistory({
    ...h,
    events: [...seen.values()]
      .sort((a, b) => a.seq - b.seq)
      .slice(-HISTORY_KEEP),
  });
}

/** Selects a node; the detail pane pages its history when it opens. */
export const selectNode = selectKey;

/** "Open session": closes the modal and switches to the node's session. */
export function openNodeSession(sessionId: string): void {
  closeWorkflows();
  deps.switchTo(sessionId);
}

// ---------- waiting tracker ----------

interface RunInfo {
  project_id: string;
  workflow: string;
}
const runInfo = new Map<string, RunInfo>();

function rememberRun(
  r: Pick<RunSummary, 'run_id' | 'project_id' | 'workflow'>,
) {
  runInfo.set(r.run_id, { project_id: r.project_id, workflow: r.workflow });
}

export const rememberRuns = (rs: RunSummary[]): void => rs.forEach(rememberRun);

async function ensureRunInfo(runId: string): Promise<RunInfo | undefined> {
  const known = runInfo.get(runId);
  if (known) return known;
  try {
    const g = await request<{ run: RunDetail }>('run.get', { run_id: runId });
    rememberRun(g.run);
  } catch {
    /* the banner just names nothing */
  }
  return runInfo.get(runId);
}

function publishWaiting(waiting: WaitingRun[]): void {
  setWaiting(waiting);
  if (waiting.length === 0) {
    hideBanner('workflow');
    return;
  }
  const w = waiting[0];
  const project = appStore
    .getState()
    .projects.find((p) => p.id === w.project_id);
  const where = project?.name ? ` in ${project.name}` : '';
  setBanner('workflow', {
    visible: true,
    actions: { open: { label: withKey('Open', shortcutLabel('workflows')) } },
    text:
      waiting.length === 1
        ? `Workflow "${showControls(w.workflow)}"${where} is waiting on you.`
        : `${waiting.length} workflows are waiting on you.`,
  });
}

/** One runs.list {state: 'waiting'} call: at boot (never notifies, so a
 * restart does not re-announce old waits), after a reconnect, and after a
 * tracked run's wait ends. */
export async function scanWaiting(): Promise<void> {
  if (!engineRunning()) return;
  try {
    const r = await request<{ runs: RunSummary[] }>('runs.list', {
      state: 'waiting',
      limit: 50,
    });
    rememberRuns(r.runs);
    publishWaiting(
      r.runs.map((x) => ({
        run_id: x.run_id,
        project_id: x.project_id,
        workflow: x.workflow,
      })),
    );
  } catch {
    /* best effort: live events keep the set current */
  }
}

let rescan: ReturnType<typeof setTimeout> | null = null;
function rescanSoon(): void {
  if (rescan) return;
  rescan = setTimeout(() => {
    rescan = null;
    void scanWaiting();
  }, 250);
}

function syncWaitingFrom(
  runId: string,
  ov: { instances: Record<string, { state: string }> },
) {
  // A run opened and found settled drops out of the banner set.
  const has = Object.values(ov.instances).some((i) => i.state === 'waiting');
  const cur = workflowsStore.getState().waiting;
  if (!has && cur.some((w) => w.run_id === runId)) rescanSoon();
}

function trackWaiting(runId: string, events: RunEvent[]): void {
  for (const ev of events) {
    if (ev.type === 'node.waiting') {
      void noteWait(runId, ev);
    } else if (
      ev.type === 'node.finished' ||
      ev.type === 'node.resumed' ||
      ev.type === 'run.finished'
    ) {
      if (workflowsStore.getState().waiting.some((w) => w.run_id === runId))
        rescanSoon();
    }
  }
}

async function noteWait(runId: string, ev: RunEvent): Promise<void> {
  const info = await ensureRunInfo(runId);
  if (info) {
    const cur = workflowsStore.getState().waiting;
    if (!cur.some((w) => w.run_id === runId)) {
      publishWaiting([
        ...cur,
        { run_id: runId, project_id: info.project_id, workflow: info.workflow },
      ]);
    }
  }
  // Only a human wait is announced here. A permission wait's notification
  // is the node session's own attention bell (events.ts syncAttentionClass).
  if ((ev.data as { reason?: string }).reason !== 'human') return;
  const project = appStore
    .getState()
    .projects.find((p) => p.id === info?.project_id);
  Notify(
    showControls(info?.workflow ?? 'Workflow'),
    project?.name ?? '',
    'Waiting for your answer — click to open.',
    `wf:${runId}:${ev.key ?? ''}`,
  ).catch(() => {
    /* best effort, like every notification: the banner still shows it */
  });
}

/** `bell-click` with a `wf:<run_id>:<key>` tag. Reports whether it was one. */
export function routeWorkflowClick(tag: string): boolean {
  if (!tag.startsWith('wf:')) return false;
  const rest = tag.slice(3);
  const i = rest.indexOf(':');
  const runId = i < 0 ? rest : rest.slice(0, i);
  const key = i < 0 ? '' : rest.slice(i + 1);
  void openWorkflowRun(runId, key || null);
  return true;
}

// ---------- plugin state / reconnect hooks (events.ts) ----------

let wasRunning = false;

/** After every plugin list/event: scan for waits when the engine comes up,
 * drop the banner when it goes. */
export function syncWorkflowsPlugin(): void {
  const running = engineRunning();
  if (running && !wasRunning) void scanWaiting();
  if (!running && wasRunning) publishWaiting([]);
  wasRunning = running;
}

/** After a control reconnect: the open run and the waiting set may have
 * missed events. */
export function resyncWorkflows(): void {
  if (!engineRunning()) return;
  const runId = workflowsStore.getState().run?.runId;
  if (runId && isModalOpen('workflows'))
    void loadRunState(runId, { reload: true });
  void scanWaiting();
}

// ---------- test affordances ----------

export function resetWorkflowsForTest(): void {
  for (const nonce of [...pending.keys()]) settle(nonce);
  loader = null;
  loadToken += 1;
  runInfo.clear();
  wasRunning = false;
  if (rescan) clearTimeout(rescan);
  rescan = null;
}

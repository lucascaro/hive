// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A run's overlay state, kept as pure functions so the page/event merge is
// testable without a transport.
//
// The engine materializes node state (`run.nodes`) and tags each page with
// the `last_seq` it reflects. `pageSeq[key]` is the highest seq already
// reflected for a key -- set by the page that supplied it, then advanced by
// each event applied -- so an event applies only when `seq > pageSeq[key]`.
// One rule makes a duplicate, or an event a page already reflects, a no-op.
// Run-level events are last-writer-wins on `runSeq`. `agent.*` and
// `check.output` are not overlay state; the node detail pane keeps them.

import { allNodes, indexUnder, nodeIdOfKey } from './workflow-graph.js';
import type {
  Conflict,
  IR,
  NodeRow,
  NodesPage,
  NodeState,
  RunEvent,
  RunState,
  Waiting,
} from './workflow-types.js';

export interface Instance {
  state: NodeState;
  attempt: number;
  cap?: string;
  error?: string;
  conflict?: Conflict;
  waiting?: Waiting;
}

export interface OverlayState {
  runId: string;
  runState: RunState | null;
  /** Seq of the last run-level event applied. */
  runSeq: number;
  instances: Record<string, Instance>;
  /** Latest round of each loop, by loop id. */
  loops: Record<string, { iteration: number; max_iters: number }>;
  /** Copies of each map, by map id. */
  maps: Record<string, { count: number; concurrency: number }>;
  /** Highest seq reflected for each key (page or applied event). */
  pageSeq: Record<string, number>;
  /** Highest seq seen, from a page or any event. */
  maxSeq: number;
}

export function emptyOverlay(
  runId: string,
  runState: RunState | null = null,
): OverlayState {
  return {
    runId,
    runState,
    runSeq: 0,
    instances: {},
    loops: {},
    maps: {},
    pageSeq: {},
    maxSeq: 0,
  };
}

const fromRow = (r: NodeRow): Instance => ({
  state: r.state,
  attempt: r.attempt,
  ...(r.cap ? { cap: r.cap } : {}),
});

/** Replaces the page's keys from its rows and records their `last_seq`. A
 * row keeps nothing of the instance it replaces: the page is a snapshot. */
export function applyPage(
  state: OverlayState,
  page: NodesPage,
  ir: IR,
): OverlayState {
  const instances = { ...state.instances };
  const pageSeq = { ...state.pageSeq };
  for (const row of page.nodes) {
    instances[row.key] = fromRow(row);
    pageSeq[row.key] = page.last_seq;
  }
  return derive(
    {
      ...state,
      instances,
      pageSeq,
      maxSeq: Math.max(state.maxSeq, page.last_seq),
    },
    ir,
  );
}

/** Whether a batch starts past the highest seq seen + 1: events are lost. */
export function hasGap(
  state: OverlayState,
  events: readonly RunEvent[],
): boolean {
  if (events.length === 0) return false;
  return Math.min(...events.map((e) => e.seq)) > state.maxSeq + 1;
}

const RUN_EVENTS = new Set(['run.started', 'run.finished', 'run.retried']);

const NODE_EVENTS = new Set([
  'node.ready',
  'node.started',
  'node.waiting',
  'node.resumed',
  'node.finished',
  'node.reset',
  'loop.iteration',
  'map.expanded',
]);

/** Folds events in seq order. Events of another run are ignored. */
export function fold(
  state: OverlayState,
  events: readonly RunEvent[],
  ir: IR,
): OverlayState {
  let s = state;
  let changed = false;
  for (const ev of [...events].sort((a, b) => a.seq - b.seq)) {
    if (ev.run_id !== s.runId) continue;
    s = applyEvent(s, ev);
    changed = true;
  }
  return changed ? derive(s, ir) : s;
}

function applyEvent(s: OverlayState, ev: RunEvent): OverlayState {
  const maxSeq = Math.max(s.maxSeq, ev.seq);
  if (RUN_EVENTS.has(ev.type)) {
    if (ev.seq <= s.runSeq) return { ...s, maxSeq };
    return { ...s, maxSeq, runSeq: ev.seq, runState: runStateAfter(ev) };
  }
  if (!NODE_EVENTS.has(ev.type)) return { ...s, maxSeq };
  const key = ev.key ?? ev.node_id;
  if (!key) return { ...s, maxSeq };
  if (ev.seq <= (s.pageSeq[key] ?? 0)) return { ...s, maxSeq };
  const pageSeq = { ...s.pageSeq, [key]: ev.seq };
  if (ev.type === 'loop.iteration') {
    const id = ev.node_id ?? nodeIdOfKey(key);
    const d = ev.data as { iteration?: number; max_iters?: number };
    return {
      ...s,
      maxSeq,
      pageSeq,
      loops: {
        ...s.loops,
        [id]: {
          iteration: Math.max(s.loops[id]?.iteration ?? 0, d.iteration ?? 0),
          max_iters: d.max_iters ?? s.loops[id]?.max_iters ?? 0,
        },
      },
    };
  }
  if (ev.type === 'map.expanded') {
    const id = ev.node_id ?? nodeIdOfKey(key);
    const d = ev.data as { count?: number; concurrency?: number };
    return {
      ...s,
      maxSeq,
      pageSeq,
      maps: {
        ...s.maps,
        [id]: {
          count: Math.max(s.maps[id]?.count ?? 0, d.count ?? 0),
          concurrency: d.concurrency ?? s.maps[id]?.concurrency ?? 0,
        },
      },
    };
  }
  return {
    ...s,
    maxSeq,
    pageSeq,
    instances: { ...s.instances, [key]: nodeAfter(s.instances[key], ev) },
  };
}

function runStateAfter(ev: RunEvent): RunState {
  if (ev.type === 'run.finished') {
    const st = (ev.data as { state?: RunState }).state;
    return st ?? 'done';
  }
  return 'running';
}

function nodeAfter(prev: Instance | undefined, ev: RunEvent): Instance {
  const base: Instance = prev ?? { state: 'pending', attempt: ev.attempt || 0 };
  // The wait, error and conflict belong to one attempt; every transition
  // below either sets them or clears them.
  const { waiting: _w, error: _e, conflict: _c, ...rest } = base;
  const d = ev.data as Record<string, unknown>;
  switch (ev.type) {
    case 'node.started':
      return { ...rest, state: 'running', attempt: ev.attempt || rest.attempt };
    case 'node.waiting':
      return {
        ...rest,
        state: 'waiting',
        attempt: ev.attempt || rest.attempt,
        waiting: d as unknown as Waiting,
      };
    case 'node.resumed':
      return { ...rest, state: 'running' };
    case 'node.finished':
      return {
        ...rest,
        state: (d.state as NodeState) ?? 'done',
        attempt: ev.attempt || rest.attempt,
        ...(typeof d.error === 'string' ? { error: d.error } : {}),
        ...(typeof d.cap === 'string' ? { cap: d.cap } : {}),
        ...(d.conflict ? { conflict: d.conflict as Conflict } : {}),
      };
    case 'node.reset':
      return { state: 'pending', attempt: 0 };
    default:
      // node.ready: the instance exists, still pending.
      return prev ?? { state: 'pending', attempt: ev.attempt || 0 };
  }
}

/** Loop rounds and map copies come from the keys themselves, bounded by the
 * IR; the events only ever raise them. */
function derive(s: OverlayState, ir: IR): OverlayState {
  const keys = Object.keys(s.instances);
  const loops = { ...s.loops };
  const maps = { ...s.maps };
  for (const n of allNodes(ir)) {
    if (n.kind !== 'loop' && n.kind !== 'map') continue;
    let max = -1;
    const seen = new Set<number>();
    for (const k of keys) {
      const i = indexUnder(k, n.id);
      if (i === null) continue;
      seen.add(i);
      max = Math.max(max, i);
    }
    if (n.kind === 'loop') {
      const iteration = Math.max(
        max < 0 ? 0 : max,
        loops[n.id]?.iteration ?? 0,
      );
      if (iteration > 0 || loops[n.id])
        loops[n.id] = { iteration, max_iters: n.max_iters };
    } else {
      const count = Math.max(seen.size, maps[n.id]?.count ?? 0);
      if (count > 0 || maps[n.id])
        maps[n.id] = { count, concurrency: n.concurrency };
    }
  }
  return { ...s, loops, maps };
}

// ---------- selectors ----------

const AGGREGATE_ORDER: readonly NodeState[] = [
  'waiting',
  'running',
  'failed',
  'interrupted',
  'pending',
  'cancelled',
  'skipped',
  'done',
];

/** The instances of an IR node: one normally, one per round or copy under
 * a loop or map. */
export function instancesOf(
  s: OverlayState,
  nodeId: string,
): [string, Instance][] {
  return Object.entries(s.instances).filter(([k]) => nodeIdOfKey(k) === nodeId);
}

/** Attention wins: waiting, running, failed ... done. A node with no
 * instance yet is pending. */
export function aggregate(s: OverlayState, nodeId: string): NodeState {
  const states = instancesOf(s, nodeId).map(([, i]) => i.state);
  if (states.length === 0) return 'pending';
  for (const st of AGGREGATE_ORDER) if (states.includes(st)) return st;
  return 'pending';
}

/** "k of n done" for a map: a copy is done when every instance under it is
 * done or skipped. */
export function mapProgress(
  s: OverlayState,
  mapId: string,
): { done: number; count: number } {
  const copies = new Map<number, NodeState[]>();
  for (const [k, inst] of Object.entries(s.instances)) {
    const i = indexUnder(k, mapId);
    if (i === null) continue;
    copies.set(i, [...(copies.get(i) ?? []), inst.state]);
  }
  let done = 0;
  for (const states of copies.values()) {
    if (states.every((st) => st === 'done' || st === 'skipped')) done += 1;
  }
  return { done, count: Math.max(copies.size, s.maps[mapId]?.count ?? 0) };
}

/** The run's state as the user reads it: a running run with a waiting node
 * is waiting. */
export function displayRunState(s: OverlayState): RunState | null {
  if (s.runState === 'running') {
    if (Object.values(s.instances).some((i) => i.state === 'waiting'))
      return 'waiting';
  }
  return s.runState;
}

export function waitingKeys(s: OverlayState): string[] {
  return Object.entries(s.instances)
    .filter(([, i]) => i.state === 'waiting')
    .map(([k]) => k);
}

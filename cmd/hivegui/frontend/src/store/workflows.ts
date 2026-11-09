// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// ---------- workflows store (spec 530) ----------
//
// The Workflows modal's view state, the open run's overlay, and the set of
// runs waiting on the user. Its own store, like store/acp.ts: run events
// stream per node and nothing outside the modal reads them. Whether the
// modal is open is appStore's (`modals`), as for every modal.
//
// Form drafts live here, not in a component, so closing the modal (the
// toggle chord does) never discards typed input.

import { useStore } from 'zustand';
import { createStore } from 'zustand/vanilla';

import type { Raw } from '../lib/schema-form.js';
import { type OverlayState, emptyOverlay } from '../lib/workflow-overlay.js';
import type { IR, RunDetail, RunEvent } from '../lib/workflow-types.js';

export type WorkflowsView = 'list' | 'trust' | 'review' | 'run';

export interface RunView {
  runId: string;
  status: 'loading' | 'ready' | 'error';
  error: string | null;
  detail: RunDetail | null;
  ir: IR | null;
  overlay: OverlayState;
}

/** A run waiting on the user, at run level (what `runs.list` knows). */
export interface WaitingRun {
  run_id: string;
  project_id: string;
  workflow: string;
}

/** One node's `agent.*` / `check.output` history for the detail pane. */
export interface NodeHistory {
  runId: string;
  key: string;
  status: 'loading' | 'ready' | 'error';
  events: RunEvent[];
}

export type Drafts = Record<string, { [name: string]: Raw }>;

interface WorkflowsData {
  view: WorkflowsView;
  file: string | null;
  runId: string | null;
  selectedKey: string | null;
  drafts: Drafts;
  run: RunView | null;
  history: NodeHistory | null;
  waiting: WaitingRun[];
}

const initial = (): WorkflowsData => ({
  view: 'list',
  file: null,
  runId: null,
  selectedKey: null,
  drafts: {},
  run: null,
  history: null,
  waiting: [],
});

export const workflowsStore = createStore<WorkflowsData>()(initial);
const set = workflowsStore.setState;
const get = workflowsStore.getState;

export const useWorkflows = <T>(sel: (s: WorkflowsData) => T): T =>
  useStore(workflowsStore, sel);

/** Test affordance: back to a fresh store. */
export function resetWorkflowsStore(): void {
  set(initial(), true);
}

export function setView(
  view: WorkflowsView,
  extra: Partial<Pick<WorkflowsData, 'file' | 'runId' | 'selectedKey'>> = {},
): void {
  set({ view, ...extra });
}

export function selectKey(key: string | null): void {
  set({ selectedKey: key, history: null });
}

export function setDraft(id: string, value: { [name: string]: Raw }): void {
  set({ drafts: { ...get().drafts, [id]: value } });
}

export function dropDraft(id: string): void {
  const { [id]: _gone, ...rest } = get().drafts;
  set({ drafts: rest });
}

export const startDraftId = (
  projectId: string,
  file: string,
  sourceHash: string,
): string => `start\u0000${projectId}\u0000${file}\u0000${sourceHash}`;

export const answerDraftId = (runId: string, key: string): string =>
  `answer\u0000${runId}\u0000${key}`;

export function beginRun(runId: string): void {
  set({
    run: {
      runId,
      status: 'loading',
      error: null,
      detail: null,
      ir: null,
      overlay: emptyOverlay(runId),
    },
  });
}

export function patchRun(patch: Partial<RunView>): void {
  const run = get().run;
  if (!run) return;
  set({ run: { ...run, ...patch } });
}

export function clearRun(): void {
  set({ run: null, history: null });
}

export function setHistory(h: NodeHistory | null): void {
  set({ history: h });
}

export function setWaiting(waiting: WaitingRun[]): void {
  set({ waiting });
}

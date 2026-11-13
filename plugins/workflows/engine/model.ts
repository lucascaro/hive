// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The shapes the store persists and the reducer reads (spec 529, Store).

import type { EventDraft } from './events.ts';

export type NodeState = 'pending' | 'running' | 'waiting' | 'done' | 'failed' | 'skipped' | 'interrupted' | 'cancelled';
export type RunState = 'running' | 'waiting' | 'done' | 'failed' | 'cancelled';

/** One node instance: the `nodes` table row (spec 529, "One row per node instance"). */
export interface Row {
  key: string;
  node_id: string;
  state: NodeState;
  /** 0 until the first start; a retry after `interrupted` starts attempt + 1. */
  attempt: number;
  /** The node became ready and was announced; kept so a throttled node is announced once. */
  ready: boolean;
  /** A loop's current iteration, a map's copy count. Survives run.retry. */
  progress: number;
  /** Agent turns this node has used, against the run's cap. */
  turns: number;
  /** The raw output. Only the reducer reads it (B-5): conditions, prompts, map.over. */
  output_raw?: unknown;
  /** The redacted output: what node.get pages and node.output events carry. Written with output_raw, from one pass. */
  output?: unknown;
  error?: string;
  cap?: string;
  session_id?: string;
  branch?: string;
  worktree?: string;
  pid?: number;
  pgid?: number;
  proc_start?: string;
  prompt_id?: number;
  epoch?: number;
  boot_id?: string;
  conflict?: { branch: string; files: string[]; worktree: string; files_omitted?: number };
  /** The data of the open `node.waiting` event; present only while the node is waiting (B-13). */
  waiting?: Record<string, unknown>;
  started_at?: string;
  ended_at?: string;
}

export interface RunRow {
  run_id: string;
  project_id: string;
  workflow: string;
  file: string;
  state: RunState;
  source_hash: string;
  /** The IR JSON, as stored. */
  ir: string;
  /** The run input JSON, as stored. */
  input: string;
  base_sha: string;
  nonce: string;
  boot_id: string;
  kept_worktrees: string[];
  error?: string;
  /** The seq of the last event this row reflects (B-14). */
  last_seq: number;
  created_at: string;
  updated_at: string;
}

/** What one transition writes, atomically: node rows, the run row, and the events that describe them. */
export interface Change {
  rows: Row[];
  events: EventDraft[];
  run?: { state?: RunState; error?: string | null; base_sha?: string; kept_worktrees?: string[] };
}

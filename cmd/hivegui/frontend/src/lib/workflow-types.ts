// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The workflows plugin's wire contract as the GUI reads it: reply shapes
// and run events (docs/exec-plans/active/529-*.md, "Contract for C"), plus
// the IR types. The IR is imported from the plugin's own source rather
// than copied: type and pure-function source only, so there is no runtime
// coupling and nothing to regenerate (DESIGN.md, GUI bullet). The `.ts`
// specifiers need tsconfig's allowImportingTsExtensions.

import type {
  Body,
  Cond,
  IR,
  Node,
} from '../../../../../plugins/workflows/sdk/dsl.ts';
import type { JsonSchema } from '../../../../../plugins/workflows/sdk/schema.ts';

export type { Body, Cond, IR, JsonSchema, Node };
export { condLabel } from '../../../../../plugins/workflows/ir/mermaid.ts';

export const WORKFLOWS_PLUGIN_ID = 'workflows';

export type NodeState =
  | 'pending'
  | 'running'
  | 'waiting'
  | 'done'
  | 'failed'
  | 'skipped'
  | 'interrupted'
  | 'cancelled';

export type RunState = 'running' | 'waiting' | 'done' | 'failed' | 'cancelled';

export type TrustState = 'none' | 'trusted' | 'changed' | 'invalid';

export interface WorkflowsList {
  folder: string;
  trust: TrustState;
  invalid_reason?: string;
  workflows: { file: string; name?: string }[];
}

export interface TrustDescribe {
  folder: string;
  hash: string;
  files: { path: string; bytes: number }[];
}

export interface BuildReply {
  name: string;
  ir: IR;
  source_hash: string;
  input_schema: JsonSchema;
}

export interface TypeErrorRow {
  file: string;
  line: number;
  col: number;
  message: string;
}

export interface RunSummary {
  run_id: string;
  project_id: string;
  workflow: string;
  file: string;
  state: RunState;
  created_at: string;
  updated_at: string;
  source_hash: string;
  source_changed: boolean;
}

export interface RunDetail extends RunSummary {
  input?: unknown;
  base_sha?: string;
  kept_worktrees?: string[];
  kept_worktrees_total?: number;
  error?: string;
}

/** A slim `run.nodes` row. Outputs, errors and wait payloads come from
 * `node.get`. */
export interface NodeRow {
  key: string;
  node_id: string;
  state: NodeState;
  attempt: number;
  started_at?: string;
  finished_at?: string;
  cap?: string;
}

export interface NodesPage {
  nodes: NodeRow[];
  last_seq: number;
  more?: boolean;
  next_after?: string;
}

export interface PermissionOption {
  option_id: string;
  name: string;
  kind: string;
}

/** The data of an open `node.waiting` event. */
export type Waiting =
  | { reason: 'human'; prompt: string; output_schema: JsonSchema }
  | {
      reason: 'permission';
      session_id: string;
      request_id: string;
      title: string;
      options: PermissionOption[];
    };

export interface Conflict {
  branch: string;
  files: string[];
  worktree: string;
}

/** `node.get` (Q-B3, B-13): the slices and facts the slim `run.nodes` rows
 * leave out. The one place the GUI reads `waiting`, `session_id`, `branch`,
 * `worktree`, `error` and `conflict` for a node it did not watch happen. */
export interface NodeGet {
  key: string;
  node_id: string;
  state: NodeState;
  attempt: number;
  output?: string;
  total_bytes?: number;
  next_offset?: number;
  more?: boolean;
  session_id?: string;
  branch?: string;
  worktree?: string;
  error?: string;
  conflict?: Conflict;
  waiting?: Waiting;
}

export interface RunEvent {
  run_id: string;
  seq: number;
  ts: string;
  key: string | null;
  node_id: string | null;
  attempt: number;
  type: string;
  data: Record<string, unknown>;
}

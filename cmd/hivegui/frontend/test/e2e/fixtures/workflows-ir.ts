// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// IR fixtures for the Workflows specs: the plugin's own worked examples
// (so the GUI cannot drift from B's builder) and one small hand-built
// workflow with a human gate.

import type { IR, Node } from '../../../src/lib/workflow-types.js';
import type { MockNode } from '../workflows-engine.js';

const EMPTY = {
  type: 'object',
  required: [],
  additionalProperties: false,
  properties: {},
} as const;

export const VERDICT = {
  type: 'object',
  required: ['verdict'],
  additionalProperties: false,
  properties: {
    verdict: { type: 'string', enum: ['approve', 'reject'] },
    note: { type: 'string' },
  },
} as const;

/** work (agent) -> approve (human) -> check, the check only on approval. */
export const shipIr = (): IR =>
  ({
    ir: 1,
    name: 'ship',
    input: {
      type: 'object',
      required: ['task'],
      additionalProperties: false,
      properties: { task: { type: 'string' }, strict: { type: 'boolean' } },
    },
    nodes: [
      {
        id: 'work',
        kind: 'agent',
        agent: 'claude',
        worktree: 'own',
        timeout_s: 60,
        prompt: 'Do {{input.task}}',
        output: EMPTY,
      },
      { id: 'approve', kind: 'human', prompt: 'Ship it?', output: VERDICT },
      { id: 'check', kind: 'check', argv: ['true'], timeout_s: 5 },
    ],
    edges: [
      { from: 'work', to: 'approve' },
      {
        from: 'approve',
        to: 'check',
        when: { ref: 'approve.verdict', op: 'eq', value: 'approve' },
      },
    ],
  }) as unknown as IR;

const STATES = ['done', 'running', 'waiting', 'pending', 'failed'] as const;

/** One materialized row per leaf, two rounds under a loop and three copies
 * under a map, with a spread of states so every mark renders. */
export function rowsFor(ir: IR): MockNode[] {
  const out: MockNode[] = [];
  let n = 0;
  const walk = (nodes: Node[], prefix: string) => {
    for (const node of nodes) {
      const local = node.id.slice(node.id.lastIndexOf('/') + 1);
      const key = prefix ? `${prefix}/${local}` : local;
      if (node.kind === 'group') walk(node.nodes, key);
      else if (node.kind === 'loop')
        for (const k of [1, 2]) walk(node.nodes, `${key}[${k}]`);
      else if (node.kind === 'map')
        for (const i of [0, 1, 2]) walk(node.nodes, `${key}[${i}]`);
      else {
        out.push({
          key,
          node_id: node.id,
          state: STATES[n++ % STATES.length],
          attempt: 1,
        });
      }
    }
  };
  walk(ir.nodes, '');
  return out;
}

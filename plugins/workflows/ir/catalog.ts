// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Checks an IR's agent nodes against the daemon's ACP catalog (spec 529).
// The catalog is the ACP_AGENTS reply to LIST_ACP_AGENTS (spec 528): it
// already carries the user's ceilings and availability, so a plugin never
// reads agent settings itself. The daemon refuses the same cases at CREATE;
// this surfaces them before a run starts.

import type { AgentNode, IR, Node } from '../sdk/dsl.ts';

/** One entry of ACP_AGENTS.agents (internal/wire/request.go AcpAgentInfo). */
export interface AcpAgentInfo {
  id: string;
  name: string;
  /** Least permissive first. */
  modes: { id: string; label: string }[];
  default_mode?: string;
  /** The most permissive mode the user allows. */
  ceiling?: string;
  no_approval_gate?: boolean;
  experimental?: boolean;
  available: boolean;
  reason?: string;
}

export function* agentNodes(nodes: Node[]): Generator<AgentNode> {
  for (const n of nodes) {
    if (n.kind === 'agent') yield n;
    else if ('nodes' in n) yield* agentNodes(n.nodes);
  }
}

/** Why one agent node cannot run under `list`, or undefined when it can. */
export function refusal(node: AgentNode, list: readonly AcpAgentInfo[]): string | undefined {
  const info = list.find((a) => a.id === node.agent);
  if (!info) return `${node.id}: agent "${node.agent}" has no ACP route`;
  if (info.experimental) return `${node.id}: agent "${node.agent}" is experimental over ACP and not allowed in workflows`;
  if (!info.available) return `${node.id}: agent "${node.agent}" is unavailable${info.reason ? `: ${info.reason}` : ''}`;
  // An omitted mode is sent as the adapter's least permissive one (never "",
  // which the daemon reads as the ceiling), so only a named mode can fail.
  if (node.mode === undefined) return undefined;
  const rank = info.modes.findIndex((m) => m.id === node.mode);
  // The daemon always fills ceiling; fall back as it does if a reply omits it.
  const ceiling = info.ceiling || info.default_mode || info.modes[0]?.id;
  if (rank < 0) return `${node.id}: mode "${node.mode}" is not one of ${info.modes.map((m) => m.id).join('|')} for agent "${node.agent}"`;
  if (rank > info.modes.findIndex((m) => m.id === ceiling)) return `${node.id}: mode "${node.mode}" is above the "${ceiling}" ceiling set for agent "${node.agent}"`;
  return undefined;
}

/** Every reason an agent node cannot run under `catalog`; empty means all can. */
export function catalog(ir: IR, list: readonly AcpAgentInfo[]): string[] {
  const errors: string[] = [];
  for (const node of agentNodes(ir.nodes)) {
    const why = refusal(node, list);
    if (why) errors.push(why);
  }
  return errors;
}

// IR → Mermaid flowchart (spec 495). Groups, loops and maps render as
// labelled subgraphs, so the picture a user reviews has the same nesting the
// engine runs; edge labels show their conditions. Output is deterministic.

import type { Body, Cond, IR, Node } from './dsl.ts';

// Ids are lowercase words joined by single hyphens, nested with `/` (see
// dsl.ts), so this mapping cannot collide.
// A Mermaid keyword as a bare id (a top-level node called `end`) would break
// the chart; ids never end in `_`, so the suffix cannot collide either.
const KEYWORDS = new Set(['end', 'subgraph', 'graph', 'flowchart', 'direction', 'style', 'class', 'classDef', 'click', 'linkStyle']);
const mid = (id: string): string => {
  const m = id.replaceAll('/', '__').replaceAll('-', '_');
  return KEYWORDS.has(m) ? `${m}_` : m;
};
const local = (id: string): string => id.slice(id.lastIndexOf('/') + 1);
const esc = (s: string): string => s.replace(/"/g, '#quot;');

// `build/fix/review.verdict` → `review.verdict`: the node's own segment.
const shortRef = (ref: string): string => {
  const dot = ref.lastIndexOf('.');
  return dot < 0 ? ref : `${local(ref.slice(0, dot))}${ref.slice(dot)}`;
};

const show = (v: unknown): string => (typeof v === 'string' ? v : JSON.stringify(v));

export function condLabel(c: Cond): string {
  if ('all' in c) return c.all.map(condLabel).join(' and ');
  if ('any' in c) return c.any.map(condLabel).join(' or ');
  if ('not' in c) return `not (${condLabel(c.not)})`;
  const r = shortRef(c.ref);
  switch (c.op) {
    case 'eq':
      return `${r} = ${show(c.value)}`;
    case 'ne':
      return `${r} ≠ ${show(c.value)}`;
    case 'in':
      return `${r} ∈ {${(c.value as unknown[]).map(show).join(', ')}}`;
    case 'truthy':
      return r;
    case 'lt':
      return `${r} < ${show(c.value)}`;
    case 'gt':
      return `${r} > ${show(c.value)}`;
  }
}

function nodeLines(n: Node, ind: string, out: string[]): void {
  const name = local(n.id);
  switch (n.kind) {
    case 'agent':
      out.push(`${ind}${mid(n.id)}["${esc(`${name} · ${n.agent}`)}"]`);
      return;
    case 'check':
      out.push(`${ind}${mid(n.id)}[["${esc(`${name} · check`)}"]]`);
      return;
    case 'human':
      out.push(`${ind}${mid(n.id)}(["${esc(`${name} · human`)}"])`);
      return;
    case 'group':
      out.push(`${ind}subgraph ${mid(n.id)}["${esc(name)}"]`);
      break;
    case 'loop':
      out.push(`${ind}subgraph ${mid(n.id)}["${esc(`${name} · loop ≤${n.max_iters}× until ${condLabel(n.until)}`)}"]`);
      break;
    case 'map':
      out.push(`${ind}subgraph ${mid(n.id)}["${esc(`${name} · for each ${shortRef(n.over)}, ≤${n.concurrency} at once`)}"]`);
      break;
  }
  bodyLines(n, `${ind}  `, out);
  out.push(`${ind}end`);
}

function bodyLines(b: Body, ind: string, out: string[]): void {
  for (const n of b.nodes) nodeLines(n, ind, out);
  for (const e of b.edges) {
    const arrow = e.when ? ` -->|"${esc(condLabel(e.when))}"| ` : ' --> ';
    out.push(`${ind}${mid(e.from)}${arrow}${mid(e.to)}`);
  }
}

export function toMermaid(ir: IR): string {
  const out = ['flowchart TD'];
  bodyLines(ir, '  ', out);
  return `${out.join('\n')}\n`;
}

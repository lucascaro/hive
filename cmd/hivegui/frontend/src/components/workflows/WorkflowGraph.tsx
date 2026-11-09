// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The workflow graph, rendered natively (exec plan 530: not Mermaid). Each
// body is a ranked grid (lib/workflow-graph.ts); containers nest. Nodes are
// buttons carrying their state in words; the run overlay (lib/workflow-
// overlay.ts) colours them. Every label is IR text and renders as React
// text, never markup.

import {
  type ReactNode,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import {
  doneLabel,
  groupHeader,
  layoutBody,
  localId,
  loopHeader,
  mapHeader,
  roundLabel,
} from '../../lib/workflow-graph.js';
import {
  type OverlayState,
  aggregate,
  instancesOf,
  mapProgress,
} from '../../lib/workflow-overlay.js';
import type { Body, IR, Node } from '../../lib/workflow-types.js';
import { plainText } from '../../lib/workflow-text.js';
import { StateMark, stateWord } from './StateMark.js';

interface Ctx {
  overlay: OverlayState | null;
  selectedKey: string | null;
  onSelect: (key: string) => void;
}

/** The key a click on a node selects: the instance that explains its
 * aggregate state (the waiting one, the latest round), else the IR id. */
export function representativeKey(
  overlay: OverlayState | null,
  nodeId: string,
): string {
  if (!overlay) return nodeId;
  const inst = instancesOf(overlay, nodeId);
  if (inst.length === 0) return nodeId;
  const agg = aggregate(overlay, nodeId);
  const match = inst.filter(([, i]) => i.state === agg);
  const pick = (match.length ? match : inst).map(([k]) => k).sort(byKey);
  return pick[pick.length - 1];
}

// [2] before [10]: compare the numbers inside keys numerically.
const byKey = (a: string, b: string): number =>
  a.localeCompare(b, undefined, { numeric: true });

export function WorkflowGraph({
  ir,
  overlay = null,
  selectedKey = null,
  onSelect = () => {},
}: {
  ir: IR;
  overlay?: OverlayState | null;
  selectedKey?: string | null;
  onSelect?: (key: string) => void;
}): ReactNode {
  return (
    <div className="hv-wf-graph" data-testid="wf-graph">
      <BodyView
        body={ir}
        ctx={{ overlay, selectedKey, onSelect }}
        inMap={false}
      />
    </div>
  );
}

interface Edge {
  d: string;
  label?: string;
  x: number;
  y: number;
}

function BodyView({
  body,
  ctx,
  inMap,
}: {
  body: Body;
  ctx: Ctx;
  inMap: boolean;
}): ReactNode {
  const layout = useMemo(() => layoutBody(body), [body]);
  const ref = useRef<HTMLDivElement>(null);
  const [edges, setEdges] = useState<Edge[]>([]);

  // Edge paths come from the laid-out boxes. Recomputed after every render
  // and on resize; the state only changes when a path does.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => {
      const base = el.getBoundingClientRect();
      const find = (id: string) =>
        [...el.querySelectorAll<HTMLElement>('[data-node-id]')].find(
          (n) => n.dataset.nodeId === id,
        );
      const next: Edge[] = [];
      for (const e of layout.edges) {
        const a = find(e.from)?.getBoundingClientRect();
        const b = find(e.to)?.getBoundingClientRect();
        if (!a || !b) continue;
        const x1 = a.left + a.width / 2 - base.left;
        const y1 = a.bottom - base.top;
        const x2 = b.left + b.width / 2 - base.left;
        const y2 = b.top - base.top;
        const ym = (y1 + y2) / 2;
        next.push({
          d: `M${x1} ${y1} C${x1} ${ym} ${x2} ${ym} ${x2} ${y2}`,
          label: e.label,
          x: (x1 + x2) / 2,
          y: ym,
        });
      }
      setEdges((cur) =>
        JSON.stringify(cur) === JSON.stringify(next) ? cur : next,
      );
    };
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  });

  return (
    <div className="hv-wf-body" ref={ref}>
      <svg className="hv-wf-edges" aria-hidden="true" focusable="false">
        <defs>
          <marker
            id="hv-wf-arrow"
            viewBox="0 0 8 8"
            refX="7"
            refY="4"
            markerWidth="7"
            markerHeight="7"
            orient="auto"
          >
            <path d="M0 0L8 4L0 8z" fill="currentColor" />
          </marker>
        </defs>
        {edges.map((e) => (
          <path key={e.d} d={e.d} markerEnd="url(#hv-wf-arrow)" />
        ))}
      </svg>
      {layout.rows.map((row, r) => (
        <div className="hv-wf-rank" key={row.map((n) => n.id).join('|') || r}>
          {row.map((n) => (
            <NodeView key={n.id} node={n} ctx={ctx} inMap={inMap} />
          ))}
        </div>
      ))}
      {edges.map(
        (e) =>
          e.label && (
            <span
              key={`l${e.d}`}
              className="hv-wf-edge-label"
              style={{ left: e.x, top: e.y }}
            >
              {plainText(e.label)}
            </span>
          ),
      )}
    </div>
  );
}

function NodeView({
  node: n,
  ctx,
  inMap,
}: {
  node: Node;
  ctx: Ctx;
  inMap: boolean;
}): ReactNode {
  if (n.kind === 'group' || n.kind === 'loop' || n.kind === 'map') {
    return <Container node={n} ctx={ctx} inMap={inMap} />;
  }
  return <Leaf node={n} ctx={ctx} inMap={inMap} />;
}

function Leaf({
  node: n,
  ctx,
  inMap,
}: {
  node: Exclude<Node, { kind: 'group' | 'loop' | 'map' }>;
  ctx: Ctx;
  inMap: boolean;
}): ReactNode {
  const { overlay } = ctx;
  const agg = overlay ? aggregate(overlay, n.id) : null;
  const insts = overlay ? instancesOf(overlay, n.id) : [];
  const key = representativeKey(overlay, n.id);
  const inst = overlay?.instances[key];
  const kind = n.kind === 'agent' ? n.agent : n.kind;
  const selected =
    ctx.selectedKey !== null &&
    (ctx.selectedKey === n.id || insts.some(([k]) => k === ctx.selectedKey));
  const permission = inst?.waiting?.reason === 'permission';
  const [open, setOpen] = useState(false);
  return (
    <div className="hv-wf-cell">
      <button
        type="button"
        className="hv-wf-node"
        data-node-id={n.id}
        data-kind={n.kind}
        data-state={agg ?? undefined}
        aria-pressed={selected}
        aria-label={
          agg
            ? `${plainText(localId(n.id))}, ${n.kind === 'agent' ? `agent ${plainText(n.agent)}` : n.kind}, ${stateWord(agg, permission)}, attempt ${inst?.attempt ?? 0}`
            : `${plainText(localId(n.id))}, ${n.kind === 'agent' ? `agent ${plainText(n.agent)}` : n.kind}`
        }
        onClick={() => ctx.onSelect(key)}
      >
        <span className="hv-wf-node__id">{plainText(localId(n.id))}</span>
        <span className="hv-wf-node__kind">{plainText(kind)}</span>
        {agg ? <StateMark state={agg} permission={permission} /> : null}
      </button>
      {inMap && insts.length > 0 ? (
        <div className="hv-wf-copies">
          <button
            type="button"
            className="hv-wf-copies__toggle"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
          >
            {open ? 'Hide' : 'Show'} {insts.length}{' '}
            {insts.length === 1 ? 'copy' : 'copies'}
          </button>
          {open ? (
            <ul className="hv-wf-copies__list">
              {insts
                .map(([k, i]) => [k, i] as const)
                .sort(([a], [b]) => byKey(a, b))
                .map(([k, i]) => (
                  <li key={k}>
                    <button
                      type="button"
                      className="hv-wf-copy"
                      data-key={k}
                      aria-pressed={ctx.selectedKey === k}
                      onClick={() => ctx.onSelect(k)}
                    >
                      <span className="hv-wf-copy__key">{plainText(k)}</span>
                      <StateMark
                        state={i.state}
                        permission={i.waiting?.reason === 'permission'}
                      />
                    </button>
                  </li>
                ))}
            </ul>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function Container({
  node: n,
  ctx,
  inMap,
}: {
  node: Extract<Node, { kind: 'group' | 'loop' | 'map' }>;
  ctx: Ctx;
  inMap: boolean;
}): ReactNode {
  const { overlay } = ctx;
  let header: string;
  let live: string | null = null;
  if (n.kind === 'loop') {
    header = loopHeader(n);
    const r = overlay?.loops[n.id];
    if (r && r.iteration > 0) live = roundLabel(r.iteration, n.max_iters);
  } else if (n.kind === 'map') {
    header = mapHeader(n);
    if (overlay) {
      const p = mapProgress(overlay, n.id);
      if (p.count > 0) live = doneLabel(p.done, p.count);
    }
  } else {
    header = groupHeader(n);
  }
  const agg =
    overlay && instancesOf(overlay, n.id).length > 0
      ? aggregate(overlay, n.id)
      : null;
  return (
    <section
      className="hv-wf-sub"
      data-node-id={n.id}
      data-kind={n.kind}
      data-state={agg ?? undefined}
      aria-label={`${plainText(localId(n.id))}, ${n.kind}${agg ? `, ${stateWord(agg)}` : ''}`}
    >
      <header className="hv-wf-sub__head">
        <span className="hv-wf-sub__title">{plainText(header)}</span>
        {live ? <span className="hv-wf-sub__live">{live}</span> : null}
        {agg ? <StateMark state={agg} /> : null}
      </header>
      <BodyView body={n} ctx={ctx} inMap={inMap || n.kind === 'map'} />
    </section>
  );
}

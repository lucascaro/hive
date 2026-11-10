// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// WorkflowGraph structure (src/components/workflows/WorkflowGraph.tsx).
// Geometry is Playwright's job (test/e2e/workflows.spec.ts): jsdom does no
// layout, so this pins what the DOM says, not where it sits.
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { examples } from '../../../../../plugins/workflows/tools/examples.ts';
import { WorkflowGraph } from '../../src/components/workflows/WorkflowGraph.js';
import { allNodes } from '../../src/lib/workflow-graph.js';
import {
  applyPage,
  emptyOverlay,
  fold,
} from '../../src/lib/workflow-overlay.js';
import type { RunEvent } from '../../src/lib/workflow-types.js';

afterEach(cleanup);

describe('WorkflowGraph', () => {
  it('every IR node renders once with data-node-id and an aria-label naming its state', () => {
    for (const make of Object.values(examples)) {
      const ir = make();
      const { container, unmount } = render(
        <WorkflowGraph ir={ir} overlay={emptyOverlay('r', 'running')} />,
      );
      const ids = allNodes(ir).map((n) => n.id);
      const found = [...container.querySelectorAll('[data-node-id]')].map(
        (e) => (e as HTMLElement).dataset.nodeId,
      );
      expect(found.sort()).toEqual([...ids].sort());
      for (const e of container.querySelectorAll('[data-node-id]')) {
        expect(e.getAttribute('aria-label'), e.outerHTML.slice(0, 80)).toMatch(
          /\S/,
        );
      }
      // A node with an overlay names its state and attempt.
      for (const b of container.querySelectorAll('button[data-node-id]')) {
        expect(b.getAttribute('aria-label')).toMatch(/, pending, attempt 0$/);
      }
      unmount();
    }
  });

  it('a static graph (no overlay) names kind only and still renders every node', () => {
    const ir = examples.reviewLoop();
    const { container } = render(<WorkflowGraph ir={ir} />);
    expect(container.querySelectorAll('[data-node-id]').length).toBe(
      allNodes(ir).length,
    );
    expect(container.querySelector('.hv-wf-mark')).toBeNull();
  });

  it('IR text renders as text: a label with markup stays literal', () => {
    const ir = structuredClone(examples.reviewLoop());
    ir.nodes[0].id = 'x<img src=x onerror=alert(1)>';
    const { container } = render(<WorkflowGraph ir={ir} />);
    expect(container.querySelector('img')).toBeNull();
  });

  const ev = (
    seq: number,
    type: string,
    key: string,
    data: object = {},
    attempt = 1,
  ): RunEvent => ({
    run_id: 'r',
    seq,
    ts: '',
    key,
    node_id: key.replace(/\[\d+\]/g, ''),
    attempt,
    type,
    data: data as Record<string, unknown>,
  });

  it('map template expands to one row per key', () => {
    const ir = examples.fanOutThenVerify();
    const select = vi.fn();
    let ov = emptyOverlay('r', 'running');
    ov = fold(
      ov,
      [
        ev(1, 'map.expanded', 'work/each', { count: 3, concurrency: 4 }),
        ev(2, 'node.finished', 'work/each[0]/do', { state: 'done' }),
        ev(3, 'node.started', 'work/each[1]/do'),
        ev(4, 'node.waiting', 'work/each[2]/do', {
          reason: 'permission',
          session_id: 's',
          request_id: 'q',
          title: 't',
          options: [],
        }),
      ],
      ir,
    );
    const { container } = render(
      <WorkflowGraph ir={ir} overlay={ov} onSelect={select} />,
    );
    // The header carries the live progress.
    expect(container.textContent).toContain('1 of 3 done');
    // Collapsed, then expanded to one row per key.
    expect(container.querySelectorAll('.hv-wf-copy')).toHaveLength(0);
    fireEvent.click(screen.getByRole('button', { name: /Show 3 copies/ }));
    const rows = [...container.querySelectorAll<HTMLElement>('.hv-wf-copy')];
    expect(rows.map((r) => r.dataset.key)).toEqual([
      'work/each[0]/do',
      'work/each[1]/do',
      'work/each[2]/do',
    ]);
    expect(rows[0].textContent).toContain('done');
    expect(rows[2].textContent).toContain('waiting for permission');
    // Selecting a row selects that key.
    fireEvent.click(rows[1]);
    expect(select).toHaveBeenCalledWith('work/each[1]/do');
    // The template node's aggregate: attention wins.
    const tmpl = container.querySelector('button[data-node-id="work/each/do"]');
    expect(tmpl?.getAttribute('data-state')).toBe('waiting');
  });

  it('loop header shows round k of N', () => {
    const ir = examples.reviewLoop();
    let ov = emptyOverlay('r', 'running');
    ov = applyPage(
      ov,
      {
        nodes: [
          {
            key: 'fix[1]/work',
            node_id: 'fix/work',
            state: 'done',
            attempt: 1,
          },
          {
            key: 'fix[2]/work',
            node_id: 'fix/work',
            state: 'running',
            attempt: 1,
          },
        ],
        last_seq: 5,
      },
      ir,
    );
    const { container } = render(<WorkflowGraph ir={ir} overlay={ov} />);
    expect(container.textContent).toContain('at most 3 rounds until');
    expect(container.textContent).toContain('round 2 of 3');
  });

  it('clicking a node selects the instance that explains its state', () => {
    const ir = examples.reviewLoop();
    const select = vi.fn();
    const ov = applyPage(
      emptyOverlay('r', 'running'),
      {
        nodes: [
          {
            key: 'fix[1]/work',
            node_id: 'fix/work',
            state: 'done',
            attempt: 1,
          },
          {
            key: 'fix[2]/work',
            node_id: 'fix/work',
            state: 'running',
            attempt: 1,
          },
        ],
        last_seq: 5,
      },
      ir,
    );
    render(<WorkflowGraph ir={ir} overlay={ov} onSelect={select} />);
    fireEvent.click(
      screen.getByRole('button', { name: /^work, agent claude, running/ }),
    );
    expect(select).toHaveBeenCalledWith('fix[2]/work');
  });
});

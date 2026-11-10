// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import { describe, expect, it } from 'vitest';
import { examples } from '../../../../../plugins/workflows/tools/examples.ts';
import {
  allNodes,
  indexUnder,
  layoutBody,
  loopHeader,
  mapHeader,
  nodeIdOfKey,
} from '../../src/lib/workflow-graph.js';
import type { Body, Node } from '../../src/lib/workflow-types.js';

const leaf = (id: string): Node => ({
  id,
  kind: 'check',
  argv: ['true'],
  timeout_s: 1,
});

describe('layoutBody', () => {
  it('ranks entry nodes 0 and joins at 1+max', () => {
    const body: Body = {
      nodes: [leaf('a'), leaf('b'), leaf('c'), leaf('d')],
      edges: [
        { from: 'a', to: 'c' },
        { from: 'b', to: 'c' },
        { from: 'c', to: 'd' },
        { from: 'a', to: 'd' },
      ],
    };
    const { rows } = layoutBody(body);
    expect(rows.map((r) => r.map((n) => n.id))).toEqual([
      ['a', 'b'],
      ['c'],
      ['d'],
    ]);
  });

  it('keeps IR order within a rank and carries edge conditions', () => {
    const body: Body = {
      nodes: [leaf('z'), leaf('a'), leaf('m')],
      edges: [{ from: 'z', to: 'm', when: { ref: 'z.ok', op: 'truthy' } }],
    };
    const l = layoutBody(body);
    expect(l.rows[0].map((n) => n.id)).toEqual(['z', 'a']);
    expect(l.edges).toEqual([{ from: 'z', to: 'm', label: 'z.ok' }]);
  });

  it('does not hang on a malformed cycle', () => {
    const body: Body = {
      nodes: [leaf('a'), leaf('b')],
      edges: [
        { from: 'a', to: 'b' },
        { from: 'b', to: 'a' },
      ],
    };
    expect(() => layoutBody(body)).not.toThrow();
  });

  it('edges never leave their body: every edge of every body joins its own nodes', () => {
    for (const make of Object.values(examples)) {
      const ir = make();
      const check = (b: Body) => {
        const ids = new Set(b.nodes.map((n) => n.id));
        for (const e of layoutBody(b).edges) {
          expect(ids.has(e.from) && ids.has(e.to)).toBe(true);
        }
        for (const n of b.nodes) {
          if (n.kind === 'group' || n.kind === 'loop' || n.kind === 'map')
            check(n);
        }
      };
      check(ir);
    }
  });

  it('the three worked examples lay out without throwing', () => {
    const names = Object.keys(examples);
    expect(names.length).toBe(3);
    for (const make of Object.values(examples)) {
      const ir = make();
      const l = layoutBody(ir);
      expect(
        l.rows
          .flat()
          .map((n) => n.id)
          .sort(),
      ).toEqual(ir.nodes.map((n) => n.id).sort());
    }
  });
});

describe('headers', () => {
  it('loop and map headers read at most N rounds and for each ref, C at once', () => {
    const loop = allNodes(examples.reviewLoop()).find((n) => n.kind === 'loop');
    expect(loop && loop.kind === 'loop' && loopHeader(loop)).toMatch(
      /^fix - at most 3 rounds until /,
    );
    const map = allNodes(examples.fanOutThenVerify()).find(
      (n) => n.kind === 'map',
    );
    expect(map && map.kind === 'map' && mapHeader(map)).toMatch(
      /^each - for each split\.tasks, 4 at once$/,
    );
  });
});

describe('instance keys', () => {
  it('strips indexes to the IR id', () => {
    expect(nodeIdOfKey('work/each[2]/do')).toBe('work/each/do');
    expect(nodeIdOfKey('a[1]/b[0]/c')).toBe('a/b/c');
    expect(nodeIdOfKey('plain')).toBe('plain');
  });

  it('reads the index under a container', () => {
    expect(indexUnder('work/each[2]/do', 'work/each')).toBe(2);
    expect(indexUnder('a[1]/b[7]/c', 'a/b')).toBe(7);
    expect(indexUnder('a[1]/b[7]/c', 'a')).toBe(1);
    expect(indexUnder('work/each/do', 'work/each')).toBeNull();
    expect(indexUnder('other[1]/do', 'work/each')).toBeNull();
  });
});

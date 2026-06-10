import { describe, it, expect } from 'vitest';
import { loadCollapsed, serializeCollapsed, pruneCollapsed } from '../../src/lib/collapsed.js';

describe('collapsed persistence helpers', () => {
  it('round-trips a set', () => {
    const s = new Set(['p1', 'p2']);
    expect(loadCollapsed(serializeCollapsed(s))).toEqual(s);
  });

  it('tolerates garbage input', () => {
    expect(loadCollapsed(null)).toEqual(new Set());
    expect(loadCollapsed('')).toEqual(new Set());
    expect(loadCollapsed('not json {')).toEqual(new Set());
    expect(loadCollapsed('{"a":1}')).toEqual(new Set());
    expect(loadCollapsed('[1, null, "p1", ""]')).toEqual(new Set(['p1']));
  });

  it('prunes ids for deleted projects', () => {
    const { set, changed } = pruneCollapsed(new Set(['p1', 'gone']), ['p1', 'p2']);
    expect(changed).toBe(true);
    expect(set).toEqual(new Set(['p1']));
  });

  it('reports unchanged when everything is live', () => {
    const { set, changed } = pruneCollapsed(new Set(['p1']), ['p1', 'p2']);
    expect(changed).toBe(false);
    expect(set).toEqual(new Set(['p1']));
  });
});

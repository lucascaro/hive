import { describe, it, expect } from 'vitest';
import { filterMinimized } from '../../src/lib/minimized.js';

describe('filterMinimized', () => {
  const sessions = [
    { id: 'a', name: 'one' },
    { id: 'b', name: 'two' },
    { id: 'c', name: 'three' },
  ];

  it('returns input unchanged when set is empty', () => {
    expect(filterMinimized(sessions, new Set())).toEqual(sessions);
  });

  it('removes sessions whose id is in the set', () => {
    const out = filterMinimized(sessions, new Set(['b']));
    expect(out.map((s) => s.id)).toEqual(['a', 'c']);
  });

  it('preserves order of surviving sessions', () => {
    const out = filterMinimized(sessions, new Set(['a']));
    expect(out.map((s) => s.id)).toEqual(['b', 'c']);
  });

  it('returns empty array when all sessions are minimized', () => {
    const out = filterMinimized(sessions, new Set(['a', 'b', 'c']));
    expect(out).toEqual([]);
  });

  it('tolerates null/undefined set by returning input unchanged', () => {
    expect(filterMinimized(sessions, null)).toEqual(sessions);
    expect(filterMinimized(sessions, undefined)).toEqual(sessions);
  });
});

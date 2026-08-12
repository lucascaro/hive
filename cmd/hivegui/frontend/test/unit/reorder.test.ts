import { describe, it, expect } from 'vitest';
import { reorderTarget } from '../../src/lib/reorder.js';

// Display order: sorted by project, then by session order. Global
// indices are the array indices, which is the index space the daemon's
// Update expects.
const ordered = [
  { id: 'a0', projectId: 'A' },
  { id: 'a1', projectId: 'A' },
  { id: 'b0', projectId: 'B' },
  { id: 'b1', projectId: 'B' },
  { id: 'b2', projectId: 'B' },
];

describe('reorderTarget', () => {
  it('moves down within the project', () => {
    // b1 swaps with b2, whose current global index is 4.
    expect(reorderTarget(ordered, 'b1', +1)).toBe(4);
  });

  it('moves up within the project', () => {
    expect(reorderTarget(ordered, 'b1', -1)).toBe(2);
  });

  it('wraps from the last session in a project to the first', () => {
    expect(reorderTarget(ordered, 'b2', +1)).toBe(2);
  });

  it('wraps from the first session in a project to the last', () => {
    expect(reorderTarget(ordered, 'b0', -1)).toBe(4);
  });

  it('returns null for a project with a single session', () => {
    const solo = [{ id: 'a0', projectId: 'A' }, ...ordered.slice(2)];
    expect(reorderTarget(solo, 'a0', +1)).toBeNull();
    expect(reorderTarget(solo, 'a0', -1)).toBeNull();
  });

  it('resolves correctly when projects are interleaved in the global order', () => {
    const mixed = [
      { id: 'b0', projectId: 'B' },
      { id: 'a0', projectId: 'A' },
      { id: 'b1', projectId: 'B' },
    ];
    // b0 down targets b1's global index 2 — it must skip over a0.
    expect(reorderTarget(mixed, 'b0', +1)).toBe(2);
    expect(reorderTarget(mixed, 'b1', -1)).toBe(0);
  });

  it('returns null for an unknown or null active session', () => {
    expect(reorderTarget(ordered, 'nope', +1)).toBeNull();
    expect(reorderTarget(ordered, null, +1)).toBeNull();
    expect(reorderTarget(ordered, 'b1', 0)).toBeNull();
  });

  it('reads both projectId and project_id spellings', () => {
    const snake = [
      { id: 'b0', project_id: 'B' },
      { id: 'a0', projectId: 'A' },
      { id: 'b1', project_id: 'B' },
    ];
    expect(reorderTarget(snake, 'b0', +1)).toBe(2);
  });
});

import { describe, it, expect } from 'vitest';
import {
  computeGridDims, buildGridLayout, computeSpatialMove,
} from '../../src/lib/grid.js';

describe('computeGridDims', () => {
  it('n=1 → 1x1', () => {
    expect(computeGridDims(1, 800, 600)).toMatchObject({ rows: 1, cols: 1 });
  });
  it('n=2 → side-by-side regardless of window shape', () => {
    expect(computeGridDims(2, 800, 600)).toMatchObject({ rows: 1, cols: 2 });
    expect(computeGridDims(2, 400, 1000)).toMatchObject({ rows: 1, cols: 2 });
  });
  it('n=4 picks a balanced 2x2 on a square-ish window', () => {
    expect(computeGridDims(4, 1000, 600)).toMatchObject({ rows: 2, cols: 2 });
  });
  it('handles degenerate inputs without throwing', () => {
    expect(computeGridDims(0, 800, 600)).toEqual({ rows: 1, cols: 1 });
    expect(computeGridDims(3, 0, 0)).toBeTruthy();
  });
});

describe('buildGridLayout', () => {
  it('n=3 in a 2x2: top-left tile absorbs the empty cell', () => {
    const { rows, cols, assignments, cellMap } = buildGridLayout(3, 1000, 600);
    expect(rows).toBe(2);
    expect(cols).toBe(2);
    // Trailing empty cell is at index 3 (row 1 col 1); the tile above
    // it (index 1) extends downward... wait, the algorithm extends the
    // tile directly above (e - cols = 3 - 2 = 1) so tile 1 grows.
    expect(assignments[1].rowSpan).toBe(2);
    expect(cellMap).toEqual([0, 1, 2, 1]);
  });

  it('n=4 in a 2x2: no absorption, every cell mapped', () => {
    const { assignments, cellMap } = buildGridLayout(4, 1000, 600);
    expect(assignments.every((a) => a.rowSpan === 1)).toBe(true);
    expect(cellMap).toEqual([0, 1, 2, 3]);
  });
});

describe('computeSpatialMove — ctrl-arrow direction convention', () => {
  // 2x2 grid, 4 tiles:
  //   0 1
  //   2 3
  const layout4 = buildGridLayout(4, 1000, 600);

  it('right from 0 lands on 1', () => {
    expect(computeSpatialMove(layout4, 0, +1, 0)).toBe(1);
  });
  it('left from 1 lands on 0', () => {
    expect(computeSpatialMove(layout4, 1, -1, 0)).toBe(0);
  });
  it('down from 0 lands on 2 (down = positive dRow)', () => {
    expect(computeSpatialMove(layout4, 0, 0, +1)).toBe(2);
  });
  it('up from 2 lands on 0 (up = negative dRow)', () => {
    expect(computeSpatialMove(layout4, 2, 0, -1)).toBe(0);
  });
  it('returns null at grid edges', () => {
    expect(computeSpatialMove(layout4, 0, 0, -1)).toBe(null);
    expect(computeSpatialMove(layout4, 0, -1, 0)).toBe(null);
    expect(computeSpatialMove(layout4, 3, +1, 0)).toBe(null);
    expect(computeSpatialMove(layout4, 3, 0, +1)).toBe(null);
  });

  it('row-span absorption: 3 tiles in 2x2, right from tile 2 lands on tile 1', () => {
    // n=3 in 2x2: tile 1 spans rows 0 and 1 of column 1.
    //   0 1
    //   2 1
    const layout3 = buildGridLayout(3, 1000, 600);
    expect(computeSpatialMove(layout3, 2, +1, 0)).toBe(1);
  });

  it('down from tile 1 (rowSpan=2) does not loop back into itself', () => {
    const layout3 = buildGridLayout(3, 1000, 600);
    // Tile 1 covers rows 0+1 col 1; pressing down from it should
    // return null (no tile below), not pick itself.
    expect(computeSpatialMove(layout3, 1, 0, +1)).toBe(null);
  });
});

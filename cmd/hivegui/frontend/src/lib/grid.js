// Pure grid layout math. No DOM, no state. All inputs explicit.

// computeGridDims picks (rows, cols) that fills a w×h container
// without scrolling, biasing tile aspect toward typical terminal
// proportions (~1.6 wide-to-tall). Small-n cases match user
// expectation rather than the optimizer: n=2 is always side-by-side.
export function computeGridDims(n, w, h) {
  if (n <= 0) return { rows: 1, cols: 1 };
  if (n === 1) return { rows: 1, cols: 1 };
  if (n === 2) return { rows: 1, cols: 2 };

  const targetAspect = 1.6;
  let best = { rows: 1, cols: n, score: Infinity };
  for (let cols = 1; cols <= n; cols++) {
    const rows = Math.ceil(n / cols);
    const tileW = w / cols;
    const tileH = h / rows;
    if (tileW <= 0 || tileH <= 0) continue;
    const aspect = tileW / tileH;
    const empty = rows * cols - n;
    const score = Math.abs(Math.log(aspect / targetAspect)) + empty * 0.05;
    if (score < best.score) best = { rows, cols, score };
  }
  return best;
}

// buildGridLayout computes the full layout (dims + per-tile
// assignments + cellMap) for n tiles in w×h. Pure: same inputs ⇒ same
// output. cellMap is row-major, length rows*cols, holding the
// session index that owns each cell (including absorbed cells from
// row-spans), or null for genuinely empty cells.
export function buildGridLayout(n, w, h) {
  const { rows, cols } = computeGridDims(n, w, h);
  const assignments = new Array(n);
  for (let i = 0; i < n; i++) {
    assignments[i] = { row: Math.floor(i / cols), col: i % cols, rowSpan: 1 };
  }
  // Tiles directly above empty trailing cells extend downward.
  for (let e = n; e < rows * cols; e++) {
    const aboveIdx = e - cols;
    if (aboveIdx >= 0 && aboveIdx < n) {
      assignments[aboveIdx].rowSpan += 1;
    }
  }
  const cellMap = new Array(rows * cols).fill(null);
  for (let i = 0; i < n; i++) {
    const a = assignments[i];
    for (let dr = 0; dr < a.rowSpan; dr++) {
      cellMap[(a.row + dr) * cols + a.col] = i;
    }
  }
  return { rows, cols, assignments, cellMap };
}

// computeSpatialMove returns the target session index for an arrow
// move in the grid, or null if no move is possible. Pure: takes a
// layout (as produced by buildGridLayout), the current index, and a
// direction vector (dCol, dRow) where each component is in {-1, 0, 1}.
//
// Direction convention is screen-space: dCol>0 = right, dRow>0 =
// down. The recent ctrl-arrow regression was a direction-sign bug;
// this function locks the convention down.
export function computeSpatialMove(layout, currentIdx, dCol, dRow) {
  const { rows, cols, cellMap, assignments } = layout;
  if (!assignments || currentIdx < 0 || currentIdx >= assignments.length) return null;
  const a = assignments[currentIdx];
  // For downward moves, start from the tile's bottom edge.
  let r = a.row;
  const c = a.col;
  if (dRow > 0) r = a.row + a.rowSpan - 1;

  let nr = r + dRow;
  let nc = c + dCol;
  while (nr >= 0 && nr < rows && nc >= 0 && nc < cols) {
    const target = cellMap[nr * cols + nc];
    if (target != null && target !== currentIdx) return target;
    nr += dRow;
    nc += dCol;
  }
  return null;
}

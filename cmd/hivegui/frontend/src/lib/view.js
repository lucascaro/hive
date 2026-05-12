// View mode constants + persistence helpers for the GUI's
// single/grid view state. Pure functions kept here so they are
// unit-testable without a DOM / localStorage harness.

export const VIEW_SINGLE = 'single';
export const VIEW_GRID_PROJECT = 'grid-project';
export const VIEW_GRID_ALL = 'grid-all';
export const VIEW_STORAGE_KEY = 'hive.view';

const VALID_VIEWS = new Set([VIEW_SINGLE, VIEW_GRID_PROJECT, VIEW_GRID_ALL]);

export function normalizeView(v) {
  return VALID_VIEWS.has(v) ? v : VIEW_SINGLE;
}

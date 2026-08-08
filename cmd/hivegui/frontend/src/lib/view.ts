// View mode constants + persistence helpers for the GUI's
// single/grid view state. Pure functions kept here so they are
// unit-testable without a DOM / localStorage harness.

export const VIEW_SINGLE = 'single';
export const VIEW_GRID_PROJECT = 'grid-project';
export const VIEW_GRID_ALL = 'grid-all';
export const VIEW_STORAGE_KEY = 'hive.view';

export type ViewMode =
  | typeof VIEW_SINGLE
  | typeof VIEW_GRID_PROJECT
  | typeof VIEW_GRID_ALL;

const VALID_VIEWS = new Set<string>([
  VIEW_SINGLE,
  VIEW_GRID_PROJECT,
  VIEW_GRID_ALL,
]);

// `v` is `unknown`: the caller feeds it straight from localStorage.
export function normalizeView(v: unknown): ViewMode {
  // The Set membership check is the narrow; TS can't see through
  // `Set<string>.has`, hence the cast.
  return typeof v === 'string' && VALID_VIEWS.has(v)
    ? (v as ViewMode)
    : VIEW_SINGLE;
}

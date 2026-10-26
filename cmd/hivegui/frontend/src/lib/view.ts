// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// View mode constants + persistence helpers for the GUI's
// single/grid view state. Pure functions kept here so they are
// unit-testable without a DOM / localStorage harness.

export const VIEW_SINGLE = 'single';
export const VIEW_GRID_PROJECT = 'grid-project';
export const VIEW_GRID_ALL = 'grid-all';
// The ad-hoc grid (spec 533): tiles are a throwaway ⌘-click selection, so
// it is never persisted or restored — see normalizeView.
export const VIEW_GRID_SELECT = 'grid-select';
export const VIEW_STORAGE_KEY = 'hive.view';

export type ViewMode =
  | typeof VIEW_SINGLE
  | typeof VIEW_GRID_PROJECT
  | typeof VIEW_GRID_ALL
  | typeof VIEW_GRID_SELECT;

// The restorable views. VIEW_GRID_SELECT is deliberately absent: its id
// set does not survive a restart, so a saved one would restore an empty grid.
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

// resolveView downgrades a grid request to 'single' when the grid would
// contain fewer than two tiles. A one-tile grid is visually identical to
// focused mode but loses the focused-mode keybindings, so entering one is
// never what the user meant.
export function resolveView(requested: ViewMode, scopeCount: number): ViewMode {
  if (requested !== VIEW_SINGLE && scopeCount < 2) return VIEW_SINGLE;
  return requested;
}

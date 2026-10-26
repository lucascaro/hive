// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Pure helpers for the ad-hoc grid (spec 533): a throwaway set of session
// ids picked with ⌘/Ctrl-click in the sidebar. Kept DOM- and store-free so
// the toggle rules are unit-testable; app/view.ts applies the result.

export const GRID_SELECT_MODE_STORAGE_KEY = 'hive.gridSelectMode';

// 'auto': the grid opens as soon as two sessions are selected.
// 'manual': ⌘-click only marks rows; ⌘G opens a grid of the marked ones.
export type GridSelectMode = 'auto' | 'manual';

// `v` is `unknown`: the caller feeds it straight from localStorage.
export function normalizeGridSelectMode(v: unknown): GridSelectMode {
  return v === 'manual' ? 'manual' : 'auto';
}

/**
 * toggleSelection returns the selection after ⌘-clicking `id`.
 *
 * An empty selection is seeded with the active session first, so ⌘-clicking
 * B while on A means {A, B} rather than {B}. `activeId` is null when the
 * active session is not on screen (none, or minimized) — then nothing is
 * seeded. Clicking the sole seeded session toggles it straight back off,
 * leaving the selection empty (a no-op by design).
 */
export function toggleSelection(
  selection: readonly string[],
  id: string,
  activeId: string | null,
): string[] {
  const base = selection.length === 0 && activeId ? [activeId] : selection;
  return base.includes(id) ? base.filter((x) => x !== id) : [...base, id];
}

/** isSelectToggle: whether a click edits the ad-hoc selection — ⌘ on
 *  macOS, Ctrl elsewhere. The other modifier is a plain click: on macOS
 *  Ctrl-click is the context-menu gesture, and on Windows/Linux the Meta
 *  key opens the OS menu. */
export function isSelectToggle(
  e: { metaKey: boolean; ctrlKey: boolean },
  mac: boolean,
): boolean {
  return mac ? e.metaKey : e.ctrlKey;
}

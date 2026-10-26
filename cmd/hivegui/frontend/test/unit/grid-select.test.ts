import { describe, expect, it } from 'vitest';
import {
  isSelectToggle,
  normalizeGridSelectMode,
  toggleSelection,
} from '../../src/lib/grid-select.js';

describe('toggleSelection', () => {
  it('seeds an empty selection with the active session', () => {
    expect(toggleSelection([], 'B', 'A')).toEqual(['A', 'B']);
  });

  it('adds to a non-empty selection without reseeding', () => {
    expect(toggleSelection(['A', 'B'], 'C', 'A')).toEqual(['A', 'B', 'C']);
  });

  it('removes an id that is already selected', () => {
    expect(toggleSelection(['A', 'B', 'C'], 'B', 'A')).toEqual(['A', 'C']);
  });

  it('does not seed when no session is on screen', () => {
    expect(toggleSelection([], 'B', null)).toEqual(['B']);
  });

  it('is a no-op when clicking the lone active session', () => {
    expect(toggleSelection([], 'A', 'A')).toEqual([]);
  });

  it('does not mutate its input', () => {
    const sel = ['A', 'B'];
    toggleSelection(sel, 'C', 'A');
    expect(sel).toEqual(['A', 'B']);
  });
});

describe('normalizeGridSelectMode', () => {
  it('defaults to auto', () => {
    expect(normalizeGridSelectMode(null)).toBe('auto');
    expect(normalizeGridSelectMode('junk')).toBe('auto');
  });

  it('keeps manual', () => {
    expect(normalizeGridSelectMode('manual')).toBe('manual');
  });
});

describe('isSelectToggle', () => {
  const mods = (metaKey: boolean, ctrlKey: boolean) => ({ metaKey, ctrlKey });
  it('is ⌘ on macOS and ignores Ctrl (the context-menu gesture)', () => {
    expect(isSelectToggle(mods(true, false), true)).toBe(true);
    expect(isSelectToggle(mods(false, true), true)).toBe(false);
  });
  it('is Ctrl elsewhere and ignores Meta', () => {
    expect(isSelectToggle(mods(false, true), false)).toBe(true);
    expect(isSelectToggle(mods(true, false), false)).toBe(false);
  });
  it('is false for a plain click', () => {
    expect(isSelectToggle(mods(false, false), true)).toBe(false);
    expect(isSelectToggle(mods(false, false), false)).toBe(false);
  });
});

import { describe, it, expect } from 'vitest';
import {
  normalizeView,
  resolveView,
  VIEW_SINGLE,
  VIEW_GRID_PROJECT,
  VIEW_GRID_ALL,
} from '../../src/lib/view.js';

describe('normalizeView', () => {
  it('passes valid views through', () => {
    expect(normalizeView('single')).toBe(VIEW_SINGLE);
    expect(normalizeView('grid-project')).toBe(VIEW_GRID_PROJECT);
    expect(normalizeView('grid-all')).toBe(VIEW_GRID_ALL);
  });
  it('falls back to single for unknown values', () => {
    expect(normalizeView('zoomed')).toBe(VIEW_SINGLE);
    expect(normalizeView('')).toBe(VIEW_SINGLE);
    expect(normalizeView(null)).toBe(VIEW_SINGLE);
    expect(normalizeView(undefined)).toBe(VIEW_SINGLE);
  });
});

describe('resolveView', () => {
  it('keeps single unchanged', () => {
    expect(resolveView(VIEW_SINGLE, 0)).toBe(VIEW_SINGLE);
    expect(resolveView(VIEW_SINGLE, 5)).toBe(VIEW_SINGLE);
  });
  it('downgrades grid views to single at 0 and 1 sessions', () => {
    expect(resolveView(VIEW_GRID_ALL, 0)).toBe(VIEW_SINGLE);
    expect(resolveView(VIEW_GRID_ALL, 1)).toBe(VIEW_SINGLE);
    expect(resolveView(VIEW_GRID_PROJECT, 0)).toBe(VIEW_SINGLE);
    expect(resolveView(VIEW_GRID_PROJECT, 1)).toBe(VIEW_SINGLE);
  });
  it('permits grid at 2 or more sessions', () => {
    expect(resolveView(VIEW_GRID_ALL, 2)).toBe(VIEW_GRID_ALL);
    expect(resolveView(VIEW_GRID_PROJECT, 3)).toBe(VIEW_GRID_PROJECT);
  });
});

import { describe, it, expect } from 'vitest';
import {
  normalizeView,
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

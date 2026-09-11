// @vitest-environment jsdom
import { describe, it, expect, beforeEach } from 'vitest';
import {
  DENSITY_KEY,
  applyDensity,
  readDensity,
  type Density,
} from '../../src/theme/density';

function storage(value: string | null): Storage {
  return {
    getItem: () => value,
  } as unknown as Storage;
}

const throwing: Storage = {
  getItem() {
    throw new Error('denied');
  },
} as unknown as Storage;

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-density');
});

describe('readDensity', () => {
  it('falls back to normal for absent, empty and garbage values', () => {
    expect(readDensity(storage(null))).toBe('normal');
    expect(readDensity(storage(''))).toBe('normal');
    expect(readDensity(storage('roomy'))).toBe('normal');
  });

  it('falls back rather than throwing when storage is denied', () => {
    expect(readDensity(throwing)).toBe('normal');
  });

  it('round-trips every valid value', () => {
    for (const d of ['normal', 'tight', 'compact'] as Density[]) {
      localStorage.setItem(DENSITY_KEY, d);
      expect(readDensity()).toBe(d);
    }
  });
});

describe('applyDensity', () => {
  it('stamps the density on the document element', () => {
    applyDensity('compact');
    expect(document.documentElement.dataset.density).toBe('compact');
    applyDensity('tight');
    expect(document.documentElement.dataset.density).toBe('tight');
  });

  it('stamps the default for a value that is not a density', () => {
    applyDensity('roomy' as Density);
    expect(document.documentElement.dataset.density).toBe('normal');
  });
});

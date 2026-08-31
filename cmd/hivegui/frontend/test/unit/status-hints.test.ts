import { describe, it, expect } from 'vitest';
import { modeHints } from '../../src/lib/status.js';

describe('modeHints', () => {
  it('offers grid + palette in single view', () => {
    expect(modeHints('single', true)).toEqual([
      { key: '⌘G', label: 'grid' },
      { key: '⇧⌘K', label: 'actions' },
    ]);
  });

  it('offers focus + move in a grid view', () => {
    expect(modeHints('grid-all', true)).toEqual([
      { key: '⌘G', label: 'focus' },
      { key: '⌘↑↓←→', label: 'move' },
    ]);
    expect(modeHints('grid-project', true)).toEqual(
      modeHints('grid-all', true),
    );
  });

  it('spells modifiers out off macOS', () => {
    expect(modeHints('single', false)[0].key).toBe('Ctrl+G');
    expect(modeHints('single', false)[1].key).toBe('Ctrl+Shift+K');
    expect(modeHints('grid-all', false)[1].key).toBe('Ctrl+Arrows');
  });

  it('never shows more than two hints', () => {
    for (const v of ['single', 'grid-all', 'grid-project', 'nonsense']) {
      expect(modeHints(v, true).length).toBeLessThanOrEqual(2);
    }
  });
});

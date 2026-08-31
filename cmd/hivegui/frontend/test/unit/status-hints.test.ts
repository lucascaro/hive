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
    expect(modeHints('grid-project', true)).toEqual([
      { key: '⌘G', label: 'focus' },
      { key: '⌘↑↓←→', label: 'move' },
    ]);
  });

  // Each grid is left by the chord that opened it, so the focus hint
  // differs between the two grids — plain ⌘G in grid-all would switch to
  // the project grid rather than focus a pane.
  it('names the chord that actually leaves each grid', () => {
    expect(modeHints('grid-all', true)[0]).toEqual({
      key: '⇧⌘G',
      label: 'focus',
    });
    expect(modeHints('grid-project', true)[0]).toEqual({
      key: '⌘G',
      label: 'focus',
    });
  });

  it('spells modifiers out off macOS', () => {
    expect(modeHints('single', false)[0].key).toBe('Ctrl+G');
    expect(modeHints('single', false)[1].key).toBe('Ctrl+Shift+K');
    expect(modeHints('grid-all', false)[0].key).toBe('Ctrl+Shift+G');
    expect(modeHints('grid-all', false)[1].key).toBe('Ctrl+Arrows');
  });

  it('never shows more than two hints', () => {
    for (const v of ['single', 'grid-all', 'grid-project', 'nonsense']) {
      expect(modeHints(v, true).length).toBeLessThanOrEqual(2);
    }
  });
});

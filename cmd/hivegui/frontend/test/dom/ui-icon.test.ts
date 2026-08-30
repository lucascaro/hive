// @vitest-environment jsdom
//
// The icon primitive (src/ui/icon.ts) and the sprite it inlines.
// The sprite is imported with Vite's `?raw`, so this test also proves
// the build-time inlining path works under vitest, not just in Wails.
import { describe, it, expect, beforeEach } from 'vitest';
import { icon, ensureSprite, ICON_NAMES } from '../../src/ui/icon.js';

beforeEach(() => {
  document.body.innerHTML = '';
});

describe('icon()', () => {
  it('injects the sprite exactly once', () => {
    icon('plus');
    icon('x');
    ensureSprite();
    expect(document.querySelectorAll('#hv-icon-sprite')).toHaveLength(1);
  });

  it('renders a <use> pointing at the sprite symbol', () => {
    const el = icon('branch');
    expect(el.tagName.toLowerCase()).toBe('svg');
    expect(el.getAttribute('class')).toBe('hv-icon');
    expect(el.querySelector('use')?.getAttribute('href')).toBe('#hv-branch');
    expect(el.getAttribute('aria-hidden')).toBe('true');
  });

  it('defaults to 14px and honours the 12px inline size', () => {
    expect(icon('check').getAttribute('width')).toBe('14');
    const small = icon('check', { size: 12 });
    expect(small.getAttribute('width')).toBe('12');
    expect(small.dataset.size).toBe('12');
  });

  it('has a symbol in the sprite for every declared name', () => {
    ensureSprite();
    for (const name of ICON_NAMES) {
      expect(document.getElementById(`hv-${name}`), name).not.toBeNull();
    }
    expect(ICON_NAMES).toHaveLength(22);
  });
});

// @vitest-environment jsdom
//
// The icon primitive (components/Icon.tsx) and the sprite it draws from
// (lib/icon-sprite.ts). The sprite is imported with Vite's `?raw`, so
// this test also proves the build-time inlining path works under vitest,
// not just in Wails.
//
// Was ui-icon.test.ts against src/ui/icon.ts's imperative icon(), which
// Phase 2 of the tile-chrome port deleted along with the rest of
// src/ui/. Same assertions, one paradigm.
import { describe, it, expect, beforeEach } from 'vitest';
import { render } from '@testing-library/react';

import { Icon } from '../../src/components/Icon.js';
import { ensureSprite, ICON_NAMES } from '../../src/lib/icon-sprite.js';

beforeEach(() => {
  document.body.innerHTML = '';
});

const draw = (el: React.ReactElement) =>
  render(el).container.querySelector('svg') as SVGSVGElement;

describe('<Icon>', () => {
  it('injects the sprite exactly once', () => {
    render(
      <>
        <Icon name="plus" />
        <Icon name="x" />
      </>,
    );
    ensureSprite();
    expect(document.querySelectorAll('#hv-icon-sprite')).toHaveLength(1);
  });

  it('renders a <use> pointing at the sprite symbol', () => {
    const el = draw(<Icon name="branch" />);
    expect(el.tagName.toLowerCase()).toBe('svg');
    expect(el.getAttribute('class')).toBe('hv-icon');
    expect(el.querySelector('use')?.getAttribute('href')).toBe('#hv-branch');
    expect(el.getAttribute('aria-hidden')).toBe('true');
  });

  it('defaults to 14px and honours the 12px inline size', () => {
    expect(draw(<Icon name="check" />).getAttribute('width')).toBe('14');
    const small = draw(<Icon name="check" size={12} />);
    expect(small.getAttribute('width')).toBe('12');
    expect(small.dataset.size).toBe('12');
  });

  it('has a symbol in the sprite for every declared name', () => {
    ensureSprite();
    for (const name of ICON_NAMES) {
      expect(document.getElementById(`hv-${name}`), name).not.toBeNull();
    }
    expect(ICON_NAMES).toHaveLength(24);
  });
});

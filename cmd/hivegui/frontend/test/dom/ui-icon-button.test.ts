// @vitest-environment jsdom
//
// iconButton() and kbd() (src/ui/). The aria-label assertion is the
// point of the file: an icon-only control with no label is invisible to
// a screen reader, so the primitive refuses to build one.
import { describe, it, expect, vi } from 'vitest';
import { iconButton } from '../../src/ui/icon-button.js';
import { kbd } from '../../src/ui/kbd.js';

describe('iconButton()', () => {
  it('is a type=button with the icon inside and no text', () => {
    const b = iconButton({ icon: 'x', label: 'Close' });
    expect(b.tagName).toBe('BUTTON');
    expect(b.type).toBe('button');
    expect(b.className).toBe('hv-icon-btn');
    expect(b.querySelector('use')?.getAttribute('href')).toBe('#hv-x');
    expect(b.textContent).toBe('');
  });

  it('mirrors the label into aria-label and title', () => {
    const b = iconButton({ icon: 'plus', label: 'New project' });
    expect(b.getAttribute('aria-label')).toBe('New project');
    expect(b.title).toBe('New project');
  });

  it('throws on a missing label rather than shipping an unlabelled control', () => {
    expect(() => iconButton({ icon: 'plus', label: '  ' })).toThrow(/label/i);
  });

  it('wires onClick and appends extra classes', () => {
    const onClick = vi.fn();
    const b = iconButton({
      icon: 'minus',
      label: 'Minimize',
      onClick,
      className: 'session-minimize',
    });
    expect(b.className).toBe('hv-icon-btn session-minimize');
    b.click();
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('carries the 22px size as a data attribute', () => {
    expect(
      iconButton({ icon: 'x', label: 'Close', size: 22 }).dataset.size,
    ).toBe('22');
  });
});

describe('kbd()', () => {
  it('renders a <kbd class="hv-kbd"> with the literal text', () => {
    const el = kbd('[esc]');
    expect(el.tagName).toBe('KBD');
    expect(el.className).toBe('hv-kbd');
    expect(el.textContent).toBe('[esc]');
  });
});

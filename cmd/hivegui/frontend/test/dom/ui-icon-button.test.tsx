// @vitest-environment jsdom
//
// IconButton (components/IconButton.tsx). The aria-label assertion is
// the point of the file: an icon-only control with no label is invisible
// to a screen reader, so the primitive refuses to build one.
//
// Was ui-icon-button.test.ts against src/ui/icon-button.ts's
// iconButton(), deleted with the rest of src/ui/ in Phase 2 of the
// tile-chrome port.
import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render } from '@testing-library/react';

import { IconButton } from '../../src/components/IconButton.js';

const draw = (el: React.ReactElement) =>
  render(el).container.querySelector('button') as HTMLButtonElement;

describe('<IconButton>', () => {
  it('is a type=button with the icon inside and no text', () => {
    const b = draw(<IconButton icon="x" label="Close" />);
    expect(b.tagName).toBe('BUTTON');
    expect(b.type).toBe('button');
    expect(b.className).toBe('hv-icon-btn');
    expect(b.querySelector('use')?.getAttribute('href')).toBe('#hv-x');
    expect(b.textContent).toBe('');
  });

  it('mirrors the label into aria-label and title', () => {
    const b = draw(<IconButton icon="plus" label="New project" />);
    expect(b.getAttribute('aria-label')).toBe('New project');
    expect(b.title).toBe('New project');
  });

  it('throws on a missing label rather than shipping an unlabelled control', () => {
    // React logs the thrown render error; the assertion is the throw.
    const err = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => render(<IconButton icon="plus" label="  " />)).toThrow(
      /label/i,
    );
    err.mockRestore();
  });

  it('wires onClick and appends extra classes', () => {
    const onClick = vi.fn();
    const b = draw(
      <IconButton
        icon="minus"
        label="Minimize"
        onClick={onClick}
        className="session-minimize"
      />,
    );
    expect(b.className).toBe('hv-icon-btn session-minimize');
    fireEvent.click(b);
    expect(onClick).toHaveBeenCalledOnce();
  });

  it('carries the 22px size as a data attribute', () => {
    expect(
      draw(<IconButton icon="x" label="Close" size={22} />).dataset.size,
    ).toBe('22');
  });
});

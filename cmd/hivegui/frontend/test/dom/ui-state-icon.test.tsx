// @vitest-environment jsdom
//
// The state icon (components/Icon.tsx › StateIcon). Was
// ui-state-icon.test.ts against src/ui/icon.ts's stateIcon(), deleted
// with the rest of src/ui/ in Phase 2 of the tile-chrome port.
import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';

import { StateIcon } from '../../src/components/Icon.js';

const draw = (el: React.ReactElement) =>
  render(el).container.querySelector('svg') as SVGSVGElement;

describe('<StateIcon>', () => {
  it('renders the shape, the data-state hook and the words', () => {
    const el = draw(<StateIcon state="attention" />);
    expect(el.dataset.state).toBe('attention');
    expect(el.getAttribute('role')).toBe('img');
    expect(el.querySelector('use')?.getAttribute('href')).toBe(
      '#hv-state-attention',
    );
    expect(el.querySelector('title')?.textContent).toBe('Waiting for you');
  });

  it('updates in place instead of being rebuilt', () => {
    // The imperative primitive needed updateStateIcon() to patch rather
    // than rebuild — the sidebar row rebuilt its icon on every render
    // otherwise. Reconciliation is what replaces it, and node identity
    // across a re-render is the assertion that proves it.
    const { container, rerender } = render(<StateIcon state="running" />);
    const use = container.querySelector('use');
    rerender(<StateIcon state="error" />);
    const el = container.querySelector('svg') as SVGSVGElement;
    expect(el.dataset.state).toBe('error');
    expect(container.querySelector('use')).toBe(use); // same node, patched
    expect(use?.getAttribute('href')).toBe('#hv-state-error');
    expect(el.querySelector('title')?.textContent).toBe('Exited with an error');
  });

  it('appends an extra class without dropping its own', () => {
    const el = draw(<StateIcon state="running" className="tile-state" />);
    expect(el.getAttribute('class')).toBe('hv-icon hv-state-icon tile-state');
  });
});

// @vitest-environment jsdom
import { describe, it, expect } from 'vitest';
import { stateIcon, updateStateIcon } from '../../src/ui/icon.js';

describe('stateIcon()', () => {
  it('renders the shape, the data-state hook and the words', () => {
    const el = stateIcon('attention');
    expect(el.dataset.state).toBe('attention');
    expect(el.getAttribute('role')).toBe('img');
    expect(el.querySelector('use')?.getAttribute('href')).toBe(
      '#hv-state-attention',
    );
    expect(el.querySelector('title')?.textContent).toBe('Waiting for you');
  });

  it('updates in place instead of being rebuilt', () => {
    const el = stateIcon('running');
    const use = el.querySelector('use');
    updateStateIcon(el, 'error');
    expect(el.dataset.state).toBe('error');
    expect(el.querySelector('use')).toBe(use); // same node, patched
    expect(use?.getAttribute('href')).toBe('#hv-state-error');
    expect(el.querySelector('title')?.textContent).toBe('Exited with an error');
  });
});

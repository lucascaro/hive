// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { button } from '../../src/ui/button.js';

describe('button()', () => {
  it('renders a type=button with the label and the default kind', () => {
    const el = button({ label: 'Restart Hive' });
    expect(el.tagName).toBe('BUTTON');
    expect(el.type).toBe('button');
    expect(el.className).toBe('hv-button');
    expect(el.dataset.kind).toBe('default');
    expect(el.textContent).toBe('Restart Hive');
  });

  it('carries the kind as a data attribute, not a class', () => {
    const el = button({ label: 'Kill', kind: 'danger' });
    expect(el.dataset.kind).toBe('danger');
    expect(el.className).toBe('hv-button');
  });

  it('prepends a leading icon before the label span', () => {
    const el = button({ label: 'New session', icon: 'plus' });
    expect(el.firstElementChild?.tagName.toLowerCase()).toBe('svg');
    expect(el.querySelector('.hv-button__label')?.textContent).toBe(
      'New session',
    );
  });

  it('wires onClick', () => {
    const spy = vi.fn();
    button({ label: 'Go', onClick: spy }).click();
    expect(spy).toHaveBeenCalledTimes(1);
  });
});

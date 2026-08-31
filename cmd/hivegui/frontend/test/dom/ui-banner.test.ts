// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { banner } from '../../src/ui/banner.js';

describe('banner()', () => {
  it('starts hidden, with the kind as data and the right aria role', () => {
    const b = banner({ kind: 'error', text: 'daemon build mismatch' });
    expect(b.el.hidden).toBe(true);
    expect(b.el.dataset.kind).toBe('error');
    expect(b.el.getAttribute('role')).toBe('alert');
    expect(b.el.querySelector('.hv-banner__text')?.textContent).toBe(
      'daemon build mismatch',
    );
  });

  it('uses role=status for the info kind', () => {
    expect(banner({ kind: 'info' }).el.getAttribute('role')).toBe('status');
  });

  it('exposes actions by id and runs their handler', () => {
    const spy = vi.fn();
    const b = banner({
      kind: 'error',
      actions: [{ id: 'restart', label: 'Restart Hive', onClick: spy }],
    });
    const btn = b.action('restart');
    expect(btn.textContent).toBe('Restart Hive');
    btn.click();
    expect(spy).toHaveBeenCalledTimes(1);
    expect(() => b.action('nope')).toThrow(/nope/);
  });

  it('renders a dismiss icon button only when onDismiss is given', () => {
    const spy = vi.fn();
    const withD = banner({ kind: 'info', onDismiss: spy });
    const dismiss = withD.el.querySelector<HTMLButtonElement>(
      '.hv-banner__dismiss',
    );
    expect(dismiss?.getAttribute('aria-label')).toBe('Dismiss');
    dismiss?.click();
    expect(spy).toHaveBeenCalledTimes(1);
    expect(
      banner({ kind: 'info' }).el.querySelector('.hv-banner__dismiss'),
    ).toBeNull();
  });

  it('show/hide/setText drive the root', () => {
    const b = banner({ kind: 'info', id: 'update-banner' });
    expect(b.el.id).toBe('update-banner');
    b.setText('Hive 2.5.0 is available.');
    b.show();
    expect(b.el.hidden).toBe(false);
    b.hide();
    expect(b.el.hidden).toBe(true);
  });
});

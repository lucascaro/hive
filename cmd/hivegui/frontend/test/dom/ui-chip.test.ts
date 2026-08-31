// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { chip } from '../../src/ui/chip';

describe('chip', () => {
  it('renders label, aria-label and fires onClick from the body button', () => {
    const onClick = vi.fn();
    const el = chip({ label: 'api', ariaLabel: 'Restore api', onClick });
    expect(el.className).toBe('hv-chip');
    const open = el.querySelector<HTMLButtonElement>('.hv-chip__open');
    expect(open?.getAttribute('aria-label')).toBe('Restore api');
    expect(el.querySelector('.hv-chip__label')?.textContent).toBe('api');
    open?.click();
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('carries state as a data attribute and renders a state icon', () => {
    const el = chip({
      label: 'api',
      state: 'attention',
      ariaLabel: 'Restore api',
      onClick: () => {},
    });
    expect(el.dataset.state).toBe('attention');
    expect(el.querySelector('.hv-state-icon')).not.toBeNull();
  });

  it('falls back to a colour dot when no state is given', () => {
    const el = chip({
      label: 'web',
      color: '#0af',
      ariaLabel: 'Restore web',
      onClick: () => {},
    });
    expect(el.querySelector('.hv-chip__swatch')).not.toBeNull();
    expect(el.style.getPropertyValue('--chip-color')).toBe('#0af');
  });

  it('renders the restore button only when onRestore is given, and it does not also fire onClick', () => {
    const onClick = vi.fn();
    const onRestore = vi.fn();
    expect(
      chip({ label: 'a', ariaLabel: 'a', onClick }).querySelector(
        '.hv-chip__restore',
      ),
    ).toBeNull();
    const el = chip({
      label: 'a',
      ariaLabel: 'Restore a',
      onClick,
      onRestore,
      restoreLabel: 'Restore a',
    });
    el.querySelector<HTMLButtonElement>('.hv-chip__restore')?.click();
    expect(onRestore).toHaveBeenCalledTimes(1);
    expect(onClick).not.toHaveBeenCalled();
  });

  it('renders the sublabel when given and omits the node when not', () => {
    const withSub = chip({
      label: 'api',
      sublabel: 'hive',
      ariaLabel: 'a',
      onClick: () => {},
    });
    expect(withSub.querySelector('.hv-chip__sub')?.textContent).toBe('hive');
    expect(
      chip({ label: 'api', ariaLabel: 'a', onClick: () => {} }).querySelector(
        '.hv-chip__sub',
      ),
    ).toBeNull();
  });
});

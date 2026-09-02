// @vitest-environment jsdom
//
// The React <Chip>. The imperative chip() it replaces is still alive for
// the minimized-SESSION tray (src/app/view.ts) until Phase 2 ports the
// chrome island, so ui-chip.test.ts covers that one and this covers this
// one. Both assert the same markup contract on purpose.
import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import type { ReactElement } from 'react';
import { Chip } from '../../src/components/Chip';

function make(node: ReactElement) {
  const r = render(node);
  const el = r.container.querySelector<HTMLElement>('.hv-chip');
  if (!el) throw new Error('no chip rendered');
  return el;
}

describe('Chip', () => {
  it('renders label, aria-label and fires onClick from the body button', () => {
    const onClick = vi.fn();
    const el = make(
      <Chip label="api" ariaLabel="Restore api" onClick={onClick} />,
    );
    expect(el.className).toBe('hv-chip');
    const open = el.querySelector<HTMLButtonElement>('.hv-chip__open');
    expect(open?.getAttribute('aria-label')).toBe('Restore api');
    expect(el.querySelector('.hv-chip__label')?.textContent).toBe('api');
    open?.click();
    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it('carries state as a data attribute and renders a state icon', () => {
    const el = make(
      <Chip
        label="api"
        state="attention"
        ariaLabel="Restore api"
        onClick={() => {}}
      />,
    );
    expect(el.dataset.state).toBe('attention');
    expect(el.querySelector('.hv-state-icon')).not.toBeNull();
  });

  it('falls back to a colour dot when no state is given', () => {
    const el = make(
      <Chip
        label="web"
        color="#0af"
        ariaLabel="Restore web"
        onClick={() => {}}
      />,
    );
    expect(el.querySelector('.hv-chip__swatch')).not.toBeNull();
    expect(el.style.getPropertyValue('--chip-color')).toBe('#0af');
  });

  // A project chip has no session state of its own; the bell it carries is
  // the union of its sessions' (patterns.md › Attention bubbling) and rides
  // the same data-state channel.
  it('carries a bubbled project bell on data-state', () => {
    const el = make(
      <Chip
        label="hive"
        ariaLabel="Restore hive"
        attention
        onClick={() => {}}
      />,
    );
    expect(el.dataset.state).toBe('attention');
    expect(el.querySelector('.hv-chip__swatch')).not.toBeNull();
  });

  it('renders the restore button only when onRestore is given, and it does not also fire onClick', () => {
    const onClick = vi.fn();
    const onRestore = vi.fn();
    expect(
      make(<Chip label="a" ariaLabel="a" onClick={onClick} />).querySelector(
        '.hv-chip__restore',
      ),
    ).toBeNull();
    const el = make(
      <Chip
        label="a"
        ariaLabel="Restore a"
        onClick={onClick}
        onRestore={onRestore}
        restoreLabel="Restore a"
      />,
    );
    el.querySelector<HTMLButtonElement>('.hv-chip__restore')?.click();
    expect(onRestore).toHaveBeenCalledTimes(1);
    expect(onClick).not.toHaveBeenCalled();
  });

  it('renders the sublabel when given and omits the node when not', () => {
    expect(
      make(
        <Chip label="api" sublabel="hive" ariaLabel="a" onClick={() => {}} />,
      ).querySelector('.hv-chip__sub')?.textContent,
    ).toBe('hive');
    expect(
      make(<Chip label="api" ariaLabel="a" onClick={() => {}} />).querySelector(
        '.hv-chip__sub',
      ),
    ).toBeNull();
  });
});

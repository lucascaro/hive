// @vitest-environment jsdom
//
// The React <Chip>, now the only one: Phase 2 ported the
// minimized-SESSION tray (components/MinimizedTray.tsx) and deleted the
// imperative src/ui/chip.ts along with its ui-chip.test.ts. This file
// carries the whole markup contract for both trays.
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

  // A project chip has no session state of its own; what it carries is the
  // union of its sessions' (patterns.md › Attention bubbling) and rides the
  // same data-state channel. The identity swatch stays — the union is drawn
  // in the alert slot, not in the leading one.
  it('carries a bubbled project bell on data-state, keeping the swatch', () => {
    const el = make(
      <Chip
        label="hive"
        ariaLabel="Restore hive"
        attention={{ count: 2, state: 'attention' }}
        onClick={() => {}}
      />,
    );
    expect(el.dataset.state).toBe('attention');
    expect(el.querySelector('.hv-chip__swatch')).not.toBeNull();
  });

  it('renders a session count when given one', () => {
    const el = make(
      <Chip label="hive" ariaLabel="a" count={3} onClick={() => {}} />,
    );
    expect(el.querySelector('.hv-chip__count')?.textContent).toBe('3');
  });

  it('renders the alert count with a state icon', () => {
    const el = make(
      <Chip
        label="hive"
        ariaLabel="a"
        attention={{ count: 2, state: 'attention' }}
        onClick={() => {}}
      />,
    );
    const alert = el.querySelector('.hv-chip__alert');
    // lastChild, not textContent: StateIcon carries a <title> for the
    // words channel, so the whole slot reads "Waiting for you2".
    expect(alert?.lastChild?.textContent).toBe('2');
    expect(alert?.querySelector('.hv-state-icon')).not.toBeNull();
  });

  // The distinction the state model exists to draw must survive the trip
  // through the chip: folding it back into 'attention' here would throw
  // away the only thing separating a permission prompt from a plain wait.
  it('reports waiting-permission rather than collapsing it to attention', () => {
    const el = make(
      <Chip
        label="hive"
        ariaLabel="a"
        attention={{ count: 1, state: 'waiting-permission' }}
        onClick={() => {}}
      />,
    );
    expect(el.dataset.state).toBe('waiting-permission');
    expect(
      el
        .querySelector('.hv-chip__alert .hv-state-icon')
        ?.getAttribute('data-state'),
    ).toBe('waiting-permission');
  });

  // A session has no sessions and no union: the session tray must not grow
  // either slot, which is what keeps these rules unscoped in chip.css.
  it('renders neither slot for a session chip', () => {
    const el = make(
      <Chip label="s1" ariaLabel="a" state="working" onClick={() => {}} />,
    );
    expect(el.querySelector('.hv-chip__count')).toBeNull();
    expect(el.querySelector('.hv-chip__alert')).toBeNull();
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

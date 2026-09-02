// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import {
  SessionRow,
  type SessionRowProps,
} from '../../src/components/SessionRow';
import type { SessionInfo } from '../../src/app/state';

const noop = () => {};
const base = {
  onSelect: noop,
  onMinimize: noop,
  onRestore: noop,
  onRestart: noop,
  onKill: noop,
  onWorktrees: noop,
  onColor: noop,
  onDoubleClick: noop,
  nameRef: null,
  onDragStart: noop,
  onDragEnd: noop,
  onDragOver: noop,
  onDrop: noop,
};

function props(
  s: Partial<SessionInfo>,
  over: Partial<SessionRowProps> = {},
): SessionRowProps {
  return {
    session: { id: 's1', name: 'api', ...s } as SessionInfo,
    state: 'running',
    selected: false,
    minimized: false,
    index: null,
    ...base,
    ...over,
  };
}

// The <li> React renders, so every assertion below reads the same node the
// imperative sessionRow() used to hand back.
function row(s: Partial<SessionInfo>, over: Partial<SessionRowProps> = {}) {
  const r = render(<SessionRow {...props(s, over)} />, {
    // A row is an <li>; give it a list to live in so the markup is valid.
    container: document.body.appendChild(document.createElement('ul')),
  });
  const el = r.container.querySelector<HTMLLIElement>('.hv-session-row');
  if (!el) throw new Error('no row rendered');
  return { el, rerender: r.rerender, r };
}

describe('SessionRow', () => {
  it('renders name on line 1 and the window title on line 2', () => {
    const { el } = row({ title: 'npm run build' });
    expect(el.dataset.sid).toBe('s1');
    expect(el.querySelector('.hv-session-row__name')?.textContent).toBe('api');
    expect(el.querySelector('.hv-session-row__sub')?.textContent).toBe(
      'npm run build',
    );
  });

  it('falls back to state words when there is no window title', () => {
    expect(
      row({}, { state: 'starting' }).el.querySelector('.hv-session-row__sub')
        ?.textContent,
    ).toBe('Starting…');
    expect(
      row({ alive: false }, { state: 'exited' }).el.querySelector(
        '.hv-session-row__sub',
      )?.textContent,
    ).toBe('Exited');
    expect(
      row(
        { alive: false, last_error: 'boom' },
        { state: 'error' },
      ).el.querySelector('.hv-session-row__sub')?.textContent,
    ).toBe('Exited — boom');
  });

  // sessionState() resolves a teardown to 'starting' (neither phase is
  // `ready`), which is right for the icon and wrong for the words: a
  // session being killed used to say "Starting…" for the whole worktree
  // removal. Fixed in the display layer only.
  it('says Closing while a session is being torn down', () => {
    for (const phase of ['checking', 'closing']) {
      expect(
        row({ phase }, { state: 'starting' }).el.querySelector(
          '.hv-session-row__sub',
        )?.textContent,
      ).toBe('Closing…');
    }
    // A real window title still wins over any state word.
    expect(
      row({ phase: 'closing', title: 'npm run build' }).el.querySelector(
        '.hv-session-row__sub',
      )?.textContent,
    ).toBe('npm run build');
  });

  it('suppresses a title equal to the name (displayTitle rule)', () => {
    expect(
      row({ title: 'api' }, { state: 'running' }).el.querySelector(
        '.hv-session-row__sub',
      )?.textContent,
    ).toBe('');
  });

  it('exposes selection and state as data attributes, never as ad-hoc classes', () => {
    const { el } = row({}, { selected: true, state: 'attention' });
    expect(el.dataset.selected).toBe('');
    expect(el.dataset.state).toBe('attention');
    expect(el.className).toBe('hv-session-row');
  });

  it('renders the key hint for the first nine rows only', () => {
    expect(row({}, { index: 3 }).el.querySelector('.hv-kbd')?.textContent).toBe(
      '[3]',
    );
    expect(row({}, { index: null }).el.querySelector('.hv-kbd')).toBeNull();
  });

  it('renders the worktree icon and agent code in the meta column', () => {
    const { el } = row({ worktree_branch: 'feat/x', agent: 'codex' });
    expect(el.querySelector('.hv-session-row__worktree')).not.toBeNull();
    expect(el.querySelector('.hv-session-row__agent')?.textContent).toBe('co');
    expect(
      row({ agent: 'claude' }).el.querySelector('.hv-session-row__agent')
        ?.textContent,
    ).toBe('cl');
    expect(row({}).el.querySelector('.hv-session-row__worktree')).toBeNull();
  });

  it('shows restart only for exited/error rows and always shows kill + minimize', () => {
    const { el: live } = row({});
    expect(live.querySelector('[data-action="restart"]')).toBeNull();
    expect(live.querySelector('[data-action="kill"]')).not.toBeNull();
    expect(live.querySelector('[data-action="minimize"]')).not.toBeNull();
    expect(
      row({ alive: false }, { state: 'exited' }).el.querySelector(
        '[data-action="restart"]',
      ),
    ).not.toBeNull();
  });

  it('wires the actions, and none of them also selects the row', () => {
    const onSelect = vi.fn();
    const onMinimize = vi.fn();
    const onKill = vi.fn();
    const { el } = row({}, { onSelect, onMinimize, onKill });
    el.querySelector<HTMLButtonElement>('[data-action="minimize"]')?.click();
    el.querySelector<HTMLButtonElement>('[data-action="kill"]')?.click();
    expect(onMinimize).toHaveBeenCalledTimes(1);
    expect(onKill).toHaveBeenCalledTimes(1);
    expect(onSelect).not.toHaveBeenCalled();
    el.click();
    expect(onSelect).toHaveBeenCalledTimes(1);
  });

  // The swatch opens the native colour picker; clicking it must not also
  // switch sessions.
  it('does not select the row when the colour swatch is clicked', () => {
    const onSelect = vi.fn();
    const { el } = row({}, { onSelect });
    el.querySelector<HTMLElement>('.hv-session-row__swatch')?.click();
    el.querySelector<HTMLInputElement>(
      '.hv-session-row__swatch input',
    )?.click();
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('minimize flips to restore when the row is minimized', () => {
    const onRestore = vi.fn();
    const { el } = row({}, { minimized: true, onRestore });
    expect(el.dataset.minimized).toBe('');
    el.querySelector<HTMLButtonElement>('[data-action="minimize"]')?.click();
    expect(onRestore).toHaveBeenCalledTimes(1);
  });

  // What updateSessionRow() used to be: a re-render with new props keeps
  // the existing nodes instead of rebuilding them. That is the whole point
  // of the migration — a replaced <li> eats the dblclick pair that starts
  // an inline rename.
  it('re-renders state, title and hint in place without replacing nodes', () => {
    const { el, rerender } = row({ title: 'a' });
    const name = el.querySelector('.hv-session-row__name');
    rerender(
      <SessionRow
        {...props(
          { title: 'b' },
          { state: 'attention', selected: true, index: 1 },
        )}
      />,
    );
    expect(el.querySelector('.hv-session-row__name')).toBe(name);
    expect(el.querySelector('.hv-session-row__sub')?.textContent).toBe('b');
    expect(el.dataset.state).toBe('attention');
    expect(el.dataset.selected).toBe('');
    expect(el.querySelector('.hv-kbd')?.textContent).toBe('[1]');
    rerender(<SessionRow {...props({ title: 'b' })} />);
    expect(el.dataset.selected).toBeUndefined();
    expect(el.querySelector('.hv-kbd')).toBeNull();
  });

  it('grows a restart button when a running row exits', () => {
    const { el, rerender } = row({});
    expect(el.querySelector('[data-action="restart"]')).toBeNull();
    rerender(<SessionRow {...props({ alive: false }, { state: 'exited' })} />);
    expect(el.querySelector('[data-action="restart"]')).not.toBeNull();
  });

  it('drops the restart button when an exited row is restarted', () => {
    const { el, rerender } = row({ alive: false }, { state: 'exited' });
    expect(el.querySelector('[data-action="restart"]')).not.toBeNull();
    rerender(<SessionRow {...props({})} />);
    expect(el.querySelector('[data-action="restart"]')).toBeNull();
  });

  it('re-renders --session-color and the swatch input on a colour change', () => {
    const { el, rerender } = row({ color: '#ff0000' });
    expect(el.style.getPropertyValue('--session-color')).toBe('#ff0000');
    expect(
      el.querySelector<HTMLInputElement>('.hv-session-row__swatch input')
        ?.value,
    ).toBe('#ff0000');
    rerender(<SessionRow {...props({ color: '#00ff00' })} />);
    expect(el.style.getPropertyValue('--session-color')).toBe('#00ff00');
    expect(
      el.querySelector<HTMLInputElement>('.hv-session-row__swatch input')
        ?.value,
    ).toBe('#00ff00');
  });

  // The picker is uncontrolled on purpose: a controlled `value` would snap
  // the swatch back to the stored colour on every unrelated re-render,
  // while the user is still dragging inside the native picker.
  it('keeps a mid-edit swatch value across an unrelated re-render', () => {
    const onColor = vi.fn();
    const { el, rerender } = row({ color: '#ff0000' }, { onColor });
    const input = el.querySelector<HTMLInputElement>(
      '.hv-session-row__swatch input',
    );
    if (!input) throw new Error('no colour input');
    fireEvent.input(input, { target: { value: '#123456' } });
    expect(onColor).toHaveBeenCalledWith('#123456');
    rerender(<SessionRow {...props({ color: '#ff0000' }, { index: 2 })} />);
    expect(input.value).toBe('#123456');
  });
});

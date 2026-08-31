// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { sessionRow, updateSessionRow } from '../../src/ui/session-row';
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
};
const row = (
  s: Partial<SessionInfo>,
  over: Partial<Parameters<typeof sessionRow>[0]> = {},
) =>
  sessionRow({
    session: { id: 's1', name: 'api', ...s } as SessionInfo,
    state: 'running',
    selected: false,
    minimized: false,
    index: null,
    ...base,
    ...over,
  });

describe('sessionRow', () => {
  it('renders name on line 1 and the window title on line 2', () => {
    const el = row({ title: 'npm run build' });
    expect(el.dataset.sid).toBe('s1');
    expect(el.querySelector('.hv-session-row__name')?.textContent).toBe('api');
    expect(el.querySelector('.hv-session-row__sub')?.textContent).toBe(
      'npm run build',
    );
  });

  it('falls back to state words when there is no window title', () => {
    expect(
      row({}, { state: 'starting' }).querySelector('.hv-session-row__sub')
        ?.textContent,
    ).toBe('Starting…');
    expect(
      row({ alive: false }, { state: 'exited' }).querySelector(
        '.hv-session-row__sub',
      )?.textContent,
    ).toBe('Exited');
    expect(
      row(
        { alive: false, last_error: 'boom' },
        { state: 'error' },
      ).querySelector('.hv-session-row__sub')?.textContent,
    ).toBe('Exited — boom');
  });

  it('suppresses a title equal to the name (displayTitle rule)', () => {
    expect(
      row({ title: 'api' }, { state: 'running' }).querySelector(
        '.hv-session-row__sub',
      )?.textContent,
    ).toBe('');
  });

  it('exposes selection and state as data attributes, never as ad-hoc classes', () => {
    const el = row({}, { selected: true, state: 'attention' });
    expect(el.dataset.selected).toBe('');
    expect(el.dataset.state).toBe('attention');
    expect(el.className).toBe('hv-session-row');
  });

  it('renders the key hint for the first nine rows only', () => {
    expect(row({}, { index: 3 }).querySelector('.hv-kbd')?.textContent).toBe(
      '[3]',
    );
    expect(row({}, { index: null }).querySelector('.hv-kbd')).toBeNull();
  });

  it('renders the worktree icon and agent code in the meta column', () => {
    const el = row({ worktree_branch: 'feat/x', agent: 'codex' });
    expect(el.querySelector('.hv-session-row__worktree')).not.toBeNull();
    expect(el.querySelector('.hv-session-row__agent')?.textContent).toBe('co');
    expect(
      row({ agent: 'claude' }).querySelector('.hv-session-row__agent')
        ?.textContent,
    ).toBe('cl');
    expect(row({}).querySelector('.hv-session-row__worktree')).toBeNull();
  });

  it('shows restart only for exited/error rows and always shows kill + minimize', () => {
    const live = row({});
    expect(live.querySelector('[data-action="restart"]')).toBeNull();
    expect(live.querySelector('[data-action="kill"]')).not.toBeNull();
    expect(live.querySelector('[data-action="minimize"]')).not.toBeNull();
    expect(
      row({ alive: false }, { state: 'exited' }).querySelector(
        '[data-action="restart"]',
      ),
    ).not.toBeNull();
  });

  it('wires the actions, and none of them also selects the row', () => {
    const onSelect = vi.fn();
    const onMinimize = vi.fn();
    const onKill = vi.fn();
    const el = row({}, { onSelect, onMinimize, onKill });
    document.body.append(el);
    el.querySelector<HTMLButtonElement>('[data-action="minimize"]')?.click();
    el.querySelector<HTMLButtonElement>('[data-action="kill"]')?.click();
    expect(onMinimize).toHaveBeenCalledTimes(1);
    expect(onKill).toHaveBeenCalledTimes(1);
    expect(onSelect).not.toHaveBeenCalled();
    el.click();
    expect(onSelect).toHaveBeenCalledTimes(1);
    el.remove();
  });

  it('minimize flips to restore when the row is minimized', () => {
    const onRestore = vi.fn();
    const el = row({}, { minimized: true, onRestore });
    expect(el.dataset.minimized).toBe('');
    el.querySelector<HTMLButtonElement>('[data-action="minimize"]')?.click();
    expect(onRestore).toHaveBeenCalledTimes(1);
  });

  it('updateSessionRow patches state, title and hint in place without replacing nodes', () => {
    const el = row({ title: 'a' });
    const name = el.querySelector('.hv-session-row__name');
    updateSessionRow(el, { id: 's1', name: 'api', title: 'b' } as SessionInfo, {
      state: 'attention',
      selected: true,
      minimized: false,
      index: 1,
    });
    expect(el.querySelector('.hv-session-row__name')).toBe(name);
    expect(el.querySelector('.hv-session-row__sub')?.textContent).toBe('b');
    expect(el.dataset.state).toBe('attention');
    expect(el.dataset.selected).toBe('');
    expect(el.querySelector('.hv-kbd')?.textContent).toBe('[1]');
    updateSessionRow(el, { id: 's1', name: 'api', title: 'b' } as SessionInfo, {
      state: 'running',
      selected: false,
      minimized: false,
      index: null,
    });
    expect(el.dataset.selected).toBeUndefined();
    expect(el.querySelector('.hv-kbd')).toBeNull();
  });
});

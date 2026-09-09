// @vitest-environment jsdom
//
// The sidebar's "these sessions share a worktree" cue (spec 384).
//
// Two channels, deliberately: the row carries data-wt-shared, which the CSS
// paints as a bar in the session's own colour (members inherit the colour of
// the worktree they adopt, internal/registry/create.go), and the branch glyph
// carries a count plus the sharing in its accessible label. Colour alone is
// not something every user can read, so the count is not decoration — the
// label assertions below are the ones that would catch its loss.
import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
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
  ideaText: '',
  worktreeShared: 1,
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

function rowOf(p: SessionRowProps): HTMLElement {
  const ul = document.createElement('ul');
  document.body.appendChild(ul);
  render(<SessionRow {...p} />, { container: ul });
  const li = ul.querySelector<HTMLElement>('.hv-session-row');
  if (!li) throw new Error('no row rendered');
  return li;
}

describe('shared-worktree cue', () => {
  it('marks a row whose worktree is shared, and counts the group', () => {
    const li = rowOf(
      props({ worktree_branch: 'feat/x' }, { worktreeShared: 2 }),
    );
    expect(li.hasAttribute('data-wt-shared')).toBe(true);
    expect(
      li.querySelector('.hv-session-row__worktree-count')?.textContent,
    ).toBe('2');
  });

  it('says so in the branch button label, not only in colour', () => {
    const li = rowOf(
      props({ worktree_branch: 'feat/x' }, { worktreeShared: 2 }),
    );
    const btn = li.querySelector('.hv-session-row__worktree');
    expect(btn?.getAttribute('aria-label')).toContain(
      'shared with 1 other session',
    );
  });

  it('pluralises the label past two', () => {
    const li = rowOf(
      props({ worktree_branch: 'feat/x' }, { worktreeShared: 3 }),
    );
    const btn = li.querySelector('.hv-session-row__worktree');
    expect(btn?.getAttribute('aria-label')).toContain(
      'shared with 2 other sessions',
    );
  });

  it('leaves a solo worktree session unmarked', () => {
    const li = rowOf(
      props({ worktree_branch: 'feat/x' }, { worktreeShared: 1 }),
    );
    expect(li.hasAttribute('data-wt-shared')).toBe(false);
    expect(li.querySelector('.hv-session-row__worktree-count')).toBeNull();
    expect(
      li.querySelector('.hv-session-row__worktree')?.getAttribute('aria-label'),
    ).toBe('Worktree: feat/x — manage worktrees');
  });

  it('renders no worktree glyph at all without a worktree', () => {
    const li = rowOf(props({}, { worktreeShared: 1 }));
    expect(li.querySelector('.hv-session-row__worktree')).toBeNull();
    expect(li.hasAttribute('data-wt-shared')).toBe(false);
  });
});

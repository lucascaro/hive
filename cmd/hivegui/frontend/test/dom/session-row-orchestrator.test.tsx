// @vitest-environment jsdom
//
// Spec 389: "may direct other sessions" on a session row. A granted
// session shows a passive glyph. There is deliberately no row button: a
// fourth hover action widens the strip over the row's click target.
import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import {
  SessionRow,
  type SessionRowProps,
} from '../../src/components/SessionRow';
import type { SessionInfo } from '../../src/app/state';

const noop = () => {};

function mount(s: Partial<SessionInfo>, onSelect = noop) {
  const p: SessionRowProps = {
    session: { id: 's1', name: 'api', alive: true, ...s } as SessionInfo,
    state: 'running',
    selected: false,
    inSelection: false,
    minimized: false,
    index: null,
    onSelect,
    onMinimize: noop,
    onRestore: noop,
    onRestart: noop,
    onKill: noop,
    onToggleBell: noop,
    onWorktrees: noop,
    onColor: noop,
    onDoubleClick: noop,
    nameRef: null,
    drag: {
      draggable: true,
      onDragStart: noop,
      onDragEnd: noop,
      onDragOver: noop,
      onDrop: noop,
    },
    ideaText: '',
    worktreeShared: 1,
    titleOnly: false,
  };
  return render(<SessionRow {...p} />, {
    container: document.body.appendChild(document.createElement('ul')),
  }).container;
}

describe('session row orchestrator grant', () => {
  it('shows no glyph until granted', () => {
    const c = mount({});
    expect(c.querySelector('.hv-session-row__orchestrator')).toBeNull();
  });

  it('shows a labelled glyph while granted', () => {
    const c = mount({ orchestrator: true });
    const g = c.querySelector('.hv-session-row__orchestrator');
    expect(g?.getAttribute('aria-label')).toBe('May direct other sessions');
  });
});

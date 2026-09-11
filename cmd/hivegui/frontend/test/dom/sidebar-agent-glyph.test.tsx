// @vitest-environment jsdom
//
// The agent is stated once per row: the glyph. Line 1 used to end in the
// same agent id (internal/agent/names.go builds "adjective-noun <agentID>"),
// so this suite pins both halves — the glyph carries the agent's colour,
// and the name element renders displayName(), not the stored name.
import { describe, it, expect, beforeEach } from 'vitest';
import { render } from '@testing-library/react';
import {
  SessionRow,
  type SessionRowProps,
} from '../../src/components/SessionRow';
import { setAgentColors, resetStore } from '../../src/store/store';
import type { SessionInfo } from '../../src/app/state';

const noop = () => {};

function row(s: Partial<SessionInfo>) {
  const props: SessionRowProps = {
    session: { id: 's1', name: 'api', ...s } as SessionInfo,
    state: 'running',
    selected: false,
    minimized: false,
    index: null,
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
    titleOnly: false,
  };
  const r = render(<SessionRow {...props} />, {
    container: document.body.appendChild(document.createElement('ul')),
  });
  const el = r.container.querySelector<HTMLLIElement>('.hv-session-row');
  if (!el) throw new Error('no row rendered');
  return el;
}

describe('agent glyph', () => {
  beforeEach(() => {
    resetStore({});
  });

  it('tints the glyph with the agent colour from the catalog', () => {
    setAgentColors(new Map([['claude', '#f59e0b']]));
    const glyph = row({
      name: 'rising-shore claude',
      agent: 'claude',
    }).querySelector<HTMLElement>('.hv-session-row__agent');
    expect(glyph?.textContent).toBe('cl');
    expect(glyph?.style.getPropertyValue('--agent-color')).toBe('#f59e0b');
    expect(glyph?.classList.contains('hv-session-row__agent--plain')).toBe(
      false,
    );
  });

  it('renders an unknown agent plain, with no colour of its own', () => {
    setAgentColors(new Map([['claude', '#f59e0b']]));
    const glyph = row({
      name: 'api mytool',
      agent: 'mytool',
    }).querySelector<HTMLElement>('.hv-session-row__agent');
    expect(glyph?.textContent).toBe('my');
    expect(glyph?.style.getPropertyValue('--agent-color')).toBe('');
    expect(glyph?.classList.contains('hv-session-row__agent--plain')).toBe(
      true,
    );
  });

  it('drops the agent id the glyph already states from line 1', () => {
    const el = row({ name: 'rising-shore claude', agent: 'claude' });
    expect(el.querySelector('.hv-session-row__name')?.textContent).toBe(
      'rising-shore',
    );
  });
});

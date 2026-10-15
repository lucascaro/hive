// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// Spec 338: the "notify me when idle" bell on a session row. An armed
// session shows a passive glyph; the hover action toggles it; a session
// with no process offers no toggle (arming it could never fire).
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import {
  SessionRow,
  type SessionRowProps,
} from '../../src/components/SessionRow';
import type { SessionInfo } from '../../src/app/state';

const noop = () => {};

function mount(s: Partial<SessionInfo>, onToggleBell = noop, onSelect = noop) {
  const p: SessionRowProps = {
    session: { id: 's1', name: 'api', alive: true, ...s } as SessionInfo,
    state: 'running',
    selected: false,
    minimized: false,
    index: null,
    onSelect,
    onMinimize: noop,
    onRestore: noop,
    onRestart: noop,
    onKill: noop,
    onToggleBell,
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

describe('session row bell', () => {
  it('shows no glyph until armed', () => {
    const c = mount({});
    expect(c.querySelector('.hv-session-row__bell')).toBeNull();
  });

  it('shows a labelled glyph while armed', () => {
    const c = mount({ notify_when_idle: true });
    const g = c.querySelector('.hv-session-row__bell');
    expect(g).not.toBeNull();
    expect(g?.getAttribute('aria-label')).toMatch(/notify/i);
  });

  it('the action button toggles, without selecting the row', () => {
    const onToggle = vi.fn();
    const onSelect = vi.fn();
    const c = mount({}, onToggle, onSelect);
    const btn = c.querySelector<HTMLButtonElement>(
      '[data-action="notify-when-idle"]',
    );
    expect(btn?.getAttribute('aria-label')).toBe('Notify me when api is idle');
    fireEvent.click(btn as HTMLButtonElement);
    expect(onToggle).toHaveBeenCalledTimes(1);
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('the label flips once armed', () => {
    const c = mount({ notify_when_idle: true });
    const btn = c.querySelector('[data-action="notify-when-idle"]');
    expect(btn?.getAttribute('aria-label')).toBe(
      'Stop notifying when api is idle',
    );
  });

  it('offers no toggle on a session with no process', () => {
    const c = mount({ alive: false });
    expect(c.querySelector('[data-action="notify-when-idle"]')).toBeNull();
  });
});

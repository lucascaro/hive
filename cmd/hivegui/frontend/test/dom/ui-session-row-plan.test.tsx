// @vitest-environment jsdom
import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import {
  SessionRow,
  type SessionRowProps,
} from '../../src/components/SessionRow';
import type { SessionInfo } from '../../src/app/state';

// The plan indicator's DOM contract: when it exists, what it announces,
// and what it reads off SessionInfo. Geometry — that it changes no row
// height and does not overlap the state icon — is CSS, so it lives in
// test/e2e/sidebar-plan-pie.spec.ts instead; jsdom computes no layout
// and would pass whatever it was given.

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
  titleOnly: false,
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

function row(s: Partial<SessionInfo>, over: Partial<SessionRowProps> = {}) {
  const r = render(<SessionRow {...props(s, over)} />, {
    container: document.body.appendChild(document.createElement('ul')),
  });
  const el = r.container.querySelector<HTMLLIElement>('.hv-session-row');
  if (!el) throw new Error('no row rendered');
  return el;
}

const plan = (el: HTMLElement) =>
  el.querySelector<HTMLElement>('.hv-session-row__plan');

describe('SessionRow plan indicator', () => {
  it('is absent with no plan, so a row renders exactly as it did before', () => {
    expect(plan(row({}))).toBeNull();
  });

  it('is absent when plan_total is zero', () => {
    // The daemon omits the field entirely at zero, but a client must
    // not depend on omitempty to decide whether to draw something.
    expect(plan(row({ plan_done: 0, plan_total: 0 }))).toBeNull();
  });

  it('appears once the session has a plan', () => {
    expect(plan(row({ plan_done: 1, plan_total: 4 }))).not.toBeNull();
  });

  it('puts the completed fraction on --hv-plan-pct', () => {
    const el = plan(row({ plan_done: 1, plan_total: 4 }));
    expect(el?.style.getPropertyValue('--hv-plan-pct')).toBe('25');
  });

  it('reads 0 and 100 at the ends', () => {
    expect(
      plan(row({ plan_done: 0, plan_total: 3 }))?.style.getPropertyValue(
        '--hv-plan-pct',
      ),
    ).toBe('0');
    expect(
      plan(row({ plan_done: 3, plan_total: 3 }))?.style.getPropertyValue(
        '--hv-plan-pct',
      ),
    ).toBe('100');
  });

  it('clamps a done count the daemon should never send', () => {
    // Defensive: plan_done > plan_total would otherwise drive the
    // conic-gradient past 100% and render as a full pie that is lying.
    expect(
      plan(row({ plan_done: 9, plan_total: 3 }))?.style.getPropertyValue(
        '--hv-plan-pct',
      ),
    ).toBe('100');
    expect(
      plan(row({ plan_done: -2, plan_total: 4 }))?.style.getPropertyValue(
        '--hv-plan-pct',
      ),
    ).toBe('0');
  });

  it('announces the fraction to assistive tech', () => {
    // On the hook tier, so the label is the plain fraction — the
    // "not currently reporting" suffix has its own test below.
    const el = plan(row({ plan_done: 2, plan_total: 5, state_source: 'hook' }));
    expect(el?.getAttribute('role')).toBe('img');
    expect(el?.getAttribute('aria-label')).toBe('Plan: 2 of 5 steps done');
  });

  it('names the running tool in the tooltip', () => {
    const el = plan(
      row({
        plan_done: 2,
        plan_total: 5,
        current_tool: 'Bash',
        state_source: 'hook',
      }),
    );
    expect(el?.getAttribute('title')).toBe('2/5 steps · Bash');
    expect(el?.getAttribute('aria-label')).toBe(
      'Plan: 2 of 5 steps done, running Bash',
    );
  });

  it('marks a plan off the reporting tier as not live', () => {
    // state_source back on the heuristic tier means the hook or
    // extension has gone quiet: the plan is still in memory but must
    // not read as current.
    const hooked = plan(
      row({ plan_done: 2, plan_total: 5, state_source: 'hook' }),
    );
    expect(hooked?.className).not.toContain('--stale');

    const stale = plan(
      row({ plan_done: 2, plan_total: 5, state_source: 'heuristic' }),
    );
    expect(stale?.className).toContain('hv-session-row__plan--stale');
    expect(stale?.getAttribute('aria-label')).toBe(
      'Plan: 2 of 5 steps done, not currently reporting',
    );
  });

  it('treats an absent state_source as the heuristic tier', () => {
    // Absent means heuristic on the wire (omitempty), which is also
    // what a daemon too old to send it looks like.
    const el = plan(row({ plan_done: 1, plan_total: 2 }));
    expect(el?.className).toContain('hv-session-row__plan--stale');
  });

  it('leaves the agent code alone', () => {
    // The mark no longer sits behind the agent code, so this is a
    // regression pin rather than a live risk — but it is the assertion
    // that fails if someone moves it back.
    const el = row({ plan_done: 1, plan_total: 2, agent: 'claude' });
    const code = el.querySelector('.hv-session-row__agent');
    expect(code?.textContent).toBe('cl');
  });
});

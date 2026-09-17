// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, cleanup, fireEvent, render } from '@testing-library/react';

// GetActivity is the one bridge call the activity store makes; the
// answer arrives on the event stream, which these tests hand-feed.
vi.mock('../../src/bridge.js', () => ({
  GetActivity: vi.fn(() => Promise.resolve()),
}));

import * as bridge from '../../src/bridge.js';
import { ActivityPanel } from '../../src/components/activity/ActivityPanel.js';
import { ActivityTile } from '../../src/components/activity/ActivityTile.js';
import type { ActivityMsg } from '../../src/lib/activity.js';
import {
  activityStore,
  applyActivityFrame,
  forgetActivity,
  resetActivityOnSessionList,
} from '../../src/store/activity.js';
import { resetStore } from '../../src/store/store.js';
import type { SessionInfo } from '../../src/app/state.js';

const GetActivity = vi.mocked(bridge.GetActivity);
const SID = 's1';
const past = new Date(Date.now() - 120_000).toISOString();
const future = new Date(Date.now() + 120_000).toISOString();

function session(over: Partial<SessionInfo> = {}): SessionInfo {
  return {
    id: SID,
    name: 'api',
    state: 'working',
    state_source: 'hook',
    ...over,
  } as SessionInfo;
}

function frame(m: Partial<ActivityMsg>) {
  act(() => applyActivityFrame({ session_id: SID, ...m }));
}

const flush = () =>
  act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });

const PLAN = [
  { id: 'a', text: 'read the spec', status: 'done' as const, tools: 7 },
  { id: 'b', text: 'write the reducer', status: 'active' as const, tools: 2 },
  { id: 'c', text: 'ship it', status: 'pending' as const },
];
const EVENTS = [
  {
    tool: 'Read',
    target: 'spec.md',
    call_id: 'r1',
    plan_idx: 0,
    started_at: past,
    ended_at: past,
    duration_ms: 20,
    ok: true,
  },
  {
    tool: 'Edit',
    target: 'activity.ts',
    call_id: 'e1',
    plan_idx: 1,
    started_at: past,
    ended_at: past,
    duration_ms: 40,
    ok: false,
  },
];

beforeEach(() => {
  resetStore({ sessions: [session()] });
  activityStore.setState({ byId: new Map() });
  GetActivity.mockReset();
  GetActivity.mockImplementation(() => Promise.resolve());
});
afterEach(cleanup);

describe('ActivityPanel', () => {
  it('expands only the active step; collapsed steps carry the item tally', async () => {
    const { container } = render(<ActivityPanel sessionId={SID} />);
    frame({ full: true, plan: PLAN, events: EVENTS, stale_at: future });
    const steps = container.querySelectorAll('.hv-activity__step');
    expect(steps).toHaveLength(3);
    expect(steps[0]).not.toHaveAttribute('data-open');
    expect(steps[1]).toHaveAttribute('data-open');
    expect(steps[2]).not.toHaveAttribute('data-open');
    // The tally is the item's counter, not the one ring event left.
    expect(steps[0].querySelector('.hv-activity__pill')).toHaveTextContent('7');
    expect(
      steps[1].querySelectorAll('.hv-activity__calls .hv-activity__call'),
    ).toHaveLength(1);
  });

  it('toggles a step with its disclosure row', () => {
    const { container } = render(<ActivityPanel sessionId={SID} />);
    frame({ full: true, plan: PLAN, events: EVENTS });
    const first = () => container.querySelectorAll('.hv-activity__step')[0];
    fireEvent.click(first().querySelector('.hv-activity__step-row') as Element);
    expect(first()).toHaveAttribute('data-open');
    fireEvent.click(first().querySelector('.hv-activity__step-row') as Element);
    expect(first()).not.toHaveAttribute('data-open');
  });

  it('has nothing focusable and cancels mousedown', () => {
    const { container } = render(<ActivityPanel sessionId={SID} />);
    frame({ full: true, plan: PLAN, events: EVENTS });
    expect(
      container.querySelectorAll(
        'button, a[href], input, textarea, select, [tabindex]',
      ),
    ).toHaveLength(0);
    const root = container.querySelector('.hv-activity') as Element;
    expect(fireEvent.mouseDown(root)).toBe(false); // defaultPrevented
  });

  it('shows the timeline newest first with the failure', () => {
    const { container } = render(<ActivityPanel sessionId={SID} />);
    frame({ full: true, plan: PLAN, events: EVENTS });
    const rows = container.querySelectorAll(
      '.hv-activity__timeline .hv-activity__call',
    );
    expect(rows[0]).toHaveTextContent('Edit');
    expect(rows[0]).toHaveAttribute('data-failed');
  });

  it('shows the empty state for a heuristic session with no data', () => {
    resetStore({ sessions: [session({ state_source: '' })] });
    const { container } = render(<ActivityPanel sessionId={SID} />);
    frame({ full: true, plan: [], events: [] });
    expect(container.querySelector('.hv-activity__empty')).not.toBeNull();
    expect(container.querySelector('.hv-activity__step')).toBeNull();
  });

  it('renders stale with an age while working past stale_at', () => {
    const { container } = render(<ActivityPanel sessionId={SID} />);
    frame({ full: true, plan: PLAN, events: EVENTS, stale_at: past });
    expect(container.querySelector('.hv-activity')).toHaveAttribute(
      'data-stale',
    );
    expect(container.querySelector('.hv-activity__stale')?.textContent).toMatch(
      /Stale for \d+m/,
    );
  });

  it('is not stale at rest, however old stale_at is', () => {
    resetStore({ sessions: [session({ state: 'waiting_input' })] });
    const { container } = render(<ActivityPanel sessionId={SID} />);
    frame({ full: true, plan: PLAN, events: EVENTS, stale_at: past });
    expect(container.querySelector('.hv-activity')).not.toHaveAttribute(
      'data-stale',
    );
  });
});

describe('ActivityTile', () => {
  it('draws one pip per plan item and the feed', () => {
    const { container } = render(<ActivityTile sessionId={SID} />);
    frame({ full: true, plan: PLAN, events: EVENTS });
    expect(container.querySelectorAll('.hv-activity__pip')).toHaveLength(3);
    expect(container.querySelectorAll('.hv-activity__call')).toHaveLength(2);
  });

  // Spec 428: the tasks are the tile's body. The heights they get are a
  // Playwright claim (test/e2e/activity-grid.spec.ts) — this is markup.
  it('renders every task, with the current one marked', () => {
    const { container } = render(<ActivityTile sessionId={SID} />);
    frame({ full: true, plan: PLAN, events: EVENTS });
    const texts = Array.from(
      container.querySelectorAll('.hv-activity__step-text'),
    ).map((el) => el.textContent);
    expect(texts).toEqual(['read the spec', 'write the reducer', 'ship it']);
    // The row wrapper activity.css selects on as a direct child.
    expect(
      container.querySelector(
        '.hv-activity__step[data-status="active"] > .hv-activity__step-row',
      ),
    ).not.toBeNull();
    // The pips are decorative beside it; the list carries the name.
    expect(container.querySelector('.hv-activity__pips')).toHaveAttribute(
      'aria-hidden',
      'true',
    );
    expect(container.querySelector('.hv-activity__plan')).toHaveAttribute(
      'aria-label',
      'Plan: 1 of 3 done',
    );
  });

  it('renders no task list for a session with no plan', () => {
    const { container } = render(<ActivityTile sessionId={SID} />);
    frame({ full: true, plan: [], events: EVENTS });
    expect(container.querySelector('.hv-activity__plan')).toBeNull();
    expect(container.querySelectorAll('.hv-activity__call')).toHaveLength(2);
  });
});

describe('snapshot requests', () => {
  it('asks once per session on mount, and not again once loaded', async () => {
    const { rerender } = render(<ActivityPanel sessionId={SID} />);
    await flush();
    expect(GetActivity).toHaveBeenCalledTimes(1);
    frame({ full: true, plan: [], events: [] });
    rerender(<ActivityPanel sessionId={SID} />);
    await flush();
    expect(GetActivity).toHaveBeenCalledTimes(1);
  });

  it('does not loop on a rejected request', async () => {
    GetActivity.mockImplementation(() =>
      Promise.reject(new Error('not connected')),
    );
    const { rerender } = render(<ActivityPanel sessionId={SID} />);
    await flush();
    for (let i = 0; i < 5; i++) {
      frame({ events: [{ tool: 'Bash', call_id: `c${i}`, plan_idx: -1 }] });
      rerender(<ActivityPanel sessionId={SID} />);
      await flush();
    }
    expect(GetActivity).toHaveBeenCalledTimes(1);
  });

  it('refetches after the next session list, not while disconnected', async () => {
    render(<ActivityPanel sessionId={SID} />);
    await flush();
    frame({ full: true, plan: [], events: [] });
    // A disconnect alone changes nothing the store watches.
    await flush();
    expect(GetActivity).toHaveBeenCalledTimes(1);
    act(() => resetActivityOnSessionList(new Set([SID])));
    await flush();
    expect(GetActivity).toHaveBeenCalledTimes(2);
  });

  it('retries a failed request after the next session list', async () => {
    GetActivity.mockImplementationOnce(() => Promise.reject(new Error('down')));
    render(<ActivityPanel sessionId={SID} />);
    await flush();
    act(() => resetActivityOnSessionList(new Set([SID])));
    await flush();
    expect(GetActivity).toHaveBeenCalledTimes(2);
  });

  it('drops the answer to a request made before a restart', async () => {
    const { container } = render(<ActivityPanel sessionId={SID} />);
    await flush();
    expect(GetActivity).toHaveBeenCalledTimes(1);
    // The session restarts while that request is still outstanding; the
    // panel asks again for the new run.
    act(() => forgetActivity(SID));
    await flush();
    expect(GetActivity).toHaveBeenCalledTimes(2);
    // Answers come back in request order on the one control connection:
    // the old run's first, then the new run's (empty).
    frame({ full: true, plan: PLAN, events: EVENTS });
    expect(container.querySelector('.hv-activity__step')).toBeNull();
    frame({ full: true, plan: [], events: [] });
    expect(container.querySelector('.hv-activity__step')).toBeNull();
    expect(activityStore.getState().byId.get(SID)?.loaded).toBe(true);
  });

  it('ignores a full frame nobody asked for', () => {
    act(() =>
      applyActivityFrame({
        session_id: SID,
        full: true,
        plan: PLAN,
        events: EVENTS,
      }),
    );
    expect(activityStore.getState().byId.get(SID)?.loaded ?? false).toBe(false);
    expect(
      activityStore.getState().byId.get(SID)?.data.plan ?? [],
    ).toHaveLength(0);
  });
});

describe('load states', () => {
  it('says it is loading until the snapshot arrives', async () => {
    const { container } = render(<ActivityPanel sessionId={SID} />);
    await flush();
    expect(container.querySelector('.hv-activity__none')).toHaveTextContent(
      'Loading',
    );
    frame({ full: true, plan: [], events: [] });
    expect(container.querySelector('.hv-activity__none')).toHaveTextContent(
      'No tool calls yet',
    );
  });

  it('says the load failed, rather than showing an empty session', async () => {
    GetActivity.mockImplementation(() => Promise.reject(new Error('down')));
    const panel = render(<ActivityPanel sessionId={SID} />);
    await flush();
    expect(
      panel.container.querySelector('.hv-activity__none'),
    ).toHaveTextContent("Couldn't load activity");
    panel.unmount();
    const tile = render(<ActivityTile sessionId={SID} />);
    expect(
      tile.container.querySelector('.hv-activity__none'),
    ).toHaveTextContent("Couldn't load activity");
  });

  it('a failed load on a heuristic session is not the no-activity empty state', async () => {
    resetStore({ sessions: [session({ state_source: '' })] });
    GetActivity.mockImplementation(() => Promise.reject(new Error('down')));
    const { container } = render(<ActivityPanel sessionId={SID} />);
    await flush();
    expect(container.querySelector('.hv-activity__empty')).toBeNull();
    expect(container.querySelector('.hv-activity__none')).toHaveTextContent(
      "Couldn't load activity",
    );
  });

  it('never asks for a session that is not in the session list', async () => {
    resetStore({ sessions: [] });
    render(<ActivityPanel sessionId={SID} />);
    await flush();
    expect(GetActivity).not.toHaveBeenCalled();
  });
});

import { describe, it, expect } from 'vitest';
import {
  ACTIVITY_OPEN_CAP,
  ACTIVITY_RING_CAP,
  applyActivity,
  emptyActivity,
  formatAge,
  formatDuration,
  groupTimeline,
  isStale,
  type ActivityMsg,
  type PlanItem,
  type SessionActivity,
  type ToolEvent,
} from '../../src/lib/activity.js';

const SID = 's1';
const t = (sec: number) =>
  new Date(Date.UTC(2026, 8, 16, 12, 0, sec)).toISOString();

function msg(m: Partial<ActivityMsg>): ActivityMsg {
  return { session_id: SID, ...m };
}
function startEv(
  id: string,
  sec: number,
  extra: Partial<ToolEvent> = {},
): ToolEvent {
  return {
    tool: 'Bash',
    target: id,
    call_id: id,
    started_at: t(sec),
    plan_idx: -1,
    ...extra,
  };
}
function endEv(
  id: string,
  sec: number,
  extra: Partial<ToolEvent> = {},
): ToolEvent {
  return {
    ...startEv(id, sec - 1),
    ended_at: t(sec),
    duration_ms: 1000,
    ok: true,
    ...extra,
  };
}
function run(...msgs: ActivityMsg[]): SessionActivity {
  return msgs.reduce(applyActivity, emptyActivity());
}
const plan = (...texts: string[]): PlanItem[] =>
  texts.map((text, i) => ({
    id: String(i),
    text,
    status: i === 0 ? 'active' : 'pending',
  }));

describe('applyActivity events', () => {
  it('pairs a start delta with its end delta into one completed call', () => {
    const a = run(
      msg({ events: [startEv('c1', 1)] }),
      msg({ events: [endEv('c1', 2)] }),
    );
    expect(a.events).toHaveLength(1);
    expect(a.events[0].ok).toBe(true);
  });

  it('keeps a still-open call when a full snapshot (which never holds open calls) arrives', () => {
    const a = run(
      msg({ events: [startEv('c1', 1)] }),
      msg({ full: true, events: [endEv('c0', 0)], plan: [] }),
    );
    expect(a.events.map((e) => e.call_id)).toEqual(['c0', 'c1']);
  });

  it('completes an open call from a full snapshot, then from its end delta', () => {
    const a = run(
      msg({ full: true, events: [], plan: [] }),
      msg({ events: [startEv('c1', 1)] }),
      msg({ events: [endEv('c1', 2)] }),
    );
    expect(a.events).toHaveLength(1);
    expect(a.events[0].ended_at).toBe(t(2));
  });

  it('keeps a completed call whose end delta raced ahead of a snapshot without it', () => {
    // The snapshot was taken before the end was accepted, so its stale_at
    // is older than the delta's.
    const a = run(
      msg({ events: [startEv('c1', 1)], stale_at: t(31) }),
      msg({ events: [endEv('c1', 2)], stale_at: t(32) }),
      msg({ full: true, events: [], plan: [], stale_at: t(31) }),
    );
    expect(a.events).toHaveLength(1);
    expect(a.events[0].ok).toBe(true);
  });

  it('never lets a late start regress a completed call', () => {
    const a = run(
      msg({ events: [endEv('c1', 2)] }),
      msg({ events: [startEv('c1', 1)] }),
    );
    expect(a.events[0].ok).toBe(true);
  });

  it('does not duplicate call-less events on a repeated snapshot', () => {
    const ev = { ...endEv('x', 3), call_id: undefined };
    const snap = msg({ full: true, events: [ev], plan: [] });
    expect(run(snap, snap).events).toHaveLength(1);
  });

  it('evicts the oldest completed calls past the cap, never an open one', () => {
    const open = startEv('open', 0);
    const done = Array.from({ length: ACTIVITY_RING_CAP + 5 }, (_, i) =>
      endEv(`d${i}`, i + 1),
    );
    const a = run(
      msg({ events: [open] }),
      msg({ full: true, events: done, plan: [] }),
    );
    expect(a.events.filter((e) => e.ok !== undefined)).toHaveLength(
      ACTIVITY_RING_CAP,
    );
    expect(a.events.some((e) => e.call_id === 'open')).toBe(true);
    expect(a.events.some((e) => e.call_id === 'd0')).toBe(false);
  });

  it('bounds open calls a dead reporter never ends', () => {
    const starts = Array.from({ length: ACTIVITY_OPEN_CAP + 3 }, (_, i) =>
      msg({ events: [startEv(`o${i}`, i)] }),
    );
    const a = run(...starts);
    expect(a.events).toHaveLength(ACTIVITY_OPEN_CAP);
    expect(a.events[0].call_id).toBe('o3');
  });

  it('orders by the daemon clock, not arrival', () => {
    const a = run(
      msg({ events: [endEv('late', 9)], stale_at: t(40) }),
      msg({
        full: true,
        events: [endEv('early', 2)],
        plan: [],
        stale_at: t(39),
      }),
    );
    expect(a.events.map((e) => e.call_id)).toEqual(['early', 'late']);
  });
});

describe('applyActivity plan and stale_at', () => {
  it('keeps the plan on a delta with plan null or absent', () => {
    const a = run(
      msg({ plan: plan('a') }),
      msg({ plan: null, events: [startEv('c', 1)] }),
      msg({}),
    );
    expect(a.plan).toHaveLength(1);
  });

  it('clears the plan on plan: []', () => {
    expect(run(msg({ plan: plan('a') }), msg({ plan: [] })).plan).toEqual([]);
  });

  it('clears the plan on a full frame with plan null', () => {
    expect(
      run(msg({ plan: plan('a') }), msg({ full: true, plan: null })).plan,
    ).toEqual([]);
  });

  it('keeps stale_at when a frame omits it', () => {
    expect(run(msg({ stale_at: t(30) }), msg({})).staleAt).toBe(t(30));
  });

  it('ignores the plan of a frame older than the stored stale_at', () => {
    const a = run(
      msg({ plan: plan('new'), stale_at: t(40) }),
      msg({ plan: plan('old', 'older'), stale_at: t(35) }),
    );
    expect(a.plan.map((p) => p.text)).toEqual(['new']);
    expect(a.staleAt).toBe(t(40));
  });

  it('orders stale_at below a millisecond (RFC3339Nano)', () => {
    const newer = '2026-09-16T12:00:05.000000900Z';
    const older = '2026-09-16T12:00:05.000000100Z';
    const a = run(
      msg({ plan: plan('new'), stale_at: newer }),
      msg({ plan: plan('old'), stale_at: older }),
    );
    expect(a.plan[0].text).toBe('new');
    expect(a.staleAt).toBe(newer);
  });

  it('orders stale_at across UTC offsets', () => {
    // 12:00:05+02:00 is 10:00:05Z, earlier than 11:00:00.5Z.
    const newer = '2026-09-16T11:00:00.5Z';
    const older = '2026-09-16T12:00:05.000000900+02:00';
    const a = run(
      msg({ plan: plan('new'), stale_at: newer }),
      msg({ plan: plan('old'), stale_at: older }),
    );
    expect(a.plan[0].text).toBe('new');
    expect(a.staleAt).toBe(newer);
  });

  it('a current full snapshot replaces events from before it', () => {
    // A restart keeps the session id but starts a fresh ring; its empty
    // snapshot must not leave the previous life's calls on screen.
    const a = run(
      msg({ events: [endEv('old', 2)], stale_at: t(30) }),
      msg({ full: true, events: [], plan: [], stale_at: t(40) }),
    );
    expect(a.events).toEqual([]);
  });

  it('compares stale_at as instants, not strings', () => {
    // "…05.5Z" sorts before "…05Z" as a string but is later.
    const later = '2026-09-16T12:00:05.5Z';
    const earlier = '2026-09-16T12:00:05Z';
    const a = run(
      msg({ plan: plan('a'), stale_at: earlier }),
      msg({ plan: plan('b'), stale_at: later }),
    );
    expect(a.plan[0].text).toBe('b');
  });
});

describe('isStale', () => {
  const past = t(0);
  const now = Date.parse(t(60));
  it('is stale on the heuristic tier', () => {
    expect(isStale('working', '', '', now)).toBe(true);
    expect(isStale('idle', undefined, '', now)).toBe(true);
  });
  it('is stale while working past stale_at', () => {
    expect(isStale('working', 'hook', past, now)).toBe(true);
  });
  it('is never stale at rest, however old stale_at is', () => {
    expect(isStale('waiting_input', 'hook', past, now)).toBe(false);
    expect(isStale('idle', 'extension', past, now)).toBe(false);
  });
  it('is fresh while working before stale_at', () => {
    expect(isStale('working', 'hook', t(90), now)).toBe(false);
  });
});

describe('groupTimeline', () => {
  it('buckets calls per step, subagents by agent id, and unplanned apart', () => {
    const p: PlanItem[] = [
      { id: 'a', text: 'one', status: 'done', tools: 7 },
      { id: 'b', text: 'two', status: 'active', tools: 2 },
    ];
    const g = groupTimeline(p, [
      endEv('m0', 1, { plan_idx: 0 }),
      endEv('m1', 2, { plan_idx: 1 }),
      endEv('s1', 3, { plan_idx: 1, agent_id: 'ag', agent_type: 'Explore' }),
      endEv('s2', 4, { plan_idx: 1, agent_id: 'ag', agent_type: 'Explore' }),
      endEv('u', 5),
    ]);
    expect(g.steps).toHaveLength(2);
    expect(g.steps[0].main.map((e) => e.call_id)).toEqual(['m0']);
    // The tally is the item's own counter, not what survived the ring.
    expect(g.steps[0].tools).toBe(7);
    expect(g.steps[1].subagents).toEqual([
      {
        agentId: 'ag',
        agentType: 'Explore',
        calls: [
          expect.objectContaining({ call_id: 's1' }),
          expect.objectContaining({ call_id: 's2' }),
        ],
      },
    ]);
    expect(g.unplanned.main.map((e) => e.call_id)).toEqual(['u']);
  });
  it('puts an out-of-range plan_idx with the unplanned calls', () => {
    expect(
      groupTimeline(plan('a'), [endEv('x', 1, { plan_idx: 5 })]).unplanned.main,
    ).toHaveLength(1);
  });
});

describe('formatting', () => {
  it('formats ages', () => {
    expect(formatAge(4_000)).toBe('4s');
    expect(formatAge(125_000)).toBe('2m');
    expect(formatAge(2 * 3600_000 + 5)).toBe('2h');
    expect(formatAge(-5)).toBe('0s');
  });
  it('formats durations', () => {
    expect(formatDuration(850)).toBe('850ms');
    expect(formatDuration(2_340)).toBe('2.3s');
    expect(formatDuration(64_000)).toBe('1m 4s');
  });
});

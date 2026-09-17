// Agent activity (spec 416): the client half of the daemon's per-session
// tool ring and plan. Pure — the store in store/activity.ts holds the
// result, the renderers in components/activity/ read it.
//
// Wire types mirror internal/wire/control.go (ToolEvent, PlanItem,
// ActivityMsg). Daemon-owned and snake_case only, like SessionInfo's
// state fields.

export interface ToolEvent {
  tool: string;
  target?: string;
  call_id?: string;
  agent_id?: string;
  agent_type?: string;
  // RFC3339Nano, daemon clock. started_at is absent for an end that never
  // paired with a start.
  started_at?: string;
  ended_at?: string;
  duration_ms?: number;
  // Absent while the call is running.
  ok?: boolean;
  // Index of the plan step active when the call started; -1 for none.
  plan_idx: number;
}

export interface PlanItem {
  id?: string;
  text: string;
  status: 'pending' | 'active' | 'done';
  // Calls this step ran. Kept on the item by the daemon, so it stays
  // right after the ring has evicted them.
  tools?: number;
}

export interface ActivityMsg {
  session_id: string;
  // The whole ring when full, otherwise the one changed event.
  events?: ToolEvent[] | null;
  // null or absent: unchanged. []: emptied. Always meaningful when full.
  plan?: PlanItem[] | null;
  full?: boolean;
  // Daemon-clock instant past which a WORKING session's tier is silent.
  // Monotonic per session, so it also orders frames.
  stale_at?: string;
}

export interface SessionActivity {
  events: ToolEvent[];
  plan: PlanItem[];
  staleAt: string;
}

// Mirrors agentstate.ActivityRingCap / activityOpenCap. The client holds
// the same bound of completed calls, plus the running ones the daemon
// keeps outside its ring.
export const ACTIVITY_RING_CAP = 200;
export const ACTIVITY_OPEN_CAP = 32;

export function emptyActivity(): SessionActivity {
  return { events: [], plan: [], staleAt: '' };
}

const isDone = (e: ToolEvent) => e.ok !== undefined || e.ended_at !== undefined;

// A call is identified by its call_id. The few without one (an end that
// never paired) are identified by their content, so a second snapshot
// does not duplicate them.
function keyOf(e: ToolEvent): string {
  return e.call_id
    ? `id:${e.call_id}`
    : `ev:${e.tool}|${e.target ?? ''}|${e.started_at ?? ''}|${e.ended_at ?? ''}`;
}

function instant(s: string | undefined): number {
  const n = s ? Date.parse(s) : Number.NaN;
  return Number.isNaN(n) ? 0 : n;
}

const whenOf = (e: ToolEvent) => instant(e.started_at ?? e.ended_at);

// applyActivity folds one ACTIVITY frame into a session's state.
//
// A GET_ACTIVITY answer and the deltas are written by different daemon
// goroutines, so they arrive in either order. Two rules make that safe:
// events merge as a union by call — a snapshot adds and completes calls
// but never removes one the client already holds (it may be an end that
// raced ahead of the snapshot) — and a frame whose stale_at is older than
// the one already stored cannot replace the plan or stale_at.
export function applyActivity(
  prev: SessionActivity,
  msg: ActivityMsg,
): SessionActivity {
  const older =
    !!msg.stale_at &&
    !!prev.staleAt &&
    instant(msg.stale_at) < instant(prev.staleAt);

  let events = prev.events;
  if (msg.events && msg.events.length > 0) {
    const byKey = new Map(prev.events.map((e) => [keyOf(e), e]));
    for (const ev of msg.events) {
      const k = keyOf(ev);
      const had = byKey.get(k);
      // A late start never regresses a finished call.
      if (had && isDone(had) && !isDone(ev)) continue;
      byKey.set(k, ev);
    }
    events = cap([...byKey.values()].sort((a, b) => whenOf(a) - whenOf(b)));
  }

  let plan = prev.plan;
  if (!older) {
    if (msg.full) plan = msg.plan ?? [];
    else if (Array.isArray(msg.plan)) plan = msg.plan;
  }
  const staleAt = msg.stale_at && !older ? msg.stale_at : prev.staleAt;

  if (events === prev.events && plan === prev.plan && staleAt === prev.staleAt)
    return prev;
  return { events, plan, staleAt };
}

// Drops the oldest completed calls past the ring cap and the oldest open
// ones past the open cap. Input is sorted oldest first.
function cap(events: ToolEvent[]): ToolEvent[] {
  let done = events.filter(isDone).length - ACTIVITY_RING_CAP;
  let open = events.length - events.filter(isDone).length - ACTIVITY_OPEN_CAP;
  if (done <= 0 && open <= 0) return events;
  return events.filter((e) => {
    if (isDone(e)) return done-- <= 0;
    return open-- <= 0;
  });
}

// isStale: the heuristic tier has nothing current to show, and a working
// session whose tier has been silent past stale_at is not reporting. A
// session at rest is never stale — no reporter heartbeats, so silence at
// rest is what a healthy tier looks like.
export function isStale(
  state: string | undefined,
  source: string | undefined,
  staleAt: string,
  now: number,
): boolean {
  if (!source || source === 'heuristic') return true;
  return state === 'working' && !!staleAt && now > instant(staleAt);
}

// How long the tier has been silent past its deadline, for the age label.
export function staleForMs(staleAt: string, now: number): number {
  return staleAt ? now - instant(staleAt) : 0;
}

export interface SubagentCalls {
  agentId: string;
  agentType: string;
  calls: ToolEvent[];
}

export interface CallGroup {
  main: ToolEvent[];
  subagents: SubagentCalls[];
}

export interface StepGroup extends CallGroup {
  item: PlanItem;
  index: number;
  tools: number;
}

// groupTimeline nests calls under the plan step they ran in. Subagent
// calls group by agent id within the step: nothing on the wire links a
// subagent to the parent call that spawned it.
export function groupTimeline(
  plan: PlanItem[],
  events: ToolEvent[],
): { steps: StepGroup[]; unplanned: CallGroup } {
  const groups: CallGroup[] = plan.map(() => ({ main: [], subagents: [] }));
  const unplanned: CallGroup = { main: [], subagents: [] };
  for (const ev of events) {
    const g =
      ev.plan_idx >= 0 && ev.plan_idx < plan.length
        ? groups[ev.plan_idx]
        : unplanned;
    if (!ev.agent_id) {
      g.main.push(ev);
      continue;
    }
    let sub = g.subagents.find((s) => s.agentId === ev.agent_id);
    if (!sub) {
      sub = { agentId: ev.agent_id, agentType: ev.agent_type ?? '', calls: [] };
      g.subagents.push(sub);
    }
    sub.calls.push(ev);
  }
  return {
    steps: plan.map((item, index) => ({
      item,
      index,
      tools: item.tools ?? 0,
      ...groups[index],
    })),
    unplanned,
  };
}

export function formatAge(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  return `${Math.floor(s / 3600)}h`;
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  return `${Math.floor(ms / 60_000)}m ${Math.floor((ms % 60_000) / 1000)}s`;
}

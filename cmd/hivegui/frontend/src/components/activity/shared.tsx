// Pieces the activity renderers share: what a session's activity looks
// like right now (data, staleness, empty) and one tool-call row.
import type { MouseEvent, ReactNode } from 'react';

import type { SessionInfo } from '../../app/state.js';
import {
  formatAge,
  formatDuration,
  isStale,
  staleForMs,
  type SessionActivity,
  type ToolEvent,
} from '../../lib/activity.js';
import {
  type ActivityLoad,
  useSessionActivity,
  useNow,
} from '../../store/activity.js';
import { useAppStore } from '../../store/store.js';

export interface ActivityView {
  info: SessionInfo;
  data: SessionActivity;
  load: ActivityLoad;
  now: number;
  // The tier cannot report activity and nothing was ever reported.
  empty: boolean;
  stale: boolean;
  // Short words for the stale state, or '' when fresh.
  staleText: string;
}

export function useActivityView(sessionId: string): ActivityView | null {
  const info = useAppStore((s) => s.sessions.find((x) => x.id === sessionId));
  const { data, load } = useSessionActivity(sessionId, !!info);
  const now = useNow();
  if (!info) return null;
  const heuristic = !info.state_source || info.state_source === 'heuristic';
  // "No activity data" is a claim about the agent, so it waits for the
  // snapshot: while loading, or after a failed load, nothing is known.
  const empty =
    load === 'loaded' &&
    heuristic &&
    data.events.length === 0 &&
    data.plan.length === 0;
  const stale =
    !empty && isStale(info.state, info.state_source, data.staleAt, now);
  let staleText = '';
  if (stale) {
    staleText = heuristic
      ? 'Not reporting — may be out of date'
      : `Stale for ${formatAge(staleForMs(data.staleAt, now))}`;
  }
  return { info, data, load, now, empty, stale, staleText };
}

// Every renderer root cancels mousedown: the renderers are read-only, and
// a click must never move keyboard focus off the terminal.
export const keepFocus = (e: MouseEvent) => e.preventDefault();

export function ActivityEmpty(): ReactNode {
  return (
    <div className="hv-activity__empty">
      No activity data — this agent doesn't report its plan or tools.
    </div>
  );
}

// The timeline's placeholder when it has no calls to show. A snapshot
// still on its way, or one that failed, is not an empty session.
export function NoCalls({ load }: { load: ActivityLoad }): ReactNode {
  const text =
    load === 'loading'
      ? 'Loading…'
      : load === 'failed'
        ? "Couldn't load activity — it will retry when Hive reconnects"
        : 'No tool calls yet';
  return <li className="hv-activity__none">{text}</li>;
}

// One call: `Tool · target`, then its outcome. A call still open while
// the session is not working never got its end (the turn ended, or the
// reporter died); it is shown without a result rather than as running.
export function CallRow({
  ev,
  working,
  now,
}: {
  ev: ToolEvent;
  working: boolean;
  now: number;
}): ReactNode {
  const running = ev.ok === undefined && ev.ended_at === undefined;
  let outcome: string;
  if (!running) {
    outcome =
      ev.duration_ms !== undefined ? formatDuration(ev.duration_ms) : '';
  } else if (working && ev.started_at) {
    outcome = `running ${formatAge(now - Date.parse(ev.started_at))}`;
  } else {
    outcome = 'no result';
  }
  return (
    <li
      className="hv-activity__call"
      data-running={running && working ? '' : undefined}
      data-failed={ev.ok === false ? '' : undefined}
    >
      <span className="hv-activity__tool">{ev.tool}</span>
      {ev.target ? (
        <span className="hv-activity__target">{ev.target}</span>
      ) : null}
      <span className="hv-activity__outcome">
        {ev.ok === false ? `failed${outcome ? ` · ${outcome}` : ''}` : outcome}
      </span>
    </li>
  );
}

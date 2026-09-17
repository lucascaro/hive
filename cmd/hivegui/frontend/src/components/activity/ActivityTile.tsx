// The activity grid's tile body (spec 416, reshaped by 428): the plan's
// tasks are the body, and the live tool feed takes whatever height they
// leave — down to none. Mounted over the tile's hidden terminal body;
// the tile header stays.
//
// The step row mirrors ActivityPanel's PlanStep (mark + text inside a
// .hv-activity__step-row), minus its chevron, pill and nested calls: the
// activity grid takes no keystrokes, so there is no disclosure to offer.
// Keep the two in step — activity.css styles both.
import { useEffect, useRef, type ReactNode } from 'react';

import type { PlanItem } from '../../lib/activity.js';
import { Icon } from '../Icon.js';
import {
  ActivityEmpty,
  NoCalls,
  CallRow,
  keepFocus,
  useActivityView,
} from './shared.js';

// More than fits any tile; the list clips.
const FEED_MAX = 40;

const keyOf = (p: PlanItem, i: number) => p.id ?? `${i}:${p.text}`;

export function ActivityTile({ sessionId }: { sessionId: string }): ReactNode {
  const view = useActivityView(sessionId);
  const list = useRef<HTMLOListElement>(null);
  const current = useRef<HTMLLIElement>(null);
  const plan = view?.data.plan ?? [];
  const activeAt = plan.findIndex((p) => p.status === 'active');
  // Identity, not position: re-scroll when the agent moves to another step,
  // and leave a scroll the user made alone while it is the same step.
  const activeKey = activeAt < 0 ? '' : keyOf(plan[activeAt], activeAt);
  useEffect(() => {
    const el = list.current;
    const row = current.current;
    if (!activeKey || !el || !row) return;
    // offsetTop is measured from the offsetParent, which the plan list's
    // own `position: relative` makes the list itself (activity.css). Not
    // scrollIntoView: it walks every scrollable ancestor, and the grid's
    // are overflow:hidden with no scrollbar to undo an offset.
    const top = row.offsetTop;
    const bottom = top + row.offsetHeight;
    if (top < el.scrollTop) el.scrollTop = top;
    else if (bottom > el.scrollTop + el.clientHeight)
      el.scrollTop = bottom - el.clientHeight;
  }, [activeKey]);
  if (!view) return null;
  const { info, data, load, now, empty, stale, staleText } = view;
  const working = info.state === 'working';
  const done = data.plan.filter((p) => p.status === 'done').length;
  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: mousedown only keeps focus off the hidden terminal
    <div
      className="hv-activity hv-activity--tile activity-tile"
      data-stale={stale ? '' : undefined}
      onMouseDown={keepFocus}
    >
      {empty ? (
        <ActivityEmpty />
      ) : (
        <>
          {data.plan.length > 0 || staleText ? (
            <div className="hv-activity__head">
              {data.plan.length > 0 ? (
                // Decorative beside the task list below, which carries the
                // plan's accessible name: the shape of the whole plan when
                // the list is scrolled away from its ends.
                <ol className="hv-activity__pips" aria-hidden="true">
                  {data.plan.map((p, i) => (
                    <li
                      key={keyOf(p, i)}
                      className="hv-activity__pip"
                      data-status={p.status}
                    />
                  ))}
                </ol>
              ) : null}
              {staleText ? (
                <span className="hv-activity__stale">{staleText}</span>
              ) : null}
            </div>
          ) : null}
          {data.plan.length > 0 ? (
            <ol
              ref={list}
              className="hv-activity__plan"
              aria-label={`Plan: ${done} of ${data.plan.length} done`}
            >
              {data.plan.map((p, i) => (
                <li
                  key={keyOf(p, i)}
                  ref={i === activeAt ? current : undefined}
                  className="hv-activity__step"
                  data-status={p.status}
                >
                  <div className="hv-activity__step-row">
                    <span className="hv-activity__mark" aria-hidden="true">
                      {p.status === 'done' ? (
                        <Icon name="check" size={12} />
                      ) : null}
                    </span>
                    <span className="hv-activity__step-text" title={p.text}>
                      {p.text}
                    </span>
                  </div>
                </li>
              ))}
            </ol>
          ) : null}
          <ol className="hv-activity__timeline" aria-label="Tool calls">
            {data.events.length === 0 ? (
              <NoCalls load={load} />
            ) : (
              data.events
                .slice(-FEED_MAX)
                .reverse()
                .map((ev, i) => (
                  <CallRow
                    key={ev.call_id ?? `n${i}`}
                    ev={ev}
                    working={working}
                    now={now}
                  />
                ))
            )}
          </ol>
        </>
      )}
    </div>
  );
}

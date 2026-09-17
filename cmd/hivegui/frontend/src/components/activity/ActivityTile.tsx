// The activity grid's tile body (spec 416): the plan's shape as pips,
// then the live tool feed, newest first. Mounted over the tile's hidden
// terminal body; the tile header stays.
import type { ReactNode } from 'react';

import {
  ActivityEmpty,
  CallRow,
  keepFocus,
  useActivityView,
} from './shared.js';

// More than fits any tile; the list clips.
const FEED_MAX = 40;

export function ActivityTile({ sessionId }: { sessionId: string }): ReactNode {
  const view = useActivityView(sessionId);
  if (!view) return null;
  const { info, data, now, empty, stale, staleText } = view;
  const working = info.state === 'working';
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
                <ol
                  className="hv-activity__pips"
                  aria-label={`Plan: ${data.plan.filter((p) => p.status === 'done').length} of ${data.plan.length} done`}
                >
                  {data.plan.map((p, i) => (
                    <li
                      key={p.id ?? i}
                      className="hv-activity__pip"
                      data-status={p.status}
                      title={p.text}
                    />
                  ))}
                </ol>
              ) : null}
              {staleText ? (
                <span className="hv-activity__stale">{staleText}</span>
              ) : null}
            </div>
          ) : null}
          <ol className="hv-activity__timeline" aria-label="Tool calls">
            {data.events.length === 0 ? (
              <li className="hv-activity__none">No tool calls yet</li>
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

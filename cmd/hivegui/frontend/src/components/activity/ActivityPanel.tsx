// The inspector panel (spec 416): beside the terminal in single view.
// Plan steps with their calls nested beneath — only the current step open
// by default — and the whole timeline below, newest first.
//
// Read-only and never focusable: disclosure rows are divs with no
// tabIndex, and the root cancels mousedown, so the terminal keeps the
// keyboard through every click.
import { useState, type ReactNode } from 'react';

import { groupTimeline, type StepGroup } from '../../lib/activity.js';
import { Icon } from '../Icon.js';
import {
  ActivityEmpty,
  NoCalls,
  CallRow,
  keepFocus,
  useActivityView,
} from './shared.js';

export function ActivityPanel({ sessionId }: { sessionId: string }): ReactNode {
  const view = useActivityView(sessionId);
  if (!view) return null;
  const { info, data, load, now, empty, stale, staleText } = view;
  const working = info.state === 'working';
  // Calls outside any step show in the timeline only.
  const { steps } = groupTimeline(data.plan, data.events);
  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: mousedown only keeps focus on the terminal
    <div
      className="hv-activity hv-activity--panel"
      data-stale={stale ? '' : undefined}
      onMouseDown={keepFocus}
    >
      <div className="hv-activity__head">
        <span className="hv-activity__title">Activity</span>
        {staleText ? (
          <span className="hv-activity__stale">{staleText}</span>
        ) : null}
      </div>
      {empty ? (
        <ActivityEmpty />
      ) : (
        <>
          {steps.length > 0 ? (
            <ol className="hv-activity__plan" aria-label="Plan">
              {steps.map((st) => (
                <PlanStep
                  key={st.item.id ?? `${st.index}:${st.item.text}`}
                  step={st}
                  working={working}
                  now={now}
                />
              ))}
            </ol>
          ) : null}
          <div className="hv-activity__section">Timeline</div>
          <ol className="hv-activity__timeline" aria-label="Timeline">
            {data.events.length === 0 ? (
              <NoCalls load={load} />
            ) : (
              [...data.events]
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

// ActivityTile renders the same mark + text row without the chevron, pill
// and nested calls. Keep the two in step — activity.css styles both.
function PlanStep({
  step,
  working,
  now,
}: {
  step: StepGroup;
  working: boolean;
  now: number;
}): ReactNode {
  // null = follow the default (open only while it is the current step);
  // a click pins it either way.
  const [pinned, setPinned] = useState<boolean | null>(null);
  const open = pinned ?? step.item.status === 'active';
  const calls =
    step.main.length + step.subagents.reduce((n, s) => n + s.calls.length, 0);
  return (
    <li
      className="hv-activity__step"
      data-status={step.item.status}
      data-open={open ? '' : undefined}
    >
      {/* biome-ignore lint/a11y/useKeyWithClickEvents: deliberately not focusable — see file header */}
      {/* biome-ignore lint/a11y/noStaticElementInteractions: see above */}
      <div className="hv-activity__step-row" onClick={() => setPinned(!open)}>
        <Icon name={open ? 'chevron-down' : 'chevron-right'} size={12} />
        <span className="hv-activity__mark" aria-hidden="true">
          {step.item.status === 'done' ? <Icon name="check" size={12} /> : null}
        </span>
        <span className="hv-activity__step-text">{step.item.text}</span>
        <span className="hv-activity__pill" title={`${step.tools} tool calls`}>
          {step.tools}
        </span>
      </div>
      {open && calls > 0 ? (
        <ol className="hv-activity__calls">
          {step.main.map((ev, i) => (
            <CallRow
              key={ev.call_id ?? `n${i}`}
              ev={ev}
              working={working}
              now={now}
            />
          ))}
          {step.subagents.map((sub) => (
            <li key={sub.agentId} className="hv-activity__subagent">
              <span className="hv-activity__subagent-name">
                {sub.agentType || 'subagent'}
              </span>
              <ol className="hv-activity__calls">
                {sub.calls.map((ev, i) => (
                  <CallRow
                    key={ev.call_id ?? `n${i}`}
                    ev={ev}
                    working={working}
                    now={now}
                  />
                ))}
              </ol>
            </li>
          ))}
        </ol>
      ) : null}
    </li>
  );
}

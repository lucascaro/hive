// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Pinned responses (spec 566): what the agent concluded, beside what it
// is doing. In the inspector it is a capped, internally scrolling
// section above the plan — collapsible to its header — with one line per
// pin, newest first; a click opens a pin's body. In an activity-grid
// tile it is a static list of titles.
//
// Like the rest of the inspector it never takes the keyboard: the root's
// keepFocus cancels mousedown, and the action buttons are tabIndex -1,
// so the terminal keeps focus through every click.
import { useState, type ReactNode } from 'react';

import { SetClipboardText, Unpin, UnpinAll } from '../../bridge.js';
import { flashStatus, reportFailure } from '../../app/dom.js';
import { openChoiceDialog } from '../../app/modals/choice-dialog.js';
import { formatAge } from '../../lib/activity.js';
import {
  pinBody,
  requestPinBody,
  setPinsCollapsed,
  usePinsCollapsed,
  useSessionPins,
  type Pin,
} from '../../store/pins.js';
import { Icon } from '../Icon.js';
import { Markdown } from '../Markdown.js';

// pinAge is formatAge with days, since a pin can be kept for weeks.
export function pinAge(createdAt: number, now: number): string {
  const ms = Math.max(0, now - createdAt);
  return ms >= 86_400_000 ? `${Math.floor(ms / 86_400_000)}d` : formatAge(ms);
}

export async function confirmUnpinAll(
  sessionId: string,
  count: number,
): Promise<void> {
  const answer = await openChoiceDialog({
    title:
      count === 1
        ? 'Unpin the pinned response?'
        : `Unpin all ${count} pinned responses?`,
    note: 'The agent wrote these; unpinning removes them from Hive for good.',
    choices: [
      { label: 'Cancel', value: 'cancel' },
      { label: 'Unpin all', value: 'unpin', danger: true },
    ],
  });
  if (answer !== 'unpin') return;
  UnpinAll(sessionId).catch(reportFailure('unpin all'));
}

function copyPin(sessionId: string, pin: Pin): void {
  pinBody(sessionId, pin.id)
    .then((body) => SetClipboardText(body))
    .then(() => flashStatus(`Copied “${pin.title}”`))
    .catch(reportFailure('copy pin'));
}

export function PinnedSection({
  sessionId,
  count,
  now,
}: {
  sessionId: string;
  count: number;
  now: number;
}): ReactNode {
  const { pins, bodies } = useSessionPins(sessionId, count);
  const collapsed = usePinsCollapsed();
  const [open, setOpen] = useState<string | null>(null);
  if (pins.length === 0) return null;
  return (
    <section
      className="hv-activity__pins"
      aria-label="Pinned"
      data-collapsed={collapsed ? '' : undefined}
    >
      <div className="hv-activity__pins-head">
        {/* biome-ignore lint/a11y/useKeyWithClickEvents: deliberately not focusable — the terminal keeps the keyboard */}
        {/* biome-ignore lint/a11y/noStaticElementInteractions: see above */}
        <div
          className="hv-activity__pins-toggle"
          onClick={() => setPinsCollapsed(!collapsed)}
        >
          <Icon name={collapsed ? 'chevron-right' : 'chevron-down'} size={12} />
          <span className="hv-activity__section-title">Pinned</span>
          <span className="hv-activity__pill" title={`${pins.length} pinned`}>
            {pins.length}
          </span>
        </div>
        <button
          type="button"
          tabIndex={-1}
          className="hv-activity__pins-clear"
          onClick={() => void confirmUnpinAll(sessionId, pins.length)}
        >
          Unpin all
        </button>
      </div>
      {collapsed ? null : (
        <ol className="hv-activity__pin-list">
          {pins.map((p) => (
            <PinRow
              key={p.id}
              sessionId={sessionId}
              pin={p}
              body={bodies.get(p.id)}
              open={open === p.id}
              onToggle={() => {
                if (open !== p.id) requestPinBody(sessionId, p.id);
                setOpen(open === p.id ? null : p.id);
              }}
              now={now}
            />
          ))}
        </ol>
      )}
    </section>
  );
}

function PinRow({
  sessionId,
  pin,
  body,
  open,
  onToggle,
  now,
}: {
  sessionId: string;
  pin: Pin;
  body: string | undefined;
  open: boolean;
  onToggle: () => void;
  now: number;
}): ReactNode {
  return (
    <li className="hv-activity__pin" data-open={open ? '' : undefined}>
      <div className="hv-activity__pin-row">
        {/* biome-ignore lint/a11y/useKeyWithClickEvents: deliberately not focusable — see file header */}
        {/* biome-ignore lint/a11y/noStaticElementInteractions: see above */}
        <div
          className="hv-activity__pin-main"
          onClick={onToggle}
          title={pin.title}
        >
          <Icon name={open ? 'chevron-down' : 'chevron-right'} size={12} />
          <span className="hv-activity__pin-title">{pin.title}</span>
          <span className="hv-activity__age">
            {pinAge(pin.created_at, now)}
          </span>
        </div>
        <button
          type="button"
          tabIndex={-1}
          className="hv-activity__pin-action"
          aria-label="Copy pinned response"
          title="Copy"
          onClick={() => copyPin(sessionId, pin)}
        >
          <Icon name="copy" size={12} />
        </button>
        <button
          type="button"
          tabIndex={-1}
          className="hv-activity__pin-action"
          aria-label="Unpin"
          title="Unpin"
          onClick={() => Unpin(sessionId, pin.id).catch(reportFailure('unpin'))}
        >
          <Icon name="x" size={12} />
        </button>
      </div>
      {open ? (
        <div className="hv-activity__pin-body">
          {body === undefined ? (
            <span className="hv-activity__none">Loading…</span>
          ) : (
            <Markdown source={body} />
          )}
        </div>
      ) : null}
    </li>
  );
}

// PinnedTitles is the activity-grid tile's view: titles only, inert.
export function PinnedTitles({
  sessionId,
  count,
}: {
  sessionId: string;
  count: number;
}): ReactNode {
  const { pins } = useSessionPins(sessionId, count);
  if (pins.length === 0) return null;
  return (
    <ol className="hv-activity__tile-pins" aria-label="Pinned">
      {pins.map((p) => (
        <li key={p.id} className="hv-activity__tile-pin" title={p.title}>
          <Icon name="pin" size={12} />
          <span className="hv-activity__pin-title">{p.title}</span>
        </li>
      ))}
    </ol>
  );
}

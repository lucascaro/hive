// Chip — the minimized-project tray in the sidebar and the
// minimized-session tray above the status bar.
// docs/design-docs/ui/components.md › chip.
//
// A <span>, not a <button>: the chip body is one action and the restore
// control is another, and a button cannot contain a button. The trays are
// role="toolbar" divs, so a span is also the only valid child of both.
import type { CSSProperties } from 'react';
import { StateIcon } from './Icon.js';
import { IconButton } from './IconButton.js';
import type { AttentionSummary, SessionState } from '../lib/session-state.js';

export interface ChipProps {
  label: string;
  sublabel?: string;
  color?: string;
  state?: SessionState;
  active?: boolean;
  title?: string;
  ariaLabel: string;
  onClick: () => void;
  onRestore?: () => void;
  restoreLabel?: string;
  /** data-pid / data-sid, whichever the tray keys its chips by. */
  pid?: string;
  sid?: string;
  /**
   * Total sessions, for a chip that stands for a project. The session
   * tray leaves it unset — a session has no sessions.
   */
  count?: number;
  /**
   * What a project's sessions collectively want from the user, from
   * attentionSummary(). Drives data-state and the alert slot; unset when
   * nothing is ringing, and always unset on a session chip.
   */
  attention?: Omit<AttentionSummary, 'state'> & { state: SessionState };
}

export function Chip({
  label,
  sublabel,
  color,
  state,
  active,
  title,
  ariaLabel,
  onClick,
  onRestore,
  restoreLabel,
  pid,
  sid,
  count,
  attention,
}: ChipProps) {
  // Only set when the user actually picked a colour: the CSS falls back to
  // --fg-subtle, so an unset property is a themed default, while a literal
  // '#888' here would be an untokenised colour smuggled in from TS.
  const style = color
    ? ({ '--chip-color': color } as CSSProperties)
    : undefined;
  return (
    <span
      className="hv-chip"
      data-pid={pid}
      data-sid={sid}
      data-state={state ?? attention?.state}
      data-active={active ? '' : undefined}
      style={style}
    >
      <button
        type="button"
        className="hv-chip__open"
        aria-label={ariaLabel}
        title={title ?? ariaLabel}
        onClick={(e) => {
          e.stopPropagation();
          onClick();
        }}
      >
        {/* State icon when the chip stands for a session (it carries the
            bell for a session that has no row on screen); a plain colour
            dot when it stands for a project, whose identity colour is the
            only thing besides the label naming it. A project's own state
            is the union of its sessions' and rides the alert slot below,
            not this one. */}
        {state ? (
          <StateIcon state={state} />
        ) : (
          <span className="hv-chip__swatch" />
        )}
        <span className="hv-chip__label">{label}</span>
        {sublabel ? <span className="hv-chip__sub">{sublabel}</span> : null}
        {/* Alert then count, right-aligned (the label takes the slack —
            see minimized.css): the session count is the rightmost thing
            so counts line up down the list, with the attention badge
            immediately left of it. */}
        {attention ? (
          <span className="hv-chip__alert">
            <StateIcon state={attention.state} />
            {attention.count}
          </span>
        ) : null}
        {count == null ? null : <span className="hv-chip__count">{count}</span>}
      </button>
      {onRestore ? (
        <IconButton
          icon="plus"
          label={restoreLabel ?? ariaLabel}
          className="hv-chip__restore"
          onClick={(e) => {
            e.stopPropagation();
            onRestore();
          }}
        />
      ) : null}
    </span>
  );
}

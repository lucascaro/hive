// Chip — minimized-project tray (the minimized-session tray is still the
// imperative src/ui/chip.ts until Phase 2 ports the chrome island).
// docs/design-docs/ui/components.md › chip.
//
// A <span>, not a <button>: the chip body is one action and the restore
// control is another, and a button cannot contain a button. The trays are
// role="toolbar" divs, so a span is also the only valid child of both.
import type { CSSProperties } from 'react';
import { StateIcon } from './Icon.js';
import { IconButton } from './IconButton.js';
import type { SessionState } from '../lib/session-state.js';

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
  /** data-state='attention' for a project whose sessions are ringing. */
  attention?: boolean;
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
      data-state={state ?? (attention ? 'attention' : undefined)}
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
            dot when it stands for a project, whose state is the union of
            its sessions' and is carried by the pulse on the dot. */}
        {state ? (
          <StateIcon state={state} />
        ) : (
          <span className="hv-chip__swatch" />
        )}
        <span className="hv-chip__label">{label}</span>
        {sublabel ? <span className="hv-chip__sub">{sublabel}</span> : null}
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

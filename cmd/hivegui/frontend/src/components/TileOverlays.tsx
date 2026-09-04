// The terminal tile's two overlays: the dead-session card and the
// lifecycle loading panel. Rendered into `.tile-overlays`, the
// `display: contents` mount app/session-term.ts appends after the body
// (see TileChrome.tsx for why the header and the overlays mount
// differently).
//
// Unlike the header, these elements are created by React outright —
// element, class, `hidden`, `role` and content. Both are
// `position: absolute` and contribute no layout, so a one-frame-late
// mount costs nothing; and both spend most of a session's life hidden,
// which is exactly what React is good at.
//
// What did NOT move: SessionTerm still owns the control flow. setDead(),
// setPhase(), _showPhaseOverlay(), _hidePhaseOverlay() and
// revealAfterReplay() keep their names, call sites and timing — their
// bodies became store writes.
import { useEffect, useRef, type ReactNode } from 'react';

import { anyModalOpen } from '../store/store.js';
import type { TileChromeState } from '../store/store.js';
import { Icon, StateIcon } from './Icon.js';

const DEAD_SUBTITLE = 'The process running in this session has exited.';

export function TileOverlays({
  chrome,
  onClose,
  onDismiss,
}: {
  chrome: TileChromeState;
  onClose: () => void;
  onDismiss: () => void;
}): ReactNode {
  return (
    <>
      <DeadOverlay
        dead={chrome.dead}
        reason={chrome.deadReason}
        onClose={onClose}
        onDismiss={onDismiss}
      />
      <PhaseOverlay visible={chrome.phaseVisible} panel={chrome.phasePanel} />
    </>
  );
}

// Hidden until the underlying process exits (Alive true→false). Centered
// card with primary "Close session" (Enter) and secondary "Dismiss"
// (Escape) — keyboard.ts routes those two keys here off the tile's
// `deadOverlayShown`.
function DeadOverlay({
  dead,
  reason,
  onClose,
  onDismiss,
}: {
  dead: boolean;
  reason: string;
  onClose: () => void;
  onDismiss: () => void;
}): ReactNode {
  const closeRef = useRef<HTMLButtonElement>(null);
  // Focus lands in an effect rather than at the setDead() call site,
  // because the button does not exist until this commit. The setTimeout
  // is carried over from the imperative version: it defers focus past
  // the visibility flip and past any pending blur from the dying xterm.
  //
  // Never while a modal is open. A session can die at any moment — the
  // daemon drives this, not the user — and stealing focus out of a modal
  // mid-keystroke drops what you were typing into the project editor or
  // the command palette. For the launcher it is worse: it closes when
  // focus leaves it, so an unrelated session exiting would make the
  // popup and its query vanish outright.
  //
  // The cleanup is what the old `deadOverlayShown` re-check bought, and
  // a little more: it also covers the tile being destroyed while the
  // overlay is up, which used to focus a detached button.
  useEffect(() => {
    if (!dead) return;
    const t = setTimeout(() => {
      if (!anyModalOpen()) closeRef.current?.focus();
    }, 0);
    return () => clearTimeout(t);
  }, [dead]);
  return (
    <div
      className="dead-overlay"
      role="alertdialog"
      aria-label="Session ended"
      hidden={!dead}
    >
      <div className="dead-card">
        <div className="dead-title">Session ended</div>
        <div className="dead-subtitle">{reason || DEAD_SUBTITLE}</div>
        <div className="dead-buttons">
          <button
            ref={closeRef}
            type="button"
            className="dead-btn primary"
            onClick={(e) => {
              e.stopPropagation();
              onClose();
            }}
          >
            Close session
          </button>
          <button
            type="button"
            className="dead-btn secondary"
            onClick={(e) => {
              e.stopPropagation();
              onDismiss();
            }}
          >
            Dismiss
          </button>
        </div>
      </div>
    </div>
  );
}

// The opaque panel over the terminal body while the session is being
// created. It covers the window in which the shell paints its startup
// output, so the user lands on a settled screen instead of watching
// rc-files scroll past. Model from lib/phase-steps.ts, rendered as-is.
function PhaseOverlay({
  visible,
  panel,
}: {
  visible: boolean;
  panel: TileChromeState['phasePanel'];
}): ReactNode {
  return (
    <div
      className="phase-overlay"
      role="status"
      aria-live="polite"
      hidden={!visible}
    >
      <div className="phase-card">
        <div className="phase-spinner" aria-hidden="true" />
        <div className="phase-status">{panel?.status ?? ''}</div>
        <ul className="phase-steps">
          {(panel?.steps ?? []).map((step) => (
            // The label is keyed on, not the index: a step's label is
            // unique within a panel and stable across the state changes
            // that walk it from todo to done.
            <li key={step.label} className="phase-step" data-state={step.state}>
              {/* The mark used to be a CSS ::before dot/check/half-circle
                  glyph; it is an icon now so it matches the rest of the
                  family. 'todo' gets no mark — the indent in
                  phase-step::before holds the column. */}
              {step.state === 'done' ? <Icon name="check" size={12} /> : null}
              {step.state === 'active' ? <StateIcon state="starting" /> : null}
              <span>{step.label}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

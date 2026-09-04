// The one resolution from a SessionInfo to the state shapes
// (docs/design-docs/ui/icons.md > State icons). Sidebar row, minimized
// chip and grid tile header all call this, so they can never disagree.
//
// Pure and structural for the same reason as lib/phase-steps.ts: it
// must be importable from the node-env unit suite, which app/state.ts
// (localStorage on import) is not. `hasAttention` is passed in rather
// than read from state.attention for the same reason.
//
// No exit code exists on the wire (internal/wire has no ExitCode field
// and SessionInfo has no exit_code). last_error is the only "it ended
// badly" signal the daemon sends, and it is what the dead-session
// overlay already reads (app/events.ts, app/session-term.ts). icons.md
// resolves the same way for the same reason.
import { phaseOf, isReady } from './phase-steps.js';

export type SessionState =
  | 'starting'
  | 'attention'
  | 'waiting-permission'
  | 'working'
  | 'running'
  | 'exited'
  | 'error';

// The daemon's own state vocabulary (internal/wire/control.go State*).
// Kept as a local map rather than imported so this module stays
// importable from the node-env unit suite.
export const DAEMON_STATE = {
  idle: '',
  working: 'working',
  waitingInput: 'waiting_input',
  waitingPermission: 'waiting_permission',
  exited: 'exited',
  error: 'error',
} as const;

export interface StateCarrier {
  alive?: boolean;
  phase?: string;
  last_error?: string;
  lastError?: string;
  // The daemon's session state. Absent = idle, which is both the
  // omitempty case and what an older daemon sends.
  state?: string;
}

/** Words for the icon's <title>: state is shape + colour + words. */
export const STATE_WORDS: Record<SessionState, string> = {
  starting: 'Starting',
  attention: 'Waiting for you',
  'waiting-permission': 'Waiting for permission',
  working: 'Working',
  running: 'Running',
  exited: 'Exited',
  error: 'Exited with an error',
};

export function sessionState(
  s: StateCarrier,
  hasAttention: boolean,
): SessionState {
  // A session mid-create has no PTY yet; `alive: false` there means
  // "not born", not "died" (same reasoning as sidebar.ts's dead class).
  if (!isReady(phaseOf(s))) return 'starting';
  if (!s.alive) return s.last_error || s.lastError ? 'error' : 'exited';

  // The daemon's state wins over the local attention flag where the two
  // could disagree: "waiting for permission" and "waiting for you" are
  // the distinction this whole state model exists to draw, and folding
  // the first into the second throws it away.
  switch (s.state) {
    case DAEMON_STATE.waitingPermission:
      return 'waiting-permission';
    case DAEMON_STATE.waitingInput:
      return 'attention';
    case DAEMON_STATE.error:
      return 'error';
  }
  // A bell the user has not acknowledged still outranks "working": the
  // heuristic tier reports both, and the one that wants a human is the
  // one worth showing.
  if (hasAttention) return 'attention';
  if (s.state === DAEMON_STATE.working) return 'working';
  return 'running';
}

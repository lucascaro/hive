// The one resolution from a SessionInfo to the five state shapes
// (docs/design-docs/ui/icons.md > State icons). Sidebar row, minimized
// chip and grid tile header all call this, so they can never disagree.
//
// Pure and structural for the same reason as lib/phase-steps.ts: it
// must be importable from the node-env unit suite, which app/state.ts
// (localStorage on import) is not. `hasAttention` is passed in rather
// than read from state.attention for the same reason.
//
// icons.md writes the exit branch as `exit_code == 0`, but no exit code
// exists on the wire (internal/wire has no ExitCode field and
// SessionInfo has no exit_code). last_error is the only "it ended
// badly" signal the daemon sends, and it is what the dead-session
// overlay already reads (app/events.ts, app/session-term.ts).
import { phaseOf, isReady } from './phase-steps.js';

export type SessionState =
  | 'starting'
  | 'attention'
  | 'running'
  | 'exited'
  | 'error';

export interface StateCarrier {
  alive?: boolean;
  phase?: string;
  last_error?: string;
  lastError?: string;
}

/** Words for the icon's <title>: state is shape + colour + words. */
export const STATE_WORDS: Record<SessionState, string> = {
  starting: 'Starting',
  attention: 'Waiting for you',
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
  return hasAttention ? 'attention' : 'running';
}

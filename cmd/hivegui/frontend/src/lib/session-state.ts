// The one resolution from a SessionInfo to the state shapes
// (docs/design-docs/ui/icons.md > State icons). Sidebar row, minimized
// chip and grid tile header all call this, so they can never disagree.
//
// Pure and structural for the same reason as lib/phase-steps.ts: it
// must be importable from the node-env unit suite, which app/state.ts
// (localStorage on import) is not — hence StateCarrier rather than
// importing SessionInfo.
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
  // The daemon's own "wants the user" flag — derived server-side from
  // `state` (needs_attention = state ∈ {waiting_input,
  // waiting_permission}). The daemon and the session list are its only
  // writers; no client keeps a second copy (see the frozen transition
  // table in docs/exec-plans/active/336-session-state-model.md).
  needs_attention?: boolean;
  // Which tier produced `state` (internal/wire/control.go StateSource*).
  // Absent = heuristic.
  state_source?: string;
  // What the agent reported it was asked to do, and what it said as it
  // finished its last turn. Both absent on the heuristic tier.
  last_prompt?: string;
  last_summary?: string;
}

/** Words for the icon's <title>: state is shape + colour + words. */
export const STATE_WORDS: Record<SessionState, string> = {
  starting: 'Starting',
  attention: 'Waiting for you',
  'waiting-permission': 'Waiting for permission',
  working: 'Working',
  running: 'Idle',
  exited: 'Exited',
  error: 'Exited with an error',
};

// How the state was arrived at, in words. Rendered because "the agent
// told us it is waiting for permission" and "no bytes arrived for two
// seconds" are not the same claim (internal/wire/control.go on
// StateSource). hook and extension collapse to one phrase on purpose:
// which transport the agent used to say it is not the user's business,
// only that the agent said it rather than us inferring it.
const SOURCE_WORDS: Record<string, string> = {
  hook: 'reported by the agent',
  extension: 'reported by the agent',
};
const HEURISTIC_WORDS = 'guessed from terminal output';

/**
 * The full tooltip for a session's state icon: the state in words, what
 * the session was asked to do, what the agent last said, and how we
 * know. Lines are dropped when the underlying field is empty, so a
 * heuristic-tier session still gets the one-line tooltip it has today.
 *
 * One helper for the same reason as sessionState above: the sidebar row
 * and the tile header must never disagree about what a session is
 * doing.
 */
export function stateTooltip(s: StateCarrier, state?: SessionState): string {
  const lines = [STATE_WORDS[state ?? sessionState(s)]];
  // Quoted: a prompt is the user's own words being read back, and
  // without the quotes it runs together with the state line above it.
  if (s.last_prompt) lines.push(`“${s.last_prompt}”`);
  if (s.last_summary) lines.push(s.last_summary);
  lines.push(SOURCE_WORDS[s.state_source ?? ''] ?? HEURISTIC_WORDS);
  return lines.join('\n');
}

export function sessionState(s: StateCarrier): SessionState {
  // A session mid-create has no PTY yet; `alive: false` there means
  // "not born", not "died" (same reasoning as sidebar.ts's dead class).
  if (!isReady(phaseOf(s))) return 'starting';
  if (!s.alive) return s.last_error || s.lastError ? 'error' : 'exited';

  // The daemon's state wins over needs_attention where the two could
  // disagree: "waiting for permission" and "waiting for you" are the
  // distinction this whole state model exists to draw, and folding the
  // first into the second throws it away.
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
  if (s.needs_attention) return 'attention';
  if (s.state === DAEMON_STATE.working) return 'working';
  return 'running';
}

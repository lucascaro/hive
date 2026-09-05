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

/** What a project's sessions collectively want from the user. */
export interface AttentionSummary {
  /** How many of the project's sessions are waiting on the user. */
  count: number;
  /** The most specific of those states, or null when count is 0. */
  state: SessionState | null;
}

// attentionSummary is the one aggregation from a project's sessions to the
// pair of numbers two surfaces show: the collapsed project card's
// "k need you" and the minimized project chip's alert count. It lives here,
// beside sessionState(), for the reason given at the top of this file — the
// card and the chip computed attention separately until now, by two
// different routes, which is exactly how they come to disagree.
//
// A session counts when its state is one of the two the daemon derives
// `needs_attention` from. This is deliberately NOT the same as reading
// `needs_attention` directly: sessionState() short-circuits on isReady()
// and `alive` first, so a session that is still starting, or already
// exited, stops counting even if its last-known flag was set. That is the
// reading we want — a dead session cannot want anything from the user, and
// a bell that outlives its session is the stale indicator patterns.md's
// "clears in the same render" rule exists to prevent.
export function attentionSummary(sessions: StateCarrier[]): AttentionSummary {
  let count = 0;
  let permission = false;
  for (const s of sessions) {
    const st = sessionState(s);
    if (st !== 'attention' && st !== 'waiting-permission') continue;
    count++;
    if (st === 'waiting-permission') permission = true;
  }
  // "Waiting for permission" is the more specific of the two, and the
  // distinction the state model exists to draw, so it wins the one icon
  // the chip has room for.
  return {
    count,
    state: count === 0 ? null : permission ? 'waiting-permission' : 'attention',
  };
}

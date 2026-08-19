// Session lifecycle phases and the loading-panel model for a session
// that isn't in its steady state. Pure — session-term.ts renders
// whatever this returns, tests assert on the model. Same shape of seam
// as lib/empty-state.ts.
//
// The daemon reports one phase at a time (internal/wire/control.go
// Phase*); the checklist is derived here rather than sent over the
// wire, so the labels can name the agent and branch the user picked.

// Only the phase field is read, so this stays structural rather than
// importing app/state.ts (which touches localStorage on import and
// can't be pulled into the node-env unit suite).
export interface PhaseCarrier {
  phase?: string;
}

// Session lifecycle phases, mirroring wire.Phase* in
// internal/wire/control.go. Ready is the empty string: a session with
// no phase on the wire (an older daemon, or the steady state) is ready.
//
// Hand-mirrored, not generated — the wire is JSON, so nothing checks
// these against the Go side at build time. A rename there would just
// make phasePanel() return null here (no overlay, no error), so
// TestPhaseConstantsMatchFrontend in internal/wire/ asserts every
// wire.Phase* string still appears in this file. Keep CREATE_ORDER
// below in sync too.
export const PHASE = {
  ready: '',
  starting: 'starting',
  fetching: 'fetching',
  worktree: 'worktree',
  spawning: 'spawning',
  checking: 'checking',
  closing: 'closing',
  restarting: 'restarting',
} as const;

export type Phase = (typeof PHASE)[keyof typeof PHASE];

/** The session's phase, defaulting to ready. */
export function phaseOf(info: PhaseCarrier | null | undefined): Phase {
  return ((info?.phase ?? PHASE.ready) || PHASE.ready) as Phase;
}

/**
 * Ready ⇒ the session is in its steady state. Only a ready session may
 * be attached to: SESSION_EVENT(added) now fires before the PTY
 * exists, and attaching earlier is answered with `session_starting`.
 */
export function isReady(phase: string): boolean {
  return phase === PHASE.ready;
}

/** Coming up: the create path, before the PTY exists. */
export function isStarting(phase: string): boolean {
  return (
    phase === PHASE.starting ||
    phase === PHASE.fetching ||
    phase === PHASE.worktree ||
    phase === PHASE.spawning ||
    phase === PHASE.restarting
  );
}

/** Going away: the kill path. Errors from a session here are noise. */
export function isClosing(phase: string): boolean {
  return phase === PHASE.checking || phase === PHASE.closing;
}

export type StepState = 'done' | 'active' | 'todo';

export interface PhaseStep {
  label: string;
  state: StepState;
}

export interface PhasePanel {
  /** Short line under the spinner, e.g. "Creating worktree…". */
  status: string;
  steps: PhaseStep[];
}

export interface PhaseInput {
  phase: string;
  /** Canonical agent id; "" ⇒ a plain shell. */
  agent?: string;
  /** Branch backing the worktree; "" ⇒ no worktree in play. */
  worktreeBranch?: string;
}

// Create-path phases, in the order the daemon walks them. A phase's
// index is how far along the checklist we are.
const CREATE_ORDER = ['starting', 'fetching', 'worktree', 'spawning'];

/**
 * The panel for a session in a transient phase, or null when the
 * session is ready (nothing to show) — including an unknown phase
 * from a newer daemon, which is better rendered as "no overlay" than
 * as a spinner that never resolves.
 */
export function phasePanel({
  phase,
  agent = '',
  worktreeBranch = '',
}: PhaseInput): PhasePanel | null {
  const agentLabel = agent || 'shell';

  if (phase === 'restarting') {
    return {
      status: `Restarting ${agentLabel}…`,
      steps: [{ label: `Restarting ${agentLabel}`, state: 'active' }],
    };
  }
  if (phase === 'checking') {
    return {
      status: 'Checking for uncommitted changes…',
      steps: [
        { label: 'Checking for uncommitted changes', state: 'active' },
        { label: 'Removing worktree', state: 'todo' },
      ],
    };
  }
  if (phase === 'closing') {
    return {
      status: 'Closing session…',
      steps: [
        { label: 'Checking for uncommitted changes', state: 'done' },
        {
          label: worktreeBranch ? 'Removing worktree' : 'Closing session',
          state: 'active',
        },
      ],
    };
  }

  const at = CREATE_ORDER.indexOf(phase);
  if (at < 0) return null;

  // A session with no worktree branch skips the git steps entirely —
  // that covers plain sessions and ⌘P duplicates, which adopt a
  // sibling's worktree instead of creating one.
  const planned: { key: string; label: string }[] = [
    { key: 'starting', label: 'Registered session' },
  ];
  if (worktreeBranch) {
    planned.push(
      { key: 'fetching', label: 'Fetching origin' },
      { key: 'worktree', label: `Creating worktree ${worktreeBranch}` },
    );
  }
  planned.push({ key: 'spawning', label: `Starting ${agentLabel}` });

  const steps: PhaseStep[] = planned.map(({ key, label }) => {
    const idx = CREATE_ORDER.indexOf(key);
    const state: StepState = idx < at ? 'done' : idx === at ? 'active' : 'todo';
    return { label, state };
  });
  // The phase in flight can be one the checklist skipped (a
  // worktree-less session never shows `fetching`), which would leave
  // the panel with no spinner. Promote the first pending step instead.
  let active = steps.find((s) => s.state === 'active');
  if (!active) {
    active = steps.find((s) => s.state === 'todo');
    if (active) active.state = 'active';
  }
  return {
    status: active ? `${active.label}…` : 'Starting…',
    steps,
  };
}

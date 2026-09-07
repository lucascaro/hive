// ---------- pending opening prompt ----------
//
// A bar above the grid offering the note a session was started from,
// for the user to place when the agent is actually ready for it.
//
// Hive used to type this in by itself, on the first idle edge after the
// session had been working. That is not a signal that an agent wants
// input: measured, `codex` in a fresh directory — which is every
// worktree "Start session" creates — is still sitting on its "do you
// trust the contents of this directory?" gate at that edge, and the
// gate is a numbered menu. It swallowed the note whole, echoing
// nothing, and an automatic Enter would have answered it "Yes,
// continue". No heuristic here can tell a prompt box from a startup
// gate. The person looking at the terminal can, so they decide.
//
// Shown for the ACTIVE session only: it is about the terminal in front
// of you, and a stack of bars for background sessions would be noise.

import type { ReactNode } from 'react';
import { ResolvePrompt } from '../bridge.js';
import { reportFailure } from '../app/dom.js';
import { useAppStore } from '../store/store.js';
import { Button } from './Button.js';

export function PendingPrompt(): ReactNode {
  const session = useAppStore((s) =>
    s.sessions.find((x) => x.id === s.activeId),
  );
  // snake_case on the wire, camelCase tolerated at the boundary — the
  // convention every SessionInfo reader in this app follows.
  const prompt = session?.pending_prompt ?? session?.pendingPrompt ?? '';
  // Only once there is something to paste INTO. The daemon refuses a
  // paste with no live process (ErrNoLiveSession) and keeps the offer,
  // so showing the bar while the session is still spawning would just
  // be a button that fails until it does not.
  if (!session || !prompt || !session.alive) return null;

  const resolve = (paste: boolean) =>
    ResolvePrompt(session.id, paste).catch(
      reportFailure(paste ? 'paste prompt' : 'dismiss prompt'),
    );

  return (
    // <fieldset>, not role="group": biome's useSemanticElements, and
    // it is literally a group of controls over one value.
    <fieldset className="hv-pending-prompt" id="pending-prompt">
      {/* Says what each button does, since "Paste" alone does not
          convey that Hive stops short of pressing Enter. */}
      <span className="hv-pending-prompt__label">
        {/* Names the session: in grid view several terminals are on
            screen at once, and "when the agent is ready" is ambiguous
            without saying which one this lands in. */}
        Opening prompt for {session.name ?? 'this session'} — paste it when the
        agent is ready; you press Enter
      </span>
      {/* The whole note, scrollable rather than clamped: this is the
          last look at it before it goes to an agent. */}
      <span className="hv-pending-prompt__text" id="pending-prompt-text">
        {prompt}
      </span>
      <span className="hv-pending-prompt__actions">
        <Button
          id="pending-prompt-paste"
          label="Paste"
          kind="primary"
          onClick={() => void resolve(true)}
        />
        <Button
          id="pending-prompt-dismiss"
          label="Dismiss"
          onClick={() => void resolve(false)}
        />
      </span>
    </fieldset>
  );
}

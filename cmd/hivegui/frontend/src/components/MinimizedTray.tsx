// The minimized-session tray above the status bar. Mounted on
// #minimized-tray, which keeps its id, role="toolbar" and the `.hidden`
// class that collapses its grid row when the set is empty.
//
// The tray content is derived, never stored: `minimized` and the session
// list are already in the store, so a second copy could only go stale.
import { memo, useLayoutEffect, type ReactNode } from 'react';
import { orderedSessions } from '../app/selectors.js';
import { sessionState, type SessionState } from '../lib/session-state.js';
import { readProjectId } from '../lib/wire.js';
import { useAppStore } from '../store/store.js';
import { Chip } from './Chip.js';

// memo, and every prop below it a primitive: this is the same place
// Phase 1's SessionItem collects the migration's performance goal.
// `updateSession()` replaces the sessions ARRAY on every `title` event —
// one per redraw of a busy child program — so the tray re-renders at that
// rate even though a title change usually concerns a session with no chip
// at all. The old renderMinimizedTray() was never wired to the title path;
// without this memo the port would have put work there that did not exist
// before, on the very axis this rewrite is meant to improve.
//
// The parent still re-runs its filter each time. That is a sort over tens
// of sessions and cannot be avoided without an equality function the
// store's hook does not expose — but it no longer rebuilds any chip
// markup, which is the part that costs.
const TrayChip = memo(function TrayChip(p: {
  id: string;
  label: string;
  sublabel: string | undefined;
  color: string | undefined;
  state: SessionState;
  onRestore: (id: string) => void;
}) {
  return (
    <Chip
      sid={p.id}
      label={p.label}
      sublabel={p.sublabel}
      color={p.color}
      state={p.state}
      ariaLabel={`Restore ${p.label}`}
      onClick={() => p.onRestore(p.id)}
    />
  );
});

export function MinimizedTray({
  root,
  restoreSession,
}: {
  root: HTMLElement | null;
  /**
   * Owned by view.ts: restoring repaints the grid and can move focus.
   * Referentially stable for the life of the app (main.ts passes the
   * module function itself), which is what lets TrayChip's memo hold.
   */
  restoreSession: (id: string) => void;
}): ReactNode {
  const projects = useAppStore((s) => s.projects);
  const minimized = useAppStore((s) => s.minimized);
  const attention = useAppStore((s) => s.attention);
  // Subscribed for the re-render; orderedSessions() reads the same list
  // back off the state facade. Calling it inside a selector instead would
  // hand useSyncExternalStore a fresh array on every store notification.
  useAppStore((s) => s.sessions);

  // Display order, so the chip row reads left-to-right like the sidebar
  // reads top-to-bottom.
  const chips = orderedSessions().filter((s) => minimized.has(s.id));

  useLayoutEffect(() => {
    root?.classList.toggle('hidden', chips.length === 0);
  }, [root, chips.length]);

  return (
    <>
      {chips.map((info) => (
        <TrayChip
          key={info.id}
          id={info.id}
          label={info.name ?? ''}
          sublabel={projects.find((p) => p.id === readProjectId(info))?.name}
          color={info.color}
          state={sessionState(info, attention.has(info.id))}
          onRestore={restoreSession}
        />
      ))}
    </>
  );
}

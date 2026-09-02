// The minimized-session tray above the status bar. Mounted on
// #minimized-tray, which keeps its id, role="toolbar" and the `.hidden`
// class that collapses its grid row when the set is empty.
//
// The tray content is derived, never stored: `minimized` and the session
// list are already in the store, so a second copy could only go stale.
import { useLayoutEffect, type ReactNode } from 'react';
import { orderedSessions } from '../app/selectors.js';
import { sessionState } from '../lib/session-state.js';
import { readProjectId } from '../lib/wire.js';
import { useAppStore } from '../store/store.js';
import { Chip } from './Chip.js';

export function MinimizedTray({
  root,
  restoreSession,
}: {
  root: HTMLElement | null;
  /** Owned by view.ts: restoring repaints the grid and can move focus. */
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
        <Chip
          key={info.id}
          sid={info.id}
          label={info.name ?? ''}
          sublabel={projects.find((p) => p.id === readProjectId(info))?.name}
          color={info.color}
          state={sessionState(info, attention.has(info.id))}
          ariaLabel={`Restore ${info.name}`}
          onClick={() => restoreSession(info.id)}
        />
      ))}
    </>
  );
}

// Places the inspector panel: a third #app column beside #terms, shown in
// single view while the panel is toggled on. Not inside #terms, whose
// children app/grid-layout.ts owns, and not inside the terminal host,
// whose mousedown selects the session.
//
// The column appears through a class on #app (theme/layout.css); the
// terminal refits through its own ResizeObserver.
import { useLayoutEffect, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

import { mustEl } from '../../app/el.js';
import { useAppStore } from '../../store/store.js';
import { activityRenderers } from './registry.js';

export function ActivityPanelHost(): ReactNode {
  const sessionId = useAppStore((s) =>
    s.view === 'single' && s.activityPanel ? s.activeId : null,
  );
  const aside = mustEl('activity-panel');
  useLayoutEffect(() => {
    mustEl('app').classList.toggle('activity-panel-open', sessionId !== null);
    aside.hidden = sessionId === null;
  }, [sessionId, aside]);
  if (!sessionId) return null;
  const Panel = activityRenderers.panel;
  return createPortal(<Panel key={sessionId} sessionId={sessionId} />, aside);
}

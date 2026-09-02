// @vitest-environment jsdom
//
// The tray's half of the rewrite's performance goal, pinned — the same
// property sidebar-render-scope.test.tsx pins for the session rows.
//
// `updateSession()` replaces the sessions ARRAY, so MinimizedTray
// re-renders on every `session:event` — including `title`, which a busy
// agent emits as fast as it redraws. The imperative renderMinimizedTray()
// was never wired to the title path at all, so without the memo on
// TrayChip this port would ADD per-title chip rebuilds that did not exist
// before, on exactly the axis the migration is meant to improve.
//
// Asserted by counting Chip renders. The first three cases fail without
// the memo; the last three pin the changes that SHOULD still reach a chip,
// because a memo that never lets anything through would be just as wrong.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { act, render } from '@testing-library/react';
import { appStore, resetStore } from '../../src/store/store.js';
import * as store from '../../src/store/store.js';

const renders: string[] = [];

vi.mock('../../src/components/Chip.js', () => ({
  Chip: (p: { sid?: string }) => {
    renders.push(p.sid ?? '');
    return <span className="hv-chip" data-sid={p.sid} />;
  },
}));

let MinimizedTray: typeof import('../../src/components/MinimizedTray.js').MinimizedTray;
let root: HTMLElement;

// Module-level and never re-created: main.ts passes view.ts's function
// itself, and a fresh arrow per render would defeat the memo outright —
// which is the failure mode this stability is here to rule out.
const restoreSession = () => {};

beforeAll(async () => {
  ({ MinimizedTray } = await import('../../src/components/MinimizedTray.js'));
});

beforeEach(() => {
  resetStore({
    projects: [{ id: 'p1', name: 'proj' }],
    sessions: [
      { id: 'a', name: 'api', project_id: 'p1', order: 0, alive: true },
      { id: 'b', name: 'web', project_id: 'p1', order: 1, alive: true },
      { id: 'c', name: 'db', project_id: 'p1', order: 2, alive: true },
    ],
    // Two of the three are in the tray; 'c' is the control that proves a
    // re-render was scoped rather than merely small.
    minimized: new Set(['a', 'b']),
  });
  // RTL's cleanup() removes a container whose parentNode IS document.body,
  // which would detach #minimized-tray between cases — see
  // status-bar.test.tsx for the same wrapper.
  document.getElementById('app')?.remove();
  const app = document.createElement('div');
  app.id = 'app';
  document.body.appendChild(app);
  root = document.createElement('div');
  root.id = 'minimized-tray';
  app.appendChild(root);
  render(<MinimizedTray root={root} restoreSession={restoreSession} />, {
    container: root,
  });
  renders.length = 0;
});

const byId = (id: string) =>
  appStore.getState().sessions.find((s) => s.id === id);

// Store writes land outside a React event handler, so they need act() for
// the useAppStore subscription to flush before the assertion.
const update = (fn: () => void) => act(fn);

describe('minimized tray re-render scope', () => {
  // A chip shows the session's NAME, never its title, so the highest-rate
  // event in the app has nothing to say to this tray. Both cases below
  // fail without the memo — with it, a redrawing agent costs the tray one
  // filter and zero chip markup.
  it('rebuilds no chip when a session with no chip is retitled', () => {
    const c = byId('c');
    if (!c) throw new Error('no session c');
    update(() => store.updateSession({ ...c, title: 'npm run build' }));
    expect(renders).toEqual([]);
  });

  it('rebuilds no chip when a session that HAS one is retitled', () => {
    const a = byId('a');
    if (!a) throw new Error('no session a');
    update(() => store.updateSession({ ...a, title: 'npm test' }));
    expect(renders).toEqual([]);
  });

  it('rebuilds only the renamed chip on a rename', () => {
    const a = byId('a');
    if (!a) throw new Error('no session a');
    update(() => store.updateSession({ ...a, name: 'api-v2' }));
    expect(renders).toEqual(['a']);
  });

  it('rebuilds no chip for an unrelated store write', () => {
    update(() => store.setActiveId('c'));
    expect(renders).toEqual([]);
  });

  it('rebuilds the chip whose session starts ringing', () => {
    update(() => store.addAttention('b'));
    expect(renders).toEqual(['b']);
  });

  it('renders a chip for a session that is newly minimized', () => {
    update(() => store.minimizeSession('c'));
    expect(renders).toContain('c');
  });
});

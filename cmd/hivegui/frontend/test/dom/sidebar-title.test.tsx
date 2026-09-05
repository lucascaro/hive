// @vitest-environment jsdom
//
// The window-title line under each sidebar session name
// (components/SessionRow.tsx + src/lib/term-title.ts).
//
// The regression this file exists for is the in-place update test: a
// title change must keep the existing row nodes. A running program
// changes its title as it works, and the imperative sidebar's only
// structural path was an innerHTML wipe that recreated every row and
// listener — taking that path thrashed the sidebar and ate dblclick
// pairs mid-rename. React keys the rows by session id, so the node
// survives; this pins that it actually does. The daemon gives title
// changes their own SESSION_EVENT kind so the path is reachable at all.
import { describe, it, expect, beforeAll, beforeEach } from 'vitest';
import type { SessionInfo } from '../../src/app/state.js';
import { appStore } from '../../src/store/store.js';
import * as store from '../../src/store/store.js';
import {
  loadSidebar,
  mountSidebar,
  row,
  seed,
  update,
} from './sidebar-harness.js';

let Sidebar: Awaited<ReturnType<typeof loadSidebar>>;

beforeAll(async () => {
  Sidebar = await loadSidebar();
});

function withSessions(sessions: SessionInfo[]) {
  seed({
    projects: [{ id: 'p1', name: 'proj', color: '#888' }],
    sessions: sessions.map((s) => ({ project_id: 'p1', alive: true, ...s })),
    collapsed: new Set(),
    activeId: null,
  });
  mountSidebar(Sidebar);
}

function titleSlot(id: string): HTMLElement {
  const el = row(id).querySelector<HTMLElement>('.hv-session-row__sub');
  if (!el) throw new Error(`no title slot for ${id}`);
  return el;
}

beforeEach(() => {
  seed({});
});

describe('sidebar window titles', () => {
  it('renders the title under the session name', () => {
    withSessions([{ id: 'a', name: 'api', order: 0, title: 'npm run build' }]);

    const r = row('a');
    // Name and title stack inside one wrapper; the dot and swatch stay
    // siblings of that wrapper so they center against the taller row.
    const text = r.querySelector('.hv-session-row__text');
    expect(text).not.toBeNull();
    expect(text?.querySelector('.hv-session-row__name')?.textContent).toBe(
      'api',
    );
    expect(text?.querySelector('.hv-session-row__sub')?.textContent).toBe(
      'npm run build',
    );
    expect(r.querySelector('.hv-session-row__state')?.parentElement).toBe(r);
    expect(r.querySelector('.hv-session-row__swatch')?.parentElement).toBe(r);
  });

  it('exposes the untruncated title as a tooltip', () => {
    const long = 'deploying '.repeat(30);
    withSessions([{ id: 'a', name: 'api', order: 0, title: long }]);
    expect(titleSlot('a').title).toBe(long.trim());
  });

  // Line 2 is one channel, not two: it carries the window title when
  // there is one, and otherwise the reason there isn't. It is never
  // `hidden` any more — that was the pre-primitive rule.
  it('leaves line 2 blank for a running session with no title', () => {
    withSessions([{ id: 'a', name: 'api', order: 0 }]);
    expect(titleSlot('a').hidden).toBe(false);
    expect(titleSlot('a').textContent).toBe('');
  });

  it('leaves line 2 blank when the title just echoes the session name', () => {
    withSessions([{ id: 'a', name: 'api', order: 0, title: 'api' }]);
    // The slot stays in the DOM and stays shown — suppressing the echo
    // empties line 2, it never re-hides it. Pinned on this path too, not
    // just the no-title one: `title === name` is its own branch in
    // displayTitle() and could regress alone.
    expect(titleSlot('a').hidden).toBe(false);
    expect(titleSlot('a').textContent).toBe('');
  });

  it('shows state words on line 2 when a titleless session is not running', () => {
    withSessions([{ id: 'a', name: 'api', order: 0, phase: 'spawning' }]);
    expect(titleSlot('a').textContent).toBe('Starting…');

    withSessions([{ id: 'a', name: 'api', order: 0, alive: false }]);
    expect(titleSlot('a').textContent).toBe('Exited');

    withSessions([
      { id: 'a', name: 'api', order: 0, alive: false, last_error: 'boom' },
    ]);
    expect(titleSlot('a').textContent).toBe('Exited — boom');
  });

  it('prefers a real window title over the state words', () => {
    withSessions([
      { id: 'a', name: 'api', order: 0, alive: false, title: 'npm run build' },
    ]);
    expect(titleSlot('a').textContent).toBe('npm run build');
  });

  it('updates titles in place without rebuilding the rows', () => {
    withSessions([
      { id: 'a', name: 'api', order: 0, title: 'step one' },
      { id: 'b', name: 'web', order: 1, title: 'step one' },
    ]);
    const before = row('a');
    const beforeSlot = titleSlot('a');

    update(() =>
      store.updateSession({
        ...appStore.getState().sessions[0],
        title: 'step two',
      }),
    );

    // Same nodes, new text. Node identity is the assertion that matters:
    // a rebuild would pass a textContent check and still break dblclick.
    expect(row('a')).toBe(before);
    expect(titleSlot('a')).toBe(beforeSlot);
    expect(beforeSlot.textContent).toBe('step two');
    expect(titleSlot('b').textContent).toBe('step one');
  });

  it('fills and clears the line as a title appears and is suppressed', () => {
    withSessions([{ id: 'a', name: 'api', order: 0 }]);
    expect(titleSlot('a').textContent).toBe('');

    update(() =>
      store.updateSession({
        ...appStore.getState().sessions[0],
        title: 'working',
      }),
    );
    expect(titleSlot('a').textContent).toBe('working');

    // A title that becomes the session name is suppressed again rather
    // than left showing a redundant second line.
    update(() =>
      store.updateSession({ ...appStore.getState().sessions[0], title: 'api' }),
    );
    expect(titleSlot('a').textContent).toBe('');
  });
});

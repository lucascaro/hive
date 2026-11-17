// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// #563: the sidebar scrolls the active session's row into view on every
// change of active session, by the minimum amount, clear of the sticky
// headers. jsdom has no layout, so the geometry is stubbed here and these
// tests pin WHEN the sidebar scrolls and by how much for a given layout.
// Whether the row is really visible is sidebar-reveal.spec.ts's job.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent } from '@testing-library/react';
import {
  loadSidebar,
  mountSidebar,
  projectsUL,
  seed,
  update,
} from './sidebar-harness';
import type { SessionInfo } from '../../src/app/state';
import { appStore } from '../../src/store/store';

const sess = (id: string, pid: string, extra: Partial<SessionInfo> = {}) =>
  ({ id, projectId: pid, name: id, agent: 'claude', ...extra }) as SessionInfo;

const PROJECTS = [
  { id: 'p1', name: 'one', order: 0 },
  { id: 'p2', name: 'two', order: 1 },
];
const SESSIONS = [
  sess('s1', 'p1'),
  sess('s2', 'p1', { worktree_path: '/wt/a', worktree_branch: 'feat/a' }),
  sess('s3', 'p1', { worktree_path: '/wt/a', worktree_branch: 'feat/a' }),
  sess('s4', 'p2'),
];

// The stubbed layout: #projects is a 200px view at y=0; the card header
// is 26px and the group header 20px. Row tops are per sid.
const VIEW = { top: 0, bottom: 200 };
const CARD_H = 26;
const GROUP_H = 20;
let rowTop: Record<string, number>;
let viewHeight: number;
let writes: number[];

const rect = (top: number, bottom: number) =>
  ({
    top,
    bottom,
    left: 0,
    right: 100,
    width: 100,
    height: bottom - top,
    x: 0,
    y: top,
  }) as DOMRect;

beforeEach(() => {
  seed({});
  rowTop = { s1: 30, s2: 60, s3: 90, s4: 500, s9: 700 };
  viewHeight = 200;
  writes = [];
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(
    function (this: Element) {
      if (this.id === 'projects') return rect(VIEW.top, VIEW.bottom);
      const sid = (this as HTMLElement).dataset?.sid;
      const top = sid ? (rowTop[sid] ?? 0) : 0;
      return rect(top, top + 24);
    },
  );
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(
    function (this: HTMLElement) {
      if (this.classList.contains('hv-project-card__header')) return CARD_H;
      if (this.classList.contains('hv-worktree-group__header')) return GROUP_H;
      return 0;
    },
  );
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockImplementation(
    function (this: HTMLElement) {
      return this.id === 'projects' ? viewHeight : 0;
    },
  );
});

afterEach(() => vi.restoreAllMocks());

async function mount(over: Record<string, unknown> = {}) {
  const Sidebar = await loadSidebar();
  // Record scrollTop writes on the scroller. Starts at 100 so an upward
  // reveal lands on a visible number rather than going negative.
  let top = 100;
  Object.defineProperty(projectsUL(), 'scrollTop', {
    configurable: true,
    get: () => top,
    set: (v: number) => {
      writes.push(v);
      top = v;
    },
  });
  seed({ projects: PROJECTS, sessions: SESSIONS, activeId: 's1', ...over });
  mountSidebar(Sidebar);
}

const setActive = (id: string | null) =>
  update(() => appStore.setState({ activeId: id }));

describe('reveal the active row (#563)', () => {
  it('leaves an already visible active row alone on mount', async () => {
    await mount();
    expect(writes).toEqual([]);
  });

  it('reveals the row once when activeId changes', async () => {
    await mount();
    setActive('s4');
    // Bottom edge 524 → 200: scroll down by 324 from 100.
    expect(writes).toEqual([424]);
  });

  it('does not scroll on unrelated store updates', async () => {
    await mount();
    setActive('s4');
    writes = [];
    rowTop.s4 = 900; // the user scrolled away
    update(() =>
      appStore.setState((st) => ({
        sessions: st.sessions.map((s) =>
          s.id === 's1' ? { ...s, name: 'renamed' } : s,
        ),
      })),
    );
    update(() =>
      appStore.setState((st) => ({
        sessions: [...st.sessions, sess('s5', 'p2')],
      })),
    );
    expect(writes).toEqual([]);
  });

  it('waits for a row that renders after the id', async () => {
    await mount();
    setActive('s9');
    expect(writes).toEqual([]);
    update(() =>
      appStore.setState((st) => ({
        sessions: [...st.sessions, sess('s9', 'p2')],
      })),
    );
    expect(writes).toEqual([100 + (724 - 200)]);
    // Consumed: a later render does not scroll again.
    update(() =>
      appStore.setState((st) => ({
        sessions: [...st.sessions, sess('s10', 'p2')],
      })),
    );
    expect(writes).toHaveLength(1);
  });

  it('clears the card header for a plain row', async () => {
    rowTop.s4 = 10;
    await mount();
    setActive('s4');
    expect(writes).toEqual([100 - (CARD_H - 10)]);
  });

  it('clears card + group header for a grouped row, in the body and in the group peek', async () => {
    rowTop.s3 = 30;
    await mount();
    setActive('s3');
    expect(writes).toEqual([100 - (CARD_H + GROUP_H - 30)]);

    // Collapse the group, step away and back: s3 is now in the group peek.
    fireEvent.click(
      document.querySelector('.hv-worktree-group__chevron') as Element,
    );
    expect(document.querySelector('.hv-worktree-group__peek')).not.toBeNull();
    setActive('s1');
    writes = [];
    setActive('s3');
    expect(
      document
        .querySelector('.hv-worktree-group__peek .hv-session-row')
        ?.getAttribute('data-sid'),
    ).toBe('s3');
    expect(writes).toEqual([84 - (CARD_H + GROUP_H - 30)]);
  });

  it('does nothing while the sidebar has no height', async () => {
    viewHeight = 0;
    await mount();
    setActive('s4');
    expect(writes).toEqual([]);
  });
});

// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// @vitest-environment jsdom
//
// #563: a collapsed project card or worktree group stays collapsed, but
// the active session's row is shown on its own beneath the header so the
// selection is never invisible. jsdom applies no stylesheet, so these
// tests assert WHERE the row is (peek list vs the clipped body); the
// Playwright spec sidebar-reveal.spec.ts asserts what is actually visible.
import { describe, it, expect, beforeEach } from 'vitest';
import { fireEvent } from '@testing-library/react';
import {
  card,
  loadSidebar,
  mountSidebar,
  projectsUL,
  seed,
  update,
} from './sidebar-harness';
import type { SessionInfo } from '../../src/app/state';
import { appStore } from '../../src/store/store';

const P1 = { id: 'p1', name: 'one', order: 0 };
const P2 = { id: 'p2', name: 'two', order: 1 };

const sess = (id: string, pid: string, extra: Partial<SessionInfo> = {}) =>
  ({ id, projectId: pid, name: id, agent: 'claude', ...extra }) as SessionInfo;

// s1–s3 in p1, where s2/s3 share a worktree (a two-member group); s4 in p2.
const SESSIONS = [
  sess('s1', 'p1'),
  sess('s2', 'p1', { worktree_path: '/wt/a', worktree_branch: 'feat/a' }),
  sess('s3', 'p1', { worktree_path: '/wt/a', worktree_branch: 'feat/a' }),
  sess('s4', 'p2'),
];

async function mount(over: Record<string, unknown> = {}) {
  const Sidebar = await loadSidebar();
  seed({ projects: [P1, P2], sessions: SESSIONS, ...over });
  mountSidebar(Sidebar);
  return projectsUL();
}

const sids = (el: Element | null | undefined) =>
  Array.from(el?.querySelectorAll<HTMLElement>('.hv-session-row') ?? []).map(
    (r) => r.dataset.sid,
  );
const cardPeek = (pid: string) =>
  card(pid)?.querySelector(':scope > .hv-project-card__peek');
const cardBody = (pid: string) =>
  card(pid)?.querySelector(':scope > .hv-project-card__body');
const group = () =>
  card('p1')?.querySelector<HTMLElement>('.hv-worktree-group') ?? null;
const groupPeek = () =>
  group()?.querySelector(':scope > .hv-worktree-group__peek');
const groupBody = () =>
  group()?.querySelector(':scope > .hv-worktree-group__body');

const setActive = (id: string | null) =>
  update(() => appStore.setState({ activeId: id }));
const toggleGroup = () =>
  fireEvent.click(
    group()?.querySelector('.hv-worktree-group__chevron') as Element,
  );

beforeEach(() => seed({}));

describe('collapsed containers peek the active row (#563)', () => {
  it('collapsed project shows only the active row, outside the body', async () => {
    await mount({ collapsed: new Set(['p1']), activeId: 's1' });
    expect(sids(cardPeek('p1'))).toEqual(['s1']);
    // No hidden duplicate in the clipped body: the reveal looks rows up
    // by data-sid, and a copy there would win.
    expect(sids(cardBody('p1'))).not.toContain('s1');
    expect(sids(cardBody('p1'))).toEqual(['s2', 's3']);
    expect(card('p1')?.hasAttribute('data-collapsed')).toBe(true);
    expect(
      card('p1')
        ?.querySelector('.hv-project-card__chevron')
        ?.getAttribute('aria-expanded'),
    ).toBe('false');
  });

  it('peeks a grouped member flat, without its group, and keeps the group', async () => {
    await mount({ collapsed: new Set(['p1']), activeId: 's2' });
    expect(sids(cardPeek('p1'))).toEqual(['s2']);
    expect(cardPeek('p1')?.querySelector('.hv-worktree-group')).toBeNull();
    // The group is built from every row, so it survives with both members
    // counted even though one of them is drawn elsewhere.
    expect(
      group()?.querySelector('.hv-worktree-group__count')?.textContent,
    ).toBe('2');
    expect(sids(groupBody())).toEqual(['s3']);
    expect(groupPeek()).toBeNull();
  });

  it('peek clears when the active session leaves the project', async () => {
    await mount({ collapsed: new Set(['p1']), activeId: 's1' });
    setActive('s4');
    expect(cardPeek('p1')).toBeNull();
    expect(sids(cardBody('p1'))).toEqual(['s1', 's2', 's3']);
    expect(appStore.getState().collapsed.has('p1')).toBe(true);
    expect(card('p1')?.hasAttribute('data-collapsed')).toBe(true);
  });

  it('collapsed worktree group peeks its active member', async () => {
    await mount({ activeId: 's3' });
    toggleGroup();
    expect(group()?.hasAttribute('data-collapsed')).toBe(true);
    expect(sids(groupPeek())).toEqual(['s3']);
    expect(sids(groupBody())).toEqual(['s2']);
    // Both classes, so the lone row keeps the group's rail and indent.
    expect(groupPeek()?.classList.contains('hv-worktree-group__rows')).toBe(
      true,
    );
    setActive('s1');
    expect(groupPeek()).toBeNull();
    expect(sids(groupBody())).toEqual(['s2', 's3']);
    expect(group()?.hasAttribute('data-collapsed')).toBe(true);
  });

  it('expanded containers render no peek', async () => {
    await mount({ activeId: 's2' });
    expect(cardPeek('p1')).toBeNull();
    expect(groupPeek()).toBeNull();
    expect(sids(cardBody('p1'))).toEqual(['s1', 's2', 's3']);
  });

  it('an active minimized session in a collapsed project still peeks', async () => {
    await mount({
      collapsed: new Set(['p1']),
      minimized: new Set(['s1']),
      activeId: 's1',
    });
    expect(sids(cardPeek('p1'))).toEqual(['s1']);
    expect(
      cardPeek('p1')
        ?.querySelector('[data-minimized]')
        ?.getAttribute('data-sid'),
    ).toBe('s1');
  });

  it('a two-member group keeps its wrapper and collapse state while a member peeks from a collapsed project', async () => {
    await mount({ activeId: 's1' });
    toggleGroup();
    const before = group();
    expect(before?.hasAttribute('data-collapsed')).toBe(true);
    update(() => appStore.setState({ collapsed: new Set(['p1']) }));
    setActive('s2');
    update(() => appStore.setState({ collapsed: new Set() }));
    // Same element (not remounted) and still collapsed.
    expect(group()).toBe(before);
    expect(group()?.hasAttribute('data-collapsed')).toBe(true);
    expect(
      group()?.querySelector('.hv-worktree-group__count')?.textContent,
    ).toBe('2');
    // Card expanded again: now the GROUP peeks its active member.
    expect(sids(groupPeek())).toEqual(['s2']);
  });
});

// @vitest-environment jsdom
//
// Worktree sessions get a panel, not just a cue. The panel exists because
// registry/create.go names a worktree session after its branch without
// uniquifying: two agents on feat/sidebar are byte-identical rows, and the
// only thing that tells them apart — the window title — was on the quiet
// second line. Inside the panel the title IS the line, and the branch is
// stated once at the top.
import { describe, it, expect, beforeEach } from 'vitest';
import { fireEvent } from '@testing-library/react';
import { loadSidebar, mountSidebar, seed, projectsUL } from './sidebar-harness';
import type { SessionInfo } from '../../src/app/state';

const PROJECT = { id: 'p1', name: 'hive', order: 0 };

function sessions(...over: Partial<SessionInfo>[]): SessionInfo[] {
  return over.map(
    (s, i) =>
      ({
        id: `s${i + 1}`,
        projectId: 'p1',
        order: i,
        agent: 'claude',
        color: '#7fb3d5',
        ...s,
      }) as SessionInfo,
  );
}

async function mount(list: SessionInfo[]) {
  const Sidebar = await loadSidebar();
  seed({ projects: [PROJECT], sessions: list });
  mountSidebar(Sidebar);
  return projectsUL();
}

function groups(ul: HTMLElement) {
  return ul.querySelectorAll<HTMLElement>('.hv-worktree-group');
}

beforeEach(() => {
  seed({});
});

describe('worktree group panel', () => {
  it('wraps sessions that share a worktree in one panel', async () => {
    const ul = await mount(
      sessions(
        {
          name: 'feat-sidebar',
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
          title: 'npm run build',
        },
        {
          name: 'feat-sidebar',
          agent: 'codex',
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
          title: 'git rebase -i',
        },
      ),
    );

    expect(groups(ul)).toHaveLength(1);
    const panel = groups(ul)[0];
    expect(
      panel.querySelector('.hv-worktree-group__branch')?.textContent,
    ).toContain('feat/sidebar');
    expect(panel.querySelector('.hv-worktree-group__count')?.textContent).toBe(
      '2',
    );
    expect(panel.querySelectorAll('.hv-session-row')).toHaveLength(2);
    // The group's colour, once, on the panel.
    expect(panel.style.getPropertyValue('--session-color')).toBe('#7fb3d5');
  });

  it('leaves a lone worktree session as a plain row', async () => {
    const ul = await mount(
      sessions({
        name: 'feat-sidebar',
        worktree_path: '/wt/a',
        worktree_branch: 'feat/sidebar',
      }),
    );
    expect(groups(ul)).toHaveLength(0);
    expect(ul.querySelectorAll('.hv-session-row')).toHaveLength(1);
  });

  it('shows the window title instead of the repeated branch name', async () => {
    const ul = await mount(
      sessions(
        {
          name: 'feat-sidebar',
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
          title: 'npm run build',
        },
        {
          name: 'feat-sidebar',
          agent: 'codex',
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
          title: 'git rebase -i',
        },
      ),
    );
    const rows = groups(ul)[0].querySelectorAll<HTMLElement>('.hv-session-row');
    expect(
      [...rows].map(
        (r) => r.querySelector('.hv-session-row__name')?.textContent,
      ),
    ).toEqual(['npm run build', 'git rebase -i']);
    // One line, not two: line 2 held the title and the title is now line 1.
    expect(rows[0].querySelector('.hv-session-row__sub')).toBeNull();
  });

  it('keeps a name the user chose, even inside a group', async () => {
    const ul = await mount(
      sessions(
        {
          name: 'feat-sidebar',
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
          title: 'npm run build',
        },
        {
          name: 'the flaky one',
          agent: 'codex',
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
          title: 'git rebase -i',
        },
      ),
    );
    const rows = groups(ul)[0].querySelectorAll<HTMLElement>('.hv-session-row');
    expect(rows[1].querySelector('.hv-session-row__name')?.textContent).toBe(
      'the flaky one',
    );
    expect(rows[1].querySelector('.hv-session-row__sub')?.textContent).toBe(
      'git rebase -i',
    );
  });

  // subtitleFor() is empty for a running session that has published no
  // window title. A titleOnly row must not then render a blank line — it
  // falls back to the name it would otherwise have hidden.
  it('falls back to the name when a group member has no title', async () => {
    // Ready and alive, so subtitleFor() has no state word to fall back on
    // either — the genuinely empty line 2 this guards against.
    const ul = await mount(
      sessions(
        {
          name: 'feat-sidebar',
          alive: true,
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
        },
        {
          name: 'feat-sidebar',
          agent: 'codex',
          alive: true,
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
        },
      ),
    );
    const names = [
      ...groups(ul)[0].querySelectorAll<HTMLElement>('.hv-session-row__name'),
    ].map((n) => n.textContent);
    expect(names).toEqual(['feat-sidebar', 'feat-sidebar']);
  });

  // Two shells on one worktree get the SAME name — the branch, with no
  // agent to tell them apart (that is why the panel leads with titles).
  it('groups two shell sessions and leads with their titles', async () => {
    const ul = await mount(
      sessions(
        {
          name: 'feat-sidebar',
          agent: '',
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
          title: 'tail -f log',
        },
        {
          name: 'feat-sidebar',
          agent: '',
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
          title: 'htop',
        },
      ),
    );
    const names = [
      ...groups(ul)[0].querySelectorAll<HTMLElement>('.hv-session-row__name'),
    ].map((n) => n.textContent);
    expect(names).toEqual(['tail -f log', 'htop']);
  });

  it('collapses and expands from the header chevron', async () => {
    const ul = await mount(
      sessions(
        {
          name: 'feat-sidebar',
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
        },
        {
          name: 'feat-sidebar',
          agent: 'codex',
          worktree_path: '/wt/a',
          worktree_branch: 'feat/sidebar',
        },
      ),
    );
    const panel = groups(ul)[0];
    const chevron = panel.querySelector<HTMLButtonElement>(
      '.hv-worktree-group__chevron',
    );
    if (!chevron) throw new Error('no chevron');
    expect(chevron.getAttribute('aria-expanded')).toBe('true');
    fireEvent.click(chevron);
    expect(panel.hasAttribute('data-collapsed')).toBe(true);
    expect(chevron.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(chevron);
    expect(panel.hasAttribute('data-collapsed')).toBe(false);
  });
});

// @vitest-environment jsdom
//
// Naming a worktree group from its header (issue #395).
//
// The invariant under test, and the one this feature was redesigned
// around: naming the GROUP must never rename its SESSIONS. An earlier
// design fanned the typed name out over every member still carrying the
// branch-derived default; it was withdrawn because it worked exactly
// once (after the first rename no member carries the default any more)
// and because it could clobber a session the operator had named by hand.
// The assertions that `renameCalls` stays empty are the guard against
// that design creeping back in.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { fireEvent } from '@testing-library/react';
import { inlineRenameActive } from '../../src/app/inline-rename.js';
import type { SessionInfo } from '../../src/app/state.js';
import { appStore } from '../../src/store/store.js';
import * as store from '../../src/store/store.js';
import { loadSidebar, mountSidebar, seed, update } from './sidebar-harness.js';

// Every UpdateSession call the sidebar makes. For this feature the
// correct value is always zero.
const renameCalls: Array<[string, string]> = [];
const labelCalls: Array<[string, string, string]> = [];

vi.mock('../../src/bridge.js', async (orig) => {
  const real = (await orig()) as Record<string, unknown>;
  return {
    ...real,
    UpdateSession: (id: string, name: string) => {
      renameCalls.push([id, name]);
      return Promise.resolve();
    },
    SetWorktreeLabel: (projectID: string, path: string, label: string) => {
      labelCalls.push([projectID, path, label]);
      return Promise.resolve();
    },
  };
});

let Sidebar: Awaited<ReturnType<typeof loadSidebar>>;

const WT = '/repo/.worktrees/feat-sidebar';
const BRANCH = 'feat/sidebar';
// What registry create.go names a worktree session: the branch with
// slashes flattened. Members holding this render `titleOnly`.
const DEFAULT_NAME = 'feat-sidebar';

// Two sessions sharing a worktree — the minimum that paints a group.
function groupSeed(
  labels?: Record<string, string>,
  secondName = DEFAULT_NAME,
  extra: Partial<SessionInfo> = {},
) {
  seed({
    projects: [
      { id: 'p1', name: 'proj', color: '#888', worktree_labels: labels },
    ],
    sessions: [
      {
        id: 'a',
        name: DEFAULT_NAME,
        project_id: 'p1',
        order: 0,
        alive: true,
        worktree_path: WT,
        worktree_branch: BRANCH,
        title: 'npm run dev',
      },
      {
        id: 'b',
        name: secondName,
        project_id: 'p1',
        order: 1,
        alive: true,
        worktree_path: WT,
        worktree_branch: BRANCH,
        title: 'npm test',
        ...extra,
      },
    ],
    collapsed: new Set(),
    activeId: null,
  });
  mountSidebar(Sidebar);
}

beforeAll(async () => {
  Sidebar = await loadSidebar();
});

beforeEach(() => {
  renameCalls.length = 0;
  labelCalls.length = 0;
});

const title = () =>
  document.querySelector<HTMLElement>('.hv-worktree-group__title');
const branchEl = () =>
  document.querySelector<HTMLElement>('.hv-worktree-group__branch');
const labelEl = () =>
  document.querySelector<HTMLElement>('.hv-worktree-group__label');
const editor = () =>
  document.querySelector<HTMLInputElement>('input.group-name-input');

function openEditor(): HTMLInputElement {
  const t = title();
  expect(t).not.toBeNull();
  if (!t) throw new Error('no title cell');
  fireEvent.dblClick(t);
  const input = editor();
  expect(input).not.toBeNull();
  if (!input) throw new Error('editor did not open');
  return input;
}

describe('worktree group rename', () => {
  it('seeds the editor with the branch when the group has no name', () => {
    groupSeed();
    expect(openEditor().value).toBe(BRANCH);
  });

  it('seeds the editor with the name once the group has one', () => {
    groupSeed({ [WT]: 'auth refactor' });
    expect(openEditor().value).toBe('auth refactor');
  });

  it('commits the name to the group, not to any session', () => {
    groupSeed();
    const input = openEditor();
    input.value = 'auth refactor';
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(labelCalls).toEqual([['p1', WT, 'auth refactor']]);
    // THE assertion. Naming a group renames nothing.
    expect(renameCalls).toEqual([]);
  });

  it('leaves every member name byte-identical', () => {
    groupSeed(undefined, 'hand-named');
    const before = appStore.getState().sessions.map((s) => s.name);
    const input = openEditor();
    input.value = 'auth refactor';
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(appStore.getState().sessions.map((s) => s.name)).toEqual(before);
    expect(renameCalls).toEqual([]);
  });

  // The one-shot regression from the withdrawn design: a second rename
  // must work exactly like the first. With the label as real state there
  // is nothing that can be "used up", and this pins that.
  it('is repeatable', () => {
    groupSeed();
    const first = openEditor();
    first.value = 'auth refactor';
    fireEvent.keyDown(first, { key: 'Enter' });

    // The daemon answers with a project event carrying the new label.
    update(() =>
      store.updateProject({
        id: 'p1',
        name: 'proj',
        color: '#888',
        worktree_labels: { [WT]: 'auth refactor' },
      }),
    );

    const second = openEditor();
    expect(second.value).toBe('auth refactor');
    second.value = 'auth v2';
    fireEvent.keyDown(second, { key: 'Enter' });

    expect(labelCalls).toEqual([
      ['p1', WT, 'auth refactor'],
      ['p1', WT, 'auth v2'],
    ]);
    expect(renameCalls).toEqual([]);
  });

  it('clears the name when the field is emptied', () => {
    groupSeed({ [WT]: 'auth refactor' });
    const input = openEditor();
    input.value = '';
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(labelCalls).toEqual([['p1', WT, '']]);
  });

  // Seeding from the branch means emptying the field on an ALREADY
  // unnamed group reads as "changed" to inline-rename and reaches
  // onCommit. Sending it would be a daemon write, a project.json
  // persist and an all-window broadcast that change nothing.
  it('sends nothing when an already-unnamed group is cleared', () => {
    groupSeed();
    const input = openEditor();
    input.value = '';
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(labelCalls).toEqual([]);
    expect(renameCalls).toEqual([]);
  });

  // The daemon rejects an over-cap name rather than truncating it, and
  // the editor tears down on commit — so without a client guard the name
  // the user typed is lost and all they get is a raw error code. The
  // editor must stay open holding exactly what they typed.
  it('refuses an over-long name and keeps the editor open on it', () => {
    groupSeed();
    const input = openEditor();
    const tooLong = 'x'.repeat(201);
    input.value = tooLong;
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(labelCalls).toEqual([]);
    expect(renameCalls).toEqual([]);
    const still = editor();
    expect(still).not.toBeNull();
    expect(still?.value).toBe(tooLong);
  });

  it('sends nothing on Escape and restores the title', () => {
    groupSeed();
    const input = openEditor();
    input.value = 'discarded';
    fireEvent.keyDown(input, { key: 'Escape' });

    expect(labelCalls).toEqual([]);
    expect(renameCalls).toEqual([]);
    expect(editor()).toBeNull();
    expect(branchEl()?.textContent).toContain(BRANCH);
  });

  it('shows the name and the branch together, name first', () => {
    groupSeed({ [WT]: 'auth refactor' });
    expect(labelEl()?.textContent).toBe('auth refactor');
    expect(branchEl()?.textContent).toContain(BRANCH);
    // The branch is never dropped: stating it is the panel's whole job.
    const t = title();
    expect(t?.textContent).toContain('auth refactor');
    expect(t?.textContent).toContain(BRANCH);
  });

  it('shows only the branch when the group has no name', () => {
    groupSeed();
    expect(labelEl()).toBeNull();
    expect(branchEl()?.textContent).toContain(BRANCH);
  });

  // titleOnly hides a member's name when it is still the branch-derived
  // default. Naming the group must not disturb that — it is what keeps
  // the member rows readable, and it is untouched by design.
  it('leaves titleOnly members rendering their window title', () => {
    groupSeed({ [WT]: 'auth refactor' });
    const names = Array.from(
      document.querySelectorAll('.hv-session-row__name'),
    ).map((n) => n.textContent);
    expect(names).toContain('npm run dev');
    expect(names).toContain('npm test');
    expect(names).not.toContain(DEFAULT_NAME);
  });

  // The branch name carries an <Icon> svg, so a double-click can land on
  // the icon rather than on the text. An identity-based target guard —
  // the one the project header uses — would reject it.
  it('opens from a double-click on the branch icon', () => {
    groupSeed();
    const icon = document.querySelector('.hv-worktree-group__branch svg');
    expect(icon).not.toBeNull();
    if (!icon) return;
    fireEvent.dblClick(icon);
    expect(editor()).not.toBeNull();
  });

  // The guard is an allowlist on `.hv-worktree-group__title`, so every
  // header control outside that cell must be inert. Asserting the element
  // exists first is load-bearing: an `if (el)` would turn a renamed
  // selector into a silently passing test that dblclicks nothing.
  it('does not open from the chevron or the member count', () => {
    groupSeed();
    const chevron = document.querySelector('.hv-worktree-group__chevron');
    expect(chevron, 'no chevron to click; selector drifted').not.toBeNull();
    if (chevron) fireEvent.dblClick(chevron);
    expect(editor()).toBeNull();

    const count = document.querySelector('.hv-worktree-group__count');
    expect(count, 'no member count to click; selector drifted').not.toBeNull();
    if (count) fireEvent.dblClick(count);
    expect(editor()).toBeNull();
  });

  // The third control the spec names, and the one the merge gate flagged
  // as covered only by the shared structural guard. It renders solely
  // while the group is COLLAPSED and a member wants attention, so the
  // fixture has to produce both before the assertion means anything.
  it('does not open from the collapsed-state attention badge', () => {
    groupSeed(undefined, DEFAULT_NAME, { needs_attention: true });

    const chevron = document.querySelector('.hv-worktree-group__chevron');
    expect(chevron, 'no chevron to collapse with').not.toBeNull();
    if (chevron) fireEvent.click(chevron);

    const alert = document.querySelector('.hv-worktree-group__alert');
    expect(
      alert,
      'no attention badge rendered; fixture did not reach the state under test',
    ).not.toBeNull();
    if (alert) fireEvent.dblClick(alert);
    expect(editor()).toBeNull();
  });

  // The <li> is keyed by worktree path and unmounts outright once the run
  // drops below two members, taking the input out of the DOM with it.
  //
  // Asserting the input is gone from the DOM would be VACUOUS — React
  // removes the subtree either way. What leaks without the cleanup is the
  // module-level registry in inline-rename.ts: it still believes an edit
  // is open, so keyboard.ts's first ladder branch swallows every key in
  // the app and Escape goes to a rename nobody can see. That is the
  // assertion.
  it('cancels the editor when its group loses a member mid-edit', () => {
    groupSeed();
    const input = openEditor();
    input.value = 'auth refactor';
    expect(inlineRenameActive()).toBe(true);

    update(() => store.removeSession('b'));

    expect(inlineRenameActive()).toBe(false);
    expect(editor()).toBeNull();
    expect(labelCalls).toEqual([]);
    expect(renameCalls).toEqual([]);
  });
});

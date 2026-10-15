// @vitest-environment jsdom
//
// The archived-sessions list (src/app/modals/archived-sessions.ts,
// rendered by src/components/modals/ArchivedSessions.tsx) and the flows
// in src/app/archive.ts. What this pins down:
//
//   • opening asks the daemon for a fresh list, and every archived
//     session is listed, grouped under its snapshotted project name
//   • the filter narrows by name/project, and says so when nothing matches
//   • Enter unarchives the highlighted row, ↑/↓ move it
//   • Delete is gated by the confirm: cancel sends nothing, "keep worktree"
//     never asks for the worktree to go
//   • an unarchive reports what was lost on the status line, and only when
//     THIS window asked for it
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import { resetStore, setArchived } from '../../src/store/store.js';
import type { ArchivedSessionInfo } from '../../src/app/state.js';

const ListArchivedSessions = vi.fn((): Promise<void> => Promise.resolve());
const UnarchiveSession = vi.fn(
  (_id: string): Promise<void> => Promise.resolve(),
);
const ArchiveSession = vi.fn((_id: string): Promise<void> => Promise.resolve());
const DeleteArchivedSession = vi.fn(
  (_id: string, _rm: boolean, _force: boolean): Promise<void> =>
    Promise.resolve(),
);
const flashStatus = vi.fn();

vi.mock('../../src/bridge.js', () => ({
  ListArchivedSessions: () => ListArchivedSessions(),
  UnarchiveSession: (...a: Parameters<typeof UnarchiveSession>) =>
    UnarchiveSession(...a),
  ArchiveSession: (...a: Parameters<typeof ArchiveSession>) =>
    ArchiveSession(...a),
  DeleteArchivedSession: (...a: Parameters<typeof DeleteArchivedSession>) =>
    DeleteArchivedSession(...a),
}));
vi.mock('../../src/app/dom.js', () => ({
  flashStatus: (...a: unknown[]) => flashStatus(...a),
  setStatus: vi.fn(),
  reportFailure: () => () => {},
}));

const MARKUP = `
  <div id="app">
    <div id="archived-sessions" class="hv-dialog hidden" role="dialog"
      aria-modal="true" aria-labelledby="archived-sessions-title"></div>
    <div id="choice-dialog" class="hv-dialog hidden" role="alertdialog"
      aria-modal="true" aria-labelledby="choice-dialog-title"></div>
  </div>`;

type ModalModule = typeof import('../../src/app/modals/archived-sessions.js');
type ArchiveModule = typeof import('../../src/app/archive.js');
let openArchivedSessions: ModalModule['openArchivedSessions'];
let initArchivedSessions: ModalModule['initArchivedSessions'];
let archive: ArchiveModule;
let refocusActiveTerm: Mock<() => void>;
let ArchivedSessions: typeof import('../../src/components/modals/ArchivedSessions.js')['ArchivedSessions'];
let ChoiceDialog: typeof import('../../src/components/modals/ChoiceDialog.js')['ChoiceDialog'];

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ openArchivedSessions, initArchivedSessions } = await import(
    '../../src/app/modals/archived-sessions.js'
  ));
  archive = await import('../../src/app/archive.js');
  ({ ArchivedSessions } = await import(
    '../../src/components/modals/ArchivedSessions.js'
  ));
  ({ ChoiceDialog } = await import(
    '../../src/components/modals/ChoiceDialog.js'
  ));
  refocusActiveTerm = vi.fn();
  initArchivedSessions({ setFocusedTile: vi.fn(), refocusActiveTerm });
});

beforeEach(() => {
  for (const m of [
    ListArchivedSessions,
    UnarchiveSession,
    ArchiveSession,
    DeleteArchivedSession,
    flashStatus,
  ]) {
    m.mockReset();
    m.mockResolvedValue?.(undefined as never);
  }
  archive.resetArchiveForTest();
  refocusActiveTerm.mockReset();
  resetStore();
  render(<ArchivedSessions root={el('archived-sessions')} />, {
    container: el('archived-sessions'),
  });
  render(<ChoiceDialog root={el('choice-dialog')} />, {
    container: el('choice-dialog'),
  });
});

function el<T extends HTMLElement = HTMLElement>(id: string): T {
  return document.getElementById(id) as T;
}
const rows = () => [
  ...el('archived-sessions').querySelectorAll<HTMLElement>('.archived-row'),
];
const names = () =>
  rows().map((r) => r.querySelector('.archived-name')?.textContent);
const button = (scope: Element | Document, label: string) =>
  [...scope.querySelectorAll('button')].find(
    (b) => b.textContent === label,
  ) as HTMLButtonElement;
const flush = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });

function arch(over: Partial<ArchivedSessionInfo>): ArchivedSessionInfo {
  return {
    session_id: 's1',
    name: 'api refactor',
    agent: 'claude',
    project_id: 'p1',
    project_name: 'hive',
    archived_at: new Date(Date.now() - 3 * 86400_000).toISOString(),
    ...over,
  };
}
const LIST = [
  arch({ session_id: 's1', name: 'api refactor' }),
  arch({
    session_id: 's2',
    name: 'docs pass',
    project_id: 'p2',
    project_name: 'site',
    worktree_branch: 'feat/docs',
  }),
  arch({ session_id: 's3', name: 'api tests' }),
];

function open() {
  act(() => openArchivedSessions());
}

describe('opening', () => {
  it('asks the daemon for a fresh list and shows the empty state', () => {
    open();
    expect(ListArchivedSessions).toHaveBeenCalledTimes(1);
    expect(el('archived-sessions-empty')).toBeTruthy();
    expect(el('archived-sessions').classList.contains('hidden')).toBe(false);
  });

  it('lists every archived session grouped under its snapshotted project name', () => {
    act(() => setArchived(LIST));
    open();
    expect(names()).toEqual(['api refactor', 'api tests', 'docs pass']);
    const groups = [
      ...el('archived-sessions').querySelectorAll('.archived-group-name'),
    ].map((h) => h.textContent);
    expect(groups).toEqual(['hive', 'site']);
    expect(rows()[0].textContent).toContain('archived 3d ago');
  });
});

describe('filtering', () => {
  it('narrows by name or project and explains an empty result', () => {
    act(() => setArchived(LIST));
    open();
    const input = el<HTMLInputElement>('archived-sessions-input');
    fireEvent.change(input, { target: { value: 'api' } });
    expect(names()).toEqual(['api refactor', 'api tests']);
    fireEvent.change(input, { target: { value: 'site' } });
    expect(names()).toEqual(['docs pass']);
    fireEvent.change(input, { target: { value: 'zzz' } });
    expect(rows()).toHaveLength(0);
    expect(el('archived-sessions-nomatch').textContent).toContain('zzz');
  });
});

describe('unarchiving', () => {
  it('Enter unarchives the highlighted row and ↓ moves the highlight', () => {
    act(() => setArchived(LIST));
    open();
    const root = el('archived-sessions');
    fireEvent.keyDown(root, { key: 'ArrowDown' });
    fireEvent.keyDown(root, { key: 'Enter' });
    expect(UnarchiveSession).toHaveBeenCalledWith('s3');
    expect(root.classList.contains('hidden')).toBe(true);
  });

  it('the Unarchive button restores that row', () => {
    act(() => setArchived(LIST));
    open();
    fireEvent.click(button(rows()[2], 'Unarchive'));
    expect(UnarchiveSession).toHaveBeenCalledWith('s2');
  });

  it('reports losses on the status line, only for an unarchive this window asked for', () => {
    act(() => setArchived(LIST));
    expect(archive.onUnarchived({ session_id: 's1' })).toBe(false);
    archive.unarchiveSession('s1');
    expect(
      archive.onUnarchived({
        session_id: 's1',
        conversation_lost: true,
        worktree_recreated: true,
      }),
    ).toBe(true);
    const text = String(flashStatus.mock.calls.at(-1)?.[0]);
    expect(text).toContain('Unarchived “api refactor”');
    expect(text).toContain('the agent started a new conversation');
    expect(text).toContain('rebuilt from its branch');
  });
});

describe('deleting', () => {
  async function pressDelete(row: number) {
    open();
    fireEvent.click(button(rows()[row], 'Delete'));
    await flush();
  }
  const choice = (label: string) => button(el('choice-dialog'), label);

  it('Cancel sends nothing', async () => {
    act(() => setArchived(LIST));
    await pressDelete(0);
    fireEvent.click(choice('Cancel'));
    await flush();
    expect(DeleteArchivedSession).not.toHaveBeenCalled();
  });

  it('"keep worktree" discards the record only', async () => {
    act(() => setArchived(LIST));
    await pressDelete(2);
    fireEvent.click(choice('Delete, keep worktree'));
    await flush();
    expect(DeleteArchivedSession).toHaveBeenCalledWith('s2', false, false);
  });

  it('offers worktree removal only for a session that has a worktree', async () => {
    act(() => setArchived(LIST));
    await pressDelete(0); // api refactor: no worktree
    expect(choice('Delete and remove worktree')).toBeUndefined();
    fireEvent.click(choice('Cancel'));
    await flush();
    fireEvent.click(button(rows()[2], 'Delete')); // docs pass: has one
    await flush();
    fireEvent.click(choice('Delete and remove worktree'));
    await flush();
    expect(DeleteArchivedSession).toHaveBeenCalledWith('s2', true, false);
  });

  it('a dirty-worktree refusal asks again and only then forces', async () => {
    act(() => setArchived(LIST));
    const p = archive.onArchivedWorktreeDirty('s2');
    await flush();
    expect(el('choice-dialog').textContent).toContain('uncommitted changes');
    fireEvent.click(choice('Cancel'));
    await p;
    expect(DeleteArchivedSession).not.toHaveBeenCalled();

    const p2 = archive.onArchivedWorktreeDirty('s2');
    await flush();
    fireEvent.click(choice('Delete and remove worktree'));
    await p2;
    expect(DeleteArchivedSession).toHaveBeenCalledWith('s2', true, true);
  });
});

describe('archiving', () => {
  it('archives without any confirmation and reports it once the list lands', async () => {
    resetStore({
      sessions: [{ id: 'live1', name: 'api refactor' }] as never,
      activeId: 'live1',
    });
    expect(archive.archiveActiveSession()).toBeUndefined();
    expect(ArchiveSession).toHaveBeenCalledWith('live1');
    expect(el('choice-dialog').classList.contains('hidden')).toBe(true);

    act(() => archive.onArchivedList([arch({ session_id: 'live1' })]));
    expect(String(flashStatus.mock.calls.at(-1)?.[0])).toContain(
      'Archived “api refactor”',
    );
    // Not announced twice.
    flashStatus.mockClear();
    act(() => archive.onArchivedList([arch({ session_id: 'live1' })]));
    expect(flashStatus).not.toHaveBeenCalled();
  });

  it('declines with nothing focused', () => {
    expect(archive.archiveActiveSession()).toBe(false);
    expect(ArchiveSession).not.toHaveBeenCalled();
  });

  it('maps the daemon refusals to words and ignores unrelated codes', () => {
    expect(archive.onArchiveError('archive_busy')).toBe(true);
    expect(String(flashStatus.mock.calls.at(-1)?.[0])).toContain(
      'still starting or closing',
    );
    expect(archive.onArchiveError('worktree_dirty')).toBe(false);
    expect(archive.onArchiveError(undefined)).toBe(false);
  });

  it('leaves shared codes to other features when no archive request is pending', () => {
    // An ACP prompt to a closed session raises no_such_session with its
    // id; archive must not swallow it before applyAcpError runs.
    expect(archive.onArchiveError('no_such_session', 'acp1')).toBe(false);
    expect(archive.onArchiveError('session_already_open')).toBe(false);
  });

  it('claims no_such_session for its own pending archive only', () => {
    resetStore({
      sessions: [{ id: 'live1', name: 'api refactor' }] as never,
      activeId: 'live1',
    });
    archive.archiveSession('live1');
    expect(archive.onArchiveError('no_such_session', 'other')).toBe(false);
    expect(archive.onArchiveError('no_such_session')).toBe(true);
    // The pending request is cleared, so a second one is not claimed.
    expect(archive.onArchiveError('no_such_session')).toBe(false);
  });
});

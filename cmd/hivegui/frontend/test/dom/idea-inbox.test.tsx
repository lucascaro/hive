// @vitest-environment jsdom
//
// Covers the ⇧⌘I inbox (src/app/modals/idea-inbox.ts, rendered by
// src/components/modals/IdeaInbox.tsx) and the choice dialog its delete
// goes through. What this pins down:
//
//   • the list is this project's OPEN ideas, newest first
//   • Done marks done rather than deleting — the note survives
//   • Delete is gated by the confirm, and cancelling sends nothing
//   • Edit hands the row to the capture sheet, and the module-boundary
//     guards refuse a blank or oversize correction
//   • Start session opens the launcher with the prompt, the project
//     pinned and the idea id along for the daemon to link
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import { resetStore, setIdeas } from '../../src/store/store.js';
import type { IdeaInfo } from '../../src/app/state.js';

const ListIdeas = vi.fn((_p: string): Promise<void> => Promise.resolve());
const UpdateIdea = vi.fn(
  (
    _id: string,
    _t: string,
    _s: string,
    _sess: string,
    _kind: string,
    _project: string,
  ): Promise<void> => Promise.resolve(),
);
const RemoveIdea = vi.fn((_id: string): Promise<void> => Promise.resolve());
const flashStatus = vi.fn();

vi.mock('../../src/bridge.js', () => ({
  ListIdeas: (...a: Parameters<typeof ListIdeas>) => ListIdeas(...a),
  UpdateIdea: (...a: Parameters<typeof UpdateIdea>) => UpdateIdea(...a),
  RemoveIdea: (...a: Parameters<typeof RemoveIdea>) => RemoveIdea(...a),
}));

vi.mock('../../src/app/dom.js', () => ({
  flashStatus: (...a: unknown[]) => flashStatus(...a),
  setStatus: vi.fn(),
  reportFailure: () => () => {},
}));

// The launcher drags in the focus pipeline and the agent list; Start
// session only needs to know what it was asked to open.
const openLauncher = vi.fn();
vi.mock('../../src/app/modals/launcher.js', () => ({
  openLauncher: (...a: unknown[]) => openLauncher(...a),
}));

// switchTo pulls in the whole view/terminal pipeline; the inbox only
// needs to know it was called.
const switchTo = vi.fn();
vi.mock('../../src/app/view.js', () => ({
  switchTo: (...a: unknown[]) => switchTo(...a),
}));

const MARKUP = `
  <div id="app">
    <div id="idea-inbox" class="hv-dialog hidden" role="dialog"
      aria-modal="true" aria-labelledby="idea-inbox-title"></div>
    <div id="choice-dialog" class="hv-dialog hidden" role="alertdialog"
      aria-modal="true" aria-labelledby="choice-dialog-title"></div>
  </div>`;

type InboxModule = typeof import('../../src/app/modals/idea-inbox.js');
let openIdeaInbox: InboxModule['openIdeaInbox'];
let initIdeaInbox: InboxModule['initIdeaInbox'];
let refocusActiveTerm: Mock<() => void>;
let setFocusedTile: Mock<(id: string | null) => void>;
let IdeaInbox: typeof import('../../src/components/modals/IdeaInbox.js')['IdeaInbox'];
let ChoiceDialog: typeof import('../../src/components/modals/ChoiceDialog.js')['ChoiceDialog'];

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ openIdeaInbox, initIdeaInbox } = await import(
    '../../src/app/modals/idea-inbox.js'
  ));
  ({ IdeaInbox } = await import('../../src/components/modals/IdeaInbox.js'));
  ({ ChoiceDialog } = await import(
    '../../src/components/modals/ChoiceDialog.js'
  ));
  refocusActiveTerm = vi.fn();
  setFocusedTile = vi.fn();
  initIdeaInbox({ setFocusedTile, refocusActiveTerm });
});

beforeEach(() => {
  for (const m of [
    ListIdeas,
    UpdateIdea,
    RemoveIdea,
    flashStatus,
    switchTo,
    openLauncher,
  ]) {
    m.mockReset();
  }
  ListIdeas.mockResolvedValue(undefined);
  UpdateIdea.mockResolvedValue(undefined);
  RemoveIdea.mockResolvedValue(undefined);
  refocusActiveTerm.mockReset();
  setFocusedTile.mockReset();
  resetStore();
  render(<IdeaInbox root={el('idea-inbox')} />, {
    container: el('idea-inbox'),
  });
  render(<ChoiceDialog root={el('choice-dialog')} />, {
    container: el('choice-dialog'),
  });
});

function el<T extends HTMLElement = HTMLElement>(id: string): T {
  return document.getElementById(id) as T;
}
const rows = () => [
  ...(document
    .getElementById('idea-inbox-list')
    ?.querySelectorAll<HTMLElement>('.idea-row') ?? []),
];
// Buttons are found by their label, which is also what the user clicks.
const button = (scope: Element | Document, label: string) =>
  [...scope.querySelectorAll('button')].find(
    (b) => b.textContent === label,
  ) as HTMLButtonElement;
const flush = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });

const PROJECT = { id: 'p1', name: 'hive' };

function idea(over: Partial<IdeaInfo> = {}): IdeaInfo {
  return {
    id: 'i1',
    project_id: 'p1',
    kind: 'idea',
    text: 'the grid loses focus',
    status: 'open',
    created: '2026-09-05T10:00:00Z',
    updated: '2026-09-05T10:00:00Z',
    ...over,
  };
}

async function openWith(list: IdeaInfo[]) {
  setIdeas(list);
  await act(async () => {
    openIdeaInbox(PROJECT);
  });
}

describe('idea inbox', () => {
  it('lists this project’s open ideas, newest first', async () => {
    await openWith([
      idea({ id: 'a', text: 'older', created: '2026-09-01T10:00:00Z' }),
      idea({ id: 'b', text: 'newer', created: '2026-09-04T10:00:00Z' }),
      idea({ id: 'c', text: 'done already', status: 'done' }),
      idea({ id: 'd', text: 'other project', project_id: 'p2' }),
    ]);
    expect(rows().map((r) => r.dataset.id)).toEqual(['b', 'a']);
  });

  it('says so when there is nothing to triage', async () => {
    await openWith([]);
    expect(el('idea-inbox-empty')).not.toBeNull();
  });

  it('Done marks the idea done instead of deleting it', async () => {
    await openWith([idea()]);
    fireEvent.click(button(rows()[0], 'Done'));
    await flush();
    expect(UpdateIdea).toHaveBeenCalledWith('i1', '', 'done', '', '', '');
    expect(RemoveIdea).not.toHaveBeenCalled();
  });

  it('gates Delete behind the confirm, and cancelling sends nothing', async () => {
    await openWith([idea()]);
    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    fireEvent.click(button(el('choice-dialog'), 'Cancel'));
    await flush();
    expect(RemoveIdea).not.toHaveBeenCalled();

    fireEvent.click(button(rows()[0], 'Delete'));
    await flush();
    fireEvent.click(button(el('choice-dialog'), 'Delete'));
    await flush();
    expect(RemoveIdea).toHaveBeenCalledWith('i1');
  });

  it('Edit hands the row to the capture sheet, pre-filled', async () => {
    const { modalEntry } = await import('../../src/store/store.js');
    await openWith([idea({ kind: 'bug', project_id: 'p1' })]);
    fireEvent.click(button(rows()[0], 'Edit'));
    await flush();
    const entry = modalEntry('quick-idea');
    expect(entry).toBeTruthy();
    expect(entry?.projectId).toBe('p1');
    expect(entry?.idea?.id).toBe('i1');
    // The inbox gives way: this app never stacks two dialogs, and a
    // sheet layered under the panel has no clickable controls.
    expect(modalEntry('idea-inbox')).toBeFalsy();
  });

  it('refuses a blank correction at the module boundary', async () => {
    // The sheet's Save is disabled on an empty field, so the row-level
    // path passes whether or not this guard exists. Call it directly:
    // it is what stands between a stray commit and a blanked note.
    const { saveIdeaEdit } = await import('../../src/app/modals/idea-inbox.js');
    expect(saveIdeaEdit('i1', '   ', 'idea', 'p1')).toBe(false);
    expect(saveIdeaEdit('i1', '', 'idea', 'p1')).toBe(false);
    expect(UpdateIdea).not.toHaveBeenCalled();
    expect(saveIdeaEdit('i1', '  kept  ', 'bug', 'p2')).toBe(true);
    expect(UpdateIdea).toHaveBeenCalledWith('i1', 'kept', '', '', 'bug', 'p2');
  });

  it('refuses a correction past the 4 KiB cap', async () => {
    const { saveIdeaEdit } = await import('../../src/app/modals/idea-inbox.js');
    const { MAX_IDEA_TEXT } = await import('../../src/lib/ideas.js');
    // Not sent — the daemon rejects rather than truncates and nothing
    // here awaits the answer, so the text would simply be gone.
    expect(
      saveIdeaEdit('i1', 'x'.repeat(MAX_IDEA_TEXT + 1), 'idea', 'p1'),
    ).toBe(false);
    expect(UpdateIdea).not.toHaveBeenCalled();
    expect(flashStatus).toHaveBeenCalledWith(
      expect.stringContaining('too long'),
      true,
    );
  });

  it('Start session opens the launcher with the prompt and the project pinned', async () => {
    const { modalEntry } = await import('../../src/store/store.js');
    await openWith([idea({ kind: 'bug', text: 'sidebar is 1px off' })]);
    fireEvent.click(button(rows()[0], 'Start session'));
    await flush();
    expect(openLauncher).toHaveBeenCalledWith('p1', {
      // An instruction, with the note as its subject — see ideaPrompt.
      initialPrompt: expect.stringContaining('sidebar is 1px off'),
      ideaId: 'i1',
      lockProject: true,
    });
    expect(openLauncher.mock.calls[0][1].initialPrompt).toContain(
      'find the root cause',
    );
    // The launcher anchors under the project's card, which is behind
    // this panel.
    expect(modalEntry('idea-inbox')).toBeFalsy();
    // Nothing is flipped from here: the daemon links the idea once the
    // prompt has actually been delivered.
    expect(UpdateIdea).not.toHaveBeenCalled();
  });

  it('offers Start session only while the idea has no live session', async () => {
    const { setSessions } = await import('../../src/store/store.js');
    await openWith([idea({ status: 'started', session_id: 's9' })]);
    // The link to the session is right there in the row; a second way
    // to start the same note beside it is just confusing.
    act(() => {
      setSessions([
        {
          id: 's9',
          name: 'working on it',
          project_id: 'p1',
        } as unknown as Parameters<typeof setSessions>[0][0],
      ]);
    });
    expect(button(rows()[0], 'Start session')).toBeUndefined();
  });
});

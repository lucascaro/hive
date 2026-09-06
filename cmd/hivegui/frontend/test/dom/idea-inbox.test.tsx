// @vitest-environment jsdom
//
// Covers the ⇧⌘I inbox (src/app/modals/idea-inbox.ts, rendered by
// src/components/modals/IdeaInbox.tsx) and the choice dialog its delete
// goes through. What this pins down:
//
//   • the list is this project's OPEN ideas, newest first
//   • Done marks done rather than deleting — the note survives
//   • Delete is gated by the confirm, and cancelling sends nothing
//   • the inline edit commits through UpdateIdea and an emptied field
//     does not destroy the note
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import { resetStore, setIdeas } from '../../src/store/store.js';
import type { IdeaInfo } from '../../src/app/state.js';

const ListIdeas = vi.fn((_p: string): Promise<void> => Promise.resolve());
const UpdateIdea = vi.fn(
  (_id: string, _t: string, _s: string, _sess: string): Promise<void> =>
    Promise.resolve(),
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
let IdeaInbox: (typeof import('../../src/components/modals/IdeaInbox.js'))['IdeaInbox'];
let ChoiceDialog: (typeof import('../../src/components/modals/ChoiceDialog.js'))['ChoiceDialog'];
let inlineRenameActive: (typeof import('../../src/app/inline-rename.js'))['inlineRenameActive'];

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ openIdeaInbox, initIdeaInbox } = await import(
    '../../src/app/modals/idea-inbox.js'
  ));
  ({ IdeaInbox } = await import('../../src/components/modals/IdeaInbox.js'));
  ({ ChoiceDialog } = await import(
    '../../src/components/modals/ChoiceDialog.js'
  ));
  ({ inlineRenameActive } = await import('../../src/app/inline-rename.js'));
  refocusActiveTerm = vi.fn();
  setFocusedTile = vi.fn();
  initIdeaInbox({ setFocusedTile, refocusActiveTerm });
});

beforeEach(() => {
  for (const m of [ListIdeas, UpdateIdea, RemoveIdea, flashStatus, switchTo]) {
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
    expect(UpdateIdea).toHaveBeenCalledWith('i1', '', 'done', '');
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

  it('commits an inline edit through UpdateIdea', async () => {
    await openWith([idea()]);
    fireEvent.click(button(rows()[0], 'Edit'));
    await flush();
    // The shared imperative editor owns the keyboard while it is up,
    // which is what makes Escape cancel the edit rather than close the
    // panel (app/keyboard.ts checks this first).
    expect(inlineRenameActive()).toBe(true);
    const input = rows()[0].querySelector('input') as HTMLInputElement;
    fireEvent.input(input, { target: { value: 'sharper wording' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    await flush();
    expect(UpdateIdea).toHaveBeenCalledWith('i1', 'sharper wording', '', '');
  });

  it('does not destroy the note when the edit is emptied', async () => {
    await openWith([idea()]);
    fireEvent.click(button(rows()[0], 'Edit'));
    await flush();
    const input = rows()[0].querySelector('input') as HTMLInputElement;
    fireEvent.input(input, { target: { value: '   ' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    await flush();
    expect(UpdateIdea).not.toHaveBeenCalled();
    expect(RemoveIdea).not.toHaveBeenCalled();
  });
});

// @vitest-environment jsdom
//
// Covers the ⌘I capture sheet (src/app/modals/quick-idea.ts, rendered by
// src/components/modals/QuickIdea.tsx). What matters here is that
// capture is cheap and never loses the session you were in:
//
//   • Enter files the note; ⇧Enter does not
//   • the filing session rides along as provenance, the project is the
//     one the sheet opened on
//   • Cancel and Escape send nothing
//   • either way focus goes back to the terminal
//   • an empty note is not filed
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import type { IdeaInfo } from '../../src/app/state.js';
import {
  openModal,
  resetStore,
  setActiveId,
  setProjects,
} from '../../src/store/store.js';

const AddIdea = vi.fn(
  (_s: string, _p: string, _k: string, _t: string): Promise<void> =>
    Promise.resolve(),
);

// Forwarded variadically so a mock that drops an argument the real
// binding gained still fails toHaveBeenCalledWith.
const UpdateIdea = vi.fn(
  (
    _id: string,
    _t: string,
    _s: string,
    _sess: string,
    _k: string,
    _p: string,
  ): Promise<void> => Promise.resolve(),
);

vi.mock('../../src/bridge.js', () => ({
  AddIdea: (...a: Parameters<typeof AddIdea>) => AddIdea(...a),
  UpdateIdea: (...a: Parameters<typeof UpdateIdea>) => UpdateIdea(...a),
  ListIdeas: vi.fn(() => Promise.resolve()),
  RemoveIdea: vi.fn(() => Promise.resolve()),
}));

vi.mock('../../src/app/dom.js', () => ({
  flashStatus: vi.fn(),
  setStatus: vi.fn(),
  reportFailure: () => () => {},
}));

const MARKUP = `
  <div id="app">
    <div id="quick-idea" class="hv-dialog hidden" role="dialog"
      aria-modal="true" aria-labelledby="quick-idea-title"></div>
  </div>`;

type QuickIdeaModule = typeof import('../../src/app/modals/quick-idea.js');
let openQuickIdea: QuickIdeaModule['openQuickIdea'];
let closeQuickIdea: QuickIdeaModule['closeQuickIdea'];
let initQuickIdea: QuickIdeaModule['initQuickIdea'];
let refocusActiveTerm: Mock<() => void>;
let setFocusedTile: Mock<(id: string | null) => void>;
let QuickIdea: typeof import('../../src/components/modals/QuickIdea.js')['QuickIdea'];

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ openQuickIdea, closeQuickIdea, initQuickIdea } = await import(
    '../../src/app/modals/quick-idea.js'
  ));
  ({ QuickIdea } = await import('../../src/components/modals/QuickIdea.js'));
  refocusActiveTerm = vi.fn();
  setFocusedTile = vi.fn();
  initQuickIdea({ setFocusedTile, refocusActiveTerm });
});

beforeEach(() => {
  AddIdea.mockReset();
  AddIdea.mockResolvedValue(undefined);
  UpdateIdea.mockReset();
  UpdateIdea.mockResolvedValue(undefined);
  refocusActiveTerm.mockReset();
  setFocusedTile.mockReset();
  resetStore();
  setProjects([
    { id: 'p1', name: 'hive' },
    { id: 'p2', name: 'other' },
  ]);
  render(<QuickIdea root={el('quick-idea')} />, {
    container: el('quick-idea'),
  });
});

function el<T extends HTMLElement = HTMLElement>(id: string): T {
  return document.getElementById(id) as T;
}
const textField = () => el<HTMLTextAreaElement>('quick-idea-text');
const flush = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });

async function openOn(projectId: string, activeSessionId?: string) {
  if (activeSessionId) setActiveId(activeSessionId);
  await act(async () => {
    openQuickIdea(projectId);
  });
}

// The inbox's Edit: the same sheet, opened on a record.
async function openEditing(over: Partial<IdeaInfo> = {}) {
  const idea: IdeaInfo = {
    id: 'i1',
    project_id: 'p1',
    kind: 'idea',
    text: 'the grid loses focus',
    status: 'open',
    created: '2026-09-05T10:00:00Z',
    updated: '2026-09-05T10:00:00Z',
    ...over,
  };
  // Opened on the idea's OWN project, the way editIdea does it.
  await act(async () => {
    openQuickIdea(idea.project_id, idea);
  });
}

describe('quick idea capture', () => {
  it('opens on the given project and takes focus', async () => {
    await openOn('p2');
    expect(el('quick-idea').classList.contains('hidden')).toBe(false);
    expect(document.activeElement).toBe(textField());
    // The dialog owns the keyboard, so the tile's visual focus goes.
    expect(setFocusedTile).toHaveBeenCalledWith(null);
  });

  it('files the note on Enter, with the session as provenance', async () => {
    await openOn('p1', 's7');
    fireEvent.change(textField(), { target: { value: '  the grid  ' } });
    fireEvent.keyDown(textField(), { key: 'Enter' });
    await flush();
    expect(AddIdea).toHaveBeenCalledWith('s7', 'p1', 'idea', 'the grid');
    // Filed and gone: the sheet closes and the terminal gets focus back.
    expect(el('quick-idea').classList.contains('hidden')).toBe(true);
    expect(refocusActiveTerm).toHaveBeenCalled();
  });

  it('sends the kind the user picked', async () => {
    await openOn('p1');
    fireEvent.change(textField(), { target: { value: 'it crashes' } });
    const bug =
      el('quick-idea-kind').querySelector<HTMLInputElement>(
        'input[value="bug"]',
      );
    fireEvent.click(bug as HTMLInputElement);
    fireEvent.click(el('quick-idea-save'));
    await flush();
    expect(AddIdea).toHaveBeenCalledWith('', 'p1', 'bug', 'it crashes');
  });

  it('files against the project the user chose, not the one it opened on', async () => {
    await openOn('p1');
    fireEvent.change(textField(), { target: { value: 'note' } });
    fireEvent.change(el('quick-idea-project'), { target: { value: 'p2' } });
    fireEvent.click(el('quick-idea-save'));
    await flush();
    expect(AddIdea).toHaveBeenCalledWith('', 'p2', 'idea', 'note');
  });

  it('leaves ⇧Enter to the field so a note can be more than one line', async () => {
    await openOn('p1');
    fireEvent.change(textField(), { target: { value: 'first line' } });
    fireEvent.keyDown(textField(), { key: 'Enter', shiftKey: true });
    await flush();
    expect(AddIdea).not.toHaveBeenCalled();
    expect(el('quick-idea').classList.contains('hidden')).toBe(false);
  });

  it('files nothing for an empty or blank note', async () => {
    await openOn('p1');
    fireEvent.change(textField(), { target: { value: '   ' } });
    fireEvent.keyDown(textField(), { key: 'Enter' });
    await flush();
    expect(AddIdea).not.toHaveBeenCalled();
  });

  it('sends nothing on cancel and hands focus back', async () => {
    await openOn('p1');
    fireEvent.change(textField(), { target: { value: 'dropped' } });
    fireEvent.click(el('quick-idea-cancel'));
    await flush();
    expect(AddIdea).not.toHaveBeenCalled();
    expect(el('quick-idea').classList.contains('hidden')).toBe(true);
    expect(refocusActiveTerm).toHaveBeenCalled();
  });

  it('closes on Escape without sending anything', async () => {
    await openOn('p1');
    fireEvent.change(textField(), { target: { value: 'dropped' } });
    // ModalShell's own root listener — the fallback for when focus is
    // already inside the sheet, which it is here.
    fireEvent.keyDown(el('quick-idea'), { key: 'Escape' });
    await flush();
    expect(AddIdea).not.toHaveBeenCalled();
    expect(el('quick-idea').classList.contains('hidden')).toBe(true);
    expect(refocusActiveTerm).toHaveBeenCalled();
  });

  it('does not hand focus to the terminal while another modal is still open', async () => {
    // ⌘I from the inbox closes the inbox first, so this is the
    // defensive case: a sheet opened over a modal that stays up must
    // not send the next keystrokes to the PTY behind it.
    openModal({ id: 'idea-inbox', projectId: 'p1', projectName: 'hive' });
    await openOn('p1');
    fireEvent.click(el('quick-idea-cancel'));
    await flush();
    expect(el('quick-idea').classList.contains('hidden')).toBe(true);
    expect(refocusActiveTerm).not.toHaveBeenCalled();
  });

  it('refuses a note past the daemon’s 4 KiB cap instead of losing it', async () => {
    const { MAX_IDEA_TEXT } = await import('../../src/lib/ideas.js');
    await openOn('p1');
    fireEvent.change(textField(), {
      target: { value: 'x'.repeat(MAX_IDEA_TEXT + 1) },
    });
    // Visible before Save, because the daemon rejects rather than
    // truncates and the sheet does not wait for the answer.
    expect(el('quick-idea-count').dataset.over).toBe('');
    expect(el<HTMLButtonElement>('quick-idea-save').disabled).toBe(true);
    fireEvent.keyDown(textField(), { key: 'Enter' });
    await flush();
    expect(AddIdea).not.toHaveBeenCalled();
    // Still up, with the text still in it — that is the whole point.
    expect(el('quick-idea').classList.contains('hidden')).toBe(false);
    expect(textField().value.length).toBe(MAX_IDEA_TEXT + 1);
  });

  it('files a note that is exactly at the cap', async () => {
    const { MAX_IDEA_TEXT } = await import('../../src/lib/ideas.js');
    await openOn('p1');
    const text = 'x'.repeat(MAX_IDEA_TEXT);
    fireEvent.change(textField(), { target: { value: text } });
    fireEvent.keyDown(textField(), { key: 'Enter' });
    await flush();
    expect(AddIdea).toHaveBeenCalledWith('', 'p1', 'idea', text);
  });

  it('starts clean on a re-open rather than showing the last draft', async () => {
    await openOn('p1');
    fireEvent.change(textField(), { target: { value: 'abandoned' } });
    await act(async () => {
      closeQuickIdea();
    });
    await openOn('p1');
    expect(textField().value).toBe('');
  });
});

// The inbox's Edit reuses this sheet, because the fields capture asked
// for are exactly the fields that can be wrong: the capture sheet
// pre-fills the project from whatever session was focused, so a
// mis-filed note is the default being wrong rather than user error.
describe('quick idea in edit mode', () => {
  it('pre-fills all three controls from the record', async () => {
    await openEditing({ kind: 'bug', project_id: 'p2' });
    expect(textField().value).toBe('the grid loses focus');
    expect(el<HTMLSelectElement>('quick-idea-project').value).toBe('p2');
    expect(
      el('quick-idea-kind').querySelector<HTMLInputElement>(
        'input[value="bug"]',
      )?.checked,
    ).toBe(true);
  });

  it('says it is editing rather than capturing', async () => {
    await openEditing();
    expect(el('quick-idea').textContent).toContain('Edit idea');
  });

  it('falls back to the default kind for an unrecognised one', async () => {
    // The daemon validates kind against a closed set, so a record
    // carrying something else must not leave the control with nothing
    // selected.
    await openEditing({ kind: 'epic' });
    expect(
      el('quick-idea-kind').querySelector<HTMLInputElement>(
        'input[value="idea"]',
      )?.checked,
    ).toBe(true);
  });

  it('saves the text, the kind and the project in one patch', async () => {
    await openEditing();
    fireEvent.change(textField(), { target: { value: '  sharper  ' } });
    const bug =
      el('quick-idea-kind').querySelector<HTMLInputElement>(
        'input[value="bug"]',
      );
    fireEvent.click(bug as HTMLInputElement);
    fireEvent.change(el('quick-idea-project'), { target: { value: 'p2' } });
    fireEvent.click(el('quick-idea-save'));
    await flush();
    expect(UpdateIdea).toHaveBeenCalledWith(
      'i1',
      'sharper',
      '',
      '',
      'bug',
      'p2',
    );
    // Never a second record: editing is not filing.
    expect(AddIdea).not.toHaveBeenCalled();
    expect(el('quick-idea').classList.contains('hidden')).toBe(true);
  });

  it('keeps what was typed when the daemon would refuse it', async () => {
    const { MAX_IDEA_TEXT } = await import('../../src/lib/ideas.js');
    await openEditing();
    const long = 'x'.repeat(MAX_IDEA_TEXT + 1);
    fireEvent.change(textField(), { target: { value: long } });
    fireEvent.keyDown(textField(), { key: 'Enter' });
    await flush();
    // The daemon rejects rather than truncates and nothing here awaits
    // the answer, so a sheet that closed would lose the text outright.
    expect(UpdateIdea).not.toHaveBeenCalled();
    expect(el('quick-idea').classList.contains('hidden')).toBe(false);
    expect(textField().value).toBe(long);
  });
});

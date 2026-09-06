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
import { resetStore, setProjects, setActiveId } from '../../src/store/store.js';

const AddIdea = vi.fn(
  (_s: string, _p: string, _k: string, _t: string): Promise<void> =>
    Promise.resolve(),
);

// Forwarded variadically so a mock that drops an argument the real
// binding gained still fails toHaveBeenCalledWith.
vi.mock('../../src/bridge.js', () => ({
  AddIdea: (...a: Parameters<typeof AddIdea>) => AddIdea(...a),
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

// @vitest-environment jsdom
//
// Covers the settings modal (src/components/modals/Settings.tsx, opened
// through the openSettings/closeSettings pair in
// src/app/modals/settings.ts): the
// add/edit/delete round-trip, the exact payload handed to
// SaveCustomAgents, and the two behaviors that carry real risk —
// existing ids must survive a rename (registry entries persist only
// the agent id, so a changed id breaks revive), and a save rejected by
// Go must surface its error instead of closing the modal.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import { resetStore } from '../../src/store/store.js';
// Type-only: erased, so the generated module is never resolved at runtime.
import type { main } from '../../wailsjs/go/models';

const listCustomAgents = vi.fn(
  (): Promise<main.CustomAgent[]> => Promise.resolve([]),
);
const saveCustomAgents = vi.fn(
  (_agents: main.CustomAgent[]): Promise<void> => Promise.resolve(),
);

// Forwarded variadically off Parameters<>, not at a fixed arity: a mock
// that drops an argument the real binding gained still satisfies
// toHaveBeenCalledWith, which is how UpdateSession/UpdateProject drifted
// twice before.
// The updates section shares the modal but not this file's subject.
// Stubbed to the quiet defaults so the agent assertions below stay
// about agents; test/dom/settings-updates.test.ts drives it for real.
const updateBridge = {
  GetUpdateSettings: vi.fn(() =>
    Promise.resolve({ channel: 'release', source_repo: '' }),
  ),
  SaveUpdateSettings: vi.fn(() => Promise.resolve()),
  SourceRepoStatusFor: vi.fn(() =>
    Promise.resolve({ path: '', detected: false, error: '' }),
  ),
  UpdateStatus: vi.fn(() => Promise.resolve(null)),
  StartUpdate: vi.fn(() => Promise.resolve()),
  ApplyUpdateAndRestart: vi.fn(() => Promise.resolve()),
  PickDirectory: vi.fn(() => Promise.resolve('')),
  EventsOn: vi.fn(),
  Confirm: vi.fn(() => Promise.resolve(true)),
  RestartDaemon: vi.fn(() => Promise.resolve()),
  CheckForUpdate: vi.fn(() => Promise.resolve(null)),
  OpenURL: vi.fn(() => Promise.resolve()),
};

vi.mock('../../src/bridge.js', () => ({
  ListCustomAgents: (...a: Parameters<typeof listCustomAgents>) =>
    listCustomAgents(...a),
  SaveCustomAgents: (...a: Parameters<typeof saveCustomAgents>) =>
    saveCustomAgents(...a),
  ...updateBridge,
}));

// settings.ts calls applyXtermTheme() when the theme changes; importing
// session-term.js for real would drag xterm and the whole view layer into
// a test about agents and updates.
vi.mock('../../src/app/session-term.js', () => ({
  applyXtermTheme: vi.fn(),
}));

// settings.ts routes Restart through banners.ts's shared confirm-and-apply
// wrapper, so this file now pulls banners.ts -> dom.ts in transitively.
// dom.ts resolves its singletons with mustEl at import time, so their
// markup has to exist even though nothing here exercises them.
// The dialog root is declared in index.html and the component renders
// into it, so the fixture carries the same element — nested under #app,
// because RTL's cleanup() removes a render() container whose parentNode
// IS document.body.
const MARKUP = `
  <div id="app"><div id="settings" class="hv-dialog hidden" role="dialog"
    aria-modal="true" aria-labelledby="settings-title"></div></div>
  <div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>`;

// Typed off the module itself rather than restated, so a changed export
// signature fails here instead of silently widening to any.
type SettingsModule = typeof import('../../src/app/modals/settings.js');
let openSettings: SettingsModule['openSettings'];
let closeSettings: SettingsModule['closeSettings'];
let initSettings: SettingsModule['initSettings'];
let splitCommand: SettingsModule['splitCommand'];
// Declared with their exact signatures, not ReturnType<typeof vi.fn>:
// vitest's Mock<T> is invariant enough that the bare form won't satisfy
// the SettingsDeps fields initSettings expects.
let refocusActiveTerm: Mock<() => void>;
let setFocusedTile: Mock<(id: string | null) => void>;
// Imported after the markup exists: the module resolves #settings with
// pageEl at load, and the component pulls app/dom.ts in for its own
// singletons.
let Settings: typeof import('../../src/components/modals/Settings.js')['Settings'];

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ openSettings, closeSettings, initSettings, splitCommand } = await import(
    '../../src/app/modals/settings.js'
  ));
  ({ Settings } = await import('../../src/components/modals/Settings.js'));
  refocusActiveTerm = vi.fn();
  setFocusedTile = vi.fn();
  initSettings({ setFocusedTile, refocusActiveTerm });
});

beforeEach(() => {
  listCustomAgents.mockReset().mockResolvedValue([]);
  saveCustomAgents.mockReset().mockResolvedValue(undefined);
  refocusActiveTerm.mockReset();
  setFocusedTile.mockReset();
  resetStore();
  render(<Settings root={el('settings')} />, { container: el('settings') });
});

// MARKUP above is this file's contract, so a missing id is a bug in the
// fixture, not a case to branch on — cast rather than null-check at 40
// call sites. The type parameter names the element kind the markup
// declares (a <button>, an <input>) so .disabled / .value resolve.
const el = <T extends HTMLElement = HTMLElement>(id: string): T =>
  document.getElementById(id) as T;
const rows = () => [
  ...document.querySelectorAll<HTMLElement>('.settings-agent-row'),
];
// Same contract, one level down: render() always builds all four cells.
const cell = <T extends HTMLElement = HTMLInputElement>(
  row: Element,
  sel: string,
): T => row.querySelector(sel) as T;
// Lets the load promises settle AND React re-render before the
// assertions: both the open and every bridge callback write state from
// outside a React event handler.
const flush = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });

// Drives an <input> the way a user does. fireEvent, not a hand-built
// event: these are controlled inputs, and React's value tracker swallows
// a change made by assigning .value directly.
function type(input: HTMLInputElement, value: string) {
  fireEvent.change(input, { target: { value } });
}

// The open/close pair writes the store from outside React.
const open = () => act(() => openSettings());
const close = () => act(() => closeSettings());
const click = (target: HTMLElement) => fireEvent.click(target);

describe('splitCommand', () => {
  it('splits a command line into argv on whitespace', () => {
    expect(splitCommand('claude --model haiku')).toEqual([
      'claude',
      '--model',
      'haiku',
    ]);
  });

  it('collapses extra whitespace and ignores padding', () => {
    expect(splitCommand('  mytool   --fast  ')).toEqual(['mytool', '--fast']);
  });

  it('returns an empty argv for blank input', () => {
    expect(splitCommand('   ')).toEqual([]);
    expect(splitCommand('')).toEqual([]);
    expect(splitCommand(null)).toEqual([]);
  });
});

describe('settings modal', () => {
  it('opens, loads existing agents, and closes', async () => {
    listCustomAgents.mockResolvedValue([
      {
        id: 'claude-lite',
        name: 'Claude Lite',
        cmd: ['claude', '--model', 'haiku'],
        color: '#8b5cf6',
      },
    ]);

    open();
    expect(el('settings').classList.contains('hidden')).toBe(false);
    expect(setFocusedTile).toHaveBeenCalledWith(null);
    await flush();

    expect(rows()).toHaveLength(1);
    expect(cell(rows()[0], '.settings-agent-name').value).toBe('Claude Lite');
    expect(cell(rows()[0], '.settings-agent-cmd').value).toBe(
      'claude --model haiku',
    );

    close();
    expect(el('settings').classList.contains('hidden')).toBe(true);
    expect(refocusActiveTerm).toHaveBeenCalled();
  });

  it('adds an agent and saves it with an empty id for Go to assign', async () => {
    open();
    await flush();

    click(el('settings-agent-add'));
    expect(rows()).toHaveLength(1);
    type(cell(rows()[0], '.settings-agent-name'), 'Claude Lite');
    type(cell(rows()[0], '.settings-agent-cmd'), 'claude --model haiku');

    click(el('settings-save'));
    await flush();

    expect(saveCustomAgents).toHaveBeenCalledWith([
      {
        id: '',
        name: 'Claude Lite',
        cmd: ['claude', '--model', 'haiku'],
        color: '#64748b',
      },
    ]);
    expect(el('settings').classList.contains('hidden')).toBe(true);
  });

  // The whole reason ids are assigned once in Go: registry entries
  // persist only the agent id, so recomputing it on rename would break
  // revive for every session already created with this agent.
  it('preserves an existing id across a rename', async () => {
    listCustomAgents.mockResolvedValue([
      {
        id: 'claude-lite',
        name: 'Claude Lite',
        cmd: ['claude'],
        color: '#8b5cf6',
      },
    ]);
    open();
    await flush();

    type(cell(rows()[0], '.settings-agent-name'), 'Claude Litest');
    click(el('settings-save'));
    await flush();

    const [payload] = saveCustomAgents.mock.calls[0];
    expect(payload[0].id).toBe('claude-lite');
    expect(payload[0].name).toBe('Claude Litest');
  });

  it('deletes the right row when several exist', async () => {
    listCustomAgents.mockResolvedValue([
      { id: 'one', name: 'One', cmd: ['one'], color: '#111111' },
      { id: 'two', name: 'Two', cmd: ['two'], color: '#222222' },
      { id: 'three', name: 'Three', cmd: ['three'], color: '#333333' },
    ]);
    open();
    await flush();
    expect(rows()).toHaveLength(3);

    click(cell(rows()[1], '.settings-agent-delete'));
    expect(rows()).toHaveLength(2);

    click(el('settings-save'));
    await flush();

    const [payload] = saveCustomAgents.mock.calls[0];
    expect(payload.map((a) => a.id)).toEqual(['one', 'three']);
  });

  it('drops fully-blank rows so a stray "+ Add agent" does not block the save', async () => {
    open();
    await flush();

    click(el('settings-agent-add'));
    type(cell(rows()[0], '.settings-agent-name'), 'Real');
    type(cell(rows()[0], '.settings-agent-cmd'), 'realtool');
    click(el('settings-agent-add')); // left entirely blank

    click(el('settings-save'));
    await flush();

    const [payload] = saveCustomAgents.mock.calls[0];
    expect(payload).toHaveLength(1);
    expect(payload[0].name).toBe('Real');
  });

  it('surfaces a rejected save and stays open', async () => {
    saveCustomAgents.mockRejectedValue(
      new Error('"claude" is a built-in agent and cannot be redefined'),
    );
    open();
    await flush();

    click(el('settings-agent-add'));
    type(cell(rows()[0], '.settings-agent-name'), 'Claude');
    type(cell(rows()[0], '.settings-agent-cmd'), 'claude');
    click(el('settings-save'));
    await flush();

    expect(el('settings').classList.contains('hidden')).toBe(false);
    expect(el('settings-error').classList.contains('hidden')).toBe(false);
    expect(el('settings-error').textContent).toContain('built-in agent');
  });

  it('reports a failed load without throwing', async () => {
    listCustomAgents.mockRejectedValue(new Error('boom'));
    open();
    await flush();

    expect(el('settings-error').classList.contains('hidden')).toBe(false);
    expect(el('settings-error').textContent).toContain('agents.json');
  });

  it('discards edits on cancel', async () => {
    listCustomAgents.mockResolvedValue([
      { id: 'keep', name: 'Keep', cmd: ['keep'], color: '#111111' },
    ]);
    open();
    await flush();
    type(cell(rows()[0], '.settings-agent-name'), 'Scribbled');

    click(el('settings-cancel'));
    expect(el('settings').classList.contains('hidden')).toBe(true);
    expect(saveCustomAgents).not.toHaveBeenCalled();

    // Reopening re-reads from disk rather than showing the discarded draft.
    open();
    await flush();
    expect(cell(rows()[0], '.settings-agent-name').value).toBe('Keep');
  });

  it('closes on Escape', async () => {
    open();
    await flush();
    act(() => {
      el('settings').dispatchEvent(
        new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
      );
    });
    expect(el('settings').classList.contains('hidden')).toBe(true);
  });
});

// A corrupt agents.json is the one case where an empty list is a lie.
// Go rejects the promise; the modal must show an error, keep Save
// disabled, and refuse to write — otherwise saving an empty draft
// destroys every definition the user opened Settings to repair.
describe('failed load', () => {
  it('shows an error and refuses to save over the broken file', async () => {
    listCustomAgents.mockRejectedValue(
      new Error('parse agents.json: invalid character'),
    );
    open();
    await flush();

    expect(el('settings-error').classList.contains('hidden')).toBe(false);
    expect(el('settings-error').textContent).toMatch(/agents\.json/);
    expect(el<HTMLButtonElement>('settings-save').disabled).toBe(true);
    expect(el<HTMLButtonElement>('settings-agent-add').disabled).toBe(true);

    click(el('settings-save'));
    await flush();
    expect(saveCustomAgents).not.toHaveBeenCalled();
    // The modal stays open so the error remains visible.
    expect(el('settings').classList.contains('hidden')).toBe(false);
    close();
  });

  it('re-enables editing on a later successful open', async () => {
    listCustomAgents.mockRejectedValue(new Error('boom'));
    open();
    await flush();
    close();

    listCustomAgents.mockResolvedValue([]);
    open();
    await flush();
    expect(el<HTMLButtonElement>('settings-save').disabled).toBe(false);
    expect(el<HTMLButtonElement>('settings-agent-add').disabled).toBe(false);
    close();
  });
});

describe('load race', () => {
  it('does not clobber a draft with a stale response from a previous open', async () => {
    let resolveFirst: (v: main.CustomAgent[]) => void = () => {};
    listCustomAgents.mockReturnValue(
      new Promise((r) => {
        resolveFirst = r;
      }),
    );
    open();

    close();
    listCustomAgents.mockResolvedValue([
      { id: 'kept', name: 'Kept', cmd: ['kept'], color: '#111111' },
    ]);
    open();
    await flush();

    resolveFirst([
      { id: 'stale', name: 'Stale', cmd: ['stale'], color: '#222222' },
    ]);
    await flush();

    expect(rows()).toHaveLength(1);
    expect(cell(rows()[0], '.settings-agent-name').value).toBe('Kept');
    close();
  });
});

describe('focus containment', () => {
  it('keeps focus inside the dialog after deleting a row', async () => {
    listCustomAgents.mockResolvedValue([
      { id: 'one', name: 'One', cmd: ['one'], color: '#111111' },
      { id: 'two', name: 'Two', cmd: ['two'], color: '#222222' },
    ]);
    open();
    await flush();

    const del = cell(rows()[0], '.settings-agent-delete');
    del.focus();
    del.click();

    // render() destroyed the focused button; focus must not fall to
    // <body>, or the Tab trap has no boundary and leaks behind the
    // backdrop.
    expect(document.activeElement).not.toBe(document.body);
    expect(el('settings').contains(document.activeElement)).toBe(true);

    // Deleting the last remaining row falls back to "+ Add agent".
    click(cell(rows()[0], '.settings-agent-delete'));
    expect(rows()).toHaveLength(0);
    expect(document.activeElement).toBe(el('settings-agent-add'));
    close();
  });
});

describe('escape', () => {
  it('consumes the event so it cannot reach the window handler', async () => {
    open();
    await flush();

    const seen = vi.fn();
    window.addEventListener('keydown', seen);
    act(() => {
      el('settings').dispatchEvent(
        new KeyboardEvent('keydown', {
          key: 'Escape',
          bubbles: true,
          cancelable: true,
        }),
      );
    });
    window.removeEventListener('keydown', seen);

    expect(el('settings').classList.contains('hidden')).toBe(true);
    expect(seen).not.toHaveBeenCalled();
  });
});

// The custom-token box debounces at 150ms. A timer that outlives the
// close fires with the text as it was, and on a reopen inside that
// window writes it back over what the box now shows — including
// writeOverrides('') when the field was cleared on the way out.
describe('appearance a11y', () => {
  it('links the custom-tokens box to the slot it reports into', async () => {
    // The controls exist only while the dialog is open now: React
    // renders the panel on open and unmounts it on close.
    open();
    await flush();
    expect(
      el<HTMLTextAreaElement>('settings-overrides').getAttribute(
        'aria-describedby',
      ),
    ).toBe('settings-overrides-error');
    expect(el('settings-overrides-error')).toBeTruthy();
  });
});

describe('appearance debounce', () => {
  it('does not write overrides after the dialog is closed', async () => {
    vi.useFakeTimers();
    try {
      open();
      const box = el<HTMLTextAreaElement>('settings-overrides');
      box.value = '--accent: red;';
      box.dispatchEvent(new window.Event('input', { bubbles: true }));
      close();
      const before = localStorage.getItem('hive.themeOverrides');
      vi.advanceTimersByTime(500);
      expect(localStorage.getItem('hive.themeOverrides')).toBe(before);
    } finally {
      vi.useRealTimers();
    }
  });
});

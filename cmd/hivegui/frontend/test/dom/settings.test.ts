// @vitest-environment jsdom
//
// Covers the settings modal (src/app/modals/settings.ts): the
// add/edit/delete round-trip, the exact payload handed to
// SaveCustomAgents, and the two behaviors that carry real risk —
// existing ids must survive a rename (registry entries persist only
// the agent id, so a changed id breaks revive), and a save rejected by
// Go must surface its error instead of closing the modal.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
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

// settings.ts routes Restart through banners.ts's shared confirm-and-apply
// wrapper, so this file now pulls banners.ts -> dom.ts in transitively.
// dom.ts resolves its singletons with mustEl at import time, so their
// markup has to exist even though nothing here exercises them.
const MARKUP = `
  <div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
  <div id="settings" class="hidden">
    <div id="settings-panel">
      <header><h3>Settings</h3><button id="settings-close">×</button></header>
      <section id="settings-agents">
        <div id="settings-agents-list"></div>
        <button id="settings-agent-add">+ Add agent</button>
        <p id="settings-error" class="settings-error hidden"></p>
      </section>
      <section id="settings-updates">
        <select id="settings-update-channel">
          <option value="release">Release</option>
          <option value="latest">Latest</option>
        </select>
        <div id="settings-source-repo-row">
          <input id="settings-source-repo" type="text"/>
          <button id="settings-source-repo-browse">Browse…</button>
        </div>
        <p id="settings-source-repo-hint"></p>
        <button id="settings-update-action">Update</button>
        <span id="settings-update-status"></span>
      </section>
      <div class="actions">
        <button id="settings-cancel">Cancel</button>
        <button id="settings-save" class="primary">Save</button>
      </div>
    </div>
  </div>`;

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

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ openSettings, closeSettings, initSettings, splitCommand } = await import(
    '../../src/app/modals/settings.js'
  ));
  refocusActiveTerm = vi.fn();
  setFocusedTile = vi.fn();
  initSettings({ setFocusedTile, refocusActiveTerm });
});

beforeEach(() => {
  listCustomAgents.mockReset().mockResolvedValue([]);
  saveCustomAgents.mockReset().mockResolvedValue(undefined);
  refocusActiveTerm.mockReset();
  setFocusedTile.mockReset();
  el('settings').classList.add('hidden');
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
const flush = () => new Promise((r) => setTimeout(r, 0));

// Drives an <input> the way a user does: set the value, fire 'input'.
function type(input: HTMLInputElement, value: string) {
  input.value = value;
  input.dispatchEvent(new window.Event('input', { bubbles: true }));
}

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

    openSettings();
    expect(el('settings').classList.contains('hidden')).toBe(false);
    expect(setFocusedTile).toHaveBeenCalledWith(null);
    await flush();

    expect(rows()).toHaveLength(1);
    expect(cell(rows()[0], '.settings-agent-name').value).toBe('Claude Lite');
    expect(cell(rows()[0], '.settings-agent-cmd').value).toBe(
      'claude --model haiku',
    );

    closeSettings();
    expect(el('settings').classList.contains('hidden')).toBe(true);
    expect(refocusActiveTerm).toHaveBeenCalled();
  });

  it('adds an agent and saves it with an empty id for Go to assign', async () => {
    openSettings();
    await flush();

    el('settings-agent-add').click();
    expect(rows()).toHaveLength(1);
    type(cell(rows()[0], '.settings-agent-name'), 'Claude Lite');
    type(cell(rows()[0], '.settings-agent-cmd'), 'claude --model haiku');

    el('settings-save').click();
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
    openSettings();
    await flush();

    type(cell(rows()[0], '.settings-agent-name'), 'Claude Litest');
    el('settings-save').click();
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
    openSettings();
    await flush();
    expect(rows()).toHaveLength(3);

    cell(rows()[1], '.settings-agent-delete').click();
    expect(rows()).toHaveLength(2);

    el('settings-save').click();
    await flush();

    const [payload] = saveCustomAgents.mock.calls[0];
    expect(payload.map((a) => a.id)).toEqual(['one', 'three']);
  });

  it('drops fully-blank rows so a stray "+ Add agent" does not block the save', async () => {
    openSettings();
    await flush();

    el('settings-agent-add').click();
    type(cell(rows()[0], '.settings-agent-name'), 'Real');
    type(cell(rows()[0], '.settings-agent-cmd'), 'realtool');
    el('settings-agent-add').click(); // left entirely blank

    el('settings-save').click();
    await flush();

    const [payload] = saveCustomAgents.mock.calls[0];
    expect(payload).toHaveLength(1);
    expect(payload[0].name).toBe('Real');
  });

  it('surfaces a rejected save and stays open', async () => {
    saveCustomAgents.mockRejectedValue(
      new Error('"claude" is a built-in agent and cannot be redefined'),
    );
    openSettings();
    await flush();

    el('settings-agent-add').click();
    type(cell(rows()[0], '.settings-agent-name'), 'Claude');
    type(cell(rows()[0], '.settings-agent-cmd'), 'claude');
    el('settings-save').click();
    await flush();

    expect(el('settings').classList.contains('hidden')).toBe(false);
    expect(el('settings-error').classList.contains('hidden')).toBe(false);
    expect(el('settings-error').textContent).toContain('built-in agent');
  });

  it('reports a failed load without throwing', async () => {
    listCustomAgents.mockRejectedValue(new Error('boom'));
    openSettings();
    await flush();

    expect(el('settings-error').classList.contains('hidden')).toBe(false);
    expect(el('settings-error').textContent).toContain('agents.json');
  });

  it('discards edits on cancel', async () => {
    listCustomAgents.mockResolvedValue([
      { id: 'keep', name: 'Keep', cmd: ['keep'], color: '#111111' },
    ]);
    openSettings();
    await flush();
    type(cell(rows()[0], '.settings-agent-name'), 'Scribbled');

    el('settings-cancel').click();
    expect(el('settings').classList.contains('hidden')).toBe(true);
    expect(saveCustomAgents).not.toHaveBeenCalled();

    // Reopening re-reads from disk rather than showing the discarded draft.
    openSettings();
    await flush();
    expect(cell(rows()[0], '.settings-agent-name').value).toBe('Keep');
  });

  it('closes on Escape', async () => {
    openSettings();
    await flush();
    el('settings').dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    );
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
    openSettings();
    await flush();

    expect(el('settings-error').classList.contains('hidden')).toBe(false);
    expect(el('settings-error').textContent).toMatch(/agents\.json/);
    expect(el<HTMLButtonElement>('settings-save').disabled).toBe(true);
    expect(el<HTMLButtonElement>('settings-agent-add').disabled).toBe(true);

    el('settings-save').click();
    await flush();
    expect(saveCustomAgents).not.toHaveBeenCalled();
    // The modal stays open so the error remains visible.
    expect(el('settings').classList.contains('hidden')).toBe(false);
    closeSettings();
  });

  it('re-enables editing on a later successful open', async () => {
    listCustomAgents.mockRejectedValue(new Error('boom'));
    openSettings();
    await flush();
    closeSettings();

    listCustomAgents.mockResolvedValue([]);
    openSettings();
    await flush();
    expect(el<HTMLButtonElement>('settings-save').disabled).toBe(false);
    expect(el<HTMLButtonElement>('settings-agent-add').disabled).toBe(false);
    closeSettings();
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
    openSettings();

    closeSettings();
    listCustomAgents.mockResolvedValue([
      { id: 'kept', name: 'Kept', cmd: ['kept'], color: '#111111' },
    ]);
    openSettings();
    await flush();

    resolveFirst([
      { id: 'stale', name: 'Stale', cmd: ['stale'], color: '#222222' },
    ]);
    await flush();

    expect(rows()).toHaveLength(1);
    expect(cell(rows()[0], '.settings-agent-name').value).toBe('Kept');
    closeSettings();
  });
});

describe('focus containment', () => {
  it('keeps focus inside the dialog after deleting a row', async () => {
    listCustomAgents.mockResolvedValue([
      { id: 'one', name: 'One', cmd: ['one'], color: '#111111' },
      { id: 'two', name: 'Two', cmd: ['two'], color: '#222222' },
    ]);
    openSettings();
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
    cell(rows()[0], '.settings-agent-delete').click();
    expect(rows()).toHaveLength(0);
    expect(document.activeElement).toBe(el('settings-agent-add'));
    closeSettings();
  });
});

describe('escape', () => {
  it('consumes the event so it cannot reach the window handler', async () => {
    openSettings();
    await flush();

    const seen = vi.fn();
    window.addEventListener('keydown', seen);
    el('settings').dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
        cancelable: true,
      }),
    );
    window.removeEventListener('keydown', seen);

    expect(el('settings').classList.contains('hidden')).toBe(true);
    expect(seen).not.toHaveBeenCalled();
  });
});

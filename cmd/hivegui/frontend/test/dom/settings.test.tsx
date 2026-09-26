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
const getAgentSettings = vi.fn(
  (): Promise<main.AgentSettings> =>
    Promise.resolve({
      claude_task_tools: true,
      pi_todo_tool: true,
    } as main.AgentSettings),
);
const saveAgentSettings = vi.fn(
  (_s: main.AgentSettings): Promise<void> => Promise.resolve(),
);
const testLayaConnection = vi.fn(
  (_url: string): Promise<string> => Promise.resolve(''),
);
const getExternalPlanReviewers = vi.fn(
  (): Promise<main.ExternalPlanReviewer[]> => Promise.resolve([]),
);
// The full agent catalog, which the launcher-visibility list is built
// from. Three agents, the same shape the Go side returns.
const CATALOG = [
  { id: 'shell', name: 'Shell', color: '#888888', available: true },
  { id: 'claude', name: 'Claude', color: '#d97757', available: true },
  { id: 'codex', name: 'Codex', color: '#10a37f', available: true },
] as main.AgentInfo[];
const listAgents = vi.fn(
  (): Promise<main.AgentInfo[]> => Promise.resolve(CATALOG),
);

// Forwarded variadically off Parameters<>, not at a fixed arity: a mock
// that drops an argument the real binding gained still satisfies
// toHaveBeenCalledWith, which is how UpdateSession/UpdateProject drifted
// twice before.
// The updates section shares the modal but not this file's subject.
// Stubbed to the quiet defaults so the agent assertions below stay
// about agents; test/dom/settings-updates.test.ts drives it for real.
// The Appearance tab's editor section shares the modal but not this
// file's subject; test/dom/settings-editor.test.tsx drives it.
const editorBridge = {
  GetEditorSettings: vi.fn(() =>
    Promise.resolve({ kind: '', command: '', app: '' }),
  ),
  SaveEditorSettings: vi.fn(() => Promise.resolve()),
};

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

// Both are read at render time, so a getter-backed `let` is enough to
// drive them per test — `isMac` is a module const, not a call.
let menuBarStatus = 'unsupported';
let onMac = false;
const setMenuBarLoginItem = vi.fn(() => Promise.resolve());

vi.mock('../../src/lib/platform.js', async (orig) => ({
  ...(await orig<typeof import('../../src/lib/platform.js')>()),
  get isMac() {
    return onMac;
  },
}));

vi.mock('../../src/bridge.js', () => ({
  ListCustomAgents: (...a: Parameters<typeof listCustomAgents>) =>
    listCustomAgents(...a),
  SaveCustomAgents: (...a: Parameters<typeof saveCustomAgents>) =>
    saveCustomAgents(...a),
  GetAgentSettings: (...a: Parameters<typeof getAgentSettings>) =>
    getAgentSettings(...a),
  SaveAgentSettings: (...a: Parameters<typeof saveAgentSettings>) =>
    saveAgentSettings(...a),
  GetExternalPlanReviewers: () => getExternalPlanReviewers(),
  TestLayaConnection: (...a: Parameters<typeof testLayaConnection>) =>
    testLayaConnection(...a),
  ListAgents: (...a: Parameters<typeof listAgents>) => listAgents(...a),
  MenuBarLoginItemStatus: () => Promise.resolve(menuBarStatus),
  SetMenuBarLoginItem: (...a: Parameters<typeof setMenuBarLoginItem>) =>
    setMenuBarLoginItem(...a),
  ...updateBridge,
  ...editorBridge,
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
  getAgentSettings.mockReset().mockResolvedValue({
    claude_task_tools: true,
    pi_todo_tool: true,
  } as main.AgentSettings);
  saveAgentSettings.mockReset().mockResolvedValue(undefined);
  getExternalPlanReviewers.mockReset().mockResolvedValue([]);
  listAgents.mockReset().mockResolvedValue(CATALOG);
  localStorage.removeItem('hive.agentPrefs');
  refocusActiveTerm.mockReset();
  setFocusedTile.mockReset();
  setMenuBarLoginItem.mockReset().mockResolvedValue(undefined);
  menuBarStatus = 'unsupported';
  onMac = false;
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
function type(input: HTMLInputElement | HTMLTextAreaElement, value: string) {
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

describe('settings: Claude plan progress toggle', () => {
  const box = () => el<HTMLInputElement>('settings-claude-task-tools');

  it('loads the saved value', async () => {
    getAgentSettings.mockResolvedValue({
      claude_task_tools: false,
    } as main.AgentSettings);
    open();
    await flush();
    expect(box().checked).toBe(false);
    expect(box().disabled).toBe(false);
  });

  it('defaults to on, matching the Go default', async () => {
    open();
    await flush();
    expect(box().checked).toBe(true);
  });

  it('saves the toggled value', async () => {
    open();
    await flush();
    fireEvent.click(box());
    expect(box().checked).toBe(false);

    click(el('settings-save'));
    await flush();
    expect(saveAgentSettings).toHaveBeenCalledWith({
      claude_task_tools: false,
      pi_todo_tool: true,
      plan_review: false,
      plan_reviewer: 'external',
      laya_enabled: false,
      laya_url: '',
      laya_model: '',
    });
    expect(el('settings').classList.contains('hidden')).toBe(true);
  });

  it('never saves over a file it could not read', async () => {
    // Saving the defaults would overwrite agent-settings.json — the very
    // file the user now has to fix.
    getAgentSettings.mockRejectedValue(new Error('parse agent-settings.json'));
    open();
    await flush();
    expect(box().disabled).toBe(true);
    expect(el('settings-error').textContent).toContain('agent-settings.json');

    click(el('settings-save'));
    await flush();
    expect(saveAgentSettings).not.toHaveBeenCalled();
  });

  it('never saves the display default before the real value loads', async () => {
    // Save clicked while GetAgentSettings is still in flight: the box shows
    // `true`, but that is a default, not the user's saved value — writing
    // it would overwrite a saved `false`.
    getAgentSettings.mockImplementation(() => new Promise(() => {}));
    open();
    await flush();
    expect(box().disabled).toBe(true);

    click(el('settings-save'));
    await flush();
    expect(saveAgentSettings).not.toHaveBeenCalled();
  });

  it('says what it costs and when it applies', async () => {
    open();
    await flush();
    const hint = el('settings-claude-task-tools-hint').textContent ?? '';
    expect(hint).toMatch(/context/);
    expect(hint).toMatch(/newly started sessions only/);
    expect(hint).toContain('CLAUDE_CODE_ENABLE_TODO_TOOLS');
    expect(box().getAttribute('aria-describedby')).toBe(
      'settings-claude-task-tools-hint',
    );
  });
});

describe('settings: Pi plan progress toggle', () => {
  const box = () => el<HTMLInputElement>('settings-pi-todo-tool');

  it('loads the saved value', async () => {
    getAgentSettings.mockResolvedValue({
      claude_task_tools: true,
      pi_todo_tool: false,
    } as main.AgentSettings);
    open();
    await flush();
    expect(box().checked).toBe(false);
    expect(box().disabled).toBe(false);
  });

  it('defaults to on when an older file has no pi_todo_tool', async () => {
    getAgentSettings.mockResolvedValue({
      claude_task_tools: false,
    } as main.AgentSettings);
    open();
    await flush();
    expect(box().checked).toBe(true);
  });

  it('saves both settings together', async () => {
    // SaveSettings writes the whole file, so dropping either field here
    // would reset it.
    getAgentSettings.mockResolvedValue({
      claude_task_tools: false,
      pi_todo_tool: true,
    } as main.AgentSettings);
    open();
    await flush();
    fireEvent.click(box());

    click(el('settings-save'));
    await flush();
    expect(saveAgentSettings).toHaveBeenCalledWith({
      claude_task_tools: false,
      pi_todo_tool: false,
      plan_review: false,
      plan_reviewer: 'external',
      laya_enabled: false,
      laya_url: '',
      laya_model: '',
    });
  });

  it('is disabled and never saved when the file could not be read', async () => {
    getAgentSettings.mockRejectedValue(new Error('parse agent-settings.json'));
    open();
    await flush();
    expect(box().disabled).toBe(true);
    click(el('settings-save'));
    await flush();
    expect(saveAgentSettings).not.toHaveBeenCalled();
  });

  it('says what it adds, what it costs and when it applies', async () => {
    open();
    await flush();
    const hint = el('settings-pi-todo-tool-hint').textContent ?? '';
    expect(hint).toContain('hive_todo');
    expect(hint).toMatch(/context/);
    expect(hint).toMatch(/newly started sessions only/);
    expect(box().getAttribute('aria-describedby')).toBe(
      'settings-pi-todo-tool-hint',
    );
  });
});

describe('settings: plan review (#457)', () => {
  const box = () => el<HTMLInputElement>('settings-plan-review');
  const reviewer = () => el<HTMLSelectElement>('settings-plan-reviewer');

  it('is off by default, and the reviewer choice waits for it', async () => {
    open();
    await flush();
    expect(box().checked).toBe(false);
    expect(reviewer().value).toBe('external');
    expect(reviewer().disabled).toBe(true);
  });

  it('saves both plan review settings with the rest of the file', async () => {
    open();
    await flush();
    fireEvent.click(box());
    fireEvent.change(reviewer(), { target: { value: 'hive' } });
    click(el('settings-save'));
    await flush();
    expect(saveAgentSettings).toHaveBeenCalledWith({
      claude_task_tools: true,
      pi_todo_tool: true,
      plan_review: true,
      plan_reviewer: 'hive',
      laya_enabled: false,
      laya_url: '',
      laya_model: '',
    });
  });

  it('loads saved values', async () => {
    getAgentSettings.mockResolvedValue({
      claude_task_tools: true,
      pi_todo_tool: true,
      plan_review: true,
      plan_reviewer: 'hive',
    } as main.AgentSettings);
    open();
    await flush();
    expect(box().checked).toBe(true);
    expect(reviewer().value).toBe('hive');
    expect(reviewer().disabled).toBe(false);
  });

  it('warns that a settings-file reviewer cannot be switched off, only when Hive is chosen', async () => {
    getExternalPlanReviewers.mockResolvedValue([
      { kind: 'settings', id: '/home/u/.claude/settings.json', active: true },
      { kind: 'plugin', id: 'plannotator@plannotator', active: true },
    ] as main.ExternalPlanReviewer[]);
    getAgentSettings.mockResolvedValue({
      claude_task_tools: true,
      pi_todo_tool: true,
      plan_review: true,
      plan_reviewer: 'external',
    } as main.AgentSettings);
    open();
    await flush();
    expect(
      document.getElementById('settings-plan-reviewer-warning'),
    ).toBeNull();
    fireEvent.change(reviewer(), { target: { value: 'hive' } });
    await flush();
    const warn = el('settings-plan-reviewer-warning').textContent ?? '';
    expect(warn).toContain('/home/u/.claude/settings.json');
    // A plugin CAN be switched off, so it is not warned about.
    expect(warn).not.toContain('plannotator');
  });
});

describe('settings: Laya state detection (spec 458)', () => {
  const box = () => el<HTMLInputElement>('settings-laya-enabled');
  const url = () => el<HTMLInputElement>('settings-laya-url');
  const model = () => el<HTMLInputElement>('settings-laya-model');

  it('is off by default, with the default URL as a placeholder', async () => {
    open();
    await flush();
    expect(box().checked).toBe(false);
    expect(url().value).toBe('');
    expect(url().placeholder).toBe('http://127.0.0.1:8000');
  });

  it('saves enabled, url and model, trimmed', async () => {
    open();
    await flush();
    fireEvent.click(box());
    fireEvent.change(url(), { target: { value: ' http://127.0.0.1:9000 ' } });
    fireEvent.change(model(), { target: { value: ' laya-terminal ' } });
    click(el('settings-save'));
    await flush();
    expect(saveAgentSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        laya_enabled: true,
        laya_url: 'http://127.0.0.1:9000',
        laya_model: 'laya-terminal',
      }),
    );
  });

  it('loads saved values', async () => {
    getAgentSettings.mockResolvedValue({
      claude_task_tools: true,
      pi_todo_tool: true,
      laya_enabled: true,
      laya_url: 'http://localhost:8000',
      laya_model: 'ft',
    } as main.AgentSettings);
    open();
    await flush();
    expect(box().checked).toBe(true);
    expect(url().value).toBe('http://localhost:8000');
    expect(model().value).toBe('ft');
  });

  it('warns only when the URL leaves this machine', async () => {
    open();
    await flush();
    const warning = () =>
      document.getElementById('settings-laya-remote-warning');
    for (const local of [
      'http://127.0.0.1:8000',
      'http://localhost:8000',
      'http://[::1]:8000',
      '',
    ]) {
      fireEvent.change(url(), { target: { value: local } });
      await flush();
      expect(warning(), local).toBeNull();
    }
    fireEvent.change(url(), { target: { value: 'http://10.0.0.5:8000' } });
    await flush();
    expect(warning()?.textContent ?? '').toMatch(/sent to that host/);
  });

  it('shows the connection test result inline', async () => {
    open();
    await flush();
    fireEvent.change(url(), { target: { value: 'http://127.0.0.1:9' } });
    testLayaConnection.mockResolvedValueOnce('connection refused');
    click(el('settings-laya-test'));
    await flush();
    expect(testLayaConnection).toHaveBeenCalledWith('http://127.0.0.1:9');
    expect(el('settings-laya-test-result').textContent).toBe(
      'Not reachable: connection refused',
    );
    click(el('settings-laya-test'));
    await flush();
    expect(el('settings-laya-test-result').textContent).toBe('Connected.');
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

  it('keeps the command as typed, trailing space included, and splits it on save', async () => {
    open();
    await flush();

    click(el('settings-agent-add'));
    const cmd = cell(rows()[0], '.settings-agent-cmd');
    type(cell(rows()[0], '.settings-agent-name'), 'Spaced');
    type(cmd, 'claude ');
    expect(cell(rows()[0], '.settings-agent-cmd').value).toBe('claude ');
    // No macOS text substitution (`--` into an em dash) in a command.
    expect(cmd.getAttribute('autocorrect')).toBe('off');
    expect(cmd.getAttribute('spellcheck')).toBe('false');

    type(cell(rows()[0], '.settings-agent-cmd'), 'claude  --model  haiku ');
    click(el('settings-save'));
    await flush();

    expect(saveCustomAgents).toHaveBeenCalledWith([
      {
        id: '',
        name: 'Spaced',
        cmd: ['claude', '--model', 'haiku'],
        color: '#64748b',
      },
    ]);
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

// The footer advertises [enter] save, so Enter has to actually confirm
// the dialog — AGENTS.md lists dialog confirm/cancel as a hard-coded
// binding. The two exclusions are the ones that would break something:
// a newline in the overrides box, and Cancel closing AND saving on one
// keystroke.
describe('enter confirms', () => {
  it('saves from a text field', async () => {
    open();
    await flush();
    click(el('settings-agent-add'));
    type(cell(rows()[0], '.settings-agent-name'), 'From Enter');
    type(cell(rows()[0], '.settings-agent-cmd'), 'entertool');
    fireEvent.keyDown(cell(rows()[0], '.settings-agent-name'), {
      key: 'Enter',
    });
    await flush();
    expect(saveCustomAgents).toHaveBeenCalledTimes(1);
    expect(saveCustomAgents.mock.calls[0][0][0].name).toBe('From Enter');
  });

  it('saves from a select, which the old text-input-only gate ignored', async () => {
    open();
    await flush();
    fireEvent.keyDown(el('settings-update-channel'), { key: 'Enter' });
    await flush();
    expect(saveCustomAgents).toHaveBeenCalledTimes(1);
  });

  it('leaves Enter alone inside the custom-tokens textarea', async () => {
    open();
    await flush();
    fireEvent.keyDown(el('settings-overrides'), { key: 'Enter' });
    await flush();
    expect(saveCustomAgents).not.toHaveBeenCalled();
  });

  // Enter on a focused button is that button's own activation. Without
  // the exclusion, Enter on Cancel would close the dialog and save the
  // draft it was meant to discard.
  it('leaves Enter alone on a button', async () => {
    open();
    await flush();
    fireEvent.keyDown(el('settings-cancel'), { key: 'Enter' });
    await flush();
    expect(saveCustomAgents).not.toHaveBeenCalled();
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
  // The positive control. Without it the case below passes just as well
  // when nothing is ever scheduled — which is what a raw `.value =`
  // assignment does to a controlled textarea, React's value tracker
  // swallowing the change and no debounce ever starting.
  it('writes overrides once the debounce elapses', async () => {
    vi.useFakeTimers();
    try {
      open();
      type(el<HTMLTextAreaElement>('settings-overrides'), '--accent: red;');
      act(() => {
        vi.advanceTimersByTime(500);
      });
      expect(localStorage.getItem('hive.themeOverrides')).toContain(
        '--accent: red',
      );
    } finally {
      vi.useRealTimers();
    }
  });

  it('does not write overrides after the dialog is closed', async () => {
    vi.useFakeTimers();
    try {
      open();
      type(el<HTMLTextAreaElement>('settings-overrides'), '--accent: red;');
      close();
      const before = localStorage.getItem('hive.themeOverrides');
      act(() => {
        vi.advanceTimersByTime(500);
      });
      expect(localStorage.getItem('hive.themeOverrides')).toBe(before);
    } finally {
      vi.useRealTimers();
    }
  });
});

// The preset picker's <optgroup> bucketing. Asserted here because
// groupPresets() is a per-RUN grouping, not a per-name one: it buckets
// only CONSECUTIVE presets sharing a group, so the rendered order is the
// PRESETS array order and a group that appears twice in the array yields
// two optgroups. Nothing else in the suite touches it, and the imperative
// field primitive whose test used to cover this shape was deleted with
// the React rewrite's last phase.
describe('theme preset picker grouping', () => {
  it('buckets consecutive presets sharing a group into one optgroup', () => {
    open();
    const select = el<HTMLSelectElement>('settings-theme');
    const groups = [...select.querySelectorAll('optgroup')];

    expect(groups.length).toBeGreaterThan(0);
    // Every option lives under an optgroup or directly under the select;
    // none is orphaned by a run boundary.
    const inGroups = groups.flatMap((g) => [...g.querySelectorAll('option')]);
    const direct = [...select.children].filter(
      (c): c is HTMLOptionElement => c.tagName === 'OPTION',
    );
    expect(inGroups.length + direct.length).toBe(
      select.querySelectorAll('option').length,
    );
    // Runs, not names: no two ADJACENT optgroups share a label.
    const labels = groups.map((g) => g.label);
    for (let i = 1; i < labels.length; i++) {
      expect(labels[i]).not.toBe(labels[i - 1]);
    }
  });

  it('renders options in PRESETS order across the groups', () => {
    open();
    const select = el<HTMLSelectElement>('settings-theme');
    const rendered = [...select.querySelectorAll('option')].map((o) => o.value);
    expect(rendered).toEqual([...new Set(rendered)]);
    expect(rendered.length).toBeGreaterThan(1);
  });
});

// ---------- tabs ----------
//
// Every panel stays mounted; only its visibility changes. jsdom applies
// no stylesheet, so `display: none` is not observable here — the
// assertions read the `hidden` class and the `hidden` attribute, which
// are what settings.css keys off. The e2e suite covers the rendered
// consequence (Tab never reaching a hidden panel's controls).
describe('settings tabs', () => {
  const panel = (id: string) => el(`settings-panel-${id}`);
  const tab = (id: string) => el<HTMLButtonElement>(`settings-tab-${id}`);

  it('opens on the Agents tab with the other panels hidden', () => {
    open();
    expect(tab('agents').getAttribute('aria-selected')).toBe('true');
    expect(panel('agents').classList.contains('hidden')).toBe(false);
    expect(panel('appearance').classList.contains('hidden')).toBe(true);
    expect(panel('updates').classList.contains('hidden')).toBe(true);
  });

  it('clicking a tab swaps which panel is visible', () => {
    open();
    click(tab('updates'));
    expect(panel('updates').classList.contains('hidden')).toBe(false);
    expect(panel('agents').classList.contains('hidden')).toBe(true);
    expect(tab('updates').getAttribute('aria-selected')).toBe('true');
    expect(tab('agents').getAttribute('aria-selected')).toBe('false');
  });

  it('arrow keys move selection and wrap around the strip', () => {
    open();
    tab('agents').focus();
    fireEvent.keyDown(tab('agents'), { key: 'ArrowLeft' });
    // Wrapped backwards past Agents to the last tab.
    expect(tab('updates').getAttribute('aria-selected')).toBe('true');
    fireEvent.keyDown(tab('updates'), { key: 'ArrowRight' });
    expect(tab('agents').getAttribute('aria-selected')).toBe('true');
    fireEvent.keyDown(tab('agents'), { key: 'End' });
    expect(tab('updates').getAttribute('aria-selected')).toBe('true');
    fireEvent.keyDown(tab('updates'), { key: 'Home' });
    expect(tab('agents').getAttribute('aria-selected')).toBe('true');
  });

  it('moves focus with the selection so the roving tabindex is followable', () => {
    open();
    tab('agents').focus();
    fireEvent.keyDown(tab('agents'), { key: 'ArrowRight' });
    expect(document.activeElement).toBe(tab('appearance'));
    expect(tab('appearance').tabIndex).toBe(0);
    expect(tab('agents').tabIndex).toBe(-1);
  });

  // The spec promises the whole edit survives a switch, not just the
  // list: Appearance holds a select and a debounced textarea, and both
  // live in a panel that is hidden rather than unmounted. Covering only
  // the agent draft would leave the two controls whose state is easiest
  // to lose untested.
  it('keeps the theme choice and token overrides across a tab round-trip', async () => {
    open();
    await flush();
    click(el('settings-tab-appearance'));

    const theme = el<HTMLSelectElement>('settings-theme');
    // Whatever the second preset is — the point is a value that is not
    // the mount default, not which preset it happens to be.
    const picked = [...theme.querySelectorAll('option')][1].value;
    fireEvent.change(theme, { target: { value: picked } });
    type(el<HTMLTextAreaElement>('settings-overrides'), '--accent: #123456;');

    click(el('settings-tab-agents'));
    click(el('settings-tab-updates'));
    click(el('settings-tab-appearance'));

    expect(el<HTMLSelectElement>('settings-theme').value).toBe(picked);
    expect(el<HTMLTextAreaElement>('settings-overrides').value).toBe(
      '--accent: #123456;',
    );
  });

  it('keeps an in-progress agent draft across a tab round-trip', async () => {
    open();
    await flush();
    click(el('settings-agent-add'));
    type(cell(rows()[0], '.settings-agent-name'), 'Roundtrip');
    type(cell(rows()[0], '.settings-agent-cmd'), 'roundtrip --x');

    click(tab('appearance'));
    click(tab('agents'));

    expect(cell(rows()[0], '.settings-agent-name').value).toBe('Roundtrip');
    expect(cell(rows()[0], '.settings-agent-cmd').value).toBe('roundtrip --x');
  });

  // The root Enter handler excludes buttons so Cancel cannot close AND
  // save on one keystroke; a tab button is the same case.
  it('does not save when Enter activates a tab', async () => {
    open();
    await flush();
    fireEvent.keyDown(tab('updates'), { key: 'Enter' });
    expect(saveCustomAgents).not.toHaveBeenCalled();
  });

  // The slot is the dialog's only error surface, and half its errors come
  // from the Updates section. Inside a panel it would render invisibly
  // whenever another tab is up.
  it('keeps the error slot outside every panel', () => {
    open();
    expect(el('settings-error').closest('.settings-panel')).toBeNull();
  });
});

// ---------- the menu-bar tab ----------
//
// The section this replaced was rendered behind `isMac && status !==
// 'unsupported'`; the tab carries the same guard, so on every other
// platform the strip is the three portable tabs and the panel does not
// exist at all — not a tab that opens onto an explanation of why it is
// empty.
describe('settings menu-bar tab', () => {
  const tabIds = () =>
    [...document.querySelectorAll('#settings-tabs [role="tab"]')].map(
      (t) => t.id,
    );

  it('is absent when the platform is not a Mac', async () => {
    menuBarStatus = 'not-registered';
    onMac = false;
    open();
    await flush();
    expect(tabIds()).toEqual([
      'settings-tab-agents',
      'settings-tab-appearance',
      'settings-tab-updates',
    ]);
    expect(document.getElementById('settings-panel-menubar')).toBeNull();
  });

  it('is absent on a Mac that cannot register a login item', async () => {
    menuBarStatus = 'unsupported';
    onMac = true;
    open();
    await flush();
    expect(tabIds()).not.toContain('settings-tab-menubar');
    expect(document.getElementById('settings-panel-menubar')).toBeNull();
  });

  it('appears between Appearance and Updates and owns the toggle', async () => {
    menuBarStatus = 'not-registered';
    onMac = true;
    open();
    await flush();
    expect(tabIds()).toEqual([
      'settings-tab-agents',
      'settings-tab-appearance',
      'settings-tab-menubar',
      'settings-tab-updates',
    ]);

    click(el('settings-tab-menubar'));
    expect(el('settings-panel-menubar').classList.contains('hidden')).toBe(
      false,
    );
    const toggle = el<HTMLButtonElement>('settings-menubar-login-item');
    expect(toggle.textContent).toContain('Start at login');
    expect(el('settings-panel-menubar').contains(toggle)).toBe(true);

    click(toggle);
    await flush();
    expect(setMenuBarLoginItem).toHaveBeenCalledWith(true);
  });

  // The toggle re-reads the status, and "unsupported" is Go's default:
  // branch — so an unexpected status code can take the tab away while it
  // is the selected one. Without the activeTab clamp the body renders a
  // strip with nothing selected, no visible panel, and dead arrow keys.
  it('falls back to Agents if the tab leaves the strip while selected', async () => {
    menuBarStatus = 'not-registered';
    onMac = true;
    open();
    await flush();
    click(el('settings-tab-menubar'));
    expect(el('settings-panel-menubar').classList.contains('hidden')).toBe(
      false,
    );

    // The next status read is the one the toggle makes.
    menuBarStatus = 'unsupported';
    click(el('settings-menubar-login-item'));
    await flush();

    expect(document.getElementById('settings-tab-menubar')).toBeNull();
    expect(document.getElementById('settings-panel-menubar')).toBeNull();
    expect(el('settings-tab-agents').getAttribute('aria-selected')).toBe(
      'true',
    );
    expect(el('settings-panel-agents').classList.contains('hidden')).toBe(
      false,
    );
  });
});

// The System preset resolves to a user-chosen PAIR — one preset for the
// OS's dark scheme, one for light. The two pickers exist only while
// System is selected: an explicit preset has nothing to pair, and a
// control that changes nothing is a lie.
describe('system theme pair pickers', () => {
  const theme = () => el<HTMLSelectElement>('settings-theme');
  const dark = () => el<HTMLSelectElement>('settings-theme-dark');
  const light = () => el<HTMLSelectElement>('settings-theme-light');
  const pick = (s: HTMLSelectElement, value: string) =>
    fireEvent.change(s, { target: { value } });

  beforeEach(() => {
    localStorage.clear();
    open();
    click(el('settings-tab-appearance'));
  });

  it('shows the pair only while the theme is System', () => {
    pick(theme(), 'classic');
    expect(dark()).toBeNull();
    expect(light()).toBeNull();
    pick(theme(), 'system');
    expect(dark()).not.toBeNull();
    expect(light()).not.toBeNull();
    pick(theme(), 'dracula');
    expect(dark()).toBeNull();
  });

  it('lists every preset except System in each half', () => {
    pick(theme(), 'system');
    const all = [...theme().querySelectorAll('option')].map((o) => o.value);
    const expected = all.filter((v) => v !== 'system');
    for (const s of [dark(), light()]) {
      expect([...s.querySelectorAll('option')].map((o) => o.value)).toEqual(
        expected,
      );
    }
  });

  it('starts on hive-dark / hive-light', () => {
    pick(theme(), 'system');
    expect(dark().value).toBe('hive-dark');
    expect(light().value).toBe('hive-light');
  });

  // jsdom has no matchMedia, and applyTheme treats that as dark — so the
  // dark half is the one that paints here.
  it('picking the dark half stores it and repaints the terminals', async () => {
    const { applyXtermTheme } = await import('../../src/app/session-term.js');
    vi.mocked(applyXtermTheme).mockClear();
    pick(theme(), 'system');
    pick(dark(), 'dracula');
    expect(localStorage.getItem('hive.theme.dark')).toBe('dracula');
    expect(document.documentElement.dataset.theme).toBe('dracula');
    expect(dark().value).toBe('dracula');
    expect(applyXtermTheme).toHaveBeenCalled();
    // The selection itself is still System, not the resolved preset.
    expect(localStorage.getItem('hive.theme')).toBe('system');
  });

  // Same contract as selectPreset: a store that refuses the write still
  // gets the repaint for this session. applyTheme must not have to read
  // the half back out of the store it just failed to write.
  it('applies a half for the session when storage refuses the write', () => {
    pick(theme(), 'system');
    const setItem = vi
      .spyOn(Storage.prototype, 'setItem')
      .mockImplementation(() => {
        throw new Error('denied');
      });
    try {
      pick(dark(), 'nord');
    } finally {
      setItem.mockRestore();
    }
    expect(document.documentElement.dataset.theme).toBe('nord');
    expect(dark().value).toBe('nord');
    expect(localStorage.getItem('hive.theme.dark')).toBeNull();
  });

  it('picking the light half stores it without repainting the dark scheme', () => {
    pick(theme(), 'system');
    pick(dark(), 'dracula');
    pick(light(), 'github-light');
    expect(localStorage.getItem('hive.theme.light')).toBe('github-light');
    expect(light().value).toBe('github-light');
    expect(document.documentElement.dataset.theme).toBe('dracula');
  });
});

// Settings → Agents → "In the new-session menu" (LauncherAgents.tsx): the
// hidden/pinned draft over the full agent catalog, written to
// hive.agentPrefs on Save only. What the launcher does with it is
// test/dom/launcher.test.tsx; the ordering rule is test/unit/agent-order.
describe('settings: launcher visibility', () => {
  const PREFS = 'hive.agentPrefs';
  const stored = () => JSON.parse(localStorage.getItem(PREFS) ?? 'null');
  const agentRow = (id: string) =>
    document.querySelector<HTMLElement>(
      `#settings-launcher-agents li[data-agent-id="${id}"]`,
    ) as HTMLElement;
  const showBox = (id: string) =>
    agentRow(id).querySelector<HTMLInputElement>(
      '.settings-check:not(.settings-launcher-pin) input',
    ) as HTMLInputElement;
  const pinBox = (id: string) =>
    agentRow(id).querySelector<HTMLInputElement>(
      '.settings-launcher-pin input',
    ) as HTMLInputElement;
  const listIds = (listId: string) =>
    [...document.querySelectorAll<HTMLElement>(`#${listId} > li`)].map(
      (li) => li.dataset.agentId,
    );
  const save = async () => {
    click(el('settings-save'));
    await flush();
  };
  // jsdom has no DataTransfer: a plain Event carrying just what
  // lib/drag-row.ts reads. clientY 0 on a zero rect reads as "below".
  function dropOn(target: HTMLElement, draggedId: string) {
    const ev = new Event('drop', { bubbles: true, cancelable: true });
    Object.defineProperty(ev, 'dataTransfer', {
      value: {
        types: ['text/x-hive-agent'],
        getData: (k: string) => (k === 'text/x-hive-agent' ? draggedId : ''),
      },
    });
    Object.defineProperty(ev, 'clientY', { value: 0 });
    act(() => {
      target.dispatchEvent(ev);
    });
  }

  it('lists every agent with a show and a pin checkbox', async () => {
    open();
    await flush();
    expect(listIds('settings-launcher-rest')).toEqual([
      'shell',
      'claude',
      'codex',
    ]);
    for (const id of ['shell', 'claude', 'codex']) {
      expect(showBox(id).checked).toBe(true);
      expect(pinBox(id).checked).toBe(false);
    }
  });

  it('writes an unticked agent to hidden on Save', async () => {
    open();
    await flush();
    click(showBox('codex'));
    // Draft only until Save.
    expect(localStorage.getItem(PREFS)).toBeNull();
    await save();
    expect(stored()).toEqual({ hidden: ['codex'], pinned: [] });
  });

  it('discards visibility edits on cancel', async () => {
    open();
    await flush();
    click(showBox('codex'));
    click(pinBox('claude'));
    click(el('settings-cancel'));
    expect(localStorage.getItem(PREFS)).toBeNull();
  });

  it('moves a pinned agent into the pinned list, in pin order', async () => {
    open();
    await flush();
    click(pinBox('codex'));
    click(pinBox('shell'));
    expect(listIds('settings-launcher-pinned')).toEqual(['codex', 'shell']);
    expect(listIds('settings-launcher-rest')).toEqual(['claude']);
    // Only pinned rows are drag rows — a drop can never resolve into the
    // unpinned list.
    expect(agentRow('codex').hasAttribute('data-drag-row')).toBe(true);
    expect(agentRow('claude').hasAttribute('data-drag-row')).toBe(false);
    await save();
    expect(stored()).toEqual({ hidden: [], pinned: ['codex', 'shell'] });
  });

  it('reorders pinned agents by drop, below the last one included', async () => {
    localStorage.setItem(
      PREFS,
      JSON.stringify({ hidden: [], pinned: ['shell', 'claude', 'codex'] }),
    );
    open();
    await flush();
    dropOn(agentRow('codex'), 'shell');
    expect(listIds('settings-launcher-pinned')).toEqual([
      'claude',
      'codex',
      'shell',
    ]);
    await save();
    expect(stored().pinned).toEqual(['claude', 'codex', 'shell']);
  });

  it('moves a pinned agent with Alt+ArrowUp and keeps focus on it', async () => {
    localStorage.setItem(
      PREFS,
      JSON.stringify({ hidden: [], pinned: ['shell', 'claude', 'codex'] }),
    );
    open();
    await flush();
    pinBox('codex').focus();
    fireEvent.keyDown(pinBox('codex'), { key: 'ArrowUp', altKey: true });
    await flush();
    expect(listIds('settings-launcher-pinned')).toEqual([
      'shell',
      'codex',
      'claude',
    ]);
    expect(document.activeElement).toBe(pinBox('codex'));
    // Without Alt the arrows are left alone.
    fireEvent.keyDown(pinBox('codex'), { key: 'ArrowUp' });
    expect(listIds('settings-launcher-pinned')).toEqual([
      'shell',
      'codex',
      'claude',
    ]);
  });

  it('drops ids of agents that no longer exist on the next Save', async () => {
    localStorage.setItem(
      PREFS,
      JSON.stringify({ hidden: ['gone'], pinned: ['gone', 'claude'] }),
    );
    open();
    await flush();
    await save();
    expect(stored()).toEqual({ hidden: [], pinned: ['claude'] });
  });

  it('warns when every agent is hidden', async () => {
    open();
    await flush();
    expect(el('settings-launcher-empty')).toBeNull();
    for (const id of ['shell', 'claude', 'codex']) click(showBox(id));
    expect(el('settings-launcher-empty')).not.toBeNull();
  });

  // Same hazard as agents.json: a draft built over a list that failed to
  // load must never be written over the user's stored choices.
  it('leaves stored prefs untouched when the agent list fails to load', async () => {
    // Spaced on purpose: a re-serialised write would compact it, so the
    // byte comparison below cannot pass by writing the same value back.
    const before = '{ "hidden": ["codex"], "pinned": ["claude"] }';
    localStorage.setItem(PREFS, before);
    listAgents.mockRejectedValue(new Error('daemon gone'));
    open();
    await flush();
    expect(el('settings-launcher-failed')).not.toBeNull();
    expect(document.querySelector('#settings-launcher-agents')).toBeNull();
    await save();
    // The rest of the dialog still saved and closed.
    expect(saveCustomAgents).toHaveBeenCalled();
    expect(el('settings').classList.contains('hidden')).toBe(true);
    expect(localStorage.getItem(PREFS)).toBe(before);
  });
});

// @vitest-environment jsdom
//
// Covers Settings › Appearance › Editor: which fields each preset
// shows, that the choice round-trips through editor.json, and — the
// one that matters — that a file which would not load is never
// overwritten by the next save.
import { act, fireEvent, render } from '@testing-library/react';
import type { Mock } from 'vitest';
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { resetStore } from '../../src/store/store.js';

const bridge = vi.hoisted(() => ({
  ListCustomAgents: vi.fn(() => Promise.resolve([])),
  SaveCustomAgents: vi.fn(() => Promise.resolve()),
  MenuBarLoginItemStatus: vi.fn(() => Promise.resolve('unsupported')),
  SetMenuBarLoginItem: vi.fn(() => Promise.resolve()),
  GetExternalPlanReviewers: vi.fn(() => Promise.resolve([])),
  GetAgentSettings: vi.fn(() =>
    Promise.resolve({ claude_task_tools: true, pi_todo_tool: true }),
  ),
  SaveAgentSettings: vi.fn(() => Promise.resolve()),
  ListAgents: vi.fn(() => Promise.resolve([])),
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
  GetEditorSettings: vi.fn(() =>
    Promise.resolve({ kind: '', command: '', app: '' }),
  ),
  SaveEditorSettings: vi.fn(() => Promise.resolve()),
}));

vi.mock('../../src/bridge.js', () => bridge);

// The "Custom application…" option is macOS-only (it shells out to
// `open -a`), so the platform has to look like a Mac for that
// assertion to mean anything.
vi.mock('../../src/lib/platform.js', () => ({
  isMac: true,
  detectMac: () => true,
  cmdOrCtrl: () => true,
}));

vi.mock('../../src/app/session-term.js', () => ({
  applyXtermTheme: vi.fn(),
}));

const MARKUP = `
  <div id="app"><div id="settings" class="hv-dialog hidden" role="dialog"
    aria-modal="true" aria-labelledby="settings-title"></div></div>
  <div id="terms"></div><ul id="projects"></ul><div id="status"><span id="status-text"></span><span id="status-hint"></span></div>`;

type SettingsModule = typeof import('../../src/app/modals/settings.js');
let openSettings: SettingsModule['openSettings'];
let closeSettings: SettingsModule['closeSettings'];
let initSettings: SettingsModule['initSettings'];
let refocusActiveTerm: Mock<() => void>;
let setFocusedTile: Mock<(id: string | null) => void>;
let Settings: typeof import('../../src/components/modals/Settings.js')['Settings'];

function el<T extends HTMLElement>(id: string): T {
  const found = document.getElementById(id);
  if (!found) throw new Error(`missing #${id} in test scaffold`);
  return found as T;
}
const maybe = (id: string) => document.getElementById(id);

const settle = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
const open = () => act(() => openSettings());

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  const mod = await import('../../src/app/modals/settings.js');
  ({ openSettings, closeSettings, initSettings } = mod);
  ({ Settings } = await import('../../src/components/modals/Settings.js'));
  refocusActiveTerm = vi.fn();
  setFocusedTile = vi.fn();
  initSettings({ refocusActiveTerm, setFocusedTile });
});

beforeEach(async () => {
  for (const fn of Object.values(bridge)) fn.mockClear();
  bridge.GetEditorSettings.mockResolvedValue({
    kind: '',
    command: '',
    app: '',
  });
  resetStore();
  render(<Settings root={el('settings')} />, { container: el('settings') });
});

describe('settings: editor', () => {
  it('defaults to none and shows no extra field', async () => {
    open();
    await settle();
    expect(el<HTMLSelectElement>('settings-editor-kind').value).toBe('');
    expect(maybe('settings-editor-command')).toBeNull();
    expect(maybe('settings-editor-app')).toBeNull();
  });

  it('loads the stored choice', async () => {
    bridge.GetEditorSettings.mockResolvedValue({
      kind: 'command',
      command: 'nvim-qt +{line} {file}',
      app: '',
    });
    open();
    await settle();
    expect(el<HTMLSelectElement>('settings-editor-kind').value).toBe('command');
    expect(el<HTMLInputElement>('settings-editor-command').value).toBe(
      'nvim-qt +{line} {file}',
    );
  });

  it('shows the command field only for a custom command', async () => {
    open();
    await settle();
    fireEvent.change(el('settings-editor-kind'), {
      target: { value: 'vscode' },
    });
    expect(maybe('settings-editor-command')).toBeNull();
    fireEvent.change(el('settings-editor-kind'), {
      target: { value: 'command' },
    });
    expect(maybe('settings-editor-command')).not.toBeNull();
  });

  it('shows the application field only for the app kind', async () => {
    open();
    await settle();
    fireEvent.change(el('settings-editor-kind'), { target: { value: 'app' } });
    expect(maybe('settings-editor-app')).not.toBeNull();
    expect(maybe('settings-editor-command')).toBeNull();
  });

  it('saves the chosen preset', async () => {
    open();
    await settle();
    fireEvent.change(el('settings-editor-kind'), { target: { value: 'zed' } });
    fireEvent.click(el('settings-save'));
    await settle();
    expect(bridge.SaveEditorSettings).toHaveBeenCalledWith({
      kind: 'zed',
      command: '',
      app: '',
    });
  });

  // The rule agents.json and update.json already follow: a file that
  // would not parse is a config the user hand-edited, and saving over
  // it loses their work.
  it('never saves over an editor.json that failed to load', async () => {
    bridge.GetEditorSettings.mockRejectedValue(new Error('parse editor.json'));
    open();
    await settle();
    fireEvent.click(el('settings-save'));
    await settle();
    expect(bridge.SaveEditorSettings).not.toHaveBeenCalled();
    // The rest of the modal still saves — one bad file does not block
    // the others.
    expect(bridge.SaveCustomAgents).toHaveBeenCalled();
  });

  // Go validates the template and can reject the save; the modal must
  // stay open on the error rather than closing as if it had saved.
  it('surfaces a rejected save and keeps the modal open', async () => {
    bridge.SaveEditorSettings.mockRejectedValueOnce(
      new Error('the command must contain {file}'),
    );
    open();
    await settle();
    fireEvent.change(el('settings-editor-kind'), {
      target: { value: 'command' },
    });
    fireEvent.change(el('settings-editor-command'), {
      target: { value: 'nvim-qt' },
    });
    fireEvent.click(el('settings-save'));
    await settle();
    expect(el('settings').classList.contains('hidden')).toBe(false);
    expect(document.body.textContent).toContain('must contain {file}');
  });

  it('disables the section until the file has loaded', async () => {
    open();
    // Deliberately no settle(): this is the window between opening the
    // modal and editor.json coming back, where an edit would be
    // silently reverted by the load.
    expect(el<HTMLSelectElement>('settings-editor-kind').disabled).toBe(true);
    await settle();
    expect(el<HTMLSelectElement>('settings-editor-kind').disabled).toBe(false);
  });

  it('disables the section when the file failed to load', async () => {
    bridge.GetEditorSettings.mockRejectedValue(new Error('parse editor.json'));
    open();
    await settle();
    expect(el<HTMLSelectElement>('settings-editor-kind').disabled).toBe(true);
  });
});

// @vitest-environment jsdom
//
// Settings › Shortcuts (spec 477, phase 2): capturing, refusing and
// warning, conflicts that block Save until reassigned or cancelled,
// plugin collisions, displaced defaults, and that a keymap.json which
// would not load is never written over.
import { act, fireEvent, render } from '@testing-library/react';
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { appStore, resetStore, setPluginUI } from '../../src/store/store.js';
import { registerCommandSource } from '../../src/app/command-registry.js';

const bridge = vi.hoisted(() => ({
  ListCustomAgents: vi.fn(() => Promise.resolve([])),
  SaveCustomAgents: vi.fn(() => Promise.resolve()),
  MenuBarLoginItemStatus: vi.fn(() => Promise.resolve('unsupported')),
  SetMenuBarLoginItem: vi.fn(() => Promise.resolve()),
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
  GetKeymap: vi.fn((): Promise<unknown> => Promise.resolve({})),
  SaveKeymap: vi.fn((_k: unknown) => Promise.resolve()),
  ExportKeymap: vi.fn((_k: unknown) => Promise.resolve(true)),
  PickKeymapFile: vi.fn(() => Promise.resolve('')),
  SetMenuAccelerators: vi.fn(() => Promise.resolve()),
  SuspendMenuAccelerators: vi.fn((_on: boolean) => Promise.resolve()),
}));
vi.mock('../../src/bridge.js', () => bridge);

vi.mock('../../src/lib/platform.js', () => ({
  isMac: true,
  detectMac: () => true,
  cmdOrCtrl: (e: KeyboardEvent) => e.metaKey,
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
let Settings: typeof import('../../src/components/modals/Settings.js')['Settings'];
let menuQueue: () => Promise<unknown>;
let resetKeymapSync: () => void;

function el<T extends HTMLElement>(id: string): T {
  const found = document.getElementById(id);
  if (!found) throw new Error(`missing #${id}`);
  return found as T;
}
const settle = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });

const row = (command: string) => {
  const r = document.querySelector<HTMLElement>(
    `.hv-shortcut-row[data-command="${command}"]`,
  );
  if (!r) throw new Error(`no row for ${command}`);
  return r;
};
const keysOf = (command: string) =>
  [...row(command).querySelectorAll('.hv-shortcut-key kbd')].map(
    (k) => k.textContent,
  );
const btn = (command: string, action: string) =>
  row(command).querySelector<HTMLButtonElement>(`[data-action="${action}"]`);
const saveBtn = () => el<HTMLButtonElement>('settings-save');

async function openTab() {
  act(() => openSettings());
  await settle();
  act(() => el('settings-tab-shortcuts').click());
}

/** Clicks + on a row and presses a key into its capture button. */
async function capture(
  command: string,
  key: string,
  code: string,
  mods: Partial<
    Record<'metaKey' | 'ctrlKey' | 'altKey' | 'shiftKey', boolean>
  > = {},
) {
  act(() => btn(command, 'add-shortcut')?.click());
  const field = row(command).querySelector<HTMLElement>('.hv-shortcut-capture');
  if (!field) throw new Error(`no capture field on ${command}`);
  expect(document.activeElement).toBe(field);
  act(() => {
    fireEvent.keyDown(field, { key, code, ...mods });
  });
}

async function save() {
  act(() => saveBtn().click());
  await settle();
}

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  const mod = await import('../../src/app/modals/settings.js');
  openSettings = mod.openSettings;
  closeSettings = mod.closeSettings;
  mod.initSettings({ refocusActiveTerm: vi.fn(), setFocusedTile: vi.fn() });
  ({ Settings } = await import('../../src/components/modals/Settings.js'));
  ({
    menuQueueSettledForTest: menuQueue,
    resetKeymapSyncForTest: resetKeymapSync,
  } = await import('../../src/app/keymap-sync.js'));
  // A palette command with no default key: it belongs in "Other commands".
  registerCommandSource(
    () => [{ id: 'restart-session', title: 'Restart session', run: () => {} }],
    'core',
  );
});

beforeEach(() => {
  for (const fn of Object.values(bridge)) fn.mockClear();
  // keymap-sync keeps module state (menu suspension, queue): one test's
  // capture must not leave the next one's menu calls off by one.
  resetKeymapSync();
  bridge.GetKeymap.mockImplementation(() => Promise.resolve({}));
  bridge.PickKeymapFile.mockImplementation(() => Promise.resolve(''));
  bridge.ExportKeymap.mockImplementation(() => Promise.resolve(true));
  resetStore();
  document.body.innerHTML = MARKUP;
  render(<Settings root={el('settings')} />, { container: el('settings') });
});

describe('settings: shortcuts tab', () => {
  it('lists the help groups, palette-only commands and the terminal keys', async () => {
    await openTab();
    const titles = [
      ...document.querySelectorAll('#settings-panel-shortcuts h4'),
    ].map((h) => h.textContent);
    expect(titles).toEqual([
      'Sessions',
      'Projects',
      'View',
      'Window',
      'Other commands',
      'Inside a terminal',
    ]);
    expect(keysOf('new-session')).toEqual(['⌘T']);
    expect(keysOf('restart-session')).toEqual([]);
    expect(row('restart-session').textContent).toContain('No shortcut');
    // Criterion 10: listed, with no control to change them.
    const fixed = document.querySelector(
      '[data-readonly="true"] .hv-shortcuts-list',
    );
    expect(fixed?.querySelectorAll('li').length).toBeGreaterThan(3);
    expect(fixed?.querySelectorAll('button').length).toBe(0);
  });

  it('captures a key, saves it, and the app uses it at once', async () => {
    await openTab();
    await capture('new-session', 'y', 'KeyY', { metaKey: true });
    expect(keysOf('new-session')).toEqual(['⌘T', '⌘Y']);
    // Nothing applies until Save.
    expect(appStore.getState().keymap).toEqual({});
    await save();
    const want = { mac: { 'new-session': ['Mod+T', 'Mod+Y'] } };
    expect(bridge.SaveKeymap).toHaveBeenCalledWith(want);
    expect(appStore.getState().keymap).toEqual(want);
  });

  it('suspends the native menu only while capturing', async () => {
    await openTab();
    act(() => btn('new-session', 'add-shortcut')?.click());
    await menuQueue();
    expect(bridge.SuspendMenuAccelerators.mock.calls).toEqual([[true]]);
    // A modifier on its own keeps listening.
    const field = row('new-session').querySelector<HTMLElement>(
      '.hv-shortcut-capture',
    );
    act(() => {
      fireEvent.keyDown(field as HTMLElement, {
        key: 'Meta',
        code: 'MetaLeft',
        metaKey: true,
      });
    });
    expect(
      row('new-session').querySelector('.hv-shortcut-capture'),
    ).not.toBeNull();
    // Escape cancels without binding anything.
    act(() => {
      fireEvent.keyDown(field as HTMLElement, {
        key: 'Escape',
        code: 'Escape',
      });
    });
    expect(row('new-session').querySelector('.hv-shortcut-capture')).toBeNull();
    expect(keysOf('new-session')).toEqual(['⌘T']);
    await menuQueue();
    expect(bridge.SuspendMenuAccelerators.mock.calls).toEqual([
      [true],
      [false],
    ]);
  });

  it('a taken key blocks Save until reassigned; the holder keeps its other keys', async () => {
    bridge.GetKeymap.mockImplementation(() =>
      Promise.resolve({ mac: { worktrees: ['Mod+E', 'Mod+Alt+W'] } }),
    );
    await openTab();
    await capture('new-session', 'e', 'KeyE', { metaKey: true });
    expect(row('new-session').dataset.conflict).toBe('source');
    expect(row('worktrees').dataset.conflict).toBe('holder');
    expect(row('new-session').textContent).toContain(
      '⌘E conflicts with Worktrees in the active project',
    );
    expect(row('worktrees').textContent).toContain(
      'conflicts with New session',
    );
    expect(saveBtn().disabled).toBe(true);
    // Enter cannot save around it either.
    act(() => {
      fireEvent.keyDown(el('settings-shortcuts-search'), { key: 'Enter' });
    });
    await settle();
    expect(bridge.SaveKeymap).not.toHaveBeenCalled();

    act(() => btn('new-session', 'reassign')?.click());
    expect(row('new-session').dataset.conflict).toBeUndefined();
    expect(keysOf('new-session')).toEqual(['⌘T', '⌘E']);
    expect(keysOf('worktrees')).toEqual(['⌥⌘W']);
    expect(saveBtn().disabled).toBe(false);
    await save();
    expect(bridge.SaveKeymap).toHaveBeenCalledWith({
      mac: { 'new-session': ['Mod+T', 'Mod+E'], worktrees: ['Mod+Alt+W'] },
    });
  });

  it('Cancel leaves both commands as they were', async () => {
    await openTab();
    await capture('new-session', 'e', 'KeyE', { metaKey: true });
    act(() => btn('new-session', 'cancel-reassign')?.click());
    expect(keysOf('new-session')).toEqual(['⌘T']);
    expect(keysOf('worktrees')).toEqual(['⌘E']);
    expect(saveBtn().disabled).toBe(false);
    await save();
    expect(bridge.SaveKeymap).not.toHaveBeenCalled(); // nothing changed
  });

  it('refuses an OS-reserved key with the reason, and warns on a terminal key', async () => {
    await openTab();
    await capture('new-session', 'q', 'KeyQ', { metaKey: true });
    expect(
      row('new-session').querySelector('[role="alert"]')?.textContent,
    ).toContain('reserved by macOS');
    expect(keysOf('new-session')).toEqual(['⌘T']);

    await capture('new-session', 'k', 'KeyK', { ctrlKey: true });
    expect(
      row('new-session').querySelector('[data-kind="warn"]')?.textContent,
    ).toContain('Programs in a session');
    expect(keysOf('new-session')).toEqual(['⌘T', '⌃K']);
  });

  it('removes a key, resets one command, and resets them all', async () => {
    await openTab();
    act(() => btn('new-session', 'remove-shortcut')?.click());
    expect(keysOf('new-session')).toEqual([]);
    expect(row('new-session').textContent).toContain('No shortcut');
    act(() => btn('new-session', 'reset-shortcut')?.click());
    expect(keysOf('new-session')).toEqual(['⌘T']);
    expect(btn('new-session', 'reset-shortcut')).toBeNull();

    await capture('settings', ';', 'Semicolon', { metaKey: true });
    act(() => btn('worktrees', 'remove-shortcut')?.click());
    act(() => el('settings-shortcuts-reset-all').click());
    expect(keysOf('settings')).toEqual(['⌘,']);
    expect(keysOf('worktrees')).toEqual(['⌘E']);
  });

  it('flags a command whose default an override now holds', async () => {
    // As after an upgrade that gave worktrees ⌘E while the user had it.
    bridge.GetKeymap.mockImplementation(() =>
      Promise.resolve({ mac: { 'quick-idea': ['Mod+E'] } }),
    );
    await openTab();
    expect(keysOf('worktrees')).toEqual([]);
    expect(
      row('worktrees').querySelector('[data-kind="displaced"]')?.textContent,
    ).toContain(
      'your shortcut for Capture an idea now uses its default key, ⌘E',
    );
    expect(saveBtn().disabled).toBe(false);
  });

  it('shows a plugin key core holds as a conflict, and can give it to the plugin', async () => {
    setPluginUI('pal', {
      status: 'active',
      contrib: {
        commands: [
          {
            id: 'open',
            title: 'Open pal',
            run: () => {},
            keys: { key: 'k', shift: true },
          },
        ],
      },
    });
    await openTab();
    const id = 'plugin:pal:open';
    expect(row(id).textContent).toContain('⇧⌘K conflicts with Command palette');
    expect(keysOf(id)).toEqual([]);
    expect(saveBtn().disabled).toBe(false); // criterion 4: shown, not blocking
    act(() => btn(id, 'give-to-plugin')?.click());
    expect(keysOf(id)).toEqual(['⇧⌘K']);
    expect(keysOf('command-palette')).toEqual([]);
    await save();
    expect(bridge.SaveKeymap).toHaveBeenCalledWith({
      mac: { 'command-palette': [], [id]: ['Mod+Shift+[KeyK]'] },
    });
  });

  it('never writes over a keymap.json that failed to load', async () => {
    bridge.GetKeymap.mockImplementation(() =>
      Promise.reject(new Error('bad json')),
    );
    await openTab();
    expect(btn('new-session', 'add-shortcut')?.disabled).toBe(true);
    expect(el('settings-error').textContent).toContain('keymap.json');
    await save();
    expect(bridge.SaveKeymap).not.toHaveBeenCalled();
  });

  it('lets Tab and Shift+Tab leave the capture field', async () => {
    await openTab();
    act(() => btn('new-session', 'add-shortcut')?.click());
    const field = row('new-session').querySelector<HTMLElement>(
      '.hv-shortcut-capture',
    ) as HTMLElement;
    for (const shiftKey of [false, true]) {
      let notCancelled = false;
      act(() => {
        notCancelled = fireEvent.keyDown(field, {
          key: 'Tab',
          code: 'Tab',
          shiftKey,
        });
      });
      // Not swallowed: the browser moves focus, and nothing is refused.
      expect(notCancelled).toBe(true);
      expect(row('new-session').querySelector('.hv-shortcut-note')).toBeNull();
    }
    expect(keysOf('new-session')).toEqual(['⌘T']);
  });

  it('a failed keymap save shows the error and keeps Settings open', async () => {
    bridge.SaveKeymap.mockImplementationOnce(() =>
      Promise.reject(new Error('disk full')),
    );
    await openTab();
    await capture('new-session', 'y', 'KeyY', { metaKey: true });
    await save();
    expect(bridge.SaveKeymap).toHaveBeenCalled();
    expect(el('settings-error').textContent).toContain('disk full');
    expect(el('settings').classList.contains('hidden')).toBe(false);
    expect(appStore.getState().keymap).toEqual({});
  });

  it('search filters rows by name or key', async () => {
    await openTab();
    act(() => {
      fireEvent.change(el('settings-shortcuts-search'), {
        target: { value: '⇧⌘K' },
      });
    });
    const shown = [
      ...document.querySelectorAll(
        '#settings-panel-shortcuts .hv-shortcut-row[data-command]',
      ),
    ].map((r) => (r as HTMLElement).dataset.command);
    expect(shown).toEqual(['command-palette']);
  });
});

// Review follow-up: every action that removes or disables the focused
// control hands focus to a stable place, so keyboard users never drop to
// <body> mid-dialog.
describe('settings: shortcuts tab focus', () => {
  const focused = () => document.activeElement as HTMLElement | null;
  const action = (e: HTMLElement | null) => e?.dataset.action;
  const rowOf = (e: HTMLElement | null) =>
    e?.closest<HTMLElement>('.hv-shortcut-row')?.dataset.command;

  it('returns to the row’s + after a capture ends, whatever ended it', async () => {
    await openTab();
    await capture('new-session', 'y', 'KeyY', { metaKey: true }); // bound
    expect([rowOf(focused()), action(focused())]).toEqual([
      'new-session',
      'add-shortcut',
    ]);
    await capture('new-session', 'q', 'KeyQ', { metaKey: true }); // refused
    expect([rowOf(focused()), action(focused())]).toEqual([
      'new-session',
      'add-shortcut',
    ]);
    await capture('new-session', 'Escape', 'Escape'); // cancelled
    expect([rowOf(focused()), action(focused())]).toEqual([
      'new-session',
      'add-shortcut',
    ]);
  });

  it('moves to Reassign while a conflict waits, and back after', async () => {
    await openTab();
    await capture('new-session', 'e', 'KeyE', { metaKey: true });
    expect([rowOf(focused()), action(focused())]).toEqual([
      'new-session',
      'reassign',
    ]);
    act(() => btn('new-session', 'cancel-reassign')?.click());
    expect([rowOf(focused()), action(focused())]).toEqual([
      'new-session',
      'add-shortcut',
    ]);
  });

  it('keeps focus on the row after remove and reset, and on search after Reset all', async () => {
    await openTab();
    act(() => btn('new-session', 'remove-shortcut')?.click());
    expect([rowOf(focused()), action(focused())]).toEqual([
      'new-session',
      'add-shortcut',
    ]);
    act(() => btn('new-session', 'reset-shortcut')?.click());
    expect([rowOf(focused()), action(focused())]).toEqual([
      'new-session',
      'add-shortcut',
    ]);
    act(() => btn('worktrees', 'remove-shortcut')?.click());
    act(() => el('settings-shortcuts-reset-all').click());
    expect(el<HTMLButtonElement>('settings-shortcuts-reset-all').disabled).toBe(
      true,
    );
    expect(focused()?.id).toBe('settings-shortcuts-search');
  });

  it('does not pull focus back when the capture loses it', async () => {
    await openTab();
    act(() => btn('new-session', 'add-shortcut')?.click());
    const search = el<HTMLInputElement>('settings-shortcuts-search');
    act(() => search.focus());
    expect(row('new-session').querySelector('.hv-shortcut-capture')).toBeNull();
    expect(focused()).toBe(search);
  });
});

describe('settings: closing mid-capture', () => {
  it('gives the native menu its shortcuts back', async () => {
    // Settings unmounts its body on close, with the capture button still
    // focused — no blur fires, so only the panel's cleanup restores it.
    await openTab();
    act(() => btn('new-session', 'add-shortcut')?.click());
    await menuQueue();
    expect(bridge.SuspendMenuAccelerators.mock.calls).toEqual([[true]]);
    act(() => closeSettings());
    await settle();
    await menuQueue();
    expect(bridge.SuspendMenuAccelerators.mock.calls).toEqual([
      [true],
      [false],
    ]);
  });
});

describe('settings: why Save is disabled', () => {
  it('says so in the footer, from any tab, and leads back to the conflict', async () => {
    await openTab();
    expect(document.getElementById('settings-save-blocked')).toBeNull();
    await capture('new-session', 'e', 'KeyE', { metaKey: true });
    const note = el('settings-save-blocked');
    expect(note.textContent).toContain('Resolve the shortcut conflict to save');
    expect(saveBtn().getAttribute('aria-describedby')).toBe(
      'settings-save-blocked',
    );
    // On the Shortcuts tab the conflict is in view: no "Show it".
    expect(document.getElementById('settings-show-conflict')).toBeNull();

    act(() => el('settings-tab-agents').click());
    expect(el('settings-save-blocked')).toBeTruthy();
    act(() => el('settings-show-conflict').click());
    expect(el('settings-panel-shortcuts').hidden).toBe(false);
    expect(document.activeElement?.getAttribute('data-action')).toBe(
      'reassign',
    );

    act(() => btn('new-session', 'cancel-reassign')?.click());
    expect(document.getElementById('settings-save-blocked')).toBeNull();
    expect(saveBtn().hasAttribute('aria-describedby')).toBe(false);
  });
});

// Spec 477 criterion 9 (phase 3): export the draft; import a file through
// a preview that changes nothing until Confirm.
describe('settings: shortcuts import and export', () => {
  const tool = (id: string) => el<HTMLButtonElement>(id);
  const preview = () =>
    document.querySelector<HTMLElement>('.hv-shortcuts-import');
  const importRow = (command: string, chord?: string) => {
    const r = [
      ...document.querySelectorAll<HTMLElement>('.hv-import-row'),
    ].find(
      (x) =>
        x.dataset.command === command &&
        (chord === undefined || x.dataset.chord === chord),
    );
    if (!r) throw new Error(`no import row for ${command} ${chord ?? ''}`);
    return r;
  };
  const action = (a: string) =>
    document.querySelector<HTMLButtonElement>(`[data-action="${a}"]`);
  const chooseMode = (mode: 'replace' | 'add') =>
    act(() => {
      fireEvent.click(
        document.querySelector(
          `input[name="shortcut-import-mode"][value="${mode}"]`,
        ) as HTMLElement,
      );
    });

  async function startImport(file: object | string) {
    bridge.PickKeymapFile.mockImplementation(() =>
      Promise.resolve(typeof file === 'string' ? file : JSON.stringify(file)),
    );
    act(() => tool('settings-shortcuts-import').click());
    await settle();
  }

  it('exports the draft, unsaved edits included', async () => {
    await openTab();
    await capture('new-session', 'y', 'KeyY', { metaKey: true });
    act(() => tool('settings-shortcuts-export').click());
    await settle();
    expect(bridge.ExportKeymap).toHaveBeenCalledWith({
      mac: { 'new-session': ['Mod+T', 'Mod+Y'] },
    });
    expect(bridge.SaveKeymap).not.toHaveBeenCalled();
    expect(el('settings-shortcuts-exported').textContent).toContain('exported');
    // An edit after the export: the file no longer matches the draft.
    await capture('new-session', 'u', 'KeyU', { metaKey: true });
    expect(document.getElementById('settings-shortcuts-exported')).toBeNull();
  });

  it('a cancelled export says nothing', async () => {
    bridge.ExportKeymap.mockImplementation(() => Promise.resolve(false));
    await openTab();
    act(() => tool('settings-shortcuts-export').click());
    await settle();
    expect(document.getElementById('settings-shortcuts-exported')).toBeNull();
  });

  // The buttons disable themselves while the file dialog is open, which
  // drops focus; without a way back, the next Esc closes Settings.
  it('focus returns to Export… / Import… after the dialog closes', async () => {
    await openTab();
    act(() => tool('settings-shortcuts-export').click());
    await settle();
    expect(document.activeElement?.id).toBe('settings-shortcuts-export');
    act(() => tool('settings-shortcuts-import').click()); // cancelled
    await settle();
    expect(document.activeElement?.id).toBe('settings-shortcuts-import');
    bridge.PickKeymapFile.mockImplementation(() =>
      Promise.reject(new Error('unreadable')),
    );
    act(() => (document.activeElement as HTMLElement).blur());
    act(() => tool('settings-shortcuts-import').click());
    await settle();
    expect(document.activeElement?.id).toBe('settings-shortcuts-import');
  });

  it('warns when the file has nothing for this platform', async () => {
    await openTab();
    await startImport({ other: { 'new-session': ['Ctrl+Y'] } });
    chooseMode('replace');
    expect(preview()?.textContent).toContain('no shortcuts for macOS');
    expect(preview()?.textContent).toContain('back to its default');
    expect(preview()?.textContent).toContain('[esc] cancel import');
  });

  it('a failed export shows the error and re-enables the buttons', async () => {
    bridge.ExportKeymap.mockImplementation(() =>
      Promise.reject(new Error('disk full')),
    );
    await openTab();
    act(() => tool('settings-shortcuts-export').click());
    await settle();
    expect(el('settings-error').textContent).toContain('disk full');
    expect(tool('settings-shortcuts-export').disabled).toBe(false);
    expect(tool('settings-shortcuts-import').disabled).toBe(false);
  });

  it('previews, settles a conflict, and applies only on Confirm', async () => {
    await openTab();
    await startImport({
      version: 1,
      mac: { 'new-session': ['Mod+N'], 'no-such-thing': ['Mod+U'] },
    });
    expect(preview()).toBeTruthy();
    // No mode yet: nothing to confirm, nothing listed.
    expect(action('confirm-import')?.disabled).toBe(true);
    expect(document.querySelectorAll('.hv-import-row')).toHaveLength(0);
    chooseMode('replace');
    expect(importRow('no-such-thing').dataset.state).toBe('skipped');
    expect(importRow('no-such-thing').textContent).toContain('Skipped');
    expect(importRow('new-session', 'Mod+N').dataset.state).toBe('conflict');
    expect(importRow('new-session', 'Mod+N').textContent).toContain(
      'Conflicts with',
    );
    expect(action('confirm-import')?.disabled).toBe(true);
    // Save waits, and says why.
    expect(saveBtn().disabled).toBe(true);
    expect(el('settings-save-blocked').textContent).toContain(
      'Finish the shortcut import',
    );
    act(() => action('reassign-import')?.click());
    expect(importRow('new-project').dataset.state).toBe('made-room');
    expect(action('confirm-import')?.disabled).toBe(false);
    // Nothing reached the draft yet.
    await save();
    expect(bridge.SaveKeymap).not.toHaveBeenCalled();
    act(() => action('confirm-import')?.click());
    expect(preview()).toBeNull();
    expect(keysOf('new-session')).toEqual(['⌘N']);
    expect(keysOf('new-project')).toEqual([]);
    await save();
    expect(bridge.SaveKeymap).toHaveBeenCalledWith({
      mac: { 'new-project': [], 'new-session': ['Mod+N'] },
    });
  });

  it('Skip drops the key and the command keeps its own', async () => {
    await openTab();
    await startImport({ mac: { 'new-session': ['Mod+N'] } });
    chooseMode('replace');
    act(() => action('skip-import')?.click());
    expect(importRow('new-session', 'Mod+N').dataset.state).toBe('skipped');
    act(() => action('confirm-import')?.click());
    expect(keysOf('new-session')).toEqual(['⌘T']);
    expect(keysOf('new-project')).toEqual(['⌘N']);
  });

  it('add keeps the draft’s other overrides; replace drops them', async () => {
    bridge.GetKeymap.mockImplementation(() =>
      Promise.resolve({ mac: { settings: ['Mod+Shift+S'] } }),
    );
    await openTab();
    await startImport({ mac: { 'new-session': ['Mod+Y'] } });
    chooseMode('add');
    act(() => action('confirm-import')?.click());
    await save();
    expect(bridge.SaveKeymap).toHaveBeenLastCalledWith({
      mac: { 'new-session': ['Mod+Y'], settings: ['Mod+Shift+S'] },
    });
  });

  it('replace makes the file this platform’s keymap', async () => {
    bridge.GetKeymap.mockImplementation(() =>
      Promise.resolve({
        mac: { settings: ['Mod+Shift+S'] },
        other: { worktrees: [] },
      }),
    );
    await openTab();
    await startImport({ mac: { 'new-session': ['Mod+Y'] } });
    chooseMode('replace');
    act(() => action('confirm-import')?.click());
    await save();
    expect(bridge.SaveKeymap).toHaveBeenLastCalledWith({
      mac: { 'new-session': ['Mod+Y'] },
      other: { worktrees: [] },
    });
  });

  it('Cancel and Escape change nothing; Escape leaves Settings open', async () => {
    await openTab();
    await startImport({ mac: { 'new-session': ['Mod+N'] } });
    chooseMode('replace');
    // Escape from inside the preview cancels the import, not Settings.
    act(() => {
      fireEvent.keyDown(action('reassign-import') as HTMLElement, {
        key: 'Escape',
        code: 'Escape',
      });
    });
    expect(preview()).toBeNull();
    expect(el('settings').classList.contains('hidden')).toBe(false);
    expect(document.activeElement?.id).toBe('settings-shortcuts-import');
    await startImport({ mac: { 'new-session': ['Mod+Y'] } });
    chooseMode('replace');
    act(() => action('cancel-import')?.click());
    expect(preview()).toBeNull();
    expect(keysOf('new-session')).toEqual(['⌘T']);
    await save();
    expect(bridge.SaveKeymap).not.toHaveBeenCalled();
  });

  it('a file that is not a keymap is refused in the error slot', async () => {
    await openTab();
    await startImport('{not json');
    expect(preview()).toBeNull();
    expect(el('settings-error').textContent).toContain('not JSON');
  });

  it('a file that cannot be read shows the error and re-enables the buttons', async () => {
    bridge.PickKeymapFile.mockImplementation(() =>
      Promise.reject(new Error('larger than 1 MB')),
    );
    await openTab();
    act(() => tool('settings-shortcuts-import').click());
    await settle();
    expect(preview()).toBeNull();
    expect(el('settings-error').textContent).toContain(
      'Could not read that file',
    );
    expect(el('settings-error').textContent).toContain('larger than 1 MB');
    expect(tool('settings-shortcuts-export').disabled).toBe(false);
    expect(tool('settings-shortcuts-import').disabled).toBe(false);
  });

  it('a cancelled pick does nothing', async () => {
    await openTab();
    await startImport('');
    expect(preview()).toBeNull();
    expect(el('settings-error').textContent).toBe('');
  });

  it('Import and Export wait while a conflict is pending or keymap.json failed', async () => {
    await openTab();
    await capture('new-session', 'e', 'KeyE', { metaKey: true });
    expect(tool('settings-shortcuts-import').disabled).toBe(true);
    expect(tool('settings-shortcuts-export').disabled).toBe(true);
    act(() => btn('new-session', 'cancel-reassign')?.click());
    expect(tool('settings-shortcuts-import').disabled).toBe(false);
  });

  it('says why the tab is off when keymap.json failed to load', async () => {
    bridge.GetKeymap.mockImplementation(() =>
      Promise.reject(new Error('bad json')),
    );
    await openTab();
    expect(el('settings-panel-shortcuts').textContent).toContain(
      'keymap.json could not be read',
    );
    expect(tool('settings-shortcuts-import').disabled).toBe(true);
    expect(tool('settings-shortcuts-export').disabled).toBe(true);
  });

  it('a null entry in keymap.json leaves the tab editable', async () => {
    bridge.GetKeymap.mockImplementation(() =>
      Promise.resolve({ mac: { 'new-session': ['Mod+Y'], worktrees: null } }),
    );
    await openTab();
    expect(btn('new-session', 'add-shortcut')?.disabled).toBe(false);
    expect(keysOf('new-session')).toEqual(['⌘Y']);
    expect(el('settings-error').textContent).toBe('');
  });

  it('the footer leads back to the import from another tab', async () => {
    await openTab();
    await startImport({ mac: { 'new-session': ['Mod+N'] } });
    chooseMode('replace');
    act(() => el('settings-tab-agents').click());
    expect(el('settings-show-conflict').textContent).toContain(
      'Finish the shortcut import',
    );
    act(() => el('settings-show-conflict').click());
    expect(document.activeElement?.getAttribute('data-action')).toBe(
      'reassign-import',
    );
  });
});

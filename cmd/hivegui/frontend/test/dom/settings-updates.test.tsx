// @vitest-environment jsdom
//
// Covers the Updates section of the settings modal: the channel choice
// gates the source-repo row, the channel joins the draft/save cycle,
// and the Update button does NOT — it starts real work in Go, so
// Cancel must not be able to lose a running download.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
import { act, fireEvent, render } from '@testing-library/react';
import { resetStore } from '../../src/store/store.js';
import type { UpdateInfoLike } from '../../src/lib/update-state.js';

const bridge = vi.hoisted(() => ({
  ListCustomAgents: vi.fn(() => Promise.resolve([])),
  SaveCustomAgents: vi.fn(() => Promise.resolve()),
  MenuBarLoginItemStatus: vi.fn(() => Promise.resolve('unsupported')),
  SetMenuBarLoginItem: vi.fn(() => Promise.resolve()),
  GetUpdateSettings: vi.fn(() =>
    Promise.resolve({ channel: 'release', source_repo: '' }),
  ),
  SaveUpdateSettings: vi.fn(() => Promise.resolve()),
  SourceRepoStatusFor: vi.fn(() =>
    Promise.resolve({ path: '/src/hive', detected: true, error: '' }),
  ),
  UpdateStatus: vi.fn(
    (): Promise<UpdateInfoLike | null> =>
      Promise.resolve({
        available: true,
        current: '2.4.0',
        latest: '2.5.0',
        stage: 'available',
        channel: 'release',
      }),
  ),
  StartUpdate: vi.fn(() => Promise.resolve()),
  ApplyUpdateAndRestart: vi.fn(() => Promise.resolve()),
  PickDirectory: vi.fn(() => Promise.resolve('/picked/hive')),
  EventsOn: vi.fn(),
  // settings.ts now routes Restart through banners.ts's shared
  // confirm-and-apply wrapper, so this file pulls banners.ts (and
  // dom.ts) in transitively — both need their bindings mocked and their
  // markup present.
  Confirm: vi.fn(() => Promise.resolve(true)),
  RestartDaemon: vi.fn(() => Promise.resolve()),
  CheckForUpdate: vi.fn(() => Promise.resolve(null)),
  OpenURL: vi.fn(() => Promise.resolve()),
}));

vi.mock('../../src/bridge.js', () => bridge);

// The button hides itself off macOS, so the platform has to look like a
// Mac for any of these assertions to mean anything.
vi.mock('../../src/lib/platform.js', () => ({
  isMac: true,
  detectMac: () => true,
  cmdOrCtrl: () => true,
}));

// settings.ts calls applyXtermTheme() when the theme changes; importing
// session-term.js for real would drag xterm and the whole view layer into
// a test about agents and updates.
vi.mock('../../src/app/session-term.js', () => ({
  applyXtermTheme: vi.fn(),
}));

// The dialog root is declared in index.html and the component renders
// into it, so the fixture carries the same element — nested under #app,
// because RTL's cleanup() removes a render() container whose parentNode
// IS document.body.
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

// Lets every pending bridge promise settle AND React re-render before
// asserting on the DOM: the bridge callbacks write state from outside a
// React event handler.
const settle = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 0));
  });
// The source-repo probe is debounced (it stats its way up a directory
// tree), so its assertions have to wait past that window rather than a
// microtask.
const settleProbe = () =>
  act(async () => {
    await new Promise((r) => setTimeout(r, 320));
  });

// The open/close pair writes the store from outside React.
const open = () => act(() => openSettings());
const close = () => act(() => closeSettings());

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
  bridge.GetUpdateSettings.mockResolvedValue({
    channel: 'release',
    source_repo: '',
  });
  resetStore();
  render(<Settings root={el('settings')} />, { container: el('settings') });
});

describe('settings: update channel', () => {
  it('hides the source-repo row on the release channel', async () => {
    open();
    await settle();
    expect(el<HTMLSelectElement>('settings-update-channel').value).toBe(
      'release',
    );
    expect(el('settings-source-repo-row').classList.contains('hidden')).toBe(
      true,
    );
  });

  it('reveals the source-repo row and its detected path on latest', async () => {
    open();
    await settle();
    const channel = el<HTMLSelectElement>('settings-update-channel');
    fireEvent.change(channel, { target: { value: 'latest' } });
    await settleProbe();

    expect(el('settings-source-repo-row').classList.contains('hidden')).toBe(
      false,
    );
    expect(el('settings-source-repo-hint').textContent).toContain('/src/hive');
  });

  it('saves the channel alongside the agents', async () => {
    open();
    await settle();
    const channel = el<HTMLSelectElement>('settings-update-channel');
    fireEvent.change(channel, { target: { value: 'latest' } });
    fireEvent.click(el('settings-save'));
    await settle();

    expect(bridge.SaveUpdateSettings).toHaveBeenCalledWith(
      expect.objectContaining({ channel: 'latest' }),
    );
    expect(bridge.SaveCustomAgents).toHaveBeenCalled();
    // Agents are written first, so a rejected channel leaves the agent
    // edits durable rather than stranding a saved channel behind a
    // Cancel that no longer discards it.
    expect(bridge.SaveCustomAgents.mock.invocationCallOrder[0]).toBeLessThan(
      bridge.SaveUpdateSettings.mock.invocationCallOrder[0],
    );
  });

  // A latest channel with no resolvable checkout is refused by Go. That
  // refusal must stop the whole save, not land after agents.json has
  // already been rewritten.
  it('keeps the modal open and shows why when the channel is rejected', async () => {
    bridge.SaveUpdateSettings.mockRejectedValueOnce(
      new Error('no hive checkout found'),
    );
    open();
    await settle();
    fireEvent.click(el('settings-save'));
    await settle();

    expect(el('settings-error').textContent).toContain('no hive checkout');
    expect(el('settings').classList.contains('hidden')).toBe(false);
  });

  // The probe stats its way up a directory tree; one per keystroke is
  // both wasteful and unordered, so a slow answer for "/Use" could
  // overwrite the fast one for "/Users/me/hive".
  it('debounces the source-repo probe across rapid typing', async () => {
    open();
    await settle();
    const channel = el<HTMLSelectElement>('settings-update-channel');
    fireEvent.change(channel, { target: { value: 'latest' } });
    await settleProbe();
    bridge.SourceRepoStatusFor.mockClear();

    const input = el<HTMLInputElement>('settings-source-repo');
    for (const v of ['/U', '/Us', '/User', '/Users/me/hive']) {
      fireEvent.change(input, { target: { value: v } });
    }
    await settleProbe();

    expect(bridge.SourceRepoStatusFor).toHaveBeenCalledTimes(1);
    expect(bridge.SourceRepoStatusFor).toHaveBeenCalledWith('/Users/me/hive');
  });

  it('fills the path from the directory picker', async () => {
    open();
    await settle();
    const channel = el<HTMLSelectElement>('settings-update-channel');
    fireEvent.change(channel, { target: { value: 'latest' } });
    fireEvent.click(el('settings-source-repo-browse'));
    await settleProbe();

    expect(el<HTMLInputElement>('settings-source-repo').value).toBe(
      '/picked/hive',
    );
  });
});

describe('settings: update button', () => {
  it('offers Update when one is available', async () => {
    open();
    await settle();
    const action = el<HTMLButtonElement>('settings-update-action');
    expect(action.textContent).toBe('Update');
    expect(action.dataset.action).toBe('start');
    expect(el('settings-update-status').textContent).toContain('2.5.0');
  });

  it('starts staging on click', async () => {
    open();
    await settle();
    fireEvent.click(el('settings-update-action'));
    await settle();
    expect(bridge.StartUpdate).toHaveBeenCalledTimes(1);
    expect(bridge.ApplyUpdateAndRestart).not.toHaveBeenCalled();
  });

  it('restarts once staging is ready, behind the same confirm the banner uses', async () => {
    bridge.UpdateStatus.mockResolvedValueOnce({
      available: true,
      stage: 'ready',
      latest: '2.5.0',
      message: 'Update ready',
      channel: 'release',
    });
    open();
    await settle();
    const action = el<HTMLButtonElement>('settings-update-action');
    expect(action.textContent).toBe('Restart');
    fireEvent.click(action);
    await settle();
    expect(bridge.Confirm).toHaveBeenCalledTimes(1);
    expect(bridge.ApplyUpdateAndRestart).toHaveBeenCalledTimes(1);
    expect(bridge.StartUpdate).not.toHaveBeenCalled();
  });

  // Applying from Settings is exactly as destructive as applying from
  // the banner, so declining must stop it here too.
  it('does not apply when the confirm is declined', async () => {
    bridge.Confirm.mockResolvedValueOnce(false);
    bridge.UpdateStatus.mockResolvedValueOnce({
      available: true,
      stage: 'ready',
      latest: '2.5.0',
      channel: 'release',
    });
    open();
    await settle();
    fireEvent.click(el('settings-update-action'));
    await settle();
    expect(bridge.ApplyUpdateAndRestart).not.toHaveBeenCalled();
  });

  // The click-time disable is a stopgap for the gap before the first
  // progress event, not a latch. Once staging is ready the button is the
  // only way to apply the update — leaving it greyed out stranded the
  // user with nothing to do but close and reopen Settings.
  it('re-enables the button when staging finishes after a click', async () => {
    open();
    await settle();
    const action = el<HTMLButtonElement>('settings-update-action');
    fireEvent.click(action);
    expect(action.disabled).toBe(true);

    // The same event the banner listens to, delivered through the
    // subscription EventsOn registered on open.
    const [, onProgress] = bridge.EventsOn.mock.calls.find(
      ([name]) => name === 'update:progress',
    ) as [string, (info: UpdateInfoLike) => void];
    act(() => {
      onProgress({
        available: true,
        stage: 'ready',
        latest: '2.5.0',
        message: 'Update ready',
        channel: 'release',
      });
    });

    expect(action.textContent).toBe('Restart');
    expect(action.disabled).toBe(false);
  });

  // Staging outlives the modal: closing and reopening must show the
  // work Go is still doing, not a reset button.
  it('re-reads staging state on reopen rather than tracking it locally', async () => {
    open();
    await settle();
    close();
    bridge.UpdateStatus.mockResolvedValueOnce({
      available: true,
      stage: 'staging',
      message: 'Downloading…',
      channel: 'release',
    });
    open();
    await settle();

    const action = el<HTMLButtonElement>('settings-update-action');
    expect(action.textContent).toBe('Updating…');
    expect(action.disabled).toBe(true);
    expect(el('settings-update-status').textContent).toBe('Downloading…');
  });
});

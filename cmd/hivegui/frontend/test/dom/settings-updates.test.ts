// @vitest-environment jsdom
//
// Covers the Updates section of the settings modal: the channel choice
// gates the source-repo row, the channel joins the draft/save cycle,
// and the Update button does NOT — it starts real work in Go, so
// Cancel must not be able to lose a running download.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
import type { UpdateInfoLike } from '../../src/lib/update-state.js';

const bridge = vi.hoisted(() => ({
  ListCustomAgents: vi.fn(() => Promise.resolve([])),
  SaveCustomAgents: vi.fn(() => Promise.resolve()),
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
}));

vi.mock('../../src/bridge.js', () => bridge);

// The button hides itself off macOS, so the platform has to look like a
// Mac for any of these assertions to mean anything.
vi.mock('../../src/lib/platform.js', () => ({
  isMac: true,
  detectMac: () => true,
  cmdOrCtrl: () => true,
}));

const MARKUP = `
  <div id="settings" class="hidden">
    <div id="settings-panel">
      <header><button id="settings-close">×</button></header>
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

type SettingsModule = typeof import('../../src/app/modals/settings.js');
let openSettings: SettingsModule['openSettings'];
let closeSettings: SettingsModule['closeSettings'];
let initSettings: SettingsModule['initSettings'];
let refocusActiveTerm: Mock<() => void>;
let setFocusedTile: Mock<(id: string | null) => void>;

function el<T extends HTMLElement>(id: string): T {
  const found = document.getElementById(id);
  if (!found) throw new Error(`missing #${id} in test scaffold`);
  return found as T;
}

// Lets every pending bridge promise settle before asserting on the DOM.
const settle = () => new Promise((r) => setTimeout(r, 0));

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  const mod = await import('../../src/app/modals/settings.js');
  ({ openSettings, closeSettings, initSettings } = mod);
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
  closeSettings();
});

describe('settings: update channel', () => {
  it('hides the source-repo row on the release channel', async () => {
    openSettings();
    await settle();
    expect(el<HTMLSelectElement>('settings-update-channel').value).toBe(
      'release',
    );
    expect(el('settings-source-repo-row').classList.contains('hidden')).toBe(
      true,
    );
  });

  it('reveals the source-repo row and its detected path on latest', async () => {
    openSettings();
    await settle();
    const channel = el<HTMLSelectElement>('settings-update-channel');
    channel.value = 'latest';
    channel.dispatchEvent(new Event('change'));
    await settle();

    expect(el('settings-source-repo-row').classList.contains('hidden')).toBe(
      false,
    );
    expect(el('settings-source-repo-hint').textContent).toContain('/src/hive');
  });

  it('saves the channel alongside the agents', async () => {
    openSettings();
    await settle();
    const channel = el<HTMLSelectElement>('settings-update-channel');
    channel.value = 'latest';
    channel.dispatchEvent(new Event('change'));
    el('settings-save').dispatchEvent(new MouseEvent('click'));
    await settle();

    expect(bridge.SaveUpdateSettings).toHaveBeenCalledWith(
      expect.objectContaining({ channel: 'latest' }),
    );
    expect(bridge.SaveCustomAgents).toHaveBeenCalled();
  });

  // A latest channel with no resolvable checkout is refused by Go. That
  // refusal must stop the whole save, not land after agents.json has
  // already been rewritten.
  it('does not write agents when the update settings are rejected', async () => {
    bridge.SaveUpdateSettings.mockRejectedValueOnce(
      new Error('no hive checkout found'),
    );
    openSettings();
    await settle();
    el('settings-save').dispatchEvent(new MouseEvent('click'));
    await settle();

    expect(bridge.SaveCustomAgents).not.toHaveBeenCalled();
    expect(el('settings-error').textContent).toContain('no hive checkout');
    expect(el('settings').classList.contains('hidden')).toBe(false);
  });

  it('fills the path from the directory picker', async () => {
    openSettings();
    await settle();
    const channel = el<HTMLSelectElement>('settings-update-channel');
    channel.value = 'latest';
    channel.dispatchEvent(new Event('change'));
    el('settings-source-repo-browse').dispatchEvent(new MouseEvent('click'));
    await settle();

    expect(el<HTMLInputElement>('settings-source-repo').value).toBe(
      '/picked/hive',
    );
  });
});

describe('settings: update button', () => {
  it('offers Update when one is available', async () => {
    openSettings();
    await settle();
    const action = el<HTMLButtonElement>('settings-update-action');
    expect(action.textContent).toBe('Update');
    expect(action.dataset.action).toBe('start');
    expect(el('settings-update-status').textContent).toContain('2.5.0');
  });

  it('starts staging on click', async () => {
    openSettings();
    await settle();
    el('settings-update-action').dispatchEvent(new MouseEvent('click'));
    await settle();
    expect(bridge.StartUpdate).toHaveBeenCalledTimes(1);
    expect(bridge.ApplyUpdateAndRestart).not.toHaveBeenCalled();
  });

  it('restarts once staging is ready', async () => {
    bridge.UpdateStatus.mockResolvedValueOnce({
      available: true,
      stage: 'ready',
      message: 'Update ready',
      channel: 'release',
    });
    openSettings();
    await settle();
    const action = el<HTMLButtonElement>('settings-update-action');
    expect(action.textContent).toBe('Restart');
    action.dispatchEvent(new MouseEvent('click'));
    await settle();
    expect(bridge.ApplyUpdateAndRestart).toHaveBeenCalledTimes(1);
    expect(bridge.StartUpdate).not.toHaveBeenCalled();
  });

  // Staging outlives the modal: closing and reopening must show the
  // work Go is still doing, not a reset button.
  it('re-reads staging state on reopen rather than tracking it locally', async () => {
    openSettings();
    await settle();
    closeSettings();
    bridge.UpdateStatus.mockResolvedValueOnce({
      available: true,
      stage: 'staging',
      message: 'Downloading…',
      channel: 'release',
    });
    openSettings();
    await settle();

    const action = el<HTMLButtonElement>('settings-update-action');
    expect(action.textContent).toBe('Updating…');
    expect(action.disabled).toBe(true);
    expect(el('settings-update-status').textContent).toBe('Downloading…');
  });
});

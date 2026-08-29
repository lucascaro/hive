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

const MARKUP = `
  <div id="terms"></div><ul id="projects"></ul><div id="status"></div>
  <div id="daemon-banner" class="hidden">
    <span id="daemon-banner-text"></span>
    <button id="daemon-banner-restart"></button>
    <button id="daemon-banner-dismiss"></button>
  </div>
  <div id="update-banner" class="hidden">
    <span id="update-banner-text"></span>
    <button id="update-banner-action"></button>
    <button id="update-banner-download"></button>
    <button id="update-banner-dismiss"></button>
  </div>
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
// The source-repo probe is debounced (it stats its way up a directory
// tree), so its assertions have to wait past that window rather than a
// microtask.
const settleProbe = () => new Promise((r) => setTimeout(r, 320));

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
    await settleProbe();

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
    openSettings();
    await settle();
    el('settings-save').dispatchEvent(new MouseEvent('click'));
    await settle();

    expect(el('settings-error').textContent).toContain('no hive checkout');
    expect(el('settings').classList.contains('hidden')).toBe(false);
  });

  // The probe stats its way up a directory tree; one per keystroke is
  // both wasteful and unordered, so a slow answer for "/Use" could
  // overwrite the fast one for "/Users/me/hive".
  it('debounces the source-repo probe across rapid typing', async () => {
    openSettings();
    await settle();
    const channel = el<HTMLSelectElement>('settings-update-channel');
    channel.value = 'latest';
    channel.dispatchEvent(new Event('change'));
    await settleProbe();
    bridge.SourceRepoStatusFor.mockClear();

    const input = el<HTMLInputElement>('settings-source-repo');
    for (const v of ['/U', '/Us', '/User', '/Users/me/hive']) {
      input.value = v;
      input.dispatchEvent(new Event('input'));
    }
    await settleProbe();

    expect(bridge.SourceRepoStatusFor).toHaveBeenCalledTimes(1);
    expect(bridge.SourceRepoStatusFor).toHaveBeenCalledWith('/Users/me/hive');
  });

  it('fills the path from the directory picker', async () => {
    openSettings();
    await settle();
    const channel = el<HTMLSelectElement>('settings-update-channel');
    channel.value = 'latest';
    channel.dispatchEvent(new Event('change'));
    el('settings-source-repo-browse').dispatchEvent(new MouseEvent('click'));
    await settleProbe();

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

  it('restarts once staging is ready, behind the same confirm the banner uses', async () => {
    bridge.UpdateStatus.mockResolvedValueOnce({
      available: true,
      stage: 'ready',
      latest: '2.5.0',
      message: 'Update ready',
      channel: 'release',
    });
    openSettings();
    await settle();
    const action = el<HTMLButtonElement>('settings-update-action');
    expect(action.textContent).toBe('Restart');
    action.dispatchEvent(new MouseEvent('click'));
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
    openSettings();
    await settle();
    el('settings-update-action').dispatchEvent(new MouseEvent('click'));
    await settle();
    expect(bridge.ApplyUpdateAndRestart).not.toHaveBeenCalled();
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

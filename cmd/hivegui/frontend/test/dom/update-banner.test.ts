// @vitest-environment jsdom
//
// The update banner's action button: it must drive the same Update →
// Updating… → Restart states the Settings modal does, and it must show
// staging progress and failures without being asked — someone who never
// opens Settings still needs to see that the update they started broke.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

const bridge = vi.hoisted(() => ({
  Confirm: vi.fn(() => Promise.resolve(true)),
  RestartDaemon: vi.fn(() => Promise.resolve()),
  CheckForUpdate: vi.fn(() => Promise.resolve(null)),
  StartUpdate: vi.fn(() => Promise.resolve()),
  ApplyUpdateAndRestart: vi.fn(() => Promise.resolve()),
  OpenURL: vi.fn(() => Promise.resolve()),
  EventsOn: vi.fn(),
}));
vi.mock('../../src/bridge.js', () => bridge);
vi.mock('../../src/lib/platform.js', () => ({
  isMac: true,
  detectMac: () => true,
  cmdOrCtrl: () => true,
}));

function el<T extends HTMLElement>(id: string): T {
  const found = document.getElementById(id);
  if (!found) throw new Error(`missing #${id} in test scaffold`);
  return found as T;
}

// The handlers registered via EventsOn, captured from the mock so the
// test can push events the way Go would.
function emit(event: string, payload: unknown) {
  for (const call of bridge.EventsOn.mock.calls) {
    if (call[0] === event) (call[1] as (p: unknown) => void)(payload);
  }
}

const settle = () => new Promise((r) => setTimeout(r, 0));

beforeAll(async () => {
  document.body.innerHTML = `
    <div id="terms"></div><ul id="projects"></ul>
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
    <div id="status"></div>`;
  const mod = await import('../../src/app/banners.js');
  mod.initBanners();
  await settle();
});

beforeEach(() => {
  bridge.StartUpdate.mockClear();
  bridge.ApplyUpdateAndRestart.mockClear();
  try {
    localStorage.removeItem('hive.updateDismissedFor');
  } catch {}
});

describe('update banner action button', () => {
  it('offers Update when a release is available, and starts staging on click', () => {
    emit('update:available', {
      available: true,
      current: '2.4.0',
      latest: '2.5.0',
      url: 'https://github.com/lucascaro/hive/releases/tag/v2.5.0',
      stage: 'available',
      channel: 'release',
    });
    const action = el<HTMLButtonElement>('update-banner-action');
    expect(action.style.display).not.toBe('none');
    expect(action.textContent).toBe('Update');

    action.dispatchEvent(new MouseEvent('click'));
    expect(bridge.StartUpdate).toHaveBeenCalledTimes(1);
  });

  it('shows staging progress without being asked', () => {
    emit('update:progress', {
      available: true,
      stage: 'staging',
      message: 'Downloading Hive-2.5.0-macos-universal.zip…',
    });
    expect(el('update-banner').classList.contains('hidden')).toBe(false);
    expect(el('update-banner-text').textContent).toContain('Downloading');
    expect(el<HTMLButtonElement>('update-banner-action').disabled).toBe(true);
  });

  it('turns into Restart and applies on click', () => {
    emit('update:progress', {
      available: true,
      stage: 'ready',
      message: 'Update ready — restart to apply',
    });
    const action = el<HTMLButtonElement>('update-banner-action');
    expect(action.textContent).toBe('Restart');
    expect(action.disabled).toBe(false);

    action.dispatchEvent(new MouseEvent('click'));
    expect(bridge.ApplyUpdateAndRestart).toHaveBeenCalledTimes(1);
    expect(bridge.StartUpdate).not.toHaveBeenCalled();
  });

  // A staging failure is the whole reason this banner is not
  // auto-hidden: the message is the only place the reason appears.
  it('surfaces a staging failure with a retry', () => {
    emit('update:progress', {
      available: true,
      stage: 'error',
      message: 'checksum mismatch for Hive-2.5.0-macos-universal.zip',
    });
    expect(el('update-banner').classList.contains('hidden')).toBe(false);
    expect(el('update-banner-text').textContent).toContain('checksum mismatch');
    expect(el<HTMLButtonElement>('update-banner-action').textContent).toBe(
      'Retry',
    );
  });
});

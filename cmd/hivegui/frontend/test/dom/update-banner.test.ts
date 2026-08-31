// @vitest-environment jsdom
//
// The update banner's action button: it must drive the same Update →
// Updating… → Restart states the Settings modal does, and it must show
// staging progress and failures without being asked — someone who never
// opens Settings still needs to see that the update they started broke.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

const bridge = vi.hoisted(() => ({
  // Typed params so mock.calls[0] destructures — the dialog's wording
  // is part of what this file asserts, not just that it was shown.
  Confirm: vi.fn((_title: string, _body: string) => Promise.resolve(true)),
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

// The banner builds its own markup now: text and actions are parts of
// the primitive, addressed by class / data-action-id rather than by id.
function part<T extends HTMLElement>(sel: string): T {
  const found = el('update-banner').querySelector<T>(sel);
  if (!found) throw new Error(`missing ${sel} in the update banner`);
  return found;
}
const actionBtn = () => part<HTMLButtonElement>('[data-action-id="action"]');
const bannerText = () => part<HTMLElement>('.hv-banner__text');

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
    <div id="app">
      <div id="terms"></div><ul id="projects"></ul>
      <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    </div>`;
  const mod = await import('../../src/app/banners.js');
  mod.initBanners();
  await settle();
});

beforeEach(() => {
  bridge.StartUpdate.mockClear();
  bridge.ApplyUpdateAndRestart.mockClear();
  bridge.Confirm.mockClear().mockResolvedValue(true);
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
    const action = actionBtn();
    expect(action.hidden).toBe(false);
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
    expect(el('update-banner').hidden).toBe(false);
    expect(bannerText().textContent).toContain('Downloading');
    expect(actionBtn().disabled).toBe(true);
  });

  it('turns into Restart and applies once confirmed', async () => {
    emit('update:progress', {
      available: true,
      stage: 'ready',
      latest: '2.5.0',
      message: 'Update ready — restart to apply',
    });
    const action = actionBtn();
    expect(action.textContent).toBe('Restart');
    expect(action.disabled).toBe(false);

    action.dispatchEvent(new MouseEvent('click'));
    await settle();
    expect(bridge.Confirm).toHaveBeenCalledTimes(1);
    // The dialog has to name what is about to happen, not just ask.
    const [title, body] = bridge.Confirm.mock.calls[0];
    expect(title).toContain('2.5.0');
    expect(body).toMatch(/terminate every running shell and agent/);
    expect(bridge.ApplyUpdateAndRestart).toHaveBeenCalledTimes(1);
    expect(bridge.StartUpdate).not.toHaveBeenCalled();
  });

  // The whole point of the overlay: declining must not restart. This
  // path terminates every running shell and agent, and the first cut of
  // the feature wired the button straight to the binding with no
  // confirmation at all.
  it('does not apply when the confirm is declined', async () => {
    bridge.Confirm.mockResolvedValueOnce(false);
    emit('update:progress', {
      available: true,
      stage: 'ready',
      latest: '2.5.0',
      message: 'Update ready',
    });
    actionBtn().dispatchEvent(new MouseEvent('click'));
    await settle();
    expect(bridge.Confirm).toHaveBeenCalledTimes(1);
    expect(bridge.ApplyUpdateAndRestart).not.toHaveBeenCalled();
  });

  // The confirm dialog is itself a window in which the other surface can
  // be clicked; the guard has to be claimed before the first await.
  it('ignores a second click while the confirm is open', async () => {
    let release: (v: boolean) => void = () => {};
    bridge.Confirm.mockReturnValueOnce(
      new Promise<boolean>((r) => {
        release = r;
      }),
    );
    emit('update:progress', {
      available: true,
      stage: 'ready',
      latest: '2.5.0',
    });
    const action = actionBtn();
    action.dispatchEvent(new MouseEvent('click'));
    action.dispatchEvent(new MouseEvent('click'));
    await settle();
    expect(bridge.Confirm).toHaveBeenCalledTimes(1);
    release(true);
    await settle();
    expect(bridge.ApplyUpdateAndRestart).toHaveBeenCalledTimes(1);
  });

  // A staging failure is the whole reason this banner is not
  // auto-hidden: the message is the only place the reason appears.
  it('surfaces a staging failure with a retry', () => {
    emit('update:progress', {
      available: true,
      stage: 'error',
      message: 'checksum mismatch for Hive-2.5.0-macos-universal.zip',
    });
    expect(el('update-banner').hidden).toBe(false);
    expect(bannerText().textContent).toContain('checksum mismatch');
    expect(actionBtn().textContent).toBe('Retry');
  });
});

// The dismissal paths were rewritten onto the banner primitive's handle
// in phase 4 and had no coverage: the update banner remembers a dismissed
// version in localStorage, and the daemon banner remembers the build it
// was dismissed for. Both are "don't nag me again about THIS one" rules
// that must not degrade into "never show me anything again".
describe('update banner dismissal', () => {
  const dismiss = () =>
    part<HTMLButtonElement>('.hv-banner__dismiss').dispatchEvent(
      new MouseEvent('click'),
    );

  it('remembers the dismissed version and stays down for it', () => {
    const available = {
      available: true,
      current: '2.4.0',
      latest: '2.5.0',
      url: 'https://github.com/lucascaro/hive/releases/tag/v2.5.0',
      stage: 'available',
      channel: 'release',
    };
    emit('update:available', available);
    expect(el('update-banner').hidden).toBe(false);

    dismiss();
    expect(el('update-banner').hidden).toBe(true);
    expect(localStorage.getItem('hive.updateDismissedFor')).toBe('2.5.0');

    // The 6h poll re-fires the same version: it must stay down.
    emit('update:available', available);
    expect(el('update-banner').hidden).toBe(true);

    // A newer release is a different fact and must surface.
    emit('update:available', { ...available, latest: '2.6.0' });
    expect(el('update-banner').hidden).toBe(false);
  });

  it('does not write a dismissal key for a transient banner', () => {
    // "Checking…" / "up to date" carry no version; dismissing one must
    // not poison the key for a real release.
    emit('update:progress', {
      available: true,
      stage: 'staging',
      message: 'Downloading…',
    });
    dismiss();
    expect(localStorage.getItem('hive.updateDismissedFor')).toBeNull();
  });
});

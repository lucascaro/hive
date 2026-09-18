// @vitest-environment jsdom
//
// The update banner's action button: it must drive the same Update →
// Updating… → Restart states the Settings modal does, and it must show
// staging progress and failures without being asked — someone who never
// opens Settings still needs to see that the update they started broke.
//
// Phase 2: the markup is now <Banners /> (components/Banners.tsx), and
// the store (store/store.ts) holds what's rendered. This mounts the real
// island and reads its DOM, same as the imperative version did.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { render, fireEvent, act } from '@testing-library/react';
import { appStore, resetStore } from '../../src/store/store.js';

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
  act(() => {
    for (const call of bridge.EventsOn.mock.calls) {
      if (call[0] === event) (call[1] as (p: unknown) => void)(payload);
    }
  });
}

const settle = () => new Promise((r) => setTimeout(r, 0));

let Banners: typeof import('../../src/components/Banners.js')['Banners'];
let banners: typeof import('../../src/app/banners.js');

// A check the user asked for — the ⤓ button, the menu item, the palette.
// Only this path raises the "available" banner; a background result
// (update:available, the boot poll) only sets the button's dot.
async function manualCheck(info: unknown) {
  bridge.CheckForUpdate.mockResolvedValueOnce(info as null);
  await act(() => banners.manualUpdateCheck());
}

const pending = () => appStore.getState().updatePending;

const AVAILABLE = {
  available: true,
  canApply: true,
  current: '2.4.0',
  latest: '2.5.0',
  url: 'https://github.com/lucascaro/hive/releases/tag/v2.5.0',
  stage: 'available',
  channel: 'release',
};

// The manual branch must show the version itself, not just leave the
// "Checking for updates…" banner up: the action button is filled in
// before any early return, so it alone cannot prove the banner showed.
function expectAvailableBanner(version: string) {
  expect(el('update-banner').hidden).toBe(false);
  const text = bannerText().textContent ?? '';
  expect(text).toContain(version);
  expect(text).not.toContain('Checking');
}

beforeAll(async () => {
  // dom.ts runs side effects on import (it decorates #terms), and
  // banners.ts pulls it in for flashStatus — so the scaffold needs
  // that element even though this file never touches it.
  document.body.innerHTML = `
    <div id="app">
      <div id="terms"></div><ul id="projects"></ul>
      <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    </div>`;
  // Dynamic, not static: a top-level import of Banners.tsx would pull in
  // app/banners.js (and its dom.js side effects) before the scaffold
  // above is in place.
  ({ Banners } = await import('../../src/components/Banners.js'));
  banners = await import('../../src/app/banners.js');
  banners.initBanners();
  await settle();
});

beforeEach(() => {
  bridge.StartUpdate.mockClear();
  bridge.ApplyUpdateAndRestart.mockClear();
  bridge.Confirm.mockClear().mockResolvedValue(true);
  resetStore();
  try {
    localStorage.removeItem('hive.updateDismissedFor');
  } catch {}
  // RTL's afterEach(cleanup) (setup-rtl.ts) unmounts the tree after every
  // test, so the island has to be remounted each time rather than once.
  render(<Banners />);
});

describe('update banner action button', () => {
  it('offers Update when a release is available, and starts staging on click', async () => {
    await manualCheck(AVAILABLE);
    expectAvailableBanner('2.5.0');
    const action = actionBtn();
    expect(action.hidden).toBe(false);
    expect(action.textContent).toBe('Update');

    fireEvent.click(action);
    expect(bridge.StartUpdate).toHaveBeenCalledTimes(1);
  });

  it('shows staging progress without being asked', () => {
    emit('update:progress', {
      available: true,
      canApply: true,
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
      canApply: true,
      stage: 'ready',
      latest: '2.5.0',
      message: 'Update ready — restart to apply',
    });
    const action = actionBtn();
    expect(action.textContent).toBe('Restart');
    expect(action.disabled).toBe(false);

    fireEvent.click(action);
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
      canApply: true,
      stage: 'ready',
      latest: '2.5.0',
      message: 'Update ready',
    });
    fireEvent.click(actionBtn());
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
      canApply: true,
      stage: 'ready',
      latest: '2.5.0',
    });
    const action = actionBtn();
    fireEvent.click(action);
    fireEvent.click(action);
    await settle();
    expect(bridge.Confirm).toHaveBeenCalledTimes(1);
    release(true);
    await settle();
    expect(bridge.ApplyUpdateAndRestart).toHaveBeenCalledTimes(1);
  });

  // info.url is empty either because Go rejected the release's html_url
  // for failing the prefix check, or because the channel has no release
  // at all. Only the first is fixed by opening the releases page.
  it('points at the releases page when a release URL was refused', async () => {
    await manualCheck({
      available: true,
      canApply: false,
      current: '2.4.0',
      latest: '2.5.0',
      url: '',
      stage: 'available',
      channel: 'release',
    });
    expectAvailableBanner('2.5.0');
    expect(bannerText().textContent).toContain('Open releases page manually.');
  });

  // The latest channel tracks a git checkout. There is no release
  // artifact to download, so sending the user to the releases page sent
  // them looking for something that does not exist.
  it('does not point at the releases page on the latest channel', async () => {
    await manualCheck({
      available: true,
      canApply: false,
      canApplyReason: 'D:\\src\\hive is not writable by Hive',
      current: 'eac84b9',
      latest: 'cf539dc',
      url: '',
      stage: 'available',
      channel: 'latest',
    });
    expectAvailableBanner('cf539dc');
    const text = bannerText().textContent ?? '';
    expect(text).not.toContain('Open releases page');
    expect(text).toContain('cf539dc');
    expect(text).toContain('is not writable');
  });

  // A staging failure is the whole reason this banner is not
  // auto-hidden: the message is the only place the reason appears.
  it('surfaces a staging failure with a retry', () => {
    emit('update:progress', {
      available: true,
      canApply: true,
      stage: 'error',
      message: 'checksum mismatch for Hive-2.5.0-macos-universal.zip',
    });
    expect(el('update-banner').hidden).toBe(false);
    expect(bannerText().textContent).toContain('checksum mismatch');
    expect(actionBtn().textContent).toBe('Retry');
  });
});

// A background check reports on the ⤓ button, not in a banner nobody
// asked for (#436). The dot stays until no update is pending — there is
// no "seen" state — so dismissing the banner must not clear it, and a
// later poll must not bring the banner back.
describe('update pip', () => {
  const dismiss = () =>
    fireEvent.click(part<HTMLButtonElement>('.hv-banner__dismiss'));

  it('a background update:available sets the pip and leaves the banner down', () => {
    emit('update:available', AVAILABLE);
    expect(el('update-banner').hidden).toBe(true);
    expect(pending()).toBe(true);
  });

  it('dismissing the banner keeps the pip, and a later poll does not reopen it', async () => {
    await manualCheck(AVAILABLE);
    expectAvailableBanner('2.5.0');
    expect(pending()).toBe(true);

    dismiss();
    expect(el('update-banner').hidden).toBe(true);

    // The 6h poll re-fires: the banner stays down, the dot stays up.
    emit('update:available', AVAILABLE);
    expect(el('update-banner').hidden).toBe(true);
    expect(pending()).toBe(true);
    // The per-version dismissal key is gone with the auto banner.
    expect(localStorage.getItem('hive.updateDismissedFor')).toBeNull();
  });

  it('a check reporting no update clears the pip', async () => {
    emit('update:available', AVAILABLE);
    expect(pending()).toBe(true);
    await manualCheck({ available: false, current: '2.5.0' });
    expect(pending()).toBe(false);
  });

  it('a background result with no update clears the pip', () => {
    emit('update:available', AVAILABLE);
    expect(pending()).toBe(true);
    // Exactly what SaveUpdateSettings emits when a settings change forgets
    // the last check (update_prefs.go: forgetUpdateState, then
    // setStage(StageIdle)) — every window's dot has to go with it.
    emit('update:progress', { available: false, stage: 'idle' });
    expect(pending()).toBe(false);
  });

  it('keeps the pip through staging', () => {
    emit('update:available', AVAILABLE);
    emit('update:progress', { ...AVAILABLE, stage: 'staging' });
    expect(pending()).toBe(true);
  });
});

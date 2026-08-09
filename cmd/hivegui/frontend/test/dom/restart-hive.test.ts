// @vitest-environment jsdom
//
// restartHive (src/app/banners.js) used to be an anonymous click
// handler on the daemon-stale banner's button — the only trigger in
// the whole app. With matching GUI/daemon builds the banner never
// renders, so there was no way to restart Hive at all. It is now an
// exported action the File menu and command palette call directly,
// and these tests pin the behaviour that made it safe to expose:
// confirm first, and surface a refusal instead of swallowing it.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

const bridge = vi.hoisted(() => ({
  Confirm: vi.fn(() => Promise.resolve(true)),
  RestartDaemon: vi.fn(() => Promise.resolve()),
  CheckForUpdate: vi.fn(() => Promise.resolve()),
  OpenURL: vi.fn(() => Promise.resolve()),
  EventsOn: vi.fn(),
}));

vi.mock('../../src/bridge.js', () => bridge);

let restartHive: typeof import('../../src/app/banners.js').restartHive;
let isDaemonRestarting: typeof import('../../src/app/banners.js').isDaemonRestarting;
let bannerEl: HTMLElement;
let bannerText: HTMLElement;

// Throwing lookup rather than `!`: biome's recommended preset bans
// non-null assertions, and a missing scaffold element should name
// itself instead of surfacing as a null-property TypeError three
// assertions later.
function mustEl(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (!el) throw new Error(`missing #${id} in test scaffold`);
  return el;
}

beforeAll(async () => {
  // dom.js runs side effects on import (it decorates #terms), and
  // banners.js pulls it in for flashStatus — so the scaffold needs
  // those elements even though this file never touches them.
  document.body.innerHTML = `
    <div id="terms"></div><ul id="projects"></ul>
    <div id="daemon-banner" class="hidden">
      <span id="daemon-banner-text"></span>
      <button id="daemon-banner-restart"></button>
      <button id="daemon-banner-dismiss"></button>
    </div>
    <div id="update-banner" class="hidden">
      <span id="update-banner-text"></span>
      <button id="update-banner-download"></button>
      <button id="update-banner-dismiss"></button>
    </div>
    <div id="status"></div>`;
  ({ restartHive, isDaemonRestarting } = await import(
    '../../src/app/banners.js'
  ));
  bannerEl = mustEl('daemon-banner');
  bannerText = mustEl('daemon-banner-text');
});

beforeEach(() => {
  bridge.Confirm.mockClear().mockResolvedValue(true);
  bridge.RestartDaemon.mockClear().mockResolvedValue(undefined);
});

describe('restartHive', () => {
  it('asks for confirmation before touching the daemon', async () => {
    bridge.Confirm.mockResolvedValue(false);
    await restartHive();
    expect(bridge.Confirm).toHaveBeenCalled();
    expect(bridge.RestartDaemon).not.toHaveBeenCalled();
  });

  it('calls RestartDaemon once confirmed', async () => {
    await restartHive();
    expect(bridge.RestartDaemon).toHaveBeenCalledTimes(1);
  });

  // The daemon now refuses to "restart" when it cannot actually
  // replace hived, rather than relaunching into the old one. That
  // rejection has to reach the user.
  it('surfaces a refusal in the banner', async () => {
    bridge.RestartDaemon.mockRejectedValue(new Error('hived still answering'));
    await restartHive();
    expect(bannerEl.classList.contains('hidden')).toBe(false);
    expect(bannerText.textContent).toMatch(
      /Restart failed.*hived still answering/,
    );
  });

  // events.js reads this to suppress the red "control disconnected"
  // status while a restart is deliberately tearing the conn down.
  it('clears the restarting flag when the call settles', async () => {
    await restartHive();
    expect(isDaemonRestarting()).toBe(false);
  });
});

// The banner button disabled itself, but the menu item and palette
// entry bypass that — and the daemon probe window is seconds long.
// Two invocations must not both reach RestartDaemon (which would
// spawn the replacement GUI twice).
describe('restartHive re-entrancy', () => {
  it('ignores a second invocation while one is in flight', async () => {
    let releaseConfirm: (() => void) | undefined;
    bridge.Confirm.mockImplementation(
      () =>
        new Promise((resolve) => {
          releaseConfirm = () => resolve(true);
        }),
    );

    const first = restartHive();
    const second = restartHive(); // must be a no-op, not a second run
    // Not `releaseConfirm?.()` — an unset releaser means Confirm was
    // never reached, which is the bug this test exists to catch.
    if (!releaseConfirm) throw new Error('Confirm was never called');
    releaseConfirm();
    await Promise.all([first, second]);

    expect(bridge.Confirm).toHaveBeenCalledTimes(1);
    expect(bridge.RestartDaemon).toHaveBeenCalledTimes(1);
  });

  it('releases the guard when the user cancels', async () => {
    bridge.Confirm.mockResolvedValue(false);
    await restartHive();
    expect(isDaemonRestarting()).toBe(false);

    // A cancel must not wedge the action permanently.
    bridge.Confirm.mockResolvedValue(true);
    await restartHive();
    expect(bridge.RestartDaemon).toHaveBeenCalledTimes(1);
  });
});

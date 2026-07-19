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

let restartHive, isDaemonRestarting, bannerEl, bannerText;

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
  ({ restartHive, isDaemonRestarting } = await import('../../src/app/banners.js'));
  bannerEl = document.getElementById('daemon-banner');
  bannerText = document.getElementById('daemon-banner-text');
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
    expect(bannerText.textContent).toMatch(/Restart failed.*hived still answering/);
  });

  // events.js reads this to suppress the red "control disconnected"
  // status while a restart is deliberately tearing the conn down.
  it('clears the restarting flag when the call settles', async () => {
    await restartHive();
    expect(isDaemonRestarting()).toBe(false);
  });
});

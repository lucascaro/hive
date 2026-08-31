// @vitest-environment jsdom
//
// restartHive (src/app/banners.ts) used to be an anonymous click
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
let initBanners: typeof import('../../src/app/banners.js').initBanners;
let bannerEl: HTMLElement;
let bannerText: HTMLElement;

// Throwing lookup rather than `!`: biome's recommended preset bans
// non-null assertions, and a missing scaffold element should name
// itself instead of surfacing as a null-property TypeError three
// assertions later.
// The handlers registered via EventsOn, replayed the way Go would.
function emit(event: string, payload: unknown) {
  for (const call of bridge.EventsOn.mock.calls) {
    if (call[0] === event) (call[1] as (p: unknown) => void)(payload);
  }
}

function mustEl(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (!el) throw new Error(`missing #${id} in test scaffold`);
  return el;
}

beforeAll(async () => {
  // dom.ts runs side effects on import (it decorates #terms), and
  // banners.ts pulls it in for flashStatus — so the scaffold needs
  // those elements even though this file never touches them.
  // The banners build their own markup now; the scaffold only has to
  // provide the #app mount point they prepend into.
  document.body.innerHTML = `
    <div id="app">
      <div id="terms"></div><ul id="projects"></ul>
      <div id="status"><span id="status-text"></span><span id="status-hint"></span></div>
    </div>`;
  ({ restartHive, isDaemonRestarting, initBanners } = await import(
    '../../src/app/banners.js'
  ));
  initBanners();
  bannerEl = mustEl('daemon-banner');
  bannerText = bannerEl.querySelector('.hv-banner__text') as HTMLElement;
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
    expect(bannerEl.hidden).toBe(false);
    expect(bannerText.textContent).toMatch(
      /Restart failed.*hived still answering/,
    );
  });

  // events.ts reads this to suppress the red "control disconnected"
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

// The daemon banner's dismissal is keyed on the specific daemon build it
// was dismissed for — rewritten onto the banner primitive's handle in
// phase 4. A dismissal that leaked across builds would hide a real
// mismatch, which is the whole reason the banner exists.
describe('daemon banner dismissal', () => {
  const stale = (daemonBuild: string) =>
    emit('daemon:stale', {
      severity: 'mismatch',
      daemonBuild,
      guiBuild: 'gui-1',
    });
  const dismiss = () =>
    (
      bannerEl.querySelector('.hv-banner__dismiss') as HTMLButtonElement
    ).dispatchEvent(new MouseEvent('click'));

  // A 'match' reconnect both hides the banner and clears the remembered
  // dismissal, which is exactly the clean slate these tests need. Without
  // it the first assertion below passes on the banner an earlier test in
  // this file left open, so a `stale()` that did nothing would still go
  // green.
  beforeEach(() => {
    emit('daemon:stale', {
      severity: 'match',
      daemonBuild: 'baseline',
      guiBuild: 'gui-1',
    });
    if (!bannerEl.hidden) throw new Error('scaffold: banner should be hidden');
  });

  it('stays down for the dismissed build and returns for a different one', () => {
    stale('daemon-1');
    expect(bannerEl.hidden).toBe(false);

    dismiss();
    expect(bannerEl.hidden).toBe(true);

    // Same build reconnecting: still dismissed.
    stale('daemon-1');
    expect(bannerEl.hidden).toBe(true);

    // A different mismatched build is a new fact.
    stale('daemon-2');
    expect(bannerEl.hidden).toBe(false);
  });

  it('clears the dismissal when the builds match again', () => {
    stale('daemon-3');
    dismiss();
    expect(bannerEl.hidden).toBe(true);

    emit('daemon:stale', {
      severity: 'match',
      daemonBuild: 'daemon-3',
      guiBuild: 'gui-1',
    });
    expect(bannerEl.hidden).toBe(true);

    // Reset means a later mismatch on that same build surfaces again.
    stale('daemon-3');
    expect(bannerEl.hidden).toBe(false);
  });
});

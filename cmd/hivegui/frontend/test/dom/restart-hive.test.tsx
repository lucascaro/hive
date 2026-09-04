// @vitest-environment jsdom
//
// restartHive (src/app/banners.ts) used to be an anonymous click
// handler on the daemon-stale banner's button — the only trigger in
// the whole app. With matching GUI/daemon builds the banner never
// renders, so there was no way to restart Hive at all. It is now an
// exported action the File menu and command palette call directly,
// and these tests pin the behaviour that made it safe to expose:
// confirm first, and surface a refusal instead of swallowing it.
//
// Phase 2: the markup is now <Banners /> (components/Banners.tsx), and
// the store (store/store.ts) holds what's rendered.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';
import { render, fireEvent, act } from '@testing-library/react';
import { resetStore } from '../../src/store/store.js';

const bridge = vi.hoisted(() => ({
  Confirm: vi.fn(() => Promise.resolve(true)),
  RestartDaemon: vi.fn(() => Promise.resolve()),
  RequestReloadAllGUIs: vi.fn(() => Promise.resolve()),
  CheckForUpdate: vi.fn(() => Promise.resolve()),
  OpenURL: vi.fn(() => Promise.resolve()),
  EventsOn: vi.fn(),
}));

vi.mock('../../src/bridge.js', () => bridge);

let restartHive: typeof import('../../src/app/banners.js').restartHive;
let reloadGui: typeof import('../../src/app/banners.js').reloadGui;
let isDaemonRestarting: typeof import('../../src/app/banners.js').isDaemonRestarting;
let initBanners: typeof import('../../src/app/banners.js').initBanners;
let Banners: typeof import('../../src/components/Banners.js')['Banners'];

// The handlers registered via EventsOn, replayed the way Go would.
function emit(event: string, payload: unknown) {
  act(() => {
    for (const call of bridge.EventsOn.mock.calls) {
      if (call[0] === event) (call[1] as (p: unknown) => void)(payload);
    }
  });
}

function bannerEl(): HTMLElement {
  const found = document.getElementById('daemon-banner');
  if (!found) throw new Error('missing #daemon-banner in test scaffold');
  return found;
}
function bannerText(): HTMLElement {
  const found = bannerEl().querySelector<HTMLElement>('.hv-banner__text');
  if (!found) throw new Error('missing .hv-banner__text in the daemon banner');
  return found;
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
  ({ restartHive, reloadGui, isDaemonRestarting, initBanners } = await import(
    '../../src/app/banners.js'
  ));
  initBanners();
});

beforeEach(() => {
  bridge.Confirm.mockClear().mockResolvedValue(true);
  bridge.RestartDaemon.mockClear().mockResolvedValue(undefined);
  bridge.RequestReloadAllGUIs.mockClear().mockResolvedValue(undefined);
  resetStore();
  // RTL's afterEach(cleanup) (setup-rtl.ts) unmounts the tree after every
  // test, so the island has to be remounted each time rather than once.
  render(<Banners />);
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
    expect(bannerEl().hidden).toBe(false);
    expect(bannerText().textContent).toMatch(
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
  const dismiss = () => {
    const btn = bannerEl().querySelector<HTMLButtonElement>(
      '.hv-banner__dismiss',
    );
    if (btn) fireEvent.click(btn);
  };

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
    if (!bannerEl().hidden)
      throw new Error('scaffold: banner should be hidden');
  });

  it('stays down for the dismissed build and returns for a different one', () => {
    stale('daemon-1');
    expect(bannerEl().hidden).toBe(false);

    dismiss();
    expect(bannerEl().hidden).toBe(true);

    // Same build reconnecting: still dismissed.
    stale('daemon-1');
    expect(bannerEl().hidden).toBe(true);

    // A different mismatched build is a new fact.
    stale('daemon-2');
    expect(bannerEl().hidden).toBe(false);
  });

  it('clears the dismissal when the builds match again', () => {
    stale('daemon-3');
    dismiss();
    expect(bannerEl().hidden).toBe(true);

    emit('daemon:stale', {
      severity: 'match',
      daemonBuild: 'daemon-3',
      guiBuild: 'gui-1',
    });
    expect(bannerEl().hidden).toBe(true);

    // Reset means a later mismatch on that same build surfaces again.
    stale('daemon-3');
    expect(bannerEl().hidden).toBe(false);
  });
});

// The reload/restart split is the point of the daemon-contract work:
// the daemon decides which of the two the user is offered, and the
// banner must never present the destructive one when the cheap one
// would do (or vice versa, which would silently leave a changed daemon
// running).
describe('daemon banner reload vs restart', () => {
  function actionLabels(): string[] {
    return Array.from(bannerEl().querySelectorAll<HTMLButtonElement>('button'))
      .filter((b) => !b.hidden && !b.classList.contains('hv-banner__dismiss'))
      .map((b) => b.textContent ?? '');
  }

  beforeEach(() => {
    emit('daemon:stale', {
      severity: 'match',
      daemonBuild: 'baseline',
      guiBuild: 'gui-1',
      guiContract: 1,
      daemonContract: 1,
    });
  });

  it('offers only Reload GUI when the contracts agree', () => {
    emit('daemon:stale', {
      severity: 'reloadable',
      daemonBuild: 'daemon-old',
      guiBuild: 'gui-new',
      guiContract: 1,
      daemonContract: 1,
    });

    expect(bannerEl().hidden).toBe(false);
    expect(actionLabels()).toEqual(['Reload GUI']);
    // The copy has to say the sessions survive — that is the whole
    // difference the user cares about.
    expect(bannerText().textContent).toMatch(/sessions keep running/i);
  });

  it('offers only Restart Hive when the contracts differ', () => {
    emit('daemon:stale', {
      severity: 'mismatch',
      daemonBuild: 'daemon-old',
      guiBuild: 'gui-new',
      guiContract: 2,
      daemonContract: 1,
    });

    expect(bannerEl().hidden).toBe(false);
    expect(actionLabels()).toEqual(['Restart Hive']);
    // And it must name the cost before the user clicks.
    expect(bannerText().textContent).toMatch(/ends every running session/i);
  });

  it('offers Restart when the daemon build cannot be verified', () => {
    emit('daemon:stale', {
      severity: 'unknown',
      daemonBuild: '',
      guiBuild: 'gui-new',
      guiContract: 1,
      daemonContract: 0,
    });

    expect(actionLabels()).toEqual(['Restart Hive']);
  });
});

// reloadGui destroys nothing, so it must NOT sit behind the confirm
// overlay: a dialog in front of a harmless action trains the user to
// click through the one that matters.
describe('reloadGui', () => {
  it('broadcasts without a confirmation dialog', async () => {
    await reloadGui();
    expect(bridge.Confirm).not.toHaveBeenCalled();
    expect(bridge.RequestReloadAllGUIs).toHaveBeenCalledTimes(1);
    expect(bridge.RestartDaemon).not.toHaveBeenCalled();
  });

  it('surfaces a failure in the banner', async () => {
    bridge.RequestReloadAllGUIs.mockRejectedValue(new Error('no control conn'));
    await reloadGui();
    expect(bannerEl().hidden).toBe(false);
    expect(bannerText().textContent).toMatch(/Reload failed.*no control conn/);
  });

  it('clears the restarting flag so the status bar does not flash', async () => {
    await reloadGui();
    expect(isDaemonRestarting()).toBe(false);
  });
});

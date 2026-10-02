// @vitest-environment jsdom
//
// Spec 477: app/keymap-sync.ts keeps the surfaces React does not render
// in step with the keymap — above all the native macOS menu, which Go
// builds and which takes a ⌘ chord before the webview ever sees it.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const bridge = vi.hoisted(() => ({
  GetKeymap: vi.fn(),
  SetMenuAccelerators: vi.fn((_a: Record<string, string>) => Promise.resolve()),
  SuspendMenuAccelerators: vi.fn((_on: boolean) => Promise.resolve()),
}));
vi.mock('../../src/bridge.js', () => bridge);
vi.mock('../../src/lib/platform.js', async (orig) => ({
  ...(await orig<typeof import('../../src/lib/platform.js')>()),
  isMac: true,
}));

import { resetStore, setKeymap, appStore } from '../../src/store/store.js';
import {
  initKeymapSync,
  loadKeymap,
  menuQueueSettledForTest,
  resetKeymapSyncForTest,
  setShortcutCapture,
} from '../../src/app/keymap-sync.js';

const refreshModeHint = vi.fn();

beforeEach(() => {
  resetStore();
  resetKeymapSyncForTest();
  bridge.GetKeymap.mockReset();
  bridge.SetMenuAccelerators.mockClear();
  bridge.SuspendMenuAccelerators.mockClear();
  refreshModeHint.mockClear();
  document.body.innerHTML =
    '<button id="new-project-btn" title="New project"></button>';
  initKeymapSync({
    refreshModeHint,
    menuCommands: ['new-session', 'new-project', 'settings'],
  });
});
afterEach(() => resetKeymapSyncForTest());

const title = () => document.getElementById('new-project-btn')?.title;

describe('native menu', () => {
  it('is reset to the defaults once per page load, even with no keymap', async () => {
    // Go outlives a webview reload: the menu may still hold an earlier
    // page's overrides, so the first load always tells it.
    bridge.GetKeymap.mockResolvedValue({});
    await loadKeymap();
    await menuQueueSettledForTest();
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(1);
    expect(bridge.SetMenuAccelerators).toHaveBeenLastCalledWith({});
    // ...and only once: later changes that move no item send nothing.
    setKeymap({ other: { 'new-session': ['Mod+Y'] } }); // not the mac half
    await loadKeymap();
    await menuQueueSettledForTest();
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(1);
  });
  it('is reset even when keymap.json cannot be read', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    bridge.GetKeymap.mockRejectedValue(new Error('parse keymap.json'));
    await loadKeymap();
    await menuQueueSettledForTest();
    expect(bridge.SetMenuAccelerators).toHaveBeenLastCalledWith({});
    warn.mockRestore();
  });
  it('gets one call per change to the items, with only those items', async () => {
    bridge.GetKeymap.mockResolvedValue({ mac: { 'new-session': ['Mod+Y'] } });
    await loadKeymap();
    await menuQueueSettledForTest();
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(1);
    expect(bridge.SetMenuAccelerators).toHaveBeenLastCalledWith({
      'new-session': 'cmdorctrl+y',
    });
    // A new object with the same items: nothing to rebuild.
    setKeymap({ mac: { 'new-session': ['Mod+Y'] } });
    await menuQueueSettledForTest();
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(1);
  });
  it('a reset after overrides restores every default with {}', async () => {
    setKeymap({ mac: { 'new-session': ['Mod+Y'] } });
    setKeymap({});
    await menuQueueSettledForTest();
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(2);
    expect(bridge.SetMenuAccelerators).toHaveBeenLastCalledWith({});
  });
  it('ignores store updates that are not the keymap', () => {
    appStore.setState({ status: { text: 'busy', isError: false } });
    appStore.setState({ activeId: 'x' });
    expect(bridge.SetMenuAccelerators).not.toHaveBeenCalled();
    expect(refreshModeHint).not.toHaveBeenCalled();
  });
});

describe('a menu update Go rejects', () => {
  it('is sent again on the next change instead of being lost', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    bridge.SetMenuAccelerators.mockRejectedValueOnce(new Error('no menu'));
    setKeymap({ mac: { 'new-session': ['Mod+Y'] } });
    await menuQueueSettledForTest();
    // Same items again: had the failure been recorded as sent, this would
    // be skipped and the menu would keep the old shortcut.
    setKeymap({ mac: { 'new-session': ['Mod+Y'] } });
    await menuQueueSettledForTest();
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(2);
    expect(bridge.SetMenuAccelerators).toHaveBeenLastCalledWith({
      'new-session': 'cmdorctrl+y',
    });
    warn.mockRestore();
  });
});

describe('updates in flight', () => {
  it('reach Go one at a time, in the order they were made', async () => {
    const order: string[] = [];
    let release: () => void = () => {};
    bridge.SetMenuAccelerators.mockImplementationOnce(
      (a: Record<string, string>) =>
        new Promise<void>((r) => {
          release = () => {
            order.push(JSON.stringify(a));
            r();
          };
        }),
    );
    bridge.SetMenuAccelerators.mockImplementationOnce(
      (a: Record<string, string>) => {
        order.push(JSON.stringify(a));
        return Promise.resolve();
      },
    );
    setKeymap({ mac: { 'new-session': ['Mod+Y'] } });
    setKeymap({ mac: { 'new-session': ['Mod+U'] } });
    await Promise.resolve();
    // The second has not been sent while the first is still in flight.
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(1);
    release();
    await menuQueueSettledForTest();
    expect(order).toEqual([
      '{"new-session":"cmdorctrl+y"}',
      '{"new-session":"cmdorctrl+u"}',
    ]);
  });
  it('an older failure does not undo a newer send', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    bridge.SetMenuAccelerators.mockRejectedValueOnce(new Error('no menu'));
    setKeymap({ mac: { 'new-session': ['Mod+Y'] } });
    setKeymap({ mac: { 'new-session': ['Mod+U'] } });
    await menuQueueSettledForTest();
    // ⌘U went through after ⌘Y failed, so ⌘U is what Go has: the same
    // keymap again sends nothing.
    setKeymap({ mac: { 'new-session': ['Mod+U'] } });
    await menuQueueSettledForTest();
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(2);
    warn.mockRestore();
  });
});

describe('other surfaces', () => {
  it('re-derives the status bar hints and the new-project title', () => {
    expect(title()).toBe('New project (⌘N)');
    setKeymap({ mac: { 'new-project': ['Mod+Shift+O'] } });
    expect(refreshModeHint).toHaveBeenCalledTimes(1);
    expect(title()).toBe('New project (⇧⌘O)');
    setKeymap({ mac: { 'new-project': [] } });
    expect(title()).toBe('New project');
  });
});

describe('loadKeymap', () => {
  it('a failed read keeps the defaults', async () => {
    bridge.GetKeymap.mockRejectedValue(new Error('parse keymap.json'));
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    await loadKeymap();
    expect(appStore.getState().keymap).toEqual({});
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });
  it('re-reading an unchanged file does not touch the store', async () => {
    bridge.GetKeymap.mockResolvedValue({ mac: { 'new-session': ['Mod+Y'] } });
    await loadKeymap();
    const first = appStore.getState().keymap;
    await loadKeymap();
    expect(appStore.getState().keymap).toBe(first);
    expect(refreshModeHint).toHaveBeenCalledTimes(1);
  });
});

// Each window is its own process: one that saves a keymap cannot tell the
// others, so every window re-reads keymap.json when it gains focus.
describe('re-read on window focus', () => {
  const focus = async () => {
    window.dispatchEvent(new Event('focus'));
    // loadKeymap awaits GetKeymap, then queues the menu.
    await vi.waitFor(() => expect(bridge.GetKeymap).toHaveBeenCalled());
    await new Promise((r) => setTimeout(r, 0));
    await menuQueueSettledForTest();
  };

  it('rebuilds nothing when the file did not change', async () => {
    bridge.GetKeymap.mockResolvedValue({ mac: { 'new-session': ['Mod+Y'] } });
    await loadKeymap();
    await menuQueueSettledForTest();
    bridge.SetMenuAccelerators.mockClear();
    const before = appStore.getState().keymap;
    // Same meaning, other spelling: Go adds a version, keys may reorder.
    bridge.GetKeymap.mockResolvedValue({
      version: 1,
      other: {},
      mac: { 'new-session': ['Mod+Y'] },
    });
    await focus();
    await focus();
    expect(appStore.getState().keymap).toBe(before);
    expect(bridge.SetMenuAccelerators).not.toHaveBeenCalled();
  });

  it('applies a keymap another window saved, once', async () => {
    bridge.GetKeymap.mockResolvedValue({});
    await loadKeymap();
    await menuQueueSettledForTest();
    bridge.SetMenuAccelerators.mockClear();
    bridge.GetKeymap.mockResolvedValue({ mac: { 'new-session': ['Mod+Y'] } });
    await focus();
    await focus();
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(1);
    expect(bridge.SetMenuAccelerators).toHaveBeenLastCalledWith({
      'new-session': 'cmdorctrl+y',
    });
  });
});

describe('shortcut capture', () => {
  it('suspends and restores the menu, once per change', async () => {
    setShortcutCapture(true);
    await menuQueueSettledForTest();
    setShortcutCapture(true);
    await menuQueueSettledForTest();
    setShortcutCapture(false);
    await menuQueueSettledForTest();
    expect(bridge.SuspendMenuAccelerators.mock.calls).toEqual([
      [true],
      [false],
    ]);
  });

  it('sends nothing for a capture that ends before Go was told', async () => {
    // Reconciled, not toggled: on-then-off with nothing sent in between
    // leaves Go where it was.
    setShortcutCapture(true);
    setShortcutCapture(false);
    await menuQueueSettledForTest();
    expect(bridge.SuspendMenuAccelerators).not.toHaveBeenCalled();
  });

  it('re-suspends after a menu update lands mid-capture', async () => {
    // Go lifts the suspension on every SetMenuAccelerators.
    setShortcutCapture(true);
    setKeymap({ mac: { 'new-session': ['Mod+Y'] } });
    await menuQueueSettledForTest();
    const order = [
      ...bridge.SuspendMenuAccelerators.mock.invocationCallOrder.map((n, i) => [
        n,
        `suspend:${bridge.SuspendMenuAccelerators.mock.calls[i][0]}`,
      ]),
      ...bridge.SetMenuAccelerators.mock.invocationCallOrder.map((n) => [
        n,
        'set',
      ]),
    ]
      .sort((a, b) => (a[0] as number) - (b[0] as number))
      .map((x) => x[1]);
    expect(order).toEqual(['suspend:true', 'set', 'suspend:true']);
  });
});

// A failed suspend / restore must not strand the native menu: Go's state
// is unknown after a failure, so the page keeps reconciling it with what
// it wants until a call succeeds.
describe('shortcut capture when Go fails', () => {
  const suspendCalls = () =>
    bridge.SuspendMenuAccelerators.mock.calls.map((c) => c[0]);

  afterEach(() => vi.useRealTimers());

  it('retries a failed restore, so the menu does not stay stripped', async () => {
    vi.useFakeTimers();
    setShortcutCapture(true);
    await menuQueueSettledForTest();
    bridge.SuspendMenuAccelerators.mockRejectedValueOnce(new Error('gone'));
    setShortcutCapture(false);
    await menuQueueSettledForTest();
    expect(suspendCalls()).toEqual([true, false]);
    await vi.advanceTimersByTimeAsync(250);
    await menuQueueSettledForTest();
    expect(suspendCalls()).toEqual([true, false, false]);
    // Restored: nothing further is sent.
    await vi.advanceTimersByTimeAsync(10_000);
    expect(suspendCalls()).toEqual([true, false, false]);
  });

  it('gives up after a few retries and reconciles on window focus', async () => {
    vi.useFakeTimers();
    setShortcutCapture(true);
    await menuQueueSettledForTest();
    bridge.SuspendMenuAccelerators.mockRejectedValue(new Error('gone'));
    setShortcutCapture(false);
    await vi.advanceTimersByTimeAsync(60_000);
    await menuQueueSettledForTest();
    // The first attempt plus three bounded retries.
    expect(suspendCalls()).toEqual([true, false, false, false, false]);
    bridge.SuspendMenuAccelerators.mockResolvedValue(undefined);
    bridge.GetKeymap.mockResolvedValue({});
    window.dispatchEvent(new Event('focus'));
    await vi.advanceTimersByTimeAsync(0);
    await menuQueueSettledForTest();
    expect(suspendCalls()).toEqual([true, false, false, false, false, false]);
  });

  it('a failed re-suspend does not count as a failed menu update', async () => {
    vi.useFakeTimers();
    setShortcutCapture(true);
    await menuQueueSettledForTest();
    bridge.SuspendMenuAccelerators.mockRejectedValueOnce(new Error('gone'));
    setKeymap({ mac: { 'new-session': ['Mod+Y'] } });
    await menuQueueSettledForTest();
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(1);
    // The re-suspend that failed is retried; the capture is still on.
    expect(suspendCalls()).toEqual([true, true]);
    await vi.advanceTimersByTimeAsync(250);
    await menuQueueSettledForTest();
    expect(suspendCalls()).toEqual([true, true, true]);
    // The same overrides again: Go has them, so they are not re-sent.
    setKeymap({ mac: { 'new-session': ['Mod+Y'] } });
    await menuQueueSettledForTest();
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(1);
  });
});

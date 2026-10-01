// @vitest-environment jsdom
//
// Spec 477: app/keymap-sync.ts keeps the surfaces React does not render
// in step with the keymap — above all the native macOS menu, which Go
// builds and which takes a ⌘ chord before the webview ever sees it.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const bridge = vi.hoisted(() => ({
  GetKeymap: vi.fn(),
  SetMenuAccelerators: vi.fn(() => Promise.resolve()),
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
  resetKeymapSyncForTest,
} from '../../src/app/keymap-sync.js';

const refreshModeHint = vi.fn();

beforeEach(() => {
  resetStore();
  resetKeymapSyncForTest();
  bridge.GetKeymap.mockReset();
  bridge.SetMenuAccelerators.mockClear();
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
  it('is never told anything while the keymap changes no item', async () => {
    bridge.GetKeymap.mockResolvedValue({});
    await loadKeymap();
    setKeymap({ other: { 'new-session': ['Mod+Y'] } }); // not the mac half
    expect(bridge.SetMenuAccelerators).not.toHaveBeenCalled();
  });
  it('gets one call per change to the items, with only those items', async () => {
    bridge.GetKeymap.mockResolvedValue({ mac: { 'new-session': ['Mod+Y'] } });
    await loadKeymap();
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(1);
    expect(bridge.SetMenuAccelerators).toHaveBeenLastCalledWith({
      'new-session': 'cmdorctrl+y',
    });
    // A new object with the same items: nothing to rebuild.
    setKeymap({ mac: { 'new-session': ['Mod+Y'] } });
    expect(bridge.SetMenuAccelerators).toHaveBeenCalledTimes(1);
  });
  it('a reset after overrides restores every default with {}', () => {
    setKeymap({ mac: { 'new-session': ['Mod+Y'] } });
    setKeymap({});
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

// @vitest-environment jsdom
//
// The UI plugin host (spec 471): src/app/plugin-host.ts loading plugin
// modules, and the containment src/components/PluginSurfaces.tsx puts
// around every piece of plugin code. The "module" is whatever the
// injected importer returns, so a plugin here is a plain object.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, cleanup, render } from '@testing-library/react';
import type { ReactNode } from 'react';
import type { PluginInfo, SessionInfo } from '../../src/app/state.js';
import {
  ACTIVATE_TIMEOUT_MS,
  IMPORT_TIMEOUT_MS,
  initPluginHost,
  pluginCommands,
  resolvePluginCommands,
  RENDER_TIMEOUT_MS,
  resetPluginHostForTest,
  type HiveAPI,
} from '../../src/app/plugin-host.js';
import {
  appStore,
  resetStore,
  setPluginUI,
  setPlugins,
  setSessions,
} from '../../src/store/store.js';
import {
  PluginBadges,
  PluginSettingsSection,
} from '../../src/components/PluginSurfaces.js';

const bridge = vi.hoisted(() => {
  const handlers = new Map<string, Set<(...a: unknown[]) => void>>();
  return {
    handlers,
    EventsOn: vi.fn((name: string, cb: (...a: unknown[]) => void) => {
      if (!handlers.has(name)) handlers.set(name, new Set());
      handlers.get(name)?.add(cb);
      return () => handlers.get(name)?.delete(cb);
    }),
    PluginAssetBase: vi.fn(() => Promise.resolve('')),
    ListPlugins: vi.fn(() => Promise.resolve()),
    SetPluginConfig: vi.fn((_id: string, _c: unknown) => Promise.resolve()),
    SetClientUI: vi.fn((_ids: string[]) => Promise.resolve()),
  };
});
vi.mock('../../src/bridge.js', () => bridge);

function emit(name: string, ...args: unknown[]) {
  for (const cb of bridge.handlers.get(name) ?? []) cb(...args);
}

function uiPlugin(id: string, over: Partial<PluginInfo> = {}): PluginInfo {
  return {
    id,
    name: id,
    version: '1.0.0',
    api_version: '0.2',
    source: `/src/${id}`,
    command: [],
    enabled: true,
    status: 'running',
    restarts: 0,
    ui: { entry: 'ui.mjs' },
    ...over,
  };
}

const session = {
  id: 's1',
  name: 'one',
  project_id: 'p1',
} as unknown as SessionInfo;

// Modules by plugin id; the importer resolves the URL's id to one.
let modules: Record<string, () => Promise<unknown>> = {};
const importer = vi.fn((url: string) => {
  const id = url.split('/')[2];
  const m = modules[id];
  return m ? m() : Promise.reject(new Error(`no module ${id}`));
});

function start() {
  initPluginHost({
    components: {},
    switchTo: vi.fn(),
    refocusActiveTerm: vi.fn(),
    importer,
  });
}

async function flush() {
  for (let i = 0; i < 6; i++) {
    await act(async () => {
      await Promise.resolve();
    });
  }
}

const ui = (id: string) => appStore.getState().pluginUI[id];

function Probe(): ReactNode {
  return <span id="probe">still here</span>;
}

function Tree(): ReactNode {
  return (
    <>
      <PluginBadges session={session} />
      <PluginSettingsSection id="bad" />
      <PluginSettingsSection id="good" />
      <Probe />
    </>
  );
}

beforeEach(() => {
  resetStore();
  resetPluginHostForTest();
  bridge.handlers.clear();
  for (const f of [
    bridge.EventsOn,
    bridge.PluginAssetBase,
    bridge.SetPluginConfig,
    bridge.SetClientUI,
    importer,
  ]) {
    f.mockClear();
  }
  modules = {};
  setSessions([session]);
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  vi.spyOn(console, 'error').mockImplementation(() => {});
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('plugin host: loading', () => {
  it('does nothing at all with zero UI plugins', async () => {
    setPlugins([
      uiPlugin('headless', { ui: undefined, command: ['node', 'main.mjs'] }),
      uiPlugin('off', { enabled: false, status: 'stopped' }),
      uiPlugin('old', { api_version: '0.1', status: 'refused' }),
    ]);
    start();
    await flush();
    expect(importer).not.toHaveBeenCalled();
    expect(bridge.PluginAssetBase).not.toHaveBeenCalled();
    expect(document.head.querySelector('link[data-plugin]')).toBeNull();
    expect(appStore.getState().pluginUI).toEqual({});
    // Nothing to announce either: the daemon hears from a window only
    // once it runs a plugin UI.
    expect(bridge.SetClientUI).not.toHaveBeenCalled();
  });

  it('activates an enabled UI plugin with a frozen hive object', async () => {
    let api: HiveAPI | null = null;
    modules.notes = async () => ({
      default: (hive: HiveAPI) => {
        api = hive;
        return { badge: () => ({ text: 'Note' }) };
      },
    });
    setPlugins([
      uiPlugin('notes', { ui: { entry: 'ui.mjs', style: 'ui.css' } }),
    ]);
    start();
    await flush();
    expect(importer).toHaveBeenCalledWith('/plugins/notes/ui.mjs?v=1.0.0-');
    expect(ui('notes')?.status).toBe('active');
    expect(api).not.toBeNull();
    expect(Object.isFrozen(api)).toBe(true);
    expect((api as unknown as HiveAPI).apiVersion).toBe('0.2');
    const link = document.head.querySelector(
      'link[data-plugin="notes"]',
    ) as HTMLLinkElement;
    expect(link?.getAttribute('href')).toBe('/plugins/notes/ui.css?v=1.0.0-');
  });

  it('disabling unmounts and removes the stylesheet', async () => {
    const deactivate = vi.fn();
    modules.notes = async () => ({
      default: () => ({ deactivate, badge: () => ({ text: 'Note' }) }),
    });
    setPlugins([
      uiPlugin('notes', { ui: { entry: 'ui.mjs', style: 'ui.css' } }),
    ]);
    start();
    await flush();
    const { container } = render(<PluginBadges session={session} />);
    expect(container.textContent).toBe('Note');
    act(() =>
      setPlugins([uiPlugin('notes', { enabled: false, status: 'stopped' })]),
    );
    await flush();
    expect(deactivate).toHaveBeenCalledOnce();
    expect(ui('notes')).toBeUndefined();
    expect(container.textContent).toBe('');
    expect(document.head.querySelector('link[data-plugin="notes"]')).toBeNull();
  });

  it('reloads on a reinstall (new version)', async () => {
    modules.notes = async () => ({ default: () => ({}) });
    setPlugins([uiPlugin('notes')]);
    start();
    await flush();
    act(() => setPlugins([uiPlugin('notes', { version: '1.0.1' })]));
    await flush();
    expect(importer).toHaveBeenCalledTimes(2);
    expect(importer).toHaveBeenLastCalledWith('/plugins/notes/ui.mjs?v=1.0.1-');
  });
});

describe('plugin host: containment', () => {
  it('a surface that throws fails only its plugin', async () => {
    modules.bad = async () => ({
      default: () => ({
        badge: () => {
          throw new Error('badge boom');
        },
        settings: () => {
          throw new Error('render boom');
        },
      }),
    });
    modules.good = async () => ({
      default: () => ({ settings: () => <span id="good-section">good</span> }),
    });
    setPlugins([uiPlugin('bad'), uiPlugin('good')]);
    start();
    await flush();
    const { container } = render(<Tree />);
    await flush();
    expect(ui('bad')?.status).toBe('failed');
    expect(ui('bad')?.error).toMatch(/boom/);
    expect(ui('good')?.status).toBe('active');
    expect(container.querySelector('#probe')?.textContent).toBe('still here');
    expect(container.querySelector('#good-section')).not.toBeNull();
    expect(container.querySelector('[data-plugin-id="bad"]')).toBeNull();
  });

  it('a module that never loads fails at the import timeout', async () => {
    vi.useFakeTimers();
    modules.slow = () => new Promise(() => {});
    setPlugins([uiPlugin('slow')]);
    start();
    await vi.advanceTimersByTimeAsync(IMPORT_TIMEOUT_MS + 10);
    expect(ui('slow')?.status).toBe('failed');
    expect(ui('slow')?.error).toMatch(/loading the module did not finish/);
  });

  it('an activate() that never resolves fails at its timeout, and a late answer is ignored', async () => {
    vi.useFakeTimers();
    let late: (v: unknown) => void = () => {};
    modules.hang = async () => ({
      default: () =>
        new Promise((r) => {
          late = r;
        }),
    });
    setPlugins([uiPlugin('hang')]);
    start();
    await vi.advanceTimersByTimeAsync(ACTIVATE_TIMEOUT_MS + 10);
    expect(ui('hang')?.status).toBe('failed');
    late({ badge: () => ({ text: 'late' }) });
    await vi.advanceTimersByTimeAsync(10);
    expect(ui('hang')?.status).toBe('failed');
  });

  it('a surface that suspends forever fails at the render timeout', async () => {
    vi.useFakeTimers();
    const never = new Promise(() => {});
    modules.sus = async () => ({
      default: () => ({
        settings: () => {
          throw never;
        },
      }),
    });
    setPlugins([uiPlugin('sus')]);
    start();
    await vi.advanceTimersByTimeAsync(10);
    render(
      <>
        <PluginSettingsSection id="sus" />
        <Probe />
      </>,
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(RENDER_TIMEOUT_MS + 10);
    });
    expect(ui('sus')?.status).toBe('failed');
    expect(document.querySelector('#probe')).not.toBeNull();
  });

  it('a throwing event handler fails the plugin and unsubscribes it', async () => {
    modules.ev = async () => ({
      default: (hive: HiveAPI) => {
        hive.on('session:event', () => {
          throw new Error('handler boom');
        });
        return {};
      },
    });
    setPlugins([uiPlugin('ev')]);
    start();
    await flush();
    expect(bridge.handlers.get('session:event')?.size).toBe(1);
    emit('session:event', '{"kind":"updated"}');
    expect(ui('ev')?.status).toBe('failed');
    expect(bridge.handlers.get('session:event')?.size).toBe(0);
  });

  it('coalesces a settings.set flood to one write in flight', async () => {
    let api: HiveAPI | null = null;
    modules.flood = async () => ({
      default: (hive: HiveAPI) => {
        api = hive;
        return {};
      },
    });
    let release: () => void = () => {};
    bridge.SetPluginConfig.mockImplementation(
      () =>
        new Promise<void>((r) => {
          release = r;
        }),
    );
    setPlugins([uiPlugin('flood')]);
    start();
    await flush();
    for (let i = 0; i < 500; i++)
      (api as unknown as HiveAPI).settings.set({ n: i });
    expect(bridge.SetPluginConfig).toHaveBeenCalledTimes(1);
    expect(bridge.SetPluginConfig).toHaveBeenLastCalledWith('flood', { n: 0 });
    release();
    await flush();
    expect(bridge.SetPluginConfig).toHaveBeenCalledTimes(2);
    expect(bridge.SetPluginConfig).toHaveBeenLastCalledWith('flood', {
      n: 499,
    });
    release();
    await flush();
    expect(bridge.SetPluginConfig).toHaveBeenCalledTimes(2);
  });

  it('refuses settings over the daemon cap', async () => {
    let api: HiveAPI | null = null;
    modules.big = async () => ({
      default: (hive: HiveAPI) => {
        api = hive;
        return {};
      },
    });
    setPlugins([uiPlugin('big')]);
    start();
    await flush();
    expect(() =>
      (api as unknown as HiveAPI).settings.set({ x: 'a'.repeat(70_000) }),
    ).toThrow(/limited/);
    expect(() =>
      (api as unknown as HiveAPI).settings.set([1] as unknown as object),
    ).toThrow(/plain object/);
    expect(bridge.SetPluginConfig).not.toHaveBeenCalled();
  });

  it('shows a write at once, until the daemon echoes it or it fails', async () => {
    let api: HiveAPI | null = null;
    modules.opt = async () => ({
      default: (hive: HiveAPI) => {
        api = hive;
        return {};
      },
    });
    let fail: (e: Error) => void = () => {};
    bridge.SetPluginConfig.mockImplementation(
      () =>
        new Promise<void>((_r, rej) => {
          fail = rej;
        }),
    );
    setPlugins([uiPlugin('opt', { config: { on: true } })]);
    start();
    await flush();
    const hive = api as unknown as HiveAPI;
    hive.settings.set({ on: false });
    expect(hive.settings.get()).toEqual({ on: false });
    // An echo of an older value does not settle it...
    act(() => setPlugins([uiPlugin('opt', { config: { on: true } })]));
    expect(hive.settings.get()).toEqual({ on: false });
    // ...the echo of this one does.
    act(() => setPlugins([uiPlugin('opt', { config: { on: false } })]));
    expect(appStore.getState().pluginConfigOverlay).toEqual({});
    // A write that fails is dropped and the list is re-read.
    hive.settings.set({ on: true });
    expect(hive.settings.get()).toEqual({ on: true });
    fail(new Error('no control'));
    await flush();
    expect(hive.settings.get()).toEqual({ on: false });
    expect(bridge.ListPlugins).toHaveBeenCalled();
  });
});

describe('plugin host: pluginCommands', () => {
  const cmd = (id: string) => ({ id, title: id, keys: { key: 'j' }, run() {} });
  const active = (...ids: string[]) => ({
    status: 'active',
    contrib: { commands: ids.map(cmd) },
  });

  afterEach(() => {
    resetPluginHostForTest();
    resetStore();
    vi.restoreAllMocks();
  });

  it('resolves once per state, so collision warnings do not repeat', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const state = { a: active('one'), b: active('two') };
    const first = pluginCommands(state);
    expect(pluginCommands(state)).toBe(first);
    expect(warn).toHaveBeenCalledTimes(1);
    expect(first.map((r) => r.bound)).toEqual([true, false]);
  });

  // Settings › Shortcuts resolves against its draft keymap; that must not
  // evict the live result, or its clash warnings repeat on the next key.
  it('a lookup for another keymap leaves the live result alone', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const state = { a: active('one'), b: active('two') };
    const first = pluginCommands(state);
    const draft = resolvePluginCommands(state, { mac: { 'new-session': [] } });
    expect(draft.map((r) => r.bound)).toEqual([true, false]);
    expect(pluginCommands(state)).toBe(first);
    expect(warn).toHaveBeenCalledTimes(1);
  });

  it('follows pluginUI: activation and deactivation take effect at once', () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    expect(pluginCommands()).toEqual([]);
    setPluginUI('a', active('one') as never);
    expect(pluginCommands().map((r) => r.command.id)).toEqual(['one']);
    setPluginUI('a', { status: 'failed' } as never);
    expect(pluginCommands()).toEqual([]);
    setPluginUI('a', null);
    expect(pluginCommands()).toEqual([]);
  });
});

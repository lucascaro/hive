// @vitest-environment jsdom
//
// Settings → Plugins (#460 phase 2): src/components/modals/PluginsPanel.tsx
// and the install correlation in src/app/plugins.ts. The daemon is
// modelled at the bridge: InstallPlugin answers the way hived does — an
// "added" PLUGIN_EVENT echoing the nonce, or a plugin_install_failed
// error echoing it — delivered through the same claim functions
// app/events.ts calls.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import type { PluginInfo } from '../../src/app/state.js';
import {
  claimPluginEvent,
  claimPluginInstallError,
  installPlugin,
  INSTALL_TIMEOUT_MS,
} from '../../src/app/plugins.js';
import {
  dropPlugin,
  resetStore,
  setPlugins,
  upsertPlugin,
} from '../../src/store/store.js';
import { PluginsPanel } from '../../src/components/modals/PluginsPanel.js';

function plugin(over: Partial<PluginInfo> = {}): PluginInfo {
  return {
    id: 'webhook',
    name: 'Webhook',
    version: '0.1.0',
    api_version: '0.1',
    source: '/src/webhook',
    command: ['node', 'main.mjs'],
    enabled: false,
    status: 'stopped',
    restarts: 0,
    ...over,
  };
}

// How the fake daemon answers the next InstallPlugin: with the plugin
// it installed, or with a failure message.
let installAnswer: { ok: PluginInfo } | { err: string } = { ok: plugin() };

const bridge = vi.hoisted(() => ({
  ListPlugins: vi.fn(() => Promise.resolve()),
  InstallPlugin: vi.fn((_s: string, _n: string) => Promise.resolve()),
  SetPluginEnabled: vi.fn((_id: string, _on: boolean) => Promise.resolve()),
  RemovePlugin: vi.fn((_id: string) => Promise.resolve()),
  Confirm: vi.fn((_t: string, _m: string) => Promise.resolve(true)),
  PickDirectory: vi.fn((_d: string) => Promise.resolve('')),
}));
vi.mock('../../src/bridge.js', () => bridge);

function answerInstall(_source: string, nonce: string): Promise<void> {
  // After the call returns, like a frame arriving on the read loop.
  setTimeout(() => {
    if ('ok' in installAnswer) {
      upsertPlugin(installAnswer.ok);
      claimPluginEvent({ kind: 'added', plugin: installAnswer.ok, nonce });
    } else {
      claimPluginInstallError({
        code: 'plugin_install_failed',
        message: installAnswer.err,
        nonce,
      });
    }
  }, 0);
  return Promise.resolve();
}

// Flushes the setTimeout above and every promise chained off it.
async function settle() {
  for (let i = 0; i < 5; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
  }
}

function mount(active = true, onError = vi.fn()) {
  const r = render(<PluginsPanel active={active} onError={onError} />);
  return { ...r, onError };
}

async function installFrom(container: HTMLElement, source: string) {
  const input = container.querySelector(
    '#settings-plugin-source',
  ) as HTMLInputElement;
  fireEvent.change(input, { target: { value: source } });
  fireEvent.click(container.querySelector('#settings-plugin-install')!);
  await settle();
}

beforeEach(() => {
  resetStore();
  installAnswer = { ok: plugin() };
  for (const f of Object.values(bridge)) f.mockClear();
  bridge.InstallPlugin.mockImplementation(answerInstall);
  bridge.Confirm.mockResolvedValue(true);
});
afterEach(() => cleanup());

describe('Settings → Plugins', () => {
  it('lists plugins with status', () => {
    setPlugins([
      plugin({ id: 'b', name: 'Bravo', enabled: true, status: 'running' }),
      plugin({
        id: 'a',
        name: 'Alpha',
        enabled: true,
        status: 'refused',
        status_detail: 'node not found on PATH',
      }),
      plugin({ id: 'c', name: 'Charlie' }),
    ]);
    const { container } = mount();
    const rows = [...container.querySelectorAll('.settings-plugin-row')];
    expect(rows.map((r) => r.getAttribute('data-plugin-id'))).toEqual([
      'a',
      'b',
      'c',
    ]);
    expect(rows.map((r) => r.getAttribute('data-status'))).toEqual([
      'refused',
      'running',
      'disabled',
    ]);
    expect(rows[0].textContent).toContain('node not found on PATH');
    const boxes = rows.map(
      (r) => r.querySelector('.settings-plugin-enabled') as HTMLInputElement,
    );
    expect(boxes.map((b) => b.checked)).toEqual([true, true, false]);
  });

  it('shows the empty state with no plugins', () => {
    const { container } = mount();
    expect(container.querySelector('#settings-plugins-empty')).not.toBeNull();
    expect(container.querySelector('#settings-plugins-list')).toBeNull();
  });

  it('lists once, on first activation of the tab', () => {
    const { rerender } = mount(false);
    expect(bridge.ListPlugins).not.toHaveBeenCalled();
    rerender(<PluginsPanel active onError={vi.fn()} />);
    rerender(<PluginsPanel active={false} onError={vi.fn()} />);
    rerender(<PluginsPanel active onError={vi.fn()} />);
    expect(bridge.ListPlugins).toHaveBeenCalledTimes(1);
  });

  it('install shows trust confirm and enables on accept', async () => {
    const { container } = mount();
    await installFrom(container, '  /src/webhook  ');
    expect(bridge.InstallPlugin).toHaveBeenCalledWith(
      '/src/webhook',
      expect.any(String),
    );
    expect(bridge.Confirm).toHaveBeenCalledTimes(1);
    const [title, body] = bridge.Confirm.mock.calls[0];
    expect(title).toContain('Webhook');
    expect(body).toContain('/src/webhook');
    expect(body).toContain('node main.mjs');
    expect(body).toContain('full user privileges');
    expect(bridge.SetPluginEnabled).toHaveBeenCalledWith('webhook', true);
    expect(bridge.RemovePlugin).not.toHaveBeenCalled();
    expect(
      (container.querySelector('#settings-plugin-source') as HTMLInputElement)
        .value,
    ).toBe('');
  });

  it('install removes on decline', async () => {
    bridge.Confirm.mockResolvedValue(false);
    const { container } = mount();
    await installFrom(container, '/src/webhook');
    expect(bridge.Confirm).toHaveBeenCalledTimes(1);
    expect(bridge.RemovePlugin).toHaveBeenCalledWith('webhook');
    expect(bridge.SetPluginEnabled).not.toHaveBeenCalled();
  });

  it('shows the pinned commit of a git install in the prompt', async () => {
    installAnswer = {
      ok: plugin({
        source: 'https://example.com/webhook.git',
        commit: '0123456789abcdef0123',
      }),
    };
    const { container } = mount();
    await installFrom(container, 'https://example.com/webhook.git');
    expect(bridge.Confirm.mock.calls[0][1]).toContain(
      'https://example.com/webhook.git @ 0123456789ab',
    );
  });

  it('a failed install surfaces the error and never prompts', async () => {
    installAnswer = { err: 'no hive-plugin.json' };
    const { container, onError } = mount();
    await installFrom(container, '/src/nothing');
    expect(bridge.Confirm).not.toHaveBeenCalled();
    expect(bridge.SetPluginEnabled).not.toHaveBeenCalled();
    expect(onError).toHaveBeenLastCalledWith(
      'plugin install failed: no hive-plugin.json',
    );
  });

  it('added event not initiated by this window shows no Confirm', async () => {
    const { container } = mount();
    act(() => {
      const p = plugin({ id: 'other', name: 'Other' });
      upsertPlugin(p);
      claimPluginEvent({ kind: 'added', plugin: p, nonce: 'someone-else' });
      claimPluginEvent({ kind: 'added', plugin: p });
    });
    await settle();
    expect(bridge.Confirm).not.toHaveBeenCalled();
    expect(container.querySelector('[data-plugin-id="other"]')).not.toBeNull();
  });

  it('toggle calls SetPluginEnabled', () => {
    setPlugins([plugin({ enabled: true, status: 'running' })]);
    const { container } = mount();
    fireEvent.click(container.querySelector('.settings-plugin-enabled')!);
    expect(bridge.SetPluginEnabled).toHaveBeenCalledWith('webhook', false);
  });

  it('remove goes through Confirm', async () => {
    setPlugins([plugin()]);
    const { container } = mount();
    const btn = container.querySelector('.settings-plugin-remove')!;

    bridge.Confirm.mockResolvedValueOnce(false);
    fireEvent.click(btn);
    await settle();
    expect(bridge.Confirm).toHaveBeenCalledTimes(1);
    expect(bridge.RemovePlugin).not.toHaveBeenCalled();

    fireEvent.click(btn);
    await settle();
    expect(bridge.RemovePlugin).toHaveBeenCalledWith('webhook');
  });

  it('plugin:event updates the row live', () => {
    setPlugins([plugin()]);
    const { container } = mount();
    act(() =>
      upsertPlugin(plugin({ enabled: true, status: 'crashed', restarts: 2 })),
    );
    const row = container.querySelector('[data-plugin-id="webhook"]')!;
    expect(row.getAttribute('data-status')).toBe('crashed');
    expect(row.textContent).toContain('2 restarts');
    act(() => dropPlugin('webhook'));
    expect(container.querySelector('[data-plugin-id="webhook"]')).toBeNull();
  });
});

describe('installPlugin correlation', () => {
  it('ignores another window’s failure', async () => {
    bridge.InstallPlugin.mockImplementation(() => Promise.resolve());
    const done = vi.fn();
    installPlugin('/x').then(done, done);
    await settle();
    expect(
      claimPluginInstallError({
        code: 'plugin_install_failed',
        message: 'nope',
        nonce: 'not-mine',
      }),
    ).toBe(false);
    expect(claimPluginInstallError({ code: 'other', nonce: 'x' })).toBe(false);
    await settle();
    expect(done).not.toHaveBeenCalled();
    // Settle it so the pending entry does not outlive the test.
    const nonce = bridge.InstallPlugin.mock.calls[0][1];
    expect(
      claimPluginInstallError({ code: 'plugin_install_failed', nonce }),
    ).toBe(true);
  });

  it('rejects when the binding itself fails', async () => {
    bridge.InstallPlugin.mockRejectedValueOnce(new Error('no control'));
    await expect(installPlugin('/x')).rejects.toThrow('no control');
  });

  it('times out when the daemon never answers', async () => {
    vi.useFakeTimers();
    try {
      bridge.InstallPlugin.mockImplementation(() => Promise.resolve());
      const p = installPlugin('/x');
      const assertion = expect(p).rejects.toThrow('timed out');
      await vi.advanceTimersByTimeAsync(INSTALL_TIMEOUT_MS);
      await assertion;
      // A late echo after the timeout finds nothing to settle.
      const nonce = bridge.InstallPlugin.mock.calls[0][1];
      expect(
        claimPluginInstallError({ code: 'plugin_install_failed', nonce }),
      ).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });
});

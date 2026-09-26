// Plugin install correlation (#460). INSTALL_PLUGIN has no reply of
// its own: the daemon answers with a PLUGIN_EVENT "added" fanned out
// to EVERY window, or a plugin_install_failed error to this one. Each
// install carries a nonce the daemon echoes on either, so the window
// that asked — and only that window — resolves its promise and shows
// the trust prompt. Every other window just renders the new, disabled
// row from the fan-out (events.ts).

import { InstallPlugin, ListPlugins } from '../bridge.js';
import type { PluginEvent, PluginInfo } from './state.js';

// Outlasts the daemon's own 120s bound on a git clone
// (internal/plugin/install.go), so a slow clone that does finish is
// still answered here and prompted for. Past it the install may still
// land, through the fan-out, as a disabled row — and enabling that row
// asks for consent itself (PluginsPanel toggle), so nothing is skipped.
export const INSTALL_TIMEOUT_MS = 130_000;

interface Pending {
  resolve: (p: PluginInfo) => void;
  reject: (e: Error) => void;
  timer: ReturnType<typeof setTimeout>;
}

const pending = new Map<string, Pending>();
let seq = 0;

function newNonce(): string {
  seq += 1;
  return `${Date.now().toString(36)}-${seq}-${Math.random().toString(36).slice(2, 10)}`;
}

/** Installs from a local directory or git URL and resolves with the
 * installed (disabled) plugin once the daemon confirms it. */
export function installPlugin(source: string): Promise<PluginInfo> {
  const nonce = newNonce();
  return new Promise<PluginInfo>((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(nonce);
      reject(
        new Error(
          'no answer from Hive yet — if the install finishes it appears ' +
            'below, turned off; turning it on asks first',
        ),
      );
    }, INSTALL_TIMEOUT_MS);
    pending.set(nonce, { resolve, reject, timer });
    InstallPlugin(source, nonce).catch((e: unknown) => {
      if (!settle(nonce)) return;
      reject(e instanceof Error ? e : new Error(String(e)));
    });
  });
}

// Removes and disarms the entry; false when it was already settled.
function settle(nonce: string): boolean {
  const p = pending.get(nonce);
  if (!p) return false;
  clearTimeout(p.timer);
  pending.delete(nonce);
  return true;
}

/** Resolves this window's install when ev is its "added" echo. */
export function claimPluginEvent(ev: PluginEvent): void {
  if (ev.kind !== 'added' || !ev.nonce) return;
  const p = pending.get(ev.nonce);
  if (p && settle(ev.nonce)) p.resolve(ev.plugin);
}

/** Rejects this window's install when e is its failure echo. Reports
 * whether it was claimed, so the generic error line can stand aside. */
export function claimPluginInstallError(e: {
  code?: string;
  message?: string;
  nonce?: string;
}): boolean {
  if (e.code !== 'plugin_install_failed' || !e.nonce) return false;
  const p = pending.get(e.nonce);
  if (!p || !settle(e.nonce)) return false;
  p.reject(new Error(e.message || 'install failed'));
  return true;
}

// Whether this window has asked for the plugin list at all. Plugins are
// listed on demand (the Settings tab), not at boot, so a control
// reconnect re-lists only when something is showing them — the fan-out
// that kept the store current was lost with the old connection.
let wanted = false;

export function markPluginsWanted(): void {
  wanted = true;
}

/** Called after a control reconnect. */
export function relistPluginsIfWanted(): void {
  if (!wanted) return;
  ListPlugins().catch(() => {
    /* the tab re-lists on its next open */
  });
}

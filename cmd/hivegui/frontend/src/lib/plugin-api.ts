// The contract between the Hive app and a UI plugin (plugin API 0.2,
// docs/plugins.md). Types plus the pure pieces of the host — chord
// labels and conflict resolution, descriptor checks — so they are
// unit-testable without a DOM. app/plugin-host.ts is the loader;
// components/PluginSurfaces.tsx renders what plugins contribute.
//
// Pure module: no DOM, no store.

import { chordsOverlap, parseChord } from './chord.js';
import { chordLabel as formatChord } from './chord-label.js';

/** The plugin API this app implements; manifests name it exactly. */
export const PLUGIN_API_VERSION = '0.2';

/** A palette chord: the platform modifier (⌘ / Ctrl), optionally Shift,
 * and one letter or digit. Nothing else is bindable by a plugin: Alt
 * rewrites e.key on macOS and is AltGr on Windows, and bare keys belong
 * to the terminal. */
export interface PluginKeys {
  key: string;
  shift?: boolean;
}

export interface PluginCommand {
  id: string;
  title: string;
  keys?: PluginKeys;
  run: () => void;
}

/** A short marker beside a session's name in the sidebar. */
export interface PluginBadge {
  text: string;
  title?: string;
  tone?: 'neutral' | 'info' | 'warn';
}

/** A notice bar above the terminal for the active session. */
export interface PluginBanner {
  text: string;
  action?: { label: string; run: () => void };
}

export interface ModalHintSpec {
  keys: string;
  label: string;
}

// Component types stay loose here (unknown) so this module needs no
// React import; PluginSurfaces narrows them where it renders.
export interface PluginContributions {
  sessionView?: {
    modal?: {
      /** A string, or one computed from the session and the props
       * openSessionView was given. */
      title: string | ((session: unknown, props: unknown) => string);
      hints?: ModalHintSpec[];
      component: unknown;
    };
    panel?: { title: string; component: unknown };
    banner?: (session: unknown) => PluginBanner | null;
  };
  commands?: PluginCommand[];
  badge?: (session: unknown) => PluginBadge | null;
  settings?: unknown;
  deactivate?: () => void;
}

export type PluginUIStatus = 'loading' | 'active' | 'failed';

export interface PluginUIState {
  status: PluginUIStatus;
  /** Why it failed, for the Settings row. */
  error?: string;
  contrib?: PluginContributions;
}

const KEY_RE = /^[A-Za-z0-9]$/;

/** The chord's label in the same style as the core shortcuts, or null
 * when the keys are not bindable. */
export function chordLabel(keys: PluginKeys, isMac: boolean): string | null {
  if (!keys || typeof keys.key !== 'string' || !KEY_RE.test(keys.key)) {
    return null;
  }
  const chord = pluginChord(keys);
  return chord ? formatChord(chord, isMac) : null;
}

/** The chord string (lib/chord.ts) a plugin's keys bind, or null when
 * they are not bindable. Matched on e.code so a layout never changes the
 * answer; exact on Shift and Alt, and `Mod` rejects the other platform
 * modifier, like every core chord. */
export function pluginChord(keys: PluginKeys): string | null {
  if (!keys || typeof keys.key !== 'string' || !KEY_RE.test(keys.key)) {
    return null;
  }
  const k = keys.key.toUpperCase();
  const code = /[0-9]/.test(k) ? `Digit${k}` : `Key${k}`;
  return `Mod+${keys.shift ? 'Shift+' : ''}[${code}]`;
}

export interface ResolvedCommand {
  pluginId: string;
  command: PluginCommand;
  /** The chord labels, or '' when the command has none (or lost them). */
  shortcut: string;
  /** Whether keys dispatch to it; false when it has no chord left. */
  bound: boolean;
  /** The chords keys dispatch to it (lib/chord.ts strings). */
  chords: string[];
}

/** Resolves every plugin's commands in plugin order. A command's chords
 * are the user's keymap override for `plugin:<plugin>:<command>` when
 * there is one, else the plugin's own keys. A chord that overlaps one in
 * `taken` (core's live chords and the reserved ones) is refused — core
 * always wins — as is one an earlier plugin took; either way the command
 * stays in the palette, just without that key. */
export function resolveCommands(
  plugins: { id: string; commands: PluginCommand[] }[],
  taken: readonly string[],
  isMac: boolean,
  warn: (msg: string) => void = () => {},
  overrides: Readonly<Record<string, readonly string[]>> = {},
): ResolvedCommand[] {
  const claimed = [...taken];
  const out: ResolvedCommand[] = [];
  for (const p of plugins) {
    for (const command of p.commands) {
      const id = `plugin:${p.id}:${command.id}`;
      const own = command.keys ? pluginChord(command.keys) : null;
      if (command.keys && own === null) {
        warn(`plugin ${p.id}: command ${command.id} has an unbindable key`);
      }
      // keymap.json can be hand-edited: an override that is not a list
      // is ignored, like a core one (lib/bindings.ts).
      const o = overrides[id];
      const wanted = Array.isArray(o) ? [...o] : own ? [own] : [];
      const chords: string[] = [];
      for (const chord of wanted) {
        try {
          parseChord(chord, isMac);
        } catch {
          warn(
            `plugin ${p.id}: ignoring keymap chord "${chord}" for ${command.id}`,
          );
          continue;
        }
        if (claimed.some((c) => chordsOverlap(c, chord, isMac))) {
          warn(
            `plugin ${p.id}: ${formatChord(chord, isMac)} is already taken; ${command.id} does not get it`,
          );
          continue;
        }
        chords.push(chord);
        claimed.push(chord);
      }
      out.push({
        pluginId: p.id,
        command,
        shortcut: chords.map((c) => formatChord(c, isMac)).join(' / '),
        bound: chords.length > 0,
        chords,
      });
    }
  }
  return out;
}

/** Checks what activate() returned; throws with the first problem, which
 * becomes the plugin's failure reason. */
export function checkContributions(c: unknown): PluginContributions {
  if (c === undefined || c === null) return {};
  if (typeof c !== 'object')
    throw new Error('activate() must return an object');
  const k = c as PluginContributions;
  if (k.commands !== undefined) {
    if (!Array.isArray(k.commands))
      throw new Error('commands must be an array');
    for (const cmd of k.commands) {
      if (
        !cmd ||
        typeof cmd.id !== 'string' ||
        typeof cmd.title !== 'string' ||
        typeof cmd.run !== 'function'
      ) {
        throw new Error(
          'each command needs a string id, a string title and a run function',
        );
      }
    }
  }
  for (const f of ['badge', 'deactivate'] as const) {
    if (k[f] !== undefined && typeof k[f] !== 'function')
      throw new Error(`${f} must be a function`);
  }
  const v = k.sessionView;
  if (v !== undefined) {
    if (typeof v !== 'object' || v === null)
      throw new Error('sessionView must be an object');
    if (v.banner !== undefined && typeof v.banner !== 'function')
      throw new Error('sessionView.banner must be a function');
    for (const s of ['modal', 'panel'] as const) {
      const part = v[s];
      if (
        part !== undefined &&
        (typeof part !== 'object' ||
          part === null ||
          !(
            typeof part.title === 'string' ||
            (s === 'modal' && typeof part.title === 'function')
          ) ||
          !part.component)
      ) {
        throw new Error(
          s === 'modal'
            ? 'sessionView.modal needs a title (a string or a function) and a component'
            : 'sessionView.panel needs a string title and a component',
        );
      }
    }
  }
  return k;
}

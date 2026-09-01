// Preset selection. Runs before first paint (imported first in main.ts) so
// the app never flashes the default preset. docs/design-docs/ui/themes.md
// NOTE: index.html contains a matching inline <script> that stamps data-theme
// synchronously before stylesheets load. Keep both in sync, especially PRESETS.
export type ThemeName = 'classic' | 'hive-dark' | 'hive-light' | 'system';
export const THEME_KEY = 'hive.theme';
export const DEFAULT_THEME: ThemeName = 'classic';
export interface Preset {
  id: ThemeName;
  label: string;
}

// The picker renders from this list, so adding a preset in phase 6
// (native-dark, native-light, terminal) is one line here plus its block
// in themes.css — no UI change. Order is the order shown.
export const PRESETS: readonly Preset[] = [
  { id: 'system', label: 'System' },
  { id: 'hive-dark', label: 'Hive Dark' },
  { id: 'hive-light', label: 'Hive Light' },
  { id: 'classic', label: 'Classic' },
];

// Everything resolveTheme can stamp on <html>. 'system' is a selection,
// not a value, so it is excluded.
const STAMPABLE = new Set<string>(
  PRESETS.map((p) => p.id).filter((id) => id !== 'system'),
);

export function resolveTheme(
  stored: string | null,
  prefersDark: boolean,
): Exclude<ThemeName, 'system'> {
  if (stored === 'system') return prefersDark ? 'hive-dark' : 'hive-light';
  if (stored && STAMPABLE.has(stored))
    return stored as Exclude<ThemeName, 'system'>;
  return DEFAULT_THEME === 'system'
    ? prefersDark
      ? 'hive-dark'
      : 'hive-light'
    : DEFAULT_THEME;
}

export function readTheme(storage?: Storage): ThemeName {
  try {
    const s = storage ?? localStorage;
    const v = s.getItem(THEME_KEY);
    return v === 'system' || STAMPABLE.has(v ?? '')
      ? (v as ThemeName)
      : DEFAULT_THEME;
  } catch {
    return DEFAULT_THEME;
  }
}

export function applyTheme(name: ThemeName, doc: Document = document): void {
  const prefersDark =
    doc.defaultView?.matchMedia?.('(prefers-color-scheme: dark)').matches ??
    true;
  doc.documentElement.dataset.theme = resolveTheme(name, prefersDark);
}

export function xtermTheme(doc: Document = document) {
  const cs = getComputedStyle(doc.documentElement);
  const v = (n: string) => cs.getPropertyValue(n).trim();
  const accent = v('--accent');
  return {
    background: v('--term-bg'),
    foreground: v('--term-fg'),
    cursor: accent,
    cursorAccent: v('--on-accent'),
    // color-mix isn't resolvable via getPropertyValue; xterm accepts 8-digit hex.
    selectionBackground: accent.length === 7 ? `${accent}4d` : accent,
  };
}

export const OVERRIDES_KEY = 'hive.themeOverrides';
export const OVERRIDES_STYLE_ID = 'theme-overrides';

export interface Sanitized {
  css: string;
  rejected: string[];
}

// Custom property names only, lowercase — the whole token vocabulary is
// lowercase, and allowing case would let `--Accent` sit in the store
// looking like it should work.
const NAME = /^--[a-z0-9-]+$/;
// The value may not contain anything that could end the declaration,
// end the :root block, start a new rule, fetch a resource, or close the
// <style> element it is injected into.
const BAD_VALUE = /[{}<>;]|url\s*\(|expression\s*\(|@import/i;

// sanitizeOverrides turns whatever the user typed into declarations
// that are safe to drop inside `:root { … }`, plus the lines it refused
// so the dialog can say which ones and why (patterns.md › Errors:
// "errors that block a dialog go in the dialog's error slot").
//
// Splitting on both ';' and newline means a value can never contain a
// semicolon. That is the spec (themes.md), and no token value needs one.
export function sanitizeOverrides(input: string): Sanitized {
  const css: string[] = [];
  const rejected: string[] = [];
  for (const raw of String(input ?? '').split(/[;\n]/)) {
    const line = raw.trim();
    if (!line) continue;
    const i = line.indexOf(':');
    const name = i < 0 ? '' : line.slice(0, i).trim();
    const value = i < 0 ? '' : line.slice(i + 1).trim();
    if (!NAME.test(name) || !value || BAD_VALUE.test(value)) {
      rejected.push(line);
      continue;
    }
    css.push(`${name}: ${value};`);
  }
  return { css: css.join('\n  '), rejected };
}

export function readOverrides(storage?: Storage): string {
  try {
    const s = storage ?? localStorage;
    // Re-sanitised, not trusted: the store is hand-editable and this
    // text is injected into a <style>.
    return sanitizeOverrides(s.getItem(OVERRIDES_KEY) ?? '').css;
  } catch {
    return '';
  }
}

// writeOverrides stores the SANITISED text, so index.html's boot script
// can inject it before first paint without a second copy of the
// sanitiser (and without a flash of un-overridden colours).
export function writeOverrides(css: string, storage?: Storage): void {
  try {
    (storage ?? localStorage).setItem(
      OVERRIDES_KEY,
      sanitizeOverrides(css).css,
    );
  } catch {
    // Private mode / denied storage: the override still applies to this
    // session, it just will not survive a restart. Nothing to report.
  }
}

// applyOverrides rewrites the <style id="theme-overrides"> that
// index.html declares after themes.css — overrides beat presets by
// cascade ORDER, not by specificity, so the element's position matters
// and it is never moved or recreated.
export function applyOverrides(css: string, doc: Document = document): void {
  let el = doc.getElementById(OVERRIDES_STYLE_ID);
  if (!el) {
    el = doc.createElement('style');
    el.id = OVERRIDES_STYLE_ID;
    doc.head.appendChild(el);
  }
  el.textContent = css ? `:root {\n  ${css}\n}` : '';
}

// Side effect on import: stamp before anything renders. index.html's
// inline script has already done both from raw localStorage; this run
// re-does them from the sanitising path, which is what makes a
// hand-edited store harmless.
if (typeof document !== 'undefined') {
  applyTheme(readTheme());
  applyOverrides(readOverrides());
}

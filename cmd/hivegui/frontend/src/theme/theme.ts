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
// in themes.css — plus the duplicated list in index.html's pre-paint
// script, which cannot import this module and is checked against this
// one by test/e2e/theme.spec.ts. Order is the order shown.
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

// The sixteen ANSI slots, in the order the tokens are numbered, mapped
// to the keys xterm's ITheme uses.
const ANSI_KEYS = [
  'black',
  'red',
  'green',
  'yellow',
  'blue',
  'magenta',
  'cyan',
  'white',
  'brightBlack',
  'brightRed',
  'brightGreen',
  'brightYellow',
  'brightBlue',
  'brightMagenta',
  'brightCyan',
  'brightWhite',
] as const;

export function xtermTheme(doc: Document = document) {
  const cs = getComputedStyle(doc.documentElement);
  const v = (n: string) => cs.getPropertyValue(n).trim();
  const accent = v('--accent');
  // A slot the preset does not define is omitted, never sent as '': an
  // empty string is not a colour, and xterm keeping its own default is
  // the right answer for a token that isn't there (jsdom, which
  // resolves no stylesheet, is the case that proves it).
  const ansi: Record<string, string> = {};
  ANSI_KEYS.forEach((key, i) => {
    const value = v(`--ansi-${i}`);
    if (value) ansi[key] = value;
  });
  return {
    ...ansi,
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
// end the :root block, start a new rule, open a comment (`/*` with no
// terminator swallows every later declaration, permanently), carry a
// CSS escape (`\75rl(` tokenizes as `url(`), or close the <style>
// element it is injected into.
// `/` and `*` stay legal on their own — `calc(var(--space-2) * 2)` and
// `rgb(0 0 0 / 50%)` are ordinary token values; only the comment
// delimiters are refused.
const BAD_VALUE = /[{}<>;@\\]|\/\*|\*\//;

// Functions are allow-listed, not deny-listed. A denylist of url() and
// friends leaked: `image-set("https://…")` fetches a remote resource
// just as url() does, and every future CSS function that can reach the
// network would have to be remembered here. This is the set a design
// token legitimately needs.
const ALLOWED_FN = new Set([
  'var',
  'calc',
  'min',
  'max',
  'clamp',
  'rgb',
  'rgba',
  'hsl',
  'hsla',
  'hwb',
  'lab',
  'lch',
  'oklab',
  'oklch',
  'color',
  'color-mix',
]);
const FN_CALL = /([a-z-]*)\(/gi;

// Unbalanced parentheses are rejected rather than passed through: an
// open `(` swallows the `;` this function appends, then every later
// declaration and the closing brace, so one half-typed value silently
// destroys the whole override block — with nothing reported, because
// the line itself looked fine.
function valueIsSafe(value: string): boolean {
  if (BAD_VALUE.test(value)) return false;
  let depth = 0;
  for (const ch of value) {
    if (ch === '(') depth++;
    else if (ch === ')' && --depth < 0) return false;
  }
  if (depth !== 0) return false;
  FN_CALL.lastIndex = 0;
  for (const m of value.matchAll(FN_CALL)) {
    if (!ALLOWED_FN.has(m[1].toLowerCase())) return false;
  }
  return true;
}

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
    if (!NAME.test(name) || !value || !valueIsSafe(value)) {
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
// index.html declares after themes.css. The element's position is what
// puts overrides last in the cascade, so it is never moved or recreated
// — but position alone is not enough: every preset block in themes.css
// is `:root[data-theme="…"]` (0,2,0), which outranks a plain `:root`
// (0,1,0) whatever the order. `:root:root` matches exactly the same
// element at the same 0,2,0, so the later rule wins the tie. Keep this
// selector in sync with index.html's pre-paint boot script.
export function applyOverrides(css: string, doc: Document = document): void {
  let el = doc.getElementById(OVERRIDES_STYLE_ID);
  if (!el) {
    el = doc.createElement('style');
    el.id = OVERRIDES_STYLE_ID;
    doc.head.appendChild(el);
  }
  el.textContent = css ? `:root:root {\n  ${css}\n}` : '';
}

// Side effect on import: stamp before anything renders. index.html's
// inline script has already done both from raw localStorage; this run
// re-does them from the sanitising path, which is what makes a
// hand-edited store harmless.
if (typeof document !== 'undefined') {
  applyTheme(readTheme());
  applyOverrides(readOverrides());
}

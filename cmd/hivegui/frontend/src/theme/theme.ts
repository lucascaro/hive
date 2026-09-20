// Preset selection. Runs before first paint (imported first in main.tsx) so
// the app never flashes the default preset. docs/design-docs/ui/themes.md
// NOTE: index.html contains a matching inline <script> that stamps data-theme
// synchronously before stylesheets load. Keep both in sync, especially PRESETS.
export type ThemeName =
  | 'classic'
  | 'hive-dark'
  | 'hive-light'
  | 'native-dark'
  | 'native-light'
  | 'terminal'
  | 'system'
  // Community ports (spec 305). Ordered as the picker shows them.
  | 'dracula'
  | 'alucard'
  | 'nord'
  | 'gruvbox-dark'
  | 'tokyo-night'
  | 'catppuccin-mocha'
  | 'one-dark'
  | 'neon'
  | 'solarized-dark'
  | 'solarized-light'
  | 'catppuccin-latte'
  | 'github-dark'
  | 'github-light'
  | 'hex';
export const THEME_KEY = 'hive.theme';
// Phase 6: new installs follow the OS. Users who already set a preset keep
// it — readTheme() only falls back when the stored value is absent or
// garbage. index.html's pre-paint script hard-codes the same fallback;
// keep the two in sync.
export const DEFAULT_THEME: ThemeName = 'system';
// What 'system' resolves to, one preset per OS scheme. Stored under two
// keys so each half validates on its own — a garbage dark key must not
// drag a good light key back to its default. Absent keys give the pair
// every install had before this was configurable. index.html's pre-paint
// script reads the same two keys; keep the three in sync.
export const THEME_DARK_KEY = 'hive.theme.dark';
export const THEME_LIGHT_KEY = 'hive.theme.light';
export type StampableTheme = Exclude<ThemeName, 'system'>;
export interface SystemPair {
  dark: StampableTheme;
  light: StampableTheme;
}
export const DEFAULT_PAIR: SystemPair = {
  dark: 'hive-dark',
  light: 'hive-light',
};
// The <optgroup> headings, in the order the picker shows them. Nineteen flat
// entries is a scanning problem; three named buckets is not.
export const GROUPS = ['Hive', 'Native', 'Community'] as const;
export type Group = (typeof GROUPS)[number];

export interface Preset {
  id: ThemeName;
  label: string;
  group: Group;
}

// The picker renders from this list, so adding a preset is one line here
// plus its block in themes.css — plus the duplicated list in index.html's
// pre-paint script, which cannot import this module and is checked against
// this one by test/e2e/theme.spec.ts. Order is the order shown.
//
// "Community" is the ported-from-another-editor section. Those presets keep
// their upstream values rather than being re-fitted to this app's contrast
// bar, and carry `--contrast-exempt` in themes.css to say so; see
// docs/design-docs/ui/themes.md > Attribution.
export const PRESETS: readonly Preset[] = [
  { id: 'system', label: 'System', group: 'Hive' },
  { id: 'hive-dark', label: 'Hive Dark', group: 'Hive' },
  { id: 'hive-light', label: 'Hive Light', group: 'Hive' },
  { id: 'native-dark', label: 'Native Dark', group: 'Native' },
  { id: 'native-light', label: 'Native Light', group: 'Native' },
  { id: 'terminal', label: 'Terminal', group: 'Native' },
  { id: 'classic', label: 'Classic', group: 'Native' },
  { id: 'dracula', label: 'Dracula', group: 'Community' },
  { id: 'alucard', label: 'Alucard', group: 'Community' },
  { id: 'nord', label: 'Nord', group: 'Community' },
  { id: 'gruvbox-dark', label: 'Gruvbox Dark', group: 'Community' },
  { id: 'tokyo-night', label: 'Tokyo Night', group: 'Community' },
  { id: 'catppuccin-mocha', label: 'Catppuccin Mocha', group: 'Community' },
  { id: 'one-dark', label: 'One Dark', group: 'Community' },
  { id: 'neon', label: 'Neon', group: 'Community' },
  { id: 'solarized-dark', label: 'Solarized Dark', group: 'Community' },
  { id: 'solarized-light', label: 'Solarized Light', group: 'Community' },
  { id: 'catppuccin-latte', label: 'Catppuccin Latte', group: 'Community' },
  { id: 'github-dark', label: 'GitHub Dark', group: 'Community' },
  { id: 'github-light', label: 'GitHub Light', group: 'Community' },
  { id: 'hex', label: 'Hex', group: 'Community' },
];

// Everything resolveTheme can stamp on <html>. 'system' is a selection,
// not a value, so it is excluded.
const STAMPABLE = new Set<string>(
  PRESETS.map((p) => p.id).filter((id) => id !== 'system'),
);

export function resolveTheme(
  stored: string | null,
  prefersDark: boolean,
  pair: SystemPair = DEFAULT_PAIR,
): StampableTheme {
  if (stored === 'system') return prefersDark ? pair.dark : pair.light;
  if (stored && STAMPABLE.has(stored)) return stored as StampableTheme;
  return DEFAULT_THEME === 'system'
    ? prefersDark
      ? pair.dark
      : pair.light
    : DEFAULT_THEME;
}

// 'system' is rejected here on purpose: a half that said "follow the OS"
// would resolve back into this pair and never terminate.
function readHalf(s: Storage, key: string, fallback: StampableTheme) {
  const v = s.getItem(key);
  return STAMPABLE.has(v ?? '') ? (v as StampableTheme) : fallback;
}

export function readPair(storage?: Storage): SystemPair {
  try {
    const s = storage ?? localStorage;
    return {
      dark: readHalf(s, THEME_DARK_KEY, DEFAULT_PAIR.dark),
      light: readHalf(s, THEME_LIGHT_KEY, DEFAULT_PAIR.light),
    };
  } catch {
    return DEFAULT_PAIR;
  }
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

// `pair` defaults to the stored one; a caller that has just changed a
// half passes it in, so a store that refused the write still repaints.
export function applyTheme(
  name: ThemeName,
  doc: Document = document,
  pair: SystemPair = readPair(),
): void {
  const prefersDark =
    doc.defaultView?.matchMedia?.('(prefers-color-scheme: dark)').matches ??
    true;
  doc.documentElement.dataset.theme = resolveTheme(name, prefersDark, pair);
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
  // xterm 6 draws its own scrollbar and themes the slider from these three
  // keys. They read the same tokens base.css skins the app scrollbar with, so
  // the two cannot drift. Omitted when absent, for the same reason the ANSI
  // slots are: xterm's own default beats an empty string.
  const scrollbar: Record<string, string> = {};
  const SCROLLBAR_KEYS = [
    ['scrollbarSliderBackground', '--scrollbar-thumb'],
    ['scrollbarSliderHoverBackground', '--scrollbar-thumb-hover'],
    ['scrollbarSliderActiveBackground', '--scrollbar-thumb-active'],
  ] as const;
  for (const [key, token] of SCROLLBAR_KEYS) {
    const value = v(token);
    if (value) scrollbar[key] = value;
  }
  return {
    ...ansi,
    ...scrollbar,
    background: v('--term-bg'),
    foreground: v('--term-fg'),
    cursor: accent,
    cursorAccent: v('--on-accent'),
    // color-mix isn't resolvable via getPropertyValue; xterm accepts 8-digit hex.
    selectionBackground: accent.length === 7 ? `${accent}4d` : accent,
  };
}

// Parses #rgb / #rrggbb into [r, g, b], or null for anything else
// (color-mix(), rgb(), an unset token). Callers fall back rather than
// guess, because a wrong colour is worse than the plain accent.
export function parseHex(c: string): [number, number, number] | null {
  const m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(c.trim());
  if (!m) return null;
  const h =
    m[1].length === 3
      ? m[1]
          .split('')
          .map((x) => x + x)
          .join('')
      : m[1];
  return [0, 2, 4].map((i) => Number.parseInt(h.slice(i, i + 2), 16)) as [
    number,
    number,
    number,
  ];
}

// Mixes `a` over `b` at weight t (0..1) and returns #rrggbb, or `a`
// unchanged when either is not plain hex.
export function mixHex(a: string, b: string, t: number): string {
  const pa = parseHex(a);
  const pb = parseHex(b);
  if (!pa || !pb) return a;
  return `#${pa
    .map((x, i) =>
      Math.round(x * t + pb[i] * (1 - t))
        .toString(16)
        .padStart(2, '0'),
    )
    .join('')}`;
}

// Match highlighting for the in-session find (spec 431), from the theme.
//
// The active match is a solid accent fill — the brightest colour every
// preset defines — and every other match an accent outline, so matches
// are unmissable and the active one is never confused with the rest.
// @xterm/addon-search only accepts #RRGGBB, hence the normalization.
//
// Read per search, not once: a theme switch mid-session must carry over.
export function findDecorations(doc: Document = document) {
  const cs = getComputedStyle(doc.documentElement);
  const v = (n: string) => cs.getPropertyValue(n).trim();
  // Never undefined: the addon only reports result counts when
  // decorations are enabled, so dropping them would silently turn every
  // search into 0/0. A preset whose accent is not plain hex falls back
  // to the base token's accent.
  const accent = toHex6(v('--accent')) ?? FIND_FALLBACK_ACCENT;
  // No matchBackground on purpose. The active match is painted by a
  // second decoration on the same cells, and the terminal renderer lets
  // the ordinary match's fill win over the active one's — measured in a
  // browser, the active match was indistinguishable from the rest. With
  // only the active match carrying a fill, nothing can cover it; the
  // others are marked by a bright accent outline instead.
  return {
    matchBorder: accent,
    matchOverviewRuler: accent,
    activeMatchBackground: accent,
    activeMatchBorder: accent,
    activeMatchColorOverviewRuler: accent,
  };
}

// The terminal theme to use while a find box is open (spec 431).
//
// @xterm/addon-search marks the active match by SELECTING it, and the
// selection paints over the active decoration's fill — measured in a
// browser, the active match showed as the pale 30% selection tint, not
// the accent. While the box has focus the terminal is unfocused, so it
// is the inactive-selection colour that shows. Making the selection the
// solid accent (with the accent's own text colour) for the lifetime of
// the box is what makes the active match bright; closing the box puts
// xtermTheme() back, so ordinary text selection is unaffected.
export function findTermTheme(doc: Document = document) {
  const cs = getComputedStyle(doc.documentElement);
  const accent =
    toHex6(cs.getPropertyValue('--accent').trim()) ?? FIND_FALLBACK_ACCENT;
  const onAccent = cs.getPropertyValue('--on-accent').trim();
  return {
    ...xtermTheme(doc),
    selectionBackground: accent,
    selectionInactiveBackground: accent,
    ...(onAccent ? { selectionForeground: onAccent } : {}),
  };
}

// tokens.css's --accent: the value every preset starts from.
const FIND_FALLBACK_ACCENT = '#ffb454';

// Normalizes #rgb / #rrggbb to #rrggbb, or null when not plain hex.
function toHex6(c: string): string | null {
  return parseHex(c) ? mixHex(c, c, 1) : null;
}

// The terminal's font is a token like every other value, so xterm has to
// be told it explicitly — it has no cascade of its own. The fallback is
// for jsdom, where no stylesheet resolves and getPropertyValue returns ''.
export function monoFontFamily(doc: Document = document): string {
  return (
    getComputedStyle(doc.documentElement)
      .getPropertyValue('--font-mono')
      .trim() || 'Menlo, "DejaVu Sans Mono", monospace'
  );
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

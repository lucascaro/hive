// Preset selection. Runs before first paint (imported first in main.ts) so
// the app never flashes the default preset. docs/design-docs/ui/themes.md
export type ThemeName = 'classic' | 'hive-dark' | 'hive-light' | 'system';
export const THEME_KEY = 'hive.theme';
export const DEFAULT_THEME: ThemeName = 'classic';
const PRESETS = new Set(['classic', 'hive-dark', 'hive-light']);

export function resolveTheme(
  stored: string | null,
  prefersDark: boolean,
): Exclude<ThemeName, 'system'> {
  if (stored === 'system') return prefersDark ? 'hive-dark' : 'hive-light';
  if (stored && PRESETS.has(stored))
    return stored as Exclude<ThemeName, 'system'>;
  return DEFAULT_THEME === 'system'
    ? prefersDark
      ? 'hive-dark'
      : 'hive-light'
    : DEFAULT_THEME;
}

export function readTheme(storage: Storage = localStorage): ThemeName {
  try {
    const v = storage.getItem(THEME_KEY);
    return v === 'system' || PRESETS.has(v ?? '')
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

// Side effect on import: stamp before anything renders.
if (typeof document !== 'undefined') applyTheme(readTheme());
